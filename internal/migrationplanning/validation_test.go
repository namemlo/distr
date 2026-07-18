package migrationplanning

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestValidateMigrationContractAcceptsBoundedRetrySafeContract(t *testing.T) {
	g := NewWithT(t)

	issues := ValidateMigrationContract(migrationContractFixture())

	g.Expect(issues).To(BeEmpty())
}

func TestValidateMigrationContractRejectsUnsafeBackupAndRetryContract(t *testing.T) {
	g := NewWithT(t)
	contract := migrationContractFixture()
	contract.Checksum = "mutable-tag"
	contract.ArtifactDigest = "migration:latest"
	contract.BackupVerifier = ""
	contract.IdempotencyKey = ""
	contract.PostconditionProbes = nil

	issues := ValidateMigrationContract(contract)

	g.Expect(validationCodes(issues)).To(ContainElements(
		"migration_checksum_invalid",
		"migration_artifact_invalid",
		"backup_verifier_required",
		"idempotency_key_required",
		"postcondition_probe_required",
	))
}

func TestValidateMigrationContractRejectsForwardOnlyReverseShortcut(t *testing.T) {
	g := NewWithT(t)
	contract := migrationContractFixture()
	contract.Reversibility = types.MigrationReversibilityForwardOnly
	contract.RequiresForwardFix = false
	contract.RecoveryProcedureReference = ""

	issues := ValidateMigrationContract(contract)

	g.Expect(validationCodes(issues)).To(ContainElements(
		"forward_fix_required",
		"recovery_procedure_required",
	))
}

func TestValidatePreviousReleaseCompatibilityBlocksForwardOnlySchema(t *testing.T) {
	g := NewWithT(t)

	issues := ValidatePreviousReleaseCompatibility(
		types.SchemaState{
			ComponentKey: "ledger", DatabaseResourceKey: "postgres:ledger",
			Version: "41", Checksum: checksum("1"),
		},
		types.PlannedState{
			ComponentKey: "ledger", DatabaseResourceKey: "postgres:ledger", SchemaState: "42",
			SchemaChecksum: checksum("2"), ForwardOnly: true,
		},
	)

	g.Expect(validationCodes(issues)).To(ContainElement("previous_release_forward_only"))
}

func TestValidatePreviousReleaseCompatibilityRequiresExactKnownState(t *testing.T) {
	g := NewWithT(t)
	current := types.SchemaState{
		ComponentKey: "ledger", DatabaseResourceKey: "postgres:ledger",
		Version: "41", Checksum: checksum("1"),
	}
	planned := types.PlannedState{
		ComponentKey: "ledger", DatabaseResourceKey: "postgres:ledger",
		SchemaState: "41", SchemaChecksum: checksum("1"),
	}
	g.Expect(ValidatePreviousReleaseCompatibility(current, planned)).To(BeEmpty())

	cases := map[string]func(*types.SchemaState, *types.PlannedState){
		"missing current version": func(current *types.SchemaState, _ *types.PlannedState) {
			current.Version = ""
		},
		"missing planned checksum": func(_ *types.SchemaState, planned *types.PlannedState) {
			planned.SchemaChecksum = ""
		},
		"unknown current checksum": func(current *types.SchemaState, _ *types.PlannedState) {
			current.Checksum = "unknown"
		},
		"version mismatch only": func(_ *types.SchemaState, planned *types.PlannedState) {
			planned.SchemaState = "42"
		},
		"checksum mismatch only": func(_ *types.SchemaState, planned *types.PlannedState) {
			planned.SchemaChecksum = checksum("2")
		},
		"database resource mismatch": func(_ *types.SchemaState, planned *types.PlannedState) {
			planned.DatabaseResourceKey = "postgres:other"
		},
		"missing database resource": func(_ *types.SchemaState, planned *types.PlannedState) {
			planned.DatabaseResourceKey = ""
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			testCurrent, testPlanned := current, planned
			mutate(&testCurrent, &testPlanned)
			NewWithT(t).Expect(
				ValidatePreviousReleaseCompatibility(testCurrent, testPlanned),
			).NotTo(BeEmpty())
		})
	}
}

func TestValidateMigrationContractBoundsDependencyIDsAndCount(t *testing.T) {
	g := NewWithT(t)
	contract := migrationContractFixture()
	contract.DependsOn = make([]string, 65)
	for index := range contract.DependsOn {
		contract.DependsOn[index] = "ledger.dep." + strings.Repeat("x", index+1)
	}
	contract.DependsOn[0] = "INVALID DEPENDENCY"

	issues := ValidateMigrationContract(contract)

	g.Expect(validationCodes(issues)).To(ContainElements(
		"migration_dependency_limit",
		"migration_dependency_invalid",
	))
}

func migrationContractFixture() types.MigrationContract {
	return types.MigrationContract{
		ID: "ledger.042", Checksum: checksum("a"), ComponentKey: "ledger",
		DatabaseResourceKey: "postgres:ledger", ExpectedSourceVersion: "41",
		ExpectedSourceChecksum: checksum("1"),
		ResultingVersion:       "42", Phase: types.MigrationPhaseExpand,
		LockType: "exclusive", LockTimeoutSeconds: 30, OperationalImpact: "brief_write_lock",
		BackupRequired: true, BackupVerifier: "backup-verifier:v1",
		PreconditionProbes: []types.MigrationProbe{{
			Name: "source schema", Reference: "probe:ledger:source:v1",
			ExpectedChecksum: checksum("b"),
		}},
		PostconditionProbes: []types.MigrationProbe{{
			Name: "target schema", Reference: "probe:ledger:target:v1",
			ExpectedChecksum: checksum("c"),
		}},
		RetryClass: types.MigrationRetrySafe, IdempotencyKey: "ledger.042",
		Reversibility:                    types.MigrationReversibilityReversible,
		PreviousApplicationCompatibility: ">=1.8.0",
		RecoveryProcedureReference:       "recovery:ledger.042:v1",
		AdapterType:                      "database.postgres.v1",
		ArtifactDigest: "registry.example.com/migrations/ledger@sha256:" +
			strings.Repeat("d", 64),
		EvidenceRetentionDays: 90,
	}
}

func validationCodes(issues []types.ValidationIssue) []string {
	result := make([]string, 0, len(issues))
	for _, issue := range issues {
		result = append(result, issue.Code)
	}
	return result
}

func checksum(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}
