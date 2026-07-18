package db

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestMigration147DefinesStructuredMigrationEvidence(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile("../migrations/sql/147_structured_migration_plans.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)

	for _, fragment := range []string{
		"CREATE TABLE DeploymentPlanMigration",
		"created_at",
		"step_input_checksum",
		"retry_class",
		"cancellation_behavior",
		"observation_requirement",
		"target_lock_key",
		"database_lock_key",
		"resulting_schema_checksum",
		"backup_required",
		"backup_verifier",
		"depends_on",
		"lock_type",
		"lock_timeout_seconds",
		"operational_impact",
		"previous_application_compatibility",
		"adapter_type",
		"artifact_digest",
		"evidence_retention_days",
		"DeploymentPlanMigration_append_only",
	} {
		g.Expect(sql).To(ContainSubstring(fragment))
	}
	g.Expect(strings.ToLower(sql)).NotTo(ContainSubstring("password"))
}

func TestCanonicalMigrationFreezesResultingSchemaChecksum(t *testing.T) {
	g := NewWithT(t)
	resultingChecksum := "sha256:" + strings.Repeat("f", 64)

	payload, err := canonicalizeDeploymentPlan(types.DeploymentPlan{
		Migrations: []types.DeploymentPlanMigration{{
			MigrationID:             "ledger.042",
			ResultingSchemaChecksum: resultingChecksum,
		}},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).To(ContainSubstring(
		`"resultingSchemaChecksum":"` + resultingChecksum + `"`,
	))
}

func TestMigration147DownRefusesToDiscardEvidence(t *testing.T) {
	g := NewWithT(t)
	down, err := os.ReadFile("../migrations/sql/147_structured_migration_plans.down.sql")
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(string(down)).To(ContainSubstring("refusing migration 147 rollback"))
	g.Expect(string(down)).To(ContainSubstring("ACCESS EXCLUSIVE"))
}

func TestDeploymentPlanStepsForInsertRetainsMigrationExecutionContract(t *testing.T) {
	g := NewWithT(t)
	source := types.TargetPlanStep{
		StepKey: "migration:ledger.042:apply", Name: "Apply ledger.042",
		ActionType: "database.migration.apply", InputBindings: json.RawMessage(`{"migrationId":"ledger.042"}`),
		TargetLockKey: "target:ledger", DatabaseLockKey: "database:postgres:ledger",
		RetryClass: "safe", CancellationBehavior: "cooperative",
		ExpectedInputChecksum:  "sha256:" + strings.Repeat("a", 64),
		ObservationRequirement: "resulting schema observation",
	}
	payload, err := json.Marshal(types.TargetDeploymentPlanCanonical{
		Graph: types.TargetPlanGraph{Steps: []types.TargetPlanStep{source}},
	})
	g.Expect(err).NotTo(HaveOccurred())

	steps, err := deploymentPlanStepsForInsert(types.DeploymentPlan{
		PlanSchema:       types.TargetDeploymentPlanSchemaV2,
		CanonicalPayload: payload,
		Steps: []types.DeploymentPlanStep{{
			StepKey: source.StepKey,
		}},
	})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(steps).To(HaveLen(1))
	g.Expect(steps[0].StepInputChecksum).To(Equal(source.ExpectedInputChecksum))
	g.Expect(steps[0].RetryClass).To(Equal("safe"))
	g.Expect(steps[0].CancellationBehavior).To(Equal("cooperative"))
	g.Expect(steps[0].ObservationRequirement).To(Equal("resulting schema observation"))
	g.Expect(steps[0].TargetLockKey).To(Equal("target:ledger"))
	g.Expect(steps[0].DatabaseLockKey).To(Equal("database:postgres:ledger"))
}

func TestDeploymentPlanStepsForInsertRejectsMalformedV2CanonicalGraph(t *testing.T) {
	g := NewWithT(t)

	_, err := deploymentPlanStepsForInsert(types.DeploymentPlan{
		PlanSchema:       types.TargetDeploymentPlanSchemaV2,
		CanonicalPayload: []byte(`{"graph":`),
		Steps:            []types.DeploymentPlanStep{{StepKey: "migration:ledger.042:apply"}},
	})

	g.Expect(err).To(MatchError(ContainSubstring("canonical graph")))
}

func TestLegacyCanonicalPlanOmitsEmptyMigrationExecutionMetadata(t *testing.T) {
	g := NewWithT(t)
	plan := types.DeploymentPlan{Steps: []types.DeploymentPlanStep{{
		StepKey: "deploy", InputBindings: map[string]any{}, Included: true,
	}}}

	payload, err := canonicalizeDeploymentPlan(plan)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(payload).NotTo(ContainSubstring("stepInputChecksum"))
	g.Expect(payload).NotTo(ContainSubstring("retryClass"))
	g.Expect(payload).NotTo(ContainSubstring("databaseLockKey"))
}
