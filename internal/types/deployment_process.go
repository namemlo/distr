package types

import (
	"time"

	"github.com/google/uuid"
)

type DeploymentProcess struct {
	ID             uuid.UUID `db:"id" json:"id"`
	CreatedAt      time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time `db:"updated_at" json:"updatedAt"`
	OrganizationID uuid.UUID `db:"organization_id" json:"organizationId"`
	ApplicationID  uuid.UUID `db:"application_id" json:"applicationId"`
	Name           string    `db:"name" json:"name"`
	Description    string    `db:"description" json:"description"`
	SortOrder      int       `db:"sort_order" json:"sortOrder"`
}

type DeploymentProcessRevision struct {
	ID                  uuid.UUID               `db:"id" json:"id"`
	CreatedAt           time.Time               `db:"created_at" json:"createdAt"`
	UpdatedAt           time.Time               `db:"updated_at" json:"updatedAt"`
	OrganizationID      uuid.UUID               `db:"organization_id" json:"organizationId"`
	DeploymentProcessID uuid.UUID               `db:"deployment_process_id" json:"deploymentProcessId"`
	RevisionNumber      int                     `db:"revision_number" json:"revisionNumber"`
	Description         string                  `db:"description" json:"description"`
	Steps               []DeploymentProcessStep `db:"-" json:"steps"`
}

type DeploymentProcessStep struct {
	ID                          uuid.UUID      `db:"id" json:"id"`
	DeploymentProcessRevisionID uuid.UUID      `db:"deployment_process_revision_id" json:"deploymentProcessRevisionId"`
	Key                         string         `db:"key" json:"key"`
	Name                        string         `db:"name" json:"name"`
	ActionType                  string         `db:"action_type" json:"actionType"`
	StepTemplateVersionID       *uuid.UUID     `db:"step_template_version_id" json:"stepTemplateVersionId,omitempty"`
	ExecutionLocation           string         `db:"execution_location" json:"executionLocation"`
	InputBindings               map[string]any `db:"input_bindings" json:"inputBindings"`
	Condition                   string         `db:"condition" json:"condition"`
	ChannelIDs                  []uuid.UUID    `db:"-" json:"channelIds"`
	EnvironmentIDs              []uuid.UUID    `db:"-" json:"environmentIds"`
	TargetTags                  []string       `db:"target_tags" json:"targetTags"`
	FailureMode                 string         `db:"failure_mode" json:"failureMode"`
	TimeoutSeconds              int            `db:"timeout_seconds" json:"timeoutSeconds"`
	RetryMaxAttempts            int            `db:"retry_max_attempts" json:"retryMaxAttempts"`
	RetryIntervalSeconds        int            `db:"retry_interval_seconds" json:"retryIntervalSeconds"`
	RequiredPermissions         []string       `db:"required_permissions" json:"requiredPermissions"`
	SortOrder                   int            `db:"sort_order" json:"sortOrder"`
	Dependencies                []string       `db:"-" json:"dependencies"`
}
