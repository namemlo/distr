package db

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/migrationplanning"
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
	g.Expect(sql).To(ContainSubstring(
		"FOR EACH ROW EXECUTE FUNCTION deployment_plan_v2_child_seal_guard();",
	))
	g.Expect(sql).To(ContainSubstring(
		"FOR EACH STATEMENT EXECUTE FUNCTION deployment_plan_v2_no_truncate_guard();",
	))
	g.Expect(sql).NotTo(ContainSubstring("deployment_plan_v2_append_only_guard"))
	g.Expect(strings.ToLower(sql)).NotTo(ContainSubstring("password"))
}

func TestProjectTargetPlanMigrationsRetainsCompleteStructuredContract(t *testing.T) {
	g := NewWithT(t)
	contract := types.MigrationContract{
		ID: "ledger.042", ComponentKey: "ledger", DatabaseResourceKey: "postgres:ledger",
		ExpectedSourceVersion: "41", ExpectedSourceChecksum: "sha256:" + strings.Repeat("1", 64),
		ResultingVersion: "42", ResultingSchemaChecksum: "sha256:" + strings.Repeat("2", 64),
		Phase: types.MigrationPhaseExpand, LockType: "exclusive", LockTimeoutSeconds: 30,
		OperationalImpact: "brief write lock", BackupRequired: true,
		BackupVerifier: "backup-verifier:v1",
		PreconditionProbes: []types.MigrationProbe{{
			Name: "source", Reference: "probe:ledger:source:v1",
			ExpectedChecksum: "sha256:" + strings.Repeat("3", 64),
		}},
		PostconditionProbes: []types.MigrationProbe{{
			Name: "result", Reference: "probe:ledger:result:v1",
			ExpectedChecksum: "sha256:" + strings.Repeat("4", 64),
		}},
		RetryClass: types.MigrationRetrySafe, IdempotencyKey: "ledger.042",
		Reversibility:                    types.MigrationReversibilityReversible,
		PreviousApplicationCompatibility: ">=1.8.0",
		RecoveryProcedureReference:       "recovery:ledger.042:v1",
		AdapterType:                      "database.migrate",
		ArtifactDigest:                   "registry.example.com/migrations/ledger@sha256:" + strings.Repeat("5", 64),
		EvidenceRetentionDays:            90,
	}
	contract.Checksum, _ = migrationplanning.CanonicalMigrationContractChecksum(contract)
	graph, err := migrationplanning.ExpandMigrationGraph(contract, types.TargetPlanGraph{})
	g.Expect(err).NotTo(HaveOccurred())

	migrations, err := projectTargetPlanMigrations(
		[]types.MigrationContract{contract},
		graph,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(migrations).To(HaveLen(1))
	g.Expect(migrations[0].MigrationContract()).To(Equal(contract))
	g.Expect(migrations[0].ApplyStepKey).To(Equal("migration:ledger.042:apply"))
	g.Expect(migrations[0].ValidateStepKey).To(Equal("migration:ledger.042:validate"))

	reverseGraph := types.TargetPlanGraph{Steps: []types.TargetPlanStep{{
		StepKey: "recovery:ledger.042:reverse", ActionType: "database.migration.reverse",
	}}}
	reverse, err := projectTargetPlanMigrations([]types.MigrationContract{contract}, reverseGraph)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reverse).To(HaveLen(1))
	g.Expect(reverse[0].ApplyStepKey).To(Equal("recovery:ledger.042:reverse"))
	g.Expect(reverse[0].ValidateStepKey).To(Equal("recovery:ledger.042:reverse"))
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

func TestMigrationStateUsesExactStructuredResultAndForwardFixFact(t *testing.T) {
	g := NewWithT(t)
	state, checksumValue, forwardOnly, err := migrationState(
		[]types.MigrationDeclaration{{Key: "ledger.042", Order: 10}},
		[]types.MigrationContract{{
			ID: "ledger.042", ResultingVersion: "42",
			ResultingSchemaChecksum: "sha256:" + strings.Repeat("f", 64),
			RequiresForwardFix:      true,
		}},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(state).To(Equal("42"))
	g.Expect(checksumValue).To(Equal("sha256:" + strings.Repeat("f", 64)))
	g.Expect(forwardOnly).To(BeTrue())
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
