package types

import (
	"time"

	"github.com/google/uuid"
)

type TaskStatus string

const (
	TaskStatusQueued    TaskStatus = "QUEUED"
	TaskStatusRunning   TaskStatus = "RUNNING"
	TaskStatusSucceeded TaskStatus = "SUCCEEDED"
	TaskStatusFailed    TaskStatus = "FAILED"
	TaskStatusCanceled  TaskStatus = "CANCELED"
)

func (s TaskStatus) IsValid() bool {
	switch s {
	case TaskStatusQueued, TaskStatusRunning, TaskStatusSucceeded, TaskStatusFailed, TaskStatusCanceled:
		return true
	default:
		return false
	}
}

func (s TaskStatus) IsTerminal() bool {
	return s == TaskStatusSucceeded || s == TaskStatusFailed || s == TaskStatusCanceled
}

type TaskConcurrencyPolicy string

const (
	TaskConcurrencyPolicyQueue         TaskConcurrencyPolicy = "QUEUE"
	TaskConcurrencyPolicyCancelOlder   TaskConcurrencyPolicy = "CANCEL_OLDER"
	TaskConcurrencyPolicyRejectNew     TaskConcurrencyPolicy = "REJECT_NEW"
	TaskConcurrencyPolicyAllowParallel TaskConcurrencyPolicy = "ALLOW_PARALLEL"
)

func (p TaskConcurrencyPolicy) IsValid() bool {
	switch p {
	case TaskConcurrencyPolicyQueue,
		TaskConcurrencyPolicyCancelOlder,
		TaskConcurrencyPolicyRejectNew,
		TaskConcurrencyPolicyAllowParallel:
		return true
	default:
		return false
	}
}

type TaskLockResourceType string

const (
	TaskLockResourceDeploymentTarget       TaskLockResourceType = "deployment_target"
	TaskLockResourceTenantEnvironment      TaskLockResourceType = "tenant_environment"
	TaskLockResourceApplicationEnvironment TaskLockResourceType = "application_environment"
	TaskLockResourceCustom                 TaskLockResourceType = "custom"
)

func (t TaskLockResourceType) IsValid() bool {
	switch t {
	case TaskLockResourceDeploymentTarget,
		TaskLockResourceTenantEnvironment,
		TaskLockResourceApplicationEnvironment,
		TaskLockResourceCustom:
		return true
	default:
		return false
	}
}

type StepRunStatus string

const (
	StepRunStatusPending   StepRunStatus = "PENDING"
	StepRunStatusRunning   StepRunStatus = "RUNNING"
	StepRunStatusSucceeded StepRunStatus = "SUCCEEDED"
	StepRunStatusFailed    StepRunStatus = "FAILED"
	StepRunStatusSkipped   StepRunStatus = "SKIPPED"
)

func (s StepRunStatus) IsValid() bool {
	switch s {
	case StepRunStatusPending,
		StepRunStatusRunning,
		StepRunStatusSucceeded,
		StepRunStatusFailed,
		StepRunStatusSkipped:
		return true
	default:
		return false
	}
}

func (s StepRunStatus) IsTerminal() bool {
	return s == StepRunStatusSucceeded || s == StepRunStatusFailed || s == StepRunStatusSkipped
}

type CreateTasksForDeploymentPlanRequest struct {
	OrganizationID      uuid.UUID
	DeploymentPlanID    uuid.UUID
	ActorUserAccountID  uuid.UUID
	ConcurrencyPolicy   TaskConcurrencyPolicy
	AdditionalResources []TaskLockResourceRequest
}

type TaskLockResourceRequest struct {
	ResourceType      TaskLockResourceType
	ResourceKey       string
	ConcurrencyPolicy TaskConcurrencyPolicy
}

type TransitionTaskStateRequest struct {
	OrganizationID uuid.UUID
	TaskID         uuid.UUID
	Status         TaskStatus
}

