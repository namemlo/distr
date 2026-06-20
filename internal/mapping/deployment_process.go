package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func DeploymentProcessToAPI(process types.DeploymentProcess) api.DeploymentProcess {
	return api.DeploymentProcess{
		ID:            process.ID,
		CreatedAt:     process.CreatedAt,
		UpdatedAt:     process.UpdatedAt,
		ApplicationID: process.ApplicationID,
		Name:          process.Name,
		Description:   process.Description,
		SortOrder:     process.SortOrder,
	}
}

func DeploymentProcessRevisionToAPI(revision types.DeploymentProcessRevision) api.DeploymentProcessRevision {
	return api.DeploymentProcessRevision{
		ID:                  revision.ID,
		CreatedAt:           revision.CreatedAt,
		UpdatedAt:           revision.UpdatedAt,
		DeploymentProcessID: revision.DeploymentProcessID,
		RevisionNumber:      revision.RevisionNumber,
		Description:         revision.Description,
		Steps:               List(revision.Steps, DeploymentProcessStepToAPI),
	}
}

func DeploymentProcessStepToAPI(step types.DeploymentProcessStep) api.DeploymentProcessStep {
	return api.DeploymentProcessStep{
		ID:                          step.ID,
		DeploymentProcessRevisionID: step.DeploymentProcessRevisionID,
		Key:                         step.Key,
		Name:                        step.Name,
		ActionType:                  step.ActionType,
		StepTemplateVersionID:       step.StepTemplateVersionID,
		ExecutionLocation:           step.ExecutionLocation,
		InputBindings:               step.InputBindings,
		Condition:                   step.Condition,
		ChannelIDs:                  step.ChannelIDs,
		EnvironmentIDs:              step.EnvironmentIDs,
		TargetTags:                  step.TargetTags,
		FailureMode:                 step.FailureMode,
		TimeoutSeconds:              step.TimeoutSeconds,
		RetryPolicy: api.DeploymentProcessStepRetryPolicy{
			MaxAttempts:     step.RetryMaxAttempts,
			IntervalSeconds: step.RetryIntervalSeconds,
		},
		RequiredPermissions: step.RequiredPermissions,
		SortOrder:           step.SortOrder,
		Dependencies:        step.Dependencies,
	}
}
