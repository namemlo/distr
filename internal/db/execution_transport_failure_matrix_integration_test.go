package db_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestExecutionV2EventCallbackFailureMatrix(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "event-callback-matrix")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID: deps.orgID, DeploymentPlanID: deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(HaveLen(1))
	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID, TaskID: tasks[0].ID, Status: types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())

	const executorID = "failure-matrix-executor"
	attemptID := insertClaimedProjectionAttempt(t, ctx, deps.orgID, tasks[0], executorID)
	callback := types.ExecutionEventInput{
		OrganizationID: deps.orgID, DeploymentTargetID: tasks[0].DeploymentTargetID,
		AttemptID: attemptID, ExecutorID: executorID,
		Identity: types.ExecutionIdentity{
			ExecutionID: tasks[0].ID, AttemptNumber: 1,
			StepKey: tasks[0].StepRuns[0].StepKey,
		},
		FenceGeneration: 1, EventSequence: 1,
		Status:          types.ExecutionEventStatusRunning,
		PayloadChecksum: "sha256:" + executionV2RepeatHex("ab"),
		Message:         "transaction deployment accepted",
		OccurredAt:      time.Now().UTC(),
	}

	invalid := callback
	invalid.EventSequence = 0
	_, err = db.RecordExecutionEvent(ctx, invalid)
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())

	outOfOrder := callback
	outOfOrder.EventSequence = 2
	_, err = db.RecordExecutionEvent(ctx, outOfOrder)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	first, err := db.RecordExecutionEvent(ctx, callback)
	g.Expect(err).NotTo(HaveOccurred())
	replay, err := db.RecordExecutionEvent(ctx, callback)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replay.ID).To(Equal(first.ID))

	conflict := callback
	conflict.PayloadChecksum = "sha256:" + executionV2RepeatHex("cd")
	_, err = db.RecordExecutionEvent(ctx, conflict)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	completed, err := db.CompleteExecutionAttempt(ctx, types.CompletionInput{
		OrganizationID: deps.orgID, DeploymentTargetID: tasks[0].DeploymentTargetID,
		AttemptID: attemptID, ExecutorID: executorID, FenceGeneration: 1,
		Status: types.ExecutionAttemptStatusFailed, CompletedAt: time.Now().UTC(),
		FailureReason: "executor reported a bounded failure",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(completed.Status).To(Equal(types.TaskStatusFailed))

	terminalReplay, err := db.RecordExecutionEvent(ctx, callback)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(terminalReplay.ID).To(Equal(first.ID))
	late := callback
	late.EventSequence = 2
	late.PayloadChecksum = "sha256:" + executionV2RepeatHex("ef")
	_, err = db.RecordExecutionEvent(ctx, late)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	g.Expect(executionEventCount(t, ctx, deps.orgID, attemptID)).To(Equal(1))
	g.Expect(activeTaskResourceLockCount(t, ctx, deps.orgID, tasks[0].ID)).To(Equal(0))
	g.Expect(executionFenceReleased(t, ctx, deps.orgID, attemptID)).To(BeTrue())
}

func TestExecutorSuccessObserverMismatchFailsClosedAndReleasesTerminalLocks(t *testing.T) {
	g := NewWithT(t)
	fixture := newDesiredObservedFixture(t)
	ctx := fixture.ctx
	input := fixture.pendingInput()
	pending, err := db.AdmitPendingDesiredRevision(ctx, input)
	g.Expect(err).NotTo(HaveOccurred())
	first, firstCredential := fixture.createObserver(t, fixture.componentID, "transport-first")
	second, secondCredential := fixture.createObserver(t, fixture.componentID, "transport-second")

	const executorID = "observer-mismatch-executor"
	targetID := prepareDesiredObservedAttemptForCompletion(
		t, ctx, fixture, executorID,
	)
	evidence := recordHealthyExecutionRuntimeEvidence(
		t, ctx, fixture.organizationID, targetID,
		fixture.executionAttemptID, executorID,
	)
	projected, err := db.CompleteExecutionAttempt(ctx, types.CompletionInput{
		OrganizationID: fixture.organizationID, DeploymentTargetID: targetID,
		AttemptID: fixture.executionAttemptID, ExecutorID: executorID,
		FenceGeneration: 1, Status: types.ExecutionAttemptStatusSucceeded,
		CompletedAt: time.Now().UTC(), RuntimeEvidenceID: evidence.ID,
		RuntimeEvidenceChecksum: evidence.CanonicalChecksum,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(projected).To(BeNil(), "executor success remains provisional until observation")

	_, err = db.IngestObservation(ctx, fixture.envelope(first, firstCredential, input, 1))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(readPendingStatus(t, ctx, pending.ID)).To(Equal(types.PendingDesiredStatusPending))

	mismatch := fixture.envelope(second, secondCredential, input, 1)
	mismatch.ArtifactDigest = desiredObservedTestDigest("wrong-runtime-after-success")
	_, err = db.IngestObservation(ctx, mismatch)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(readPendingStatus(t, ctx, pending.ID)).To(Equal(types.PendingDesiredStatusConflict))

	task, err := db.GetTask(ctx, fixture.executionID, fixture.organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(task.Status).To(Equal(types.TaskStatusFailed))
	g.Expect(task.StepRuns).To(HaveLen(1))
	g.Expect(task.StepRuns[0].Status).To(Equal(types.StepRunStatusFailed))
	g.Expect(countRowsForOrganization(
		t, ctx, "ActiveDesiredRevision", fixture.organizationID,
	)).To(Equal(int64(0)))
	g.Expect(activeTaskResourceLockCount(
		t, ctx, fixture.organizationID, fixture.executionID,
	)).To(Equal(0))
	g.Expect(executionFenceReleased(
		t, ctx, fixture.organizationID, fixture.executionAttemptID,
	)).To(BeTrue())
}

func TestConcurrentExecutorCompletionAndReconciliationProduceOneTerminalProjection(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "callback-reconciliation-race")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID: deps.orgID, DeploymentPlanID: deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(HaveLen(1))
	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID, TaskID: tasks[0].ID, Status: types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())

	const executorID = "callback-race-executor"
	attemptID := insertClaimedProjectionAttempt(t, ctx, deps.orgID, tasks[0], executorID)
	query, err := db.RequestExecutionStatus(ctx, types.StatusRequest{
		OrganizationID: deps.orgID, ExecutionID: tasks[0].ID,
		RequestedBy: deps.actorID, IdempotencyKey: "callback-loss-status",
		Reason: "executor response was lost", RequestedTTLSeconds: 60,
	})
	g.Expect(err).NotTo(HaveOccurred())
	payload := []byte(`{"outcome":"PROVEN_FAILED"}`)
	reconciliation := types.ReconciliationStatusInput{
		OrganizationID: deps.orgID, ExecutionID: tasks[0].ID, AttemptID: attemptID,
		StatusQueryID: query.ID, EventIdentity: uuid.New(),
		Outcome:          types.ReconciliationOutcomeProvenFailed,
		EvidenceChecksum: "sha256:" + executionV2RepeatHex("a1"),
		ObservedAt:       time.Now().UTC(),
		SignedEvidence: types.SignedReconciliationEvidence{
			Payload: payload, Checksum: fmt.Sprintf("sha256:%x", sha256.Sum256(payload)),
			KeyID: "sha256:" + executionV2RepeatHex("b2"), Signature: strings.Repeat("a", 80),
		},
	}
	completion := types.CompletionInput{
		OrganizationID: deps.orgID, DeploymentTargetID: tasks[0].DeploymentTargetID,
		AttemptID: attemptID, ExecutorID: executorID, FenceGeneration: 1,
		Status: types.ExecutionAttemptStatusFailed, CompletedAt: time.Now().UTC(),
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		_, completeErr := db.CompleteExecutionAttempt(ctx, completion)
		errs <- completeErr
	}()
	go func() {
		defer group.Done()
		<-start
		_, reconcileErr := db.ImportReconciliationStatusWithTask(ctx, reconciliation)
		errs <- reconcileErr
	}()
	close(start)
	group.Wait()
	close(errs)

	successes := 0
	for raceErr := range errs {
		if raceErr == nil {
			successes++
			continue
		}
		g.Expect(errors.Is(raceErr, apierrors.ErrConflict)).To(BeTrue())
	}
	g.Expect(successes).To(BeNumerically(">=", 1))

	replayed, err := db.CompleteExecutionAttempt(ctx, completion)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayed.Status).To(Equal(types.TaskStatusFailed))
	g.Expect(replayed.StepRuns[0].Status).To(Equal(types.StepRunStatusFailed))
	g.Expect(activeTaskResourceLockCount(t, ctx, deps.orgID, tasks[0].ID)).To(Equal(0))
	g.Expect(executionFenceReleased(t, ctx, deps.orgID, attemptID)).To(BeTrue())
	g.Expect(reconciliationEventCount(t, ctx, deps.orgID, attemptID)).To(
		BeNumerically("<=", 1),
	)
}

func prepareDesiredObservedAttemptForCompletion(
	t *testing.T,
	ctx context.Context,
	fixture desiredObservedFixture,
	executorID string,
) uuid.UUID {
	t.Helper()
	g := NewWithT(t)
	targetID := readExecutionTargetID(t, ctx, fixture.executionAttemptID)
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ExecutionAttempt
		SET status = 'CLAIMED', claimed_by = @executorID
		WHERE id = @attemptID AND organization_id = @organizationID`, pgx.NamedArgs{
		"executorID": executorID, "attemptID": fixture.executionAttemptID,
		"organizationID": fixture.organizationID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ExecutionFence
		SET lease_expires_at = clock_timestamp() + interval '5 minutes'
		WHERE execution_attempt_id = @attemptID
		  AND organization_id = @organizationID`, pgx.NamedArgs{
		"attemptID":      fixture.executionAttemptID,
		"organizationID": fixture.organizationID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO TaskResourceLock (
			organization_id, task_id, resource_type, resource_key,
			concurrency_policy, acquired_at
		) VALUES (
			@organizationID, @taskID, 'deployment_target', @resourceKey,
			'QUEUE', clock_timestamp()
		) ON CONFLICT (task_id, resource_type, resource_key) DO UPDATE
		  SET acquired_at = COALESCE(TaskResourceLock.acquired_at, EXCLUDED.acquired_at),
		      released_at = NULL,
		      updated_at = clock_timestamp()`, pgx.NamedArgs{
		"organizationID": fixture.organizationID, "taskID": fixture.executionID,
		"resourceKey": targetID.String(),
	})
	g.Expect(err).NotTo(HaveOccurred())
	return targetID
}

func executionEventCount(
	t *testing.T,
	ctx context.Context,
	organizationID, attemptID uuid.UUID,
) int {
	t.Helper()
	var count int
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*) FROM ExecutionEvent
		WHERE organization_id = @organizationID
		  AND execution_attempt_id = @attemptID`, pgx.NamedArgs{
		"organizationID": organizationID, "attemptID": attemptID,
	}).Scan(&count)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return count
}

func reconciliationEventCount(
	t *testing.T,
	ctx context.Context,
	organizationID, attemptID uuid.UUID,
) int {
	t.Helper()
	var count int
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*) FROM ExecutionReconciliationEvent
		WHERE organization_id = @organizationID
		  AND execution_attempt_id = @attemptID`, pgx.NamedArgs{
		"organizationID": organizationID, "attemptID": attemptID,
	}).Scan(&count)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return count
}

func activeTaskResourceLockCount(
	t *testing.T,
	ctx context.Context,
	organizationID, taskID uuid.UUID,
) int {
	t.Helper()
	var count int
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*) FROM TaskResourceLock
		WHERE organization_id = @organizationID
		  AND task_id = @taskID
		  AND acquired_at IS NOT NULL
		  AND released_at IS NULL`, pgx.NamedArgs{
		"organizationID": organizationID, "taskID": taskID,
	}).Scan(&count)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return count
}

func executionFenceReleased(
	t *testing.T,
	ctx context.Context,
	organizationID, attemptID uuid.UUID,
) bool {
	t.Helper()
	var released bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT released_at IS NOT NULL AND lease_expires_at IS NULL
		FROM ExecutionFence
		WHERE organization_id = @organizationID
		  AND execution_attempt_id = @attemptID`, pgx.NamedArgs{
		"organizationID": organizationID, "attemptID": attemptID,
	}).Scan(&released)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return released
}
