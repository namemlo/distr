package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestApprovalDecisionScopeDenialStopsBeforeRepositoryMutation(t *testing.T) {
	g := NewWithT(t)
	called := false
	requestID := uuid.New()
	requirementID := uuid.New()
	handler := recordApprovalDecisionHandlerWithDependencies(approvalHandlerDependencies{
		authorizeDecision: func(context.Context, approvalAuthorizationRequest) error {
			return apierrors.NewForbidden("approval.decide is denied for this scope")
		},
		recordDecision: func(
			ctx context.Context,
			input types.ApprovalDecisionInput,
		) (*types.ApprovalDecision, error) {
			if err := input.Authorize(ctx, types.ApprovalAuthorizationContext{
				OrganizationID:        input.OrganizationID,
				ActorUserAccountID:    input.ActorUserAccountID,
				DecisionAt:            time.Now().UTC(),
				DeploymentPlanID:      uuid.New(),
				ApprovalRequestID:     input.ApprovalRequestID,
				ApprovalRequirementID: input.ApprovalRequirementID,
			}); err != nil {
				return nil, err
			}
			called = true
			return nil, nil
		},
	})
	body := `{"approvalRequirementId":"` + requirementID.String() +
		`","decision":"APPROVE","comment":"Reviewed immutable evidence.",` +
		`"expectedRequestRevision":1,"idempotencyKey":"decision-1"}`
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/approval-requests/"+requestID.String()+"/decisions",
		strings.NewReader(body),
	)
	request.SetPathValue("approvalRequestId", requestID.String())
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleAdmin
	request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	g.Expect(response.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}

func TestApprovalDecisionPreservesSampleRetirementAuthorizationContext(t *testing.T) {
	g := NewWithT(t)
	requestID := uuid.New()
	requirementID := uuid.New()
	jobID := uuid.New()
	var gotAuthorization approvalAuthorizationRequest
	handler := recordApprovalDecisionHandlerWithDependencies(approvalHandlerDependencies{
		authorizeDecision: func(
			_ context.Context,
			request approvalAuthorizationRequest,
		) error {
			gotAuthorization = request
			return nil
		},
		recordDecision: func(
			ctx context.Context,
			input types.ApprovalDecisionInput,
		) (*types.ApprovalDecision, error) {
			err := input.Authorize(ctx, types.ApprovalAuthorizationContext{
				OrganizationID:        input.OrganizationID,
				ActorUserAccountID:    input.ActorUserAccountID,
				DecisionAt:            time.Now().UTC(),
				SampleRetirementJobID: jobID,
				ApprovalRequestID:     input.ApprovalRequestID,
				ApprovalRequirementID: input.ApprovalRequirementID,
			})
			if err != nil {
				return nil, err
			}
			return &types.ApprovalDecision{
				ID:                    uuid.New(),
				ApprovalRequestID:     input.ApprovalRequestID,
				ApprovalRequirementID: input.ApprovalRequirementID,
				ActorUserAccountID:    input.ActorUserAccountID,
				Decision:              input.Decision,
				Comment:               input.Comment,
				RequestRevision:       input.ExpectedRequestRevision + 1,
				IdempotencyKey:        input.IdempotencyKey,
			}, nil
		},
	})
	body := `{"approvalRequirementId":"` + requirementID.String() +
		`","decision":"APPROVE","comment":"Reviewed immutable retirement evidence.",` +
		`"expectedRequestRevision":1,"idempotencyKey":"retirement-decision-1"}`
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/approval-requests/"+requestID.String()+"/decisions",
		strings.NewReader(body),
	)
	request.SetPathValue("approvalRequestId", requestID.String())
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleAdmin
	request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	g.Expect(response.Code).To(Equal(http.StatusOK))
	g.Expect(gotAuthorization.SampleRetirementJobID).To(Equal(jobID))
	g.Expect(gotAuthorization.EnvironmentID).To(Equal(uuid.Nil))
	g.Expect(gotAuthorization.DeploymentUnitID).To(BeNil())
}

