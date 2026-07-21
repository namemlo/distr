package db_test

import (
	"context"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestExecutionV2AcknowledgeAndCompletionProjectTaskAndStepExactlyOnce(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "projection-success")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(HaveLen(1))
	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         tasks[0].ID,
		Status:         types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())

	attemptID := insertClaimedProjectionAttempt(t, ctx, deps.orgID, tasks[0], "executor-projection")
	ack := types.HeartbeatRequest{
		OrganizationID: deps.orgID, DeploymentTargetID: tasks[0].DeploymentTargetID,
		AttemptID: attemptID, ExecutorID: "executor-projection", FenceGeneration: 1,
	}
	g.Expect(db.AcknowledgeExecutionAttempt(ctx, ack)).To(Succeed())

	running, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(running.Status).To(Equal(types.TaskStatusRunning))
	g.Expect(running.StepRuns[0].Status).To(Equal(types.StepRunStatusRunning))

	completion := types.CompletionInput{
		OrganizationID: deps.orgID, DeploymentTargetID: tasks[0].DeploymentTargetID,
		AttemptID: attemptID, ExecutorID: "executor-projection", FenceGeneration: 1,
		Status: types.ExecutionAttemptStatusSucceeded, CompletedAt: time.Now().UTC(),
	}
	completed, err := db.CompleteExecutionAttempt(ctx, completion)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(completed.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(completed.StepRuns[0].Status).To(Equal(types.StepRunStatusSucceeded))

	replayed, err := db.CompleteExecutionAttempt(ctx, completion)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayed.Status).To(Equal(types.TaskStatusSucceeded))

	var activeLocks int
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*) FROM TaskResourceLock
		WHERE organization_id = @organizationId AND task_id = @taskId
		  AND acquired_at IS NOT NULL AND released_at IS NULL`, pgx.NamedArgs{
		"organizationId": deps.orgID, "taskId": tasks[0].ID,
	}).Scan(&activeLocks)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(activeLocks).To(Equal(0))
}

func TestExecutionV2ReadyStepsExcludeOnlyTheExactAttemptedStep(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "projection-ready-step")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	ready, err := db.GetExecutionV2ReadyStepRuns(ctx, deps.orgID, tasks[0].ID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ready).To(HaveLen(1))

	insertClaimedProjectionAttempt(t, ctx, deps.orgID, tasks[0], "executor-ready-step")
	ready, err = db.GetExecutionV2ReadyStepRuns(ctx, deps.orgID, tasks[0].ID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ready).To(BeEmpty())
}

func insertClaimedProjectionAttempt(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
	task types.Task,
	executorID string,
) uuid.UUID {
	t.Helper()
	g := NewWithT(t)
	attemptID := uuid.New()
	now := time.Now().UTC()
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ExecutionAttempt (
		  id, organization_id, deployment_target_id, task_id, step_run_id,
		  execution_id, attempt_number, step_key, status, claimed_by,
		  plan_checksum, artifact_digest, config_checksum, adapter_revision,
		  intent_issued_at, intent_expires_at, cancellable, retry_safe
		) VALUES (
		  @id, @organizationId, @deploymentTargetId, @taskId, @stepRunId,
		  @executionId, 1, @stepKey, 'CLAIMED', @executorId,
		  @planChecksum, @artifactDigest, @configChecksum, 'adapter.compose@2',
		  @issuedAt, @expiresAt, TRUE, TRUE
		)`, pgx.NamedArgs{
		"id": attemptID, "organizationId": organizationID,
		"deploymentTargetId": task.DeploymentTargetID,
		"taskId":             task.ID, "stepRunId": task.StepRuns[0].ID,
		"executionId": task.ID, "stepKey": task.StepRuns[0].StepKey,
		"executorId":     executorID,
		"planChecksum":   "sha256:" + executionV2RepeatHex("11"),
		"artifactDigest": "sha256:" + executionV2RepeatHex("22"),
		"configChecksum": "sha256:" + executionV2RepeatHex("33"),
		"issuedAt":       now.Add(-time.Minute), "expiresAt": now.Add(10 * time.Minute),
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ExecutionFence (
		  execution_attempt_id, organization_id, resource_key, generation,
		  lease_expires_at
		) VALUES (@id, @organizationId, @resourceKey, 1, @leaseExpiresAt)`, pgx.NamedArgs{
		"id": attemptID, "organizationId": organizationID,
		"resourceKey":    "target:" + task.DeploymentTargetID.String(),
		"leaseExpiresAt": now.Add(5 * time.Minute),
	})
	g.Expect(err).NotTo(HaveOccurred())
	return attemptID
}
