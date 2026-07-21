package db_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestExecutionV2KnownReconciliationClearsOnlyResolvedCampaignUncertainty(t *testing.T) {
	ctx := executionV2ProjectionAuditTestContext(t, taskQueueDBTestContext(t))
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "reconcile-member-a")
	targetB := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "reconcile-member-b")
	planB := createReadyDeploymentPlanForTaskQueueWithTargets(
		t, ctx, deps, "Reconcile member B", targetB,
	)
	plans := []*types.DeploymentPlan{deps.plan, planB}
	tasks := make([]types.Task, 0, len(plans))
	attemptIDs := make([]uuid.UUID, 0, len(plans))
	for i, plan := range plans {
		created, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
			OrganizationID: deps.orgID, DeploymentPlanID: plan.ID,
			ActorUserAccountID: deps.actorID,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(created).To(HaveLen(1))
		tasks = append(tasks, created[0])
		attemptIDs = append(attemptIDs, insertClaimedProjectionAttempt(
			t, ctx, deps.orgID, created[0], fmt.Sprintf("reconcile-executor-%d", i),
		))
	}
	runID, memberIDs := insertExecutionV2ProjectionCampaign(t, ctx, deps, plans, tasks)

	for i := range attemptIDs {
		importExecutionV2Reconciliation(
			t, ctx, deps, tasks[i], attemptIDs[i],
			fmt.Sprintf("unknown-%d", i), types.ReconciliationOutcomeUnknown,
		)
	}
	assertExecutionV2CampaignFlags(t, ctx, runID, true, true)

	importExecutionV2Reconciliation(
		t, ctx, deps, tasks[0], attemptIDs[0], "known-a",
		types.ReconciliationOutcomeProvenSucceeded,
	)
	assertExecutionV2MemberUncertain(t, ctx, memberIDs[0], false)
	assertExecutionV2MemberUncertain(t, ctx, memberIDs[1], true)
	assertExecutionV2CampaignFlags(t, ctx, runID, true, true)

	importExecutionV2Reconciliation(
		t, ctx, deps, tasks[1], attemptIDs[1], "known-b",
		types.ReconciliationOutcomeProvenSucceeded,
	)
	assertExecutionV2MemberUncertain(t, ctx, memberIDs[1], false)
	assertExecutionV2CampaignFlags(t, ctx, runID, false, false)
	var state types.CampaignRunState
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT state FROM DeploymentCampaignRun WHERE id = @id`,
		pgx.NamedArgs{"id": runID},
	).Scan(&state)).To(Succeed())
	g.Expect(state).To(Equal(types.CampaignRunStateRunning), "reconciliation must not complete the campaign")
}

func TestExecutionV2KnownReconciliationPreservesCampaignGovernanceBlock(t *testing.T) {
	ctx := executionV2ProjectionAuditTestContext(t, taskQueueDBTestContext(t))
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "reconcile-paused")
	created, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID: deps.orgID, DeploymentPlanID: deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	attemptID := insertClaimedProjectionAttempt(t, ctx, deps.orgID, created[0], "reconcile-paused-executor")
	runID, memberIDs := insertExecutionV2ProjectionCampaign(
		t, ctx, deps, []*types.DeploymentPlan{deps.plan}, created,
	)
	importExecutionV2Reconciliation(
		t, ctx, deps, created[0], attemptID, "unknown-paused",
		types.ReconciliationOutcomeUnknown,
	)
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE DeploymentCampaignRun
		SET pause_requested = TRUE
		WHERE id = @id AND organization_id = @organizationId`, pgx.NamedArgs{
		"id": runID, "organizationId": deps.orgID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	importExecutionV2Reconciliation(
		t, ctx, deps, created[0], attemptID, "known-paused",
		types.ReconciliationOutcomeProvenFailed,
	)
	assertExecutionV2MemberUncertain(t, ctx, memberIDs[0], false)
	assertExecutionV2CampaignFlags(t, ctx, runID, false, true)
}

