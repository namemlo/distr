package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func ExternalExecutionToAPI(execution types.ExternalExecution) api.ExternalExecution {
	mapped := api.ExternalExecution{
		ID: execution.ID, CreatedAt: execution.CreatedAt, UpdatedAt: execution.UpdatedAt,
		StartedAt: execution.StartedAt, CompletedAt: execution.CompletedAt,
		CallbackDeadlineAt: execution.CallbackDeadlineAt, StepRunID: execution.StepRunID,
		TaskID: execution.TaskID, DeploymentPlanID: execution.DeploymentPlanID,
		DeploymentPlanTargetID: execution.DeploymentPlanTargetID,
		DeploymentTargetID:     execution.DeploymentTargetID, ApplicationID: execution.ApplicationID,
		ReleaseBundleID: execution.ReleaseBundleID, Component: execution.Component,
		PlanChecksum: execution.PlanChecksum, IdempotencyKey: execution.IdempotencyKey,
		ExpectedStateVersion:  execution.ExpectedStateVersion,
		ExpectedStateChecksum: execution.ExpectedStateChecksum,
		ExpectedState: api.ExternalExecutionExpectedState{
			Version: execution.ExpectedVersion, Image: execution.ExpectedImage,
			Platform: execution.ExpectedPlatform, Contracts: execution.ExpectedContracts,
			ConfigReference:  execution.ExpectedConfigReference,
			ConfigChecksum:   execution.ExpectedConfigChecksum,
			ComposeReference: execution.ExpectedComposeReference,
			ComposeChecksum:  execution.ExpectedComposeChecksum,
		},
		Status: execution.Status, ProviderReference: execution.ProviderReference,
		ProviderURL: execution.ProviderURL, TriggerAttempts: execution.TriggerAttempts,
		LastCallbackSequence: execution.LastCallbackSequence, LastMessage: execution.LastMessage,
		ErrorSummary: execution.ErrorSummary, ObservedStateChecksum: execution.ObservedStateChecksum,
		Events: []api.ExternalExecutionEvent{},
	}
	if execution.ActualPlatform != nil && execution.ActualHealth != nil {
		mapped.ObservedState = &api.ExternalExecutionObservedState{
			Version: execution.ActualVersion, Image: execution.ActualImage,
			Platform: *execution.ActualPlatform, Contracts: execution.ActualContracts,
			ConfigReference: execution.ActualConfigReference,
			ConfigChecksum:  execution.ActualConfigChecksum, Health: *execution.ActualHealth,
		}
	}
	return mapped
}

func ExternalExecutionEventToAPI(event types.ExternalExecutionEvent) api.ExternalExecutionEvent {
	return api.ExternalExecutionEvent{
		ID: event.ID, CreatedAt: event.CreatedAt, Sequence: event.Sequence, Status: event.Status,
		ProviderReference: event.ProviderReference, ProviderURL: event.ProviderURL, Message: event.Message,
	}
}
