package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func TaskToAPI(task types.Task) api.Task {
	return api.Task{
		ID:                     task.ID,
		CreatedAt:              task.CreatedAt,
		UpdatedAt:              task.UpdatedAt,
		QueuedAt:               task.QueuedAt,
		StartedAt:              task.StartedAt,
		CompletedAt:            task.CompletedAt,
		DeploymentPlanID:       task.DeploymentPlanID,
		DeploymentPlanTargetID: task.DeploymentPlanTargetID,
		DeploymentTargetID:     task.DeploymentTargetID,
		ApplicationID:          task.ApplicationID,
		ReleaseBundleID:        task.ReleaseBundleID,
		ChannelID:              task.ChannelID,
		EnvironmentID:          task.EnvironmentID,
		Status:                 task.Status,
		QueueOrder:             task.QueueOrder,
		StepRuns:               List(task.StepRuns, StepRunToAPI),
	}
}

func StepRunToAPI(stepRun types.StepRun) api.StepRun {
	return api.StepRun{
		ID:                   stepRun.ID,
		CreatedAt:            stepRun.CreatedAt,
		UpdatedAt:            stepRun.UpdatedAt,
		StartedAt:            stepRun.StartedAt,
		CompletedAt:          stepRun.CompletedAt,
		TaskID:               stepRun.TaskID,
		DeploymentPlanID:     stepRun.DeploymentPlanID,
		DeploymentPlanStepID: stepRun.DeploymentPlanStepID,
		StepKey:              stepRun.StepKey,
		Name:                 stepRun.Name,
		ActionType:           stepRun.ActionType,
		Status:               stepRun.Status,
		SortOrder:            stepRun.SortOrder,
		SkippedReason:        stepRun.SkippedReason,
	}
}
