package api

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type VariableSnapshot struct {
	ID                uuid.UUID               `json:"id"`
	CreatedAt         time.Time               `json:"createdAt"`
	ReleaseBundleID   uuid.UUID               `json:"releaseBundleId"`
	ApplicationID     uuid.UUID               `json:"applicationId"`
	ChannelID         uuid.UUID               `json:"channelId"`
	CanonicalChecksum string                  `json:"canonicalChecksum"`
	ResolutionMode    string                  `json:"resolutionMode"`
	Values            []VariableSnapshotValue `json:"values"`
}

type VariableSnapshotValue struct {
	ID                 uuid.UUID                      `json:"id"`
	VariableSnapshotID uuid.UUID                      `json:"variableSnapshotId"`
	VariableSetID      uuid.UUID                      `json:"variableSetId"`
	VariableID         uuid.UUID                      `json:"variableId"`
	Key                string                         `json:"key"`
	Type               VariableType                   `json:"type"`
	IsRequired         bool                           `json:"isRequired"`
	Status             string                         `json:"status"`
	Source             string                         `json:"source"`
	Value              json.RawMessage                `json:"value,omitempty"`
	ReferenceID        string                         `json:"referenceId,omitempty"`
	ReferenceName      string                         `json:"referenceName,omitempty"`
	Redacted           bool                           `json:"redacted"`
	Trace              []VariableResolutionTraceEntry `json:"trace"`
}

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
	Key           string          `json:"key"`
	Type          VariableType    `json:"type"`
	IsRequired    bool            `json:"isRequired"`
	Source        string          `json:"source"`
	Value         json.RawMessage `json:"value,omitempty"`
	ReferenceID   string          `json:"referenceId,omitempty"`
	ReferenceName string          `json:"referenceName,omitempty"`
	Redacted      bool            `json:"redacted"`
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