func TestApprovalScopedAuthorizationUsesRetirementOrganizationScopeAndPreservesPlanScope(t *testing.T) {
	organizationID := uuid.New()
	actorID := uuid.New()
	role := types.UserRoleAdmin
	decisionAt := time.Now().UTC()

	t.Run("sample retirement", func(t *testing.T) {
		g := NewWithT(t)
		jobID := uuid.New()
		var gotResource types.ResourceRef
		var gotAccess types.AccessRequest
		err := approvalScopedAuthorizationWithDependencies(
			t.Context(),
			approvalAuthorizationRequest{
				OrganizationID:        organizationID,
				ActorUserAccountID:    actorID,
				CredentialRole:        &role,
				Action:                string(types.ActionApprovalDecide),
				DecisionAt:            decisionAt,
				SampleRetirementJobID: jobID,
			},
			controlPlaneResourceAuthorizationDependencies{
				resolveScopes: func(
					_ context.Context,
					resource types.ResourceRef,
				) ([]types.ScopeRef, error) {
					gotResource = resource
					return []types.ScopeRef{{
						Kind: types.PermissionScopeOrganization,
						ID:   organizationID,
					}}, nil
				},
				authorize: func(
					_ context.Context,
					request types.AccessRequest,
				) (types.AccessDecision, error) {
					gotAccess = request
					return types.AccessDecision{Allowed: true}, nil
				},
			},
		)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(gotResource).To(Equal(types.ResourceRef{
			OrganizationID: organizationID,
			Kind:           types.PermissionScopeOrganization,
			ID:             organizationID,
		}))
		g.Expect(gotAccess.Action).To(Equal(types.ActionApprovalDecide))
	})

	t.Run("deployment plan", func(t *testing.T) {
		g := NewWithT(t)
		environmentID := uuid.New()
		unitID := uuid.New()
		var gotResource types.ResourceRef
		err := approvalScopedAuthorizationWithDependencies(
			t.Context(),
			approvalAuthorizationRequest{
				OrganizationID:     organizationID,
				ActorUserAccountID: actorID,
				CredentialRole:     &role,
				Action:             string(types.ActionApprovalDecide),
				DecisionAt:         decisionAt,
				DeploymentPlanID:   uuid.New(),
				EnvironmentID:      environmentID,
				DeploymentUnitID:   &unitID,
			},
			controlPlaneResourceAuthorizationDependencies{
				resolveScopes: func(
					_ context.Context,
					resource types.ResourceRef,
				) ([]types.ScopeRef, error) {
					gotResource = resource
					return []types.ScopeRef{
						{Kind: types.PermissionScopeOrganization, ID: organizationID},
						{Kind: types.PermissionScopeEnvironment, ID: environmentID},
						{Kind: types.PermissionScopeDeploymentUnit, ID: unitID},
					}, nil
				},
				authorize: func(
					context.Context,
					types.AccessRequest,
				) (types.AccessDecision, error) {
					return types.AccessDecision{Allowed: true}, nil
				},
			},
		)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(gotResource).To(Equal(types.ResourceRef{
			OrganizationID: organizationID,
			Kind:           types.PermissionScopeDeploymentUnit,
			ID:             unitID,
		}))
	})
}

func TestApprovalJSONBodyRejectsUnknownFields(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/",
		strings.NewReader(`{"expiresAt":"2026-07-19T08:00:00Z","unknown":true}`),
	)
	recorder := httptest.NewRecorder()

	_, err := approvalJSONBody[api.CreateApprovalRequestRequest](recorder, request)

	g := NewWithT(t)
	g.Expect(err).To(HaveOccurred())
	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
}

func TestApprovalMutationGuardKeepsReadsAvailableWhenFlagDisabled(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := approvalMutationAccessMiddlewareWithFlags(nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleAdmin

	mutation := httptest.NewRequest(http.MethodPost, "/api/v1/approval-requests", nil)
	mutation = mutation.WithContext(auth.Authentication.NewContext(mutation.Context(), userAuth))
	mutationResponse := httptest.NewRecorder()
	handler.ServeHTTP(mutationResponse, mutation)
	g.Expect(mutationResponse.Code).To(Equal(http.StatusNotFound))
	g.Expect(called).To(BeFalse())

	read := httptest.NewRequest(http.MethodGet, "/api/v1/approval-requests", nil)
	read = read.WithContext(auth.Authentication.NewContext(read.Context(), userAuth))
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, read)
	g.Expect(readResponse.Code).To(Equal(http.StatusNoContent))
	g.Expect(called).To(BeTrue())
}

func TestCreateApprovalRequestUsesServerClockForExpiryValidation(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
	request := api.CreateApprovalRequestRequest{ExpiresAt: now.Add(time.Hour)}
	g.Expect(request.Validate(now)).To(Succeed())
}
