package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const DeploymentPolicySchemaV1 = "distr.deployment-policy/v1"

type DeploymentPolicyVersionState string

const (
	DeploymentPolicyVersionStateDraft     DeploymentPolicyVersionState = "DRAFT"
	DeploymentPolicyVersionStatePublished DeploymentPolicyVersionState = "PUBLISHED"
)

func (state DeploymentPolicyVersionState) IsValid() bool {
	return state == DeploymentPolicyVersionStateDraft ||
		state == DeploymentPolicyVersionStatePublished
}

type RequirementResolutionMode string

const (
	RequirementResolutionModeIncluded         RequirementResolutionMode = "included"
	RequirementResolutionModePinnedExisting   RequirementResolutionMode = "pinned_existing"
	RequirementResolutionModeSharedProvider   RequirementResolutionMode = "shared_provider"
	RequirementResolutionModeApprovedExternal RequirementResolutionMode = "approved_external"
	RequirementResolutionModeFeatureDisabled  RequirementResolutionMode = "feature_disabled"
)

func (mode RequirementResolutionMode) IsValid() bool {
	switch mode {
	case RequirementResolutionModeIncluded,
		RequirementResolutionModePinnedExisting,
		RequirementResolutionModeSharedProvider,
		RequirementResolutionModeApprovedExternal,
		RequirementResolutionModeFeatureDisabled:
		return true
	default:
		return false
	}
}

type SeparationConstraint string

const (
	SeparationConstraintRequesterCannotApprove SeparationConstraint = "requester_cannot_approve"
	SeparationConstraintPublisherCannotApprove SeparationConstraint = "publisher_cannot_approve"
	SeparationConstraintExecutorCannotApprove  SeparationConstraint = "executor_cannot_approve"
	SeparationConstraintDistinctApprovers      SeparationConstraint = "distinct_approvers"
)

func (constraint SeparationConstraint) IsValid() bool {
	switch constraint {
	case SeparationConstraintRequesterCannotApprove,
		SeparationConstraintPublisherCannotApprove,
		SeparationConstraintExecutorCannotApprove,
		SeparationConstraintDistinctApprovers:
		return true
	default:
		return false
	}
}

type PolicyAuthorityKind string

const (
	PolicyAuthorityOwner      PolicyAuthorityKind = "owner"
	PolicyAuthoritySubscriber PolicyAuthorityKind = "subscriber"
)

func (kind PolicyAuthorityKind) IsValid() bool {
	return kind == PolicyAuthorityOwner || kind == PolicyAuthoritySubscriber
}

type ApprovalRule struct {
	Key                   string                 `json:"key"`
	PrincipalGroupID      uuid.UUID              `json:"principalGroupId"`
	Quorum                int                    `json:"quorum"`
	SeparationConstraints []SeparationConstraint `json:"separationConstraints"`
	PolicyVersionID       uuid.UUID              `json:"policyVersionId,omitempty"`
	AuthorityKind         PolicyAuthorityKind    `json:"authorityKind,omitempty"`
	AuthorityID           uuid.UUID              `json:"authorityId,omitempty"`
}

type PolicyRiskGate struct {
	Key       string `json:"key"`
	Condition string `json:"condition"`
}

type AdmissionRules struct {
	AllowedResolutionModes      []RequirementResolutionMode `json:"allowedResolutionModes"`
	MinimumBakeSeconds          int                         `json:"minimumBakeSeconds"`
	MaximumWaitSeconds          int                         `json:"maximumWaitSeconds"`
	MaintenanceWindowVersionIDs []uuid.UUID                 `json:"maintenanceWindowVersionIds"`
	FreezeRuleVersionIDs        []uuid.UUID                 `json:"freezeRuleVersionIds"`
}

