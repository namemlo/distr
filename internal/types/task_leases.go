package types

import (
	"time"

	"github.com/google/uuid"
)

type TaskExecutorType string

const (
	TaskExecutorTypeAgent TaskExecutorType = "AGENT"
	TaskExecutorTypeHub   TaskExecutorType = "HUB"
)

func (t TaskExecutorType) IsValid() bool {
	return t == TaskExecutorTypeAgent || t == TaskExecutorTypeHub
}

func (t TaskExecutorType) ExecutionLocation() string {
	if t == TaskExecutorTypeHub {
		return "hub"
	}
	return "target"
}

type LeaseAgentTaskRequest struct {
	OrganizationID uuid.UUID
	AgentID        uuid.UUID
}

type HeartbeatAgentTaskLeaseRequest struct {
	OrganizationID uuid.UUID
	AgentID        uuid.UUID
	TaskID         uuid.UUID
	LeaseToken     string
}

type HeartbeatHubTaskLeaseRequest struct {
	OrganizationID     uuid.UUID
	DeploymentTargetID uuid.UUID
	TaskID             uuid.UUID
	LeaseToken         string
}

type TaskLease struct {
	ID             uuid.UUID        `db:"id" json:"id"`
	CreatedAt      time.Time        `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time        `db:"updated_at" json:"updatedAt"`
	OrganizationID uuid.UUID        `db:"organization_id" json:"organizationId"`
	TaskID         uuid.UUID        `db:"task_id" json:"taskId"`
	AgentID        uuid.UUID        `db:"agent_id" json:"agentId"`
	ExecutorType   TaskExecutorType `db:"executor_type" json:"executorType"`
	LeaseTokenHash string           `db:"lease_token_hash" json:"-"`
	LeaseToken     string           `db:"-" json:"leaseToken"`
	LeasedAt       time.Time        `db:"leased_at" json:"leasedAt"`
	ExpiresAt      time.Time        `db:"expires_at" json:"expiresAt"`
	HeartbeatAt    time.Time        `db:"heartbeat_at" json:"heartbeatAt"`
	Attempt        int              `db:"attempt" json:"attempt"`
	ReleasedAt     *time.Time       `db:"released_at" json:"releasedAt,omitempty"`
	PlanChecksum   string           `db:"plan_checksum" json:"planChecksum"`
	Task           Task             `db:"-" json:"task"`
	Steps          []TaskLeaseStep  `db:"-" json:"steps"`
}

type TaskLeaseStep struct {
	StepRunID        uuid.UUID      `db:"step_run_id" json:"stepRunId"`
	StepKey          string         `db:"step_key" json:"stepKey"`
	Name             string         `db:"name" json:"name"`
	ActionType       string         `db:"action_type" json:"actionType"`
	ActionVersion    string         `db:"-" json:"actionVersion"`
	InputBindings    map[string]any `db:"input_bindings" json:"inputBindings"`
	SecretReferences []string       `db:"-" json:"secretReferences"`
	IdempotencyKey   string         `db:"-" json:"idempotencyKey"`
	SortOrder        int            `db:"sort_order" json:"sortOrder"`
}
