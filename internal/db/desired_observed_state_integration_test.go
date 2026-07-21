package db_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/deploymentregistry"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

type desiredObservedFixture struct {
	ctx                context.Context
	organizationID     uuid.UUID
	deploymentPlanID   uuid.UUID
	executionID        uuid.UUID
	executionAttemptID uuid.UUID
	deploymentUnitID   uuid.UUID
	componentID        uuid.UUID
	secondComponentID  uuid.UUID
	otherUnitID        uuid.UUID
	otherComponentID   uuid.UUID
}

func TestDesiredObservedLifecyclePromotesOnlyAfterExecutorAndIndependentEvidence(
	t *testing.T,
) {
	g := NewWithT(t)
	fixture := newDesiredObservedFixture(t)
	input := fixture.pendingInput()
	pending, err := db.AdmitPendingDesiredRevision(fixture.ctx, input)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pending.ExecutionAttemptID).To(Equal(fixture.executionAttemptID))
	registration, credential := fixture.createObserver(t, fixture.componentID, "lifecycle")

	observed, err := db.IngestObservation(
		fixture.ctx,
		fixture.envelope(registration, credential, input, 1),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(observed.Disposition).To(Equal(types.ObservationDispositionAccepted))
	g.Expect(readPendingStatus(t, fixture.ctx, pending.ID)).To(
		Equal(types.PendingDesiredStatusPending),
	)

	_, err = db.RecordExecutorReport(fixture.ctx, types.ExecutorReport{
		OrganizationID: fixture.organizationID, PendingRevisionID: pending.ID,
		ExecutionID: input.ExecutionID, Outcome: types.ExecutorOutcomeSucceeded,
		ReportedStateChecksum: observed.StateChecksum,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(readPendingStatus(t, fixture.ctx, pending.ID)).To(
		Equal(types.PendingDesiredStatusVerified),
	)
	projected, err := db.GetTask(fixture.ctx, fixture.executionID, fixture.organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(projected.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(projected.StepRuns).To(HaveLen(1))
	g.Expect(projected.StepRuns[0].Status).To(Equal(types.StepRunStatusSucceeded))
	g.Expect(countRowsForOrganization(
		t,
		fixture.ctx,
		"ActiveDesiredRevision",
		fixture.organizationID,
	)).To(Equal(int64(1)))
}

func TestExecutorSuccessWaitsForTrustedObservationBeforeProjection(t *testing.T) {
	g := NewWithT(t)
	fixture := newDesiredObservedFixture(t)
	input := fixture.pendingInput()
	pending, err := db.AdmitPendingDesiredRevision(fixture.ctx, input)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.RecordExecutorReport(fixture.ctx, types.ExecutorReport{
		OrganizationID: fixture.organizationID, PendingRevisionID: pending.ID,
		ExecutionID: input.ExecutionID, Outcome: types.ExecutorOutcomeSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(readPendingStatus(t, fixture.ctx, pending.ID)).To(
		Equal(types.PendingDesiredStatusPending),
	)
	deferred, err := db.GetTask(fixture.ctx, fixture.executionID, fixture.organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deferred.Status).To(Equal(types.TaskStatusRunning))
	g.Expect(deferred.StepRuns[0].Status).To(Equal(types.StepRunStatusRunning))

	registration, credential := fixture.createObserver(t, fixture.componentID, "projection-gate")
	_, err = db.IngestObservation(
		fixture.ctx,
		fixture.envelope(registration, credential, input, 1),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(readPendingStatus(t, fixture.ctx, pending.ID)).To(
		Equal(types.PendingDesiredStatusVerified),
	)
	projected, err := db.GetTask(fixture.ctx, fixture.executionID, fixture.organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(projected.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(projected.StepRuns[0].Status).To(Equal(types.StepRunStatusSucceeded))
}

func TestComponentDeployCompletionDefersProjectionUntilObservationGate(t *testing.T) {
	g := NewWithT(t)
	fixture := newDesiredObservedFixture(t)
	input := fixture.pendingInput()
	pending, err := db.AdmitPendingDesiredRevision(fixture.ctx, input)
	g.Expect(err).NotTo(HaveOccurred())
	const executorID = "component-gate-executor"
	_, err = internalctx.GetDb(fixture.ctx).Exec(fixture.ctx, `
		UPDATE ExecutionAttempt
		SET status = 'CLAIMED', claimed_by = @executorID
		WHERE id = @attemptID AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"executorID": executorID, "attemptID": fixture.executionAttemptID,
			"organizationID": fixture.organizationID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(fixture.ctx).Exec(fixture.ctx, `
		UPDATE ExecutionFence
		SET lease_expires_at = clock_timestamp() + interval '5 minutes'
		WHERE execution_attempt_id = @attemptID
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"attemptID":      fixture.executionAttemptID,
			"organizationID": fixture.organizationID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())

	projected, err := db.CompleteExecutionAttempt(fixture.ctx, types.CompletionInput{
		OrganizationID: fixture.organizationID, DeploymentTargetID: readExecutionTargetID(
			t, fixture.ctx, fixture.executionAttemptID,
		),
		AttemptID: fixture.executionAttemptID, ExecutorID: executorID,
		FenceGeneration: 1, Status: types.ExecutionAttemptStatusSucceeded,
		CompletedAt: time.Now().UTC(),
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(projected).To(BeNil())
	g.Expect(readPendingStatus(t, fixture.ctx, pending.ID)).To(
		Equal(types.PendingDesiredStatusPending),
	)
	deferred, err := db.GetTask(fixture.ctx, fixture.executionID, fixture.organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deferred.Status).To(Equal(types.TaskStatusRunning))
	g.Expect(deferred.StepRuns[0].Status).To(Equal(types.StepRunStatusRunning))

	registration, credential := fixture.createObserver(t, fixture.componentID, "completion-gate")
	_, err = db.IngestObservation(
		fixture.ctx,
		fixture.envelope(registration, credential, input, 1),
	)
	g.Expect(err).NotTo(HaveOccurred())
	completed, err := db.GetTask(fixture.ctx, fixture.executionID, fixture.organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(completed.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(completed.StepRuns[0].Status).To(Equal(types.StepRunStatusSucceeded))
}

func TestUnknownDesiredOutcomeKeepsTaskAndStepNonterminal(t *testing.T) {
	g := NewWithT(t)
	fixture := newDesiredObservedFixture(t)
	input := fixture.pendingInput()
	pending, err := db.AdmitPendingDesiredRevision(fixture.ctx, input)
	g.Expect(err).NotTo(HaveOccurred())

	_, projected, err := db.RecordExecutorReportWithTask(fixture.ctx, types.ExecutorReport{
		OrganizationID: fixture.organizationID, PendingRevisionID: pending.ID,
		ExecutionID: input.ExecutionID, Outcome: types.ExecutorOutcomeUnknown,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(projected).To(BeNil())
	g.Expect(readPendingStatus(t, fixture.ctx, pending.ID)).To(
		Equal(types.PendingDesiredStatusUnknown),
	)
	deferred, err := db.GetTask(fixture.ctx, fixture.executionID, fixture.organizationID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deferred.Status).To(Equal(types.TaskStatusRunning))
	g.Expect(deferred.StepRuns).To(HaveLen(1))
	g.Expect(deferred.StepRuns[0].Status).To(Equal(types.StepRunStatusRunning))
}

func TestDesiredObservedPromotionRechecksAllObserversForConflict(t *testing.T) {
	g := NewWithT(t)
	fixture := newDesiredObservedFixture(t)
	input := fixture.pendingInput()
	pending, err := db.AdmitPendingDesiredRevision(fixture.ctx, input)
	g.Expect(err).NotTo(HaveOccurred())
	first, firstCredential := fixture.createObserver(t, fixture.componentID, "first")
	second, secondCredential := fixture.createObserver(t, fixture.componentID, "second")

	_, err = db.IngestObservation(
		fixture.ctx,
		fixture.envelope(first, firstCredential, input, 1),
	)
	g.Expect(err).NotTo(HaveOccurred())
	conflicting := fixture.envelope(second, secondCredential, input, 1)
	conflicting.ArtifactDigest = desiredObservedTestDigest("conflicting-runtime")
	_, err = db.IngestObservation(fixture.ctx, conflicting)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.RecordExecutorReport(fixture.ctx, types.ExecutorReport{
		OrganizationID: fixture.organizationID, PendingRevisionID: pending.ID,
		ExecutionID: input.ExecutionID, Outcome: types.ExecutorOutcomeSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(readPendingStatus(t, fixture.ctx, pending.ID)).To(
		Equal(types.PendingDesiredStatusConflict),
	)
	g.Expect(countRowsForOrganization(
		t,
		fixture.ctx,
		"ActiveDesiredRevision",
		fixture.organizationID,
	)).To(Equal(int64(0)))
}

func TestDesiredObservedRepositoryEnforcesPlacementExecutionAndReplayBoundaries(
	t *testing.T,
) {
	g := NewWithT(t)
	fixture := newDesiredObservedFixture(t)
	input := fixture.pendingInput()
	wrongPlacement := input
	wrongPlacement.DeploymentUnitID = fixture.otherUnitID
	_, err := db.AdmitPendingDesiredRevision(fixture.ctx, wrongPlacement)
	g.Expect(err).To(HaveOccurred())

	pending, err := db.AdmitPendingDesiredRevision(fixture.ctx, input)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.RecordExecutorReport(fixture.ctx, types.ExecutorReport{
		OrganizationID: fixture.organizationID, PendingRevisionID: pending.ID,
		ExecutionID: uuid.New(), Outcome: types.ExecutorOutcomeSucceeded,
	})
	g.Expect(err).To(HaveOccurred())

	registration, credential := fixture.createObserver(t, uuid.Nil, "replay")
	envelope := fixture.envelope(registration, credential, input, 1)
	first, err := db.IngestObservation(fixture.ctx, envelope)
	g.Expect(err).NotTo(HaveOccurred())
	replay, err := db.IngestObservation(fixture.ctx, envelope)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replay.ID).To(Equal(first.ID))

	conflict := envelope
	conflict.EvidenceReference = "probe://mutated-material"
	retained, err := db.IngestObservation(fixture.ctx, conflict)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(retained.Disposition).To(Equal(types.ObservationDispositionConflict))

	secondComponent := envelope
	secondComponent.ComponentInstanceID = fixture.secondComponentID
	secondComponent.ComponentKey = "worker"
	accepted, err := db.IngestObservation(fixture.ctx, secondComponent)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(accepted.Disposition).To(Equal(types.ObservationDispositionAccepted))
}

func TestDesiredObservedRetentionDeletesEvidenceBeforeDeploymentPlan(t *testing.T) {
	g := NewWithT(t)
	fixture := newDesiredObservedFixture(t)
	input := fixture.pendingInput()
	pending, err := db.AdmitPendingDesiredRevision(fixture.ctx, input)
	g.Expect(err).NotTo(HaveOccurred())
	registration, credential := fixture.createObserver(t, fixture.componentID, "retention")
	_, err = db.IngestObservation(
		fixture.ctx,
		fixture.envelope(registration, credential, input, 1),
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.RecordExecutorReport(fixture.ctx, types.ExecutorReport{
		OrganizationID: fixture.organizationID, PendingRevisionID: pending.ID,
		ExecutionID: input.ExecutionID, Outcome: types.ExecutorOutcomeSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(fixture.ctx).Exec(fixture.ctx, `
		UPDATE Organization
		SET deleted_at = now() - interval '2 hours'
		WHERE id = @organizationID`,
		pgx.NamedArgs{"organizationID": fixture.organizationID},
	)
	g.Expect(err).NotTo(HaveOccurred())

	deleted, err := db.DeleteOrganizationsOlderThan(fixture.ctx, time.Hour)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deleted).To(Equal(int64(1)))
	g.Expect(countRowsForOrganization(
		t,
		fixture.ctx,
		"PendingDesiredRevision",
		fixture.organizationID,
	)).To(Equal(int64(0)))
	g.Expect(countRowsForOrganization(
		t,
		fixture.ctx,
		"ObservedComponentState",
		fixture.organizationID,
	)).To(Equal(int64(0)))
}

func TestDriftAndReconciliationRequireSamePlacementAndProvenOutcome(t *testing.T) {
	g := NewWithT(t)
	fixture := newDesiredObservedFixture(t)
	input := fixture.pendingInput()
	pending, err := db.AdmitPendingDesiredRevision(fixture.ctx, input)
	g.Expect(err).NotTo(HaveOccurred())
	registration, credential := fixture.createObserver(t, uuid.Nil, "drift")
	_, err = db.IngestObservation(
		fixture.ctx,
		fixture.envelope(registration, credential, input, 1),
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.RecordExecutorReport(fixture.ctx, types.ExecutorReport{
		OrganizationID: fixture.organizationID, PendingRevisionID: pending.ID,
		ExecutionID: input.ExecutionID, Outcome: types.ExecutorOutcomeSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())
	active := readActiveDesired(t, fixture.ctx, pending.ID)

	other := fixture.envelope(registration, credential, input, 2)
	other.ComponentInstanceID = fixture.secondComponentID
	other.ComponentKey = "worker"
	otherState, err := db.IngestObservation(fixture.ctx, other)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.OpenDriftCase(fixture.ctx, types.DriftInput{
		OrganizationID: fixture.organizationID, ActiveDesiredRevisionID: active.ID,
		ObservationID: otherState.ID,
		Classification: types.DriftClassification{
			Drifted: true, Classes: []types.DriftClass{types.DriftClassArtifact},
			Summary: "cross-placement drift must be rejected",
		},
		Reason: "boundary test",
	})
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	driftedEnvelope := fixture.envelope(registration, credential, input, 2)
	driftedEnvelope.ArtifactDigest = desiredObservedTestDigest("manual-drift")
	drifted, err := db.IngestObservation(fixture.ctx, driftedEnvelope)
	g.Expect(err).NotTo(HaveOccurred())
	driftCase, err := db.ClassifyAndOpenDriftCase(
		fixture.ctx,
		active,
		*drifted,
		"manual drift",
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(driftCase).NotTo(BeNil())

	err = db.ResolveDriftCase(fixture.ctx, types.ReconciliationDecision{
		OrganizationID: fixture.organizationID, DriftCaseID: driftCase.ID,
		Action: types.ReconciliationActionCreatePlan, Reason: "create reviewed restoration",
		ActorID: uuid.New(), DeploymentPlanID: &fixture.deploymentPlanID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(readDriftStatus(t, fixture.ctx, driftCase.ID)).To(
		Equal(types.DriftCaseStatusAssigned),
	)

	err = db.ResolveDriftCase(fixture.ctx, types.ReconciliationDecision{
		OrganizationID: fixture.organizationID, DriftCaseID: driftCase.ID,
		Action: types.ReconciliationActionCloseWithEvidence, Reason: "still drifted",
		ActorID: uuid.New(), OutcomeObservationID: &drifted.ID,
	})
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	restoredEnvelope := fixture.envelope(registration, credential, input, 3)
	restored, err := db.IngestObservation(fixture.ctx, restoredEnvelope)
	g.Expect(err).NotTo(HaveOccurred())
	restoredID := restored.ID
	err = db.ResolveDriftCase(fixture.ctx, types.ReconciliationDecision{
		OrganizationID: fixture.organizationID, DriftCaseID: driftCase.ID,
		Action: types.ReconciliationActionCloseWithEvidence, Reason: "restored and verified",
		ActorID: uuid.New(), OutcomeObservationID: &restoredID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(readDriftStatus(t, fixture.ctx, driftCase.ID)).To(
		Equal(types.DriftCaseStatusResolved),
	)
}

func TestAcceptedObservationReplayRepairsDriftWithoutDuplicateCaseEvents(t *testing.T) {
	g := NewWithT(t)
	fixture := newDesiredObservedFixture(t)
	input := fixture.pendingInput()
	pending, err := db.AdmitPendingDesiredRevision(fixture.ctx, input)
	g.Expect(err).NotTo(HaveOccurred())
	registration, credential := fixture.createObserver(t, fixture.componentID, "replay-drift")
	initial := fixture.envelope(registration, credential, input, 1)
	observed, err := db.IngestObservation(fixture.ctx, initial)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.RecordExecutorReport(fixture.ctx, types.ExecutorReport{
		OrganizationID: fixture.organizationID, PendingRevisionID: pending.ID,
		ExecutionID: input.ExecutionID, Outcome: types.ExecutorOutcomeSucceeded,
		ReportedStateChecksum: observed.StateChecksum,
	})
	g.Expect(err).NotTo(HaveOccurred())

	drifted := fixture.envelope(registration, credential, input, 2)
	drifted.ArtifactDigest = desiredObservedTestDigest("replay-drifted-artifact")
	first, err := db.IngestObservation(fixture.ctx, drifted)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first.Disposition).To(Equal(types.ObservationDispositionAccepted))
	g.Expect(countRowsForOrganization(
		t, fixture.ctx, "DriftCase", fixture.organizationID,
	)).To(Equal(int64(1)))
	g.Expect(countRowsForOrganization(
		t, fixture.ctx, "DriftCaseEvent", fixture.organizationID,
	)).To(Equal(int64(1)))

	replay, err := db.IngestObservation(fixture.ctx, drifted)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replay.ID).To(Equal(first.ID))
	g.Expect(countRowsForOrganization(
		t, fixture.ctx, "DriftCase", fixture.organizationID,
	)).To(Equal(int64(1)))
	g.Expect(countRowsForOrganization(
		t, fixture.ctx, "DriftCaseEvent", fixture.organizationID,
	)).To(Equal(int64(1)))
}

func newDesiredObservedFixture(t *testing.T) desiredObservedFixture {
	t.Helper()
	ctx := taskQueueDBTestContext(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "desired-observed-target")
	targetID := deps.plan.Targets[0].DeploymentTargetID
	scope := types.DeploymentScope{
		OrganizationID: deps.orgID, Key: "desired-observed",
		Name: "Desired observed", DeliveryModel: types.DeliveryModelExternal,
		ManagementState: types.RegistryManagementStateManaged,
	}
	NewWithT(t).Expect(db.CreateDeploymentScope(ctx, &scope)).To(Succeed())
	assignment := types.TargetEnvironmentAssignment{
		OrganizationID: deps.orgID, DeploymentTargetID: targetID,
		EnvironmentID: deps.devEnvironmentID, ActiveFrom: time.Now().UTC().Add(-time.Hour),
	}
	NewWithT(t).Expect(db.CreateTargetEnvironmentAssignment(ctx, &assignment)).To(Succeed())

	unit := createDesiredObservedUnit(t, ctx, deps.orgID, scope.ID, assignment, "primary")
	first := createDesiredObservedComponent(t, ctx, deps.orgID, unit.ID, "api")
	second := createDesiredObservedComponent(t, ctx, deps.orgID, unit.ID, "worker")
	otherUnit := createDesiredObservedUnit(t, ctx, deps.orgID, scope.ID, assignment, "other")
	other := createDesiredObservedComponent(t, ctx, deps.orgID, otherUnit.ID, "other")
	executionID, executionAttemptID := createDesiredObservedExecutionAttempt(
		t, ctx, deps, targetID,
	)
	return desiredObservedFixture{
		ctx: ctx, organizationID: deps.orgID, deploymentPlanID: deps.plan.ID,
		executionID: executionID, executionAttemptID: executionAttemptID,
		deploymentUnitID: unit.ID, componentID: first.ID,
		secondComponentID: second.ID, otherUnitID: otherUnit.ID,
		otherComponentID: other.ID,
	}
}

func createDesiredObservedExecutionAttempt(
	t *testing.T,
	ctx context.Context,
	deps taskQueuePlanDeps,
	targetID uuid.UUID,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	g := NewWithT(t)
	taskID := uuid.New()
	stepRunID := uuid.New()
	attemptID := uuid.New()
	step := deps.plan.Steps[0]
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO Task (
			id, organization_id, task_type, deployment_plan_id,
			deployment_plan_target_id, deployment_target_id, application_id,
			release_bundle_id, channel_id, environment_id, status, protocol_version,
			started_at
		) VALUES (
			@id, @organizationID, 'deployment', @deploymentPlanID,
			@deploymentPlanTargetID, @deploymentTargetID, @applicationID,
			@releaseBundleID, @channelID, @environmentID, 'RUNNING', 'v2', now()
		)`,
		pgx.NamedArgs{
			"id": taskID, "organizationID": deps.orgID,
			"deploymentPlanID":       deps.plan.ID,
			"deploymentPlanTargetID": deps.plan.Targets[0].ID,
			"deploymentTargetID":     targetID, "applicationID": deps.plan.ApplicationID,
			"releaseBundleID": deps.plan.ReleaseBundleID, "channelID": deps.plan.ChannelID,
			"environmentID": deps.plan.EnvironmentID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO StepRun (
			id, organization_id, task_id, deployment_plan_id,
			deployment_plan_step_id, step_key, name, action_type,
			status, sort_order, started_at
		) VALUES (
			@id, @organizationID, @taskID, @deploymentPlanID,
			@deploymentPlanStepID, @stepKey, @name, @actionType,
			'RUNNING', @sortOrder, now()
		)`,
		pgx.NamedArgs{
			"id": stepRunID, "organizationID": deps.orgID, "taskID": taskID,
			"deploymentPlanID": deps.plan.ID, "deploymentPlanStepID": step.ID,
			"stepKey": step.StepKey, "name": step.Name, "actionType": step.ActionType,
			"sortOrder": step.SortOrder,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ExecutionAttempt (
			id, organization_id, deployment_target_id, task_id, step_run_id,
			execution_id, attempt_number, step_key, status, plan_checksum,
			artifact_digest, config_checksum, adapter_revision,
			intent_issued_at, intent_expires_at
		) VALUES (
			@id, @organizationID, @deploymentTargetID, @taskID, @stepRunID,
			@executionID, 1, @stepKey, 'PENDING', @planChecksum,
			@artifactDigest, @configChecksum, 'test.adapter@2',
			now(), now() + interval '10 minutes'
		)`,
		pgx.NamedArgs{
			"id": attemptID, "organizationID": deps.orgID,
			"deploymentTargetID": targetID, "taskID": taskID, "stepRunID": stepRunID,
			"executionID": taskID, "stepKey": step.StepKey,
			"planChecksum":   desiredObservedTestDigest("plan"),
			"artifactDigest": desiredObservedTestDigest("artifact"),
			"configChecksum": desiredObservedTestDigest("config"),
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ExecutionFence (
			execution_attempt_id, organization_id, resource_key, generation
		) VALUES (@attemptID, @organizationID, @resourceKey, 1)`,
		pgx.NamedArgs{
			"attemptID": attemptID, "organizationID": deps.orgID,
			"resourceKey": "desired-observed:" + taskID.String(),
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	return taskID, attemptID
}

func readExecutionTargetID(
	t *testing.T,
	ctx context.Context,
	attemptID uuid.UUID,
) uuid.UUID {
	t.Helper()
	g := NewWithT(t)
	var targetID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT deployment_target_id
		FROM ExecutionAttempt
		WHERE id = @attemptID`,
		pgx.NamedArgs{"attemptID": attemptID},
	).Scan(&targetID)
	g.Expect(err).NotTo(HaveOccurred())
	return targetID
}

func createDesiredObservedUnit(
	t *testing.T,
	ctx context.Context,
	organizationID, scopeID uuid.UUID,
	assignment types.TargetEnvironmentAssignment,
	key string,
) types.DeploymentUnit {
	t.Helper()
	unit := types.DeploymentUnit{
		OrganizationID: organizationID, DeploymentScopeID: scopeID,
		TargetEnvironmentAssignmentID: assignment.ID,
		DeploymentTargetID:            assignment.DeploymentTargetID,
		Key:                           "desired-observed-" + key, Name: "Desired observed " + key,
		PhysicalIdentity:      "desired-observed/" + key,
		ManagementState:       types.RegistryManagementStateManaged,
		SubscriberSetChecksum: deploymentregistry.SubscriberSetChecksum(nil),
	}
	NewWithT(t).Expect(db.CreateDeploymentUnit(ctx, &unit)).To(Succeed())
	return unit
}

func createDesiredObservedComponent(
	t *testing.T,
	ctx context.Context,
	organizationID, unitID uuid.UUID,
	key string,
) types.ComponentInstance {
	t.Helper()
	definition := types.ComponentDefinition{
		OrganizationID: organizationID, Key: "desired-observed-" + key,
		Name:            "Desired observed " + key,
		ManagementState: types.RegistryManagementStateManaged,
	}
	NewWithT(t).Expect(db.CreateComponentDefinition(ctx, &definition)).To(Succeed())
	instance := types.ComponentInstance{
		OrganizationID: organizationID, DeploymentUnitID: unitID,
		ComponentDefinitionID: definition.ID, PhysicalName: key,
		ManagementState: types.RegistryManagementStateManaged,
	}
	NewWithT(t).Expect(db.CreateComponentInstance(ctx, &instance)).To(Succeed())
	return instance
}

func (f desiredObservedFixture) pendingInput() types.PendingDesiredRevisionInput {
	return types.PendingDesiredRevisionInput{
		OrganizationID: f.organizationID, DeploymentPlanID: f.deploymentPlanID,
		ExecutionID: f.executionID, ExecutionAttemptID: f.executionAttemptID,
		DeploymentUnitID:    f.deploymentUnitID,
		ComponentInstanceID: f.componentID, ComponentKey: "api",
		ArtifactDigest: desiredObservedTestDigest("artifact"),
		ConfigChecksum: desiredObservedTestDigest("config"), SchemaVersion: "1",
		CapabilityChecksum: desiredObservedTestDigest("capability"),
		Platform:           "linux/amd64", TopologyChecksum: desiredObservedTestDigest("topology"),
		ObservationDeadline: time.Now().UTC().Add(5 * time.Minute),
	}
}

func (f desiredObservedFixture) createObserver(
	t *testing.T,
	componentID uuid.UUID,
	key string,
) (types.ObserverRegistration, string) {
	t.Helper()
	credential := "observer-credential-" + uuid.NewString()
	registration := types.ObserverRegistration{
		OrganizationID: f.organizationID, DeploymentUnitID: f.deploymentUnitID,
		ObserverKey: "desired-observed-" + key, AdapterImplementation: "test",
		AdapterVersion: "1", Enabled: true,
		CredentialFingerprint: desiredObservedTestDigest(credential),
		MaxFreshness:          time.Hour, MaxClockSkew: time.Minute,
		Measurements: []string{"artifact", "config", "schema", "health"},
	}
	if componentID != uuid.Nil {
		registration.ComponentInstanceID = &componentID
	}
	created, err := db.CreateObserverRegistration(f.ctx, &registration)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return *created, credential
}

func (f desiredObservedFixture) envelope(
	registration types.ObserverRegistration,
	credential string,
	input types.PendingDesiredRevisionInput,
	sequence int64,
) types.ObservationEnvelope {
	return types.ObservationEnvelope{
		OrganizationID: f.organizationID, ObserverID: registration.ID,
		DeploymentUnitID:    input.DeploymentUnitID,
		ComponentInstanceID: input.ComponentInstanceID,
		ComponentKey:        input.ComponentKey, SourceSequence: sequence,
		CapturedAt: time.Now().UTC(), CredentialFingerprint: desiredObservedTestDigest(credential),
		EvidenceChecksum:  desiredObservedTestDigest(fmt.Sprintf("evidence-%s-%d", registration.ID, sequence)),
		EvidenceReference: "probe://desired-observed",
		ArtifactDigest:    input.ArtifactDigest, ConfigChecksum: input.ConfigChecksum,
		SchemaVersion: input.SchemaVersion, CapabilityChecksum: input.CapabilityChecksum,
		Platform: input.Platform, TopologyChecksum: input.TopologyChecksum,
		Health: types.ObservedHealthHealthy, Outcome: types.ObservationOutcomeComplete,
	}
}

func readPendingStatus(
	t *testing.T,
	ctx context.Context,
	pendingID uuid.UUID,
) types.PendingDesiredStatus {
	t.Helper()
	var status types.PendingDesiredStatus
	NewWithT(t).Expect(internalctx.GetDb(ctx).QueryRow(
		ctx,
		"SELECT status FROM PendingDesiredRevision WHERE id = @id",
		pgx.NamedArgs{"id": pendingID},
	).Scan(&status)).To(Succeed())
	return status
}

func readDriftStatus(
	t *testing.T,
	ctx context.Context,
	driftCaseID uuid.UUID,
) types.DriftCaseStatus {
	t.Helper()
	var status types.DriftCaseStatus
	NewWithT(t).Expect(internalctx.GetDb(ctx).QueryRow(
		ctx,
		"SELECT status FROM DriftCase WHERE id = @id",
		pgx.NamedArgs{"id": driftCaseID},
	).Scan(&status)).To(Succeed())
	return status
}

func readActiveDesired(
	t *testing.T,
	ctx context.Context,
	pendingID uuid.UUID,
) types.ActiveDesiredRevision {
	t.Helper()
	var value types.ActiveDesiredRevision
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id, created_at, organization_id, pending_revision_id,
			deployment_plan_id, execution_id, deployment_unit_id,
			component_instance_id, component_key, revision, artifact_digest,
			config_checksum, schema_version, capability_checksum, platform,
			topology_checksum, verified_observation_id
		FROM ActiveDesiredRevision
		WHERE pending_revision_id = @pendingRevisionID`,
		pgx.NamedArgs{"pendingRevisionID": pendingID},
	).Scan(
		&value.ID, &value.CreatedAt, &value.OrganizationID, &value.PendingRevisionID,
		&value.DeploymentPlanID, &value.ExecutionID, &value.DeploymentUnitID,
		&value.ComponentInstanceID, &value.ComponentKey, &value.Revision,
		&value.ArtifactDigest, &value.ConfigChecksum, &value.SchemaVersion,
		&value.CapabilityChecksum, &value.Platform, &value.TopologyChecksum,
		&value.VerifiedObservationID,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return value
}

func countRowsForOrganization(
	t *testing.T,
	ctx context.Context,
	table string,
	organizationID uuid.UUID,
) int64 {
	t.Helper()
	var count int64
	statement := "SELECT count(*) FROM " + pgx.Identifier{table}.Sanitize() +
		" WHERE organization_id = @organizationID"
	NewWithT(t).Expect(internalctx.GetDb(ctx).QueryRow(
		ctx,
		statement,
		pgx.NamedArgs{"organizationID": organizationID},
	).Scan(&count)).To(Succeed())
	return count
}

func desiredObservedTestDigest(value string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}
