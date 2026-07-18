package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/scheduling"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestAdmissionMigrationPinsAppendOnlyEvidenceAndIdempotency(t *testing.T) {
	g := NewWithT(t)
	up := readAdmissionMigration(t, "152_deployment_admission_overrides.up.sql")
	down := readAdmissionMigration(t, "152_deployment_admission_overrides.down.sql")

	for _, expected := range []string{
		"CREATE TABLE EmergencyOverride",
		"CREATE TABLE AdmissionEvaluation",
		"plan_revision",
		"plan_checksum",
		"campaign_revision",
		"effective_policy_checksum",
		"policy_version_ids",
		"calendar_version_ids",
		"freeze_revision_ids",
		"approval_request_revision",
		"temporal_evidence",
		"material_checksum",
		"decision_checksum",
		"scheduler_idempotency_key",
		"AdmissionEvaluation_scheduler_idempotency",
		"admission_append_only",
		"distr.deployment_registry_deletion_reason",
	} {
		g.Expect(up).To(ContainSubstring(expected))
	}
	g.Expect(up).To(ContainSubstring("TIMESTAMPTZ"))
	g.Expect(up).To(ContainSubstring("pg_column_size"))
	g.Expect(up).To(ContainSubstring("ORGANIZATION_RETENTION"))

	g.Expect(down).To(ContainSubstring("LOCK TABLE"))
	g.Expect(down).To(ContainSubstring("ACCESS EXCLUSIVE MODE"))
	g.Expect(down).To(ContainSubstring("downgrade crossing 152 is forbidden"))
}

