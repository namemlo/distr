package db

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestMigration159CreatesIndependentAppendOnlyStateAndMutableHeads(t *testing.T) {
	g := NewWithT(t)
	root := filepath.Join("..", "migrations", "sql")
	up, err := os.ReadFile(filepath.Join(root, "159_desired_observed_reconciliation.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join(root, "159_desired_observed_reconciliation.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upText := string(up)
	downText := string(down)

	for _, table := range []string{
		"PendingDesiredRevision", "ActiveDesiredRevision", "ComponentDesiredStateHead",
		"ExecutorReport", "ObserverRegistration", "ObservedComponentState",
		"ComponentObservationHead", "DriftCase", "DriftCaseEvent", "ReconciliationAction",
	} {
		g.Expect(upText).To(ContainSubstring("CREATE TABLE " + table))
	}
	g.Expect(upText).To(ContainSubstring("desired_observed_append_only_guard"))
	g.Expect(upText).To(ContainSubstring("FOREIGN KEY (organization_id)"))
	g.Expect(upText).To(ContainSubstring(
		"FOREIGN KEY (actual_observation_id, organization_id)",
	))
	g.Expect(upText).To(ContainSubstring(
		"REFERENCES ObservedComponentState(id, organization_id)",
	))
	g.Expect(downText).To(ContainSubstring(
		"campaignprerequisiteevaluation_observation_fk",
	))
	g.Expect(upText).To(ContainSubstring("source_sequence"))
	g.Expect(upText).To(ContainSubstring("runtime_state_checksum TEXT NOT NULL CHECK"))
	g.Expect(upText).To(ContainSubstring("NEW.runtime_state_checksum = OLD.runtime_state_checksum"))
	g.Expect(upText).To(ContainSubstring("execution_attempt_id UUID NOT NULL"))
	g.Expect(upText).To(ContainSubstring(
		"FOREIGN KEY (execution_attempt_id, organization_id, execution_id)",
	))
	g.Expect(upText).To(ContainSubstring("PendingDesiredRevision_pending_deadline"))
	g.Expect(upText).To(ContainSubstring(
		"UNIQUE (id, deployment_unit_id, organization_id)",
	))
	g.Expect(upText).To(ContainSubstring(
		"component_instance_id, deployment_unit_id, organization_id\n    )\n    REFERENCES ComponentInstance",
	))
	g.Expect(upText).To(ContainSubstring(
		"FOREIGN KEY (pending_revision_id, execution_id, organization_id)",
	))
	g.Expect(upText).To(ContainSubstring(
		"organization_id, observer_id, deployment_unit_id,\n      component_instance_id, source_sequence, state_checksum",
	))
	g.Expect(upText).To(ContainSubstring("credential_fingerprint"))
	g.Expect(upText).To(ContainSubstring("accepted_until"))
	g.Expect(upText).To(ContainSubstring(
		"NEW.id IS DISTINCT FROM OLD.id",
	))
	g.Expect(upText).To(ContainSubstring(
		"NEW.created_at IS DISTINCT FROM OLD.created_at",
	))
	g.Expect(upText).NotTo(ContainSubstring("ALTER TABLE TargetComponentState"))
	g.Expect(downText).To(ContainSubstring("refusing migration 159 rollback"))
	g.Expect(downText).To(ContainSubstring("LOCK TABLE ObserverRegistration"))
	g.Expect(downText).To(ContainSubstring("EXISTS (SELECT 1 FROM ObserverRegistration)"))
}

func TestDesiredObservedRepositoryUsesTenantFencesAndSerializedHeadUpdates(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("desired_observed_state.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(strings.Count(text, "organization_id = @organizationID")).To(
		BeNumerically(">=", 10),
	)
	g.Expect(text).To(ContainSubstring("RunTxIso(ctx, pgx.Serializable"))
	g.Expect(text).To(ContainSubstring("FOR UPDATE"))
	g.Expect(text).To(ContainSubstring("EvaluateAdmission"))
	g.Expect(text).To(ContainSubstring("EvaluateGate"))
	g.Expect(text).To(ContainSubstring("VerifyTrustedObservation"))
	g.Expect(text).To(ContainSubstring("ResolveTrustedCampaignObservation"))
	g.Expect(text).To(ContainSubstring("component_instance_id = @componentInstanceID"))
	g.Expect(text).To(ContainSubstring("runtime_state_checksum = @checksum"))
	g.Expect(text).To(ContainSubstring("SELECT id, runtime_state_checksum"))
	g.Expect(text).To(ContainSubstring("runtime_state_checksum = @expectedChecksum"))
	g.Expect(text).To(ContainSubstring("trusted = TRUE"))
	g.Expect(text).To(ContainSubstring("is_current = TRUE"))
	g.Expect(text).To(ContainSubstring("health = 'HEALTHY'"))
	g.Expect(text).To(ContainSubstring("fresh_until >= now()"))
	g.Expect(text).To(ContainSubstring("length(btrim(evidence_reference)) > 0"))
	g.Expect(text).To(ContainSubstring("o.disposition IN ('ACCEPTED', 'CONFLICT')"))
	g.Expect(text).NotTo(ContainSubstring("SELECT DISTINCT ON (o.observer_id)"))
}

func TestDesiredStateAdvanceRevalidatesSuppliedVerifiedGate(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	pending := types.PendingDesiredRevision{
		OrganizationID: uuid.New(), DeploymentUnitID: uuid.New(),
		ComponentInstanceID: uuid.New(), ComponentKey: "api",
		ArtifactDigest: desiredObservedDigest("artifact"),
		ConfigChecksum: desiredObservedDigest("config"), SchemaVersion: "1",
		CapabilityChecksum: desiredObservedDigest("capability"),
		Platform:           "linux/amd64", TopologyChecksum: desiredObservedDigest("topology"),
		ObservationDeadline: now.Add(time.Minute),
	}
	observed := types.ObservedComponentState{
		ID: uuid.New(), OrganizationID: pending.OrganizationID,
		DeploymentUnitID:    pending.DeploymentUnitID,
		ComponentInstanceID: pending.ComponentInstanceID,
		ComponentKey:        pending.ComponentKey, ObserverID: uuid.New(),
		ArtifactDigest: desiredObservedDigest("wrong"),
		ConfigChecksum: pending.ConfigChecksum, SchemaVersion: pending.SchemaVersion,
		CapabilityChecksum: pending.CapabilityChecksum, Platform: pending.Platform,
		TopologyChecksum: pending.TopologyChecksum,
		Health:           types.ObservedHealthHealthy,
		Outcome:          types.ObservationOutcomeComplete, Trusted: true, Current: true,
		StateChecksum: desiredObservedDigest("state"),
	}
	supplied := types.ObservationGateResult{
		Status:        types.ObservationGateStatusVerified,
		ObservationID: observed.ID, ObservationChecksum: observed.StateChecksum,
	}

	_, err := validateVerifiedGate(pending, observed, supplied, now)

	g.Expect(err).To(HaveOccurred())
}

func TestExecutorReportRejectsMissingImmutableLineage(t *testing.T) {
	g := NewWithT(t)

	_, err := RecordExecutorReport(context.Background(), types.ExecutorReport{})

	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())

	_, err = RecordExecutorReport(context.Background(), types.ExecutorReport{
		OrganizationID: uuid.New(), PendingRevisionID: uuid.New(),
		ExecutionID: uuid.New(), Outcome: types.ExecutorOutcomeSucceeded,
		ReportedStateChecksum: "sha256:" + strings.Repeat("A", 64),
	})
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}

func TestPromotionRejectsCallerGateWithoutMatchingRepositoryEvidence(t *testing.T) {
	g := NewWithT(t)
	observationID := uuid.New()
	checksum := desiredObservedDigest("terminal-evidence")

	err := validatePromotionGateHint(
		types.ObservationGateResult{Status: types.ObservationGateStatusFailed},
		types.ObservationGateResult{Status: types.ObservationGateStatusPending},
	)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	err = validatePromotionGateHint(
		types.ObservationGateResult{
			Status: types.ObservationGateStatusFailed, ObservationID: uuid.New(),
		},
		types.ObservationGateResult{
			Status:        types.ObservationGateStatusFailed,
			ObservationID: observationID, ObservationChecksum: checksum,
		},
	)
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	err = validatePromotionGateHint(
		types.ObservationGateResult{
			Status:        types.ObservationGateStatusFailed,
			ObservationID: observationID, ObservationChecksum: checksum,
		},
		types.ObservationGateResult{
			Status:        types.ObservationGateStatusFailed,
			ObservationID: observationID, ObservationChecksum: checksum,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
}

func TestObservationStateChecksumCoversEvidenceReferenceAndComponentScope(t *testing.T) {
	g := NewWithT(t)
	envelope := types.ObservationEnvelope{
		OrganizationID: uuid.New(), ObserverID: uuid.New(),
		DeploymentUnitID: uuid.New(), ComponentInstanceID: uuid.New(),
		ComponentKey: "api", SourceSequence: 1, CapturedAt: time.Now().UTC(),
		EvidenceChecksum:  desiredObservedDigest("evidence"),
		EvidenceReference: "probe://one",
		ArtifactDigest:    desiredObservedDigest("artifact"),
		ConfigChecksum:    desiredObservedDigest("config"), SchemaVersion: "1",
		CapabilityChecksum: desiredObservedDigest("capability"),
		Platform:           "linux/amd64", TopologyChecksum: desiredObservedDigest("topology"),
		Health: types.ObservedHealthHealthy, Outcome: types.ObservationOutcomeComplete,
	}
	original, err := observationStateChecksum(envelope)
	g.Expect(err).NotTo(HaveOccurred())

	changedReference := envelope
	changedReference.EvidenceReference = "probe://two"
	referenceChecksum, err := observationStateChecksum(changedReference)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(referenceChecksum).NotTo(Equal(original))

	changedComponent := envelope
	changedComponent.ComponentInstanceID = uuid.New()
	componentChecksum, err := observationStateChecksum(changedComponent)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(componentChecksum).NotTo(Equal(original))
}

func TestDesiredObservationDeadlineSweepUsesExactAttemptProjection(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("desired_observed_state.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("func SweepExpiredPendingDesiredRevisions("))
	g.Expect(text).To(ContainSubstring("observation_deadline <= clock_timestamp()"))
	g.Expect(text).To(ContainSubstring("FOR UPDATE SKIP LOCKED"))
	g.Expect(text).To(ContainSubstring("finalizeExecutionV2Mutation"))
	g.Expect(text).To(ContainSubstring("getExecutionAttemptForUpdate("))
	g.Expect(text).To(ContainSubstring("projectExecutionV2Terminal("))
	g.Expect(text).NotTo(ContainSubstring("SET status = 'FENCED'"))
	g.Expect(text).NotTo(ContainSubstring("generation = generation + 1"))
}

func TestObservationIngestionAutomaticallyOpensDriftAndMismatchCases(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("desired_observed_state.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("openAutomaticDriftCaseForObservation"))
	g.Expect(text).NotTo(ContainSubstring(
		"inserted && ingested.Disposition == types.ObservationDispositionAccepted",
	))
	g.Expect(text).To(ContainSubstring("openAutomaticDriftCaseTx"))
	g.Expect(text).To(ContainSubstring("reconciliation.ClassifyDriftAt"))
	g.Expect(text).To(ContainSubstring("types.DriftClassExecutorMismatch"))
	g.Expect(text).To(ContainSubstring("types.DriftClassConflict"))
}

func TestDesiredObservedAuditSkipsNoopAndUsesInjectedHook(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	desiredStateID := uuid.New()
	var captured []types.ControlPlaneAuditEventInput
	hook := ControlPlaneAuditAppendHookFunc(func(
		_ context.Context,
		input types.ControlPlaneAuditEventInput,
	) error {
		captured = append(captured, input)
		return nil
	})

	ctx := WithControlPlaneDomainAuditHook(context.Background(), hook)
	err := appendDesiredObservedAudit(ctx, false,
		types.ControlPlaneAuditEventInput{
			OrganizationID: organizationID,
			EventType:      "desired_revision.pending",
			Outcome:        "PENDING",
			DesiredStateID: &desiredStateID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(captured).To(BeEmpty())

	err = appendDesiredObservedAudit(ctx, true,
		types.ControlPlaneAuditEventInput{
			OrganizationID: organizationID,
			EventType:      "desired_revision.pending",
			Outcome:        "PENDING",
			DesiredStateID: &desiredStateID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(captured).To(HaveLen(1))
	g.Expect(captured[0].DesiredStateID).NotTo(BeNil())
	g.Expect(*captured[0].DesiredStateID).To(Equal(desiredStateID))

	auditFailure := errors.New("audit unavailable")
	err = appendDesiredObservedAudit(WithControlPlaneDomainAuditHook(
		context.Background(), ControlPlaneAuditAppendHookFunc(func(
			_ context.Context,
			_ types.ControlPlaneAuditEventInput,
		) error {
			return auditFailure
		})), true, types.ControlPlaneAuditEventInput{
		OrganizationID: organizationID,
		EventType:      "desired_revision.pending",
		Outcome:        "PENDING",
		DesiredStateID: &desiredStateID,
	})
	g.Expect(errors.Is(err, auditFailure)).To(BeTrue())
}

func desiredObservedDigest(value string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}
