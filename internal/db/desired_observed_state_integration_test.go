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
	ctx               context.Context
	organizationID    uuid.UUID
	deploymentPlanID  uuid.UUID
	executionID       uuid.UUID
	deploymentUnitID  uuid.UUID
	componentID       uuid.UUID
	secondComponentID uuid.UUID
	otherUnitID       uuid.UUID
	otherComponentID  uuid.UUID
}

func TestDesiredObservedLifecyclePromotesOnlyAfterExecutorAndIndependentEvidence(
	t *testing.T,
) {
	g := NewWithT(t)
	fixture := newDesiredObservedFixture(t)
	input := fixture.pendingInput()
	pending, err := db.AdmitPendingDesiredRevision(fixture.ctx, input)
	g.Expect(err).NotTo(HaveOccurred())
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
	g.Expect(countRowsForOrganization(
		t,
		fixture.ctx,
		"ActiveDesiredRevision",
		fixture.organizationID,
	)).To(Equal(int64(1)))
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
	return desiredObservedFixture{
		ctx: ctx, organizationID: deps.orgID, deploymentPlanID: deps.plan.ID,
		executionID: uuid.New(), deploymentUnitID: unit.ID, componentID: first.ID,
		secondComponentID: second.ID, otherUnitID: otherUnit.ID,
		otherComponentID: other.ID,
	}
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
		ExecutionID: f.executionID, DeploymentUnitID: f.deploymentUnitID,
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
