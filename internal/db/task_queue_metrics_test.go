package db_test

import (
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/observability/metrics"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestTaskQueueRepositoryRecordsTaskExecutionMetrics(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	recorder := &recordingTaskMetrics{}
	ctx = internalctx.WithObservabilityMetricsRecorder(ctx, recorder)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")

	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         tasks[0].ID,
		Status:         types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	time.Sleep(10 * time.Millisecond)

	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         tasks[0].ID,
		Status:         types.TaskStatusSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(recorder.tasks).To(HaveLen(2))
	g.Expect(recorder.tasks[0].Status).To(Equal("running"))
	g.Expect(recorder.tasks[0].Duration).To(Equal(time.Duration(0)))
	g.Expect(recorder.tasks[1].Status).To(Equal("succeeded"))
	g.Expect(recorder.tasks[1].Duration).To(BeNumerically(">", 0))
}

type recordingTaskMetrics struct {
	tasks []metrics.TaskObservation
}

func (r *recordingTaskMetrics) ObserveHTTPRequest(metrics.HTTPObservation) {}

func (r *recordingTaskMetrics) ObserveTask(observation metrics.TaskObservation) {
	r.tasks = append(r.tasks, observation)
}

var _ metrics.Recorder = (*recordingTaskMetrics)(nil)