func TestResolveAdmissionPersistenceReplayIsIdempotentOnlyForExactDecision(t *testing.T) {
	g := NewWithT(t)
	existing := types.AdmissionEvaluation{
		ID:               uuid.New(),
		DecisionChecksum: admissionTestChecksum("first"),
	}

	replayed, err := resolveAdmissionPersistenceReplay(existing, admissionTestChecksum("first"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayed.ID).To(Equal(existing.ID))

	_, err = resolveAdmissionPersistenceReplay(existing, admissionTestChecksum("changed"))
	g.Expect(err).To(MatchError(ContainSubstring("idempotency key")))
}

func TestCreateEmergencyOverrideRepositoryValidationRejectsDuplicateApprovals(t *testing.T) {
	g := NewWithT(t)
	approvalID := uuid.New()
	err := validateCreateEmergencyOverrideRequest(types.CreateEmergencyOverrideRequest{
		OrganizationID:     uuid.New(),
		DeploymentPlanID:   uuid.New(),
		ActorUserAccountID: uuid.New(),
		Accelerations: []types.EmergencyAcceleration{{
			GateKey:                types.AdmissionGateMaintenanceWait,
			MaxAccelerationSeconds: 300,
		}},
		Reason:             "critical customer recovery",
		ApprovalRequestIDs: []uuid.UUID{approvalID, approvalID},
		ExpiresAt:          time.Now().UTC().Add(time.Hour),
		IdempotencyKey:     "incident:42",
		Authorize: func(
			context.Context,
			types.AdmissionAuthorizationContext,
		) error {
			return nil
		},
	})

	g.Expect(err).To(MatchError(ContainSubstring("duplicate approval")))
}

func TestAdmitDeploymentPlanRequiresTrustedGateEvidenceRepository(t *testing.T) {
	g := NewWithT(t)
	err := validateAdmitDeploymentPlanRequest(types.AdmitDeploymentPlanRequest{
		OrganizationID:          uuid.New(),
		DeploymentPlanID:        uuid.New(),
		ActorUserAccountID:      uuid.New(),
		SchedulerIdempotencyKey: "scheduler:1",
		Authorize: func(
			context.Context,
			types.AdmissionAuthorizationContext,
		) error {
			return nil
		},
	}, nil)

	g.Expect(err).To(MatchError(ContainSubstring("trusted gate evidence")))
}

func TestSealedAdmissionEvaluationRejectsForgedAdmit(t *testing.T) {
	g := NewWithT(t)
	request := sealedAdmissionTestRequest()
	evaluation, err := scheduling.EvaluateAdmission(context.Background(), request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(evaluation.Decision).To(Equal(types.AdmissionDecisionBlock))
	evaluation.Decision = types.AdmissionDecisionAdmit

	_, err = verifySealedAdmissionEvaluation(
		context.Background(),
		sealedAdmissionEvaluation{
			AdmissionRequest: request,
			Evaluation:       evaluation,
		},
	)

	g.Expect(err).To(MatchError(ContainSubstring("recomputed material and decision checksums")))
}

func TestEmergencyOverrideReplayPinsApprovalIDsAndEvidence(t *testing.T) {
	g := NewWithT(t)
	approvalID := uuid.New()
	planID := uuid.New()
	organizationID := uuid.New()
	actorID := uuid.New()
	snapshot := types.AdmissionPlanSnapshot{
		PlanRevision: 1,
		Plan: types.DeploymentPlan{
			ID:                      planID,
			CanonicalChecksum:       admissionTestChecksum("plan"),
			EffectivePolicyChecksum: admissionTestChecksum("policy"),
		},
	}
	request := types.CreateEmergencyOverrideRequest{
		OrganizationID:     organizationID,
		DeploymentPlanID:   planID,
		ActorUserAccountID: actorID,
		Accelerations: []types.EmergencyAcceleration{{
			GateKey:                types.AdmissionGateMaintenanceWait,
			MaxAccelerationSeconds: 300,
		}},
		Reason:             "critical customer recovery",
		ApprovalRequestIDs: []uuid.UUID{approvalID},
		ExpiresAt:          time.Now().UTC().Add(time.Hour),
		IdempotencyKey:     "incident:42",
	}
	evidence := []types.EmergencyOverrideApprovalEvidence{{
		RequestID:       approvalID,
		RequestRevision: 2,
		RequestChecksum: admissionTestChecksum("approval"),
		Eligible:        true,
		State:           types.ApprovalRequestStateApproved,
	}}
	override := types.EmergencyOverride{
		ID:                      uuid.New(),
		CreatedAt:               time.Now().UTC(),
		OrganizationID:          organizationID,
		DeploymentPlanID:        planID,
		PlanRevision:            snapshot.PlanRevision,
		PlanChecksum:            snapshot.Plan.CanonicalChecksum,
		EffectivePolicyChecksum: snapshot.Plan.EffectivePolicyChecksum,
		Accelerations:           request.Accelerations,
		Reason:                  request.Reason,
		ActorUserAccountID:      actorID,
		ApprovalEvidence:        evidence,
		ExpiresAt:               request.ExpiresAt,
		IdempotencyKey:          request.IdempotencyKey,
	}
	override.Checksum = scheduling.EmergencyOverrideChecksum(override)

	g.Expect(emergencyOverrideMatchesRequest(override, request, snapshot, evidence)).To(BeTrue())

	changedEvidence := append([]types.EmergencyOverrideApprovalEvidence(nil), evidence...)
	changedEvidence[0].RequestRevision++
	g.Expect(emergencyOverrideMatchesRequest(
		override,
		request,
		snapshot,
		changedEvidence,
	)).To(BeFalse())

	request.ApprovalRequestIDs = []uuid.UUID{uuid.New()}
	g.Expect(emergencyOverrideMatchesRequest(override, request, snapshot, evidence)).To(BeFalse())
}

func sealedAdmissionTestRequest() types.AdmissionRequest {
	approvalID := uuid.New()
	return types.AdmissionRequest{
		OrganizationID: uuid.New(),
		Plan: types.AdmissionPlanEvidence{
			ID:              uuid.New(),
			Revision:        1,
			Checksum:        admissionTestChecksum("plan"),
			Schema:          types.AdmissionRequiredPlanSchemaV2,
			ProtocolVersion: types.AdmissionRequiredProtocolV2,
		},
		EffectivePolicy: types.EffectivePolicy{
			VersionIDs:            []uuid.UUID{uuid.New()},
			Checksum:              admissionTestChecksum("policy"),
			SubscriberSetChecksum: admissionTestChecksum("subscribers"),
		},
		Approval: types.AdmissionApprovalEvidence{
			RequestID:               approvalID,
			RequestRevision:         1,
			SubjectChecksum:         admissionTestChecksum("plan"),
			EffectivePolicyChecksum: admissionTestChecksum("policy"),
			SubscriberSetChecksum:   admissionTestChecksum("subscribers"),
			Evaluation: types.ApprovalEvaluation{
				RequestID: approvalID,
				State:     types.ApprovalRequestStatePending,
				Eligible:  false,
			},
		},
		EvaluatedAt: time.Now().UTC(),
	}
}

func readAdmissionMigration(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "migrations", "sql", name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func admissionTestChecksum(seed string) string {
	if seed == "changed" {
		return "sha256:1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	}
	return "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}
