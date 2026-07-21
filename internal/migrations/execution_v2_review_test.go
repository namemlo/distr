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
				"Task", "ExecutionAttempt", "ExecutionFence", "ExecutionIntent", "ExecutionEvent",
			},
		},
		{
			path: "sql/158_execution_controls.down.sql",
			tables: []string{
				"ExecutionAttempt", "ExecutionCancelRequest", "ExecutionStatusQuery",
				"ExecutionReconciliationEvent", "CampaignMemberTaskExecution",
				"ExecutionCampaignControlHandoff",
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

func TestExecutionControlsPersistExactCampaignTaskHandoff(t *testing.T) {
	content, err := os.ReadFile("sql/158_execution_controls.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	for _, required := range []string{
		"CREATE TABLE CampaignMemberTaskExecution",
		"campaign_member_run_id",
		"campaignmembertaskexecution_task_fk",
		"campaignmembertaskexecution_member_fk",
		"UNIQUE (organization_id, task_id)",
		"CREATE TABLE ExecutionCampaignControlHandoff",
		"execution_cancel_request_id",
		"executioncampaigncontrolhandoff_cancel_unique",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("campaign execution handoff schema missing %q", required)
		}
	}
}

func TestExecutionControlsDowngradeLocksCampaignLineageParents(t *testing.T) {
	content, err := os.ReadFile("sql/158_execution_controls.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	lockAt := strings.Index(sql, "LOCK TABLE")
	checkAt := strings.Index(sql, "IF EXISTS")
	dropAt := strings.Index(sql, "DROP TABLE IF EXISTS ExecutionCampaignControlHandoff")
	alterAt := strings.Index(sql, "ALTER TABLE DeploymentCampaignMemberRun")
	if lockAt < 0 || checkAt < 0 || dropAt < 0 || alterAt < 0 ||
		!(lockAt < checkAt && checkAt < dropAt && dropAt < alterAt) {
		t.Fatal("campaign lineage parents must be locked before refusal, child drop and parent alter")
	}
	for _, table := range []string{"Task", "DeploymentCampaignMemberRun"} {
		if !strings.Contains(sql[lockAt:checkAt], table) {
			t.Fatalf("campaign lineage downgrade lock is missing parent %s", table)
		}
	}
}

func TestExecutionV2DowngradeRefusesRetainedV2Tasks(t *testing.T) {
	content, err := os.ReadFile("sql/157_external_execution_protocol_v2.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	lockAt := strings.Index(sql, "LOCK TABLE")
	checkAt := strings.Index(sql, "protocol_version = 'v2'")
	dropAt := strings.Index(sql, "DROP COLUMN IF EXISTS protocol_version")
	if lockAt < 0 || checkAt < 0 || dropAt < 0 || !(lockAt < checkAt && checkAt < dropAt) {
		t.Fatal("downgrade must lock Task and reject retained v2 tasks before dropping protocol_version")
	}
}
