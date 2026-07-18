package types

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

type Action string

const (
	ActionReleaseCreate        Action = "release.create"
	ActionReleasePublish       Action = "release.publish"
	ActionReleaseBlock         Action = "release.block"
	ActionRegistryManage       Action = "registry.manage"
	ActionConfigManage         Action = "config.manage"
	ActionPlanCreate           Action = "plan.create"
	ActionPlanPublish          Action = "plan.publish"
	ActionPlanExecute          Action = "plan.execute"
	ActionApprovalDecide       Action = "approval.decide"
	ActionPolicyManage         Action = "policy.manage"
	ActionCalendarManage       Action = "calendar.manage"
	ActionFreezeManage         Action = "freeze.manage"
	ActionEmergencyOverride    Action = "emergency.override"
	ActionCampaignControl      Action = "campaign.control"
	ActionObserverManage       Action = "observer.manage"
	ActionReconciliationDecide Action = "reconciliation.decide"
	ActionAuditView            Action = "audit.view"
	ActionAuditExport          Action = "audit.export"
	ActionSampleRetire         Action = "sample.retire"
	ActionAuthorizationManage  Action = "authorization.manage"
)

var allControlPlaneActions = []Action{
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
}

var readWriteControlPlaneActions = []Action{
	ActionReleaseCreate,
	ActionReleasePublish,
	ActionRegistryManage,
	ActionConfigManage,
	ActionPlanCreate,
	ActionPlanPublish,
	ActionPlanExecute,
	ActionCampaignControl,
	ActionAuditView,
	ActionAuditExport,
}

var reservedAuthorizationRoleKeys = []string{
	"legacy.read_only",
	"legacy.read_write",
	"legacy.admin",
}

func IsReservedAuthorizationRoleKey(key string) bool {
	return slices.Contains(reservedAuthorizationRoleKeys, key)
}

func AllControlPlaneActions() []Action {
	return slices.Clone(allControlPlaneActions)
}

func (a Action) Valid() bool {
	return slices.Contains(allControlPlaneActions, a)
}

func ActionsForLegacyRole(role UserRole) []Action {
	switch role {
	case UserRoleReadOnly:
		return []Action{ActionAuditView, ActionAuditExport}
	case UserRoleReadWrite:
		return slices.Clone(readWriteControlPlaneActions)
	case UserRoleAdmin:
		return AllControlPlaneActions()
	default:
		return nil
	}
}

type AuthorizationPrincipalKind string

const (
	AuthorizationPrincipalUser  AuthorizationPrincipalKind = "user"
	AuthorizationPrincipalGroup AuthorizationPrincipalKind = "group"
)

func (k AuthorizationPrincipalKind) Valid() bool {
	return k == AuthorizationPrincipalUser || k == AuthorizationPrincipalGroup
}

type ScopeRef struct {
	Kind PermissionScope `db:"scope_kind" json:"kind"`
	ID   uuid.UUID       `db:"scope_id" json:"id"`
}

type ResourceRef struct {
	OrganizationID uuid.UUID       `json:"organizationId"`
	Kind           PermissionScope `json:"kind"`
	ID             uuid.UUID       `json:"id"`
}

type AccessRequest struct {
	OrganizationID uuid.UUID  `json:"organizationId"`
	PrincipalID    uuid.UUID  `json:"principalId"`
	CredentialRole *UserRole  `json:"-"`
	IsSuperAdmin   bool       `json:"-"`
	DecisionAt     time.Time  `json:"-"`
	Action         Action     `json:"action"`
	ResourceScopes []ScopeRef `json:"resourceScopes"`
}

const (
	AccessReasonBindingMatch   = "binding_match"
	AccessReasonLegacyFallback = "legacy_role_fallback"
	AccessReasonDenied         = "access_denied"
)

type AccessDecision struct {
	Allowed         bool        `json:"allowed"`
	MatchedBindings []uuid.UUID `json:"matchedBindings"`
	ReasonCode      string      `json:"reasonCode"`
}

type AccessGrant struct {
	BindingID      uuid.UUID                  `db:"binding_id" json:"bindingId"`
	PrincipalKind  AuthorizationPrincipalKind `db:"principal_kind" json:"principalKind"`
	Scope          ScopeRef                   `json:"scope"`
	Actions        []Action                   `json:"actions"`
	EffectiveFrom  time.Time                  `db:"effective_from" json:"effectiveFrom"`
	EffectiveUntil *time.Time                 `db:"effective_until" json:"effectiveUntil,omitempty"`
}

type RoleDefinition struct {
	ID               uuid.UUID  `db:"id" json:"id"`
	CreatedAt        time.Time  `db:"created_at" json:"createdAt"`
	OrganizationID   uuid.UUID  `db:"organization_id" json:"organizationId"`
	Key              string     `db:"role_key" json:"key"`
	DisplayName      string     `db:"display_name" json:"displayName"`
	Description      string     `db:"description" json:"description"`
	BuiltIn          bool       `db:"built_in" json:"builtIn"`
	SourceLegacyRole *UserRole  `db:"source_legacy_role" json:"sourceLegacyRole,omitempty"`
	Revision         int64      `db:"revision" json:"revision"`
	CreatedByUserID  *uuid.UUID `db:"created_by_useraccount_id" json:"createdByUserAccountId,omitempty"`
	Permissions      []Action   `json:"permissions"`
}

