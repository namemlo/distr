package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/api"
	. "github.com/onsi/gomega"
)

func TestGetObservabilityDashboardsHandler(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/observability/dashboards", nil)

	getObservabilityDashboardsHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.ObservabilityDashboardListResponse
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.Dashboards).To(HaveLen(3))
	g.Expect(response.Dashboards[0].ID).To(Equal("http-overview"))
	g.Expect(response.Dashboards[0].Version).NotTo(BeEmpty())
	g.Expect(json.Valid(response.Dashboards[0].Template)).To(BeTrue())
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
