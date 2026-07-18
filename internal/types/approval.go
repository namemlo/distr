package types

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type ApprovalSubjectType string

const (
	ApprovalSubjectDeploymentPlan ApprovalSubjectType = "deployment_plan"
)

func (subjectType ApprovalSubjectType) IsValid() bool {
	return subjectType == ApprovalSubjectDeploymentPlan
}

type ApprovalRequestState string

const (
	ApprovalRequestStatePending     ApprovalRequestState = "PENDING"
	ApprovalRequestStateApproved    ApprovalRequestState = "APPROVED"
	ApprovalRequestStateRejected    ApprovalRequestState = "REJECTED"
	ApprovalRequestStateExpired     ApprovalRequestState = "EXPIRED"
	ApprovalRequestStateSuperseded  ApprovalRequestState = "SUPERSEDED"
	ApprovalRequestStateInvalidated ApprovalRequestState = "INVALIDATED"
)

func (state ApprovalRequestState) IsValid() bool {
	switch state {
	case ApprovalRequestStatePending,
		ApprovalRequestStateApproved,
		ApprovalRequestStateRejected,
		ApprovalRequestStateExpired,
		ApprovalRequestStateSuperseded,
		ApprovalRequestStateInvalidated:
		return true
	default:
		return false
	}
}

func (state ApprovalRequestState) IsActive() bool {
	return state == ApprovalRequestStatePending ||
		state == ApprovalRequestStateApproved
}

type ApprovalDecisionValue string

const (
	ApprovalDecisionApprove ApprovalDecisionValue = "APPROVE"
	ApprovalDecisionReject  ApprovalDecisionValue = "REJECT"
)

func (decision ApprovalDecisionValue) IsValid() bool {
	return decision == ApprovalDecisionApprove ||
		decision == ApprovalDecisionReject
}

type ApprovalInvalidationReason string

const (
	ApprovalInvalidationExpired                  ApprovalInvalidationReason = "expired"
	ApprovalInvalidationSuperseded               ApprovalInvalidationReason = "superseded"
	ApprovalInvalidationPlanChanged              ApprovalInvalidationReason = "plan_changed"
	ApprovalInvalidationPolicyChanged            ApprovalInvalidationReason = "policy_changed"
	ApprovalInvalidationSubscriberSetChanged     ApprovalInvalidationReason = "subscriber_set_changed"
	ApprovalInvalidationCampaignMemberUnapproved ApprovalInvalidationReason = "campaign_member_unapproved"
)

func (reason ApprovalInvalidationReason) IsValid() bool {
	switch reason {
	case ApprovalInvalidationExpired,
		ApprovalInvalidationSuperseded,
		ApprovalInvalidationPlanChanged,
		ApprovalInvalidationPolicyChanged,
		ApprovalInvalidationSubscriberSetChanged,
		ApprovalInvalidationCampaignMemberUnapproved:
		return true
	default:
		return false
	}
}

type ApprovalRequest struct {
	ID                      uuid.UUID                  `db:"id" json:"id"`
	CreatedAt               time.Time                  `db:"created_at" json:"createdAt"`
	UpdatedAt               time.Time                  `db:"updated_at" json:"updatedAt"`
	OrganizationID          uuid.UUID                  `db:"organization_id" json:"organizationId"`
	SubjectType             ApprovalSubjectType        `db:"subject_type" json:"subjectType"`
	SubjectID               uuid.UUID                  `db:"subject_id" json:"subjectId"`
	SubjectRevision         int64                      `db:"subject_revision" json:"subjectRevision"`
	SubjectChecksum         string                     `db:"subject_checksum" json:"subjectChecksum"`
	EffectivePolicyChecksum string                     `db:"effective_policy_checksum" json:"effectivePolicyChecksum"`
	SubscriberSetChecksum   string                     `db:"subscriber_set_checksum" json:"subscriberSetChecksum"`
	RequesterUserAccountID  uuid.UUID                  `db:"requester_useraccount_id" json:"requesterUserAccountId"`
	ExpiresAt               time.Time                  `db:"expires_at" json:"expiresAt"`
	State                   ApprovalRequestState       `db:"state" json:"state"`
	Revision                int64                      `db:"revision" json:"revision"`
	InvalidationReason      ApprovalInvalidationReason `db:"invalidation_reason" json:"invalidationReason,omitempty"`
	InvalidatedAt           *time.Time                 `db:"invalidated_at" json:"invalidatedAt,omitempty"`
	ResolvedAt              *time.Time                 `db:"resolved_at" json:"resolvedAt,omitempty"`
	Requirements            []ApprovalRequirement      `db:"-" json:"requirements"`
	Decisions               []ApprovalDecision         `db:"-" json:"decisions"`
}

