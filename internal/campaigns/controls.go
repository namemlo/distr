package campaigns

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var (
	ErrCampaignV2RetryUnavailable  = errors.New("campaign v2 retry unavailable until PR-075")
	ErrCampaignControlUnauthorized = errors.New("campaign control not authorized")
)

func DecideCampaignControl(
	run types.CampaignRun,
	input types.CampaignControlInput,
	facts types.CampaignControlFacts,
) (types.CampaignControlDecision, error) {
	if run.Version != input.ExpectedVersion {
		return types.CampaignControlDecision{}, fmt.Errorf(
			"%w: expected %d, found %d",
			ErrCampaignVersionConflict,
			input.ExpectedVersion,
			run.Version,
		)
	}
	if input.RequestID == uuid.Nil {
		return types.CampaignControlDecision{}, errors.New("campaign control request ID is required")
	}
	if strings.TrimSpace(input.Reason) == "" {
		return types.CampaignControlDecision{}, errors.New("campaign control reason is required")
	}

	decision := types.CampaignControlDecision{
		Run:    run,
		Status: types.CampaignControlStatusApplied,
	}
	decision.Run.Version++
	switch input.Kind {
	case types.CampaignControlKindPause:
		if run.State != types.CampaignRunStateRunning &&
			run.State != types.CampaignRunStateScheduled {
			return types.CampaignControlDecision{}, fmt.Errorf(
				"cannot pause campaign in state %s",
				run.State,
			)
		}
		decision.Run.AdmissionsBlocked = true
		if facts.AtSafePoint {
			decision.Run.State = types.CampaignRunStatePaused
		} else {
			decision.PausePending = true
			decision.Run.PauseRequested = true
			decision.Status = types.CampaignControlStatusPendingSafePoint
		}
	case types.CampaignControlKindResume:
		if run.State != types.CampaignRunStatePaused {
			return types.CampaignControlDecision{}, fmt.Errorf(
				"cannot resume campaign in state %s",
				run.State,
			)
		}
		decision.Run.State = types.CampaignRunStateRunning
		decision.Run.AdmissionsBlocked = false
		decision.Run.PauseRequested = false
	case types.CampaignControlKindCancel:
		if run.State == types.CampaignRunStateCompleted ||
			run.State == types.CampaignRunStateFailed ||
			run.State == types.CampaignRunStateCanceled {
			return types.CampaignControlDecision{}, fmt.Errorf(
				"cannot cancel campaign in state %s",
				run.State,
			)
		}
		decision.Run.AdmissionsBlocked = true
		if facts.HasUncertainSteps {
			decision.ReconciliationRequired = true
			decision.Run.ReconciliationRequired = true
			decision.Status = types.CampaignControlStatusPendingReconciliation
		} else if !facts.AllActiveStepsCancellable {
			return types.CampaignControlDecision{}, errors.New(
				"campaign has active non-cancellable steps",
			)
		} else {
			decision.Run.State = types.CampaignRunStateCanceled
		}
	default:
		return types.CampaignControlDecision{}, fmt.Errorf(
			"unsupported campaign control %s",
			input.Kind,
		)
	}
	return decision, nil
}

func BuildCampaignExclusion(
	input types.CampaignMemberControlInput,
	facts types.CampaignExclusionFacts,
) (types.CampaignExclusion, error) {
	if !facts.Authorized {
		return types.CampaignExclusion{}, ErrCampaignControlUnauthorized
	}
	if input.MemberRunID == uuid.Nil {
		return types.CampaignExclusion{}, errors.New("campaign member run ID is required")
	}
	if strings.TrimSpace(input.Reason) == "" {
		return types.CampaignExclusion{}, errors.New("campaign exclusion reason is required")
	}
	exclusion := types.CampaignExclusion{
		ID:                input.RequestID,
		OrganizationID:    input.OrganizationID,
		CampaignRunID:     input.RunID,
		MemberRunID:       input.MemberRunID,
		ControlRequestID:  input.RequestID,
		Reason:            strings.TrimSpace(input.Reason),
		VisibleIncomplete: facts.WasAdmitted,
		ExcludedAt:        input.RequestedAt,
		ExcludedByActorID: input.ActorID,
	}
	if facts.WasAdmitted {
		exclusion.DriftReason = "admitted member excluded; campaign remains visibly incomplete"
	}
	return exclusion, nil
}