type CampaignRules struct {
	MinimumWaveBakeSeconds      int `json:"minimumWaveBakeSeconds"`
	MaximumWaveSize             int `json:"maximumWaveSize"`
	MaximumConcurrency          int `json:"maximumConcurrency"`
	FailureToleranceBasisPoints int `json:"failureToleranceBasisPoints"`
	MinimumHealthyBasisPoints   int `json:"minimumHealthyBasisPoints"`
}

type OverrideRules struct {
	Allowed             bool                `json:"allowed"`
	AuthorityGroupID    *uuid.UUID          `json:"authorityGroupId,omitempty"`
	ShortenableGateKeys []string            `json:"shortenableGateKeys"`
	MinimumReasonLength int                 `json:"minimumReasonLength"`
	PolicyVersionID     uuid.UUID           `json:"policyVersionId,omitempty"`
	AuthorityKind       PolicyAuthorityKind `json:"authorityKind,omitempty"`
	AuthorityID         uuid.UUID           `json:"authorityId,omitempty"`
}

type BootstrapMode string

const (
	BootstrapModeBlock               BootstrapMode = "block"
	BootstrapModeRequireApproval     BootstrapMode = "require_approval"
	BootstrapModeAllowAfterPreflight BootstrapMode = "allow_after_preflight"
)

func (mode BootstrapMode) IsValid() bool {
	switch mode {
	case BootstrapModeBlock, BootstrapModeRequireApproval, BootstrapModeAllowAfterPreflight:
		return true
	default:
		return false
	}
}

type BootstrapRules struct {
	Mode             BootstrapMode `json:"mode"`
	ApprovalRuleKeys []string      `json:"approvalRuleKeys"`
	RequiredGateKeys []string      `json:"requiredGateKeys"`
}

type DeploymentPolicyDocument struct {
	Schema           string           `json:"schema"`
	ApprovalRules    []ApprovalRule   `json:"approvalRules"`
	RiskGates        []PolicyRiskGate `json:"riskGates"`
	AdmissionRules   AdmissionRules   `json:"admissionRules"`
	CampaignRules    CampaignRules    `json:"campaignRules"`
	OverrideRules    OverrideRules    `json:"overrideRules"`
	RequiredEvidence []string         `json:"requiredEvidence"`
	BootstrapRules   BootstrapRules   `json:"bootstrapRules"`
}

type DeploymentPolicy struct {
	ID             uuid.UUID `db:"id" json:"id"`
	CreatedAt      time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time `db:"updated_at" json:"updatedAt"`
	OrganizationID uuid.UUID `db:"organization_id" json:"organizationId"`
	Key            string    `db:"key" json:"key"`
	Name           string    `db:"name" json:"name"`
	Description    string    `db:"description" json:"description"`
}

type DeploymentPolicyVersion struct {
	ID                       uuid.UUID                    `db:"id" json:"id"`
	CreatedAt                time.Time                    `db:"created_at" json:"createdAt"`
	UpdatedAt                time.Time                    `db:"updated_at" json:"updatedAt"`
	OrganizationID           uuid.UUID                    `db:"organization_id" json:"organizationId"`
	PolicyID                 uuid.UUID                    `db:"deployment_policy_id" json:"policyId"`
	VersionNumber            int                          `db:"version_number" json:"versionNumber"`
	State                    DeploymentPolicyVersionState `db:"state" json:"state"`
	Document                 DeploymentPolicyDocument     `db:"document" json:"document"`
	CanonicalChecksum        string                       `db:"canonical_checksum" json:"canonicalChecksum"`
	CanonicalPayload         json.RawMessage              `db:"canonical_payload" json:"-"`
	CreatedByUserAccountID   uuid.UUID                    `db:"created_by_useraccount_id" json:"createdByUserAccountId"`
	PublishedByUserAccountID *uuid.UUID                   `db:"published_by_useraccount_id" json:"publishedByUserAccountId,omitempty"`
	PublishedAt              *time.Time                   `db:"published_at" json:"publishedAt,omitempty"`
}

