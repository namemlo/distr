package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/validation"
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
	Key           string          `json:"key"`
	Description   string          `json:"description"`
	Type          VariableType    `json:"type"`
	IsRequired    bool            `json:"isRequired"`
	DefaultValue  json.RawMessage `json:"defaultValue,omitempty"`
	ReferenceID   string          `json:"referenceId,omitempty"`
	ReferenceName string          `json:"referenceName,omitempty"`
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
	if isReferenceVariableType(r.Type) {
		return r.validateReference()
	}
	return r.validateDefaultValue()
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
	ID            uuid.UUID       `json:"id"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
	Key           string          `json:"key"`
	Description   string          `json:"description"`
	Type          VariableType    `json:"type"`
	IsRequired    bool            `json:"isRequired"`
	DefaultValue  json.RawMessage `json:"defaultValue,omitempty"`
	ReferenceID   string          `json:"referenceId,omitempty"`
	ReferenceName string          `json:"referenceName,omitempty"`
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
