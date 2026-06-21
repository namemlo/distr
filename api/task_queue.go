package api

import (
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

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
	ID                     uuid.UUID        `json:"id"`
	CreatedAt              time.Time        `json:"createdAt"`
	UpdatedAt              time.Time        `json:"updatedAt"`
	QueuedAt               time.Time        `json:"queuedAt"`
	StartedAt              *time.Time       `json:"startedAt,omitempty"`
	CompletedAt            *time.Time       `json:"completedAt,omitempty"`
	DeploymentPlanID       uuid.UUID        `json:"deploymentPlanId"`
	DeploymentPlanTargetID uuid.UUID        `json:"deploymentPlanTargetId"`
	DeploymentTargetID     uuid.UUID        `json:"deploymentTargetId"`
	ApplicationID          uuid.UUID        `json:"applicationId"`
	ReleaseBundleID        uuid.UUID        `json:"releaseBundleId"`
	ChannelID              uuid.UUID        `json:"channelId"`
	EnvironmentID          uuid.UUID        `json:"environmentId"`
	Status                 types.TaskStatus `json:"status"`
	QueueOrder             int64            `json:"queueOrder"`
	StepRuns               []StepRun        `json:"stepRuns"`
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
