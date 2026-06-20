package api

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateUpdateLifecycleRequestValidate(t *testing.T) {
	environmentID := uuid.New()

	tests := []struct {
		name    string
		request CreateUpdateLifecycleRequest
		wantErr bool
	}{
		{
			name: "accepts lifecycle with ordered environment phases",
			request: CreateUpdateLifecycleRequest{
				Name:        "Standard",
				Description: "Development to production promotion",
				SortOrder:   20,
				Phases: []CreateUpdateLifecyclePhaseRequest{
					{
						Name:                         "Development",
						Description:                  "Internal validation",
						SortOrder:                    10,
						EnvironmentIDs:               []uuid.UUID{environmentID},
						Optional:                     false,
						AutomaticPromotion:           true,
						MinimumSuccessfulDeployments: 1,
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "rejects blank lifecycle names",
			request: CreateUpdateLifecycleRequest{Name: "   "},
			wantErr: true,
		},
		{
			name:    "rejects negative lifecycle sort order",
			request: CreateUpdateLifecycleRequest{Name: "Standard", SortOrder: -1},
			wantErr: true,
		},
		{
			name: "rejects blank phase names",
			request: CreateUpdateLifecycleRequest{
				Name: "Standard",
				Phases: []CreateUpdateLifecyclePhaseRequest{
					{Name: " ", SortOrder: 10, EnvironmentIDs: []uuid.UUID{environmentID}},
				},
			},
			wantErr: true,
		},
		{
			name: "rejects phases without environments",
			request: CreateUpdateLifecycleRequest{
				Name:   "Standard",
				Phases: []CreateUpdateLifecyclePhaseRequest{{Name: "Production", SortOrder: 10}},
			},
			wantErr: true,
		},
		{
			name: "rejects negative minimum successful deployments",
			request: CreateUpdateLifecycleRequest{
				Name: "Standard",
				Phases: []CreateUpdateLifecyclePhaseRequest{
					{
						Name:                         "Production",
						SortOrder:                    10,
						EnvironmentIDs:               []uuid.UUID{environmentID},
						MinimumSuccessfulDeployments: -1,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "rejects duplicate phase names",
			request: CreateUpdateLifecycleRequest{
				Name: "Standard",
				Phases: []CreateUpdateLifecyclePhaseRequest{
					{Name: "Production", SortOrder: 10, EnvironmentIDs: []uuid.UUID{environmentID}},
					{Name: " Production ", SortOrder: 20, EnvironmentIDs: []uuid.UUID{environmentID}},
				},
			},
			wantErr: true,
		},
		{
			name: "rejects duplicate phase sort orders",
			request: CreateUpdateLifecycleRequest{
				Name: "Standard",
				Phases: []CreateUpdateLifecyclePhaseRequest{
					{Name: "Staging", SortOrder: 10, EnvironmentIDs: []uuid.UUID{environmentID}},
					{Name: "Production", SortOrder: 10, EnvironmentIDs: []uuid.UUID{environmentID}},
				},
			},
			wantErr: true,
		},
		{
			name: "rejects duplicate phase environments",
			request: CreateUpdateLifecycleRequest{
				Name: "Standard",
				Phases: []CreateUpdateLifecyclePhaseRequest{
					{
						Name:           "Production",
						SortOrder:      10,
						EnvironmentIDs: []uuid.UUID{environmentID, environmentID},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}
