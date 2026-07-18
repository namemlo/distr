package api

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateDeploymentPlanRequestValidate(t *testing.T) {
	releaseBundleID := uuid.New()
	environmentID := uuid.New()
	targetID := uuid.New()

	tests := []struct {
		name    string
		request CreateDeploymentPlanRequest
		wantErr string
	}{
		{
			name: "accepts selected targets",
			request: CreateDeploymentPlanRequest{
				ReleaseBundleID: releaseBundleID,
				EnvironmentID:   environmentID,
				TargetIDs:       []uuid.UUID{targetID},
			},
		},
		{
			name: "missing release bundle",
			request: CreateDeploymentPlanRequest{
				EnvironmentID: environmentID,
				TargetIDs:     []uuid.UUID{targetID},
			},
			wantErr: "releaseBundleId is required",
		},
		{
			name: "missing environment",
			request: CreateDeploymentPlanRequest{
				ReleaseBundleID: releaseBundleID,
				TargetIDs:       []uuid.UUID{targetID},
			},
			wantErr: "environmentId is required",
		},
		{
			name: "missing targets",
			request: CreateDeploymentPlanRequest{
				ReleaseBundleID: releaseBundleID,
				EnvironmentID:   environmentID,
			},
			wantErr: "at least one targetId is required",
		},
		{
			name: "nil target",
			request: CreateDeploymentPlanRequest{
				ReleaseBundleID: releaseBundleID,
				EnvironmentID:   environmentID,
				TargetIDs:       []uuid.UUID{uuid.Nil},
			},
			wantErr: "targetIds must not contain empty IDs",
		},
		{
			name: "duplicate target",
			request: CreateDeploymentPlanRequest{
				ReleaseBundleID: releaseBundleID,
				EnvironmentID:   environmentID,
				TargetIDs:       []uuid.UUID{targetID, targetID},
			},
			wantErr: "targetIds must be unique",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			if tt.wantErr == "" {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
			}
		})
	}
}

func TestCreatePreviousStateDeploymentPlanRequestValidate(t *testing.T) {
	g := NewWithT(t)

	g.Expect(CreatePreviousStateDeploymentPlanRequest{
		SuccessfulDeploymentPlanID: uuid.New(),
		Reason:                     "Restore the last verified compatible state",
	}.Validate()).To(Succeed())
	g.Expect(CreatePreviousStateDeploymentPlanRequest{
		Reason: "missing plan",
	}.Validate()).To(MatchError(ContainSubstring("successfulDeploymentPlanId is required")))
	g.Expect(CreatePreviousStateDeploymentPlanRequest{
		SuccessfulDeploymentPlanID: uuid.New(),
		Reason:                     "line one\nline two",
	}.Validate()).To(MatchError(ContainSubstring("reason is invalid")))
}
