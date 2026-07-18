package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/authjwt"
	"github.com/distr-sh/distr/internal/authkey"
	"github.com/distr-sh/distr/internal/authn"
	"github.com/distr-sh/distr/internal/authn/authinfo"
	"github.com/distr-sh/distr/internal/authorization"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/featureflags"
	obsermetrics "github.com/distr-sh/distr/internal/observability/metrics"
	obsertracing "github.com/distr-sh/distr/internal/observability/tracing"
	"github.com/distr-sh/distr/internal/oidc"
	"github.com/distr-sh/distr/internal/prometheus"
	"github.com/distr-sh/distr/internal/types"
	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-mailx/mailx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

func ContextInjectorMiddleware(
	db *pgxpool.Pool,
	mailer *mailx.Mailer,
	oidcer *oidc.OIDCer,
	prometheusCollector *prometheus.DistrCollector,
	metricsRecorder obsermetrics.Recorder,
	tracingTracer obsertracing.Tracer,
	s3Client *s3.Client,
) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			ctx = internalctx.WithDb(ctx, db)
			ctx = internalctx.WithMailer(ctx, mailer)
			ctx = internalctx.WithPrometheusCollector(ctx, prometheusCollector)
			ctx = internalctx.WithObservabilityMetricsRecorder(ctx, metricsRecorder)
			ctx = internalctx.WithObservabilityTracer(ctx, tracingTracer)
			ctx = internalctx.WithOIDCer(ctx, oidcer)
			if s3Client != nil {
				ctx = internalctx.WithS3Client(ctx, s3Client)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func LoggerCtxMiddleware(logger *zap.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger := logger.With(zap.String("requestId", middleware.GetReqID(r.Context())))
			ctx := internalctx.WithLogger(r.Context(), logger)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func LoggingMiddleware(handler http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		now := time.Now()
		handler.ServeHTTP(ww, r)
		elapsed := time.Since(now)
		logger := internalctx.GetLogger(r.Context())
		logger.Info("handling request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", ww.Status()),
			zap.String("time", elapsed.String()))
	}
	return http.HandlerFunc(fn)
}

func isSuperAdmin(ctx context.Context) bool {
	if auth, err := auth.Authentication.Get(ctx); err == nil {
		return auth.IsSuperAdmin()
	}
	return false
}

// RequireAnyUserRole remains for exact persisted-role gates, such as admin-only
// operations. Resource authorization should prefer scoped permission middleware.
func RequireAnyUserRole(userRoles ...types.UserRole) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if isSuperAdmin(ctx) {
				handler.ServeHTTP(w, r)
				return
			}
			if auth, err := auth.Authentication.Get(ctx); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
			} else if auth.CurrentUserRole() == nil || !slices.Contains(userRoles, *auth.CurrentUserRole()) {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
			} else {
				handler.ServeHTTP(w, r)
			}
		}
		return http.HandlerFunc(fn)
	}
}

func RequireScopedPermission(scoped types.ScopedPermission) func(handler http.Handler) http.Handler {
	return RequireAnyScopedPermission(scoped.Scope, scoped.Permission)
}

func RequireOrganizationPermission(permission types.Permission) func(handler http.Handler) http.Handler {
	return RequireScopedPermission(types.OrganizationPermission(permission))
}

func RequireAnyOrganizationPermission(permissions ...types.Permission) func(handler http.Handler) http.Handler {
	return RequireAnyScopedPermission(types.PermissionScopeOrganization, permissions...)
}

func RequireAnyScopedPermission(scope types.PermissionScope, permissions ...types.Permission) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if !scope.Supported() {
				http.Error(w, "unsupported permission scope", http.StatusForbidden)
				return
			}
			if isSuperAdmin(ctx) {
				handler.ServeHTTP(w, r)
				return
			}
			auth, err := auth.Authentication.Get(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			if auth.CurrentUserRole() == nil {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
			for _, permission := range permissions {
				if auth.CurrentUserRole().HasScopedPermission(types.ScopedPermission{
					Permission: permission,
					Scope:      scope,
				}) {
					handler.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, "insufficient permissions", http.StatusForbidden)
		}
		return http.HandlerFunc(fn)
	}
}

type ControlPlaneResourceResolver func(
	*http.Request,
	uuid.UUID,
) (types.ResourceRef, error)

