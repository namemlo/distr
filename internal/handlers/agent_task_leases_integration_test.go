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
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestPostAgentLeaseHandlerClaimsTaskForCurrentAgent(t *testing.T) {
	ctx := agentTaskLeaseHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyAgentTaskLeaseHandlerPlan(t, ctx, "cluster-a")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	target, err := db.GetDeploymentTarget(ctx, tasks[0].DeploymentTargetID, &deps.orgID, nil)
	g.Expect(err).NotTo(HaveOccurred())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+target.ID.String()+"/lease", nil)
	request.SetPathValue("agentId", target.ID.String())
	request = request.WithContext(agentTaskLeaseHandlerContext(ctx, target))

	postAgentLeaseHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.AgentTaskLease
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.TaskID).To(Equal(tasks[0].ID))
	g.Expect(response.LeaseToken).NotTo(BeEmpty())
	g.Expect(response.Steps).To(HaveLen(1))
}

func TestPostAgentLeaseHandlerReturnsNoContentWhenNoTaskIsQueued(t *testing.T) {
	ctx := agentTaskLeaseHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID := createAgentCapabilityHandlerOrganization(t, ctx)
	targetID := createVariableSnapshotHandlerDockerTarget(t, ctx, orgID, "cluster-a")
	target, err := db.GetDeploymentTarget(ctx, targetID, &orgID, nil)
	g.Expect(err).NotTo(HaveOccurred())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+target.ID.String()+"/lease", nil)
	request.SetPathValue("agentId", target.ID.String())
	request = request.WithContext(agentTaskLeaseHandlerContext(ctx, target))

	postAgentLeaseHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNoContent))
}

func TestPostAgentLeaseHandlerRejectsPathMismatch(t *testing.T) {
	ctx := agentTaskLeaseHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID := createAgentCapabilityHandlerOrganization(t, ctx)
	targetID := createVariableSnapshotHandlerDockerTarget(t, ctx, orgID, "cluster-a")
	target, err := db.GetDeploymentTarget(ctx, targetID, &orgID, nil)
	g.Expect(err).NotTo(HaveOccurred())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+uuid.NewString()+"/lease", nil)
	request.SetPathValue("agentId", uuid.NewString())
	request = request.WithContext(agentTaskLeaseHandlerContext(ctx, target))

	postAgentLeaseHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func TestPostAgentTaskHeartbeatHandlerExtendsLease(t *testing.T) {
	ctx := agentTaskLeaseHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyAgentTaskLeaseHandlerPlan(t, ctx, "cluster-a")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	target, err := db.GetDeploymentTarget(ctx, tasks[0].DeploymentTargetID, &deps.orgID, nil)
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        target.ID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agents/"+target.ID.String()+"/tasks/"+tasks[0].ID.String()+"/heartbeat",
		strings.NewReader(`{"leaseToken":"`+lease.LeaseToken+`"}`),
	)
	request.SetPathValue("agentId", target.ID.String())
	request.SetPathValue("taskId", tasks[0].ID.String())
	request = request.WithContext(agentTaskLeaseHandlerContext(ctx, target))

	postAgentTaskHeartbeatHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.AgentTaskLease
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.ID).To(Equal(lease.ID))
	g.Expect(response.HeartbeatAt).To(BeTemporally(">", lease.HeartbeatAt))
}

func TestPostAgentTaskHeartbeatHandlerRejectsInvalidPayload(t *testing.T) {
	ctx := agentTaskLeaseHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID := createAgentCapabilityHandlerOrganization(t, ctx)
	targetID := createVariableSnapshotHandlerDockerTarget(t, ctx, orgID, "cluster-a")
	target, err := db.GetDeploymentTarget(ctx, targetID, &orgID, nil)
	g.Expect(err).NotTo(HaveOccurred())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agents/"+target.ID.String()+"/tasks/"+uuid.NewString()+"/heartbeat",
		strings.NewReader(`{"leaseToken":" "}`),
	)
	request.SetPathValue("agentId", target.ID.String())
	request.SetPathValue("taskId", uuid.NewString())
	request = request.WithContext(agentTaskLeaseHandlerContext(ctx, target))

	postAgentTaskHeartbeatHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
}

func TestAgentTaskLeasesFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := agentTaskLeaseFeatureFlagMiddleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/id/lease", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}

func TestPostAgentTaskHeartbeatHandlerRejectsMalformedUUIDs(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/not-a-uuid/tasks/not-a-uuid/heartbeat", nil)
	request.SetPathValue("agentId", "not-a-uuid")
	request.SetPathValue("taskId", "not-a-uuid")

	postAgentTaskHeartbeatHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func agentTaskLeaseHandlerDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return taskHandlerDBTestContext(t)
}

func createReadyAgentTaskLeaseHandlerPlan(t *testing.T, ctx context.Context, targetName string) taskHandlerPlanDeps {
	t.Helper()
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityHandlerDependencies(t, ctx)
	_, revision := createReleaseBundleHandlerProcessRevisionWithExecutionLocation(
		t,
		ctx,
		deps.orgID,
		deps.applicationID,
		"target",
	)
	targetID := createVariableSnapshotHandlerDockerTarget(t, ctx, deps.orgID, targetName)
	actorID := createReleaseBundleHandlerUser(t, ctx, deps.orgID)
	bundle := types.ReleaseBundle{
		OrganizationID:              deps.orgID,
		ApplicationID:               deps.applicationID,
		ChannelID:                   deps.channelID,
		DeploymentProcessRevisionID: &revision.ID,
		ReleaseNumber:               "2026.06.21",
		ReleaseNotes:                "Initial release",
		SourceRevision:              "abc123",
		Components: []types.ReleaseBundleComponent{
			{
				Key:                  "api",
				Name:                 "API",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &deps.versionID,
			},
		},
	}
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Valid).To(BeTrue())
	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusReady))
	return taskHandlerPlanDeps{orgID: deps.orgID, actorID: actorID, plan: plan}
}

func createReleaseBundleHandlerProcessRevisionWithExecutionLocation(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	applicationID uuid.UUID,
	executionLocation string,
) (types.DeploymentProcess, types.DeploymentProcessRevision) {
	t.Helper()
	process := types.DeploymentProcess{
		OrganizationID: orgID,
		ApplicationID:  applicationID,
		Name:           "Standard deploy " + uuid.NewString(),
	}
	if err := db.CreateDeploymentProcess(ctx, &process); err != nil {
		t.Fatalf("create deployment process: %v", err)
	}
	revision := types.DeploymentProcessRevision{
		OrganizationID:      orgID,
		DeploymentProcessID: process.ID,
		Description:         "Initial revision",
		Steps: []types.DeploymentProcessStep{
			{
				Key:               "deploy",
				Name:              "Deploy",
				ActionType:        "distr.http.check",
				ExecutionLocation: executionLocation,
				InputBindings:     map[string]any{"url": "https://example.com/health"},
				FailureMode:       "fail",
				SortOrder:         10,
			},
		},
	}
	if err := db.CreateDeploymentProcessRevision(ctx, &revision); err != nil {
		t.Fatalf("create deployment process revision: %v", err)
	}
	return process, revision
}

func agentTaskLeaseHandlerContext(
	ctx context.Context,
	target *types.DeploymentTargetFull,
) context.Context {
	ctx = internalctx.WithLogger(ctx, zap.NewNop())
	return internalctx.WithDeploymentTarget(ctx, target)
}
