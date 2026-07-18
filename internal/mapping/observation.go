package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func ObserverRegistrationToAPI(
	registration types.ObserverRegistration,
) api.ObserverRegistration {
	return api.ObserverRegistration{
		ID: registration.ID, CreatedAt: registration.CreatedAt,
		UpdatedAt: registration.UpdatedAt, DeploymentUnitID: registration.DeploymentUnitID,
		ComponentInstanceID:   registration.ComponentInstanceID,
		ObserverKey:           registration.ObserverKey,
		AdapterImplementation: registration.AdapterImplementation,
		AdapterVersion:        registration.AdapterVersion, Enabled: registration.Enabled,
		MaxFreshnessSeconds: int64(registration.MaxFreshness.Seconds()),
		MaxClockSkewSeconds: int64(registration.MaxClockSkew.Seconds()),
		Measurements:        registration.Measurements,
	}
}

func ObservedComponentStateToAPI(
	state types.ObservedComponentState,
) api.ObservedComponentState {
	return api.ObservedComponentState{
		ID: state.ID, CreatedAt: state.CreatedAt, ObserverID: state.ObserverID,
		DeploymentUnitID:    state.DeploymentUnitID,
		ComponentInstanceID: state.ComponentInstanceID, ComponentKey: state.ComponentKey,
		SourceSequence: state.SourceSequence, CapturedAt: state.CapturedAt,
		ReceivedAt: state.ReceivedAt, EvidenceChecksum: state.EvidenceChecksum,
		EvidenceReference: state.EvidenceReference, ArtifactDigest: state.ArtifactDigest,
		ConfigChecksum: state.ConfigChecksum, SchemaVersion: state.SchemaVersion,
		CapabilityChecksum: state.CapabilityChecksum, Platform: state.Platform,
		TopologyChecksum: state.TopologyChecksum, Health: state.Health,
		Outcome: state.Outcome, Disposition: state.Disposition, Trusted: state.Trusted,
		Current: state.Current, StateChecksum: state.StateChecksum,
	}
}
