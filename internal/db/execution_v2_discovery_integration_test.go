package db_test

import (
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestLeaseExecutionV2AttemptFencesExpiredPendingAttemptAndReleasesLocks(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "expired-executor-v2")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(HaveLen(1))
	running, err := db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         tasks[0].ID,
		Status:         types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(running.Locks).NotTo(BeEmpty())

	attemptID := uuid.New()
	executionID := uuid.New()
	now := time.Now().UTC()
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ExecutionAttempt (
			id, organization_id, deployment_target_id, task_id, step_run_id,
			execution_id, attempt_number, step_key, status, plan_checksum,
			artifact_digest, config_checksum, adapter_revision,
			intent_issued_at, intent_expires_at
		) VALUES (
			@id, @organizationId, @deploymentTargetId, @taskId, @stepRunId,
			@executionId, 1, 'deploy', 'PENDING', @planChecksum,
			@artifactDigest, @configChecksum, @adapterRevision,
			@intentIssuedAt, @intentExpiresAt
		)`, pgx.NamedArgs{
		"id": attemptID, "organizationId": deps.orgID,
		"deploymentTargetId": deps.plan.Targets[0].DeploymentTargetID,
		"taskId":             tasks[0].ID, "stepRunId": tasks[0].StepRuns[0].ID,
		"executionId": executionID, "planChecksum": "sha256:" + executionV2RepeatHex("11"),
		"artifactDigest":  "sha256:" + executionV2RepeatHex("22"),
		"configChecksum":  "sha256:" + executionV2RepeatHex("33"),
		"adapterRevision": "adapter.compose@2", "intentIssuedAt": now.Add(-2 * time.Hour),
		"intentExpiresAt": now.Add(-time.Hour),
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ExecutionFence (
			execution_attempt_id, organization_id, resource_key, generation
		) VALUES (@id, @organizationId, @resourceKey, 1)`, pgx.NamedArgs{
		"id": attemptID, "organizationId": deps.orgID,
		"resourceKey": "target:" + tasks[0].DeploymentTargetID.String(),
	})
	g.Expect(err).NotTo(HaveOccurred())

	lease, err := db.LeaseExecutionV2Attempt(ctx, types.LeaseExecutionV2Request{
		OrganizationID: deps.orgID, DeploymentTargetID: tasks[0].DeploymentTargetID,
		ExecutorID: "executor-a", AdapterRevision: "adapter.compose@2",
		KeyID: "sha256:" + executionV2RepeatHex("ab"), Now: now, LeaseDuration: time.Minute,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).To(BeNil())

	var status types.ExecutionAttemptStatus
	var reason string
	var completedAt, fenceReleasedAt *time.Time
	var generation int64
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT ea.status, ea.failure_reason, ea.completed_at, ef.released_at, ef.generation
		FROM ExecutionAttempt ea
		JOIN ExecutionFence ef ON ef.execution_attempt_id = ea.id
		WHERE ea.id = @attemptId AND ea.organization_id = @organizationId`, pgx.NamedArgs{
		"attemptId": attemptID, "organizationId": deps.orgID,
	}).Scan(&status, &reason, &completedAt, &fenceReleasedAt, &generation)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(types.ExecutionAttemptStatusFenced))
	g.Expect(reason).To(ContainSubstring("expired before lease discovery"))
	g.Expect(completedAt).NotTo(BeNil())
	g.Expect(fenceReleasedAt).NotTo(BeNil())
	g.Expect(generation).To(Equal(int64(2)))

	var activeTaskLocks int
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*)
		FROM TaskResourceLock
		WHERE task_id = @taskId AND organization_id = @organizationId
			AND acquired_at IS NOT NULL AND released_at IS NULL`, pgx.NamedArgs{
		"taskId": tasks[0].ID, "organizationId": deps.orgID,
	}).Scan(&activeTaskLocks)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(activeTaskLocks).To(Equal(0))
}

func executionV2RepeatHex(pair string) string {
	result := ""
	for len(result) < 64 {
		result += pair
	}
	return result[:64]
}
