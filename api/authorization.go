package api

import (
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type AuthorizationRole struct {
	ID                     uuid.UUID       `json:"id"`
	CreatedAt              time.Time       `json:"createdAt"`
	Key                    string          `json:"key"`
	DisplayName            string          `json:"displayName"`
	Description            string          `json:"description"`
	BuiltIn                bool            `json:"builtIn"`
	SourceLegacyRole       *types.UserRole `json:"sourceLegacyRole,omitempty"`
	Revision               int64           `json:"revision"`
	CreatedByUserAccountID *uuid.UUID      `json:"createdByUserAccountId,omitempty"`
	Permissions            []types.Action  `json:"permissions"`
}

type AuthorizationRoleListResponse struct {
	Roles []AuthorizationRole `json:"roles"`
}

type CreateAuthorizationRoleRequest struct {
	Key         string         `json:"key"`
	DisplayName string         `json:"displayName"`
	Description string         `json:"description"`
	Permissions []types.Action `json:"permissions"`
}

func (request CreateAuthorizationRoleRequest) Validate() error {
	if strings.TrimSpace(request.Key) == "" {
		return validation.NewValidationFailedError("key is required")
	}
	if strings.TrimSpace(request.DisplayName) == "" {
		return validation.NewValidationFailedError("displayName is required")
	}
	if len(request.Permissions) == 0 {
		return validation.NewValidationFailedError("at least one permission is required")
	}
	for _, permission := range request.Permissions {
		if !permission.Valid() {
			return validation.NewValidationFailedError("unsupported permission")
		}
	}
	return nil
}

type AuthorizationRoleBinding struct {
	ID                     uuid.UUID                        `json:"id"`
	CreatedAt              time.Time                        `json:"createdAt"`
	RoleDefinitionID       uuid.UUID                        `json:"roleDefinitionId"`
	PrincipalKind          types.AuthorizationPrincipalKind `json:"principalKind"`
	PrincipalID            uuid.UUID                        `json:"principalId"`
	Scope                  types.ScopeRef                   `json:"scope"`
	EffectiveFrom          time.Time                        `json:"effectiveFrom"`
	EffectiveUntil         *time.Time                       `json:"effectiveUntil,omitempty"`
	Reason                 string                           `json:"reason"`
	Revision               int64                            `json:"revision"`
	CreatedByUserAccountID *uuid.UUID                       `json:"createdByUserAccountId,omitempty"`
	Source                 string                           `json:"source"`
}

type AuthorizationRoleBindingListResponse struct {
	Bindings []AuthorizationRoleBinding `json:"bindings"`
}

type CreateAuthorizationRoleBindingRequest struct {
	RoleDefinitionID uuid.UUID                        `json:"roleDefinitionId"`
	PrincipalKind    types.AuthorizationPrincipalKind `json:"principalKind"`
	PrincipalID      uuid.UUID                        `json:"principalId"`
	Scope            types.ScopeRef                   `json:"scope"`
	EffectiveFrom    time.Time                        `json:"effectiveFrom"`
	EffectiveUntil   *time.Time                       `json:"effectiveUntil,omitempty"`
	Reason           string                           `json:"reason"`
}

func (request CreateAuthorizationRoleBindingRequest) Validate() error {
	if request.RoleDefinitionID == uuid.Nil {
		return validation.NewValidationFailedError("roleDefinitionId is required")
	}
	if !request.PrincipalKind.Valid() || request.PrincipalID == uuid.Nil {
		return validation.NewValidationFailedError("principal is invalid")
	}
	if !request.Scope.Kind.Supported() || request.Scope.ID == uuid.Nil {
		return validation.NewValidationFailedError("scope is invalid")
	}
	if request.EffectiveFrom.IsZero() {
		return validation.NewValidationFailedError("effectiveFrom is required")
	}
	if request.EffectiveUntil != nil &&
		!request.EffectiveUntil.After(request.EffectiveFrom) {
		return validation.NewValidationFailedError("effectiveUntil must be after effectiveFrom")
	}
	if strings.TrimSpace(request.Reason) == "" {
		return validation.NewValidationFailedError("reason is required")
	}
	return nil
}

type AuthorizationPrincipalGroup struct {
	ID                     uuid.UUID  `json:"id"`
	CreatedAt              time.Time  `json:"createdAt"`
	Key                    string     `json:"key"`
	DisplayName            string     `json:"displayName"`
	Description            string     `json:"description"`
	CreatedByUserAccountID *uuid.UUID `json:"createdByUserAccountId,omitempty"`
}

type AuthorizationPrincipalGroupListResponse struct {
	Groups []AuthorizationPrincipalGroup `json:"groups"`
}

type CreateAuthorizationPrincipalGroupRequest struct {
	Key         string `json:"key"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

func (request CreateAuthorizationPrincipalGroupRequest) Validate() error {
	if strings.TrimSpace(request.Key) == "" {
		return validation.NewValidationFailedError("key is required")
	}
	if strings.TrimSpace(request.DisplayName) == "" {
		return validation.NewValidationFailedError("displayName is required")
	}
	return nil
}

type AuthorizationPrincipalGroupMember struct {
	ID                   uuid.UUID  `json:"id"`
	CreatedAt            time.Time  `json:"createdAt"`
	GroupID              uuid.UUID  `json:"groupId"`
	UserAccountID        uuid.UUID  `json:"userAccountId"`
	EffectiveFrom        time.Time  `json:"effectiveFrom"`
	EffectiveUntil       *time.Time `json:"effectiveUntil,omitempty"`
	AddedByUserAccountID *uuid.UUID `json:"addedByUserAccountId,omitempty"`
	Reason               string     `json:"reason"`
}

type AuthorizationPrincipalGroupMemberListResponse struct {
	Members []AuthorizationPrincipalGroupMember `json:"members"`
}

type AddAuthorizationPrincipalGroupMemberRequest struct {
	UserAccountID  uuid.UUID  `json:"userAccountId"`
	EffectiveFrom  time.Time  `json:"effectiveFrom"`
	EffectiveUntil *time.Time `json:"effectiveUntil,omitempty"`
	Reason         string     `json:"reason"`
}

func (request AddAuthorizationPrincipalGroupMemberRequest) Validate() error {
	if request.UserAccountID == uuid.Nil {
		return validation.NewValidationFailedError("userAccountId is required")
	}
	if request.EffectiveFrom.IsZero() {
		return validation.NewValidationFailedError("effectiveFrom is required")
	}
	if request.EffectiveUntil != nil &&
		!request.EffectiveUntil.After(request.EffectiveFrom) {
		return validation.NewValidationFailedError("effectiveUntil must be after effectiveFrom")
	}
	if strings.TrimSpace(request.Reason) == "" {
		return validation.NewValidationFailedError("reason is required")
	}
	return nil
}

type ControlPlaneEnrollment struct {
	ID                 uuid.UUID      `json:"id"`
	CreatedAt          time.Time      `json:"createdAt"`
	Scope              types.ScopeRef `json:"scope"`
	Enabled            bool           `json:"enabled"`
	EffectiveFrom      time.Time      `json:"effectiveFrom"`
	EffectiveUntil     *time.Time     `json:"effectiveUntil,omitempty"`
	ActorUserAccountID uuid.UUID      `json:"actorUserAccountId"`
	Reason             string         `json:"reason"`
	Revision           int64          `json:"revision"`
}

type ControlPlaneEnrollmentListResponse struct {
	Enrollments []ControlPlaneEnrollment `json:"enrollments"`
}

type CreateControlPlaneEnrollmentRequest struct {
	Scope          types.ScopeRef `json:"scope"`
	Enabled        bool           `json:"enabled"`
	EffectiveFrom  time.Time      `json:"effectiveFrom"`
	EffectiveUntil *time.Time     `json:"effectiveUntil,omitempty"`
	Reason         string         `json:"reason"`
}

func (request CreateControlPlaneEnrollmentRequest) Validate() error {
	if request.Scope.ID == uuid.Nil {
		return validation.NewValidationFailedError("scopeId is required")
	}
	if request.Scope.Kind != types.PermissionScopeOrganization &&
		request.Scope.Kind != types.PermissionScopeEnvironment {
		return validation.NewValidationFailedError(
			"scope must be organization or environment",
		)
	}
	if request.EffectiveFrom.IsZero() {
		return validation.NewValidationFailedError("effectiveFrom is required")
	}
	if request.EffectiveUntil != nil &&
		!request.EffectiveUntil.After(request.EffectiveFrom) {
		return validation.NewValidationFailedError("effectiveUntil must be after effectiveFrom")
	}
	if strings.TrimSpace(request.Reason) == "" {
		return validation.NewValidationFailedError("reason is required")
	}
	return nil
}
