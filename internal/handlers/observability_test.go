package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/observability/dashboards"
	. "github.com/onsi/gomega"
)

func TestGetObservabilityDashboardsHandler(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/observability/dashboards", nil)

	getObservabilityDashboardsHandlerWithConfig("", nil).ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.ObservabilityDashboardListResponse
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.Dashboards).To(HaveLen(3))
	g.Expect(response.Dashboards[0].ID).To(Equal("http-overview"))
	g.Expect(response.Dashboards[0].Version).NotTo(BeEmpty())
	g.Expect(json.Valid(response.Dashboards[0].Template)).To(BeTrue())
	g.Expect(response.Dashboards[0].TraceLinkTemplate).To(BeEmpty())
	g.Expect(response.Dashboards[0].MetricsQueryTemplate).To(BeEmpty())
	g.Expect(response.Dashboards[0].CorrelationHints).To(BeNil())
}

func TestGetObservabilityDashboardsHandlerIncludesCorrelationMetadataWhenEnabled(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/observability/dashboards", nil)

	getObservabilityDashboardsHandlerWithConfig(
		"https://grafana.example.com/ops",
		[]featureflags.Key{featureflags.KeyObservabilityCorrelation},
	).ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.ObservabilityDashboardListResponse
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.Dashboards).To(HaveLen(3))

	httpDashboard := response.Dashboards[0]
	g.Expect(httpDashboard.ID).To(Equal("http-overview"))
	g.Expect(httpDashboard.TraceLinkTemplate).To(ContainSubstring("https://grafana.example.com/ops/explore?"))
	g.Expect(httpDashboard.TraceLinkTemplate).To(ContainSubstring("spanId=%24%7Bspan_id%7D"))
	g.Expect(httpDashboard.TraceLinkTemplate).To(ContainSubstring("var-service=%24%7Bservice%7D"))
	g.Expect(httpDashboard.MetricsQueryTemplate).To(Equal("sum(rate(distr_http_requests_total[$__rate_interval])) by (status_class)"))
	g.Expect(httpDashboard.CorrelationHints).NotTo(BeNil())
	g.Expect(httpDashboard.CorrelationHints.TraceIDPlaceholder).To(Equal("${trace_id}"))
	g.Expect(httpDashboard.CorrelationHints.SpanIDPlaceholder).To(Equal("${span_id}"))
	g.Expect(httpDashboard.CorrelationHints.ServiceLabel).To(Equal("service"))
	g.Expect(httpDashboard.CorrelationHints.EnvironmentLabel).To(Equal("environment"))
	g.Expect(httpDashboard.CorrelationHints.DashboardVariables).To(Equal([]string{"environment", "service"}))
	g.Expect(httpDashboard.CorrelationHints.MetricsLinkTemplate).To(ContainSubstring("https://grafana.example.com/ops/explore?"))
	g.Expect(httpDashboard.CorrelationHints.MetricsLinkTemplate).To(ContainSubstring("distr_http_requests_total"))
	g.Expect(httpDashboard.CorrelationHints.DashboardLinkTemplate).To(ContainSubstring("https://grafana.example.com/ops/d/http-overview?"))
	g.Expect(httpDashboard.CorrelationHints.DashboardLinkTemplate).To(ContainSubstring("from=now-1h"))
	g.Expect(httpDashboard.CorrelationHints.DashboardLinkTemplate).To(ContainSubstring("var-environment=%24%7Benvironment%7D"))
}

func TestObservabilityDashboardResponsesAreDeterministicWhenEnriched(t *testing.T) {
	g := NewWithT(t)
	options := observabilityDashboardResponseOptions{
		GrafanaBaseURL:     "https://grafana.example.com/ops",
		IncludeCorrelation: true,
	}

	first := observabilityDashboardResponses(dashboards.Definitions(), options)
	second := observabilityDashboardResponses(dashboards.Definitions(), options)

	firstJSON, err := json.Marshal(first)
	g.Expect(err).NotTo(HaveOccurred())
	secondJSON, err := json.Marshal(second)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(firstJSON).To(Equal(secondJSON))
}

func TestObservabilityDashboardsFeatureFlagMiddlewareReturnsNotFoundWhenDisabled(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := observabilityDashboardsFeatureFlagMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/observability/dashboards", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
	g.Expect(called).To(BeFalse())
}
