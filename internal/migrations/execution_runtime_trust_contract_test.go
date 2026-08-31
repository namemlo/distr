package migrations

import (
	"os"
	"strings"
	"testing"
)

const (
	runtimeTrustUpMigration   = "sql/167_execution_runtime_trust_contract.up.sql"
	runtimeTrustDownMigration = "sql/167_execution_runtime_trust_contract.down.sql"
)

func readRuntimeTrustMigration(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func normalizedMigrationSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

func TestExecutionRuntimeTrustMigrationPreservesLegacyRowsAndRequiresCompleteV3Shape(t *testing.T) {
	sql := readRuntimeTrustMigration(t, runtimeTrustUpMigration)
	normalized := normalizedMigrationSQL(sql)

	legacyDefaultAt := strings.Index(
		normalized,
		"ADD COLUMN runtime_contract_version TEXT NOT NULL DEFAULT 'legacy-v2'",
	)
	v3DefaultAt := strings.Index(
		normalized,
		"ALTER COLUMN runtime_contract_version SET DEFAULT 'v3'",
	)
	if legacyDefaultAt < 0 || v3DefaultAt < 0 || legacyDefaultAt > v3DefaultAt {
		t.Fatal("migration must preserve existing attempts as legacy before defaulting new attempts to v3")
	}
	if strings.Contains(normalized, "UPDATE ExecutionAttempt") {
		t.Fatal("migration must not fabricate runtime trust bindings for retained attempts")
	}

	for _, field := range []string{
		"expected_observed_state_revision",
		"expected_observed_state_checksum",
		"expected_current_image_digest",
		"expected_current_config_checksum",
		"expected_platform",
		"intent_caller",
		"intent_audience",
	} {
		if !strings.Contains(normalized, "AND "+field+" IS NULL") {
			t.Fatalf("legacy runtime contract must leave %s unset", field)
		}
		if !strings.Contains(normalized, "AND "+field+" IS NOT NULL") {
			t.Fatalf("v3 runtime contract must require %s", field)
		}
	}

	for _, required := range []string{
		"runtime_contract_version IN ('legacy-v2', 'v3')",
		"expected_observed_state_revision > 0",
		"expected_observed_state_checksum ~ '^sha256:[0-9a-f]{64}$'",
		"expected_current_image_digest ~ '^sha256:[0-9a-f]{64}$'",
		"expected_current_config_checksum ~ '^sha256:[0-9a-f]{64}$'",
		"expected_platform IN ('linux/amd64', 'linux/arm64')",
		"length(intent_caller) BETWEEN 1 AND 512",
		"length(intent_audience) BETWEEN 1 AND 512",
		"CREATE FUNCTION execution_attempt_runtime_contract_immutable_guard()",
		"CREATE TRIGGER ExecutionAttempt_runtime_contract_immutable",
		"execution attempt runtime trust contract is immutable",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("runtime contract migration missing %q", required)
		}
	}
}

func TestExecutionRuntimeTrustMigrationCreatesBoundedAppendOnlyAttemptEvidence(t *testing.T) {
	sql := readRuntimeTrustMigration(t, runtimeTrustUpMigration)
	normalized := normalizedMigrationSQL(sql)

	for _, required := range []string{
		"CREATE TABLE ExecutionRuntimeEvidence",
		"event_identity UUID NOT NULL",
		"schema_version = 'distr.execution-runtime-evidence/v1'",
		"expected_observed_state_revision BIGINT NOT NULL",
		"expected_observed_state_checksum TEXT NOT NULL",
		"pre_execution_image_digest TEXT NOT NULL",
		"pre_execution_config_checksum TEXT NOT NULL",
		"result_image_digest TEXT NOT NULL",
		"result_config_checksum TEXT NOT NULL",
		"platform IN ('linux/amd64', 'linux/arm64')",
		"length(caller_identity) BETWEEN 1 AND 512",
		"length(audience) BETWEEN 1 AND 512",
		"health_status IN ('HEALTHY', 'UNHEALTHY')",
		"ADD CONSTRAINT executionattempt_runtime_evidence_lineage_unique UNIQUE",
		"result_checksum ~ '^sha256:[0-9a-f]{64}$'",
		"evidence_checksum ~ '^sha256:[0-9a-f]{64}$'",
		"canonical_checksum ~ '^sha256:[0-9a-f]{64}$'",
		"CONSTRAINT executionruntimeevidence_attempt_unique UNIQUE (execution_attempt_id)",
		"CONSTRAINT executionruntimeevidence_event_identity_unique UNIQUE (organization_id, event_identity)",
		"ADD CONSTRAINT executionintent_attempt_org_checksum_unique UNIQUE (execution_attempt_id, organization_id, checksum)",
		"FOREIGN KEY ( execution_attempt_id, organization_id, intent_checksum ) REFERENCES ExecutionIntent( execution_attempt_id, organization_id, checksum )",
		"CREATE TRIGGER ExecutionRuntimeEvidence_append_only",
		"CREATE TRIGGER ExecutionRuntimeEvidence_no_truncate",
		"EXECUTE FUNCTION execution_protocol_v2_append_only_guard()",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("runtime evidence migration missing %q", required)
		}
	}

	exactAttemptBinding := "FOREIGN KEY ( execution_attempt_id, organization_id, deployment_target_id, execution_id, attempt_number, step_key ) REFERENCES ExecutionAttempt( id, organization_id, deployment_target_id, execution_id, attempt_number, step_key )"
	if !strings.Contains(normalized, exactAttemptBinding) {
		t.Fatal("runtime evidence must bind the exact tenant, target, attempt, execution and step identity")
	}
}

func TestExecutionRuntimeTrustDowngradeLocksAndRefusesRetainedV3Evidence(t *testing.T) {
	sql := readRuntimeTrustMigration(t, runtimeTrustDownMigration)
	normalized := normalizedMigrationSQL(sql)

	lockAt := strings.Index(normalized, "LOCK TABLE")
	checkAt := strings.Index(normalized, "IF EXISTS")
	dropEvidenceAt := strings.Index(normalized, "DROP TABLE IF EXISTS ExecutionRuntimeEvidence")
	dropIntentConstraintAt := strings.Index(
		normalized,
		"DROP CONSTRAINT IF EXISTS executionintent_attempt_org_checksum_unique",
	)
	dropAttemptColumnsAt := strings.Index(
		normalized,
		"DROP COLUMN IF EXISTS runtime_contract_version",
	)
	if lockAt < 0 || checkAt < 0 || dropEvidenceAt < 0 || dropIntentConstraintAt < 0 ||
		dropAttemptColumnsAt < 0 ||
		!(lockAt < checkAt && checkAt < dropEvidenceAt &&
			dropEvidenceAt < dropIntentConstraintAt &&
			dropIntentConstraintAt < dropAttemptColumnsAt) {
		t.Fatal("downgrade must lock and refuse retained evidence before removing dependent schema")
	}
	lockClause := normalized[lockAt:checkAt]
	for _, table := range []string{
		"ExecutionAttempt",
		"ExecutionIntent",
		"ExecutionRuntimeEvidence",
	} {
		if !strings.Contains(lockClause, table) {
			t.Fatalf("downgrade lock is missing %s", table)
		}
	}
	for _, required := range []string{
		"IN ACCESS EXCLUSIVE MODE",
		"FROM ExecutionAttempt WHERE runtime_contract_version = 'v3'",
		"FROM ExecutionRuntimeEvidence",
		"refusing migration 167 rollback while v3 runtime trust contracts or evidence exist",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("runtime trust downgrade missing %q", required)
		}
	}
}
