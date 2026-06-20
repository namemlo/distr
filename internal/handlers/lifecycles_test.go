package handlers

import (
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestLifecycleFromCreateUpdateRequest(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	envID := uuid.New()

	lifecycle := lifecycleFromCreateUpdateRequest(orgID, api.CreateUpdateLifecycleRequest{
		Name:        " Standard ",
		Description: "Development to production promotion",
		SortOrder:   20,
		Phases: []api.CreateUpdateLifecyclePhaseRequest{
			{
				Name:                         " Development ",
				Description:                  "Internal validation",
				SortOrder:                    10,
				EnvironmentIDs:               []uuid.UUID{envID},
				Optional:                     true,
				AutomaticPromotion:           false,
				MinimumSuccessfulDeployments: 2,
			},
		},
	})

	g.Expect(lifecycle).To(Equal(types.Lifecycle{
		OrganizationID: orgID,
		Name:           "Standard",
		Description:    "Development to production promotion",
		SortOrder:      20,
		Phases: []types.LifecyclePhase{
			{
				Name:                         "Development",
				Description:                  "Internal validation",
				SortOrder:                    10,
				EnvironmentIDs:               []uuid.UUID{envID},
				Optional:                     true,
				AutomaticPromotion:           false,
				MinimumSuccessfulDeployments: 2,
			},
		},
	}))
}

func TestLifecycleResponses(t *testing.T) {
	g := NewWithT(t)
	id := uuid.New()

	responses := lifecycleResponses([]types.Lifecycle{{ID: id, Name: "Standard"}})

	g.Expect(responses).To(Equal([]api.Lifecycle{{ID: id, Name: "Standard", Phases: []api.LifecyclePhase{}}}))
}
