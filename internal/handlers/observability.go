package handlers

import (
	"net/http"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/observability/correlation"
	"github.com/distr-sh/distr/internal/observability/dashboards"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
)

const (
	observabilityTraceIDPlaceholder     = "${trace_id}"
	observabilitySpanIDPlaceholder      = "${span_id}"
	observabilityServicePlaceholder     = "${service}"
	observabilityEnvironmentPlaceholder = "${environment}"
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
	return getObservabilityDashboardsHandlerWithConfig(env.ObservabilityGrafanaBaseURL(), env.ExperimentalFeatureFlags())
}

func getObservabilityDashboardsHandlerWithConfig(grafanaBaseURL string, enabledFlags []featureflags.Key) http.HandlerFunc {
	registry := featureflags.NewRegistry(enabledFlags)
	return func(w http.ResponseWriter, r *http.Request) {
		RespondJSON(w, api.ObservabilityDashboardListResponse{
			Dashboards: observabilityDashboardResponses(dashboards.Definitions(), observabilityDashboardResponseOptions{
				GrafanaBaseURL:     grafanaBaseURL,
				IncludeCorrelation: registry.IsEnabled(featureflags.KeyObservabilityCorrelation),
			}),
		})
	}
}

type observabilityDashboardResponseOptions struct {
	GrafanaBaseURL     string
	IncludeCorrelation bool
}

func observabilityDashboardResponses(definitions []dashboards.Definition, options observabilityDashboardResponseOptions) []api.ObservabilityDashboard {
	responses := make([]api.ObservabilityDashboard, 0, len(definitions))
	for _, definition := range definitions {
		response := api.ObservabilityDashboard{
			ID:          definition.ID,
			Name:        definition.Name,
			Description: definition.Description,
			Category:    definition.Category,
			Version:     definition.Version,
			Template:    definition.Template,
		}
		if options.IncludeCorrelation {
			response.TraceLinkTemplate = correlation.BuildTraceLink(options.GrafanaBaseURL, observabilityTraceTemplateContext(definition.Correlation))
			response.MetricsQueryTemplate = definition.Correlation.MetricsQueryTemplate
			response.CorrelationHints = observabilityCorrelationHints(options.GrafanaBaseURL, definition)
		}
		responses = append(responses, response)
	}
	return responses
}

func observabilityTraceTemplateContext(metadata dashboards.CorrelationMetadata) correlation.CorrelationContext {
	context := correlation.CorrelationContext{
		TraceID: observabilityTraceIDPlaceholder,
		SpanID:  observabilitySpanIDPlaceholder,
	}
	for _, variable := range metadata.DashboardVariables {
		switch variable {
		case "service":
			context.Service = observabilityServicePlaceholder
		case "environment":
			context.Environment = observabilityEnvironmentPlaceholder
		}
	}
	return context
}

func observabilityCorrelationHints(baseURL string, definition dashboards.Definition) *api.ObservabilityDashboardCorrelationHints {
	filters := observabilityDashboardVariableFilters(definition.Correlation.DashboardVariables)
	return &api.ObservabilityDashboardCorrelationHints{
		TraceIDPlaceholder:    observabilityTraceIDPlaceholder,
		SpanIDPlaceholder:     observabilitySpanIDPlaceholder,
		ServiceLabel:          "service",
		EnvironmentLabel:      "environment",
		DashboardVariables:    append([]string{}, definition.Correlation.DashboardVariables...),
		MetricsLinkTemplate:   correlation.BuildMetricsLink(baseURL, definition.Correlation.MetricName, filters),
		DashboardLinkTemplate: correlation.BuildDashboardLink(baseURL, definition.ID, correlation.TimeRange{From: "now-1h", To: "now"}, filters),
	}
}

func observabilityDashboardVariableFilters(variables []string) map[string]string {
	filters := map[string]string{}
	for _, variable := range variables {
		switch variable {
		case "service":
			filters["service"] = observabilityServicePlaceholder
		case "environment":
			filters["environment"] = observabilityEnvironmentPlaceholder
		}
	}
	return filters
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
