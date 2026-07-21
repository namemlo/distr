package api

import (
	"encoding/json"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"testing"
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

func TestCampaignRunResponseIncludesFencingAndAdmissionState(t *testing.T) {
	g := NewWithT(t)
	payload, err := json.Marshal(DeploymentCampaignRun{
		ID:                uuid.New(),
		State:             types.CampaignRunStatePaused,
		Version:           4,
		AdmissionsBlocked: true,
		FencingToken:      12,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).To(ContainSubstring(`"admissionsBlocked":true`))
	g.Expect(string(payload)).To(ContainSubstring(`"fencingToken":12`))
}

func TestCampaignControlRequestValidation(t *testing.T) {
	g := NewWithT(t)
	valid := CampaignControlRequest{
		RequestID:       uuid.New(),
		ExpectedVersion: 2,
		Reason:          "operator incident response",
	}
	g.Expect(valid.Validate()).To(Succeed())

	invalid := valid
	invalid.RequestID = uuid.Nil
	g.Expect(invalid.Validate()).To(MatchError(ContainSubstring("requestId")))
	invalid = valid
	invalid.Reason = " "
	g.Expect(invalid.Validate()).To(MatchError(ContainSubstring("reason")))
	invalid = valid
	invalid.Reason = " padded reason "
	g.Expect(invalid.Validate()).To(MatchError(ContainSubstring("trimmed")))
}

func TestStartDeploymentCampaignRunRequestRequiresRevision(t *testing.T) {
	g := NewWithT(t)
	g.Expect((StartDeploymentCampaignRunRequest{}).Validate()).To(
		MatchError(ContainSubstring("campaignRevisionId")),
	)
	g.Expect((StartDeploymentCampaignRunRequest{CampaignRevisionID: uuid.New()}).Validate()).To(Succeed())
}

func TestTransitionDeploymentCampaignRunRequestRequiresVersionAndTrimmedReason(t *testing.T) {
	g := NewWithT(t)
	g.Expect((TransitionDeploymentCampaignRunRequest{}).Validate()).To(HaveOccurred())
	g.Expect((TransitionDeploymentCampaignRunRequest{
		ExpectedVersion: 1, To: types.CampaignRunStateValidated, Reason: " validate ",
	}).Validate()).To(HaveOccurred())
	g.Expect((TransitionDeploymentCampaignRunRequest{
		ExpectedVersion: 1, To: types.CampaignRunStateValidated, Reason: "validated frozen inputs",
	}).Validate()).To(Succeed())
}

func TestCampaignMemberControlRequiresMemberAndProtocolForRetry(t *testing.T) {
	g := NewWithT(t)
	request := CampaignMemberControlRequest{
		CampaignControlRequest: CampaignControlRequest{
			RequestID:       uuid.New(),
			ExpectedVersion: 3,
			Reason:          "retry",
		},
		MemberRunID: uuid.New(),
	}
	g.Expect(request.Validate(false)).To(Succeed())
	g.Expect(request.Validate(true)).To(MatchError(ContainSubstring("protocolVersion")))
	request.ProtocolVersion = "v1"
	g.Expect(request.Validate(true)).To(Succeed())
}
