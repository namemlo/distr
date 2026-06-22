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
	g.Expect(PermissionScopeApplication.Supported()).To(BeFalse())
	g.Expect(PermissionScopeApplication.Known()).To(BeTrue())

	g.Expect(UserRoleReadWrite.HasScopedPermission(OrganizationPermission(PermissionDeploymentExecute))).To(BeTrue())
	g.Expect(UserRoleReadOnly.HasScopedPermission(OrganizationPermission(PermissionDeploymentExecute))).To(BeFalse())
	g.Expect(UserRoleAdmin.HasScopedPermission(ScopedPermission{
		Permission: PermissionDeploymentExecute,
		Scope:      PermissionScopeApplication,
	})).To(BeFalse())

	g.Expect(SupportedPermissionScopes()).To(Equal([]PermissionScope{PermissionScopeOrganization}))
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
