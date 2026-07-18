package handlers

import (
	"net/http"

	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
)

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
