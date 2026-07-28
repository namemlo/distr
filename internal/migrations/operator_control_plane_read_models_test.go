package migrations

import (
	"os"
	"regexp"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestOperatorControlPlaneMigrationIsIndexesOnlyAndTenantKeysetScoped(t *testing.T) {
	g := NewWithT(t)
	content, err := os.ReadFile("sql/161_operator_control_plane_read_models.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(content)
	normalizedSQL := strings.Join(strings.Fields(sql), " ")

	for _, forbidden := range []string{
		"CREATE TABLE", "CREATE VIEW", "CREATE MATERIALIZED VIEW",
		"ALTER TABLE", "INSERT INTO", "UPDATE ", "DELETE FROM",
	} {
		g.Expect(strings.ToUpper(sql)).NotTo(ContainSubstring(forbidden))
	}

	indexPattern := regexp.MustCompile(`(?is)CREATE\s+(?:UNIQUE\s+)?INDEX\s+\S+\s+ON\s+\S+\s*\(\s*organization_id\b`)
	allIndexPattern := regexp.MustCompile(`(?is)CREATE\s+(?:UNIQUE\s+)?INDEX\s+`)
	g.Expect(indexPattern.FindAllString(sql, -1)).To(HaveLen(len(allIndexPattern.FindAllString(sql, -1))))
	g.Expect(allIndexPattern.FindAllString(sql, -1)).NotTo(BeEmpty())

	for _, keyset := range []string{
		"created_at DESC, id DESC",
		"updated_at DESC, id DESC",
		"evaluated_at DESC, id DESC",
	} {
		g.Expect(normalizedSQL).To(ContainSubstring(keyset))
	}
}

func TestOperatorControlPlaneMigrationCoversEveryReadModelFilterPath(t *testing.T) {
	g := NewWithT(t)
	content, err := os.ReadFile("sql/161_operator_control_plane_read_models.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(content)
	normalizedSQL := strings.Join(strings.Fields(sql), " ")

	for _, indexName := range operatorControlPlaneIndexNames() {
		g.Expect(sql).To(ContainSubstring("CREATE INDEX " + indexName))
	}

	for _, predicate := range []string{
		"WHERE retired_at IS NULL",
		"WHERE status IN ('OPEN', 'ASSIGNED', 'EXCEPTION')",
		"organization_id, status, created_at DESC, id DESC",
		"organization_id, environment_id, created_at DESC, id DESC",
		"organization_id, deployment_unit_id, created_at DESC, id DESC",
		"organization_id, deployment_target_id, created_at DESC, id DESC",
		"organization_id, event_type, created_at DESC, id DESC",
		"organization_id, actor_id, created_at DESC, id DESC",
	} {
		g.Expect(normalizedSQL).To(ContainSubstring(predicate))
	}
}

func TestOperatorControlPlaneMigrationDownDropsOnlyOwnedIndexes(t *testing.T) {
	g := NewWithT(t)
	content, err := os.ReadFile("sql/161_operator_control_plane_read_models.down.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(content)

	for _, indexName := range operatorControlPlaneIndexNames() {
		g.Expect(sql).To(ContainSubstring("DROP INDEX IF EXISTS " + indexName + ";"))
	}
	g.Expect(strings.ToUpper(sql)).NotTo(ContainSubstring("DROP TABLE"))
	g.Expect(strings.Count(sql, "DROP INDEX IF EXISTS ")).To(Equal(len(operatorControlPlaneIndexNames())))
}

func operatorControlPlaneIndexNames() []string {
	return []string{
		"OperatorFleet_component_page",
		"OperatorFleet_customer_unit",
		"OperatorFleet_component_definition",
		"OperatorFleet_assignment_environment",
		"OperatorFleet_open_drift",
		"OperatorRelease_page",
		"OperatorRelease_status_page",
		"OperatorRelease_application_page",
		"OperatorRelease_kind_page",
		"OperatorPlan_page",
		"OperatorPlan_status_page",
		"OperatorPlan_environment_page",
		"OperatorPlan_unit_page",
		"OperatorPlan_release_page",
		"OperatorPlan_campaign_member",
		"OperatorCampaign_run_page",
		"OperatorCampaign_member_scope",
		"OperatorCampaign_member_run_filter",
		"OperatorCampaign_prerequisite_latest",
		"OperatorCampaign_threshold_latest",
		"OperatorExecution_page",
		"OperatorExecution_target_page",
		"OperatorExecution_task_plan",
		"OperatorExecution_task_target",
		"OperatorReconciliation_page",
		"OperatorAudit_type_page",
		"OperatorAudit_actor_page",
	}
}
