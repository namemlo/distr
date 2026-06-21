package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type VariableType string

const (
	VariableTypeString               VariableType = "string"
	VariableTypeNumber               VariableType = "number"
	VariableTypeBoolean              VariableType = "boolean"
	VariableTypeJSON                 VariableType = "json"
	VariableTypeSecretReference      VariableType = "secret_reference"
	VariableTypeAccountReference     VariableType = "account_reference"
	VariableTypeCertificateReference VariableType = "certificate_reference"
)

type VariableSet struct {
	ID             uuid.UUID   `db:"id" json:"id"`
	CreatedAt      time.Time   `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time   `db:"updated_at" json:"updatedAt"`
	OrganizationID uuid.UUID   `db:"organization_id" json:"organizationId"`
	Name           string      `db:"name" json:"name"`
	Description    string      `db:"description" json:"description"`
	SortOrder      int         `db:"sort_order" json:"sortOrder"`
	ApplicationIDs []uuid.UUID `db:"-" json:"applicationIds"`
	Variables      []Variable  `db:"-" json:"variables"`
}

type Variable struct {
	ID             uuid.UUID             `db:"id" json:"id"`
	CreatedAt      time.Time             `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time             `db:"updated_at" json:"updatedAt"`
	OrganizationID uuid.UUID             `db:"organization_id" json:"organizationId"`
	VariableSetID  uuid.UUID             `db:"variable_set_id" json:"variableSetId"`
	Key            string                `db:"key" json:"key"`
	Description    string                `db:"description" json:"description"`
	Type           VariableType          `db:"type" json:"type"`
	IsRequired     bool                  `db:"is_required" json:"isRequired"`
	DefaultValue   json.RawMessage       `db:"default_value" json:"defaultValue,omitempty"`
	ReferenceID    string                `db:"reference_id" json:"referenceId,omitempty"`
	ReferenceName  string                `db:"reference_name" json:"referenceName,omitempty"`
	ScopedValues   []VariableScopedValue `db:"-" json:"scopedValues"`
}

type VariableScope struct {
	CustomerOrganizationID *uuid.UUID `json:"customerOrganizationId,omitempty"`
	EnvironmentID          *uuid.UUID `json:"environmentId,omitempty"`
	ChannelID              *uuid.UUID `json:"channelId,omitempty"`
	DeploymentTargetID     *uuid.UUID `json:"deploymentTargetId,omitempty"`
	ApplicationID          *uuid.UUID `json:"applicationId,omitempty"`
	TargetTag              string     `json:"targetTag,omitempty"`
	ProcessStepKey         string     `json:"processStepKey,omitempty"`
}

type VariableResolutionScope struct {
	CustomerOrganizationID *uuid.UUID `json:"customerOrganizationId,omitempty"`
	EnvironmentID          *uuid.UUID `json:"environmentId,omitempty"`
	ChannelID              *uuid.UUID `json:"channelId,omitempty"`
	DeploymentTargetID     *uuid.UUID `json:"deploymentTargetId,omitempty"`
	ApplicationID          *uuid.UUID `json:"applicationId,omitempty"`
	TargetTags             []string   `json:"targetTags,omitempty"`
	ProcessStepKey         string     `json:"processStepKey,omitempty"`
}

type VariableScopedValue struct {
	ID             uuid.UUID       `db:"id" json:"id"`
	CreatedAt      time.Time       `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time       `db:"updated_at" json:"updatedAt"`
	OrganizationID uuid.UUID       `db:"organization_id" json:"organizationId"`
	VariableSetID  uuid.UUID       `db:"variable_set_id" json:"variableSetId"`
	VariableID     uuid.UUID       `db:"variable_id" json:"variableId"`
	Scope          VariableScope   `db:"-" json:"scope"`
	SortOrder      int             `db:"sort_order" json:"sortOrder"`
	Value          json.RawMessage `db:"value" json:"value,omitempty"`
	ReferenceID    string          `db:"reference_id" json:"referenceId,omitempty"`
	ReferenceName  string          `db:"reference_name" json:"referenceName,omitempty"`
}

type VariablePromptedValue struct {
	Key           string          `json:"key"`
	Value         json.RawMessage `json:"value,omitempty"`
	ReferenceID   string          `json:"referenceId,omitempty"`
	ReferenceName string          `json:"referenceName,omitempty"`
}

type VariableResolutionStatus string

const (
	VariableResolutionStatusResolved   VariableResolutionStatus = "resolved"
	VariableResolutionStatusUnresolved VariableResolutionStatus = "unresolved"
)

type VariableResolutionSource string

const (
	VariableResolutionSourcePrompted               VariableResolutionSource = "prompted"
	VariableResolutionSourceExactTenantEnvironment VariableResolutionSource = "exact_tenant_environment"
	VariableResolutionSourceExactEnvironment       VariableResolutionSource = "exact_environment"
	VariableResolutionSourceChannel                VariableResolutionSource = "channel"
	VariableResolutionSourceApplication            VariableResolutionSource = "application"
	VariableResolutionSourceDefault                VariableResolutionSource = "default"
	VariableResolutionSourceUnresolved             VariableResolutionSource = "unresolved"
)

const VariableResolutionSourceExactTenantEnvironmentTargetChannelStep = VariableResolutionSource(
	"exact_tenant_environment_target_channel_step",
)

const VariableResolutionSourceExactTenantEnvironmentTarget = VariableResolutionSource(
	"exact_tenant_environment_target",
)

const VariableResolutionSourceExactTenantEnvironmentChannel = VariableResolutionSource(
	"exact_tenant_environment_channel",
)

const VariableResolutionSourceExactEnvironmentTargetTag = VariableResolutionSource(
	"exact_environment_target_tag",
)

type VariableResolutionTraceEntry struct {
	Source   VariableResolutionSource `json:"source"`
	Scope    VariableScope            `json:"scope,omitempty"`
	Selected bool                     `json:"selected"`
	Reason   string                   `json:"reason"`
}

type ResolvedVariable struct {
	VariableSetID uuid.UUID                      `json:"variableSetId"`
	VariableID    uuid.UUID                      `json:"variableId"`
	Key           string                         `json:"key"`
	Type          VariableType                   `json:"type"`
	IsRequired    bool                           `json:"isRequired"`
	Status        VariableResolutionStatus       `json:"status"`
	Source        VariableResolutionSource       `json:"source"`
	Value         json.RawMessage                `json:"value,omitempty"`
	ReferenceID   string                         `json:"referenceId,omitempty"`
	ReferenceName string                         `json:"referenceName,omitempty"`
	Redacted      bool                           `json:"redacted"`
	Trace         []VariableResolutionTraceEntry `json:"trace"`
}
