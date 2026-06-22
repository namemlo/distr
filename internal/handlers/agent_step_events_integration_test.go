package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestPostAgentStepRunEventHandlerRecordsEvent(t *testing.T) {
	ctx := agentTaskLeaseHandlerDBTestContext(t)
	g := NewWithT(t)
	fixture := createAgentStepEventHandlerFixture(t, ctx)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agents/"+fixture.target.ID.String()+"/step-runs/"+fixture.stepRunID.String()+"/events",
		strings.NewReader(`{
			"leaseToken": "`+fixture.leaseToken+`",
			"sequence": 1,
			"type": "STARTED",
			"message": "Authorization: Bearer abc123",
			"logs": [{"stream": "stdout", "severity": "info", "body": "token=secret"}]
		}`),
	)
	request.SetPathValue("agentId", fixture.target.ID.String())
	request.SetPathValue("stepRunId", fixture.stepRunID.String())
	request = request.WithContext(agentTaskLeaseHandlerContext(ctx, fixture.target))

	postAgentStepRunEventHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.StepRunEvent
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.StepRunID).To(Equal(fixture.stepRunID))
	g.Expect(response.Message).To(Equal("Authorization: Bearer [REDACTED]"))
	g.Expect(response.Logs).To(HaveLen(1))
	g.Expect(response.Logs[0].Body).To(Equal("token=[REDACTED]"))
}

func TestAgentStepRunEventRedactsFloatNumericWebhookSecret(t *testing.T) {
	ctx := agentTaskLeaseHandlerDBTestContext(t)
	g := NewWithT(t)
	fixture := createWebhookAgentStepEventHandlerFixture(t, ctx, "123")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agents/"+fixture.target.ID.String()+"/step-runs/"+fixture.stepRunID.String()+"/events",
		strings.NewReader(`{
			"leaseToken": "`+fixture.leaseToken+`",
			"sequence": 1,
			"type": "STARTED",
			"outputs": [{"name": "numericSecret", "value": 123}]
		}`),
	)
	request.SetPathValue("agentId", fixture.target.ID.String())
	request.SetPathValue("stepRunId", fixture.stepRunID.String())
	request = request.WithContext(agentTaskLeaseHandlerContext(ctx, fixture.target))

	postAgentStepRunEventHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.StepRunEvent
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.Redacted).To(BeTrue())
	g.Expect(response.Outputs).To(HaveLen(1))
	g.Expect(response.Outputs[0].Name).To(Equal("numericSecret"))
	g.Expect(response.Outputs[0].Redacted).To(BeTrue())
	g.Expect(string(response.Outputs[0].Value)).To(Equal(`"[REDACTED]"`))
}

func TestPostAgentStepRunEventHandlerRejectsInvalidPayload(t *testing.T) {
	ctx := agentTaskLeaseHandlerDBTestContext(t)
	g := NewWithT(t)
	fixture := createAgentStepEventHandlerFixture(t, ctx)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agents/"+fixture.target.ID.String()+"/step-runs/"+fixture.stepRunID.String()+"/events",
		strings.NewReader(`{"leaseToken": " ", "sequence": 1, "type": "STARTED"}`),
	)
	request.SetPathValue("agentId", fixture.target.ID.String())
	request.SetPathValue("stepRunId", fixture.stepRunID.String())
	request = request.WithContext(agentTaskLeaseHandlerContext(ctx, fixture.target))

	postAgentStepRunEventHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
}

func TestPostAgentStepRunEventHandlerRejectsMalformedUUIDs(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/not-a-uuid/step-runs/not-a-uuid/events", nil)
	request.SetPathValue("agentId", "not-a-uuid")
	request.SetPathValue("stepRunId", "not-a-uuid")

	postAgentStepRunEventHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func TestStepEventsFeatureFlagMiddlewareRejectsDisabledAgentAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := agentStepEventsFeatureFlagMiddleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agents/id/step-runs/id/events", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())

	flags := featureflags.NewRegistry(featureflags.AllKeys())
	g.Expect(flags.IsEnabled(featureflags.KeyStepEvents)).To(BeTrue())
}

type agentStepEventHandlerFixture struct {
	target     *types.DeploymentTargetFull
	stepRunID  uuid.UUID
	leaseToken string
}

func createAgentStepEventHandlerFixture(t *testing.T, ctx context.Context) agentStepEventHandlerFixture {
	t.Helper()
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
	return agentStepEventHandlerFixture{
		target:     target,
		stepRunID:  tasks[0].StepRuns[0].ID,
		leaseToken: lease.LeaseToken,
	}
}

func createWebhookAgentStepEventHandlerFixture(
	t *testing.T,
	ctx context.Context,
	headerSecret string,
) agentStepEventHandlerFixture {
	t.Helper()
	g := NewWithT(t)
	deps := createReadyAgentTaskLeaseHandlerPlanWithWebhookStep(t, ctx, "cluster-a")
	_, err := db.CreateSecret(ctx, deps.orgID, nil, deps.actorID, "webhook_auth_token", headerSecret)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.CreateSecret(ctx, deps.orgID, nil, deps.actorID, "webhook_signing_key", "signing-secret")
	g.Expect(err).NotTo(HaveOccurred())
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
	return agentStepEventHandlerFixture{
		target:     target,
		stepRunID:  tasks[0].StepRuns[0].ID,
		leaseToken: lease.LeaseToken,
	}
}

func createReadyAgentTaskLeaseHandlerPlanWithWebhookStep(
	t *testing.T,
	ctx context.Context,
	targetName string,
) taskHandlerPlanDeps {
	t.Helper()
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityHandlerDependencies(t, ctx)
	process := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           "Webhook deploy " + uuid.NewString(),
	}
	g.Expect(db.CreateDeploymentProcess(ctx, &process)).To(Succeed())
	revision := types.DeploymentProcessRevision{
		OrganizationID:      deps.orgID,
		DeploymentProcessID: process.ID,
		Description:         "Initial revision",
		Steps: []types.DeploymentProcessStep{
			{
				Key:               "webhook",
				Name:              "Notify webhook",
				ActionType:        "distr.webhook",
				ExecutionLocation: "target",
				InputBindings: map[string]any{
					"url":     "https://hooks.example.com/deployments",
					"method":  "POST",
					"headers": map[string]any{"X-Deployment": "demo"},
					"secretHeaders": map[string]any{
						"Authorization": "webhook_auth_token",
					},
					"body":                map[string]any{"deploymentId": "dep-123"},
					"signingSecret":       "webhook_signing_key",
					"timeoutSeconds":      30,
					"expectedStatusCodes": []any{200, 202},
					"outputs": []any{
						map[string]any{"name": "remoteId", "pointer": "/id", "type": "string"},
					},
				},
				FailureMode:          "fail",
				TimeoutSeconds:       120,
				RetryMaxAttempts:     3,
				RetryIntervalSeconds: 10,
				RequiredPermissions:  []string{"deploy:write"},
				SortOrder:            10,
			},
		},
	}
	g.Expect(db.CreateDeploymentProcessRevision(ctx, &revision)).To(Succeed())
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
