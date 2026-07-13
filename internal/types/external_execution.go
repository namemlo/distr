package types

import (
	"time"

	"github.com/google/uuid"
)

type ExternalExecutionStatus string

const (
	ExternalExecutionStatusQueued    ExternalExecutionStatus = "QUEUED"
	ExternalExecutionStatusRunning   ExternalExecutionStatus = "RUNNING"
	ExternalExecutionStatusSucceeded ExternalExecutionStatus = "SUCCEEDED"
	ExternalExecutionStatusFailed    ExternalExecutionStatus = "FAILED"
	ExternalExecutionStatusCanceled  ExternalExecutionStatus = "CANCELED"
	ExternalExecutionStatusTimedOut  ExternalExecutionStatus = "TIMED_OUT"
)

func (s ExternalExecutionStatus) IsValid() bool {
	switch s {
	case ExternalExecutionStatusQueued,
		ExternalExecutionStatusRunning,
		ExternalExecutionStatusSucceeded,
		ExternalExecutionStatusFailed,
		ExternalExecutionStatusCanceled,
		ExternalExecutionStatusTimedOut:
		return true
	default:
		return false
	}
}

func (s ExternalExecutionStatus) IsCallbackStatus() bool {
	return s == ExternalExecutionStatusRunning || s == ExternalExecutionStatusSucceeded ||
		s == ExternalExecutionStatusFailed || s == ExternalExecutionStatusCanceled
}

func (s ExternalExecutionStatus) IsTerminal() bool {
	return s == ExternalExecutionStatusSucceeded || s == ExternalExecutionStatusFailed ||
		s == ExternalExecutionStatusCanceled || s == ExternalExecutionStatusTimedOut
}

type ExternalExecution struct {
	ID                      uuid.UUID                 `db:"id" json:"id"`
	CreatedAt               time.Time                 `db:"created_at" json:"createdAt"`
	UpdatedAt               time.Time                 `db:"updated_at" json:"updatedAt"`
	StartedAt               *time.Time                `db:"started_at" json:"startedAt,omitempty"`
	CompletedAt             *time.Time                `db:"completed_at" json:"completedAt,omitempty"`
	CallbackDeadlineAt      time.Time                 `db:"callback_deadline_at" json:"callbackDeadlineAt"`
	OrganizationID          uuid.UUID                 `db:"organization_id" json:"organizationId"`
	StepRunID               uuid.UUID                 `db:"step_run_id" json:"stepRunId"`
	TaskID                  uuid.UUID                 `db:"task_id" json:"taskId"`
	DeploymentPlanID        uuid.UUID                 `db:"deployment_plan_id" json:"deploymentPlanId"`
	DeploymentPlanTargetID  uuid.UUID                 `db:"deployment_plan_target_id" json:"deploymentPlanTargetId"`
	DeploymentTargetID      uuid.UUID                 `db:"deployment_target_id" json:"deploymentTargetId"`
	ApplicationID           uuid.UUID                 `db:"application_id" json:"applicationId"`
	ReleaseBundleID         uuid.UUID                 `db:"release_bundle_id" json:"releaseBundleId"`
	Component               string                    `db:"component" json:"component"`
	PlanChecksum            string                    `db:"plan_checksum" json:"planChecksum"`
	IdempotencyKey          string                    `db:"idempotency_key" json:"idempotencyKey"`
	ExpectedStateVersion    int64                     `db:"expected_state_version" json:"expectedStateVersion"`
	ExpectedStateChecksum   string                    `db:"expected_state_checksum" json:"expectedStateChecksum"`
	ExpectedVersion         string                    `db:"expected_version" json:"expectedVersion"`
	ExpectedImage           string                    `db:"expected_image" json:"expectedImage"`
	ExpectedPlatform        DeploymentTargetPlatform  `db:"expected_platform" json:"expectedPlatform"`
	ExpectedContracts       []string                  `db:"expected_contracts" json:"expectedContracts"`
	ExpectedConfigReference string                    `db:"expected_config_reference" json:"expectedConfigReference"`
	ExpectedConfigChecksum  string                    `db:"expected_config_checksum" json:"expectedConfigChecksum"`
	Status                  ExternalExecutionStatus   `db:"status" json:"status"`
	ProviderReference       string                    `db:"provider_reference" json:"providerReference,omitempty"`
	ProviderURL             string                    `db:"provider_url" json:"providerUrl,omitempty"`
	TriggerAttempts         int                       `db:"trigger_attempts" json:"triggerAttempts"`
	LastCallbackSequence    int64                     `db:"last_callback_sequence" json:"lastCallbackSequence"`
	LastMessage             string                    `db:"last_message" json:"lastMessage,omitempty"`
	ErrorSummary            string                    `db:"error_summary" json:"errorSummary,omitempty"`
	ActualVersion           string                    `db:"actual_version" json:"actualVersion,omitempty"`
	ActualImage             string                    `db:"actual_image" json:"actualImage,omitempty"`
	ActualPlatform          *DeploymentTargetPlatform `db:"actual_platform" json:"actualPlatform,omitempty"`
	ActualContracts         []string                  `db:"actual_contracts" json:"actualContracts"`
	ActualConfigReference   string                    `db:"actual_config_reference" json:"actualConfigReference,omitempty"`
	ActualConfigChecksum    string                    `db:"actual_config_checksum" json:"actualConfigChecksum,omitempty"`
	ActualHealth            *TargetComponentHealth    `db:"actual_health" json:"actualHealth,omitempty"`
	ObservedStateChecksum   string                    `db:"observed_state_checksum" json:"observedStateChecksum,omitempty"`
}

