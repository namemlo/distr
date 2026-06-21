package types

import (
	"encoding/json"

	"github.com/google/uuid"
)

type ConfigurationDrift struct {
	DeploymentID           uuid.UUID                           `json:"deploymentId"`
	ApplicationID          uuid.UUID                           `json:"applicationId"`
	HasDrift               bool                                `json:"hasDrift"`
	NewRequiredVariables   []ConfigurationDriftVariable        `json:"newRequiredVariables"`
	MissingVariables       []ConfigurationDriftVariable        `json:"missingVariables"`
	RemovedVariables       []ConfigurationDriftRemovedValue    `json:"removedVariables"`
	TypeChanges            []ConfigurationDriftTypeChange      `json:"typeChanges"`
	DefaultChanges         []ConfigurationDriftDefaultChange   `json:"defaultChanges"`
	SecretReferenceChanges []ConfigurationDriftReferenceChange `json:"secretReferenceChanges"`
}

type ConfigurationDriftVariable struct {
	Key           string                   `json:"key"`
	Type          VariableType             `json:"type"`
	IsRequired    bool                     `json:"isRequired"`
	Source        VariableResolutionSource `json:"source"`
	Value         json.RawMessage          `json:"value,omitempty"`
	ReferenceID   string                   `json:"referenceId,omitempty"`
	ReferenceName string                   `json:"referenceName,omitempty"`
	Redacted      bool                     `json:"redacted"`
}

type ConfigurationDriftRemovedValue struct {
	Key string `json:"key"`
}

type ConfigurationDriftTypeChange struct {
	Key          string       `json:"key"`
	ExpectedType VariableType `json:"expectedType"`
	DeployedType string       `json:"deployedType"`
}

type ConfigurationDriftDefaultChange struct {
	Key           string          `json:"key"`
	Type          VariableType    `json:"type"`
	CurrentValue  json.RawMessage `json:"currentValue,omitempty"`
	DeployedValue json.RawMessage `json:"deployedValue,omitempty"`
}

type ConfigurationDriftReferenceChange struct {
	Key           string       `json:"key"`
	Type          VariableType `json:"type"`
	ReferenceID   string       `json:"referenceId,omitempty"`
	ReferenceName string       `json:"referenceName,omitempty"`
	Redacted      bool         `json:"redacted"`
}
