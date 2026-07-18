package governance

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestApprovalDecisionDeniesRequesterSelfApproval(t *testing.T) {
	now := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
	requesterID := uuid.New()
	request, requirement := pendingApprovalFixture(now, requesterID)

	err := ValidateApprovalDecision(
		request,
		requirement,
		nil,
		types.ApprovalDecisionInput{
			OrganizationID:          request.OrganizationID,
			ApprovalRequestID:       request.ID,
			ApprovalRequirementID:   requirement.ID,
			ActorUserAccountID:      requesterID,
			Decision:                types.ApprovalDecisionApprove,
			Comment:                 "Reviewed the exact deployment plan.",
			ExpectedRequestRevision: request.Revision,
			IdempotencyKey:          "approve-plan-1",
		},
		true,
		now,
	)

	NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
		"requester cannot approve",
	)))

	requirement.SeparationConstraints = []types.SeparationConstraint{
		types.SeparationConstraintPublisherCannotApprove,
	}
	err = ValidateApprovalDecision(
		request,
		requirement,
		nil,
		types.ApprovalDecisionInput{
			OrganizationID:          request.OrganizationID,
			ApprovalRequestID:       request.ID,
			ApprovalRequirementID:   requirement.ID,
			ActorUserAccountID:      requesterID,
			Decision:                types.ApprovalDecisionApprove,
			Comment:                 "Reviewed the exact deployment plan.",
			ExpectedRequestRevision: request.Revision,
			IdempotencyKey:          "approve-plan-publisher",
		},
		true,
		now,
	)
	NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
		"requester cannot approve",
	)))
}

func TestApprovalDecisionDeniesActorOutsideRequiredGroup(t *testing.T) {
	now := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
	request, requirement := pendingApprovalFixture(now, uuid.New())

	err := ValidateApprovalDecision(
		request,
		requirement,
		nil,
		types.ApprovalDecisionInput{
			OrganizationID:          request.OrganizationID,
			ApprovalRequestID:       request.ID,
			ApprovalRequirementID:   requirement.ID,
			ActorUserAccountID:      uuid.New(),
			Decision:                types.ApprovalDecisionApprove,
			Comment:                 "Reviewed the exact deployment plan.",
			ExpectedRequestRevision: request.Revision,
			IdempotencyKey:          "approve-plan-1",
		},
		false,
		now,
	)

	NewWithT(t).Expect(err).To(MatchError(apierrors.ErrForbidden))
}

func TestApprovalEvaluationRequiresIndependentSubscriberQuorums(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
	request, ownerRequirement := pendingApprovalFixture(now, uuid.New())
	ownerRequirement.Quorum = 1
	subscriberA := ownerRequirement
	subscriberA.ID = uuid.New()
	subscriberA.RuleKey = "subscriber-a"
	subscriberA.AuthorityKind = types.PolicyAuthoritySubscriber
	subscriberA.AuthorityID = uuid.New()
	subscriberB := subscriberA
	subscriberB.ID = uuid.New()
	subscriberB.RuleKey = "subscriber-b"
	subscriberB.AuthorityID = uuid.New()
	request.Requirements = []types.ApprovalRequirement{
		ownerRequirement,
		subscriberA,
		subscriberB,
	}

	decisions := []types.ApprovalDecision{
		approvedDecision(request, ownerRequirement, uuid.New(), 1),
		approvedDecision(request, subscriberA, uuid.New(), 2),
	}
	evaluation := EvaluateApproval(request, decisions, now)

	g.Expect(evaluation.Eligible).To(BeFalse())
	g.Expect(evaluation.State).To(Equal(types.ApprovalRequestStatePending))
	g.Expect(evaluation.MissingRequirementIDs).To(Equal([]uuid.UUID{subscriberB.ID}))

	decisions = append(decisions, approvedDecision(request, subscriberB, uuid.New(), 3))
	evaluation = EvaluateApproval(request, decisions, now)
	g.Expect(evaluation.Eligible).To(BeTrue())
	g.Expect(evaluation.State).To(Equal(types.ApprovalRequestStateApproved))
	g.Expect(evaluation.MissingRequirementIDs).To(BeEmpty())
}

func TestApprovalWithoutRequirementsFailsClosed(t *testing.T) {
	now := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
	request, _ := pendingApprovalFixture(now, uuid.New())
	request.Requirements = nil

	evaluation := EvaluateApproval(request, nil, now)

	g := NewWithT(t)
	g.Expect(evaluation.Eligible).To(BeFalse())
	g.Expect(evaluation.State).To(Equal(types.ApprovalRequestStatePending))
	g.Expect(evaluation.Requirements).To(BeEmpty())
}

func TestApprovalDecisionRejectsConflictingOptimisticRevision(t *testing.T) {
	now := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
	request, requirement := pendingApprovalFixture(now, uuid.New())

	err := ValidateApprovalDecision(
		request,
		requirement,
		nil,
		types.ApprovalDecisionInput{
			OrganizationID:          request.OrganizationID,
			ApprovalRequestID:       request.ID,
			ApprovalRequirementID:   requirement.ID,
			ActorUserAccountID:      uuid.New(),
			Decision:                types.ApprovalDecisionApprove,
			Comment:                 "Reviewed the exact deployment plan.",
			ExpectedRequestRevision: request.Revision - 1,
			IdempotencyKey:          "stale-decision",
		},
		true,
		now,
	)

	NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
		"approval request revision changed",
	)))
}

