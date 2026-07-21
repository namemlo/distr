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

func TestExecutionControlsCampaignTaskLineageBindsExactTaskIdentity(t *testing.T) {
	upContent, err := os.ReadFile("sql/158_execution_controls.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	upSQL := strings.Join(strings.Fields(string(upContent)), " ")
	for _, required := range []string{
		"ADD CONSTRAINT task_id_plan_target_organization_unique UNIQUE ( id, deployment_plan_id, deployment_target_id, organization_id )",
		"FOREIGN KEY ( task_id, deployment_plan_id, deployment_target_id, organization_id ) REFERENCES Task( id, deployment_plan_id, deployment_target_id, organization_id )",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("campaign task lineage must bind the exact task identity; missing %q", required)
		}
	}

	downContent, err := os.ReadFile("sql/158_execution_controls.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	downSQL := string(downContent)
	childDropAt := strings.Index(downSQL, "DROP TABLE IF EXISTS CampaignMemberTaskExecution")
	taskConstraintDropAt := strings.Index(
		downSQL,
		"DROP CONSTRAINT IF EXISTS task_id_plan_target_organization_unique",
	)
	if childDropAt < 0 || taskConstraintDropAt < 0 || childDropAt > taskConstraintDropAt {
		t.Fatal("downgrade must drop campaign task lineage before its Task identity constraint")
	}
}

func TestExecutionControlsFreezeCampaignRunInitiatorFromExactRevision(t *testing.T) {
	upContent, err := os.ReadFile("sql/158_execution_controls.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	upSQL := strings.Join(strings.Fields(string(upContent)), " ")
	for _, required := range []string{
		"ALTER TABLE DeploymentCampaignRun ADD COLUMN started_by_useraccount_id UUID",
		"UPDATE DeploymentCampaignRun AS campaign_run SET started_by_useraccount_id = campaign_revision.published_by_useraccount_id FROM DeploymentCampaignRevision AS campaign_revision WHERE campaign_revision.id = campaign_run.campaign_revision_id AND campaign_revision.organization_id = campaign_run.organization_id",
		"ALTER COLUMN started_by_useraccount_id SET NOT NULL",
		"ADD CONSTRAINT deploymentcampaignrun_started_by_useraccount_fk FOREIGN KEY (started_by_useraccount_id) REFERENCES UserAccount(id) ON UPDATE NO ACTION ON DELETE RESTRICT",
		"CREATE FUNCTION deploymentcampaignrun_started_by_immutable_guard()",
		"CREATE TRIGGER DeploymentCampaignRun_started_by_immutable BEFORE UPDATE OF started_by_useraccount_id ON DeploymentCampaignRun",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("campaign run initiator migration missing %q", required)
		}
	}

	downContent, err := os.ReadFile("sql/158_execution_controls.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	downSQL := string(downContent)
	normalizedDownSQL := strings.Join(strings.Fields(downSQL), " ")
	for _, required := range []string{
		"DeploymentCampaignRun, DeploymentCampaignRevision",
		"campaign_run.started_by_useraccount_id IS DISTINCT FROM campaign_revision.published_by_useraccount_id",
		"refusing migration 158 rollback while campaign run initiator evidence differs from its publication actor",
	} {
		if !strings.Contains(normalizedDownSQL, required) {
			t.Fatalf("campaign run initiator downgrade guard missing %q", required)
		}
	}
	controlDropAt := strings.Index(downSQL, "DROP TABLE IF EXISTS CampaignMemberTaskExecution")
	actorTriggerDropAt := strings.Index(
		downSQL,
		"DROP TRIGGER IF EXISTS DeploymentCampaignRun_started_by_immutable",
	)
	actorConstraintDropAt := strings.Index(
		downSQL,
		"DROP CONSTRAINT IF EXISTS deploymentcampaignrun_started_by_useraccount_fk",
	)
	actorColumnDropAt := strings.Index(
		downSQL,
		"DROP COLUMN IF EXISTS started_by_useraccount_id",
	)
	if controlDropAt < 0 || actorTriggerDropAt < 0 || actorConstraintDropAt < 0 ||
		actorColumnDropAt < 0 ||
		!(controlDropAt < actorTriggerDropAt && actorTriggerDropAt < actorConstraintDropAt &&
			actorConstraintDropAt < actorColumnDropAt) {
		t.Fatal("downgrade must remove control lineage before campaign run initiator identity")
	}
}

func TestExecutionControlsAllowSamePlanTargetAcrossExecutionOccurrences(t *testing.T) {
	upContent, err := os.ReadFile("sql/158_execution_controls.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	upSQL := strings.Join(strings.Fields(string(upContent)), " ")
	upSteps := []string{
		"ADD COLUMN execution_occurrence_id UUID",
		"UPDATE Task SET execution_occurrence_id = deployment_plan_id WHERE execution_occurrence_id IS NULL",
		"ALTER COLUMN execution_occurrence_id SET NOT NULL",
		"DROP CONSTRAINT task_plan_target_unique",
		"ADD CONSTRAINT task_plan_target_occurrence_unique UNIQUE ( deployment_plan_id, deployment_plan_target_id, execution_occurrence_id )",
	}
	previousAt := -1
	for _, step := range upSteps {
		at := strings.Index(upSQL, step)
		if at < 0 {
			t.Fatalf("execution occurrence migration missing %q", step)
		}
		if at <= previousAt {
			t.Fatalf("execution occurrence migration step %q is out of order", step)
		}
		previousAt = at
	}

	downContent, err := os.ReadFile("sql/158_execution_controls.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	downSQL := strings.Join(strings.Fields(string(downContent)), " ")
	for _, required := range []string{
		"GROUP BY deployment_plan_id, deployment_plan_target_id HAVING count(*) > 1",
		"refusing migration 158 rollback while duplicate plan-target execution occurrences exist",
	} {
		if !strings.Contains(downSQL, required) {
			t.Fatalf("execution occurrence downgrade guard missing %q", required)
		}
	}
	downSteps := []string{
		"DROP CONSTRAINT IF EXISTS task_plan_target_occurrence_unique",
		"ADD CONSTRAINT task_plan_target_unique UNIQUE (deployment_plan_id, deployment_plan_target_id)",
		"DROP COLUMN IF EXISTS execution_occurrence_id",
	}
	previousAt = -1
	for _, step := range downSteps {
		at := strings.Index(downSQL, step)
		if at < 0 {
			t.Fatalf("execution occurrence downgrade missing %q", step)
		}
		if at <= previousAt {
			t.Fatalf("execution occurrence downgrade step %q is out of order", step)
		}
		previousAt = at
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
