package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/authorization"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type controlPlaneResourceAuthorizationRequest struct {
	OrganizationID    uuid.UUID
	PrincipalID       uuid.UUID
	CredentialRole    *types.UserRole
	IsSuperAdmin      bool
	Action            types.Action
	Resource          types.ResourceRef
	DecisionAt        time.Time
	RequireEnrollment bool
	EnvironmentID     uuid.UUID
}

type controlPlaneResourceAuthorizationDependencies struct {
	resolveScopes func(context.Context, types.ResourceRef) ([]types.ScopeRef, error)
	authorize     func(context.Context, types.AccessRequest) (types.AccessDecision, error)
	isEffective   func(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error)
}

func defaultControlPlaneResourceAuthorizationDependencies() controlPlaneResourceAuthorizationDependencies {
	return controlPlaneResourceAuthorizationDependencies{
		resolveScopes: authorization.ResolveResourceScopes,
		authorize:     authorization.Authorize,
		isEffective:   authorization.IsControlPlaneV2EffectiveAt,
	}
}

func authorizeControlPlaneResource(
	ctx context.Context,
	request controlPlaneResourceAuthorizationRequest,
) error {
	return authorizeControlPlaneResourceWithDependencies(
		ctx,
		request,
		defaultControlPlaneResourceAuthorizationDependencies(),
	)
}

func authorizeControlPlaneResourceWithDependencies(
	ctx context.Context,
	request controlPlaneResourceAuthorizationRequest,
	dependencies controlPlaneResourceAuthorizationDependencies,
) error {
	if request.OrganizationID == uuid.Nil ||
		request.PrincipalID == uuid.Nil ||
		request.CredentialRole == nil ||
		request.IsSuperAdmin ||
		!request.Action.Valid() ||
		request.DecisionAt.IsZero() ||
		request.Resource.OrganizationID != request.OrganizationID ||
		dependencies.resolveScopes == nil ||
		dependencies.authorize == nil {
		return apierrors.ErrForbidden
	}

	scopes, err := dependencies.resolveScopes(ctx, request.Resource)
	if err != nil {
		return err
	}
	decision, err := dependencies.authorize(ctx, types.AccessRequest{
		OrganizationID: request.OrganizationID,
		PrincipalID:    request.PrincipalID,
		CredentialRole: request.CredentialRole,
		IsSuperAdmin:   request.IsSuperAdmin,
		DecisionAt:     request.DecisionAt,
		Action:         request.Action,
		ResourceScopes: scopes,
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return apierrors.ErrForbidden
	}
	if !request.RequireEnrollment {
		return nil
	}
	if request.EnvironmentID == uuid.Nil || dependencies.isEffective == nil {
		return apierrors.ErrForbidden
	}
	environmentScopeResolved := false
	for _, scope := range scopes {
		if scope.Kind == types.PermissionScopeEnvironment &&
			scope.ID == request.EnvironmentID {
			environmentScopeResolved = true
			break
		}
	}
	if !environmentScopeResolved {
		return apierrors.ErrForbidden
	}
	effective, err := dependencies.isEffective(
		ctx,
		request.OrganizationID,
		request.EnvironmentID,
		request.DecisionAt,
	)
	if err != nil {
		return err
	}
	if !effective {
		return apierrors.ErrForbidden
	}
	return nil
}

func requireControlPlaneOrganizationAction(
	action types.Action,
) func(http.Handler) http.Handler {
	return requireControlPlaneOrganizationActionWithDependencies(
		action,
		defaultControlPlaneResourceAuthorizationDependencies(),
	)
}

func requireControlPlaneOrganizationActionWithDependencies(
	action types.Action,
	dependencies controlPlaneResourceAuthorizationDependencies,
) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authInfo, err := auth.Authentication.Get(r.Context())
			if err != nil || authInfo.CurrentOrgID() == nil {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
			organizationID := *authInfo.CurrentOrgID()
			err = authorizeControlPlaneResourceWithDependencies(
				r.Context(),
				controlPlaneResourceAuthorizationRequest{
					OrganizationID: organizationID,
					PrincipalID:    authInfo.CurrentUserID(),
					CredentialRole: authInfo.CurrentUserRole(),
					IsSuperAdmin:   authInfo.IsSuperAdmin(),
					Action:         action,
					Resource: types.ResourceRef{
						OrganizationID: organizationID,
						Kind:           types.PermissionScopeOrganization,
						ID:             organizationID,
					},
					DecisionAt: time.Now().UTC(),
				},
				dependencies,
			)
			if err != nil {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}
}

// requireIntegratedCampaignControlAuthorization is an explicit synthetic-stack
// stop. PR-066 owns the effective enrollment and scoped-action resolver, so a
// branch that does not contain PR-066 must not approximate campaign.control
// with legacy organization roles. Ordered integration replaces this function
// at the call sites with RequireEffectiveControlPlaneAction.
func requireIntegratedCampaignControlAuthorization(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "campaign.control authorization is not integrated", http.StatusForbidden)
	})
}

func operatorControlPlaneMutationAccessMiddlewareWithFlags(
	enabledFlags []featureflags.Key,
) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		flagged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !featureflags.NewRegistry(enabledFlags).IsEnabled(featureflags.KeyOperatorControlPlaneV2) {
				http.NotFound(w, r)
				return
			}
			handler.ServeHTTP(w, r)
		})
		protected := middleware.RequireReadWriteOrAdmin(middleware.BlockSuperAdmin(flagged))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				handler.ServeHTTP(w, r)
			default:
				protected.ServeHTTP(w, r)
			}
		})
	}
}