func OrganizationResourceRef(
	_ *http.Request,
	organizationID uuid.UUID,
) (types.ResourceRef, error) {
	if organizationID == uuid.Nil {
		return types.ResourceRef{}, apierrors.ErrNotFound
	}
	return types.ResourceRef{
		OrganizationID: organizationID,
		Kind:           types.PermissionScopeOrganization,
		ID:             organizationID,
	}, nil
}

func PathResourceRef(
	scope types.PermissionScope,
	pathParameter string,
) ControlPlaneResourceResolver {
	return func(
		request *http.Request,
		organizationID uuid.UUID,
	) (types.ResourceRef, error) {
		id, err := uuid.Parse(request.PathValue(pathParameter))
		if err != nil || !scope.Supported() {
			return types.ResourceRef{}, apierrors.ErrNotFound
		}
		return types.ResourceRef{
			OrganizationID: organizationID,
			Kind:           scope,
			ID:             id,
		}, nil
	}
}

func RequireControlPlaneAction(
	action types.Action,
	resourceResolver ControlPlaneResourceResolver,
) func(http.Handler) http.Handler {
	return requireControlPlaneActionWith(
		action,
		resourceResolver,
		false,
		defaultControlPlaneActionDependencies(),
	)
}

func RequireEffectiveControlPlaneAction(
	action types.Action,
	resourceResolver ControlPlaneResourceResolver,
) func(http.Handler) http.Handler {
	return requireControlPlaneActionWith(
		action,
		resourceResolver,
		true,
		defaultControlPlaneActionDependencies(),
	)
}

type controlPlaneActionDependencies struct {
	processEnabled func() bool
	resolveScopes  func(context.Context, types.ResourceRef) ([]types.ScopeRef, error)
	authorize      func(context.Context, types.AccessRequest) (types.AccessDecision, error)
	isEffective    func(context.Context, uuid.UUID, uuid.UUID) (bool, error)
}

func defaultControlPlaneActionDependencies() controlPlaneActionDependencies {
	return controlPlaneActionDependencies{
		processEnabled: func() bool {
			return featureflags.NewRegistry(env.ExperimentalFeatureFlags()).
				IsEnabled(featureflags.KeyOperatorControlPlaneV2)
		},
		resolveScopes: authorization.ResolveResourceScopes,
		authorize:     authorization.Authorize,
		isEffective:   authorization.IsControlPlaneV2Effective,
	}
}

func requireControlPlaneActionWith(
	action types.Action,
	resourceResolver ControlPlaneResourceResolver,
	requireEnrollment bool,
	dependencies controlPlaneActionDependencies,
) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if dependencies.processEnabled == nil || !dependencies.processEnabled() {
				http.NotFound(w, request)
				return
			}
			if !action.Valid() ||
				dependencies.resolveScopes == nil ||
				resourceResolver == nil {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}

			authInfo, err := auth.Authentication.Get(request.Context())
			if err != nil ||
				authInfo.CurrentOrgID() == nil ||
				*authInfo.CurrentOrgID() == uuid.Nil ||
				authInfo.CurrentUserID() == uuid.Nil {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
			organizationID := *authInfo.CurrentOrgID()
			resource, err := resourceResolver(request, organizationID)
			if err != nil {
				writeControlPlaneAuthorizationError(w, request, err)
				return
			}
			scopes, err := dependencies.resolveScopes(request.Context(), resource)
			if err != nil {
				writeControlPlaneAuthorizationError(w, request, err)
				return
			}

			if dependencies.authorize == nil {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
			decision, err := dependencies.authorize(
				request.Context(),
				types.AccessRequest{
					OrganizationID: organizationID,
					PrincipalID:    authInfo.CurrentUserID(),
					Action:         action,
					ResourceScopes: scopes,
				},
			)
			if err != nil {
				http.Error(
					w,
					http.StatusText(http.StatusInternalServerError),
					http.StatusInternalServerError,
				)
				return
			}
			if !decision.Allowed {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}

			if requireEnrollment {
				if dependencies.isEffective == nil {
					http.NotFound(w, request)
					return
				}
				environmentIDs := make([]uuid.UUID, 0, 1)
				for _, scope := range scopes {
					if scope.Kind == types.PermissionScopeEnvironment &&
						!slices.Contains(environmentIDs, scope.ID) {
						environmentIDs = append(environmentIDs, scope.ID)
					}
				}
				if len(environmentIDs) == 0 {
					http.NotFound(w, request)
					return
				}
				for _, environmentID := range environmentIDs {
					effective, err := dependencies.isEffective(
						request.Context(),
						organizationID,
						environmentID,
					)
					if err != nil {
						http.Error(
							w,
							http.StatusText(http.StatusInternalServerError),
							http.StatusInternalServerError,
						)
						return
					}
					if !effective {
						http.NotFound(w, request)
						return
					}
				}
			}

			handler.ServeHTTP(w, request)
		})
	}
}

