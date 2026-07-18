package api

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCampaignRevisionDraftRequestValidatesBoundedFrozenInputs(t *testing.T) {
	g := NewWithT(t)
	request := validDeploymentCampaignDraftRequest()

	g.Expect(request.Validate()).To(Succeed())

	request.Waves[1].BakeSeconds = request.Waves[0].BakeSeconds - 1
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("non-decreasing")))
}

func TestCampaignRevisionDraftRequestRejectsMissingMembership(t *testing.T) {
	g := NewWithT(t)
	request := validDeploymentCampaignDraftRequest()
	request.Membership.PlanIDs = nil
	request.Membership.TagQuery = ""

	g.Expect(request.Validate()).To(MatchError(ContainSubstring("membership")))
}

func validDeploymentCampaignDraftRequest() CreateDeploymentCampaignDraftRequest {
	firstPlanID := uuid.MustParse("30000000-0000-0000-0000-000000000001")
	secondPlanID := uuid.MustParse("30000000-0000-0000-0000-000000000002")
	return CreateDeploymentCampaignDraftRequest{
		Name: "production rollout",
		Membership: CampaignMembershipRequest{
			PlanIDs: []uuid.UUID{firstPlanID, secondPlanID},
		},
		Waves: []CampaignWaveRequest{
			{Order: 1, Name: "canary", PlanIDs: []uuid.UUID{firstPlanID}, BakeSeconds: 3600, MaximumConcurrency: 1},
			{Order: 2, Name: "broad", PlanIDs: []uuid.UUID{secondPlanID}, BakeSeconds: 7200, MaximumConcurrency: 2},
		},
		RiskPolicy: CampaignRiskPolicy{
			MaximumConcurrency:          2,
			FailureToleranceBasisPoints: 500,
			MinimumHealthyBasisPoints:   9500,
		},
	}
}
