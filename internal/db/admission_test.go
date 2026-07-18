package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestPersistAdmissionEvaluationRejectsMismatchedAuthorizationScopeBeforeDatabase(t *testing.T) {
	g := NewWithT(t)
	evaluation := types.AdmissionEvaluation{
		OrganizationID:          uuid.New(),
		DeploymentPlanID:        uuid.New(),
		ActorUserAccountID:      uuid.New(),
		Decision:                types.AdmissionDecisionAdmit,
		SchedulerIdempotencyKey: "scheduler:1",
	}

	_, err := PersistAdmissionEvaluation(
		context.Background(),
		types.PersistAdmissionEvaluationRequest{
			Evaluation: evaluation,
			Authorize: func(
				context.Context,
				types.AdmissionAuthorizationContext,
			) error {
				return nil
			},
			Authorization: types.AdmissionAuthorizationContext{
				OrganizationID:     evaluation.OrganizationID,
				ActorUserAccountID: evaluation.ActorUserAccountID,
				DeploymentPlanID:   uuid.New(),
				Action:             "plan.execute",
				DecisionAt:         time.Now().UTC(),
			},
		},
	)

	g.Expect(err).To(MatchError(ContainSubstring("authorization scope")))
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
