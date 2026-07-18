package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func ApprovalRequestToAPI(request types.ApprovalRequest) api.ApprovalRequest {
	requirements := make([]api.ApprovalRequirement, len(request.Requirements))
	for index, requirement := range request.Requirements {
		requirements[index] = ApprovalRequirementToAPI(requirement)
	}
	decisions := make([]api.ApprovalDecision, len(request.Decisions))
	for index, decision := range request.Decisions {
		decisions[index] = ApprovalDecisionToAPI(decision)
	}
	return api.ApprovalRequest{
		ID:                      request.ID,
		CreatedAt:               request.CreatedAt,
		UpdatedAt:               request.UpdatedAt,
		SubjectType:             request.SubjectType,
		SubjectID:               request.SubjectID,
		SubjectRevision:         request.SubjectRevision,
		SubjectChecksum:         request.SubjectChecksum,
		EffectivePolicyChecksum: request.EffectivePolicyChecksum,
		SubscriberSetChecksum:   request.SubscriberSetChecksum,
		RequesterUserAccountID:  request.RequesterUserAccountID,
		ExpiresAt:               request.ExpiresAt,
		State:                   request.State,
		Revision:                request.Revision,
		InvalidationReason:      request.InvalidationReason,
		InvalidatedAt:           request.InvalidatedAt,
		ResolvedAt:              request.ResolvedAt,
		Requirements:            requirements,
		Decisions:               decisions,
	}
}

func ApprovalRequirementToAPI(
	requirement types.ApprovalRequirement,
) api.ApprovalRequirement {
	return api.ApprovalRequirement{
		ID:               requirement.ID,
		RuleKey:          requirement.RuleKey,
		PolicyVersionID:  requirement.PolicyVersionID,
		AuthorityKind:    requirement.AuthorityKind,
		AuthorityID:      requirement.AuthorityID,
		PrincipalGroupID: requirement.PrincipalGroupID,
		Quorum:           requirement.Quorum,
		SeparationConstraints: append(
			[]types.SeparationConstraint{},
			requirement.SeparationConstraints...,
		),
		SortOrder: requirement.SortOrder,
	}
}

func ApprovalDecisionToAPI(decision types.ApprovalDecision) api.ApprovalDecision {
	return api.ApprovalDecision{
		ID:                    decision.ID,
		CreatedAt:             decision.CreatedAt,
		ApprovalRequestID:     decision.ApprovalRequestID,
		ApprovalRequirementID: decision.ApprovalRequirementID,
		ActorUserAccountID:    decision.ActorUserAccountID,
		Decision:              decision.Decision,
		Comment:               decision.Comment,
		RequestRevision:       decision.RequestRevision,
		IdempotencyKey:        decision.IdempotencyKey,
	}
}

func ApprovalRequestPageToAPI(
	page types.Page[types.ApprovalRequest],
) api.ApprovalRequestPage {
	items := make([]api.ApprovalRequest, len(page.Items))
	for index, request := range page.Items {
		items[index] = ApprovalRequestToAPI(request)
	}
	return api.ApprovalRequestPage{
		Items:      items,
		NextCursor: page.NextCursor,
	}
}
