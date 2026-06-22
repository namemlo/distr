package handlers

import (
	"net/http"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/observability/dashboards"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
)

func ObservabilityRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Observability"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		observabilityDashboardsFeatureFlagMiddleware,
	).Group(func(r chiopenapi.Router) {
		r.Get("/dashboards", getObservabilityDashboardsHandler()).
			With(option.Description("List static observability dashboard templates")).
			With(option.Response(http.StatusOK, api.ObservabilityDashboardListResponse{}))
	})
}

func getObservabilityDashboardsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		RespondJSON(w, api.ObservabilityDashboardListResponse{
			Dashboards: observabilityDashboardResponses(dashboards.Definitions()),
		})
	}
}

func observabilityDashboardResponses(definitions []dashboards.Definition) []api.ObservabilityDashboard {
	responses := make([]api.ObservabilityDashboard, 0, len(definitions))
	for _, definition := range definitions {
		responses = append(responses, api.ObservabilityDashboard{
			ID:          definition.ID,
			Name:        definition.Name,
			Description: definition.Description,
			Category:    definition.Category,
			Version:     definition.Version,
			Template:    definition.Template,
		})
	}
	return responses
}

func observabilityDashboardsFeatureFlagMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		registry := featureflags.NewRegistry(env.ExperimentalFeatureFlags())
		if !registry.IsEnabled(featureflags.KeyObservabilityDashboards) {
			http.NotFound(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	})
}
