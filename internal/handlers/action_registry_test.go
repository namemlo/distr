package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	. "github.com/onsi/gomega"
)

func TestGetActionDefinitionsHandler(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/action-definitions", nil)

	getActionDefinitionsHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var actions []api.ActionDefinition
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &actions)).To(Succeed())
	g.Expect(actions).To(HaveLen(5))
	g.Expect(actions[0].Type).To(Equal("distr.preflight"))
	g.Expect(actions[1].Type).To(Equal("distr.http.check"))
	g.Expect(actions[2].Type).To(Equal("distr.wait"))
	g.Expect(actions[3].Type).To(Equal("distr.compose.deploy"))
	g.Expect(actions[4].Type).To(Equal("distr.oci.job"))
	g.Expect(actions[1].InputSchema).To(HaveKeyWithValue("type", "object"))
}

func TestActionDefinitionsFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyDeploymentProcesses)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/action-definitions", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}
