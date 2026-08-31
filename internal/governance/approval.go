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
	return evaluateApproval(request, decisions, uuid.Nil, now)
}

// EvaluateApprovalForAdmission re-evaluates frozen approval evidence for the
// actor that would execute the plan. Callers must supply only approval
// decisions whose actors still hold the requirement's current authority.
func EvaluateApprovalForAdmission(
	request types.ApprovalRequest,
	currentlyAuthorizedDecisions []types.ApprovalDecision,
	executorUserAccountID uuid.UUID,
	now time.Time,
) types.ApprovalEvaluation {
	return evaluateApproval(
		request,
		currentlyAuthorizedDecisions,
		executorUserAccountID,
		now,
	)
}

func evaluateApproval(
	request types.ApprovalRequest,
	decisions []types.ApprovalDecision,
	executorUserAccountID uuid.UUID,
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

	requirements := slices.Clone(request.Requirements)
	sort.Slice(requirements, func(i, j int) bool {
		if requirements[i].SortOrder != requirements[j].SortOrder {
			return requirements[i].SortOrder < requirements[j].SortOrder
		}
		return requirements[i].ID.String() < requirements[j].ID.String()
	})
	approvedActors := approvalActorsByRequirement(request.ID, requirements, decisions)
	if executorUserAccountID != uuid.Nil {
		for _, requirement := range requirements {
			if slices.Contains(
				requirement.SeparationConstraints,
				types.SeparationConstraintExecutorCannotApprove,
			) {
				delete(approvedActors[requirement.ID], executorUserAccountID)
			}
		}
	}
	rejected := approvalRejected(request.ID, decisions)
	distinctApprovedCounts := distinctApprovalCounts(requirements, approvedActors)
	for _, requirement := range requirements {
		actors := approvedActors[requirement.ID]
		approvedCount := len(actors)
		if slices.Contains(
			requirement.SeparationConstraints,
			types.SeparationConstraintDistinctApprovers,
		) {
			approvedCount = distinctApprovedCounts[requirement.ID]
		}
		item := types.ApprovalRequirementEvaluation{
			RequirementID: requirement.ID,
			ApprovedCount: approvedCount,
			RequiredCount: requirement.Quorum,
			Satisfied:     approvedCount >= requirement.Quorum,
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

func approvalActorsByRequirement(
	requestID uuid.UUID,
	requirements []types.ApprovalRequirement,
	decisions []types.ApprovalDecision,
) map[uuid.UUID]map[uuid.UUID]struct{} {
	actors := make(map[uuid.UUID]map[uuid.UUID]struct{}, len(requirements))
	for _, requirement := range requirements {
		actors[requirement.ID] = map[uuid.UUID]struct{}{}
	}
	for _, decision := range decisions {
		if decision.ApprovalRequestID != requestID ||
			decision.Decision != types.ApprovalDecisionApprove {
			continue
		}
		if requirementActors, ok := actors[decision.ApprovalRequirementID]; ok {
			requirementActors[decision.ActorUserAccountID] = struct{}{}
		}
	}
	return actors
}

func approvalRejected(
	requestID uuid.UUID,
	decisions []types.ApprovalDecision,
) bool {
	for _, decision := range decisions {
		if decision.ApprovalRequestID == requestID &&
			decision.Decision == types.ApprovalDecisionReject {
			return true
		}
	}
	return false
}

type approvalSlot struct {
	requirementID uuid.UUID
	actors        []uuid.UUID
}

// distinctApprovalCounts performs a deterministic bipartite matching across
// every requirement that opts into cross-requirement approver separation.
func distinctApprovalCounts(
	requirements []types.ApprovalRequirement,
	approvedActors map[uuid.UUID]map[uuid.UUID]struct{},
) map[uuid.UUID]int {
	counts := make(map[uuid.UUID]int, len(requirements))
	slots := make([]approvalSlot, 0)
	for _, requirement := range requirements {
		if !slices.Contains(
			requirement.SeparationConstraints,
			types.SeparationConstraintDistinctApprovers,
		) {
			continue
		}
		actors := make([]uuid.UUID, 0, len(approvedActors[requirement.ID]))
		for actorID := range approvedActors[requirement.ID] {
			actors = append(actors, actorID)
		}
		sort.Slice(actors, func(i, j int) bool {
			return actors[i].String() < actors[j].String()
		})
		for range max(requirement.Quorum, 0) {
			slots = append(slots, approvalSlot{
				requirementID: requirement.ID,
				actors:        actors,
			})
		}
	}
	actorSlots := map[uuid.UUID]int{}
	for slotIndex := range slots {
		seenActors := map[uuid.UUID]struct{}{}
		if matchApprovalSlot(slotIndex, slots, actorSlots, seenActors) {
			counts[slots[slotIndex].requirementID]++
		}
	}
	return counts
}

func matchApprovalSlot(
	slotIndex int,
	slots []approvalSlot,
	actorSlots map[uuid.UUID]int,
	seenActors map[uuid.UUID]struct{},
) bool {
	for _, actorID := range slots[slotIndex].actors {
		if _, seen := seenActors[actorID]; seen {
			continue
		}
		seenActors[actorID] = struct{}{}
		previousSlot, assigned := actorSlots[actorID]
		if !assigned || matchApprovalSlot(previousSlot, slots, actorSlots, seenActors) {
			actorSlots[actorID] = slotIndex
			return true
		}
	}
	return false
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
