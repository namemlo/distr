package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/actionregistry"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type AgentCapabilitiesRequest struct {
	ProtocolVersion      string                         `json:"protocolVersion"`
	AgentVersion         string                         `json:"agentVersion"`
	SupportedRuntimes    []string                       `json:"supportedRuntimes"`
	SupportedActions     []AgentActionCapabilityRequest `json:"supportedActions"`
	OperatingSystem      string                         `json:"operatingSystem"`
	Architecture         string                         `json:"architecture"`
	AvailableTooling     []string                       `json:"availableTooling"`
	StrategyCapabilities []string                       `json:"strategyCapabilities"`
}

type AgentActionCapabilityRequest struct {
	ActionType string   `json:"actionType"`
	Versions   []string `json:"versions"`
}

func (r *AgentCapabilitiesRequest) Validate() error {
	r.ProtocolVersion = strings.TrimSpace(r.ProtocolVersion)
	if r.ProtocolVersion == "" {
		return validation.NewValidationFailedError("protocolVersion is required")
	}
	if r.ProtocolVersion != types.AgentCapabilityProtocolV1 {
		return validation.NewValidationFailedError("protocolVersion is unsupported")
	}
	r.AgentVersion = strings.TrimSpace(r.AgentVersion)
	if r.AgentVersion == "" {
		return validation.NewValidationFailedError("agentVersion is required")
	}
	r.OperatingSystem = strings.TrimSpace(r.OperatingSystem)
	if r.OperatingSystem == "" {
		return validation.NewValidationFailedError("operatingSystem is required")
	}
	r.Architecture = strings.TrimSpace(r.Architecture)
	if r.Architecture == "" {
		return validation.NewValidationFailedError("architecture is required")
	}
	var err error
	if r.SupportedRuntimes, err = trimUniqueRequiredStringList(
		r.SupportedRuntimes,
		"supportedRuntimes",
		true,
	); err != nil {
		return err
	}
	if r.AvailableTooling, err = trimUniqueRequiredStringList(
		r.AvailableTooling,
		"availableTooling",
		false,
	); err != nil {
		return err
	}
	if r.StrategyCapabilities, err = trimUniqueRequiredStringList(
		r.StrategyCapabilities,
		"strategyCapabilities",
		false,
	); err != nil {
		return err
	}
	seenActions := map[string]struct{}{}
	registry := actionregistry.DefaultRegistry()
	for i := range r.SupportedActions {
		action := &r.SupportedActions[i]
		action.ActionType = strings.TrimSpace(action.ActionType)
		if action.ActionType == "" {
			return validation.NewValidationFailedError(
				fmt.Sprintf("supportedActions[%d].actionType is required", i),
			)
		}
		if _, ok := registry.Get(action.ActionType); !ok {
			return validation.NewValidationFailedError(
				fmt.Sprintf("supportedActions[%d].actionType is unknown", i),
			)
		}
		if _, ok := seenActions[action.ActionType]; ok {
			return validation.NewValidationFailedError("supportedActions actionType values must be unique")
		}
		seenActions[action.ActionType] = struct{}{}
		action.Versions, err = trimUniqueRequiredStringList(
			action.Versions,
			fmt.Sprintf("supportedActions[%d].versions", i),
			true,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

type AgentCapabilities struct {
	ID                    uuid.UUID               `json:"id"`
	CreatedAt             time.Time               `json:"createdAt"`
	UpdatedAt             time.Time               `json:"updatedAt"`
	DeploymentTargetID    uuid.UUID               `json:"deploymentTargetId"`
	ProtocolVersion       string                  `json:"protocolVersion"`
	AgentVersion          string                  `json:"agentVersion"`
	SupportedRuntimes     []string                `json:"supportedRuntimes"`
	SupportedActions      []AgentActionCapability `json:"supportedActions"`
	OperatingSystem       string                  `json:"operatingSystem"`
	Architecture          string                  `json:"architecture"`
	AvailableTooling      []string                `json:"availableTooling"`
	StrategyCapabilities  []string                `json:"strategyCapabilities"`
	CompatibilityWarnings []string                `json:"compatibilityWarnings"`
}

type AgentActionCapability struct {
	ActionType string   `json:"actionType"`
	Versions   []string `json:"versions"`
}

func trimUniqueRequiredStringList(values []string, field string, requireNonEmpty bool) ([]string, error) {
	if len(values) == 0 {
		if requireNonEmpty {
			return nil, validation.NewValidationFailedError(field + " is required")
		}
		return []string{}, nil
	}
	trimmed := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, validation.NewValidationFailedError(field + " must not contain empty values")
		}
		if _, ok := seen[value]; ok {
			return nil, validation.NewValidationFailedError(field + " must be unique")
		}
		seen[value] = struct{}{}
		trimmed = append(trimmed, value)
	}
	return trimmed, nil
}
