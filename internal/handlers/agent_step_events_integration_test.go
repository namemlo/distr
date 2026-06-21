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
