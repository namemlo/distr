package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestTaskStepEventHandlersReturnTimelineAndLogs(t *testing.T) {
	ctx := taskHandlerDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyAgentTaskLeaseHandlerPlan(t, ctx, "cluster-a")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
		StepRunID:      tasks[0].StepRuns[0].ID,
		LeaseToken:     lease.LeaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Logs: []types.RecordStepRunLogChunkRequest{
			{Stream: types.StepRunLogStreamStdout, Severity: types.StepRunLogSeverityInfo, Body: "started"},
		},
	})
	g.Expect(err).NotTo(HaveOccurred())

	timelineRecorder := httptest.NewRecorder()
	timelineRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+tasks[0].ID.String()+"/timeline", nil)
	timelineRequest.SetPathValue("taskId", tasks[0].ID.String())
	timelineRequest = timelineRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	getTaskTimelineHandler().ServeHTTP(timelineRecorder, timelineRequest)

	g.Expect(timelineRecorder.Code).To(Equal(http.StatusOK))
	var timeline api.TaskTimeline
	g.Expect(json.Unmarshal(timelineRecorder.Body.Bytes(), &timeline)).To(Succeed())
	g.Expect(timeline.TaskID).To(Equal(tasks[0].ID))
	g.Expect(timeline.Events).To(HaveLen(1))

	logsRecorder := httptest.NewRecorder()
	logsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+tasks[0].ID.String()+"/logs", nil)
	logsRequest.SetPathValue("taskId", tasks[0].ID.String())
	logsRequest = logsRequest.WithContext(authenticatedReleaseBundleHandlerContext(ctx, deps.orgID, deps.actorID))

	getTaskLogsHandler().ServeHTTP(logsRecorder, logsRequest)

	g.Expect(logsRecorder.Code).To(Equal(http.StatusOK))
	var logs []api.StepRunLogChunk
	g.Expect(json.Unmarshal(logsRecorder.Body.Bytes(), &logs)).To(Succeed())
	g.Expect(logs).To(HaveLen(1))
	g.Expect(logs[0].Body).To(Equal("started"))
}
