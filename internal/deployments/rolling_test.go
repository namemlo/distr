package deployments

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/google/uuid"
)

func TestNewRollingStateInitializesPendingTargetsInStableOrder(t *testing.T) {
	g := NewWithT(t)
	targetA := rollingTarget(uuid.New(), uuid.New(), 30)
	targetB := rollingTarget(uuid.New(), uuid.New(), 10)
	targetC := rollingTarget(uuid.New(), uuid.New(), 20)

	state, err := NewRollingState(RollingConfig{WindowSize: 2}, []RollingTarget{targetA, targetB, targetC})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(state.Phase).To(Equal(RollingPhasePending))
	g.Expect(targetPlanIDs(state.Targets)).To(Equal([]uuid.UUID{
		targetB.DeploymentPlanTargetID,
		targetC.DeploymentPlanTargetID,
		targetA.DeploymentPlanTargetID,
	}))
	for _, target := range state.Targets {
		g.Expect(target.State).To(Equal(RollingTargetPending))
		g.Expect(target.WindowNumber).To(Equal(0))
	}
}

func TestRollingStateStartsNextWindowWithinWindowAndUnavailableLimits(t *testing.T) {
	g := NewWithT(t)
	targets := []RollingTarget{
		rollingTarget(uuid.New(), uuid.New(), 10),
		rollingTarget(uuid.New(), uuid.New(), 20),
		rollingTarget(uuid.New(), uuid.New(), 30),
	}
	state, err := NewRollingState(RollingConfig{
		WindowSize:          3,
		MaximumUnavailable:  2,
		PauseBetweenWindows: 30 * time.Second,
	}, targets)
	g.Expect(err).NotTo(HaveOccurred())

	started, err := state.StartNextWindow()

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(state.Phase).To(Equal(RollingPhaseInProgress))
	g.Expect(state.CurrentWindow).To(Equal(1))
	g.Expect(targetPlanIDs(started)).To(Equal([]uuid.UUID{
		targets[0].DeploymentPlanTargetID,
		targets[1].DeploymentPlanTargetID,
	}))
	g.Expect(state.Targets[0].State).To(Equal(RollingTargetInProgress))
	g.Expect(state.Targets[1].State).To(Equal(RollingTargetInProgress))
	g.Expect(state.Targets[2].State).To(Equal(RollingTargetPending))
}

func TestRollingStateAdvancesWindowsOnlyAfterCurrentWindowIsTerminal(t *testing.T) {
	g := NewWithT(t)
	targets := []RollingTarget{
		rollingTarget(uuid.New(), uuid.New(), 10),
		rollingTarget(uuid.New(), uuid.New(), 20),
		rollingTarget(uuid.New(), uuid.New(), 30),
	}
	state, err := NewRollingState(RollingConfig{WindowSize: 2}, targets)
	g.Expect(err).NotTo(HaveOccurred())
	firstWindow, err := state.StartNextWindow()
	g.Expect(err).NotTo(HaveOccurred())

	nextWindow, err := state.StartNextWindow()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(nextWindow).To(BeEmpty())
	g.Expect(state.CurrentWindow).To(Equal(1))

	g.Expect(state.TransitionTarget(firstWindow[0].DeploymentPlanTargetID, RollingTargetSucceeded, "")).To(Succeed())
	g.Expect(state.TransitionTarget(firstWindow[1].DeploymentPlanTargetID, RollingTargetSucceeded, "")).To(Succeed())
	nextWindow, err = state.StartNextWindow()

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(targetPlanIDs(nextWindow)).To(Equal([]uuid.UUID{targets[2].DeploymentPlanTargetID}))
	g.Expect(state.CurrentWindow).To(Equal(2))
}

func TestRollingStateAppliesFailureThresholdAction(t *testing.T) {
	tests := []struct {
		name      string
		action    RollingFailureAction
		wantPhase RollingPhase
	}{
		{name: "pause", action: RollingFailureActionPause, wantPhase: RollingPhasePaused},
		{name: "abort", action: RollingFailureActionAbort, wantPhase: RollingPhaseAborted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			targets := []RollingTarget{
				rollingTarget(uuid.New(), uuid.New(), 10),
				rollingTarget(uuid.New(), uuid.New(), 20),
			}
			state, err := NewRollingState(RollingConfig{
				WindowSize: 2,
				FailureThreshold: RollingFailureThreshold{
					MaxFailedTargets: 1,
					Action:           tt.action,
				},
			}, targets)
			g.Expect(err).NotTo(HaveOccurred())
			started, err := state.StartNextWindow()
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(state.TransitionTarget(started[0].DeploymentPlanTargetID, RollingTargetFailed, "health check failed")).To(Succeed())

			g.Expect(state.Phase).To(Equal(tt.wantPhase))
			g.Expect(state.Targets[0].FailureReason).To(Equal("health check failed"))
		})
	}
}

func TestRollingStateAppliesPercentageFailureThreshold(t *testing.T) {
	g := NewWithT(t)
	targets := []RollingTarget{
		rollingTarget(uuid.New(), uuid.New(), 10),
		rollingTarget(uuid.New(), uuid.New(), 20),
		rollingTarget(uuid.New(), uuid.New(), 30),
		rollingTarget(uuid.New(), uuid.New(), 40),
	}
	state, err := NewRollingState(RollingConfig{
		WindowSize: 4,
		FailureThreshold: RollingFailureThreshold{
			MaxFailurePercentage: 50,
			Action:               RollingFailureActionAbort,
		},
	}, targets)
	g.Expect(err).NotTo(HaveOccurred())
	started, err := state.StartNextWindow()
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(state.TransitionTarget(started[0].DeploymentPlanTargetID, RollingTargetFailed, "first failure")).To(Succeed())
	g.Expect(state.Phase).To(Equal(RollingPhaseInProgress))
	g.Expect(state.TransitionTarget(started[1].DeploymentPlanTargetID, RollingTargetFailed, "second failure")).To(Succeed())

	g.Expect(state.Phase).To(Equal(RollingPhaseAborted))
}

func TestRollingStateRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config RollingConfig
	}{
		{name: "missing window", config: RollingConfig{}},
		{name: "negative maximum unavailable", config: RollingConfig{WindowSize: 1, MaximumUnavailable: -1}},
		{name: "negative pause", config: RollingConfig{WindowSize: 1, PauseBetweenWindows: -time.Second}},
		{name: "invalid percentage", config: RollingConfig{WindowSize: 1, FailureThreshold: RollingFailureThreshold{MaxFailurePercentage: 101}}},
		{name: "invalid action", config: RollingConfig{WindowSize: 1, FailureThreshold: RollingFailureThreshold{MaxFailedTargets: 1, Action: "stop"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			_, err := NewRollingState(tt.config, []RollingTarget{rollingTarget(uuid.New(), uuid.New(), 10)})

			g.Expect(err).To(HaveOccurred())
		})
	}
}

func rollingTarget(planTargetID uuid.UUID, deploymentTargetID uuid.UUID, sortOrder int) RollingTarget {
	return RollingTarget{
		DeploymentPlanTargetID: planTargetID,
		DeploymentTargetID:     deploymentTargetID,
		SortOrder:              sortOrder,
	}
}

func targetPlanIDs(targets []RollingTarget) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.DeploymentPlanTargetID)
	}
	return ids
}