func TestApprovalInvalidationDetectsExpiryAndMaterialChanges(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
	request, _ := pendingApprovalFixture(now, uuid.New())
	current := types.ApprovalSubjectSnapshot{
		SubjectType:             request.SubjectType,
		SubjectID:               request.SubjectID,
		SubjectRevision:         request.SubjectRevision,
		SubjectChecksum:         request.SubjectChecksum,
		EffectivePolicyChecksum: request.EffectivePolicyChecksum,
		SubscriberSetChecksum:   request.SubscriberSetChecksum,
	}

	g.Expect(DetectApprovalInvalidation(request, current, now)).To(Equal(
		types.ApprovalInvalidationReason(""),
	))

	changed := current
	changed.SubjectChecksum = testChecksum("d")
	g.Expect(DetectApprovalInvalidation(request, changed, now)).To(Equal(
		types.ApprovalInvalidationPlanChanged,
	))

	changed = current
	changed.EffectivePolicyChecksum = testChecksum("e")
	g.Expect(DetectApprovalInvalidation(request, changed, now)).To(Equal(
		types.ApprovalInvalidationPolicyChanged,
	))

	changed = current
	changed.SubscriberSetChecksum = testChecksum("f")
	g.Expect(DetectApprovalInvalidation(request, changed, now)).To(Equal(
		types.ApprovalInvalidationSubscriberSetChanged,
	))

	g.Expect(DetectApprovalInvalidation(
		request,
		current,
		request.ExpiresAt,
	)).To(Equal(types.ApprovalInvalidationExpired))

	request.State = types.ApprovalRequestStateSuperseded
	g.Expect(DetectApprovalInvalidation(request, current, now)).To(Equal(
		types.ApprovalInvalidationSuperseded,
	))

	request.State = types.ApprovalRequestStateRejected
	g.Expect(DetectApprovalInvalidation(
		request,
		current,
		request.ExpiresAt.Add(time.Hour),
	)).To(BeEmpty())

	request.State = types.ApprovalRequestStateInvalidated
	request.InvalidationReason = types.ApprovalInvalidationPolicyChanged
	g.Expect(DetectApprovalInvalidation(
		request,
		current,
		request.ExpiresAt.Add(time.Hour),
	)).To(Equal(types.ApprovalInvalidationPolicyChanged))
}

func TestUnapprovedCampaignMemberIsBlocked(t *testing.T) {
	g := NewWithT(t)
	err := RequireApprovedCampaignMember(types.ApprovalEvaluation{
		State:    types.ApprovalRequestStatePending,
		Eligible: false,
	})
	g.Expect(err).To(MatchError(ContainSubstring("campaign member requires")))

	g.Expect(RequireApprovedCampaignMember(types.ApprovalEvaluation{
		State:    types.ApprovalRequestStateApproved,
		Eligible: true,
	})).To(Succeed())
}

func pendingApprovalFixture(
	now time.Time,
	requesterID uuid.UUID,
) (types.ApprovalRequest, types.ApprovalRequirement) {
	requestID := uuid.New()
	organizationID := uuid.New()
	requirement := types.ApprovalRequirement{
		ID:                uuid.New(),
		OrganizationID:    organizationID,
		ApprovalRequestID: requestID,
		RuleKey:           "four-eyes",
		PolicyVersionID:   uuid.New(),
		AuthorityKind:     types.PolicyAuthorityOwner,
		AuthorityID:       uuid.New(),
		PrincipalGroupID:  uuid.New(),
		Quorum:            1,
		SeparationConstraints: []types.SeparationConstraint{
			types.SeparationConstraintRequesterCannotApprove,
			types.SeparationConstraintDistinctApprovers,
		},
	}
	request := types.ApprovalRequest{
		ID:                      requestID,
		OrganizationID:          organizationID,
		SubjectType:             types.ApprovalSubjectDeploymentPlan,
		SubjectID:               uuid.New(),
		SubjectRevision:         1,
		SubjectChecksum:         testChecksum("a"),
		EffectivePolicyChecksum: testChecksum("b"),
		SubscriberSetChecksum:   testChecksum("c"),
		RequesterUserAccountID:  requesterID,
		ExpiresAt:               now.Add(time.Hour),
		State:                   types.ApprovalRequestStatePending,
		Revision:                1,
		Requirements:            []types.ApprovalRequirement{requirement},
	}
	return request, requirement
}

func approvedDecision(
	request types.ApprovalRequest,
	requirement types.ApprovalRequirement,
	actorID uuid.UUID,
	revision int64,
) types.ApprovalDecision {
	return types.ApprovalDecision{
		ID:                    uuid.New(),
		OrganizationID:        request.OrganizationID,
		ApprovalRequestID:     request.ID,
		ApprovalRequirementID: requirement.ID,
		ActorUserAccountID:    actorID,
		Decision:              types.ApprovalDecisionApprove,
		RequestRevision:       revision,
		IdempotencyKey:        uuid.NewString(),
	}
}

func testChecksum(character string) string {
	result := "sha256:"
	for range 64 {
		result += character
	}
	return result
}
