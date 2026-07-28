package db

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestOperatorReconciliationQueryIsTenantScopedFilteredAndKeysetPaginated(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	query := strings.Join(strings.Fields(operatorReconciliationListSQL), " ")
	for _, fragment := range []string{
		"drift.organization_id = @organizationId",
		"@status::text IS NULL OR drift.status = @status",
		"@drift::text IS NULL OR @drift = ANY(drift.classes)",
		"@environmentId::uuid IS NULL OR assignment.environment_id = @environmentId",
		"@deploymentTargetId::uuid IS NULL",
		"OR unit.deployment_target_id = @deploymentTargetId",
		"drift.created_at < @afterCreatedAt",
		"drift.created_at = @afterCreatedAt AND drift.id < @afterId",
		"count(*) OVER() AS total_count",
		"ORDER BY created_at DESC, id DESC",
		"LIMIT @limit",
	} {
		g.Expect(query).To(ContainSubstring(fragment))
	}

	g.Expect(operatorReconciliationListSQL).To(ContainSubstring("DeploymentUnitSubscriber"))
	g.Expect(operatorReconciliationListSQL).To(ContainSubstring("@authorizedCustomerIds::uuid[]"))
	g.Expect(operatorReconciliationListSQL).To(ContainSubstring("@authorizedEnvironmentIds::uuid[]"))
	g.Expect(operatorReconciliationListSQL).To(ContainSubstring("@authorizedDeploymentUnitIds::uuid[]"))
	g.Expect(operatorReconciliationListSQL).To(ContainSubstring("@authorizedComponentIds::uuid[]"))
}

func TestOperatorReconciliationQueryClassifiesPartialStaleAndUnknownEvidence(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	for _, fragment := range []string{
		"observation.outcome = 'PARTIAL' THEN 'PARTIAL'",
		"observation.outcome = 'UNKNOWN' THEN 'UNKNOWN'",
		"observation.fresh_until <= @evaluatedAt THEN 'STALE'",
		"ELSE 'CURRENT'",
	} {
		g.Expect(operatorReconciliationListSQL).To(ContainSubstring(fragment))
	}
}

func TestOperatorReconciliationDetailIsTenantAndScopeFiltered(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	for _, fragment := range []string{
		"drift.organization_id = @organizationId",
		"drift.id = @reconciliationId",
		"@organizationWide",
		"@authorizedCustomerIds::uuid[]",
		"@authorizedEnvironmentIds::uuid[]",
		"@authorizedDeploymentUnitIds::uuid[]",
		"@authorizedComponentIds::uuid[]",
		"observation.evidence_checksum",
	} {
		g.Expect(operatorReconciliationDetailSQL).To(ContainSubstring(fragment))
	}
}
