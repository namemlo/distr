package mapping

import (
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/lifecycle"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestReleaseBundleToAPI(t *testing.T) {
	g := NewWithT(t)
	bundleID := uuid.New()
	componentID := uuid.New()
	applicationID := uuid.New()
	channelID := uuid.New()
	processSnapshotID := uuid.New()
	versionID := uuid.New()

	response := ReleaseBundleToAPI(types.ReleaseBundle{
		ID:                bundleID,
		ApplicationID:     applicationID,
		ChannelID:         channelID,
		ProcessSnapshotID: &processSnapshotID,
		ReleaseNumber:     "2026.06.20",
		ReleaseNotes:      "Initial release",
		SourceRevision:    "abc123",
		Status:            types.ReleaseBundleStatusDraft,
		CanonicalChecksum: "sha256:abc",
		Components: []types.ReleaseBundleComponent{
			{
				ID:                   componentID,
				ReleaseBundleID:      bundleID,
				Key:                  "api",
				Name:                 "API",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	})

	g.Expect(response).To(Equal(api.ReleaseBundle{
		ID:                bundleID,
		ApplicationID:     applicationID,
		ChannelID:         channelID,
		ProcessSnapshotID: &processSnapshotID,
		ReleaseNumber:     "2026.06.20",
		ReleaseNotes:      "Initial release",
		SourceRevision:    "abc123",
		Status:            types.ReleaseBundleStatusDraft,
		CanonicalChecksum: "sha256:abc",
		Components: []api.ReleaseBundleComponent{
			{
				ID:                   componentID,
				ReleaseBundleID:      bundleID,
				Key:                  "api",
				Name:                 "API",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	}))
}

func TestReleaseBundleEligibilityToAPI(t *testing.T) {
	g := NewWithT(t)
	releaseBundleID := uuid.New()
	applicationID := uuid.New()
	channelID := uuid.New()
	lifecycleID := uuid.New()
	environmentID := uuid.New()
	phaseID := uuid.New()

	response := ReleaseBundleEligibilityToAPI(lifecycle.EligibilityResult{
		ReleaseBundleID: releaseBundleID,
		ApplicationID:   applicationID,
		ChannelID:       channelID,
		LifecycleID:     lifecycleID,
		EnvironmentID:   environmentID,
		EngineReady:     true,
		Eligible:        false,
		TargetPhase: &lifecycle.EligibilityPhase{
			ID:                 phaseID,
			Name:               "Production",
			SortOrder:          20,
			EnvironmentIDs:     []uuid.UUID{environmentID},
			MatchesEnvironment: true,
			BlocksEligibility:  true,
		},
		Phases: []lifecycle.EligibilityPhase{
			{
				ID:                 phaseID,
				Name:               "Production",
				SortOrder:          20,
				EnvironmentIDs:     []uuid.UUID{environmentID},
				MatchesEnvironment: true,
				BlocksEligibility:  true,
			},
		},
		Reasons: []lifecycle.EligibilityReason{
			{
				Code:    lifecycle.EligibilityReasonRequiredPriorPhaseIncomplete,
				Field:   "phases." + phaseID.String(),
				Message: "required lifecycle phase is incomplete",
			},
		},
	})

	g.Expect(response).To(Equal(api.ReleaseBundleEligibilityResponse{
		ReleaseBundleID: releaseBundleID,
		ApplicationID:   applicationID,
		ChannelID:       channelID,
		LifecycleID:     lifecycleID,
		EnvironmentID:   environmentID,
		EngineReady:     true,
		Eligible:        false,
		TargetPhase: &api.ReleaseBundleEligibilityPhase{
			ID:                 phaseID,
			Name:               "Production",
			SortOrder:          20,
			EnvironmentIDs:     []uuid.UUID{environmentID},
			MatchesEnvironment: true,
			BlocksEligibility:  true,
		},
		Phases: []api.ReleaseBundleEligibilityPhase{
			{
				ID:                 phaseID,
				Name:               "Production",
				SortOrder:          20,
				EnvironmentIDs:     []uuid.UUID{environmentID},
				MatchesEnvironment: true,
				BlocksEligibility:  true,
			},
		},
		Reasons: []api.ReleaseBundleEligibilityReason{
			{
				Code:    string(lifecycle.EligibilityReasonRequiredPriorPhaseIncomplete),
				Field:   "phases." + phaseID.String(),
				Message: "required lifecycle phase is incomplete",
			},
		},
	}))
}
