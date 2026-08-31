package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestAdmissionScopeDenialStopsBeforePersistence(t *testing.T) {
	g := NewWithT(t)
	planID := uuid.New()
	persisted := false
	dependencies := admissionHandlerDependencies{
		clock: func() time.Time {
			return time.Date(2026, time.July, 18, 12, 0, 0, 0, time.UTC)
		},
		authorize: func(
			context.Context,
			types.AdmissionAuthorizationContext,
		) error {
			return apierrors.NewForbidden("plan.execute is denied for this enrollment")
		},
		admit: func(
			ctx context.Context,
			request types.AdmitDeploymentPlanRequest,
		) (*types.AdmissionEvaluation, error) {
			if err := request.Authorize(ctx, types.AdmissionAuthorizationContext{
				OrganizationID:     request.OrganizationID,
				ActorUserAccountID: request.ActorUserAccountID,
				DeploymentPlanID:   request.DeploymentPlanID,
				EnvironmentID:      uuid.New(),
				DeploymentUnitID:   new(uuid.UUID),
				Action:             "plan.execute",
				DecisionAt:         time.Date(2026, time.July, 18, 12, 0, 0, 0, time.UTC),
			}); err != nil {
				return nil, err
			}
			persisted = true
			return &types.AdmissionEvaluation{}, nil
		},
	}
	handler := admitDeploymentPlanHandlerWithDependencies(dependencies)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-plans/"+planID.String()+"/admission",
		strings.NewReader(
			`{"schedulerIdempotencyKey":"scheduler:1"}`,
		),
	)
	request.SetPathValue("deploymentPlanId", planID.String())
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleAdmin
	request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	g.Expect(response.Code).To(Equal(http.StatusForbidden))
	g.Expect(persisted).To(BeFalse())
}

func TestAdmissionRejectsCallerSuppliedClockAndGateEvidence(t *testing.T) {
	g := NewWithT(t)
	planID := uuid.New()
	admitCalls := 0
	dependencies := admissionHandlerDependencies{
		admit: func(
			context.Context,
			types.AdmitDeploymentPlanRequest,
		) (*types.AdmissionEvaluation, error) {
			admitCalls++
			return &types.AdmissionEvaluation{}, nil
		},
	}
	handler := admitDeploymentPlanHandlerWithDependencies(dependencies)
	for _, body := range []string{
		`{"schedulerIdempotencyKey":"scheduler:1","evaluatedAt":"2026-07-18T12:00:00Z"}`,
		`{"schedulerIdempotencyKey":"scheduler:1","gateEvidence":[]}`,
	} {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/deployment-plans/"+planID.String()+"/admission",
			strings.NewReader(body),
		)
		request.SetPathValue("deploymentPlanId", planID.String())
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		g.Expect(response.Code).To(Equal(http.StatusBadRequest))
	}
	g.Expect(admitCalls).To(Equal(0))
}

func TestEmergencyOverrideScopeDenialStopsBeforePersistence(t *testing.T) {
	g := NewWithT(t)
	planID := uuid.New()
	approvalID := uuid.New()
	persisted := false
	dependencies := admissionHandlerDependencies{
		clock: func() time.Time {
			return time.Date(2026, time.July, 18, 12, 0, 0, 0, time.UTC)
		},
		authorize: func(
			context.Context,
			types.AdmissionAuthorizationContext,
		) error {
			return apierrors.NewForbidden("emergency.override is denied for this enrollment")
		},
		createOverride: func(
			ctx context.Context,
			request types.CreateEmergencyOverrideRequest,
		) (*types.EmergencyOverride, error) {
			if err := request.Authorize(ctx, types.AdmissionAuthorizationContext{
				OrganizationID:     request.OrganizationID,
				ActorUserAccountID: request.ActorUserAccountID,
				DeploymentPlanID:   request.DeploymentPlanID,
				EnvironmentID:      uuid.New(),
				Action:             "emergency.override",
				DecisionAt:         time.Now().UTC(),
			}); err != nil {
				return nil, err
			}
			persisted = true
			return &types.EmergencyOverride{}, nil
		},
	}
	handler := createEmergencyOverrideHandlerWithDependencies(dependencies)
	body := `{"accelerations":[{"gateKey":"maintenance-wait",` +
		`"maxAccelerationSeconds":300}],"reason":"critical customer recovery",` +
		`"approvalRequestIds":["` + approvalID.String() + `"],` +
		`"expiresAt":"2026-07-18T13:00:00Z","idempotencyKey":"incident:42"}`
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-plans/"+planID.String()+"/emergency-overrides",
		strings.NewReader(body),
	)
	request.SetPathValue("deploymentPlanId", planID.String())
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleAdmin
	request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	g.Expect(response.Code).To(Equal(http.StatusForbidden))
	g.Expect(persisted).To(BeFalse())
}

