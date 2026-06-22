package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func RunbookToAPI(runbook types.Runbook) api.Runbook {
	return api.Runbook{
		ID:            runbook.ID,
		CreatedAt:     runbook.CreatedAt,
		UpdatedAt:     runbook.UpdatedAt,
		ApplicationID: runbook.ApplicationID,
		Name:          runbook.Name,
		Description:   runbook.Description,
		SortOrder:     runbook.SortOrder,
	}
}

func RunbookRevisionToAPI(revision types.RunbookRevision) api.RunbookRevision {
	return api.RunbookRevision{
		ID:             revision.ID,
		CreatedAt:      revision.CreatedAt,
		UpdatedAt:      revision.UpdatedAt,
		RunbookID:      revision.RunbookID,
		RevisionNumber: revision.RevisionNumber,
		Description:    revision.Description,
		Steps:          List(revision.Steps, RunbookStepToAPI),
	}
}

func RunbookStepToAPI(step types.RunbookStep) api.RunbookStep {
	return api.RunbookStep{
		ID:                    step.ID,
		RunbookRevisionID:     step.RunbookRevisionID,
		Key:                   step.Key,
		Name:                  step.Name,
		ActionType:            step.ActionType,
		StepTemplateVersionID: step.StepTemplateVersionID,
		ExecutionLocation:     step.ExecutionLocation,
		InputBindings:         step.InputBindings,
		Condition:             step.Condition,
		FailureMode:           step.FailureMode,
		TimeoutSeconds:        step.TimeoutSeconds,
		RetryPolicy: api.RunbookStepRetryPolicy{
			MaxAttempts:     step.RetryMaxAttempts,
			IntervalSeconds: step.RetryIntervalSeconds,
		},
		RequiredPermissions: step.RequiredPermissions,
		SortOrder:           step.SortOrder,
		Dependencies:        step.Dependencies,
	}
}

func RunbookSnapshotToAPI(snapshot types.RunbookSnapshot) api.RunbookSnapshot {
	return api.RunbookSnapshot{
		ID:                       snapshot.ID,
		CreatedAt:                snapshot.CreatedAt,
		PublishedAt:              snapshot.PublishedAt,
		PublishedByUserAccountID: snapshot.PublishedByUserAccountID,
		ApplicationID:            snapshot.ApplicationID,
		RunbookID:                snapshot.RunbookID,
		RunbookRevisionID:        snapshot.RunbookRevisionID,
		RevisionNumber:           snapshot.RevisionNumber,
		CanonicalChecksum:        snapshot.CanonicalChecksum,
		Revision:                 RunbookRevisionToAPI(snapshot.Revision),
	}
}
