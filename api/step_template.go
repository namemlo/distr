package api

import (
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type ImportStepTemplateRequest struct {
	SourceType                string         `json:"sourceType"`
	SourceRef                 string         `json:"sourceRef"`
	Name                      string         `json:"name"`
	Description               string         `json:"description"`
	Category                  string         `json:"category"`
	Version                   string         `json:"version"`
	ActionType                string         `json:"actionType"`
	ExecutionLocation         string         `json:"executionLocation"`
	InputSchema               map[string]any `json:"inputSchema"`
	OutputSchema              map[string]any `json:"outputSchema"`
	DefaultInputBindings      map[string]any `json:"defaultInputBindings"`
	MinimumAgentVersion       string         `json:"minimumAgentVersion"`
	CompatibleActionVersion   string         `json:"compatibleActionVersion"`
	RuntimeCompatibilityNotes string         `json:"runtimeCompatibilityNotes"`
	Deprecated                bool           `json:"deprecated"`
}

func (r *ImportStepTemplateRequest) Validate() error {
	r.SourceType = strings.TrimSpace(r.SourceType)
	r.SourceRef = strings.TrimSpace(r.SourceRef)
	r.Name = strings.TrimSpace(r.Name)
	r.Description = strings.TrimSpace(r.Description)
	r.Category = strings.TrimSpace(r.Category)
	r.Version = strings.TrimSpace(r.Version)
	r.ActionType = strings.TrimSpace(r.ActionType)
	r.ExecutionLocation = strings.TrimSpace(r.ExecutionLocation)
	r.MinimumAgentVersion = strings.TrimSpace(r.MinimumAgentVersion)
	r.CompatibleActionVersion = strings.TrimSpace(r.CompatibleActionVersion)
	r.RuntimeCompatibilityNotes = strings.TrimSpace(r.RuntimeCompatibilityNotes)
	if r.SourceType != "builtin" && r.SourceType != "oci" {
		return validation.NewValidationFailedError("sourceType must be builtin or oci")
	}
	if r.SourceRef == "" {
		return validation.NewValidationFailedError("sourceRef is required")
	}
	if r.Name == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if r.Version == "" {
		return validation.NewValidationFailedError("version is required")
	}
	if r.ActionType == "" {
		return validation.NewValidationFailedError("actionType is required")
	}
	if r.ExecutionLocation == "" {
		return validation.NewValidationFailedError("executionLocation is required")
	}
	if r.InputSchema == nil {
		r.InputSchema = map[string]any{"type": "object", "additionalProperties": true}
	}
	if r.OutputSchema == nil {
		r.OutputSchema = map[string]any{"type": "object", "additionalProperties": true}
	}
	if r.DefaultInputBindings == nil {
		r.DefaultInputBindings = map[string]any{}
	}
	return nil
}

type StepTemplate struct {
	ID                       uuid.UUID             `json:"id"`
	CreatedAt                time.Time             `json:"createdAt"`
	UpdatedAt                time.Time             `json:"updatedAt"`
	SourceType               string                `json:"sourceType"`
	SourceRef                string                `json:"sourceRef"`
	Name                     string                `json:"name"`
	Description              string                `json:"description"`
	Category                 string                `json:"category"`
	InstalledAt              time.Time             `json:"installedAt"`
	InstalledByUserAccountID *uuid.UUID            `json:"installedByUserAccountId,omitempty"`
	Versions                 []StepTemplateVersion `json:"versions"`
}

type StepTemplateVersion struct {
	ID                        uuid.UUID      `json:"id"`
	CreatedAt                 time.Time      `json:"createdAt"`
	StepTemplateID            uuid.UUID      `json:"stepTemplateId"`
	Version                   string         `json:"version"`
	ActionType                string         `json:"actionType"`
	ExecutionLocation         string         `json:"executionLocation"`
	InputSchema               map[string]any `json:"inputSchema"`
	OutputSchema              map[string]any `json:"outputSchema"`
	DefaultInputBindings      map[string]any `json:"defaultInputBindings"`
	MinimumAgentVersion       string         `json:"minimumAgentVersion"`
	CompatibleActionVersion   string         `json:"compatibleActionVersion"`
	RuntimeCompatibilityNotes string         `json:"runtimeCompatibilityNotes"`
	Deprecated                bool           `json:"deprecated"`
}
