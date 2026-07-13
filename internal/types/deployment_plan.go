package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type DeploymentPlanStatus string

const (
	DeploymentPlanStatusDraft      DeploymentPlanStatus = "DRAFT"
	DeploymentPlanStatusValidating DeploymentPlanStatus = "VALIDATING"
	DeploymentPlanStatusBlocked    DeploymentPlanStatus = "BLOCKED"
	DeploymentPlanStatusReady      DeploymentPlanStatus = "READY"
	DeploymentPlanStatusExpired    DeploymentPlanStatus = "EXPIRED"
	DeploymentPlanStatusExecuted   DeploymentPlanStatus = "EXECUTED"
)

type DeploymentPlanIssueSeverity string

const (
	DeploymentPlanIssueSeverityBlocker DeploymentPlanIssueSeverity = "blocker"
	DeploymentPlanIssueSeverityWarning DeploymentPlanIssueSeverity = "warning"
)

type CreateDeploymentPlanRequest struct {
	OrganizationID  uuid.UUID
	ReleaseBundleID uuid.UUID
	EnvironmentID   uuid.UUID
	TargetIDs       []uuid.UUID
}

type DeploymentPlan struct {
	ID                 uuid.UUID                       `db:"id" json:"id"`
	CreatedAt          time.Time                       `db:"created_at" json:"createdAt"`
	OrganizationID     uuid.UUID                       `db:"organization_id" json:"organizationId"`
	ApplicationID      uuid.UUID                       `db:"application_id" json:"applicationId"`
	ReleaseBundleID    uuid.UUID                       `db:"release_bundle_id" json:"releaseBundleId"`
	ChannelID          uuid.UUID                       `db:"channel_id" json:"channelId"`
	EnvironmentID      uuid.UUID                       `db:"environment_id" json:"environmentId"`
	ProcessSnapshotID  *uuid.UUID                      `db:"process_snapshot_id" json:"processSnapshotId,omitempty"`
	VariableSnapshotID *uuid.UUID                      `db:"variable_snapshot_id" json:"variableSnapshotId,omitempty"`
	ReleaseContract    *ReleaseContract                `db:"release_contract" json:"releaseContract,omitempty"`
	Status             DeploymentPlanStatus            `db:"status" json:"status"`
	CanonicalChecksum  string                          `db:"canonical_checksum" json:"canonicalChecksum"`
	CanonicalPayload   []byte                          `db:"canonical_payload" json:"-"`
	Targets            []DeploymentPlanTarget          `db:"-" json:"targets"`
	TargetComponents   []DeploymentPlanTargetComponent `db:"-" json:"targetComponents"`
	Steps              []DeploymentPlanStep            `db:"-" json:"steps"`
	Variables          []DeploymentPlanVariable        `db:"-" json:"variables"`
	Issues             []DeploymentPlanIssue           `db:"-" json:"issues"`
}

type DeploymentPlanTarget struct {
	ID                     uuid.UUID                `db:"id" json:"id"`
	DeploymentPlanID       uuid.UUID                `db:"deployment_plan_id" json:"deploymentPlanId"`
	OrganizationID         uuid.UUID                `db:"organization_id" json:"organizationId"`
	DeploymentTargetID     uuid.UUID                `db:"deployment_target_id" json:"deploymentTargetId"`
	Name                   string                   `db:"name" json:"name"`
	Type                   DeploymentType           `db:"type" json:"type"`
	Platform               DeploymentTargetPlatform `db:"platform" json:"platform"`
	CustomerOrganizationID *uuid.UUID               `db:"customer_organization_id" json:"customerOrganizationId,omitempty"`
	SortOrder              int                      `db:"sort_order" json:"sortOrder"`
}

