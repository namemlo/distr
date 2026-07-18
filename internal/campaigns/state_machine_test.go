package campaigns

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/onsi/gomega"
)

func TestCampaignRunHappyPathAndTerminalTransitions(t *testing.T) {
	g := gomega.NewWithT(t)

	run := types.CampaignRun{State: types.CampaignRunStateDraft, Version: 1}
	for _, next := range []types.CampaignRunState{
		types.CampaignRunStateValidated,
		types.CampaignRunStateAwaitingApproval,
		types.CampaignRunStateScheduled,
		types.CampaignRunStateRunning,
		types.CampaignRunStateCompleted,
	} {
		var err error
		run, err = NextCampaignRun(run, types.CampaignTransition{
			ExpectedVersion: run.Version,
			To:              next,
			Reason:          "test transition",
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(run.State).To(gomega.Equal(next))
	}

	for _, tc := range []struct {
		from types.CampaignRunState
		to   types.CampaignRunState
	}{
		{types.CampaignRunStateRunning, types.CampaignRunStatePaused},
		{types.CampaignRunStatePaused, types.CampaignRunStateRunning},
		{types.CampaignRunStateRunning, types.CampaignRunStateFailed},
		{types.CampaignRunStateDraft, types.CampaignRunStateCanceled},
		{types.CampaignRunStateScheduled, types.CampaignRunStateCanceled},
		{types.CampaignRunStatePaused, types.CampaignRunStateCanceled},
	} {
		_, err := NextCampaignRun(types.CampaignRun{State: tc.from, Version: 4}, types.CampaignTransition{
			ExpectedVersion: 4,
			To:              tc.to,
			Reason:          "operator action",
		})
		g.Expect(err).NotTo(gomega.HaveOccurred(), "%s -> %s", tc.from, tc.to)
	}
}

func TestCampaignRunRejectsIllegalAndStaleTransitions(t *testing.T) {
	g := gomega.NewWithT(t)
	run := types.CampaignRun{State: types.CampaignRunStateDraft, Version: 8}

	_, err := NextCampaignRun(run, types.CampaignTransition{
		ExpectedVersion: 8,
		To:              types.CampaignRunStateRunning,
		Reason:          "skip validation",
	})
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("illegal campaign transition")))

	_, err = NextCampaignRun(run, types.CampaignTransition{
		ExpectedVersion: 7,
		To:              types.CampaignRunStateValidated,
		Reason:          "stale writer",
	})
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("campaign version conflict")))
}
