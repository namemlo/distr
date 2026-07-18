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