func writeControlPlaneAuthorizationError(
	w http.ResponseWriter,
	request *http.Request,
	err error,
) {
	if errors.Is(err, apierrors.ErrNotFound) ||
		errors.Is(err, authorization.ErrInvalidResourceRef) {
		http.NotFound(w, request)
		return
	}
	http.Error(
		w,
		http.StatusText(http.StatusInternalServerError),
		http.StatusInternalServerError,
	)
}

var (
	RequireReadWriteOrAdmin = RequireAnyOrganizationPermission(types.AllMutationPermissions()...)
	RequireAdmin            = RequireAnyUserRole(types.UserRoleAdmin)
)

func RequireAnySubscriptionType(types ...types.SubscriptionType) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if auth, err := auth.Authentication.Get(ctx); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
			} else if auth.CurrentOrg() == nil {
				http.Error(w, "inadequate access token", http.StatusForbidden)
			} else if !slices.Contains(types, auth.CurrentOrg().SubscriptionType) {
				typesStr := make([]string, 0, len(types))
				for _, t := range types {
					typesStr = append(typesStr, string(t))
				}
				http.Error(w, fmt.Sprintf(
					"this operation can only be performed on an organization with one of the following subscription types: %v",
					strings.Join(typesStr, ", "),
				), http.StatusForbidden)
			} else {
				handler.ServeHTTP(w, r)
			}
		}
		return http.HandlerFunc(fn)
	}
}

var ProFeature = RequireAnySubscriptionType(
	types.SubscriptionTypePro,
	types.SubscriptionTypeTrial,
	types.SubscriptionTypeEnterprise,
)

func RequireVendor(handler http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if isSuperAdmin(ctx) {
			handler.ServeHTTP(w, r)
			return
		}
		if auth, err := auth.Authentication.Get(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else if auth.CurrentCustomerOrgID() != nil || auth.CurrentPartnerOrgID() != nil {
			http.Error(w, "insufficient permissions", http.StatusForbidden)
		} else {
			handler.ServeHTTP(w, r)
		}
	}
	return http.HandlerFunc(fn)
}

func RequireVendorOrPartner(handler http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if isSuperAdmin(ctx) {
			handler.ServeHTTP(w, r)
			return
		}
		if auth, err := auth.Authentication.Get(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
		} else if auth.CurrentCustomerOrgID() != nil {
			http.Error(w, "insufficient permissions", http.StatusForbidden)
		} else {
			handler.ServeHTTP(w, r)
		}
	}
	return http.HandlerFunc(fn)
}

var Sentry = sentryhttp.New(sentryhttp.Options{Repanic: true}).Handle

// SetSentryUserFromUserAuth sets the authenticated user's identity on the Sentry scope. It
// must run after auth.Authentication.Middleware so the user is available in the context; if
// there is no authenticated user it panics, since that is a wiring bug.
func SetSentryUserFromUserAuth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if hub := sentry.GetHubFromContext(ctx); hub != nil {
			auth := auth.Authentication.Require(ctx)
			hub.Scope().SetUser(sentry.User{
				ID:    auth.CurrentUserID().String(),
				Email: auth.CurrentUserEmail(),
			})
		}
		h.ServeHTTP(w, r)
	})
}

// SetSentryUserFromAgentAuth sets the authenticated agent's identity on the Sentry scope. It
// must run after auth.AgentAuthentication.Middleware so the agent is available in the context;
// if there is no authenticated agent it panics, since that is a wiring bug.
func SetSentryUserFromAgentAuth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if hub := sentry.GetHubFromContext(ctx); hub != nil {
			auth := auth.AgentAuthentication.Require(ctx)
			hub.Scope().SetUser(sentry.User{
				ID: auth.CurrentDeploymentTargetID().String(),
			})
		}
		h.ServeHTTP(w, r)
	})
}

func getTokenIdKey(token any, id uuid.UUID) string {
	prefix := ""
	switch token.(type) {
	case jwt.Token:
		prefix = "jwt"
	case authkey.Key:
		prefix = "authkey"
	default:
		panic("unknown token type")
	}
	return fmt.Sprintf("%v-%v", prefix, id)
}