type TransitionStepRunStateRequest struct {
	OrganizationID uuid.UUID
	StepRunID      uuid.UUID
	Status         StepRunStatus
}

type Task struct {
	ID                     uuid.UUID          `db:"id" json:"id"`
	CreatedAt              time.Time          `db:"created_at" json:"createdAt"`
	UpdatedAt              time.Time          `db:"updated_at" json:"updatedAt"`
	QueuedAt               time.Time          `db:"queued_at" json:"queuedAt"`
	StartedAt              *time.Time         `db:"started_at" json:"startedAt,omitempty"`
	CompletedAt            *time.Time         `db:"completed_at" json:"completedAt,omitempty"`
	OrganizationID         uuid.UUID          `db:"organization_id" json:"organizationId"`
	DeploymentPlanID       uuid.UUID          `db:"deployment_plan_id" json:"deploymentPlanId"`
	DeploymentPlanTargetID uuid.UUID          `db:"deployment_plan_target_id" json:"deploymentPlanTargetId"`
	DeploymentTargetID     uuid.UUID          `db:"deployment_target_id" json:"deploymentTargetId"`
	ApplicationID          uuid.UUID          `db:"application_id" json:"applicationId"`
	ReleaseBundleID        uuid.UUID          `db:"release_bundle_id" json:"releaseBundleId"`
	ChannelID              uuid.UUID          `db:"channel_id" json:"channelId"`
	EnvironmentID          uuid.UUID          `db:"environment_id" json:"environmentId"`
	Status                 TaskStatus         `db:"status" json:"status"`
	QueueOrder             int64              `db:"queue_order" json:"queueOrder"`
	Locks                  []TaskResourceLock `db:"-" json:"locks"`
	StepRuns               []StepRun          `db:"-" json:"stepRuns"`
}

type TaskResourceLock struct {
	ID                uuid.UUID             `db:"id" json:"id"`
	CreatedAt         time.Time             `db:"created_at" json:"createdAt"`
	UpdatedAt         time.Time             `db:"updated_at" json:"updatedAt"`
	AcquiredAt        *time.Time            `db:"acquired_at" json:"acquiredAt,omitempty"`
	ReleasedAt        *time.Time            `db:"released_at" json:"releasedAt,omitempty"`
	OrganizationID    uuid.UUID             `db:"organization_id" json:"organizationId"`
	TaskID            uuid.UUID             `db:"task_id" json:"taskId"`
	ResourceType      TaskLockResourceType  `db:"resource_type" json:"resourceType"`
	ResourceKey       string                `db:"resource_key" json:"resourceKey"`
	ConcurrencyPolicy TaskConcurrencyPolicy `db:"concurrency_policy" json:"concurrencyPolicy"`
}

type StepRun struct {
	ID                   uuid.UUID     `db:"id" json:"id"`
	CreatedAt            time.Time     `db:"created_at" json:"createdAt"`
	UpdatedAt            time.Time     `db:"updated_at" json:"updatedAt"`
	StartedAt            *time.Time    `db:"started_at" json:"startedAt,omitempty"`
	CompletedAt          *time.Time    `db:"completed_at" json:"completedAt,omitempty"`
	OrganizationID       uuid.UUID     `db:"organization_id" json:"organizationId"`
	TaskID               uuid.UUID     `db:"task_id" json:"taskId"`
	DeploymentPlanID     uuid.UUID     `db:"deployment_plan_id" json:"deploymentPlanId"`
	DeploymentPlanStepID uuid.UUID     `db:"deployment_plan_step_id" json:"deploymentPlanStepId"`
	StepKey              string        `db:"step_key" json:"stepKey"`
	Name                 string        `db:"name" json:"name"`
	ActionType           string        `db:"action_type" json:"actionType"`
	Status               StepRunStatus `db:"status" json:"status"`
	SortOrder            int           `db:"sort_order" json:"sortOrder"`
	SkippedReason        string        `db:"skipped_reason" json:"skippedReason,omitempty"`
}
