package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func LifecycleToAPI(lifecycle types.Lifecycle) api.Lifecycle {
	return api.Lifecycle{
		ID:          lifecycle.ID,
		CreatedAt:   lifecycle.CreatedAt,
		UpdatedAt:   lifecycle.UpdatedAt,
		Name:        lifecycle.Name,
		Description: lifecycle.Description,
		SortOrder:   lifecycle.SortOrder,
		Phases:      List(lifecycle.Phases, LifecyclePhaseToAPI),
	}
}

func LifecyclePhaseToAPI(phase types.LifecyclePhase) api.LifecyclePhase {
	return api.LifecyclePhase{
		ID:                           phase.ID,
		Name:                         phase.Name,
		Description:                  phase.Description,
		SortOrder:                    phase.SortOrder,
		EnvironmentIDs:               phase.EnvironmentIDs,
		Optional:                     phase.Optional,
		AutomaticPromotion:           phase.AutomaticPromotion,
		MinimumSuccessfulDeployments: phase.MinimumSuccessfulDeployments,
		ApprovalPolicyID:             phase.ApprovalPolicyID,
		RetentionPolicyID:            phase.RetentionPolicyID,
	}
}
