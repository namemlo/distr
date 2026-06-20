package lifecycle

import (
	"context"
	"slices"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type EligibilityService struct{}

func NewEligibilityService() EligibilityService {
	return EligibilityService{}
}

type EligibilityRequest struct {
	Lifecycle     types.Lifecycle
	EnvironmentID uuid.UUID
}

type EligibilityResult struct {
	EngineReady bool
	Eligible    bool
	Phases      []EligibilityPhase
	Reasons     []string
}

type EligibilityPhase struct {
	Name               string
	SortOrder          int
	MatchesEnvironment bool
}

func (EligibilityService) Explain(_ context.Context, request EligibilityRequest) EligibilityResult {
	phases := slices.Clone(request.Lifecycle.Phases)
	slices.SortStableFunc(phases, func(a, b types.LifecyclePhase) int {
		return a.SortOrder - b.SortOrder
	})

	result := EligibilityResult{
		EngineReady: false,
		Eligible:    false,
		Reasons:     []string{"release, channel, and deployment history models are not available yet"},
	}
	for _, phase := range phases {
		result.Phases = append(result.Phases, EligibilityPhase{
			Name:               phase.Name,
			SortOrder:          phase.SortOrder,
			MatchesEnvironment: slices.Contains(phase.EnvironmentIDs, request.EnvironmentID),
		})
	}
	return result
}
