package deploymentpreflight

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestEvaluateMigrationPreflightChecksBackupSchemaLocksProbesAndAdapter(t *testing.T) {
	g := NewWithT(t)
	input := withPlanMigrations(Input{Migrations: []types.MigrationPreflight{{
		Contract: migrationPreflightContract(),
		Backup:   &types.BackupEvidence{ID: "backup-1", Checksum: preflightChecksum("b"), Verified: true},
		CurrentSchema: types.SchemaState{
			DatabaseResourceKey: "postgres:ledger", Version: "41", Checksum: preflightChecksum("c"),
		},
		TargetLockAvailable:      true,
		DatabaseLockAvailable:    true,
		AdapterAvailable:         true,
		PreconditionProbesPassed: true,
	}}})

	checks := Evaluate(input)

	g.Expect(failedCheckKeys(checks)).To(BeEmpty())
	g.Expect(checkKeys(checks)).To(ContainElements(
		"migration_backup:ledger.042",
		"migration_schema:ledger.042",
		"migration_target_lock:ledger.042",
		"migration_database_lock:ledger.042",
		"migration_adapter:ledger.042",
		"migration_probes:ledger.042",
	))
}

func TestEvaluateMigrationPreflightFailsClosedBeforeMutation(t *testing.T) {
	g := NewWithT(t)
	contract := migrationPreflightContract()
	input := withPlanMigrations(Input{Migrations: []types.MigrationPreflight{{
		Contract: contract,
		Backup: &types.BackupEvidence{
			ID: "backup-1", Checksum: preflightChecksum("b"), Verified: false,
		},
		CurrentSchema: types.SchemaState{
			DatabaseResourceKey: "postgres:ledger", Version: "40", Checksum: preflightChecksum("c"),
		},
	}}})

	checks := Evaluate(input)

	g.Expect(failedCheckKeys(checks)).To(ContainElements(
		"migration_backup:ledger.042",
		"migration_schema:ledger.042",
		"migration_target_lock:ledger.042",
		"migration_database_lock:ledger.042",
		"migration_adapter:ledger.042",
		"migration_probes:ledger.042",
	))
}

func TestEvaluateMigrationPreflightRejectsMalformedVerifiedBackupChecksum(t *testing.T) {
	g := NewWithT(t)
	input := withPlanMigrations(Input{Migrations: []types.MigrationPreflight{{
		Contract: migrationPreflightContract(),
		Backup: &types.BackupEvidence{
			ID: "backup-1", Checksum: "sha256:not-a-digest", Verified: true,
		},
		CurrentSchema: types.SchemaState{
			DatabaseResourceKey: "postgres:ledger", Version: "41", Checksum: preflightChecksum("c"),
		},
		TargetLockAvailable:      true,
		DatabaseLockAvailable:    true,
		AdapterAvailable:         true,
		PreconditionProbesPassed: true,
	}}})

	checks := Evaluate(input)

	g.Expect(failedCheckKeys(checks)).To(ContainElement("migration_backup:ledger.042"))
}

func TestEvaluateMigrationPreflightRejectsMissingOrUnboundedBackupIdentity(t *testing.T) {
	for name, backupID := range map[string]string{
		"missing":   "",
		"malformed": "bad backup id",
		"too long":  strings.Repeat("a", 257),
	} {
		t.Run(name, func(t *testing.T) {
			input := passingMigrationPreflight()
			input.Migrations[0].Backup.ID = backupID

			checks := Evaluate(input)

			NewWithT(t).Expect(failedCheckKeys(checks)).To(
				ContainElement("migration_backup:ledger.042"),
			)
		})
	}
}

func TestEvaluateMigrationPreflightRequiresExactSourceSchemaChecksum(t *testing.T) {
	g := NewWithT(t)
	input := passingMigrationPreflight()
	input.Migrations[0].CurrentSchema.Checksum = preflightChecksum("d")

	checks := Evaluate(input)

	g.Expect(failedCheckKeys(checks)).To(ContainElement("migration_schema:ledger.042"))
}

