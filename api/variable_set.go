package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/distr-sh/distr/internal/variablescope"
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

type CreateUpdateVariableSetRequest struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	SortOrder      int               `json:"sortOrder"`
	ApplicationIDs []uuid.UUID       `json:"applicationIds"`
	Variables      []VariableRequest `json:"variables"`
}

func (r *CreateUpdateVariableSetRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if r.SortOrder < 0 {
		return validation.NewValidationFailedError("sortOrder must be non-negative")
	}
	for _, applicationID := range r.ApplicationIDs {
		if applicationID == uuid.Nil {
			return validation.NewValidationFailedError("applicationIds must not contain empty IDs")
		}
	}

	keys := map[string]struct{}{}
	for i := range r.Variables {
		if err := r.Variables[i].Validate(); err != nil {
			return err
		}
		key := strings.TrimSpace(r.Variables[i].Key)
		if _, ok := keys[key]; ok {
			return validation.NewValidationFailedError("variable keys must be unique within a variable set")
		}
		keys[key] = struct{}{}
	}
	return nil
}

type VariableRequest struct {
	Key           string                       `json:"key"`
	Description   string                       `json:"description"`
	Type          VariableType                 `json:"type"`
	IsRequired    bool                         `json:"isRequired"`
	DefaultValue  json.RawMessage              `json:"defaultValue,omitempty"`
	ReferenceID   string                       `json:"referenceId,omitempty"`
	ReferenceName string                       `json:"referenceName,omitempty"`
	ScopedValues  []VariableScopedValueRequest `json:"scopedValues,omitempty"`
}

func (r *VariableRequest) Validate() error {
	r.Key = strings.TrimSpace(r.Key)
	r.ReferenceID = strings.TrimSpace(r.ReferenceID)
	r.ReferenceName = strings.TrimSpace(r.ReferenceName)
	if r.Key == "" {
		return validation.NewValidationFailedError("variable key is required")
	}
	if !isKnownVariableType(r.Type) {
		return validation.NewValidationFailedError(fmt.Sprintf("unsupported variable type %q", r.Type))
	}
	var err error
	if isReferenceVariableType(r.Type) {
		err = r.validateReference()
	} else {
		err = r.validateDefaultValue()
	}
	if err != nil {
		return err
	}
	return r.validateScopedValues()
}

func (r *VariableRequest) validateDefaultValue() error {
	if r.ReferenceID != "" || r.ReferenceName != "" {
		return validation.NewValidationFailedError("non-reference variables must not include reference metadata")
	}
	if !hasJSONValue(r.DefaultValue) {
		if r.IsRequired {
			return nil
		}
		return validation.NewValidationFailedError("defaultValue is required unless the variable is required")
	}

	value, err := decodeVariableJSON(r.DefaultValue)
	if err != nil {
		return validation.NewValidationFailedError("defaultValue must be valid JSON")
	}
	switch r.Type {
	case VariableTypeString:
		if _, ok := value.(string); !ok {
			return validation.NewValidationFailedError("string variables require a JSON string defaultValue")
		}
	case VariableTypeNumber:
		number, ok := value.(json.Number)
		if !ok {
			return validation.NewValidationFailedError("number variables require a JSON number defaultValue")
		}
		if _, err := number.Float64(); err != nil {
			return validation.NewValidationFailedError("number variables require a valid JSON number defaultValue")
		}
	case VariableTypeBoolean:
		if _, ok := value.(bool); !ok {
			return validation.NewValidationFailedError("boolean variables require a JSON boolean defaultValue")
		}
	case VariableTypeJSON:
		if value == nil {
			return validation.NewValidationFailedError("json variables require a non-null defaultValue")
		}
	}
	return nil
}

func (r *VariableRequest) validateReference() error {
	if hasJSONValue(r.DefaultValue) {
		return validation.NewValidationFailedError("reference variables must not include defaultValue")
	}
	if r.ReferenceID == "" {
		if r.IsRequired {
			return nil
		}
		return validation.NewValidationFailedError("referenceId is required unless the variable is required")
	}
	id, err := uuid.Parse(r.ReferenceID)
	if err != nil || id == uuid.Nil {
		return validation.NewValidationFailedError("referenceId must be a valid UUID")
	}
	if r.Type != VariableTypeSecretReference && r.ReferenceName == "" {
		return validation.NewValidationFailedError("referenceName is required for metadata-only references")
	}
	return nil
}