type DeploymentPlanTargetComponent struct {
	ID                      uuid.UUID                `db:"id" json:"id"`
	DeploymentPlanID        uuid.UUID                `db:"deployment_plan_id" json:"deploymentPlanId"`
	DeploymentPlanTargetID  uuid.UUID                `db:"deployment_plan_target_id" json:"deploymentPlanTargetId"`
	OrganizationID          uuid.UUID                `db:"organization_id" json:"organizationId"`
	DeploymentTargetID      uuid.UUID                `db:"deployment_target_id" json:"deploymentTargetId"`
	Component               string                   `db:"component" json:"component"`
	Version                 string                   `db:"version" json:"version"`
	Image                   string                   `db:"image" json:"image"`
	Platform                DeploymentTargetPlatform `db:"platform" json:"platform"`
	Contracts               []string                 `db:"contracts" json:"contracts"`
	ConfigChecksum          string                   `db:"config_checksum" json:"configChecksum"`
	ExpectedStateVersion    int64                    `db:"expected_state_version" json:"expectedStateVersion"`
	ExpectedStateChecksum   string                   `db:"expected_state_checksum" json:"expectedStateChecksum"`
	ExpectedReleaseBundleID *uuid.UUID               `db:"expected_release_bundle_id" json:"expectedReleaseBundleId,omitempty"`
	SortOrder               int                      `db:"sort_order" json:"sortOrder"`
}

type DeploymentPlanStep struct {
	ID                   uuid.UUID      `db:"id" json:"id"`
	DeploymentPlanID     uuid.UUID      `db:"deployment_plan_id" json:"deploymentPlanId"`
	OrganizationID       uuid.UUID      `db:"organization_id" json:"organizationId"`
	StepKey              string         `db:"step_key" json:"stepKey"`
	Name                 string         `db:"name" json:"name"`
	ActionType           string         `db:"action_type" json:"actionType"`
	ActionName           string         `db:"action_name" json:"actionName"`
	ExecutionLocation    string         `db:"execution_location" json:"executionLocation"`
	InputBindings        map[string]any `db:"input_bindings" json:"inputBindings"`
	Condition            string         `db:"condition" json:"condition"`
	TargetTags           []string       `db:"target_tags" json:"targetTags"`
	FailureMode          string         `db:"failure_mode" json:"failureMode"`
	TimeoutSeconds       int            `db:"timeout_seconds" json:"timeoutSeconds"`
	RetryMaxAttempts     int            `db:"retry_max_attempts" json:"retryMaxAttempts"`
	RetryIntervalSeconds int            `db:"retry_interval_seconds" json:"retryIntervalSeconds"`
	RequiredPermissions  []string       `db:"required_permissions" json:"requiredPermissions"`
	SortOrder            int            `db:"sort_order" json:"sortOrder"`
	Dependencies         []string       `db:"dependencies" json:"dependencies"`
	Included             bool           `db:"included" json:"included"`
	ExcludedReason       string         `db:"excluded_reason" json:"excludedReason,omitempty"`
}

type DeploymentPlanVariable struct {
	ID               uuid.UUID                      `db:"id" json:"id"`
	DeploymentPlanID uuid.UUID                      `db:"deployment_plan_id" json:"deploymentPlanId"`
	OrganizationID   uuid.UUID                      `db:"organization_id" json:"organizationId"`
	VariableSetID    uuid.UUID                      `db:"variable_set_id" json:"variableSetId"`
	VariableID       uuid.UUID                      `db:"variable_id" json:"variableId"`
	Key              string                         `db:"key" json:"key"`
	Type             VariableType                   `db:"type" json:"type"`
	IsRequired       bool                           `db:"is_required" json:"isRequired"`
	Status           VariableResolutionStatus       `db:"status" json:"status"`
	Source           VariableResolutionSource       `db:"source" json:"source"`
	Value            json.RawMessage                `db:"value" json:"value,omitempty"`
	ReferenceID      string                         `db:"reference_id" json:"referenceId,omitempty"`
	ReferenceName    string                         `db:"reference_name" json:"referenceName,omitempty"`
	Redacted         bool                           `db:"redacted" json:"redacted"`
	Trace            []VariableResolutionTraceEntry `db:"-" json:"trace"`
}

type DeploymentPlanIssue struct {
	ID               uuid.UUID                   `db:"id" json:"id"`
	DeploymentPlanID uuid.UUID                   `db:"deployment_plan_id" json:"deploymentPlanId"`
	OrganizationID   uuid.UUID                   `db:"organization_id" json:"organizationId"`
	Severity         DeploymentPlanIssueSeverity `db:"severity" json:"severity"`
	Code             string                      `db:"code" json:"code"`
	Field            string                      `db:"field" json:"field"`
	Message          string                      `db:"message" json:"message"`
	SortOrder        int                         `db:"sort_order" json:"sortOrder"`
}