func TestEvaluateMigrationPreflightRequiresExactPlanEvidenceCoverage(t *testing.T) {
	second := passingMigrationPreflight().Migrations[0]
	second.Contract.ID = "ledger.043"
	second.Contract.Checksum = preflightChecksum("d")
	base := passingMigrationPreflight()
	base.Migrations = append(base.Migrations, second)
	base = withPlanMigrations(base)

	exact := Evaluate(base)
	NewWithT(t).Expect(failedCheckKeys(exact)).NotTo(
		ContainElement("migration_evidence_coverage"),
	)

	cases := map[string]func(*Input){
		"missing": func(input *Input) {
			input.Migrations = input.Migrations[:1]
		},
		"duplicate": func(input *Input) {
			input.Migrations[1] = input.Migrations[0]
		},
		"extra": func(input *Input) {
			extra := input.Migrations[0]
			extra.Contract.ID = "ledger.999"
			input.Migrations = append(input.Migrations, extra)
		},
		"contract drift": func(input *Input) {
			input.Migrations[0].Contract.AdapterType = "database.other.v1"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := base
			input.Migrations = append([]types.MigrationPreflight(nil), base.Migrations...)
			mutate(&input)

			checks := Evaluate(input)

			NewWithT(t).Expect(failedCheckKeys(checks)).To(
				ContainElement("migration_evidence_coverage"),
			)
		})
	}
}

func passingMigrationPreflight() Input {
	return withPlanMigrations(Input{Migrations: []types.MigrationPreflight{{
		Contract: migrationPreflightContract(),
		Backup: &types.BackupEvidence{
			ID: "backup-1", Checksum: preflightChecksum("b"), Verified: true,
		},
		CurrentSchema: types.SchemaState{
			DatabaseResourceKey: "postgres:ledger", Version: "41", Checksum: preflightChecksum("c"),
		},
		TargetLockAvailable: true, DatabaseLockAvailable: true,
		AdapterAvailable: true, PreconditionProbesPassed: true,
	}}})
}

func withPlanMigrations(input Input) Input {
	input.Plan.Migrations = make([]types.DeploymentPlanMigration, len(input.Migrations))
	for index, migration := range input.Migrations {
		contract := migration.Contract
		input.Plan.Migrations[index] = types.DeploymentPlanMigration{
			MigrationID: contract.ID, ContractChecksum: contract.Checksum,
			ComponentKey: contract.ComponentKey, DatabaseResourceKey: contract.DatabaseResourceKey,
			ExpectedSourceVersion:            contract.ExpectedSourceVersion,
			ExpectedSourceChecksum:           contract.ExpectedSourceChecksum,
			ResultingVersion:                 contract.ResultingVersion,
			Phase:                            contract.Phase,
			DependsOn:                        append([]string(nil), contract.DependsOn...),
			LockType:                         contract.LockType,
			LockTimeoutSeconds:               contract.LockTimeoutSeconds,
			OperationalImpact:                contract.OperationalImpact,
			BackupRequired:                   contract.BackupRequired,
			BackupVerifier:                   contract.BackupVerifier,
			RetryClass:                       contract.RetryClass,
			IdempotencyKey:                   contract.IdempotencyKey,
			Reversibility:                    contract.Reversibility,
			PreviousApplicationCompatibility: contract.PreviousApplicationCompatibility,
			RecoveryProcedureReference:       contract.RecoveryProcedureReference,
			RequiresForwardFix:               contract.RequiresForwardFix,
			AdapterType:                      contract.AdapterType,
			ArtifactDigest:                   contract.ArtifactDigest,
			PreconditionProbes:               append([]types.MigrationProbe(nil), contract.PreconditionProbes...),
			PostconditionProbes:              append([]types.MigrationProbe(nil), contract.PostconditionProbes...),
			EvidenceRetentionDays:            contract.EvidenceRetentionDays,
		}
	}
	return input
}

func migrationPreflightContract() types.MigrationContract {
	return types.MigrationContract{
		ID: "ledger.042", Checksum: preflightChecksum("a"),
		ComponentKey: "ledger", DatabaseResourceKey: "postgres:ledger",
		ExpectedSourceVersion: "41", ExpectedSourceChecksum: preflightChecksum("c"),
		ResultingVersion: "42",
		BackupRequired:   true, BackupVerifier: "backup-verifier:v1",
		AdapterType: "database.postgres.v1",
	}
}

func preflightChecksum(character string) string {
	result := "sha256:"
	for i := 0; i < 64; i++ {
		result += character
	}
	return result
}