func (r *VariableRequest) validateScopedValues() error {
	scopes := map[string]struct{}{}
	for i := range r.ScopedValues {
		if err := r.ScopedValues[i].Validate(r.Type); err != nil {
			return err
		}
		key := variablescope.Key(r.ScopedValues[i].Scope.toTypesScope())
		if _, ok := scopes[key]; ok {
			return validation.NewValidationFailedError("scoped value scopes must be unique within a variable")
		}
		scopes[key] = struct{}{}
	}
	return nil
}

type VariableScopedValueRequest struct {
	Scope         VariableScopeRequest `json:"scope"`
	SortOrder     int                  `json:"sortOrder"`
	Value         json.RawMessage      `json:"value,omitempty"`
	ReferenceID   string               `json:"referenceId,omitempty"`
	ReferenceName string               `json:"referenceName,omitempty"`
}

func (r *VariableScopedValueRequest) Validate(variableType VariableType) error {
	r.ReferenceID = strings.TrimSpace(r.ReferenceID)
	r.ReferenceName = strings.TrimSpace(r.ReferenceName)
	if r.SortOrder < 0 {
		return validation.NewValidationFailedError("scoped value sortOrder must be non-negative")
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if isReferenceVariableType(variableType) {
		return r.validateReference(variableType)
	}
	return r.validateValue(variableType)
}

func (r *VariableScopedValueRequest) validateValue(variableType VariableType) error {
	if r.ReferenceID != "" || r.ReferenceName != "" {
		return validation.NewValidationFailedError("non-reference scoped values must not include reference metadata")
	}
	if !hasJSONValue(r.Value) {
		return validation.NewValidationFailedError("scoped value is required")
	}
	return validateVariableJSONValue(variableType, r.Value, "scoped value")
}

func (r *VariableScopedValueRequest) validateReference(variableType VariableType) error {
	if hasJSONValue(r.Value) {
		return validation.NewValidationFailedError("reference scoped values must not include value")
	}
	if r.ReferenceID == "" {
		return validation.NewValidationFailedError("referenceId is required for reference scoped values")
	}
	id, err := uuid.Parse(r.ReferenceID)
	if err != nil || id == uuid.Nil {
		return validation.NewValidationFailedError("referenceId must be a valid UUID")
	}
	if variableType != VariableTypeSecretReference && r.ReferenceName == "" {
		return validation.NewValidationFailedError("referenceName is required for metadata-only scoped references")
	}
	return nil
}

type VariableScopeRequest struct {
	CustomerOrganizationID *uuid.UUID `json:"customerOrganizationId,omitempty"`
	EnvironmentID          *uuid.UUID `json:"environmentId,omitempty"`
	ChannelID              *uuid.UUID `json:"channelId,omitempty"`
	DeploymentTargetID     *uuid.UUID `json:"deploymentTargetId,omitempty"`
	ApplicationID          *uuid.UUID `json:"applicationId,omitempty"`
	TargetTag              string     `json:"targetTag,omitempty"`
	ProcessStepKey         string     `json:"processStepKey,omitempty"`
}

func (r *VariableScopeRequest) Validate() error {
	r.TargetTag = strings.TrimSpace(r.TargetTag)
	r.ProcessStepKey = strings.TrimSpace(r.ProcessStepKey)
	for _, id := range []*uuid.UUID{
		r.CustomerOrganizationID,
		r.EnvironmentID,
		r.ChannelID,
		r.DeploymentTargetID,
		r.ApplicationID,
	} {
		if id != nil && *id == uuid.Nil {
			return validation.NewValidationFailedError("scope IDs must not be empty")
		}
	}
	if !variablescope.Supported(r.toTypesScope()) {
		return validation.NewValidationFailedError("unsupported scoped value shape")
	}
	return nil
}

func (r VariableScopeRequest) toTypesScope() types.VariableScope {
	return types.VariableScope{
		CustomerOrganizationID: r.CustomerOrganizationID,
		EnvironmentID:          r.EnvironmentID,
		ChannelID:              r.ChannelID,
		DeploymentTargetID:     r.DeploymentTargetID,
		ApplicationID:          r.ApplicationID,
		TargetTag:              strings.TrimSpace(r.TargetTag),
		ProcessStepKey:         strings.TrimSpace(r.ProcessStepKey),
	}
}

type VariableSet struct {
	ID             uuid.UUID   `json:"id"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
	Name           string      `json:"name"`
	Description    string      `json:"description"`
	SortOrder      int         `json:"sortOrder"`
	ApplicationIDs []uuid.UUID `json:"applicationIds"`
	Variables      []Variable  `json:"variables"`
}

type Variable struct {
	ID            uuid.UUID             `json:"id"`
	CreatedAt     time.Time             `json:"createdAt"`
	UpdatedAt     time.Time             `json:"updatedAt"`
	Key           string                `json:"key"`
	Description   string                `json:"description"`
	Type          VariableType          `json:"type"`
	IsRequired    bool                  `json:"isRequired"`
	DefaultValue  json.RawMessage       `json:"defaultValue,omitempty"`
	ReferenceID   string                `json:"referenceId,omitempty"`
	ReferenceName string                `json:"referenceName,omitempty"`
	ScopedValues  []VariableScopedValue `json:"scopedValues,omitempty"`
}

type VariableScopedValue struct {
	ID            uuid.UUID       `json:"id"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
	Scope         VariableScope   `json:"scope"`
	SortOrder     int             `json:"sortOrder"`
	Value         json.RawMessage `json:"value,omitempty"`
	ReferenceID   string          `json:"referenceId,omitempty"`
	ReferenceName string          `json:"referenceName,omitempty"`
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

type ResolveVariablesPreviewRequest struct {
	VariableSetIDs []uuid.UUID                    `json:"variableSetIds"`
	Scope          VariableResolutionScopeRequest `json:"scope"`
	PromptedValues []VariablePromptedValueRequest `json:"promptedValues,omitempty"`
}

func (r *ResolveVariablesPreviewRequest) Validate() error {
	if len(r.VariableSetIDs) == 0 {
		return validation.NewValidationFailedError("variableSetIds is required")
	}
	ids := map[uuid.UUID]struct{}{}
	for _, id := range r.VariableSetIDs {
		if id == uuid.Nil {
			return validation.NewValidationFailedError("variableSetIds must not contain empty IDs")
		}
		if _, ok := ids[id]; ok {
			return validation.NewValidationFailedError("variableSetIds must be unique")
		}
		ids[id] = struct{}{}
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	keys := map[string]struct{}{}
	for i := range r.PromptedValues {
		if err := r.PromptedValues[i].Validate(); err != nil {
			return err
		}
		key := r.PromptedValues[i].Key
		if _, ok := keys[key]; ok {
			return validation.NewValidationFailedError("prompted value keys must be unique")
		}
		keys[key] = struct{}{}
	}
	return nil
}

type VariableResolutionScopeRequest struct {
	CustomerOrganizationID *uuid.UUID `json:"customerOrganizationId,omitempty"`
	EnvironmentID          *uuid.UUID `json:"environmentId,omitempty"`
	ChannelID              *uuid.UUID `json:"channelId,omitempty"`
	DeploymentTargetID     *uuid.UUID `json:"deploymentTargetId,omitempty"`
	ApplicationID          *uuid.UUID `json:"applicationId,omitempty"`
	TargetTags             []string   `json:"targetTags,omitempty"`
	ProcessStepKey         string     `json:"processStepKey,omitempty"`
}

func (r *VariableResolutionScopeRequest) Validate() error {
	r.ProcessStepKey = strings.TrimSpace(r.ProcessStepKey)
	for _, id := range []*uuid.UUID{
		r.CustomerOrganizationID,
		r.EnvironmentID,
		r.ChannelID,
		r.DeploymentTargetID,
		r.ApplicationID,
	} {
		if id != nil && *id == uuid.Nil {
			return validation.NewValidationFailedError("scope IDs must not be empty")
		}
	}
	tags := map[string]struct{}{}
	for i := range r.TargetTags {
		r.TargetTags[i] = strings.TrimSpace(r.TargetTags[i])
		if r.TargetTags[i] == "" {
			return validation.NewValidationFailedError("targetTags must not contain empty values")
		}
		if _, ok := tags[r.TargetTags[i]]; ok {
			return validation.NewValidationFailedError("targetTags must be unique")
		}
		tags[r.TargetTags[i]] = struct{}{}
	}
	return nil
}

type VariablePromptedValueRequest struct {
	Key           string          `json:"key"`
	Value         json.RawMessage `json:"value,omitempty"`
	ReferenceID   string          `json:"referenceId,omitempty"`
	ReferenceName string          `json:"referenceName,omitempty"`
}

func (r *VariablePromptedValueRequest) Validate() error {
	r.Key = strings.TrimSpace(r.Key)
	r.ReferenceID = strings.TrimSpace(r.ReferenceID)
	r.ReferenceName = strings.TrimSpace(r.ReferenceName)
	if r.Key == "" {
		return validation.NewValidationFailedError("prompted value key is required")
	}
	hasValue := hasJSONValue(r.Value)
	hasReference := r.ReferenceID != "" || r.ReferenceName != ""
	if hasValue && hasReference {
		return validation.NewValidationFailedError("prompted values must include either value or reference metadata")
	}
	if !hasValue && !hasReference {
		return validation.NewValidationFailedError("prompted values must include value or reference metadata")
	}
	if hasValue {
		if _, err := decodeVariableJSON(r.Value); err != nil {
			return validation.NewValidationFailedError("prompted value must be valid JSON")
		}
		return nil
	}
	id, err := uuid.Parse(r.ReferenceID)
	if err != nil || id == uuid.Nil {
		return validation.NewValidationFailedError("referenceId must be a valid UUID")
	}
	return nil
}

type ResolvedVariable struct {
	VariableSetID uuid.UUID                      `json:"variableSetId"`
	VariableID    uuid.UUID                      `json:"variableId"`
	Key           string                         `json:"key"`
	Type          VariableType                   `json:"type"`
	IsRequired    bool                           `json:"isRequired"`
	Status        string                         `json:"status"`
	Source        string                         `json:"source"`
	Value         json.RawMessage                `json:"value,omitempty"`
	ReferenceID   string                         `json:"referenceId,omitempty"`
	ReferenceName string                         `json:"referenceName,omitempty"`
	Redacted      bool                           `json:"redacted"`
	Trace         []VariableResolutionTraceEntry `json:"trace"`
}

type VariableResolutionTraceEntry struct {
	Source   string        `json:"source"`
	Scope    VariableScope `json:"scope,omitempty"`
	Selected bool          `json:"selected"`
	Reason   string        `json:"reason"`
}

func isKnownVariableType(value VariableType) bool {
	switch value {
	case VariableTypeString,
		VariableTypeNumber,
		VariableTypeBoolean,
		VariableTypeJSON,
		VariableTypeSecretReference,
		VariableTypeAccountReference,
		VariableTypeCertificateReference:
		return true
	default:
		return false
	}
}

func isReferenceVariableType(value VariableType) bool {
	switch value {
	case VariableTypeSecretReference, VariableTypeAccountReference, VariableTypeCertificateReference:
		return true
	default:
		return false
	}
}

func hasJSONValue(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func decodeVariableJSON(value json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("defaultValue must contain one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return decoded, nil
}

func validateVariableJSONValue(variableType VariableType, raw json.RawMessage, field string) error {
	value, err := decodeVariableJSON(raw)
	if err != nil {
		return validation.NewValidationFailedError(field + " must be valid JSON")
	}
	switch variableType {
	case VariableTypeString:
		if _, ok := value.(string); !ok {
			return validation.NewValidationFailedError("string variables require a JSON string " + field)
		}
	case VariableTypeNumber:
		number, ok := value.(json.Number)
		if !ok {
			return validation.NewValidationFailedError("number variables require a JSON number " + field)
		}
		if _, err := number.Float64(); err != nil {
			return validation.NewValidationFailedError("number variables require a valid JSON number " + field)
		}
	case VariableTypeBoolean:
		if _, ok := value.(bool); !ok {
			return validation.NewValidationFailedError("boolean variables require a JSON boolean " + field)
		}
	case VariableTypeJSON:
		if value == nil {
			return validation.NewValidationFailedError("json variables require a non-null " + field)
		}
	}
	return nil
}
