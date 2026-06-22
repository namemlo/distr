package types

import (
	"time"

	"github.com/google/uuid"
)

type Runbook struct {
	ID             uuid.UUID `db:"id" json:"id"`
	CreatedAt      time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time `db:"updated_at" json:"updatedAt"`
	OrganizationID uuid.UUID `db:"organization_id" json:"organizationId"`
	ApplicationID  uuid.UUID `db:"application_id" json:"applicationId"`
	Name           string    `db:"name" json:"name"`
	Description    string    `db:"description" json:"description"`
	SortOrder      int       `db:"sort_order" json:"sortOrder"`
}

type RunbookRevision struct {
	ID             uuid.UUID     `db:"id" json:"id"`
	CreatedAt      time.Time     `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time     `db:"updated_at" json:"updatedAt"`
	OrganizationID uuid.UUID     `db:"organization_id" json:"organizationId"`
	RunbookID      uuid.UUID     `db:"runbook_id" json:"runbookId"`
	RevisionNumber int           `db:"revision_number" json:"revisionNumber"`
	Description    string        `db:"description" json:"description"`
	Steps          []RunbookStep `db:"-" json:"steps"`
}

type RunbookStep struct {
	ID                    uuid.UUID      `db:"id" json:"id"`
	RunbookRevisionID     uuid.UUID      `db:"runbook_revision_id" json:"runbookRevisionId"`
	Key                   string         `db:"key" json:"key"`
	Name                  string         `db:"name" json:"name"`
	ActionType            string         `db:"action_type" json:"actionType"`
	StepTemplateVersionID *uuid.UUID     `db:"step_template_version_id" json:"stepTemplateVersionId,omitempty"`
	ExecutionLocation     string         `db:"execution_location" json:"executionLocation"`
	InputBindings         map[string]any `db:"input_bindings" json:"inputBindings"`
	Condition             string         `db:"condition" json:"condition"`
	FailureMode           string         `db:"failure_mode" json:"failureMode"`
	TimeoutSeconds        int            `db:"timeout_seconds" json:"timeoutSeconds"`
	RetryMaxAttempts      int            `db:"retry_max_attempts" json:"retryMaxAttempts"`
	RetryIntervalSeconds  int            `db:"retry_interval_seconds" json:"retryIntervalSeconds"`
	RequiredPermissions   []string       `db:"required_permissions" json:"requiredPermissions"`
	SortOrder             int            `db:"sort_order" json:"sortOrder"`
	Dependencies          []string       `db:"-" json:"dependencies"`
}

type RunbookSnapshot struct {
	ID                       uuid.UUID       `db:"id" json:"id"`
	CreatedAt                time.Time       `db:"created_at" json:"createdAt"`
	PublishedAt              time.Time       `db:"published_at" json:"publishedAt"`
	PublishedByUserAccountID *uuid.UUID      `db:"published_by_useraccount_id" json:"publishedByUserAccountId,omitempty"`
	OrganizationID           uuid.UUID       `db:"organization_id" json:"organizationId"`
	ApplicationID            uuid.UUID       `db:"application_id" json:"applicationId"`
	RunbookID                uuid.UUID       `db:"runbook_id" json:"runbookId"`
	RunbookRevisionID        uuid.UUID       `db:"runbook_revision_id" json:"runbookRevisionId"`
	RevisionNumber           int             `db:"revision_number" json:"revisionNumber"`
	CanonicalChecksum        string          `db:"canonical_checksum" json:"canonicalChecksum"`
	CanonicalPayload         []byte          `db:"canonical_payload" json:"-"`
	Revision                 RunbookRevision `db:"-" json:"revision"`
}
