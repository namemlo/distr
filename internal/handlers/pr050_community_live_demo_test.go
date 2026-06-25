package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestPR050CommunityLiveReleaseToTaskDemo(t *testing.T) {
	ctx := taskHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyAgentTaskLeaseHandlerPlan(t, ctx, "pr050-community-demo")

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-plans/"+deps.plan.ID.String()+"/tasks",
		nil,
	)
	createRequest.SetPathValue("deploymentPlanId", deps.plan.ID.String())
	createRequest = createRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	createTasksForDeploymentPlanHandler().ServeHTTP(createRecorder, createRequest)

	g.Expect(createRecorder.Code).To(Equal(http.StatusOK))
	var created []api.Task
	g.Expect(json.Unmarshal(createRecorder.Body.Bytes(), &created)).To(Succeed())
	g.Expect(created).To(HaveLen(1))
	g.Expect(created[0].Status).To(Equal(types.TaskStatusQueued))
	g.Expect(created[0].ReleaseBundleID).To(Equal(deps.plan.ReleaseBundleID))
	g.Expect(created[0].StepRuns).To(HaveLen(1))
	g.Expect(created[0].StepRuns[0].ActionType).To(Equal("distr.http.check"))

	deploymentTimelineRecorder := httptest.NewRecorder()
	deploymentTimelineRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-timeline?applicationId="+created[0].ApplicationID.String(),
		nil,
	)
	deploymentTimelineRequest = deploymentTimelineRequest.WithContext(
		authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID),
	)

	getDeploymentTimelineHandler().ServeHTTP(deploymentTimelineRecorder, deploymentTimelineRequest)

	g.Expect(deploymentTimelineRecorder.Code).To(Equal(http.StatusOK))
	var deploymentTimeline api.DeploymentTimeline
	g.Expect(json.Unmarshal(deploymentTimelineRecorder.Body.Bytes(), &deploymentTimeline)).To(Succeed())
	g.Expect(deploymentTimeline.Items).To(HaveLen(1))
	g.Expect(deploymentTimeline.Items[0].TaskID).NotTo(BeNil())
	g.Expect(*deploymentTimeline.Items[0].TaskID).To(Equal(created[0].ID))

	target, err := db.GetDeploymentTarget(ctx, created[0].DeploymentTargetID, &deps.orgID, nil)
	g.Expect(err).NotTo(HaveOccurred())
	leaseRecorder := httptest.NewRecorder()
	leaseRequest := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+target.ID.String()+"/lease", nil)
	leaseRequest.SetPathValue("agentId", target.ID.String())
	leaseRequest = leaseRequest.WithContext(agentTaskLeaseHandlerContext(ctx, target))

	postAgentLeaseHandler().ServeHTTP(leaseRecorder, leaseRequest)

	g.Expect(leaseRecorder.Code).To(Equal(http.StatusOK))
	var lease api.AgentTaskLease
	g.Expect(json.Unmarshal(leaseRecorder.Body.Bytes(), &lease)).To(Succeed())
	g.Expect(lease.TaskID).To(Equal(created[0].ID))
	g.Expect(lease.AgentID).To(Equal(target.ID))
	g.Expect(lease.Steps).To(HaveLen(1))
	g.Expect(lease.Steps[0].ActionType).To(Equal("distr.http.check"))
	g.Expect(lease.Steps[0].Inputs["url"]).To(Equal("https://example.com/health"))

	progressStarted := 0
	started := recordPR050CommunityStepEvent(t, ctx, target, lease.Steps[0].StepRunID, api.AgentStepRunEventRequest{
		LeaseToken:      lease.LeaseToken,
		Sequence:        1,
		Type:            types.StepRunEventTypeStarted,
		Message:         "HTTP check started",
		ProgressPercent: &progressStarted,
		Logs: []api.AgentStepRunLogChunkRequest{
			{Stream: types.StepRunLogStreamStdout, Severity: types.StepRunLogSeverityInfo, Body: "checking https://example.com/health"},
		},
	})
	g.Expect(started.Type).To(Equal(types.StepRunEventTypeStarted))

	progressComplete := 100
	succeeded := recordPR050CommunityStepEvent(t, ctx, target, lease.Steps[0].StepRunID, api.AgentStepRunEventRequest{
		LeaseToken:      lease.LeaseToken,
		Sequence:        2,
		Type:            types.StepRunEventTypeSucceeded,
		Message:         "HTTP check returned 200",
		ProgressPercent: &progressComplete,
		Logs: []api.AgentStepRunLogChunkRequest{
			{Stream: types.StepRunLogStreamStdout, Severity: types.StepRunLogSeverityInfo, Body: "status=200"},
		},
		Outputs: []api.AgentStepRunOutputRequest{
			{Name: "statusCode", Value: 200},
		},
	})
	g.Expect(succeeded.Type).To(Equal(types.StepRunEventTypeSucceeded))
	g.Expect(succeeded.Outputs).To(HaveLen(1))
	g.Expect(succeeded.Outputs[0].Name).To(Equal("statusCode"))

	taskRecorder := httptest.NewRecorder()
	taskRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+created[0].ID.String(), nil)
	taskRequest.SetPathValue("taskId", created[0].ID.String())
	taskRequest = taskRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	getTaskHandler().ServeHTTP(taskRecorder, taskRequest)

	g.Expect(taskRecorder.Code).To(Equal(http.StatusOK))
	var completed api.Task
	g.Expect(json.Unmarshal(taskRecorder.Body.Bytes(), &completed)).To(Succeed())
	g.Expect(completed.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(completed.StepRuns).To(HaveLen(1))
	g.Expect(completed.StepRuns[0].Status).To(Equal(types.StepRunStatusSucceeded))

	timelineRecorder := httptest.NewRecorder()
	timelineRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+created[0].ID.String()+"/timeline", nil)
	timelineRequest.SetPathValue("taskId", created[0].ID.String())
	timelineRequest = timelineRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	getTaskTimelineHandler().ServeHTTP(timelineRecorder, timelineRequest)

	g.Expect(timelineRecorder.Code).To(Equal(http.StatusOK))
	var timeline api.TaskTimeline
	g.Expect(json.Unmarshal(timelineRecorder.Body.Bytes(), &timeline)).To(Succeed())
	g.Expect(timeline.TaskID).To(Equal(created[0].ID))
	g.Expect(timeline.Events).To(HaveLen(2))
	g.Expect(timeline.Events[0].Type).To(Equal(types.StepRunEventTypeStarted))
	g.Expect(timeline.Events[1].Type).To(Equal(types.StepRunEventTypeSucceeded))

	logsRecorder := httptest.NewRecorder()
	logsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+created[0].ID.String()+"/logs", nil)
	logsRequest.SetPathValue("taskId", created[0].ID.String())
	logsRequest = logsRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	getTaskLogsHandler().ServeHTTP(logsRecorder, logsRequest)

	g.Expect(logsRecorder.Code).To(Equal(http.StatusOK))
	var logs []api.StepRunLogChunk
	g.Expect(json.Unmarshal(logsRecorder.Body.Bytes(), &logs)).To(Succeed())
	g.Expect(logs).To(HaveLen(2))
	g.Expect(logs[0].Body).To(Equal("checking https://example.com/health"))
	g.Expect(logs[1].Body).To(Equal("status=200"))
}

func recordPR050CommunityStepEvent(
	t *testing.T,
	ctx context.Context,
	target *types.DeploymentTargetFull,
	stepRunID uuid.UUID,
	eventRequest api.AgentStepRunEventRequest,
) api.StepRunEvent {
	t.Helper()
	g := NewWithT(t)
	payload, err := json.Marshal(eventRequest)
	g.Expect(err).NotTo(HaveOccurred())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agents/"+target.ID.String()+"/step-runs/"+stepRunID.String()+"/events",
		bytes.NewReader(payload),
	)
	request.SetPathValue("agentId", target.ID.String())
	request.SetPathValue("stepRunId", stepRunID.String())
	request = request.WithContext(agentTaskLeaseHandlerContext(ctx, target))

	postAgentStepRunEventHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var response api.StepRunEvent
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	return response
}
