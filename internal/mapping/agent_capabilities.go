package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func AgentCapabilitiesToAPI(report types.AgentCapabilityReport) api.AgentCapabilities {
	return api.AgentCapabilities{
		ID:                    report.ID,
		CreatedAt:             report.CreatedAt,
		UpdatedAt:             report.UpdatedAt,
		DeploymentTargetID:    report.DeploymentTargetID,
		ProtocolVersion:       report.ProtocolVersion,
		AgentVersion:          report.AgentVersion,
		SupportedRuntimes:     report.SupportedRuntimes,
		SupportedActions:      List(report.SupportedActions, AgentActionCapabilityToAPI),
		OperatingSystem:       report.OperatingSystem,
		Architecture:          report.Architecture,
		AvailableTooling:      report.AvailableTooling,
		StrategyCapabilities:  report.StrategyCapabilities,
		CompatibilityWarnings: report.CompatibilityWarnings,
	}
}

func AgentActionCapabilityToAPI(action types.AgentActionCapability) api.AgentActionCapability {
	return api.AgentActionCapability{
		ActionType: action.ActionType,
		Versions:   action.Versions,
	}
}

func AgentCapabilitiesRequestToInternal(
	request api.AgentCapabilitiesRequest,
	orgID uuid.UUID,
	deploymentTargetID uuid.UUID,
) types.AgentCapabilityReport {
	return types.AgentCapabilityReport{
		OrganizationID:       orgID,
		DeploymentTargetID:   deploymentTargetID,
		ProtocolVersion:      request.ProtocolVersion,
		AgentVersion:         request.AgentVersion,
		SupportedRuntimes:    request.SupportedRuntimes,
		OperatingSystem:      request.OperatingSystem,
		Architecture:         request.Architecture,
		AvailableTooling:     request.AvailableTooling,
		StrategyCapabilities: request.StrategyCapabilities,
		SupportedActions:     List(request.SupportedActions, AgentActionCapabilityRequestToInternal),
	}
}

func AgentActionCapabilityRequestToInternal(
	action api.AgentActionCapabilityRequest,
) types.AgentActionCapability {
	return types.AgentActionCapability{
		ActionType: action.ActionType,
		Versions:   action.Versions,
	}
}