type RoleBinding struct {
	ID                           uuid.UUID                  `db:"id" json:"id"`
	CreatedAt                    time.Time                  `db:"created_at" json:"createdAt"`
	OrganizationID               uuid.UUID                  `db:"organization_id" json:"organizationId"`
	RoleDefinitionID             uuid.UUID                  `db:"role_definition_id" json:"roleDefinitionId"`
	PrincipalKind                AuthorizationPrincipalKind `db:"principal_kind" json:"principalKind"`
	PrincipalID                  uuid.UUID                  `db:"principal_id" json:"principalId"`
	PrincipalMembershipCreatedAt *time.Time                 `db:"principal_membership_created_at" json:"-"`
	Scope                        ScopeRef                   `json:"scope"`
	EffectiveFrom                time.Time                  `db:"effective_from" json:"effectiveFrom"`
	EffectiveUntil               *time.Time                 `db:"effective_until" json:"effectiveUntil,omitempty"`
	Reason                       string                     `db:"reason" json:"reason"`
	Revision                     int64                      `db:"revision" json:"revision"`
	CreatedByUserID              *uuid.UUID                 `db:"created_by_useraccount_id" json:"createdByUserAccountId,omitempty"`
	Source                       string                     `db:"source" json:"source"`
}

type PrincipalGroup struct {
	ID              uuid.UUID  `db:"id" json:"id"`
	CreatedAt       time.Time  `db:"created_at" json:"createdAt"`
	OrganizationID  uuid.UUID  `db:"organization_id" json:"organizationId"`
	Key             string     `db:"group_key" json:"key"`
	DisplayName     string     `db:"display_name" json:"displayName"`
	Description     string     `db:"description" json:"description"`
	CreatedByUserID *uuid.UUID `db:"created_by_useraccount_id" json:"createdByUserAccountId,omitempty"`
}

type PrincipalGroupMember struct {
	ID                      uuid.UUID  `db:"id" json:"id"`
	CreatedAt               time.Time  `db:"created_at" json:"createdAt"`
	OrganizationID          uuid.UUID  `db:"organization_id" json:"organizationId"`
	GroupID                 uuid.UUID  `db:"group_id" json:"groupId"`
	UserAccountID           uuid.UUID  `db:"user_account_id" json:"userAccountId"`
	UserMembershipCreatedAt time.Time  `db:"user_membership_created_at" json:"-"`
	EffectiveFrom           time.Time  `db:"effective_from" json:"effectiveFrom"`
	EffectiveUntil          *time.Time `db:"effective_until" json:"effectiveUntil,omitempty"`
	AddedByUserID           *uuid.UUID `db:"added_by_useraccount_id" json:"addedByUserAccountId,omitempty"`
	Reason                  string     `db:"reason" json:"reason"`
}

type AuthorizationRevisionState string

const (
	AuthorizationRevisionActive  AuthorizationRevisionState = "active"
	AuthorizationRevisionRevoked AuthorizationRevisionState = "revoked"
)

type RoleBindingRevision struct {
	ID             uuid.UUID                  `db:"id" json:"id"`
	CreatedAt      time.Time                  `db:"created_at" json:"createdAt"`
	OrganizationID uuid.UUID                  `db:"organization_id" json:"organizationId"`
	RoleBindingID  uuid.UUID                  `db:"role_binding_id" json:"roleBindingId"`
	Revision       int64                      `db:"revision" json:"revision"`
	State          AuthorizationRevisionState `db:"state" json:"state"`
	EffectiveFrom  time.Time                  `db:"effective_from" json:"effectiveFrom"`
	ActorUserID    uuid.UUID                  `db:"actor_useraccount_id" json:"actorUserAccountId"`
	Reason         string                     `db:"reason" json:"reason"`
}

type PrincipalGroupMemberRevision struct {
	ID                     uuid.UUID                  `db:"id" json:"id"`
	CreatedAt              time.Time                  `db:"created_at" json:"createdAt"`
	OrganizationID         uuid.UUID                  `db:"organization_id" json:"organizationId"`
	PrincipalGroupMemberID uuid.UUID                  `db:"principal_group_member_id" json:"principalGroupMemberId"`
	Revision               int64                      `db:"revision" json:"revision"`
	State                  AuthorizationRevisionState `db:"state" json:"state"`
	EffectiveFrom          time.Time                  `db:"effective_from" json:"effectiveFrom"`
	ActorUserID            uuid.UUID                  `db:"actor_useraccount_id" json:"actorUserAccountId"`
	Reason                 string                     `db:"reason" json:"reason"`
}

type ControlPlaneEnrollment struct {
	ID             uuid.UUID  `db:"id" json:"id"`
	CreatedAt      time.Time  `db:"created_at" json:"createdAt"`
	OrganizationID uuid.UUID  `db:"organization_id" json:"organizationId"`
	Scope          ScopeRef   `json:"scope"`
	Enabled        bool       `db:"enabled" json:"enabled"`
	EffectiveFrom  time.Time  `db:"effective_from" json:"effectiveFrom"`
	EffectiveUntil *time.Time `db:"effective_until" json:"effectiveUntil,omitempty"`
	ActorUserID    uuid.UUID  `db:"actor_useraccount_id" json:"actorUserAccountId"`
	Reason         string     `db:"reason" json:"reason"`
	Revision       int64      `db:"revision" json:"revision"`
}

type AuthorizationListFilter struct {
	OrganizationID uuid.UUID
	Collection     string
	ParentID       uuid.UUID
	Cursor         string
	Limit          int
}
