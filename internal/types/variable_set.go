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
	ID             uuid.UUID       `db:"id" json:"id"`
	CreatedAt      time.Time       `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time       `db:"updated_at" json:"updatedAt"`
	OrganizationID uuid.UUID       `db:"organization_id" json:"organizationId"`
	VariableSetID  uuid.UUID       `db:"variable_set_id" json:"variableSetId"`
	Key            string          `db:"key" json:"key"`
	Description    string          `db:"description" json:"description"`
	Type           VariableType    `db:"type" json:"type"`
	IsRequired     bool            `db:"is_required" json:"isRequired"`
	DefaultValue   json.RawMessage `db:"default_value" json:"defaultValue,omitempty"`
	ReferenceID    string          `db:"reference_id" json:"referenceId,omitempty"`
	ReferenceName  string          `db:"reference_name" json:"referenceName,omitempty"`
}
