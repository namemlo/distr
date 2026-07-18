package db

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentPolicyMigrationRequiresDraftFirstAuditedPublication(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile("../migrations/sql/149_deployment_policies.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := strings.ToLower(string(up))

	for _, fragment := range []string{
		"if tg_op = 'insert' then",
		"deployment policy versions must be inserted as drafts",
		"before insert or update or delete on deploymentpolicyversion",
		"new.state is distinct from old.state",
		"old.state = 'draft'",
		"new.state = 'published'",
		"new.published_by_useraccount_id is not null",
		"new.published_at is not null",
		"new.document is not distinct from old.document",
		"new.canonical_checksum is not distinct from",
		"new.canonical_payload is not distinct from",
		"invalid deployment policy version lifecycle transition from % to %",
	} {
		g.Expect(sql).To(ContainSubstring(fragment))
	}
}

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
		"check (coalesce((",
		"effective_policy is not null",
		"deployment_plan_policy_evidence_immutable",
		"before update of",
		"deployment policy binding scope does not belong to organization",
		"from customerorganization customer",
		"from environment scoped_environment",
		"from deploymentunit deployment_unit",
		"from componentdefinition component",
		"deploymentpolicy_organization_order",
		"deploymentpolicyversion_organization_policy_order",
		"deploymentpolicybinding_organization_order",
		"created_at desc",
		"id desc",
	} {
		g.Expect(sql).To(ContainSubstring(fragment))
	}

	organizationRepository, err := os.ReadFile("organization.go")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(organizationRepository)).To(ContainSubstring(
		"'distr.deployment_policy_deletion_reason'",
	))
}

func TestDeploymentPolicyRepositoryListsAreBoundedAndVersionsUseSummaries(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_policies.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring(`" LIMIT @fetchLimit"`))
	g.Expect(text).To(ContainSubstring(`"fetchLimit":      limit + 1`))
	g.Expect(strings.Count(text, "return listDeploymentPolicyEntities(")).To(Equal(3))

	summaryStart := strings.Index(text, "const deploymentPolicyVersionSummaryOutputExpr")
	summaryEnd := strings.Index(text[summaryStart:], "const deploymentPolicyBindingOutputExpr")
	g.Expect(summaryStart).To(BeNumerically(">=", 0))
	g.Expect(summaryEnd).To(BeNumerically(">", 0))
	summaryExpression := text[summaryStart : summaryStart+summaryEnd]
	g.Expect(summaryExpression).NotTo(ContainSubstring("version.document"))
	g.Expect(summaryExpression).NotTo(ContainSubstring("version.canonical_payload"))
}

func TestDeploymentPolicyMigrationDowngradeRefusesEvidenceLoss(t *testing.T) {
	g := NewWithT(t)
	down, err := os.ReadFile("../migrations/sql/149_deployment_policies.down.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := strings.ToLower(string(down))

	g.Expect(sql).To(ContainSubstring("lock table"))
	g.Expect(sql).To(ContainSubstring("downgrade crossing 149 is forbidden"))
	g.Expect(sql).To(ContainSubstring("exists (select 1 from deploymentpolicyversion)"))
	g.Expect(sql).To(ContainSubstring(
		"drop index deploymentpolicybinding_organization_order",
	))
	for _, field := range []string{
		"deployment_unit_id is not null",
		"effective_policy is not null",
		"effective_policy_checksum is not null",
		"subscriber_set_checksum is not null",
	} {
		g.Expect(sql).To(ContainSubstring(field))
	}
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

func TestDeploymentPolicyCursorIsOpaqueBoundedAndResourceScoped(t *testing.T) {
	g := NewWithT(t)
	parentID := uuid.New()
	cursor := deploymentPolicyCursor{
		Version:   deploymentPolicyCursorVersion,
		Resource:  deploymentPolicyCursorResourceVersions,
		ParentID:  parentID,
		CreatedAt: time.Date(2026, time.July, 18, 1, 2, 3, 0, time.UTC),
		ID:        uuid.New(),
	}

	encoded, err := encodeDeploymentPolicyCursor(cursor)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(encoded).To(MatchRegexp(`^[A-Za-z0-9_-]+$`))
	decoded, err := decodeDeploymentPolicyCursor(
		encoded,
		deploymentPolicyCursorResourceVersions,
		parentID,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(*decoded).To(Equal(cursor))

	_, err = decodeDeploymentPolicyCursor(
		encoded,
		deploymentPolicyCursorResourceBindings,
		uuid.Nil,
	)
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
	_, _, err = normalizeDeploymentPolicyListFilter(
		types.DeploymentPolicyListFilter{OrganizationID: uuid.New(), Limit: 101},
		deploymentPolicyCursorResourcePolicies,
		uuid.Nil,
	)
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
	limit, emptyCursor, err := normalizeDeploymentPolicyListFilter(
		types.DeploymentPolicyListFilter{OrganizationID: uuid.New()},
		deploymentPolicyCursorResourcePolicies,
		uuid.Nil,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(limit).To(Equal(50))
	g.Expect(emptyCursor).To(BeNil())
	_, err = decodeDeploymentPolicyCursor(
		strings.Repeat("a", deploymentPolicyMaximumCursorSize+1),
		deploymentPolicyCursorResourcePolicies,
		uuid.Nil,
	)
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}

func TestFinishDeploymentPolicyPageUsesLimitPlusOneAndLastReturnedKey(t *testing.T) {
	g := NewWithT(t)
	base := time.Date(2026, time.July, 18, 1, 0, 0, 0, time.UTC)
	items := []types.DeploymentPolicy{
		{ID: uuid.New(), CreatedAt: base.Add(3 * time.Minute)},
		{ID: uuid.New(), CreatedAt: base.Add(2 * time.Minute)},
		{ID: uuid.New(), CreatedAt: base.Add(time.Minute)},
	}

	page, err := finishDeploymentPolicyPage(
		items,
		2,
		deploymentPolicyCursorResourcePolicies,
		uuid.Nil,
		func(policy types.DeploymentPolicy) (time.Time, uuid.UUID) {
			return policy.CreatedAt, policy.ID
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(page.Items).To(Equal(items[:2]))
	g.Expect(page.NextCursor).NotTo(BeEmpty())
	cursor, err := decodeDeploymentPolicyCursor(
		page.NextCursor,
		deploymentPolicyCursorResourcePolicies,
		uuid.Nil,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cursor.CreatedAt).To(Equal(items[1].CreatedAt))
	g.Expect(cursor.ID).To(Equal(items[1].ID))

	finalPage, err := finishDeploymentPolicyPage(
		items[:2],
		2,
		deploymentPolicyCursorResourcePolicies,
		uuid.Nil,
		func(policy types.DeploymentPolicy) (time.Time, uuid.UUID) {
			return policy.CreatedAt, policy.ID
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(finalPage.NextCursor).To(BeEmpty())
}
