package migrations

import (
	"os"
	"strings"
	"testing"
)

const (
	runtimeChecksumIdentitiesUpMigration   = "sql/172_execution_runtime_checksum_identities.up.sql"
	runtimeChecksumIdentitiesDownMigration = "sql/172_execution_runtime_checksum_identities.down.sql"
)

func readRuntimeChecksumIdentitiesMigration(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return normalizedMigrationSQL(string(content))
}

func TestMigration172PreservesLegacyShapesAndDefaultsNewAttemptsToV4(t *testing.T) {
	sql := readRuntimeChecksumIdentitiesMigration(
		t, runtimeChecksumIdentitiesUpMigration,
	)

	if strings.Contains(sql, "UPDATE ExecutionAttempt") {
		t.Fatal("migration must not fabricate checksum identities for retained attempts")
	}
	for _, required := range []string{
		"ADD COLUMN runtime_manifest_checksum TEXT",
		"ADD COLUMN desired_service_config_checksum TEXT",
		"ADD COLUMN expected_current_service_config_checksum TEXT",
		"runtime_contract_version IN ('legacy-v2', 'v3', 'v4')",
		"ALTER COLUMN runtime_contract_version SET DEFAULT 'v4'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 172 is missing %q", required)
		}
	}

	for _, field := range []string{
		"runtime_manifest_checksum",
		"desired_service_config_checksum",
		"expected_current_service_config_checksum",
	} {
		if strings.Count(sql, "AND "+field+" IS NULL") < 2 {
			t.Fatalf("legacy-v2 and v3 attempts must leave %s unset", field)
		}
		if !strings.Contains(sql, "AND "+field+" IS NOT NULL") {
			t.Fatalf("v4 attempts must require %s", field)
		}
		if !strings.Contains(sql, field+" ~ '^sha256:[0-9a-f]{64}$'") {
			t.Fatalf("v4 attempts must validate %s as sha256", field)
		}
	}
}

func TestMigration172WidensProtectedHistorySourceVersion(t *testing.T) {
	sql := readRuntimeChecksumIdentitiesMigration(
		t, runtimeChecksumIdentitiesUpMigration,
	)

	lockAt := strings.Index(
		sql,
		"LOCK TABLE ExecutionAttempt, ExecutionRuntimeEvidence, ProtectedHistoryArtifact",
	)
	constraintAt := strings.Index(sql, "source_schema_version BETWEEN 138 AND 172")
	if lockAt < 0 || constraintAt < 0 || lockAt >= constraintAt {
		t.Fatal("migration 172 must lock protected history before widening its source version")
	}
}

func TestMigration172MakesNewAttemptChecksumIdentitiesImmutable(t *testing.T) {
	sql := readRuntimeChecksumIdentitiesMigration(
		t, runtimeChecksumIdentitiesUpMigration,
	)

	for _, required := range []string{
		"DROP TRIGGER ExecutionAttempt_runtime_contract_immutable ON ExecutionAttempt",
		"CREATE OR REPLACE FUNCTION execution_attempt_runtime_contract_immutable_guard()",
		"CREATE TRIGGER ExecutionAttempt_runtime_contract_immutable",
		"NEW.runtime_manifest_checksum IS DISTINCT FROM OLD.runtime_manifest_checksum",
		"NEW.desired_service_config_checksum IS DISTINCT FROM OLD.desired_service_config_checksum",
		"NEW.expected_current_service_config_checksum IS DISTINCT FROM OLD.expected_current_service_config_checksum",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 172 immutable guard is missing %q", required)
		}
	}
}

func TestMigration172SQLVersionsPhysicalRuntimeEvidence(t *testing.T) {
	sql := readRuntimeChecksumIdentitiesMigration(
		t, runtimeChecksumIdentitiesUpMigration,
	)

	for _, required := range []string{
		"ADD COLUMN pre_execution_service_config_checksum TEXT",
		"ADD COLUMN result_service_config_checksum TEXT",
		"'distr.execution-runtime-evidence/v1', 'distr.execution-runtime-evidence/v2'",
		"schema_version = 'distr.execution-runtime-evidence/v1' AND pre_execution_service_config_checksum IS NULL AND result_service_config_checksum IS NULL",
		"schema_version = 'distr.execution-runtime-evidence/v2' AND pre_execution_service_config_checksum IS NOT NULL",
		"pre_execution_service_config_checksum ~ '^sha256:[0-9a-f]{64}$'",
		"result_service_config_checksum IS NOT NULL",
		"result_service_config_checksum ~ '^sha256:[0-9a-f]{64}$'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 172 runtime evidence shape is missing %q", required)
		}
	}
}

func TestMigration172DowngradeLocksAndRefusesV4DataBeforeDroppingColumns(t *testing.T) {
	sql := readRuntimeChecksumIdentitiesMigration(
		t, runtimeChecksumIdentitiesDownMigration,
	)

	lockAt := strings.Index(
		sql,
		"LOCK TABLE ExecutionAttempt, ExecutionRuntimeEvidence, ProtectedHistoryArtifact",
	)
	checkAt := strings.Index(sql, "IF EXISTS")
	protectedHistoryCheckAt := strings.Index(sql, "source_schema_version > 171")
	protectedHistoryConstraintAt := strings.Index(
		sql,
		"source_schema_version BETWEEN 138 AND 171",
	)
	dropAt := strings.Index(sql, "DROP COLUMN runtime_manifest_checksum")
	if lockAt < 0 || checkAt < 0 || protectedHistoryCheckAt < 0 ||
		protectedHistoryConstraintAt < 0 || dropAt < 0 ||
		!(lockAt < checkAt && checkAt < protectedHistoryCheckAt &&
			protectedHistoryCheckAt < protectedHistoryConstraintAt &&
			protectedHistoryConstraintAt < dropAt) {
		t.Fatal("migration 172 downgrade must lock and refuse retained v4 data before dropping columns")
	}
	for _, required := range []string{
		"runtime_contract_version = 'v4'",
		"schema_version = 'distr.execution-runtime-evidence/v2'",
		"pre_execution_service_config_checksum IS NOT NULL",
		"result_service_config_checksum IS NOT NULL",
		"source_schema_version > 171",
		"schema-172 protected history exists",
		"source_schema_version BETWEEN 138 AND 171",
		"refusing migration 172 rollback while v4 runtime checksum contracts or evidence exist",
		"ALTER COLUMN runtime_contract_version SET DEFAULT 'v3'",
		"schema_version = 'distr.execution-runtime-evidence/v1'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 172 downgrade is missing %q", required)
		}
	}
}
