package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestRetentionPolicyFromCreateRequest(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()

	policy := retentionPolicyFromCreateRequest(orgID, api.CreateUpdateRetentionPolicyRequest{
		Name:                              "  Default policy ",
		Description:                       " preview first ",
		KeepLastSuccessfulReleases:        3,
		FailedTaskRetentionDays:           14,
		ProductionFailedTaskRetentionDays: 90,
		StepLogRetentionDays:              7,
		ProtectCurrentlyDeployedReleases:  true,
		ProtectRetentionProtectedReleases: true,
		MinimumAuditRetentionDays:         365,
	})

	g.Expect(policy).To(Equal(types.CreateRetentionPolicyRequest{
		OrganizationID:                    orgID,
		Name:                              "Default policy",
		Description:                       "preview first",
		KeepLastSuccessfulReleases:        3,
		FailedTaskRetentionDays:           14,
		ProductionFailedTaskRetentionDays: 90,
		StepLogRetentionDays:              7,
		ProtectCurrentlyDeployedReleases:  true,
		ProtectRetentionProtectedReleases: true,
		MinimumAuditRetentionDays:         365,
	}))
}

func TestRetentionPolicyHandlersRejectMalformedUUIDPathValues(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		body    string
	}{
		{name: "get", handler: getRetentionPolicyHandler(), method: http.MethodGet},
		{name: "preview", handler: previewRetentionCleanupHandler(), method: http.MethodPost},
		{name: "cleanup job", handler: createRetentionCleanupJobHandler(), method: http.MethodPost, body: `{"dryRun":true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, "/api/v1/retention-policies/not-a-uuid", strings.NewReader(tt.body))
			request.SetPathValue("retentionPolicyId", "not-a-uuid")
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testReleaseBundleAuth()))

			tt.handler.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
		})
	}
}

func TestRetentionPoliciesFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := retentionPoliciesFeatureFlagMiddleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/retention-policies", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}