func TestAdmissionRoutesRequireBothControlPlaneFlags(t *testing.T) {
	g := NewWithT(t)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	for _, flags := range [][]featureflags.Key{
		nil,
		{featureflags.KeyOperatorControlPlaneV2},
		{featureflags.KeyExecutorProtocolV2},
	} {
		called = false
		handler := admissionMutationAccessMiddlewareWithFlags(flags)(next)
		response := httptest.NewRecorder()
		handler.ServeHTTP(
			response,
			httptest.NewRequest(http.MethodPost, "/admission", nil),
		)
		g.Expect(response.Code).To(Equal(http.StatusNotFound))
		g.Expect(called).To(BeFalse())
	}

	handler := admissionMutationAccessMiddlewareWithFlags([]featureflags.Key{
		featureflags.KeyOperatorControlPlaneV2,
		featureflags.KeyExecutorProtocolV2,
	})(next)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/admission", nil))
	g.Expect(response.Code).To(Equal(http.StatusNoContent))
	g.Expect(called).To(BeTrue())
}

func TestReviewMaterialReadReturnsCurrentDecisionState(t *testing.T) {
	g := NewWithT(t)
	planID := uuid.New()
	material := &types.ReviewAdmissionMaterial{
		DeploymentPlanID:       planID,
		PlanRevision:           1,
		PlanChecksum:           "sha256:" + strings.Repeat("a", 64),
		ObservedStateChecksum:  "sha256:" + strings.Repeat("b", 64),
		ReviewMaterialChecksum: "sha256:" + strings.Repeat("c", 64),
		ReviewMaterialValid:    true,
		AdmissionValid:         true,
		State:                  types.ReviewAdmissionMaterialStateNoGo,
		CanDecide:              true,
		Blockers:               []string{},
	}
	dependencies := admissionHandlerDependencies{
		getReviewMaterial: func(
			_ context.Context,
			organizationID, requestedPlanID uuid.UUID,
		) (*types.ReviewAdmissionMaterial, error) {
			g.Expect(organizationID).NotTo(Equal(uuid.Nil))
			g.Expect(requestedPlanID).To(Equal(planID))
			return material, nil
		},
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-plans/"+planID.String()+"/review-material",
		nil,
	)
	request.SetPathValue("deploymentPlanId", planID.String())
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleAdmin
	request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
	response := httptest.NewRecorder()

	getReviewAdmissionMaterialHandlerWithDependencies(dependencies).
		ServeHTTP(response, request)

	g.Expect(response.Code).To(Equal(http.StatusOK))
	var decoded types.ReviewAdmissionMaterial
	g.Expect(json.Unmarshal(response.Body.Bytes(), &decoded)).To(Succeed())
	g.Expect(decoded.State).To(Equal(types.ReviewAdmissionMaterialStateNoGo))
	g.Expect(decoded.CanDecide).To(BeTrue())
}

func TestReviewDecisionPostPreservesChecksumBoundMaterial(t *testing.T) {
	g := NewWithT(t)
	planID := uuid.New()
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	planChecksum := "sha256:" + strings.Repeat("a", 64)
	observedChecksum := "sha256:" + strings.Repeat("b", 64)
	materialChecksum := "sha256:" + strings.Repeat("c", 64)
	dependencies := admissionHandlerDependencies{
		clock: func() time.Time { return now },
		createReviewDecision: func(
			_ context.Context,
			request types.CreateReviewAdmissionDecisionRequest,
		) (*types.ReviewAdmissionDecisionRecord, error) {
			g.Expect(request.DeploymentPlanID).To(Equal(planID))
			g.Expect(request.ExpectedPlanChecksum).To(Equal(planChecksum))
			g.Expect(request.ObservedStateChecksum).To(Equal(observedChecksum))
			g.Expect(request.ReviewMaterialChecksum).To(Equal(materialChecksum))
			g.Expect(request.Decision).To(Equal(types.ReviewAdmissionDecisionGo))
			return &types.ReviewAdmissionDecisionRecord{
				ID: uuid.New(), DeploymentPlanID: planID,
				Decision: types.ReviewAdmissionDecisionGo,
			}, nil
		},
	}
	body := `{"expectedPlanChecksum":"` + planChecksum +
		`","reviewMaterialChecksum":"` + materialChecksum +
		`","observedStateChecksum":"` + observedChecksum +
		`","decision":"GO","reason":"Reviewed exact material",` +
		`"expiresAt":"2026-09-01T13:00:00Z","idempotencyKey":"review-ui-1"}`
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-plans/"+planID.String()+"/review-decisions",
		strings.NewReader(body),
	)
	request.SetPathValue("deploymentPlanId", planID.String())
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleAdmin
	request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
	response := httptest.NewRecorder()

	createReviewAdmissionDecisionHandlerWithDependencies(dependencies).
		ServeHTTP(response, request)

	g.Expect(response.Code).To(Equal(http.StatusOK))
}
