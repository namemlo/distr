package deploymentpreflight

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestEvaluateMigrationPreflightChecksBackupSchemaLocksProbesAndAdapter(t *testing.T) {
	g := NewWithT(t)
	input := Input{Migrations: []types.MigrationPreflight{{
		Contract: migrationPreflightContract(),
		Backup:   &types.BackupEvidence{ID: "backup-1", Checksum: preflightChecksum("b"), Verified: true},
		CurrentSchema: types.SchemaState{
			DatabaseResourceKey: "postgres:ledger", Version: "41", Checksum: preflightChecksum("c"),
		},
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
		CurrentSchema: types.SchemaState{
			DatabaseResourceKey: "postgres:ledger", Version: "40", Checksum: preflightChecksum("c"),
		},
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
		CurrentSchema: types.SchemaState{
			DatabaseResourceKey: "postgres:ledger", Version: "41", Checksum: preflightChecksum("c"),
		},
		TargetLockAvailable:      true,
		DatabaseLockAvailable:    true,
		AdapterAvailable:         true,
		PreconditionProbesPassed: true,
	}}}

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

func passingMigrationPreflight() Input {
	return Input{Migrations: []types.MigrationPreflight{{
		Contract: migrationPreflightContract(),
		Backup: &types.BackupEvidence{
			ID: "backup-1", Checksum: preflightChecksum("b"), Verified: true,
		},
		CurrentSchema: types.SchemaState{
			DatabaseResourceKey: "postgres:ledger", Version: "41", Checksum: preflightChecksum("c"),
		},
		TargetLockAvailable: true, DatabaseLockAvailable: true,
		AdapterAvailable: true, PreconditionProbesPassed: true,
	}}}
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