func TestExecutionV2AuditFailureRollsBackAcknowledgementAndProjection(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "projection-audit-rollback")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID, TaskID: tasks[0].ID, Status: types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	attemptID := insertClaimedProjectionAttempt(t, ctx, deps.orgID, tasks[0], "executor-audit-rollback")
	auditFailure := errors.New("control-plane audit unavailable")
	auditCtx := db.WithExecutionV2AuditHook(ctx, db.ControlPlaneAuditAppendHookFunc(
		func(context.Context, types.ControlPlaneAuditEventInput) error { return auditFailure },
	))

	err = db.AcknowledgeExecutionAttempt(auditCtx, types.HeartbeatRequest{
		OrganizationID: deps.orgID, DeploymentTargetID: tasks[0].DeploymentTargetID,
		AttemptID: attemptID, ExecutorID: "executor-audit-rollback", FenceGeneration: 1,
	})
	g.Expect(err).To(MatchError(auditFailure))
	rolledBack, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rolledBack.StepRuns[0].Status).To(Equal(types.StepRunStatusPending))
	var attemptStatus types.ExecutionAttemptStatus
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT status FROM ExecutionAttempt
		WHERE id = @attemptId AND organization_id = @organizationId`, pgx.NamedArgs{
		"attemptId": attemptID, "organizationId": deps.orgID,
	}).Scan(&attemptStatus)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(attemptStatus).To(Equal(types.ExecutionAttemptStatusClaimed))
}

func TestExecutionV2AcknowledgeAndCompletionProjectTaskAndStepExactlyOnce(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	var auditEvents []types.ControlPlaneAuditEventInput
	auditCtx := db.WithExecutionV2AuditHook(ctx, db.ControlPlaneAuditAppendHookFunc(
		func(_ context.Context, event types.ControlPlaneAuditEventInput) error {
			auditEvents = append(auditEvents, event)
			return nil
		},
	))
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
	g.Expect(db.AcknowledgeExecutionAttempt(auditCtx, ack)).To(Succeed())
	g.Expect(db.AcknowledgeExecutionAttempt(auditCtx, ack)).To(Succeed())
	g.Expect(auditEvents).To(HaveLen(1))
	g.Expect(auditEvents[0].EventType).To(Equal("execution.acknowledged"))

	running, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(running.Status).To(Equal(types.TaskStatusRunning))
	g.Expect(running.StepRuns[0].Status).To(Equal(types.StepRunStatusRunning))

	completion := types.CompletionInput{
		OrganizationID: deps.orgID, DeploymentTargetID: tasks[0].DeploymentTargetID,
		AttemptID: attemptID, ExecutorID: "executor-projection", FenceGeneration: 1,
		Status: types.ExecutionAttemptStatusSucceeded, CompletedAt: time.Now().UTC(),
	}
	completed, err := db.CompleteExecutionAttempt(auditCtx, completion)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(completed.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(completed.StepRuns[0].Status).To(Equal(types.StepRunStatusSucceeded))

	replayed, err := db.CompleteExecutionAttempt(auditCtx, completion)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayed.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(auditEvents).To(HaveLen(2), "exact acknowledgement and completion replay must not duplicate audit")
	g.Expect(auditEvents[1].EventType).To(Equal("execution.completed"))

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

func TestControlPlaneAuditAttemptReplayIsIdempotentAndRetriesRemainDistinct(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "attempt-audit-idempotency")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID: deps.orgID, DeploymentPlanID: deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	firstAttemptID := insertClaimedProjectionAttempt(t, ctx, deps.orgID, tasks[0], "executor-audit-1")
	secondAttemptID := insertProjectionRetryAttempt(t, ctx, deps.orgID, tasks[0], 2)
	executionID := tasks[0].ID

	inputFor := func(attemptID uuid.UUID) types.ControlPlaneAuditEventInput {
		return types.ControlPlaneAuditEventInput{
			OrganizationID: deps.orgID, EventType: "campaign.execution.terminal", Outcome: "FAILED",
			ExecutionID: &executionID, ExecutionAttemptID: &attemptID,
			TaskID: &tasks[0].ID, StepRunID: &tasks[0].StepRuns[0].ID,
		}
	}
	first, err := db.AppendControlPlaneAuditEvent(ctx, inputFor(firstAttemptID))
	g.Expect(err).NotTo(HaveOccurred())
	replay, err := db.AppendControlPlaneAuditEvent(ctx, inputFor(firstAttemptID))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replay.ID).To(Equal(first.ID))

	second, err := db.AppendControlPlaneAuditEvent(ctx, inputFor(secondAttemptID))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second.ID).NotTo(Equal(first.ID))

	reconciledInput := inputFor(firstAttemptID)
	reconciledInput.EventType = "campaign.execution.reconciled"
	reconciledFirst, err := db.AppendControlPlaneAuditEvent(ctx, reconciledInput)
	g.Expect(err).NotTo(HaveOccurred())
	reconciledReplay, err := db.AppendControlPlaneAuditEvent(ctx, reconciledInput)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reconciledReplay.ID).To(Equal(reconciledFirst.ID))

	var count int
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*) FROM ControlPlaneAuditEvent
		WHERE organization_id = @organizationId
		  AND event_type = 'campaign.execution.terminal'
		  AND execution_attempt_id IN (@firstAttemptId, @secondAttemptId)`, pgx.NamedArgs{
		"organizationId": deps.orgID, "firstAttemptId": firstAttemptID,
		"secondAttemptId": secondAttemptID,
	}).Scan(&count)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(count).To(Equal(2))

	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*) FROM ControlPlaneAuditEvent
		WHERE organization_id = @organizationId
		  AND event_type = 'campaign.execution.reconciled'
		  AND execution_attempt_id = @attemptId`, pgx.NamedArgs{
		"organizationId": deps.orgID, "attemptId": firstAttemptID,
	}).Scan(&count)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(count).To(Equal(1))
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

func executionV2ProjectionAuditTestContext(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	hook := db.ControlPlaneAuditAppendHookFunc(
		func(context.Context, types.ControlPlaneAuditEventInput) error { return nil },
	)
	return db.WithExecutionV2AuditHook(db.WithCampaignControlPlaneAuditHook(ctx, hook), hook)
}

func insertExecutionV2ProjectionCampaign(
	t *testing.T,
	ctx context.Context,
	deps taskQueuePlanDeps,
	plans []*types.DeploymentPlan,
	tasks []types.Task,
) (uuid.UUID, []uuid.UUID) {
	t.Helper()
	g := NewWithT(t)
	database := internalctx.GetDb(ctx)
	draftID, revisionID, waveID := uuid.New(), uuid.New(), uuid.New()
	runID, waveRunID := uuid.New(), uuid.New()
	payload := []byte(`{}`)
	checksum := fmt.Sprintf("sha256:%x", sha256.Sum256(payload))
	_, err := database.Exec(ctx, `
		INSERT INTO DeploymentCampaignDraft (
		  id, organization_id, name, membership, waves, risk_policy,
		  created_by_useraccount_id, updated_by_useraccount_id
		) VALUES (
		  @draftId, @organizationId, @name, '{}'::jsonb, '[{}]'::jsonb,
		  '{"maximumConcurrency":10}'::jsonb, @actorId, @actorId
		);
		INSERT INTO DeploymentCampaignRevision (
		  id, deployment_campaign_draft_id, organization_id, revision_number,
		  source_draft_revision, publication_key, name, risk_policy,
		  canonical_payload, canonical_checksum, published_by_useraccount_id
		) VALUES (
		  @revisionId, @draftId, @organizationId, 1, 1, @publicationKey, @name,
		  '{"maximumConcurrency":10}'::jsonb, @payload, @checksum, @actorId
		);
		INSERT INTO DeploymentCampaignWave (
		  id, organization_id, campaign_revision_id, wave_order, name,
		  bake_seconds, maximum_concurrency
		) VALUES (@waveId, @organizationId, @revisionId, 1, 'Wave 1', 0, 10);
		INSERT INTO DeploymentCampaignRun (
		  id, organization_id, campaign_revision_id, started_by_useraccount_id,
		  state, version, current_wave_order, fencing_token
		) VALUES (@runId, @organizationId, @revisionId, @actorId, 'RUNNING', 1, 1, 1);
		INSERT INTO DeploymentCampaignWaveRun (
		  id, campaign_run_id, organization_id, campaign_wave_id,
		  campaign_revision_id, wave_order, maximum_concurrency, status,
		  bake_duration_seconds, started_at
		) VALUES (
		  @waveRunId, @runId, @organizationId, @waveId, @revisionId,
		  1, 10, 'RUNNING', 0, clock_timestamp()
		)`, pgx.NamedArgs{
		"draftId": draftID, "revisionId": revisionID, "waveId": waveID,
		"runId": runID, "waveRunId": waveRunID, "organizationId": deps.orgID,
		"actorId": deps.actorID, "name": "Execution reconciliation " + uuid.NewString(),
		"publicationKey": uuid.NewString(), "payload": payload, "checksum": checksum,
	})
	g.Expect(err).NotTo(HaveOccurred())

	memberRunIDs := make([]uuid.UUID, 0, len(plans))
	for i, plan := range plans {
		var approvalID, admissionID uuid.UUID
		var approvalRevision int64
		var approvalChecksum, admissionChecksum string
		g.Expect(database.QueryRow(ctx, `
			SELECT id, revision, subject_checksum
			FROM ApprovalRequest
			WHERE organization_id = @organizationId AND subject_id = @planId
			ORDER BY created_at DESC, id DESC LIMIT 1`, pgx.NamedArgs{
			"organizationId": deps.orgID, "planId": plan.ID,
		}).Scan(&approvalID, &approvalRevision, &approvalChecksum)).To(Succeed())
		g.Expect(database.QueryRow(ctx, `
			SELECT id, decision_checksum
			FROM AdmissionEvaluation
			WHERE organization_id = @organizationId AND deployment_plan_id = @planId
			ORDER BY created_at DESC, id DESC LIMIT 1`, pgx.NamedArgs{
			"organizationId": deps.orgID, "planId": plan.ID,
		}).Scan(&admissionID, &admissionChecksum)).To(Succeed())
		g.Expect(plan.DeploymentUnitID).NotTo(BeNil())
		memberID, memberRunID := uuid.New(), uuid.New()
		_, err = database.Exec(ctx, `
			INSERT INTO DeploymentCampaignMember (
			  id, organization_id, campaign_revision_id, deployment_plan_id,
			  deployment_unit_id, plan_checksum, effective_policy_checksum,
			  approval_request_id, approval_request_revision, approval_checksum,
			  admission_evaluation_id, admission_checksum, wave_order, member_order
			) VALUES (
			  @memberId, @organizationId, @revisionId, @planId, @deploymentUnitId,
			  @planChecksum, @effectivePolicyChecksum, @approvalId, @approvalRevision,
			  @approvalChecksum, @admissionId, @admissionChecksum, 1, @memberOrder
			);
			INSERT INTO DeploymentCampaignMemberRun (
			  id, campaign_run_id, wave_run_id, organization_id, campaign_member_id,
			  campaign_revision_id, deployment_plan_id, deployment_unit_id,
			  wave_order, member_order, status, admitted_at, admitted_fencing_token
			) VALUES (
			  @memberRunId, @runId, @waveRunId, @organizationId, @memberId,
			  @revisionId, @planId, @deploymentUnitId, 1, @memberOrder, 'RUNNING',
			  clock_timestamp(), 1
			)`, pgx.NamedArgs{
			"memberId": memberID, "memberRunId": memberRunID,
			"runId": runID, "waveRunId": waveRunID, "organizationId": deps.orgID,
			"revisionId": revisionID, "planId": plan.ID,
			"deploymentUnitId": *plan.DeploymentUnitID, "planChecksum": plan.CanonicalChecksum,
			"effectivePolicyChecksum": plan.EffectivePolicyChecksum,
			"approvalId":              approvalID, "approvalRevision": approvalRevision,
			"approvalChecksum": approvalChecksum, "admissionId": admissionID,
			"admissionChecksum": admissionChecksum, "memberOrder": i + 1,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(db.BindCampaignMemberTaskExecution(ctx, db.CampaignMemberTaskExecutionBinding{
			OrganizationID: deps.orgID, CampaignRunID: runID,
			CampaignMemberRunID: memberRunID, DeploymentPlanID: plan.ID,
			TaskID: tasks[i].ID, DeploymentTargetID: tasks[i].DeploymentTargetID,
		})).To(Succeed())
		memberRunIDs = append(memberRunIDs, memberRunID)
	}
	return runID, memberRunIDs
}

func importExecutionV2Reconciliation(
	t *testing.T,
	ctx context.Context,
	deps taskQueuePlanDeps,
	task types.Task,
	attemptID uuid.UUID,
	identity string,
	outcome types.ReconciliationOutcome,
) {
	t.Helper()
	g := NewWithT(t)
	query, err := db.RequestExecutionStatus(ctx, types.StatusRequest{
		OrganizationID: deps.orgID, ExecutionID: task.ID, RequestedBy: deps.actorID,
		IdempotencyKey: identity, Reason: "verify campaign execution outcome",
		RequestedTTLSeconds: 60,
	})
	g.Expect(err).NotTo(HaveOccurred())
	payload := []byte(`{}`)
	_, err = db.ImportReconciliationStatusWithTask(ctx, types.ReconciliationStatusInput{
		OrganizationID: deps.orgID, ExecutionID: task.ID, AttemptID: attemptID,
		StatusQueryID: query.ID, EventIdentity: uuid.New(), Outcome: outcome,
		EvidenceChecksum: "sha256:" + executionV2RepeatHex("44"),
		ObservedAt:       time.Now().UTC(),
		SignedEvidence: types.SignedReconciliationEvidence{
			Payload: payload, Checksum: fmt.Sprintf("sha256:%x", sha256.Sum256(payload)),
			KeyID: "sha256:" + executionV2RepeatHex("55"), Signature: strings.Repeat("a", 80),
		},
	})
	g.Expect(err).NotTo(HaveOccurred())
}

func assertExecutionV2MemberUncertain(
	t *testing.T,
	ctx context.Context,
	memberRunID uuid.UUID,
	expected bool,
) {
	t.Helper()
	g := NewWithT(t)
	var uncertain bool
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT execution_uncertain
		FROM DeploymentCampaignMemberRun WHERE id = @id`,
		pgx.NamedArgs{"id": memberRunID},
	).Scan(&uncertain)).To(Succeed())
	g.Expect(uncertain).To(Equal(expected))
}