type DeploymentPolicyBindingScopeKind string

const (
	DeploymentPolicyBindingScopeOrganization   DeploymentPolicyBindingScopeKind = "organization"
	DeploymentPolicyBindingScopeCustomer       DeploymentPolicyBindingScopeKind = "customer"
	DeploymentPolicyBindingScopeEnvironment    DeploymentPolicyBindingScopeKind = "environment"
	DeploymentPolicyBindingScopeDeploymentUnit DeploymentPolicyBindingScopeKind = "deployment_unit"
	DeploymentPolicyBindingScopeComponent      DeploymentPolicyBindingScopeKind = "component"
	DeploymentPolicyBindingScopeCampaign       DeploymentPolicyBindingScopeKind = "campaign"
)

func (kind DeploymentPolicyBindingScopeKind) IsValid() bool {
	switch kind {
	case DeploymentPolicyBindingScopeOrganization,
		DeploymentPolicyBindingScopeCustomer,
		DeploymentPolicyBindingScopeEnvironment,
		DeploymentPolicyBindingScopeDeploymentUnit,
		DeploymentPolicyBindingScopeComponent,
		DeploymentPolicyBindingScopeCampaign:
		return true
	default:
		return false
	}
}

type DeploymentPolicyBindingRole string

const (
	DeploymentPolicyBindingRoleOwner      DeploymentPolicyBindingRole = "owner"
	DeploymentPolicyBindingRoleSubscriber DeploymentPolicyBindingRole = "subscriber"
)

func (role DeploymentPolicyBindingRole) IsValid() bool {
	return role == DeploymentPolicyBindingRoleOwner ||
		role == DeploymentPolicyBindingRoleSubscriber
}

type DeploymentPolicyBinding struct {
	ID                     uuid.UUID                        `db:"id" json:"id"`
	CreatedAt              time.Time                        `db:"created_at" json:"createdAt"`
	OrganizationID         uuid.UUID                        `db:"organization_id" json:"organizationId"`
	PolicyVersionID        uuid.UUID                        `db:"deployment_policy_version_id" json:"policyVersionId"`
	ScopeKind              DeploymentPolicyBindingScopeKind `db:"scope_kind" json:"scopeKind"`
	ScopeID                uuid.UUID                        `db:"scope_id" json:"scopeId"`
	Role                   DeploymentPolicyBindingRole      `db:"binding_role" json:"role"`
	CreatedByUserAccountID uuid.UUID                        `db:"created_by_useraccount_id" json:"createdByUserAccountId"`
	RetiredAt              *time.Time                       `db:"retired_at" json:"retiredAt,omitempty"`
}

type PolicyBindingRequest struct {
	OrganizationID         uuid.UUID
	PolicyVersionID        uuid.UUID
	ScopeKind              DeploymentPolicyBindingScopeKind
	ScopeID                uuid.UUID
	Role                   DeploymentPolicyBindingRole
	CreatedByUserAccountID uuid.UUID
}

type PolicySet struct {
	AuthorityKind         PolicyAuthorityKind
	AuthorityID           uuid.UUID
	SubscriberSetChecksum string
	Versions              []DeploymentPolicyVersion
}

type EffectivePolicy struct {
	VersionIDs            []uuid.UUID      `json:"versionIds"`
	Checksum              string           `json:"checksum"`
	SubscriberSetChecksum string           `json:"subscriberSetChecksum"`
	ApprovalRules         []ApprovalRule   `json:"approvalRules"`
	RiskGates             []PolicyRiskGate `json:"riskGates"`
	AdmissionRules        AdmissionRules   `json:"admissionRules"`
	CampaignRules         CampaignRules    `json:"campaignRules"`
	OverrideRules         []OverrideRules  `json:"overrideRules"`
	RequiredEvidence      []string         `json:"requiredEvidence"`
	BootstrapRules        BootstrapRules   `json:"bootstrapRules"`
}
