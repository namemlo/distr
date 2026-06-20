package lifecycle

import (
	"context"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestEligibilityServiceReturnsSkeletonExplanation(t *testing.T) {
	g := NewWithT(t)
	envID := uuid.New()
	service := NewEligibilityService()

	result := service.Explain(context.Background(), EligibilityRequest{
		Lifecycle: types.Lifecycle{
			Name: "Standard",
			Phases: []types.LifecyclePhase{
				{Name: "Production", SortOrder: 20, EnvironmentIDs: []uuid.UUID{envID}},
				{Name: "Development", SortOrder: 10, EnvironmentIDs: []uuid.UUID{uuid.New()}},
			},
		},
		EnvironmentID: envID,
	})

	g.Expect(result.EngineReady).To(BeFalse())
	g.Expect(result.Eligible).To(BeFalse())
	g.Expect(result.Phases).To(Equal([]EligibilityPhase{
		{Name: "Development", SortOrder: 10, MatchesEnvironment: false},
		{Name: "Production", SortOrder: 20, MatchesEnvironment: true},
	}))
	g.Expect(result.Reasons).To(ContainElement("release, channel, and deployment history models are not available yet"))
}
