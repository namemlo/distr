package api

import (
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/governance"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

const (
	deploymentPolicyMaximumPageLimit  = 100
	deploymentPolicyMaximumCursorSize = 2048
)

var (
	deploymentPolicyKeyPattern    = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
	deploymentPolicyCursorPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

type DeploymentPolicyListRequest struct {
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit"`
}

func (request DeploymentPolicyListRequest) Validate() error {
	if request.Limit < 0 || request.Limit > deploymentPolicyMaximumPageLimit {
		return validation.NewValidationFailedError(
			"limit must be between 1 and 100 when provided",
		)
	}
	if len(request.Cursor) > deploymentPolicyMaximumCursorSize {
		return validation.NewValidationFailedError("cursor is too large")
	}
	if request.Cursor != "" && !deploymentPolicyCursorPattern.MatchString(request.Cursor) {
		return validation.NewValidationFailedError(
			"cursor must be an opaque URL-safe token",
		)
	}
	return nil
}

type CreateDeploymentPolicyRequest struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (request *CreateDeploymentPolicyRequest) Validate() error {
	request.Key = strings.TrimSpace(request.Key)
	request.Name = strings.TrimSpace(request.Name)
	if !deploymentPolicyKeyPattern.MatchString(request.Key) ||
		len(request.Key) > 128 {
		return validation.NewValidationFailedError(
			"key must be canonical lowercase text with at most 128 characters",
		)
	}
	if request.Name == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if len(request.Name) > 256 {
		return validation.NewValidationFailedError("name must contain at most 256 characters")
	}
	if len(request.Description) > 4096 {
		return validation.NewValidationFailedError(
			"description must contain at most 4096 characters",
		)
	}
	return nil
}

type UpdateDeploymentPolicyRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (request *UpdateDeploymentPolicyRequest) Validate() error {
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if len(request.Name) > 256 {
		return validation.NewValidationFailedError("name must contain at most 256 characters")
	}
	if len(request.Description) > 4096 {
		return validation.NewValidationFailedError(
			"description must contain at most 4096 characters",
		)
	}
	return nil
}

type CreateDeploymentPolicyVersionRequest struct {
	Document types.DeploymentPolicyDocument `json:"document"`
}

func (request *CreateDeploymentPolicyVersionRequest) Validate() error {
	issues := governance.ValidateDeploymentPolicyVersion(types.DeploymentPolicyVersion{
		State:    types.DeploymentPolicyVersionStateDraft,
		Document: request.Document,
	})
	if len(issues) != 0 {
		return validation.NewValidationFailedError(issues[0].Message)
	}
	request.Document = governance.NormalizeDeploymentPolicyDocument(request.Document)
	return nil
}

type CreateDeploymentPolicyBindingRequest struct {
	PolicyVersionID uuid.UUID                              `json:"policyVersionId"`
	ScopeKind       types.DeploymentPolicyBindingScopeKind `json:"scopeKind"`
	ScopeID         uuid.UUID                              `json:"scopeId"`
	Role            types.DeploymentPolicyBindingRole      `json:"role"`
}

func (request CreateDeploymentPolicyBindingRequest) Validate() error {
	if request.PolicyVersionID == uuid.Nil {
		return validation.NewValidationFailedError("policyVersionId is required")
	}
	if !request.ScopeKind.IsValid() {
		return validation.NewValidationFailedError("scopeKind is invalid")
	}
	if request.ScopeID == uuid.Nil {
		return validation.NewValidationFailedError("scopeId is required")
	}
	if !request.Role.IsValid() {
		return validation.NewValidationFailedError("role is invalid")
	}
	if request.Role == types.DeploymentPolicyBindingRoleSubscriber &&
		request.ScopeKind != types.DeploymentPolicyBindingScopeCustomer {
		return validation.NewValidationFailedError(
			"subscriber bindings require customer scope",
		)
	}
	return nil
}

type DeploymentPolicy struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

type DeploymentPolicyVersion struct {
	ID                       uuid.UUID                          `json:"id"`
	CreatedAt                time.Time                          `json:"createdAt"`
	UpdatedAt                time.Time                          `json:"updatedAt"`
	PolicyID                 uuid.UUID                          `json:"policyId"`
	VersionNumber            int                                `json:"versionNumber"`
	State                    types.DeploymentPolicyVersionState `json:"state"`
	Document                 types.DeploymentPolicyDocument     `json:"document"`
	CanonicalChecksum        string                             `json:"canonicalChecksum"`
	CreatedByUserAccountID   uuid.UUID                          `json:"createdByUserAccountId"`
	PublishedByUserAccountID *uuid.UUID                         `json:"publishedByUserAccountId,omitempty"`
	PublishedAt              *time.Time                         `json:"publishedAt,omitempty"`
}

type DeploymentPolicyVersionSummary struct {
	ID                       uuid.UUID                          `json:"id"`
	CreatedAt                time.Time                          `json:"createdAt"`
	UpdatedAt                time.Time                          `json:"updatedAt"`
	PolicyID                 uuid.UUID                          `json:"policyId"`
	VersionNumber            int                                `json:"versionNumber"`
	State                    types.DeploymentPolicyVersionState `json:"state"`
	CanonicalChecksum        string                             `json:"canonicalChecksum"`
	CreatedByUserAccountID   uuid.UUID                          `json:"createdByUserAccountId"`
	PublishedByUserAccountID *uuid.UUID                         `json:"publishedByUserAccountId,omitempty"`
	PublishedAt              *time.Time                         `json:"publishedAt,omitempty"`
}

type DeploymentPolicyBinding struct {
	ID                     uuid.UUID                              `json:"id"`
	CreatedAt              time.Time                              `json:"createdAt"`
	PolicyVersionID        uuid.UUID                              `json:"policyVersionId"`
	ScopeKind              types.DeploymentPolicyBindingScopeKind `json:"scopeKind"`
	ScopeID                uuid.UUID                              `json:"scopeId"`
	Role                   types.DeploymentPolicyBindingRole      `json:"role"`
	CreatedByUserAccountID uuid.UUID                              `json:"createdByUserAccountId"`
	RetiredAt              *time.Time                             `json:"retiredAt,omitempty"`
}

type DeploymentPolicyValidationResponse struct {
	Valid  bool                    `json:"valid"`
	Issues []types.ValidationIssue `json:"issues"`
}

type DeploymentPolicyPage struct {
	Items      []DeploymentPolicy `json:"items"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

type DeploymentPolicyVersionPage struct {
	Items      []DeploymentPolicyVersionSummary `json:"items"`
	NextCursor string                           `json:"nextCursor,omitempty"`
}

type DeploymentPolicyBindingPage struct {
	Items      []DeploymentPolicyBinding `json:"items"`
	NextCursor string                    `json:"nextCursor,omitempty"`
}
