package campaigns

import (
	"context"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestResolveCampaignMembershipOrdersFrozenTagResults(t *testing.T) {
	g := NewWithT(t)
	draft := campaignDraftFixture()
	draft.Membership = types.CampaignMembership{
		TagQuery: "region=eu && tier=production",
	}
	draft.CandidatePlans[0], draft.CandidatePlans[1] =
		draft.CandidatePlans[1], draft.CandidatePlans[0]

	members, err := ResolveCampaignMembership(context.Background(), draft)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(members).To(HaveLen(2))
	g.Expect(members[0].WaveOrder).To(Equal(1))
	g.Expect(members[0].MemberOrder).To(Equal(1))
	g.Expect(members[0].PlanID).To(Equal(draft.CandidatePlans[1].PlanID))
	g.Expect(members[0].EffectivePolicyChecksum).To(Equal(
		draft.CandidatePlans[1].EffectivePolicyChecksum,
	))
	g.Expect(members[0].ApprovalRequestRevision).To(Equal(
		draft.CandidatePlans[1].ApprovalRequestRevision,
	))
	g.Expect(members[0].ApprovalChecksum).To(Equal(
		draft.CandidatePlans[1].ApprovalChecksum,
	))
	g.Expect(members[0].CalendarChecksums).To(Equal(
		draft.CandidatePlans[1].CalendarChecksums,
	))
	g.Expect(members[0].AdmissionChecksum).To(Equal(
		draft.CandidatePlans[1].AdmissionChecksum,
	))
	g.Expect(members[1].WaveOrder).To(Equal(2))
	g.Expect(members[1].MemberOrder).To(Equal(1))
}

func TestPublishedMembershipDoesNotFollowLaterTagChanges(t *testing.T) {
	g := NewWithT(t)
	draft := campaignDraftFixture()
	draft.Membership = types.CampaignMembership{TagQuery: "tier=production"}
	frozen, err := ResolveCampaignMembership(context.Background(), draft)
	g.Expect(err).NotTo(HaveOccurred())
	revision := types.CampaignRevision{Members: frozen}

	draft.CandidatePlans[0].Tags = []string{"tier=staging"}
	current, err := ResolveCampaignMembership(context.Background(), draft)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(current).To(HaveLen(1))
	g.Expect(revision.Members).To(HaveLen(2))
	g.Expect(revision.Members[0].PlanID).NotTo(Equal(current[0].PlanID))
}

func TestResolveCampaignMembershipRejectsUnknownExplicitPlan(t *testing.T) {
	g := NewWithT(t)
	draft := campaignDraftFixture()
	draft.Membership = types.CampaignMembership{
		PlanIDs: []uuid.UUID{uuid.New()},
	}

	_, err := ResolveCampaignMembership(context.Background(), draft)

	g.Expect(err).To(MatchError(ContainSubstring("selected deployment plan")))
}

func campaignDraftFixture() types.CampaignDraft {
	firstPlanID := uuid.MustParse("30000000-0000-0000-0000-000000000001")
	secondPlanID := uuid.MustParse("30000000-0000-0000-0000-000000000002")
	return types.CampaignDraft{
		ID:             uuid.MustParse("20000000-0000-0000-0000-000000000001"),
		OrganizationID: uuid.MustParse("10000000-0000-0000-0000-000000000001"),
		Name:           "production rollout",
		Revision:       1,
		Waves: []types.CampaignWaveDraft{
			{Order: 1, Name: "canary", PlanIDs: []uuid.UUID{firstPlanID}, BakeSeconds: 3600, MaximumConcurrency: 1},
			{Order: 2, Name: "broad", PlanIDs: []uuid.UUID{secondPlanID}, BakeSeconds: 7200, MaximumConcurrency: 2},
		},
		RiskPolicy: types.CampaignRiskPolicy{
			MaximumConcurrency:          2,
			FailureToleranceBasisPoints: 500,
			MinimumHealthyBasisPoints:   9500,
		},
		CandidatePlans: []types.CampaignPlanCandidate{
			{
				PlanID:                  firstPlanID,
				OrganizationID:          uuid.MustParse("10000000-0000-0000-0000-000000000001"),
				DeploymentUnitID:        uuid.MustParse("40000000-0000-0000-0000-000000000001"),
				PlanChecksum:            "sha256:" + repeatHex("1"),
				CurrentPlanChecksum:     "sha256:" + repeatHex("1"),
				ApprovalRequestID:       uuid.MustParse("50000000-0000-0000-0000-000000000001"),
				ApprovalRequestRevision: 2,
				ApprovalChecksum:        "sha256:" + repeatHex("1"),
				EffectivePolicyChecksum: "sha256:" + repeatHex("3"),
				CalendarVersionIDs: []uuid.UUID{
					uuid.MustParse("70000000-0000-0000-0000-000000000001"),
				},
				CalendarChecksums: []string{"sha256:" + repeatHex("4")},
				AdmissionEvaluationID: uuid.MustParse(
					"80000000-0000-0000-0000-000000000001",
				),
				AdmissionChecksum: "sha256:" + repeatHex("5"),
				Admitted:          true,
				Approved:          true,
				Tags:              []string{"region=eu", "tier=production"},
				ExpectedStepPlacementEvidence: map[types.CampaignStepPlacement]types.CampaignStepPlacementEvidence{
					{
						StepKey:     "database.migrate",
						PlacementID: uuid.MustParse("60000000-0000-0000-0000-000000000001"),
					}: {
						ExpectedObservedStateChecksum: "sha256:" + repeatHex("6"),
						ProviderDeploymentUnitID: uuid.MustParse(
							"40000000-0000-0000-0000-000000000001",
						),
						ProviderComponentInstanceID: uuid.MustParse(
							"90000000-0000-0000-0000-000000000001",
						),
					},
				},
				SharedProviderPlacements: []uuid.UUID{
					uuid.MustParse("60000000-0000-0000-0000-000000000001"),
				},
			},
			{
				PlanID:                  secondPlanID,
				OrganizationID:          uuid.MustParse("10000000-0000-0000-0000-000000000001"),
				DeploymentUnitID:        uuid.MustParse("40000000-0000-0000-0000-000000000002"),
				PlanChecksum:            "sha256:" + repeatHex("2"),
				CurrentPlanChecksum:     "sha256:" + repeatHex("2"),
				ApprovalRequestID:       uuid.MustParse("50000000-0000-0000-0000-000000000002"),
				ApprovalRequestRevision: 3,
				ApprovalChecksum:        "sha256:" + repeatHex("2"),
				EffectivePolicyChecksum: "sha256:" + repeatHex("6"),
				CalendarVersionIDs: []uuid.UUID{
					uuid.MustParse("70000000-0000-0000-0000-000000000002"),
				},
				CalendarChecksums: []string{"sha256:" + repeatHex("7")},
				AdmissionEvaluationID: uuid.MustParse(
					"80000000-0000-0000-0000-000000000002",
				),
				AdmissionChecksum: "sha256:" + repeatHex("8"),
				Admitted:          true,
				Approved:          true,
				Tags:              []string{"region=eu", "tier=production"},
			},
		},
	}
}
