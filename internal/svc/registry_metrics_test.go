package svc

import (
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
)

func TestMetricsRouterReturnsNotFoundWhenObservabilityMetricsDisabled(t *testing.T) {
	g := NewWithT(t)
	registry := &Registry{
		promRegistry:                prometheus.NewRegistry(),
		observabilityMetricsEnabled: false,
	}

	recorder := httptest.NewRecorder()
	registry.GetMetricsRouter().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func TestMetricsRouterServesPrometheusWhenObservabilityMetricsEnabled(t *testing.T) {
	g := NewWithT(t)
	promRegistry := prometheus.NewRegistry()
	promRegistry.MustRegister(prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "distr_test_metric",
		Help: "Test metric",
	}))
	registry := &Registry{
		promRegistry:                promRegistry,
		observabilityMetricsEnabled: true,
	}

	recorder := httptest.NewRecorder()
	registry.GetMetricsRouter().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	g.Expect(recorder.Body.String()).To(ContainSubstring("# HELP distr_test_metric Test metric"))
}
