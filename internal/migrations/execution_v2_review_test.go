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
	}
	for _, value := range required {
		if !strings.Contains(sql, value) {
			t.Fatalf("migration missing %q", value)
		}
	}
}
