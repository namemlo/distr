package deploymentpreflight

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestEvaluateMigrationPreflightChecksBackupSchemaLocksProbesAndAdapter(t *testing.T) {
	g := NewWithT(t)
	input := Input{Migrations: []types.MigrationPreflight{{
		Contract:                 migrationPreflightContract(),
		Backup:                   &types.BackupEvidence{ID: "backup-1", Checksum: preflightChecksum("b"), Verified: true},
		CurrentSchema:            types.SchemaState{Version: "41", Checksum: preflightChecksum("c")},
		TargetLockAvailable:      true,
		DatabaseLockAvailable:    true,
		AdapterAvailable:         true,
		PreconditionProbesPassed: true,
	}}}

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
	input := Input{Migrations: []types.MigrationPreflight{{
		Contract: contract,
		Backup: &types.BackupEvidence{
			ID: "backup-1", Checksum: preflightChecksum("b"), Verified: false,
		},
		CurrentSchema: types.SchemaState{Version: "40", Checksum: preflightChecksum("c")},
	}}}

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
	input := Input{Migrations: []types.MigrationPreflight{{
		Contract: migrationPreflightContract(),
		Backup: &types.BackupEvidence{
			ID: "backup-1", Checksum: "sha256:not-a-digest", Verified: true,
		},
		CurrentSchema:            types.SchemaState{Version: "41", Checksum: preflightChecksum("c")},
		TargetLockAvailable:      true,
		DatabaseLockAvailable:    true,
		AdapterAvailable:         true,
		PreconditionProbesPassed: true,
	}}}

	checks := Evaluate(input)

	g.Expect(failedCheckKeys(checks)).To(ContainElement("migration_backup:ledger.042"))
}

func migrationPreflightContract() types.MigrationContract {
	return types.MigrationContract{
		ID: "ledger.042", Checksum: preflightChecksum("a"),
		ComponentKey: "ledger", DatabaseResourceKey: "postgres:ledger",
		ExpectedSourceVersion: "41", ResultingVersion: "42",
		BackupRequired: true, BackupVerifier: "backup-verifier:v1",
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