type ExternalExecutionExpectedState struct {
	Version         string
	Image           string
	Platform        DeploymentTargetPlatform
	Contracts       []string
	ConfigReference string
	ConfigChecksum  string
}

type ExternalExecutionObservedState struct {
	Version         string                   `json:"version"`
	Image           string                   `json:"image"`
	Platform        DeploymentTargetPlatform `json:"platform"`
	Contracts       []string                 `json:"contracts"`
	ConfigReference string                   `json:"configReference"`
	ConfigChecksum  string                   `json:"configChecksum"`
	Health          TargetComponentHealth    `json:"health"`
}

type ExternalExecutionEvent struct {
	ID                  uuid.UUID               `db:"id" json:"id"`
	CreatedAt           time.Time               `db:"created_at" json:"createdAt"`
	OrganizationID      uuid.UUID               `db:"organization_id" json:"organizationId"`
	ExternalExecutionID uuid.UUID               `db:"external_execution_id" json:"externalExecutionId"`
	Sequence            int64                   `db:"sequence" json:"sequence"`
	Status              ExternalExecutionStatus `db:"status" json:"status"`
	ProviderReference   string                  `db:"provider_reference" json:"providerReference,omitempty"`
	ProviderURL         string                  `db:"provider_url" json:"providerUrl,omitempty"`
	Message             string                  `db:"message" json:"message,omitempty"`
	PayloadHash         string                  `db:"payload_hash" json:"payloadHash"`
}

type PrepareExternalExecutionRequest struct {
	OrganizationID         uuid.UUID
	StepRunID              uuid.UUID
	Component              string
	CallbackTimeoutSeconds int
}

type MarkExternalExecutionTriggeredRequest struct {
	OrganizationID      uuid.UUID
	ExternalExecutionID uuid.UUID
	TriggerAttempts     int
}

type RecordExternalExecutionCallbackRequest struct {
	OrganizationID      uuid.UUID
	ExternalExecutionID uuid.UUID
	Sequence            int64
	Status              ExternalExecutionStatus
	ProviderReference   string
	ProviderURL         string
	Message             string
	ObservedState       *ExternalExecutionObservedState
}

type TimeoutExternalExecutionRequest struct {
	OrganizationID      uuid.UUID
	ExternalExecutionID uuid.UUID
	Message             string
}

type FailExternalExecutionRequest struct {
	OrganizationID      uuid.UUID
	ExternalExecutionID uuid.UUID
	Message             string
}
