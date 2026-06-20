package mapping

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestLifecycleToAPI(t *testing.T) {
	g := NewWithT(t)
	id := uuid.New()
	orgID := uuid.New()
	phaseID := uuid.New()
	envID := uuid.New()
	createdAt := time.Date(2026, 6, 20, 9, 30, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 20, 10, 45, 0, 0, time.UTC)

	res := LifecycleToAPI(types.Lifecycle{
		ID:             id,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		OrganizationID: orgID,
		Name:           "Standard",
		Description:    "Development to production promotion",
		SortOrder:      20,
		Phases: []types.LifecyclePhase{
			{
				ID:                           phaseID,
				LifecycleID:                  id,
				Name:                         "Development",
				Description:                  "Internal validation",
				SortOrder:                    10,
				EnvironmentIDs:               []uuid.UUID{envID},
				Optional:                     false,
				AutomaticPromotion:           true,
				MinimumSuccessfulDeployments: 1,
			},
		},
	})

	g.Expect(res).To(Equal(api.Lifecycle{
		ID:          id,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		Name:        "Standard",
		Description: "Development to production promotion",
		SortOrder:   20,
		Phases: []api.LifecyclePhase{
			{
				ID:                           phaseID,
				Name:                         "Development",
				Description:                  "Internal validation",
				SortOrder:                    10,
				EnvironmentIDs:               []uuid.UUID{envID},
				Optional:                     false,
				AutomaticPromotion:           true,
				MinimumSuccessfulDeployments: 1,
			},
		},
	}))
}
