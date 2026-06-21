package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestPostAgentCapabilitiesHandlerSavesCurrentAgentReport(t *testing.T) {
	ctx := agentCapabilityHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID := createAgentCapabilityHandlerOrganization(t, ctx)
	targetID := createVariableSnapshotHandlerDockerTarget(t, ctx, orgID, "cluster-a")
	target, err := db.GetDeploymentTarget(ctx, targetID, &orgID, nil)
	g.Expect(err).NotTo(HaveOccurred())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agents/"+targetID.String()+"/capabilities",
		strings.NewReader(agentCapabilitiesRequestBody("distr.http.check")),
	)
	request.SetPathValue("agentId", targetID.String())
	request = request.WithContext(agentCapabilitiesHandlerContext(ctx, target))

	postAgentCapabilitiesHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.AgentCapabilities
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.DeploymentTargetID).To(Equal(targetID))
	g.Expect(response.ProtocolVersion).To(Equal(types.AgentCapabilityProtocolV1))
	g.Expect(response.SupportedActions).To(HaveLen(1))
	g.Expect(response.SupportedActions[0].ActionType).To(Equal("distr.http.check"))

	saved, err := db.GetAgentCapabilityReportForDeploymentTarget(ctx, targetID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(saved.SupportedActions).To(HaveLen(1))
	g.Expect(saved.SupportedActions[0].ActionType).To(Equal("distr.http.check"))
	g.Expect(saved.SupportedActions[0].Versions).To(Equal([]string{types.AgentActionVersionV1}))
}

func TestPostAgentCapabilitiesHandlerRejectsPathMismatch(t *testing.T) {
	ctx := agentCapabilityHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID := createAgentCapabilityHandlerOrganization(t, ctx)
	targetID := createVariableSnapshotHandlerDockerTarget(t, ctx, orgID, "cluster-a")
	target, err := db.GetDeploymentTarget(ctx, targetID, &orgID, nil)
	g.Expect(err).NotTo(HaveOccurred())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agents/"+uuid.NewString()+"/capabilities",
		strings.NewReader(agentCapabilitiesRequestBody("distr.http.check")),
	)
	request.SetPathValue("agentId", uuid.NewString())
	request = request.WithContext(agentCapabilitiesHandlerContext(ctx, target))

	postAgentCapabilitiesHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func TestPostAgentCapabilitiesHandlerRejectsInvalidPayload(t *testing.T) {
	ctx := agentCapabilityHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID := createAgentCapabilityHandlerOrganization(t, ctx)
	targetID := createVariableSnapshotHandlerDockerTarget(t, ctx, orgID, "cluster-a")
	target, err := db.GetDeploymentTarget(ctx, targetID, &orgID, nil)
	g.Expect(err).NotTo(HaveOccurred())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agents/"+targetID.String()+"/capabilities",
		strings.NewReader(agentCapabilitiesRequestBody("shell")),
	)
	request.SetPathValue("agentId", targetID.String())
	request = request.WithContext(agentCapabilitiesHandlerContext(ctx, target))

	postAgentCapabilitiesHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
}

func TestAgentCapabilitiesFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyAgentCapabilities)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/id/capabilities", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}

func agentCapabilityHandlerDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return channelHandlerDBTestContext(t)
}

func createAgentCapabilityHandlerOrganization(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	orgID, applicationID, lifecycleID, environmentID := createReleaseBundleHandlerDependencies(t, ctx)
	_ = applicationID
	_ = lifecycleID
	_ = environmentID
	return orgID
}

func agentCapabilitiesHandlerContext(
	ctx context.Context,
	target *types.DeploymentTargetFull,
) context.Context {
	ctx = internalctx.WithLogger(ctx, zap.NewNop())
	return internalctx.WithDeploymentTarget(ctx, target)
}

func agentCapabilitiesRequestBody(actionType string) string {
	return `{
		"protocolVersion":"v1",
		"agentVersion":"1.2.3",
		"supportedRuntimes":["docker"],
		"supportedActions":[{"actionType":"` + actionType + `","versions":["1"]}],
		"operatingSystem":"linux",
		"architecture":"amd64",
		"availableTooling":["docker"],
		"strategyCapabilities":["rolling"]
	}`
}
