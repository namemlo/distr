package api

import (
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type HeartbeatAgentTaskLeaseRequest struct {
	LeaseToken string `json:"leaseToken"`
}

func (r *HeartbeatAgentTaskLeaseRequest) Validate() error {
	r.LeaseToken = strings.TrimSpace(r.LeaseToken)
	if r.LeaseToken == "" {
		return validation.NewValidationFailedError("leaseToken is required")
	}
	return nil
}

type AgentTaskLease struct {
	ID           uuid.UUID            `json:"id"`
	CreatedAt    time.Time            `json:"createdAt"`
	UpdatedAt    time.Time            `json:"updatedAt"`
	TaskID       uuid.UUID            `json:"taskId"`
	PlanChecksum string               `json:"planChecksum"`
	LeaseToken   string               `json:"leaseToken"`
	LeasedAt     time.Time            `json:"leasedAt"`
	ExpiresAt    time.Time            `json:"expiresAt"`
	HeartbeatAt  time.Time            `json:"heartbeatAt"`
	Attempt      int                  `json:"attempt"`
	Steps        []AgentTaskLeaseStep `json:"steps"`
}

type AgentTaskLeaseStep struct {
	StepRunID        uuid.UUID      `json:"stepRunId"`
	Key              string         `json:"key"`
	ActionType       string         `json:"actionType"`
	ActionVersion    string         `json:"actionVersion"`
	Inputs           map[string]any `json:"inputs"`
	SecretReferences []string       `json:"secretReferences"`
	IdempotencyKey   string         `json:"idempotencyKey"`
}
