package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentPolicyScopedAuthorizationUsesOrganizationResource(t *testing.T) {
	g := NewWithT(t)
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleAdmin
	organizationID := *userAuth.CurrentOrgID()
	called := false
	handler := requireControlPlaneOrganizationActionWithDependencies(
		types.ActionPolicyManage,
		controlPlaneResourceAuthorizationDependencies{
			resolveScopes: func(_ context.Context, resource types.ResourceRef) ([]types.ScopeRef, error) {
				g.Expect(resource).To(Equal(types.ResourceRef{
					OrganizationID: organizationID,
					Kind:           types.PermissionScopeOrganization,
					ID:             organizationID,
				}))
				return []types.ScopeRef{{
					Kind: types.PermissionScopeOrganization,
					ID:   organizationID,
				}}, nil
			},
			authorize: func(_ context.Context, request types.AccessRequest) (types.AccessDecision, error) {
				g.Expect(request.Action).To(Equal(types.ActionPolicyManage))
				g.Expect(request.DecisionAt).To(BeTemporally("~", time.Now(), time.Second))
				return types.AccessDecision{Allowed: true}, nil
			},
			isEffective: func(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error) {
				t.Fatal("policy authoring must not require selected-environment enrollment")
				return false, nil
			},
		},
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNoContent))
	g.Expect(called).To(BeTrue())
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
