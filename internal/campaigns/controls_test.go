package campaigns

import (
	"context"
	"errors"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/onsi/gomega"
)

func TestPauseCampaignBlocksAdmissionsBeforeSafePointAndResumeSurvivesRestart(t *testing.T) {
	g := gomega.NewWithT(t)
	run := types.CampaignRun{
		ID:                uuid.New(),
		State:             types.CampaignRunStateRunning,
		Version:           4,
		AdmissionsBlocked: false,
	}
	pause, err := DecideCampaignControl(run, types.CampaignControlInput{
		RequestID:       uuid.New(),
		ExpectedVersion: 4,
		Kind:            types.CampaignControlKindPause,
		Reason:          "provider degradation",
	}, types.CampaignControlFacts{AtSafePoint: false})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pause.Run.State).To(gomega.Equal(types.CampaignRunStateRunning))
	g.Expect(pause.Run.AdmissionsBlocked).To(gomega.BeTrue())
	g.Expect(pause.PausePending).To(gomega.BeTrue())

	persisted := pause.Run
	persisted.State = types.CampaignRunStatePaused
	resume, err := DecideCampaignControl(persisted, types.CampaignControlInput{
		RequestID:       uuid.New(),
		ExpectedVersion: persisted.Version,
		Kind:            types.CampaignControlKindResume,
		Reason:          "incident cleared",
	}, types.CampaignControlFacts{AtSafePoint: true})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(resume.Run.State).To(gomega.Equal(types.CampaignRunStateRunning))
	g.Expect(resume.Run.AdmissionsBlocked).To(gomega.BeFalse())
	g.Expect(resume.PausePending).To(gomega.BeFalse())
}

func TestScheduledCampaignPauseResumesToScheduled(t *testing.T) {
	g := gomega.NewWithT(t)
	run := types.CampaignRun{ID: uuid.New(), State: types.CampaignRunStateScheduled, Version: 1}
	pause, err := DecideCampaignControl(run, types.CampaignControlInput{
		RequestID: uuid.New(), ExpectedVersion: 1, Kind: types.CampaignControlKindPause, Reason: "operator hold",
	}, types.CampaignControlFacts{AtSafePoint: true})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pause.Run.State).To(gomega.Equal(types.CampaignRunStatePaused))
	g.Expect(pause.Run.ResumeState).To(gomega.Equal(types.CampaignRunStateScheduled))

	resume, err := DecideCampaignControl(pause.Run, types.CampaignControlInput{
		RequestID: uuid.New(), ExpectedVersion: pause.Run.Version, Kind: types.CampaignControlKindResume, Reason: "hold cleared",
	}, types.CampaignControlFacts{AtSafePoint: true})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(resume.Run.State).To(gomega.Equal(types.CampaignRunStateScheduled))
	g.Expect(resume.Run.ResumeState).To(gomega.Equal(types.CampaignRunState("")))
}

func TestCancelCampaignOnlyCancellableWorkAndReconcilesUncertainState(t *testing.T) {
	g := gomega.NewWithT(t)
	run := types.CampaignRun{
		ID:      uuid.New(),
		State:   types.CampaignRunStateRunning,
		Version: 8,
	}
	input := types.CampaignControlInput{
		RequestID:       uuid.New(),
		ExpectedVersion: 8,
		Kind:            types.CampaignControlKindCancel,
		Reason:          "operator canceled campaign",
	}

	uncertain, err := DecideCampaignControl(run, input, types.CampaignControlFacts{
		HasUncertainSteps: true,
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(uncertain.Status).To(gomega.Equal(types.CampaignControlStatusPendingReconciliation))
	g.Expect(uncertain.Run.State).To(gomega.Equal(types.CampaignRunStateRunning))
	g.Expect(uncertain.Run.AdmissionsBlocked).To(gomega.BeTrue())

	canceled, err := DecideCampaignControl(run, input, types.CampaignControlFacts{
		AllActiveStepsCancellable: true,
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(canceled.Status).To(gomega.Equal(types.CampaignControlStatusApplied))
	g.Expect(canceled.Run.State).To(gomega.Equal(types.CampaignRunStateCanceled))

	_, err = DecideCampaignControl(run, input, types.CampaignControlFacts{})
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("non-cancellable")))
}

func TestCampaignControlRejectsStaleAndConflictingState(t *testing.T) {
	g := gomega.NewWithT(t)
	run := types.CampaignRun{
		ID:      uuid.New(),
		State:   types.CampaignRunStatePaused,
		Version: 10,
	}
	_, err := DecideCampaignControl(run, types.CampaignControlInput{
		RequestID:       uuid.New(),
		ExpectedVersion: 9,
		Kind:            types.CampaignControlKindResume,
		Reason:          "stale resume",
	}, types.CampaignControlFacts{})
	g.Expect(errors.Is(err, ErrCampaignVersionConflict)).To(gomega.BeTrue())

	_, err = DecideCampaignControl(run, types.CampaignControlInput{
		RequestID:       uuid.New(),
		ExpectedVersion: 10,
		Kind:            types.CampaignControlKindPause,
		Reason:          "already paused",
	}, types.CampaignControlFacts{})
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("cannot pause")))
}

