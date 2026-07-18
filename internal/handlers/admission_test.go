package handlers

import (
	"context"
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
				DecisionAt:         request.EvaluatedAt,
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
			`{"schedulerIdempotencyKey":"scheduler:1",`+
				`"evaluatedAt":"2026-07-18T12:00:00Z","gateEvidence":[]}`,
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
