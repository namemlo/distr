package governance

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func EvaluateApproval(
	request types.ApprovalRequest,
	decisions []types.ApprovalDecision,
	now time.Time,
) types.ApprovalEvaluation {
	evaluation := types.ApprovalEvaluation{
		RequestID:             request.ID,
		State:                 request.State,
		Requirements:          []types.ApprovalRequirementEvaluation{},
		MissingRequirementIDs: []uuid.UUID{},
	}
	if reason := DetectApprovalInvalidation(request, subjectSnapshot(request), now); reason != "" {
		evaluation.InvalidationReason = reason
		evaluation.State = approvalStateForInvalidation(reason)
		return evaluation
	}
	if !request.State.IsActive() {
		return evaluation
	}
	if len(request.Requirements) == 0 {
		evaluation.State = types.ApprovalRequestStatePending
		return evaluation
	}

	rejected := false
	requirements := slices.Clone(request.Requirements)
	sort.Slice(requirements, func(i, j int) bool {
		if requirements[i].SortOrder != requirements[j].SortOrder {
			return requirements[i].SortOrder < requirements[j].SortOrder
		}
		return requirements[i].ID.String() < requirements[j].ID.String()
	})
	for _, requirement := range requirements {
		actors := map[uuid.UUID]struct{}{}
		for _, decision := range decisions {
			if decision.ApprovalRequestID != request.ID ||
				decision.ApprovalRequirementID != requirement.ID {
				continue
			}
			if decision.Decision == types.ApprovalDecisionReject {
				rejected = true
				continue
			}
			if decision.Decision == types.ApprovalDecisionApprove {
				actors[decision.ActorUserAccountID] = struct{}{}
			}
		}
		item := types.ApprovalRequirementEvaluation{
			RequirementID: requirement.ID,
			ApprovedCount: len(actors),
			RequiredCount: requirement.Quorum,
			Satisfied:     len(actors) >= requirement.Quorum,
		}
		evaluation.Requirements = append(evaluation.Requirements, item)
		if !item.Satisfied {
			evaluation.MissingRequirementIDs = append(
				evaluation.MissingRequirementIDs,
				requirement.ID,
			)
		}
	}
	if rejected {
		evaluation.State = types.ApprovalRequestStateRejected
		return evaluation
	}
	if len(evaluation.MissingRequirementIDs) == 0 {
		evaluation.State = types.ApprovalRequestStateApproved
		evaluation.Eligible = true
	} else {
		evaluation.State = types.ApprovalRequestStatePending
	}
	return evaluation
}

func ValidateApprovalDecision(
	request types.ApprovalRequest,
	requirement types.ApprovalRequirement,
	existing []types.ApprovalDecision,
	input types.ApprovalDecisionInput,
	actorInRequiredGroup bool,
	now time.Time,
) error {
	if input.OrganizationID == uuid.Nil ||
		input.ApprovalRequestID == uuid.Nil ||
		input.ApprovalRequirementID == uuid.Nil ||
		input.ActorUserAccountID == uuid.Nil {
		return apierrors.NewBadRequest("approval decision identity is required")
	}
	if input.OrganizationID != request.OrganizationID ||
		input.ApprovalRequestID != request.ID ||
		requirement.OrganizationID != request.OrganizationID ||
		requirement.ApprovalRequestID != request.ID ||
		input.ApprovalRequirementID != requirement.ID {
		return apierrors.ErrNotFound
	}
	if !actorInRequiredGroup {
		return apierrors.ErrForbidden
	}
	if !request.State.IsActive() || request.State == types.ApprovalRequestStateApproved {
		return apierrors.NewConflict("approval request is not pending")
	}
	if !now.Before(request.ExpiresAt) {
		return apierrors.NewConflict("approval request has expired")
	}
	if input.ExpectedRequestRevision != request.Revision {
		return apierrors.NewConflict("approval request revision changed")
	}
	if !input.Decision.IsValid() {
		return apierrors.NewBadRequest("decision is invalid")
	}
	if strings.TrimSpace(input.Comment) == "" || len(input.Comment) > 4096 {
		return apierrors.NewBadRequest("comment is required and must contain at most 4096 characters")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" ||
		len(input.IdempotencyKey) > 128 {
		return apierrors.NewBadRequest("idempotencyKey is invalid")
	}
	if input.Decision == types.ApprovalDecisionApprove &&
		input.ActorUserAccountID == request.RequesterUserAccountID &&
		(slices.Contains(
			requirement.SeparationConstraints,
			types.SeparationConstraintRequesterCannotApprove,
		) ||
			slices.Contains(
				requirement.SeparationConstraints,
				types.SeparationConstraintPublisherCannotApprove,
			)) {
		return apierrors.NewForbidden("requester cannot approve this deployment")
	}
	for _, decision := range existing {
		if decision.ApprovalRequirementID == requirement.ID &&
			decision.ActorUserAccountID == input.ActorUserAccountID {
			return apierrors.NewConflict("actor already recorded a decision for this requirement")
		}
	}
	return nil
}

func DetectApprovalInvalidation(
	request types.ApprovalRequest,
	current types.ApprovalSubjectSnapshot,
	now time.Time,
) types.ApprovalInvalidationReason {
	if request.State == types.ApprovalRequestStateSuperseded {
		return types.ApprovalInvalidationSuperseded
	}
	if request.State == types.ApprovalRequestStateExpired {
		return types.ApprovalInvalidationExpired
	}
	if request.State == types.ApprovalRequestStateInvalidated &&
		request.InvalidationReason.IsValid() {
		return request.InvalidationReason
	}
	if request.State == types.ApprovalRequestStateRejected {
		return ""
	}
	if !now.Before(request.ExpiresAt) {
		return types.ApprovalInvalidationExpired
	}
	if current.SubjectType != request.SubjectType ||
		current.SubjectID != request.SubjectID ||
		current.SubjectRevision != request.SubjectRevision ||
		current.SubjectChecksum != request.SubjectChecksum {
		return types.ApprovalInvalidationPlanChanged
	}
	if current.EffectivePolicyChecksum != request.EffectivePolicyChecksum {
		return types.ApprovalInvalidationPolicyChanged
	}
	if current.SubscriberSetChecksum != request.SubscriberSetChecksum {
		return types.ApprovalInvalidationSubscriberSetChanged
	}
	return ""
}

func RequireApprovedCampaignMember(evaluation types.ApprovalEvaluation) error {
	if !evaluation.Eligible ||
		evaluation.State != types.ApprovalRequestStateApproved {
		return apierrors.NewConflict(
			"campaign member requires an approved checksum-bound deployment plan",
		)
	}
	return nil
}

func subjectSnapshot(request types.ApprovalRequest) types.ApprovalSubjectSnapshot {
	return types.ApprovalSubjectSnapshot{
		SubjectType:             request.SubjectType,
		SubjectID:               request.SubjectID,
		SubjectRevision:         request.SubjectRevision,
		SubjectChecksum:         request.SubjectChecksum,
		EffectivePolicyChecksum: request.EffectivePolicyChecksum,
		SubscriberSetChecksum:   request.SubscriberSetChecksum,
	}
}

func approvalStateForInvalidation(
	reason types.ApprovalInvalidationReason,
) types.ApprovalRequestState {
	switch reason {
	case types.ApprovalInvalidationExpired:
		return types.ApprovalRequestStateExpired
	case types.ApprovalInvalidationSuperseded:
		return types.ApprovalRequestStateSuperseded
	default:
		return types.ApprovalRequestStateInvalidated
	}
}
