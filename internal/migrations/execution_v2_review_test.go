package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestExecutionControlsMigrationKeepsUnknownCompletionConsistentAndAttemptScoped(t *testing.T) {
	content, err := os.ReadFile("sql/158_execution_controls.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	required := []string{
		"executionattempt_completion_check",
		"status IN ('PENDING', 'CLAIMED', 'RUNNING', 'UNKNOWN')",
		"UNIQUE (organization_id, execution_attempt_id, idempotency_key)",
		"requested_ttl_seconds",
		"executionstatusquery_id_org_execution_attempt_unique",
		"executionreconciliationevent_query_attempt_fk",
	}
	for _, value := range required {
		if !strings.Contains(sql, value) {
			t.Fatalf("migration missing %q", value)
		}
	}
}

func TestExecutionV2DowngradesLockAllOwnedEvidenceBeforeRefusalChecks(t *testing.T) {
	tests := []struct {
		path   string
		tables []string
	}{
		{
			path: "sql/157_external_execution_protocol_v2.down.sql",
			tables: []string{
				"ExecutionAttempt", "ExecutionFence", "ExecutionIntent", "ExecutionEvent",
			},
		},
		{
			path: "sql/158_execution_controls.down.sql",
			tables: []string{
				"ExecutionAttempt", "ExecutionCancelRequest", "ExecutionStatusQuery",
				"ExecutionReconciliationEvent",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			content, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			sql := string(content)
			lockAt := strings.Index(sql, "LOCK TABLE")
			checkAt := strings.Index(sql, "IF EXISTS")
			if lockAt < 0 || checkAt < 0 || lockAt > checkAt {
				t.Fatal("downgrade must lock evidence tables before retained-data checks")
			}
			if !strings.Contains(sql, "ACCESS EXCLUSIVE MODE") {
				t.Fatal("downgrade evidence lock must exclude concurrent writers")
			}
			for _, table := range tc.tables {
				if !strings.Contains(sql[lockAt:checkAt], table) {
					t.Fatalf("downgrade lock is missing %s", table)
				}
				if !strings.Contains(sql[checkAt:], "FROM "+table) {
					t.Fatalf("downgrade refusal check is missing %s", table)
				}
			}
		})
	}
}