type ApprovalRequirement struct {
	ID                    uuid.UUID              `db:"id" json:"id"`
	CreatedAt             time.Time              `db:"created_at" json:"createdAt"`
	OrganizationID        uuid.UUID              `db:"organization_id" json:"organizationId"`
	ApprovalRequestID     uuid.UUID              `db:"approval_request_id" json:"approvalRequestId"`
	RuleKey               string                 `db:"rule_key" json:"ruleKey"`
	PolicyVersionID       uuid.UUID              `db:"policy_version_id" json:"policyVersionId"`
	AuthorityKind         PolicyAuthorityKind    `db:"authority_kind" json:"authorityKind"`
	AuthorityID           uuid.UUID              `db:"authority_id" json:"authorityId"`
	PrincipalGroupID      uuid.UUID              `db:"principal_group_id" json:"principalGroupId"`
	Quorum                int                    `db:"quorum" json:"quorum"`
	SeparationConstraints []SeparationConstraint `db:"separation_constraints" json:"separationConstraints"`
	SortOrder             int                    `db:"sort_order" json:"sortOrder"`
}

type ApprovalDecision struct {
	ID                    uuid.UUID             `db:"id" json:"id"`
	CreatedAt             time.Time             `db:"created_at" json:"createdAt"`
	OrganizationID        uuid.UUID             `db:"organization_id" json:"organizationId"`
	ApprovalRequestID     uuid.UUID             `db:"approval_request_id" json:"approvalRequestId"`
	ApprovalRequirementID uuid.UUID             `db:"approval_requirement_id" json:"approvalRequirementId"`
	ActorUserAccountID    uuid.UUID             `db:"actor_useraccount_id" json:"actorUserAccountId"`
	Decision              ApprovalDecisionValue `db:"decision" json:"decision"`
	Comment               string                `db:"comment" json:"comment"`
	RequestRevision       int64                 `db:"request_revision" json:"requestRevision"`
	IdempotencyKey        string                `db:"idempotency_key" json:"idempotencyKey"`
}

type ApprovalRequestInput struct {
	OrganizationID           uuid.UUID
	DeploymentPlanID         uuid.UUID
	RequestedByUserAccountID uuid.UUID
	ExpiresAt                time.Time
	Authorize                ApprovalAuthorizer
}

type ApprovalDecisionInput struct {
	OrganizationID          uuid.UUID
	ApprovalRequestID       uuid.UUID
	ApprovalRequirementID   uuid.UUID
	ActorUserAccountID      uuid.UUID
	Decision                ApprovalDecisionValue
	Comment                 string
	ExpectedRequestRevision int64
	IdempotencyKey          string
	Authorize               ApprovalAuthorizer
}

type ApprovalAuthorizationContext struct {
	OrganizationID        uuid.UUID
	ActorUserAccountID    uuid.UUID
	DecisionAt            time.Time
	DeploymentPlanID      uuid.UUID
	ApprovalRequestID     uuid.UUID
	ApprovalRequirementID uuid.UUID
}

type ApprovalAuthorizer func(context.Context, ApprovalAuthorizationContext) error

type ApprovalSubjectSnapshot struct {
	SubjectType             ApprovalSubjectType
	SubjectID               uuid.UUID
	SubjectRevision         int64
	SubjectChecksum         string
	EffectivePolicyChecksum string
	SubscriberSetChecksum   string
}

type ApprovalRequirementEvaluation struct {
	RequirementID uuid.UUID `json:"requirementId"`
	ApprovedCount int       `json:"approvedCount"`
	RequiredCount int       `json:"requiredCount"`
	Satisfied     bool      `json:"satisfied"`
}

type ApprovalEvaluation struct {
	RequestID             uuid.UUID                       `json:"requestId"`
	State                 ApprovalRequestState            `json:"state"`
	Eligible              bool                            `json:"eligible"`
	InvalidationReason    ApprovalInvalidationReason      `json:"invalidationReason,omitempty"`
	Requirements          []ApprovalRequirementEvaluation `json:"requirements"`
	MissingRequirementIDs []uuid.UUID                     `json:"missingRequirementIds"`
}

type ApprovalRequestListFilter struct {
	OrganizationID uuid.UUID
	State          ApprovalRequestState
	Cursor         string
	Limit          int
}
