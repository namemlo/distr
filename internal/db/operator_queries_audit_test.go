package db

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestOperatorAuditQueryIsTenantScopedAndUsesExactTypedCorrelation(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	for _, fragment := range []string{
		"event.organization_id = @organizationId",
		"@action::text IS NULL OR event.event_type = @action",
		"@actorId::uuid IS NULL OR event.actor_id = @actorId",
		"@from::timestamptz IS NULL OR event.created_at >= @from",
		"@to::timestamptz IS NULL OR event.created_at < @to",
		"subject.correlation_kind = @subjectType",
		"subject.subject_id = @subjectId",
		"subject.organization_id = event.organization_id",
		"count(*) OVER() AS total_count",
		"ORDER BY created_at DESC, id DESC",
		"LIMIT @limit",
	} {
		g.Expect(operatorAuditSearchSQL).To(ContainSubstring(fragment))
	}
}

func TestOperatorAuditSearchUsesOnlySafeMetadataAndNeverPayloads(t *testing.T) {
	t.Parallel()

	lowerSQL := strings.ToLower(operatorAuditSearchSQL)
	g := NewWithT(t)
	g.Expect(lowerSQL).To(ContainSubstring("event.event_type ilike"))
	g.Expect(lowerSQL).To(ContainSubstring("event.outcome ilike"))
	g.Expect(lowerSQL).NotTo(ContainSubstring("payload::text"))
	g.Expect(lowerSQL).NotTo(ContainSubstring("endpoint_reference"))
	g.Expect(lowerSQL).NotTo(ContainSubstring("credential"))
}

func TestOperatorAuditQueryFiltersAuthorizedScopesBeforePagination(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	for _, fragment := range []string{
		"@organizationWide",
		"@authorizedCustomerIds::uuid[]",
		"@authorizedEnvironmentIds::uuid[]",
		"@authorizedDeploymentUnitIds::uuid[]",
		"@authorizedComponentIds::uuid[]",
		"@authorizedCampaignIds::uuid[]",
		"ControlPlaneAuditEventSubject authorized_subject",
	} {
		g.Expect(operatorAuditSearchSQL).To(ContainSubstring(fragment))
	}
}

func TestOperatorAuditDetailIsTenantScopedAndReturnsTypedEvidenceOnly(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	for _, fragment := range []string{
		"event.organization_id = @organizationId",
		"event.id = @auditEventId",
		"ControlPlaneAuditEventSubject authorized_subject",
		"@organizationWide",
		"release_checksum",
		"deployment_plan_checksum",
		"observation_checksum",
		"reconciliation_checksum",
	} {
		g.Expect(operatorAuditDetailSQL).To(ContainSubstring(fragment))
	}
	g.Expect(strings.ToLower(operatorAuditDetailSQL)).NotTo(ContainSubstring("endpoint_reference"))
	g.Expect(strings.ToLower(operatorAuditDetailSQL)).NotTo(ContainSubstring("credential"))
}
