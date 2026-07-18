package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestDeploymentPolicyMutationAccessMiddleware(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := deploymentPolicyMutationAccessMiddlewareWithFlags(nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleAdmin

	mutation := httptest.NewRequest(http.MethodPost, "/api/v1/deployment-policies", nil)
	mutation = mutation.WithContext(auth.Authentication.NewContext(mutation.Context(), userAuth))
	mutationResponse := httptest.NewRecorder()
	handler.ServeHTTP(mutationResponse, mutation)
	g.Expect(mutationResponse.Code).To(Equal(http.StatusNotFound))
	g.Expect(called).To(BeFalse())

	read := httptest.NewRequest(http.MethodGet, "/api/v1/deployment-policies", nil)
	read = read.WithContext(auth.Authentication.NewContext(read.Context(), userAuth))
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, read)
	g.Expect(readResponse.Code).To(Equal(http.StatusNoContent))
	g.Expect(called).To(BeTrue())
}

func TestDeploymentPolicyJSONBodyRejectsUnknownFieldsAndTrailingValues(t *testing.T) {
	g := NewWithT(t)
	for _, body := range []string{
		`{"key":"standard-dev","name":"Standard DEV","unknown":true}`,
		`{"key":"standard-dev","name":"Standard DEV"} {}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		recorder := httptest.NewRecorder()

		_, err := deploymentPolicyJSONBody[api.CreateDeploymentPolicyRequest](
			recorder,
			request,
		)

		g.Expect(err).To(HaveOccurred())
		g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	}
}
