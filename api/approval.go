package api

import (
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

const (
	approvalMaximumPageLimit  = 100
	approvalMaximumCursorSize = 2048
)

var (
	approvalCursorPattern         = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	approvalIdempotencyKeyPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
	)
)

type CreateApprovalRequestRequest struct {
	ExpiresAt time.Time `json:"expiresAt"`
}

func (request CreateApprovalRequestRequest) Validate(now time.Time) error {
	if !request.ExpiresAt.After(now) {
		return validation.NewValidationFailedError("expiresAt must be in the future")
	}
	if request.ExpiresAt.After(now.Add(366 * 24 * time.Hour)) {
		return validation.NewValidationFailedError("expiresAt must be within 366 days")
	}
	return nil
}

type RecordApprovalDecisionRequest struct {
	ApprovalRequirementID   uuid.UUID                   `json:"approvalRequirementId"`
	Decision                types.ApprovalDecisionValue `json:"decision"`
	Comment                 string                      `json:"comment"`
	ExpectedRequestRevision int64                       `json:"expectedRequestRevision"`
	IdempotencyKey          string                      `json:"idempotencyKey"`
}

func (request *RecordApprovalDecisionRequest) Validate() error {
	request.Comment = strings.TrimSpace(request.Comment)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.ApprovalRequirementID == uuid.Nil {
		return validation.NewValidationFailedError("approvalRequirementId is required")
	}
	if !request.Decision.IsValid() {
		return validation.NewValidationFailedError("decision must be APPROVE or REJECT")
	}
	if request.Comment == "" {
		return validation.NewValidationFailedError("comment is required")
	}
	if len(request.Comment) > 4096 {
		return validation.NewValidationFailedError(
			"comment must contain at most 4096 characters",
		)
	}
	if request.ExpectedRequestRevision < 1 {
		return validation.NewValidationFailedError(
			"expectedRequestRevision must be greater than zero",
		)
	}
	if !approvalIdempotencyKeyPattern.MatchString(request.IdempotencyKey) {
		return validation.NewValidationFailedError(
			"idempotencyKey must be 1-128 URL-safe characters",
		)
	}
	return nil
}

type ApprovalRequestListRequest struct {
	State  types.ApprovalRequestState `query:"state"`
	Cursor string                     `query:"cursor"`
	Limit  int                        `query:"limit"`
}

func (request ApprovalRequestListRequest) Validate() error {
	if request.State != "" && !request.State.IsValid() {
		return validation.NewValidationFailedError("state is invalid")
	}
	if request.Limit < 0 || request.Limit > approvalMaximumPageLimit {
		return validation.NewValidationFailedError("limit must be between 1 and 100")
	}
	if len(request.Cursor) > approvalMaximumCursorSize {
		return validation.NewValidationFailedError("cursor is too large")
	}
	if request.Cursor != "" && !approvalCursorPattern.MatchString(request.Cursor) {
		return validation.NewValidationFailedError(
			"cursor must be an opaque URL-safe token",
		)
	}
	return nil
}

type ApprovalRequirement struct {
	ID                    uuid.UUID                    `json:"id"`
	RuleKey               string                       `json:"ruleKey"`
	PolicyVersionID       uuid.UUID                    `json:"policyVersionId"`
	AuthorityKind         types.PolicyAuthorityKind    `json:"authorityKind"`
	AuthorityID           uuid.UUID                    `json:"authorityId"`
	PrincipalGroupID      uuid.UUID                    `json:"principalGroupId"`
	Quorum                int                          `json:"quorum"`
	SeparationConstraints []types.SeparationConstraint `json:"separationConstraints"`
	SortOrder             int                          `json:"sortOrder"`
}

type ApprovalDecision struct {
	ID                    uuid.UUID                   `json:"id"`
	CreatedAt             time.Time                   `json:"createdAt"`
	ApprovalRequestID     uuid.UUID                   `json:"approvalRequestId"`
	ApprovalRequirementID uuid.UUID                   `json:"approvalRequirementId"`
	ActorUserAccountID    uuid.UUID                   `json:"actorUserAccountId"`
	Decision              types.ApprovalDecisionValue `json:"decision"`
	Comment               string                      `json:"comment"`
	RequestRevision       int64                       `json:"requestRevision"`
	IdempotencyKey        string                      `json:"idempotencyKey"`
}

type ApprovalRequest struct {
	ID                      uuid.UUID                        `json:"id"`
	CreatedAt               time.Time                        `json:"createdAt"`
	UpdatedAt               time.Time                        `json:"updatedAt"`
	SubjectType             types.ApprovalSubjectType        `json:"subjectType"`
	SubjectID               uuid.UUID                        `json:"subjectId"`
	SubjectRevision         int64                            `json:"subjectRevision"`
	SubjectChecksum         string                           `json:"subjectChecksum"`
	EffectivePolicyChecksum string                           `json:"effectivePolicyChecksum"`
	SubscriberSetChecksum   string                           `json:"subscriberSetChecksum"`
	RequesterUserAccountID  uuid.UUID                        `json:"requesterUserAccountId"`
	ExpiresAt               time.Time                        `json:"expiresAt"`
	State                   types.ApprovalRequestState       `json:"state"`
	Revision                int64                            `json:"revision"`
	InvalidationReason      types.ApprovalInvalidationReason `json:"invalidationReason,omitempty"`
	InvalidatedAt           *time.Time                       `json:"invalidatedAt,omitempty"`
	ResolvedAt              *time.Time                       `json:"resolvedAt,omitempty"`
	Requirements            []ApprovalRequirement            `json:"requirements"`
	Decisions               []ApprovalDecision               `json:"decisions"`
}

type ApprovalRequestPage struct {
	Items      []ApprovalRequest `json:"items"`
	NextCursor string            `json:"nextCursor,omitempty"`
}
