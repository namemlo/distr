package db

import (
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestOperatorPlanScopeFilterRequiresBoundedTenantVisibility(t *testing.T) {
	t.Parallel()

	organizationID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	environmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	unitID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	err := validateOperatorPlanScopeFilter(types.OperatorScopeFilter{
		OrganizationID: organizationID, DecisionAt: time.Now().UTC(),
		EnvironmentIDs: []uuid.UUID{environmentID}, DeploymentUnitIDs: []uuid.UUID{unitID},
		CustomerIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	err = validateOperatorPlanScopeFilter(types.OperatorScopeFilter{
		OrganizationID: organizationID, DecisionAt: time.Now().UTC(), OrganizationWide: true,
		CustomerIDs: []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
	})
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
}

func TestOperatorPlanScopeFilterRejectsEmptyAndMalformedScope(t *testing.T) {
	t.Parallel()

	organizationID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	err := validateOperatorPlanScopeFilter(types.OperatorScopeFilter{
		OrganizationID: organizationID, DecisionAt: time.Now().UTC(),
	})
	NewWithT(t).Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
	err = validateOperatorPlanScopeFilter(types.OperatorScopeFilter{
		OrganizationID: organizationID, DecisionAt: time.Now().UTC(),
		EnvironmentIDs: []uuid.UUID{uuid.Nil},
	})
	NewWithT(t).Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
}

func TestOperatorPlanListSQLFiltersScopeBeforeStableKeysetPagination(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	for _, fragment := range []string{
		"dp.organization_id = @organizationID",
		"dp.status = @status",
		"dp.environment_id = @environmentID",
		"dp.deployment_unit_id = @deploymentUnitID",
		"dp.release_bundle_id = @productReleaseID",
		"scope.customer_organization_id = ANY(@customerIDs::uuid[])",
		"subscriber.customer_organization_id = ANY(@customerIDs::uuid[])",
		"instance.component_definition_id = ANY(@componentIDs::uuid[])",
		"revision.deployment_campaign_draft_id = ANY(@campaignIDs::uuid[])",
		"(dp.created_at, dp.id) <",
		"ORDER BY dp.created_at DESC, dp.id DESC",
		"LIMIT @limitPlusOne",
	} {
		g.Expect(operatorPlanListSQL).To(ContainSubstring(fragment))
	}

	g.Expect(operatorPlanListSQL).NotTo(ContainSubstring("OFFSET"))
}

func TestOperatorPlanDetailIdentitySQLHidesForeignAndOutOfScopePlans(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	g.Expect(operatorPlanDetailIdentitySQL).To(ContainSubstring("dp.id = @planID"))
	g.Expect(operatorPlanDetailIdentitySQL).To(ContainSubstring("dp.organization_id = @organizationID"))
	g.Expect(operatorPlanDetailIdentitySQL).To(ContainSubstring("@organizationWide"))
	g.Expect(operatorPlanDetailIdentitySQL).To(ContainSubstring("instance.component_definition_id"))
	g.Expect(operatorPlanDetailIdentitySQL).To(ContainSubstring("revision.deployment_campaign_draft_id"))
	g.Expect(operatorPlanDetailIdentitySQL).NotTo(ContainSubstring("OR dp.organization_id"))
}

func TestOperatorPlanDetailQueriesAreConstantAndTenantPlanBound(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	g.Expect(operatorPlanAdditionalDetailQueryCount).To(BeNumerically("<=", 6))
	for _, query := range []string{
		operatorPlanApprovalSQL,
		operatorPlanAdmissionSQL,
		operatorPlanIntentEvidenceSQL,
		operatorPlanAuditEvidenceSQL,
	} {
		g.Expect(query).To(ContainSubstring("organization_id = @organizationID"))
		g.Expect(query).To(ContainSubstring("@planID"))
	}
}