func TestExcludeCampaignMemberIsAuthorizedAndVisibleAsIncompleteDrift(t *testing.T) {
	g := gomega.NewWithT(t)
	input := types.CampaignMemberControlInput{
		CampaignControlInput: types.CampaignControlInput{
			RequestID: uuid.New(),
			RunID:     uuid.New(),
			Reason:    "target withdrawn",
		},
		MemberRunID: uuid.New(),
	}
	_, err := BuildCampaignExclusion(input, types.CampaignExclusionFacts{Authorized: false})
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("not authorized")))

	exclusion, err := BuildCampaignExclusion(input, types.CampaignExclusionFacts{
		Authorized:  true,
		WasAdmitted: true,
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(exclusion.ID).NotTo(gomega.Equal(input.RequestID))
	g.Expect(exclusion.ID).NotTo(gomega.Equal(uuid.Nil))
	g.Expect(exclusion.VisibleIncomplete).To(gomega.BeTrue())
	g.Expect(exclusion.DriftReason).To(gomega.ContainSubstring("admitted member excluded"))
}

func TestCampaignControlsRejectWhitespacePaddedReason(t *testing.T) {
	g := gomega.NewWithT(t)
	_, err := DecideCampaignControl(
		types.CampaignRun{ID: uuid.New(), State: types.CampaignRunStateRunning, Version: 1},
		types.CampaignControlInput{RequestID: uuid.New(), ExpectedVersion: 1, Kind: types.CampaignControlKindPause, Reason: " padded "},
		types.CampaignControlFacts{AtSafePoint: true},
	)
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("trimmed")))
}

func TestCampaignMemberMutationRejectsTerminalRunAndAdvancesVersion(t *testing.T) {
	g := gomega.NewWithT(t)
	input := types.CampaignMemberControlInput{
		CampaignControlInput: types.CampaignControlInput{
			RequestID:       uuid.New(),
			RunID:           uuid.New(),
			ExpectedVersion: 7,
			Reason:          "exclude",
		},
		MemberRunID: uuid.New(),
	}
	for _, state := range []types.CampaignRunState{
		types.CampaignRunStateFailed,
		types.CampaignRunStateCompleted,
		types.CampaignRunStateCanceled,
	} {
		_, err := DecideCampaignMemberMutation(
			types.CampaignRun{ID: input.RunID, State: state, Version: 7},
			input,
		)
		g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("terminal")))
	}
	updated, err := DecideCampaignMemberMutation(
		types.CampaignRun{
			ID:      input.RunID,
			State:   types.CampaignRunStateRunning,
			Version: 7,
		},
		input,
	)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(updated.Version).To(gomega.Equal(int64(8)))
}

type campaignControlStoreFake struct {
	result        *types.CampaignControlResult
	exclusion     *types.CampaignExclusion
	applied       int
	excluded      int
	retryPrepared int
}

func (s *campaignControlStoreFake) PersistCampaignMemberRetry(
	ctx context.Context,
	input types.CampaignMemberControlInput,
	creator SupersedingPlanCreator,
) (*types.DeploymentPlan, error) {
	s.retryPrepared++
	return creator.CreateSupersedingPlan(ctx, input)
}

