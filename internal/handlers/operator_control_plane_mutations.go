package handlers

import (
	"context"
	"net/http"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
)

type scopedAuthorizationStackProbe func(context.Context) (bool, error)

func pr066ScopedAuthorizationSchemaPresent(
	ctx context.Context,
) (bool, error) {
	var present bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
		  to_regclass(
		    format('%I.%I', current_schema(), 'roledefinition')
		  ) IS NOT NULL
		  OR to_regclass(
		    format('%I.%I', current_schema(), 'controlplaneenrollment')
		  ) IS NOT NULL
	`).Scan(&present)
	return present, err
}

func failClosedUntilScopedAuthorizationAdapter(
	probe scopedAuthorizationStackProbe,
) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			stackPresent, err := probe(r.Context())
			if err != nil || stackPresent {
				// PR-066 must replace this isolated-branch adapter with
				// RequireControlPlaneAction(policy.manage, ...) plus effective
				// ControlPlaneEnrollment resolution. Presence or uncertainty
				// therefore denies instead of falling back to legacy roles.
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}
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
