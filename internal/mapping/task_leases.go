package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func TaskLeaseToAPI(lease types.TaskLease) api.AgentTaskLease {
	return api.AgentTaskLease{
		ID:             lease.ID,
		CreatedAt:      lease.CreatedAt,
		UpdatedAt:      lease.UpdatedAt,
		OrganizationID: lease.OrganizationID,
		TaskID:         lease.TaskID,
		AgentID:        lease.AgentID,
		PlanChecksum:   lease.PlanChecksum,
		LeaseToken:     lease.LeaseToken,
		LeasedAt:       lease.LeasedAt,
		ExpiresAt:      lease.ExpiresAt,
		HeartbeatAt:    lease.HeartbeatAt,
		Attempt:        lease.Attempt,
		Steps:          List(lease.Steps, TaskLeaseStepToAPI),
	}
}

func TaskLeaseStepToAPI(step types.TaskLeaseStep) api.AgentTaskLeaseStep {
	return api.AgentTaskLeaseStep{
		StepRunID:        step.StepRunID,
		Key:              step.StepKey,
		ActionType:       step.ActionType,
		ActionVersion:    step.ActionVersion,
		Inputs:           step.InputBindings,
		SecretReferences: step.SecretReferences,
		IdempotencyKey:   step.IdempotencyKey,
	}
}
