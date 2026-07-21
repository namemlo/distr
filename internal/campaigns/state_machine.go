package campaigns

import (
	"errors"
	"fmt"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

var (
	ErrIllegalCampaignTransition = errors.New("illegal campaign transition")
	ErrCampaignVersionConflict   = errors.New("campaign version conflict")
)

var allowedCampaignTransitions = map[types.CampaignRunState]map[types.CampaignRunState]struct{}{
	types.CampaignRunStateDraft: {
		types.CampaignRunStateValidated: {},
		types.CampaignRunStateCanceled:  {},
	},
	types.CampaignRunStateValidated: {
		types.CampaignRunStateAwaitingApproval: {},
		types.CampaignRunStateCanceled:         {},
	},
	types.CampaignRunStateAwaitingApproval: {
		types.CampaignRunStateScheduled: {},
		types.CampaignRunStateCanceled:  {},
	},
	types.CampaignRunStateScheduled: {
		types.CampaignRunStateRunning:  {},
		types.CampaignRunStatePaused:   {},
		types.CampaignRunStateCanceled: {},
	},
	types.CampaignRunStateRunning: {
		types.CampaignRunStatePaused:    {},
		types.CampaignRunStateFailed:    {},
		types.CampaignRunStateCompleted: {},
		types.CampaignRunStateCanceled:  {},
	},
	types.CampaignRunStatePaused: {
		types.CampaignRunStateRunning:  {},
		types.CampaignRunStateFailed:   {},
		types.CampaignRunStateCanceled: {},
	},
}

func IsCampaignPreRunTransition(from, to types.CampaignRunState) bool {
	switch from {
	case types.CampaignRunStateDraft:
		return to == types.CampaignRunStateValidated
	case types.CampaignRunStateValidated:
		return to == types.CampaignRunStateAwaitingApproval
	case types.CampaignRunStateAwaitingApproval:
		return to == types.CampaignRunStateScheduled
	case types.CampaignRunStateScheduled:
		return to == types.CampaignRunStateRunning
	default:
		return false
	}
}

func NextCampaignRun(
	run types.CampaignRun,
	transition types.CampaignTransition,
) (types.CampaignRun, error) {
	if transition.ExpectedVersion != run.Version {
		return types.CampaignRun{}, fmt.Errorf(
			"%w: expected %d, found %d",
			ErrCampaignVersionConflict,
			transition.ExpectedVersion,
			run.Version,
		)
	}
	if strings.TrimSpace(transition.Reason) == "" {
		return types.CampaignRun{}, fmt.Errorf("%w: reason is required", ErrIllegalCampaignTransition)
	}
	if _, allowed := allowedCampaignTransitions[run.State][transition.To]; !allowed {
		return types.CampaignRun{}, fmt.Errorf(
			"%w: %s -> %s",
			ErrIllegalCampaignTransition,
			run.State,
			transition.To,
		)
	}

	run.State = transition.To
	run.Version++
	run.AdmissionsBlocked = transition.To == types.CampaignRunStatePaused ||
		transition.To == types.CampaignRunStateFailed ||
		transition.To == types.CampaignRunStateCompleted ||
		transition.To == types.CampaignRunStateCanceled
	if !transition.At.IsZero() {
		run.UpdatedAt = transition.At
	}
	return run, nil
}
