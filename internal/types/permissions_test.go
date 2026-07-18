package types

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestBuiltInRolePermissions(t *testing.T) {
	g := NewWithT(t)

	g.Expect(UserRoleReadOnly.HasPermission(PermissionReleaseView)).To(BeTrue())
	g.Expect(UserRoleReadOnly.HasPermission(PermissionProcessView)).To(BeTrue())
	g.Expect(UserRoleReadOnly.HasPermission(PermissionVariableView)).To(BeTrue())
	g.Expect(UserRoleReadOnly.HasPermission(PermissionRunbookView)).To(BeTrue())
	g.Expect(UserRoleReadOnly.HasPermission(PermissionAuditView)).To(BeTrue())
	g.Expect(UserRoleReadOnly.HasPermission(PermissionReleaseCreate)).To(BeFalse())
	g.Expect(UserRoleReadOnly.HasPermission(PermissionDeploymentExecute)).To(BeFalse())

	g.Expect(UserRoleReadWrite.HasPermission(PermissionReleaseCreate)).To(BeTrue())
	g.Expect(UserRoleReadWrite.HasPermission(PermissionReleasePublish)).To(BeTrue())
	g.Expect(UserRoleReadWrite.HasPermission(PermissionDeploymentExecute)).To(BeTrue())
	g.Expect(UserRoleReadWrite.HasPermission(PermissionRunbookExecute)).To(BeTrue())
	g.Expect(UserRoleReadWrite.HasPermission(PermissionEnvironmentManage)).To(BeTrue())

	for _, permission := range AllPermissions() {
		g.Expect(UserRoleAdmin.HasPermission(permission)).To(BeTrue(), "admin missing %s", permission)
	}
}

func TestScopedPermissionSupport(t *testing.T) {
	g := NewWithT(t)

	g.Expect(PermissionScopeOrganization.Supported()).To(BeTrue())
	g.Expect(PermissionScopeCustomer.Supported()).To(BeTrue())
	g.Expect(PermissionScopeEnvironment.Supported()).To(BeTrue())
	g.Expect(PermissionScopeDeploymentUnit.Supported()).To(BeTrue())
	g.Expect(PermissionScopeComponent.Supported()).To(BeTrue())
	g.Expect(PermissionScopeCampaign.Supported()).To(BeTrue())
	g.Expect(PermissionScopeApplication.Supported()).To(BeFalse())
	g.Expect(PermissionScopeApplication.Known()).To(BeTrue())

	g.Expect(UserRoleReadWrite.HasScopedPermission(OrganizationPermission(PermissionDeploymentExecute))).To(BeTrue())
	g.Expect(UserRoleReadOnly.HasScopedPermission(OrganizationPermission(PermissionDeploymentExecute))).To(BeFalse())
	g.Expect(UserRoleAdmin.HasScopedPermission(ScopedPermission{
		Permission: PermissionDeploymentExecute,
		Scope:      PermissionScopeApplication,
	})).To(BeFalse())

	g.Expect(SupportedPermissionScopes()).To(Equal([]PermissionScope{
		PermissionScopeOrganization,
		PermissionScopeCustomer,
		PermissionScopeEnvironment,
		PermissionScopeDeploymentUnit,
		PermissionScopeComponent,
		PermissionScopeCampaign,
	}))
}

func TestParsePermission(t *testing.T) {
	g := NewWithT(t)

	permission, err := ParsePermission("DeploymentExecute")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(permission).To(Equal(PermissionDeploymentExecute))

	_, err = ParsePermission("DeploymentDestroyEverything")
	g.Expect(err).To(MatchError(ErrInvalidPermission))
}

func TestBuiltInRoleDefinitionsAreIsolatedCopies(t *testing.T) {
	g := NewWithT(t)

	definitions := BuiltInRoleDefinitions()
	g.Expect(definitions).To(HaveLen(3))
	g.Expect(definitions[0].Scope).To(Equal(PermissionScopeOrganization))
	g.Expect(definitions[0].Permissions).NotTo(BeEmpty())

	definitions[0].Permissions[0] = PermissionDeploymentExecute

	fresh := BuiltInRoleDefinitions()
	g.Expect(fresh[0].Permissions[0]).NotTo(Equal(PermissionDeploymentExecute))
}

func TestControlPlaneActionsAreValidAndIsolated(t *testing.T) {
	g := NewWithT(t)

	actions := AllControlPlaneActions()
	g.Expect(actions).To(ContainElements(
		ActionReleaseCreate,
		ActionReleasePublish,
		ActionReleaseBlock,
		ActionRegistryManage,
		ActionConfigManage,
		ActionPlanCreate,
		ActionPlanPublish,
		ActionPlanExecute,
		ActionApprovalDecide,
		ActionPolicyManage,
		ActionCalendarManage,
		ActionFreezeManage,
		ActionEmergencyOverride,
		ActionCampaignControl,
		ActionObserverManage,
		ActionReconciliationDecide,
		ActionAuditView,
		ActionAuditExport,
		ActionSampleRetire,
		ActionAuthorizationManage,
	))
	for _, action := range actions {
		g.Expect(action.Valid()).To(BeTrue(), "invalid registered action %q", action)
	}
	g.Expect(Action("deployment.destroy-everything").Valid()).To(BeFalse())

	actions[0] = Action("mutated")
	g.Expect(AllControlPlaneActions()[0]).NotTo(Equal(Action("mutated")))
}

func TestLegacyControlPlaneActionsKeepDeveloperAndAdministratorDistinct(t *testing.T) {
	g := NewWithT(t)

	g.Expect(ActionsForLegacyRole(UserRoleReadOnly)).To(ConsistOf(
		ActionAuditView,
		ActionAuditExport,
	))
	g.Expect(ActionsForLegacyRole(UserRoleReadWrite)).To(ContainElements(
		ActionReleaseCreate,
		ActionRegistryManage,
		ActionPlanExecute,
		ActionAuditView,
	))
	g.Expect(ActionsForLegacyRole(UserRoleReadWrite)).NotTo(ContainElements(
		ActionAuthorizationManage,
		ActionApprovalDecide,
		ActionEmergencyOverride,
	))
	g.Expect(ActionsForLegacyRole(UserRoleAdmin)).To(ConsistOf(AllControlPlaneActions()))
}
