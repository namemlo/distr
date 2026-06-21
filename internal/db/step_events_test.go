package db_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestStepEventRepositoryRecordsEventLogsAndOutputs(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)

	event, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "Authorization: Bearer abc123",
		Details: map[string]any{
			"token":   "secret",
			"message": "password=plaintext",
		},
		Logs: []types.RecordStepRunLogChunkRequest{
			{
				Stream:   types.StepRunLogStreamStdout,
				Severity: types.StepRunLogSeverityInfo,
				Body:     "password=plaintext",
			},
		},
		Outputs: []types.RecordStepRunOutputRequest{
			{Name: "url", Value: "https://example.com"},
			{Name: "token", Value: "plain-token", Sensitive: true},
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(event.Sequence).To(Equal(int64(1)))
	g.Expect(event.Type).To(Equal(types.StepRunEventTypeStarted))
	g.Expect(event.Message).To(Equal("Authorization: Bearer [REDACTED]"))
	g.Expect(event.Redacted).To(BeTrue())
	g.Expect(event.Logs).To(HaveLen(1))
	g.Expect(event.Logs[0].Body).To(Equal("password=[REDACTED]"))
	g.Expect(event.Logs[0].Redacted).To(BeTrue())
	g.Expect(event.Outputs).To(HaveLen(2))
	g.Expect(event.Outputs).To(ContainElement(WithTransform(
		func(output types.StepRunOutput) string { return output.Name },
		Equal("url"),
	)))
	var tokenOutput *types.StepRunOutput
	for i := range event.Outputs {
		if event.Outputs[i].Name == "token" {
			tokenOutput = &event.Outputs[i]
			break
		}
	}
	g.Expect(tokenOutput).NotTo(BeNil())
	g.Expect(tokenOutput.Value).To(BeNil())
	g.Expect(tokenOutput.Redacted).To(BeTrue())

	task, err := db.GetTask(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(task.Status).To(Equal(types.TaskStatusRunning))
	g.Expect(task.StepRuns[0].Status).To(Equal(types.StepRunStatusRunning))
	g.Expect(task.StepRuns[0].StartedAt).NotTo(BeNil())

	timeline, err := db.GetTaskTimeline(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(timeline.TaskID).To(Equal(fixture.taskID))
	g.Expect(timeline.Events).To(HaveLen(1))
	g.Expect(timeline.Events[0].ID).To(Equal(event.ID))

	logs, err := db.GetTaskLogs(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(logs).To(HaveLen(1))
	g.Expect(logs[0].Body).To(Equal("password=[REDACTED]"))
}

func TestStepEventRepositoryReplaysSameSequenceIdempotently(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	request := types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
		Message:        "started",
	}

	first, err := db.RecordAgentStepRunEvent(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())
	second, err := db.RecordAgentStepRunEvent(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(second.ID).To(Equal(first.ID))
	g.Expect(countStepRunEventsForTest(t, ctx, fixture.stepRunID)).To(Equal(1))
}

func TestStepEventRepositoryRejectsOutOfOrderSequences(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)

	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       2,
		Type:           types.StepRunEventTypeStarted,
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestStepEventRepositoryPreservesOrganizationAndAgentIsolation(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	otherOrgID := createReleaseBundleTestOrganization(t, ctx)

	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: otherOrgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
	})
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	_, err = db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        uuid.New(),
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
	})
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestStepEventRepositoryRejectsExpiredLease(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	expireTaskLeaseForTest(t, ctx, fixture.leaseID)

	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestStepEventRepositoryCompletesStepRunAndTaskOnSucceededEvent(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	fixture := createStepEventFixture(t, ctx)
	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       1,
		Type:           types.StepRunEventTypeStarted,
	})
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: fixture.orgID,
		AgentID:        fixture.agentID,
		StepRunID:      fixture.stepRunID,
		LeaseToken:     fixture.leaseToken,
		Sequence:       2,
		Type:           types.StepRunEventTypeSucceeded,
	})

	g.Expect(err).NotTo(HaveOccurred())
	task, err := db.GetTask(ctx, fixture.taskID, fixture.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(task.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(task.CompletedAt).NotTo(BeNil())
	g.Expect(task.StepRuns[0].Status).To(Equal(types.StepRunStatusSucceeded))
	g.Expect(countActiveTaskLeasesForTest(t, ctx, fixture.taskID)).To(Equal(0))
}

func TestStepEventMigrationDefinesEventLogAndOutputTables(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "125_step_events.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := string(up)
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE StepRunEvent"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE StepRunLogChunk"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE StepRunOutput"))
	g.Expect(upSQL).To(ContainSubstring("UNIQUE (step_run_id, task_lease_id, sequence)"))
	g.Expect(upSQL).To(ContainSubstring("FOREIGN KEY (task_lease_id, task_id, agent_id, organization_id)"))
	g.Expect(upSQL).To(ContainSubstring("octet_length(body) <="))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "125_step_events.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	downSQL := string(down)
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS StepRunOutput"))
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS StepRunLogChunk"))
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS StepRunEvent"))
}

type stepEventFixture struct {
	orgID      uuid.UUID
	agentID    uuid.UUID
	taskID     uuid.UUID
	stepRunID  uuid.UUID
	leaseID    uuid.UUID
	leaseToken string
}

func createStepEventFixture(t *testing.T, ctx context.Context) stepEventFixture {
	t.Helper()
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
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
	return stepEventFixture{
		orgID:      deps.orgID,
		agentID:    tasks[0].DeploymentTargetID,
		taskID:     tasks[0].ID,
		stepRunID:  tasks[0].StepRuns[0].ID,
		leaseID:    lease.ID,
		leaseToken: lease.LeaseToken,
	}
}

func countStepRunEventsForTest(t *testing.T, ctx context.Context, stepRunID uuid.UUID) int {
	t.Helper()
	var count int
	err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT count(*) FROM StepRunEvent WHERE step_run_id = @stepRunId`,
		pgx.NamedArgs{"stepRunId": stepRunID},
	).Scan(&count)
	if err != nil {
		t.Fatalf("count step events: %v", err)
	}
	return count
}