func (s *campaignControlStoreFake) ApplyCampaignControl(
	context.Context,
	types.CampaignControlInput,
) (*types.CampaignControlResult, error) {
	s.applied++
	return s.result, nil
}

func (s *campaignControlStoreFake) ExcludeCampaignMember(
	context.Context,
	types.CampaignMemberControlInput,
) (*types.CampaignExclusion, error) {
	s.excluded++
	return s.exclusion, nil
}

func TestCompositeCampaignControlServiceIsRouteCompatible(t *testing.T) {
	g := gomega.NewWithT(t)
	store := &campaignControlStoreFake{
		result:    &types.CampaignControlResult{RequestID: uuid.New()},
		exclusion: &types.CampaignExclusion{ID: uuid.New()},
	}
	creator := supersedingPlanCreatorFake{plan: &types.DeploymentPlan{ID: uuid.New()}}
	service := NewCampaignControlService(store, creator)
	var routeService interface {
		ApplyCampaignControl(context.Context, types.CampaignControlInput) (*types.CampaignControlResult, error)
		ExcludeCampaignMember(context.Context, types.CampaignMemberControlInput) (*types.CampaignExclusion, error)
		RetryCampaignMember(context.Context, types.CampaignMemberControlInput) (*types.DeploymentPlan, error)
	} = service

	_, err := routeService.ApplyCampaignControl(context.Background(), types.CampaignControlInput{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	_, err = routeService.ExcludeCampaignMember(context.Background(), types.CampaignMemberControlInput{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	_, err = routeService.RetryCampaignMember(context.Background(), types.CampaignMemberControlInput{
		CampaignControlInput: types.CampaignControlInput{
			RequestID: uuid.New(), RunID: uuid.New(), Reason: "retry incomplete delivery",
		},
		MemberRunID: uuid.New(), ProtocolVersion: "v1",
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(store.applied).To(gomega.Equal(1))
	g.Expect(store.excluded).To(gomega.Equal(1))
	g.Expect(store.retryPrepared).To(gomega.Equal(1))
}

type supersedingPlanCreatorFake struct {
	plan *types.DeploymentPlan
}

func (f supersedingPlanCreatorFake) CreateSupersedingPlan(
	context.Context,
	types.CampaignMemberControlInput,
) (*types.DeploymentPlan, error) {
	return f.plan, nil
}

func TestRetryCampaignSplitKeepsV1SupersedingPlanAndBlocksV2(t *testing.T) {
	g := gomega.NewWithT(t)
	expected := &types.DeploymentPlan{ID: uuid.New()}
	controller := NewCampaignController(nil, supersedingPlanCreatorFake{plan: expected})
	input := types.CampaignMemberControlInput{
		CampaignControlInput: types.CampaignControlInput{
			RequestID: uuid.New(),
			RunID:     uuid.New(),
			Reason:    "delivery cannot be proven",
		},
		MemberRunID: uuid.New(),
	}

	input.ProtocolVersion = "v1"
	plan, err := controller.RetryCampaignMember(context.Background(), input)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(plan).To(gomega.Equal(expected))

	input.ProtocolVersion = "v2"
	plan, err = controller.RetryCampaignMember(context.Background(), input)
	g.Expect(plan).To(gomega.BeNil())
	g.Expect(errors.Is(err, ErrCampaignV2RetryUnavailable)).To(gomega.BeTrue())
}

func TestCampaignMemberRetryOnlyAllowsFailedMember(t *testing.T) {
	g := gomega.NewWithT(t)
	g.Expect(ValidateCampaignMemberRetryStatus("FAILED")).To(gomega.Succeed())
	g.Expect(ValidateCampaignMemberRetryStatus("CANCELED")).To(gomega.Succeed())
	for _, status := range []string{"PENDING", "ADMITTED", "RUNNING", "SUCCEEDED", "EXCLUDED"} {
		g.Expect(ValidateCampaignMemberRetryStatus(status)).To(gomega.HaveOccurred(), status)
	}
}
