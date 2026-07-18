package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestFailClosedUntilScopedAuthorizationAdapter(t *testing.T) {
	tests := []struct {
		name       string
		stackFound bool
		probeErr   error
		wantStatus int
		wantCalled bool
	}{
		{
			name:       "isolated branch delegates to legacy process guard",
			wantStatus: http.StatusNoContent,
			wantCalled: true,
		},
		{
			name:       "PR-066 stack cannot bypass scoped authorization",
			stackFound: true,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "probe failure denies",
			probeErr:   errors.New("catalog unavailable"),
			wantStatus: http.StatusForbidden,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			called := false
			handler := failClosedUntilScopedAuthorizationAdapter(
				func(context.Context) (bool, error) {
					return test.stackFound, test.probeErr
				},
			)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(
				recorder,
				httptest.NewRequest(http.MethodPost, "/", nil),
			)

			g.Expect(recorder.Code).To(Equal(test.wantStatus))
			g.Expect(called).To(Equal(test.wantCalled))
		})
	}
}

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

func TestDeploymentPolicyListRequestFromHTTP(t *testing.T) {
	g := NewWithT(t)
	request, ok := deploymentPolicyListRequestFromHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/deployment-policies?limit=25&cursor=eyJ2IjoxfQ",
			nil,
		),
	)
	g.Expect(ok).To(BeTrue())
	g.Expect(request.Limit).To(Equal(25))
	g.Expect(request.Cursor).To(Equal("eyJ2IjoxfQ"))

	for _, target := range []string{
		"/api/v1/deployment-policies?limit=0",
		"/api/v1/deployment-policies?limit=invalid",
		"/api/v1/deployment-policies?cursor=not+a+cursor",
	} {
		recorder := httptest.NewRecorder()
		_, ok := deploymentPolicyListRequestFromHTTP(
			recorder,
			httptest.NewRequest(http.MethodGet, target, nil),
		)
		g.Expect(ok).To(BeFalse())
		g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	}
}
