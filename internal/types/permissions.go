package types

import (
	"errors"
	"slices"
)

type Permission string

const (
	PermissionReleaseView            Permission = "ReleaseView"
	PermissionReleaseCreate          Permission = "ReleaseCreate"
	PermissionReleasePublish         Permission = "ReleasePublish"
	PermissionReleaseBlock           Permission = "ReleaseBlock"
	PermissionProcessView            Permission = "ProcessView"
	PermissionProcessEdit            Permission = "ProcessEdit"
	PermissionVariableView           Permission = "VariableView"
	PermissionVariableEdit           Permission = "VariableEdit"
	PermissionSecretReferenceManage  Permission = "SecretReferenceManage"
	PermissionDeploymentPlan         Permission = "DeploymentPlan"
	PermissionDeploymentExecute      Permission = "DeploymentExecute"
	PermissionDeploymentApprove      Permission = "DeploymentApprove"
	PermissionDeploymentGuideFailure Permission = "DeploymentGuideFailure"
	PermissionRunbookView            Permission = "RunbookView"
	PermissionRunbookEdit            Permission = "RunbookEdit"
	PermissionRunbookExecute         Permission = "RunbookExecute"
	PermissionEnvironmentManage      Permission = "EnvironmentManage"
	PermissionTargetManage           Permission = "TargetManage"
	PermissionFreezeManage           Permission = "FreezeManage"
	PermissionTemplateManage         Permission = "TemplateManage"
	PermissionAuditView              Permission = "AuditView"
)

var ErrInvalidPermission = errors.New("invalid permission")

var allPermissions = []Permission{
	PermissionReleaseView,
	PermissionReleaseCreate,
	PermissionReleasePublish,
	PermissionReleaseBlock,
	PermissionProcessView,
	PermissionProcessEdit,
	PermissionVariableView,
	PermissionVariableEdit,
	PermissionSecretReferenceManage,
	PermissionDeploymentPlan,
	PermissionDeploymentExecute,
	PermissionDeploymentApprove,
	PermissionDeploymentGuideFailure,
	PermissionRunbookView,
	PermissionRunbookEdit,
	PermissionRunbookExecute,
	PermissionEnvironmentManage,
	PermissionTargetManage,
	PermissionFreezeManage,
	PermissionTemplateManage,
	PermissionAuditView,
}

var mutationPermissions = []Permission{
	PermissionReleaseCreate,
	PermissionReleasePublish,
	PermissionReleaseBlock,
	PermissionProcessEdit,
	PermissionVariableEdit,
	PermissionSecretReferenceManage,
	PermissionDeploymentPlan,
	PermissionDeploymentExecute,
	PermissionDeploymentApprove,
	PermissionDeploymentGuideFailure,
	PermissionRunbookEdit,
	PermissionRunbookExecute,
	PermissionEnvironmentManage,
	PermissionTargetManage,
	PermissionFreezeManage,
	PermissionTemplateManage,
}

type PermissionScope string

const (
	PermissionScopeOrganization   PermissionScope = "organization"
	PermissionScopeApplication    PermissionScope = "application"
	PermissionScopeEnvironment    PermissionScope = "environment"
	PermissionScopeTenantCustomer PermissionScope = "tenant_customer"
	PermissionScopeTagSet         PermissionScope = "tag_set"
)

var knownPermissionScopes = []PermissionScope{
	PermissionScopeOrganization,
	PermissionScopeApplication,
	PermissionScopeEnvironment,
	PermissionScopeTenantCustomer,
	PermissionScopeTagSet,
}

var supportedPermissionScopes = []PermissionScope{
	PermissionScopeOrganization,
}

type ScopedPermission struct {
	Permission Permission      `json:"permission"`
	Scope      PermissionScope `json:"scope"`
}

type BuiltInRoleDefinition struct {
	Role        UserRole        `json:"role"`
	DisplayName string          `json:"displayName"`
	Scope       PermissionScope `json:"scope"`
	Permissions []Permission    `json:"permissions"`
}

var readOnlyPermissions = []Permission{
	PermissionReleaseView,
	PermissionProcessView,
	PermissionVariableView,
	PermissionRunbookView,
	PermissionAuditView,
}

var builtInRoleDefinitions = []BuiltInRoleDefinition{
	{
		Role:        UserRoleReadOnly,
		DisplayName: "Viewer",
		Scope:       PermissionScopeOrganization,
		Permissions: readOnlyPermissions,
	},
	{
		Role:        UserRoleReadWrite,
		DisplayName: "Developer",
		Scope:       PermissionScopeOrganization,
		Permissions: allPermissions,
	},
	{
		Role:        UserRoleAdmin,
		DisplayName: "Administrator",
		Scope:       PermissionScopeOrganization,
		Permissions: allPermissions,
	},
}

func ParsePermission(value string) (Permission, error) {
	permission := Permission(value)
	if !permission.Valid() {
		return "", ErrInvalidPermission
	}
	return permission, nil
}

func (p Permission) Valid() bool {
	return slices.Contains(allPermissions, p)
}

func AllPermissions() []Permission {
	return slices.Clone(allPermissions)
}

func AllMutationPermissions() []Permission {
	return slices.Clone(mutationPermissions)
}

func KnownPermissionScopes() []PermissionScope {
	return slices.Clone(knownPermissionScopes)
}

func SupportedPermissionScopes() []PermissionScope {
	return slices.Clone(supportedPermissionScopes)
}

func (s PermissionScope) Known() bool {
	return slices.Contains(knownPermissionScopes, s)
}

func (s PermissionScope) Supported() bool {
	return slices.Contains(supportedPermissionScopes, s)
}

func (p ScopedPermission) Supported() bool {
	return p.Permission.Valid() && p.Scope.Supported()
}

func OrganizationPermission(permission Permission) ScopedPermission {
	return ScopedPermission{
		Permission: permission,
		Scope:      PermissionScopeOrganization,
	}
}

func BuiltInRoleDefinitions() []BuiltInRoleDefinition {
	result := slices.Clone(builtInRoleDefinitions)
	for i := range result {
		result[i].Permissions = slices.Clone(result[i].Permissions)
	}
	return result
}

func PermissionsForRole(role UserRole) []Permission {
	for _, definition := range builtInRoleDefinitions {
		if definition.Role == role {
			return slices.Clone(definition.Permissions)
		}
	}
	return nil
}

func (r UserRole) HasPermission(permission Permission) bool {
	if !permission.Valid() {
		return false
	}
	return slices.Contains(PermissionsForRole(r), permission)
}

func (r UserRole) HasScopedPermission(scoped ScopedPermission) bool {
	if !scoped.Supported() {
		return false
	}
	return r.HasPermission(scoped.Permission)
}