func DecideCampaignMemberMutation(
	run types.CampaignRun,
	input types.CampaignMemberControlInput,
) (types.CampaignRun, error) {
	if run.ID != input.RunID {
		return types.CampaignRun{}, errors.New("campaign member mutation run identity does not match")
	}
	if run.Version != input.ExpectedVersion {
		return types.CampaignRun{}, fmt.Errorf(
			"%w: expected %d, found %d",
			ErrCampaignVersionConflict,
			input.ExpectedVersion,
			run.Version,
		)
	}
	switch run.State {
	case types.CampaignRunStateFailed,
		types.CampaignRunStateCompleted,
		types.CampaignRunStateCanceled:
		return types.CampaignRun{}, fmt.Errorf(
			"campaign member mutation is forbidden for terminal state %s",
			run.State,
		)
	}
	run.Version++
	return run, nil
}

type CampaignControlStore interface {
	ApplyCampaignControl(
		context.Context,
		types.CampaignControlInput,
	) (*types.CampaignControlResult, error)
	ExcludeCampaignMember(
		context.Context,
		types.CampaignMemberControlInput,
	) (*types.CampaignExclusion, error)
}

type SupersedingPlanCreator interface {
	CreateSupersedingPlan(
		context.Context,
		types.CampaignMemberControlInput,
	) (*types.DeploymentPlan, error)
}

type CampaignController struct {
	store       CampaignControlStore
	planCreator SupersedingPlanCreator
}

type CampaignControlService struct {
	controller *CampaignController
	store      CampaignControlStore
}

func NewCampaignControlService(
	store CampaignControlStore,
	planCreator SupersedingPlanCreator,
) *CampaignControlService {
	return &CampaignControlService{
		controller: NewCampaignController(store, planCreator),
		store:      store,
	}
}

func (s *CampaignControlService) ApplyCampaignControl(
	ctx context.Context,
	input types.CampaignControlInput,
) (*types.CampaignControlResult, error) {
	return s.store.ApplyCampaignControl(ctx, input)
}

func (s *CampaignControlService) ExcludeCampaignMember(
	ctx context.Context,
	input types.CampaignMemberControlInput,
) (*types.CampaignExclusion, error) {
	return s.store.ExcludeCampaignMember(ctx, input)
}

func (s *CampaignControlService) RetryCampaignMember(
	ctx context.Context,
	input types.CampaignMemberControlInput,
) (*types.DeploymentPlan, error) {
	return s.controller.RetryCampaignMember(ctx, input)
}

func NewCampaignController(
	store CampaignControlStore,
	planCreator SupersedingPlanCreator,
) *CampaignController {
	return &CampaignController{store: store, planCreator: planCreator}
}

func (c *CampaignController) PauseCampaign(
	ctx context.Context,
	input types.CampaignControlInput,
) error {
	input.Kind = types.CampaignControlKindPause
	_, err := c.store.ApplyCampaignControl(ctx, input)
	return err
}

func (c *CampaignController) ResumeCampaign(
	ctx context.Context,
	input types.CampaignControlInput,
) error {
	input.Kind = types.CampaignControlKindResume
	_, err := c.store.ApplyCampaignControl(ctx, input)
	return err
}

func (c *CampaignController) CancelCampaign(
	ctx context.Context,
	input types.CampaignControlInput,
) error {
	input.Kind = types.CampaignControlKindCancel
	_, err := c.store.ApplyCampaignControl(ctx, input)
	return err
}

func (c *CampaignController) ExcludeCampaignMember(
	ctx context.Context,
	input types.CampaignMemberControlInput,
) error {
	input.Kind = types.CampaignControlKindExclude
	_, err := c.store.ExcludeCampaignMember(ctx, input)
	return err
}

func (c *CampaignController) RetryCampaignMember(
	ctx context.Context,
	input types.CampaignMemberControlInput,
) (*types.DeploymentPlan, error) {
	switch input.ProtocolVersion {
	case "v1":
		if c.planCreator == nil {
			return nil, errors.New("campaign v1 superseding plan creator is not wired")
		}
		return c.planCreator.CreateSupersedingPlan(ctx, input)
	case "v2":
		return nil, ErrCampaignV2RetryUnavailable
	default:
		return nil, fmt.Errorf("unsupported campaign retry protocol %q", input.ProtocolVersion)
	}
}