var RequireOrgAndRole = auth.Authentication.ValidatorMiddleware(
	func(value authinfo.AuthInfoWithUserAndOrganization) error {
		if value.IsSuperAdmin() {
			// Super admins still need org context, but don't need a role
			if value.CurrentOrgID() == nil || value.CurrentOrg() == nil {
				return authn.ErrBadAuthentication
			}
			return nil
		}
		if value.CurrentOrgID() == nil || value.CurrentOrg() == nil || value.CurrentUserRole() == nil {
			return authn.ErrBadAuthentication
		}
		return nil
	},
)

// RequireTokenScope rejects the request unless the authenticated credential was minted with the
// given token scope. It restricts the password-setting endpoints to their dedicated special tokens:
// regular login tokens, PATs and agent tokens carry the empty scope and are therefore rejected.
func RequireTokenScope(scope authjwt.TokenScope) func(http.Handler) http.Handler {
	return auth.Authentication.ValidatorMiddleware(
		func(value authinfo.AuthInfoWithUserAndOrganization) error {
			if value.TokenScope() != scope {
				return authn.ErrBadAuthentication
			}
			return nil
		},
	)
}

// RequireEmailVerified rejects requests with 403 when USER_EMAIL_VERIFICATION_REQUIRED is
// enabled and the authenticated user's DB record has no EmailVerifiedAt. It must run after
// auth.Authentication.Middleware so the DB-loaded user is available in the context; if
// there is no authenticated user it panics, since that is a wiring bug.
func RequireEmailVerified(handler http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		if !env.UserEmailVerificationRequired() {
			handler.ServeHTTP(w, r)
			return
		}
		value := auth.Authentication.Require(r.Context())
		if user := value.CurrentUser(); user == nil || user.EmailVerifiedAt == nil {
			http.Error(w, "email not verified", http.StatusForbidden)
		} else {
			handler.ServeHTTP(w, r)
		}
	}
	return http.HandlerFunc(fn)
}

func BlockSuperAdmin(handler http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		if isSuperAdmin(r.Context()) {
			http.Error(w, "super admins cannot modify resources", http.StatusForbidden)
			return
		}
		handler.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

func BlockSuperAdminUnlessOrganizationExpired(handler http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if isSuperAdmin(ctx) {
			org := auth.Authentication.Require(ctx).CurrentOrg()
			if org == nil || org.HasActiveSubscription() {
				http.Error(
					w,
					"super admins cannot delete an active organization",
					http.StatusForbidden,
				)
				return
			}
		}
		handler.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

func FeatureFlagMiddleware(feature types.Feature) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if auth, err := auth.Authentication.Get(ctx); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
			} else {
				org := auth.CurrentOrg()
				if !org.HasFeature(feature) {
					http.Error(w, fmt.Sprintf("%v not enabled for organization", feature), http.StatusForbidden)
				} else {
					handler.ServeHTTP(w, r)
				}
			}
		}
		return http.HandlerFunc(fn)
	}
}

var (
	LicensingFeatureFlagEnabledMiddleware = FeatureFlagMiddleware(types.FeatureLicensing)
	VendorBillingFeatureMiddleware        = FeatureFlagMiddleware(types.FeatureVendorBilling)
	PartnerManagementFeatureMiddleware    = FeatureFlagMiddleware(types.FeaturePartnerManagement)
)

func ExperimentalFeatureFlagMiddleware(feature featureflags.Key) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			registry := featureflags.NewRegistry(env.ExperimentalFeatureFlags())
			if !registry.IsEnabled(feature) {
				http.Error(w, fmt.Sprintf("experimental feature flag %q not enabled", feature), http.StatusForbidden)
				return
			}
			handler.ServeHTTP(w, r)
		}
		return http.HandlerFunc(fn)
	}
}

func SetRequestPattern(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.Pattern == "" {
			r.Pattern = chi.RouteContext(r.Context()).RoutePattern()
		}
	})
}

func OTEL(provider trace.TracerProvider) func(next http.Handler) http.Handler {
	mw := otelhttp.NewMiddleware(
		"",
		otelhttp.WithTracerProvider(provider),
		otelhttp.WithSpanNameFormatter(
			func(operation string, r *http.Request) string {
				var b strings.Builder
				if operation != "" {
					b.WriteString(operation)
					b.WriteString(" ")
				}
				b.WriteString(r.Method)
				if r.Pattern != "" {
					b.WriteString(" ")
					b.WriteString(r.Pattern)
				}
				return b.String()
			},
		),
	)
	return func(next http.Handler) http.Handler {
		return mw(SetRequestPattern(next))
	}
}
