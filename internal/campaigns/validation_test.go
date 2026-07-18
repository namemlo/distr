package campaigns

import (
	"context"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCampaignRevisionValidationRejectsUnapprovedAndChangedPlans(t *testing.T) {
	g := NewWithT(t)
	draft := campaignDraftFixture()
	draft.Membership.PlanIDs = []uuid.UUID{
		draft.CandidatePlans[0].PlanID,
		draft.CandidatePlans[1].PlanID,
	}
	draft.CandidatePlans[0].Approved = false
	draft.CandidatePlans[1].CurrentPlanChecksum = "sha256:" + repeatHex("8")

	issues := ValidateCampaignDraft(context.Background(), draft)

	g.Expect(campaignIssueCodes(issues)).To(ContainElements(
		"campaign.member.unapproved",
		"campaign.member.plan_checksum_mismatch",
	))
}

func TestCampaignRevisionValidationRejectsDuplicateDeploymentUnit(t *testing.T) {
	g := NewWithT(t)
	draft := campaignDraftFixture()
	draft.Membership.PlanIDs = []uuid.UUID{
		draft.CandidatePlans[0].PlanID,
		draft.CandidatePlans[1].PlanID,
	}
	draft.CandidatePlans[1].DeploymentUnitID =
		draft.CandidatePlans[0].DeploymentUnitID

	issues := ValidateCampaignDraft(context.Background(), draft)

	g.Expect(campaignIssueCodes(issues)).To(ContainElement(
		"campaign.member.duplicate_deployment_unit",
	))
}

func TestCampaignRevisionValidationAcceptsFrozenSharedProviderPrerequisite(t *testing.T) {
	g := NewWithT(t)
	draft := campaignDraftFixture()
	draft.Membership.PlanIDs = []uuid.UUID{
		draft.CandidatePlans[0].PlanID,
		draft.CandidatePlans[1].PlanID,
	}
	draft.Prerequisites = []types.CampaignPrerequisiteDraft{{
		DownstreamPlanID:    draft.CandidatePlans[1].PlanID,
		UpstreamPlanID:      draft.CandidatePlans[0].PlanID,
		UpstreamStepKey:     "database.migrate",
		ProviderPlacementID: draft.CandidatePlans[0].SharedProviderPlacements[0],
		ExpectedObservedStateChecksum: draft.CandidatePlans[0].
			ExpectedStepPlacementChecksums[types.CampaignStepPlacement{
			StepKey:     "database.migrate",
			PlacementID: draft.CandidatePlans[0].SharedProviderPlacements[0],
		}],
	}}

	issues := ValidateCampaignDraft(context.Background(), draft)

	g.Expect(issues).To(BeEmpty())
}

func TestCampaignRevisionValidationRejectsIndependentStepAndPlacementEvidence(t *testing.T) {
	g := NewWithT(t)
	draft := campaignDraftFixture()
	draft.Membership.PlanIDs = []uuid.UUID{
		draft.CandidatePlans[0].PlanID,
		draft.CandidatePlans[1].PlanID,
	}
	otherPlacement := uuid.MustParse("60000000-0000-0000-0000-000000000009")
	draft.CandidatePlans[0].SharedProviderPlacements = append(
		draft.CandidatePlans[0].SharedProviderPlacements,
		otherPlacement,
	)
	draft.Prerequisites = []types.CampaignPrerequisiteDraft{{
		DownstreamPlanID:              draft.CandidatePlans[1].PlanID,
		UpstreamPlanID:                draft.CandidatePlans[0].PlanID,
		UpstreamStepKey:               "database.migrate",
		ProviderPlacementID:           otherPlacement,
		ExpectedObservedStateChecksum: "sha256:" + repeatHex("6"),
	}}

	issues := ValidateCampaignDraft(context.Background(), draft)

	g.Expect(campaignIssueCodes(issues)).To(ContainElement(
		"campaign.prerequisite.observation_checksum_mismatch",
	))
}

func TestCampaignRevisionValidationRejectsMissingAdmissionEvidence(t *testing.T) {
	g := NewWithT(t)
	draft := campaignDraftFixture()
	draft.Membership.PlanIDs = []uuid.UUID{draft.CandidatePlans[0].PlanID}
	draft.Waves = draft.Waves[:1]
	draft.CandidatePlans[0].Admitted = false

	issues := ValidateCampaignDraft(context.Background(), draft)

	g.Expect(campaignIssueCodes(issues)).To(ContainElement(
		"campaign.member.admission_invalid",
	))
}

func TestCampaignRevisionValidationRejectsObservationExpectationMismatch(t *testing.T) {
	g := NewWithT(t)
	draft := campaignDraftFixture()
	draft.Membership.PlanIDs = []uuid.UUID{
		draft.CandidatePlans[0].PlanID,
		draft.CandidatePlans[1].PlanID,
	}
	draft.Prerequisites = []types.CampaignPrerequisiteDraft{{
		DownstreamPlanID:              draft.CandidatePlans[1].PlanID,
		UpstreamPlanID:                draft.CandidatePlans[0].PlanID,
		UpstreamStepKey:               "database.migrate",
		ProviderPlacementID:           draft.CandidatePlans[0].SharedProviderPlacements[0],
		ExpectedObservedStateChecksum: "sha256:" + repeatHex("7"),
	}}

	issues := ValidateCampaignDraft(context.Background(), draft)

	g.Expect(campaignIssueCodes(issues)).To(ContainElement(
		"campaign.prerequisite.observation_checksum_mismatch",
	))
}

func TestCampaignRevisionValidationRejectsInvalidOrDecreasingBake(t *testing.T) {
	g := NewWithT(t)
	draft := campaignDraftFixture()
	draft.Membership.PlanIDs = []uuid.UUID{
		draft.CandidatePlans[0].PlanID,
		draft.CandidatePlans[1].PlanID,
	}
	draft.Waves[0].BakeSeconds = 3600
	draft.Waves[1].BakeSeconds = 1800
	draft.Waves = append(draft.Waves, types.CampaignWaveDraft{
		Order:              3,
		Name:               "invalid",
		BakeSeconds:        -1,
		MaximumConcurrency: 1,
	})

	issues := ValidateCampaignDraft(context.Background(), draft)

	g.Expect(campaignIssueCodes(issues)).To(ContainElements(
		"campaign.wave.bake_decreased",
		"campaign.wave.bake_invalid",
	))
}

func campaignIssueCodes(issues []types.ValidationIssue) []string {
	result := make([]string, len(issues))
	for index := range issues {
		result[index] = issues[index].Code
	}
	return result
}
