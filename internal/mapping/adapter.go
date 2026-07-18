package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func AdapterImplementationFromCreateRequest(
	organizationID uuid.UUID,
	request api.CreateAdapterImplementationRequest,
) types.AdapterImplementation {
	return types.AdapterImplementation{
		OrganizationID: organizationID,
		Key:            request.Key,
		Name:           request.Name,
		Version:        request.Version,
		Enabled:        request.Enabled,
		Capabilities: List(request.Capabilities, func(value api.AdapterCapabilityRequest) types.AdapterCapability {
			return types.AdapterCapability{Capability: value.Capability, Version: value.Version}
		}),
	}
}

func AdapterImplementationToAPI(value types.AdapterImplementation) api.AdapterImplementation {
	return api.AdapterImplementation{
		ID: value.ID, CreatedAt: value.CreatedAt, Key: value.Key, Name: value.Name,
		Version: value.Version, Enabled: value.Enabled,
		Capabilities: List(value.Capabilities, func(capability types.AdapterCapability) api.AdapterCapability {
			return api.AdapterCapability{
				Capability: capability.Capability, Version: capability.Version,
			}
		}),
	}
}

func AdapterAssignmentFromCreateRequest(
	organizationID uuid.UUID,
	request api.CreateAdapterAssignmentRequest,
) types.AdapterAssignment {
	return types.AdapterAssignment{
		OrganizationID:          organizationID,
		AdapterImplementationID: request.AdapterImplementationID,
		ScopeType:               request.ScopeType,
		ScopeReference:          request.ScopeReference,
		ConfigSnapshotID:        request.ConfigSnapshotID,
		ConfigChecksum:          request.ConfigChecksum,
		KeyConfiguration: types.AdapterKeyConfiguration{
			KeyID:                        request.KeyConfiguration.KeyID,
			PublicKeyFingerprint:         request.KeyConfiguration.PublicKeyFingerprint,
			SigningKeyReference:          request.KeyConfiguration.SigningKeyReference,
			SigningKeyVersionFingerprint: request.KeyConfiguration.SigningKeyVersionFingerprint,
		},
		Enabled: request.Enabled,
	}
}

func AdapterAssignmentToAPI(value types.AdapterAssignment) api.AdapterAssignment {
	value.NormalizeKeyConfiguration()
	return api.AdapterAssignment{
		ID: value.ID, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		AdapterImplementationID: value.AdapterImplementationID,
		ScopeType:               value.ScopeType, ScopeReference: value.ScopeReference,
		ConfigSnapshotID: value.ConfigSnapshotID, ConfigChecksum: value.ConfigChecksum,
		KeyConfiguration: api.AdapterKeyConfiguration{
			KeyID:                        value.KeyConfiguration.KeyID,
			PublicKeyFingerprint:         value.KeyConfiguration.PublicKeyFingerprint,
			SigningKeyReference:          value.KeyConfiguration.SigningKeyReference,
			SigningKeyVersionFingerprint: value.KeyConfiguration.SigningKeyVersionFingerprint,
		},
		Enabled: value.Enabled,
	}
}

func DeploymentPlanStepAdapterToAPI(
	value types.DeploymentPlanStepAdapter,
) api.DeploymentPlanStepAdapter {
	value.NormalizeKeyConfiguration()
	return api.DeploymentPlanStepAdapter{
		StepKey: value.StepKey, AdapterAssignmentID: value.AdapterAssignmentID,
		AdapterImplementationID: value.AdapterImplementationID,
		ImplementationVersion:   value.ImplementationVersion,
		Capability:              value.Capability, CapabilityVersion: value.CapabilityVersion,
		ScopeType: value.ScopeType, ScopeReference: value.ScopeReference,
		ConfigSnapshotID: value.ConfigSnapshotID, ConfigChecksum: value.ConfigChecksum,
		KeyConfiguration: api.AdapterKeyConfiguration{
			KeyID:                        value.KeyConfiguration.KeyID,
			PublicKeyFingerprint:         value.KeyConfiguration.PublicKeyFingerprint,
			SigningKeyReference:          value.KeyConfiguration.SigningKeyReference,
			SigningKeyVersionFingerprint: value.KeyConfiguration.SigningKeyVersionFingerprint,
		},
		SortOrder: value.SortOrder,
	}
}
