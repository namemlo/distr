package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestDeploymentPlansFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := deploymentPlansFeatureFlagMiddleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/deployment-plans", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}

func TestPreviousStateRouteUsesCurrentTenantAndActor(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plans.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring(`Post("/previous-state"`))
	g.Expect(text).To(ContainSubstring("CreatePreviousStatePlanForOrganization"))
	g.Expect(text).To(ContainSubstring("*authentication.CurrentOrgID()"))
	g.Expect(text).To(ContainSubstring("authentication.CurrentUserID()"))
	g.Expect(strings.Count(text, "RequireReadWriteOrAdmin")).To(BeNumerically(">=", 3))
}
