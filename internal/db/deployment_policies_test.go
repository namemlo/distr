package db

import (
	"os"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentPolicyMigrationDefinesImmutableOrganizationScopedResources(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile("../migrations/sql/149_deployment_policies.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := strings.ToLower(string(up))

	for _, fragment := range []string{
		"create table deploymentpolicy (",
		"create table deploymentpolicyversion (",
		"create table deploymentpolicybinding (",
		"unique (id, organization_id)",
		"references deploymentpolicy(id, organization_id)",
		"references deploymentpolicyversion(id, organization_id)",
		"document jsonb not null",
		"canonical_checksum text not null",
		"sha256(canonical_payload)",
		"state in ('draft', 'published')",
		"deployment_policy_version_published_immutable",
		"'distr.deployment_policy_deletion_reason'",
		"scope_kind in (",
		"'organization'",
		"'customer'",
		"'environment'",
		"'deployment_unit'",
		"'component'",
		"'campaign'",
		"binding_role in ('owner', 'subscriber')",
		"before insert or update or delete on deploymentpolicybinding",
		"alter table deploymentplan",
		"effective_policy jsonb",
		"effective_policy_checksum text",
		"subscriber_set_checksum text",
	} {
		g.Expect(sql).To(ContainSubstring(fragment))
	}

	organizationRepository, err := os.ReadFile("organization.go")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(organizationRepository)).To(ContainSubstring(
		"'distr.deployment_policy_deletion_reason'",
	))
}

func TestDeploymentPolicyMigrationDowngradeRefusesEvidenceLoss(t *testing.T) {
	g := NewWithT(t)
	down, err := os.ReadFile("../migrations/sql/149_deployment_policies.down.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := strings.ToLower(string(down))

	g.Expect(sql).To(ContainSubstring("lock table"))
	g.Expect(sql).To(ContainSubstring("downgrade crossing 149 is forbidden"))
	g.Expect(sql).To(ContainSubstring("exists (select 1 from deploymentpolicyversion)"))
}

func TestValidatePolicyBindingRequestEnforcesExactScopeRoleContract(t *testing.T) {
	g := NewWithT(t)
	request := types.PolicyBindingRequest{
		OrganizationID:         uuid.New(),
		PolicyVersionID:        uuid.New(),
		ScopeKind:              types.DeploymentPolicyBindingScopeCustomer,
		ScopeID:                uuid.New(),
		Role:                   types.DeploymentPolicyBindingRoleSubscriber,
		CreatedByUserAccountID: uuid.New(),
	}
	g.Expect(validatePolicyBindingRequest(request)).To(Succeed())

	request.ScopeKind = types.DeploymentPolicyBindingScopeDeploymentUnit
	g.Expect(validatePolicyBindingRequest(request)).To(MatchError(ContainSubstring(
		"subscriber bindings require customer scope",
	)))

	request.Role = types.DeploymentPolicyBindingRoleOwner
	request.ScopeKind = types.DeploymentPolicyBindingScopeCampaign
	g.Expect(validatePolicyBindingRequest(request)).To(MatchError(ContainSubstring(
		"campaign bindings are unavailable until campaign resources are present",
	)))
}
