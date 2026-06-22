package types

import (
	"time"

	"github.com/google/uuid"
)

type StepTemplateSourceType string

const (
	StepTemplateSourceBuiltin StepTemplateSourceType = "builtin"
	StepTemplateSourceOCI     StepTemplateSourceType = "oci"
)

func (s StepTemplateSourceType) IsValid() bool {
	return s == StepTemplateSourceBuiltin || s == StepTemplateSourceOCI
}

type StepTemplate struct {
	ID                       uuid.UUID              `db:"id" json:"id"`
	CreatedAt                time.Time              `db:"created_at" json:"createdAt"`
	UpdatedAt                time.Time              `db:"updated_at" json:"updatedAt"`
	OrganizationID           uuid.UUID              `db:"organization_id" json:"organizationId"`
	SourceType               StepTemplateSourceType `db:"source_type" json:"sourceType"`
	SourceRef                string                 `db:"source_ref" json:"sourceRef"`
	Name                     string                 `db:"name" json:"name"`
	Description              string                 `db:"description" json:"description"`
	Category                 string                 `db:"category" json:"category"`
	InstalledAt              time.Time              `db:"installed_at" json:"installedAt"`
	InstalledByUserAccountID *uuid.UUID             `db:"installed_by_useraccount_id" json:"installedByUserAccountId,omitempty"`
	Versions                 []StepTemplateVersion  `db:"-" json:"versions"`
}

type StepTemplateVersion struct {
	ID                        uuid.UUID      `db:"id" json:"id"`
	CreatedAt                 time.Time      `db:"created_at" json:"createdAt"`
	StepTemplateID            uuid.UUID      `db:"step_template_id" json:"stepTemplateId"`
	OrganizationID            uuid.UUID      `db:"organization_id" json:"organizationId"`
	Version                   string         `db:"version" json:"version"`
	ActionType                string         `db:"action_type" json:"actionType"`
	ExecutionLocation         string         `db:"execution_location" json:"executionLocation"`
	InputSchema               map[string]any `db:"input_schema" json:"inputSchema"`
	OutputSchema              map[string]any `db:"output_schema" json:"outputSchema"`
	DefaultInputBindings      map[string]any `db:"default_input_bindings" json:"defaultInputBindings"`
	MinimumAgentVersion       string         `db:"minimum_agent_version" json:"minimumAgentVersion"`
	CompatibleActionVersion   string         `db:"compatible_action_version" json:"compatibleActionVersion"`
	RuntimeCompatibilityNotes string         `db:"runtime_compatibility_notes" json:"runtimeCompatibilityNotes"`
	Deprecated                bool           `db:"deprecated" json:"deprecated"`
}

type StepTemplateImport struct {
	OrganizationID            uuid.UUID
	InstalledByUserAccountID  *uuid.UUID
	SourceType                StepTemplateSourceType
	SourceRef                 string
	Name                      string
	Description               string
	Category                  string
	Version                   string
	ActionType                string
	ExecutionLocation         string
	InputSchema               map[string]any
	OutputSchema              map[string]any
	DefaultInputBindings      map[string]any
	MinimumAgentVersion       string
	CompatibleActionVersion   string
	RuntimeCompatibilityNotes string
	Deprecated                bool
}