func assertExecutionV2CampaignFlags(
	t *testing.T,
	ctx context.Context,
	runID uuid.UUID,
	reconciliationRequired, admissionsBlocked bool,
) {
	t.Helper()
	g := NewWithT(t)
	var actualReconciliation, actualBlocked bool
	g.Expect(internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT reconciliation_required, admissions_blocked
		FROM DeploymentCampaignRun WHERE id = @id`,
		pgx.NamedArgs{"id": runID},
	).Scan(&actualReconciliation, &actualBlocked)).To(Succeed())
	g.Expect(actualReconciliation).To(Equal(reconciliationRequired))
	g.Expect(actualBlocked).To(Equal(admissionsBlocked))
}

func insertProjectionRetryAttempt(
	t *testing.T,
	ctx context.Context,
	organizationID uuid.UUID,
	task types.Task,
	attemptNumber int,
) uuid.UUID {
	t.Helper()
	g := NewWithT(t)
	attemptID := uuid.New()
	now := time.Now().UTC()
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ExecutionAttempt (
		  id, organization_id, deployment_target_id, task_id, step_run_id,
		  execution_id, attempt_number, step_key, status,
		  plan_checksum, artifact_digest, config_checksum, adapter_revision,
		  intent_issued_at, intent_expires_at, cancellable, retry_safe
		) VALUES (
		  @id, @organizationId, @deploymentTargetId, @taskId, @stepRunId,
		  @executionId, @attemptNumber, @stepKey, 'PENDING',
		  @planChecksum, @artifactDigest, @configChecksum, 'adapter.compose@2',
		  @issuedAt, @expiresAt, TRUE, TRUE
		)`, pgx.NamedArgs{
		"id": attemptID, "organizationId": organizationID,
		"deploymentTargetId": task.DeploymentTargetID, "taskId": task.ID,
		"stepRunId": task.StepRuns[0].ID, "executionId": task.ID,
		"attemptNumber": attemptNumber, "stepKey": task.StepRuns[0].StepKey,
		"planChecksum":   "sha256:" + executionV2RepeatHex("11"),
		"artifactDigest": "sha256:" + executionV2RepeatHex("22"),
		"configChecksum": "sha256:" + executionV2RepeatHex("33"),
		"issuedAt":       now.Add(-time.Minute), "expiresAt": now.Add(10 * time.Minute),
	})
	g.Expect(err).NotTo(HaveOccurred())
	return attemptID
}
