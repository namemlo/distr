package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type CreateTasksForDeploymentPlanRequest struct {
	ConcurrencyPolicy types.TaskConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`
	LockResources     []TaskLockResourceRequest   `json:"lockResources,omitempty"`
}

type TaskLockResourceRequest struct {
	ResourceType      types.TaskLockResourceType  `json:"resourceType"`
	ResourceKey       string                      `json:"resourceKey"`
	ConcurrencyPolicy types.TaskConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`
}

func (r CreateTasksForDeploymentPlanRequest) Validate() error {
	if r.ConcurrencyPolicy != "" && !r.ConcurrencyPolicy.IsValid() {
		return validation.NewValidationFailedError("concurrencyPolicy is invalid")
	}
	seen := map[string]struct{}{}
	for i, resource := range r.LockResources {
		if !resource.ResourceType.IsValid() {
			return validation.NewValidationFailedError(fmt.Sprintf("lockResources[%d].resourceType is invalid", i))
		}
		key := strings.TrimSpace(resource.ResourceKey)
		if key == "" {
			return validation.NewValidationFailedError(fmt.Sprintf("lockResources[%d].resourceKey is required", i))
		}
		if resource.ConcurrencyPolicy != "" && !resource.ConcurrencyPolicy.IsValid() {
			return validation.NewValidationFailedError(fmt.Sprintf("lockResources[%d].concurrencyPolicy is invalid", i))
		}
		duplicateKey := string(resource.ResourceType) + "\x00" + key
		if _, ok := seen[duplicateKey]; ok {
			return validation.NewValidationFailedError("lockResources contains duplicate resource")
		}
		seen[duplicateKey] = struct{}{}
	}
	return nil
}

type TransitionTaskStateRequest struct {
	Status types.TaskStatus `json:"status"`
}

func (r TransitionTaskStateRequest) Validate() error {
	if r.Status == "" {
		return validation.NewValidationFailedError("status is required")
	}
	if !r.Status.IsValid() {
		return validation.NewValidationFailedError("status is invalid")
	}
	return nil
}

type Task struct {
	ID                     uuid.UUID          `json:"id"`
	CreatedAt              time.Time          `json:"createdAt"`
	UpdatedAt              time.Time          `json:"updatedAt"`
	QueuedAt               time.Time          `json:"queuedAt"`
	StartedAt              *time.Time         `json:"startedAt,omitempty"`
	CompletedAt            *time.Time         `json:"completedAt,omitempty"`
	TaskType               types.TaskType     `json:"taskType"`
	DeploymentPlanID       uuid.UUID          `json:"deploymentPlanId"`
	DeploymentPlanTargetID uuid.UUID          `json:"deploymentPlanTargetId"`
	DeploymentTargetID     uuid.UUID          `json:"deploymentTargetId"`
	ApplicationID          uuid.UUID          `json:"applicationId"`
	ReleaseBundleID        uuid.UUID          `json:"releaseBundleId"`
	ChannelID              uuid.UUID          `json:"channelId"`
	EnvironmentID          uuid.UUID          `json:"environmentId"`
	ActorUserAccountID     *uuid.UUID         `json:"actorUserAccountId,omitempty"`
	Status                 types.TaskStatus   `json:"status"`
	QueueOrder             int64              `json:"queueOrder"`
	Locks                  []TaskResourceLock `json:"locks"`
	StepRuns               []StepRun          `json:"stepRuns"`
}

type TaskResourceLock struct {
	ID                uuid.UUID                   `json:"id"`
	CreatedAt         time.Time                   `json:"createdAt"`
	UpdatedAt         time.Time                   `json:"updatedAt"`
	AcquiredAt        *time.Time                  `json:"acquiredAt,omitempty"`
	ReleasedAt        *time.Time                  `json:"releasedAt,omitempty"`
	TaskID            uuid.UUID                   `json:"taskId"`
	ResourceType      types.TaskLockResourceType  `json:"resourceType"`
	ResourceKey       string                      `json:"resourceKey"`
	ConcurrencyPolicy types.TaskConcurrencyPolicy `json:"concurrencyPolicy"`
}

type StepRun struct {
	ID                   uuid.UUID           `json:"id"`
	CreatedAt            time.Time           `json:"createdAt"`
	UpdatedAt            time.Time           `json:"updatedAt"`
	StartedAt            *time.Time          `json:"startedAt,omitempty"`
	CompletedAt          *time.Time          `json:"completedAt,omitempty"`
	TaskID               uuid.UUID           `json:"taskId"`
	DeploymentPlanID     uuid.UUID           `json:"deploymentPlanId"`
	DeploymentPlanStepID uuid.UUID           `json:"deploymentPlanStepId"`
	StepKey              string              `json:"stepKey"`
	Name                 string              `json:"name"`
	ActionType           string              `json:"actionType"`
	Status               types.StepRunStatus `json:"status"`
	SortOrder            int                 `json:"sortOrder"`
	SkippedReason        string              `json:"skippedReason,omitempty"`
}
