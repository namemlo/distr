package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

const (
	MaxStepRunEventMessageLength   = types.MaxStepRunEventMessageLength
	MaxStepRunEventDetailsBytes    = types.MaxStepRunEventDetailsBytes
	MaxStepRunLogChunkBodyLength   = types.MaxStepRunLogChunkBodyLength
	MaxStepRunOutputNameLength     = types.MaxStepRunOutputNameLength
	MaxStepRunOutputValueBytes     = types.MaxStepRunOutputValueBytes
	MaxStepRunEventLogChunkCount   = types.MaxStepRunEventLogChunkCount
	MaxStepRunEventOutputItemCount = types.MaxStepRunEventOutputItemCount
)

type AgentStepRunEventRequest struct {
	LeaseToken      string                        `json:"leaseToken"`
	Sequence        int64                         `json:"sequence"`
	Type            types.StepRunEventType        `json:"type"`
	OccurredAt      *time.Time                    `json:"occurredAt,omitempty"`
	Message         string                        `json:"message,omitempty"`
	ProgressPercent *int                          `json:"progressPercent,omitempty"`
	Details         map[string]any                `json:"details,omitempty"`
	Logs            []AgentStepRunLogChunkRequest `json:"logs,omitempty"`
	Outputs         []AgentStepRunOutputRequest   `json:"outputs,omitempty"`
}

type AgentStepRunLogChunkRequest struct {
	OccurredAt *time.Time               `json:"occurredAt,omitempty"`
	Stream     types.StepRunLogStream   `json:"stream"`
	Severity   types.StepRunLogSeverity `json:"severity"`
	Body       string                   `json:"body"`
}

type AgentStepRunOutputRequest struct {
	Name      string `json:"name"`
	Value     any    `json:"value,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

func (r *AgentStepRunEventRequest) Validate() error {
	r.LeaseToken = strings.TrimSpace(r.LeaseToken)
	r.Message = strings.TrimSpace(r.Message)
	if r.LeaseToken == "" {
		return validation.NewValidationFailedError("leaseToken is required")
	}
	if r.Sequence <= 0 {
		return validation.NewValidationFailedError("sequence must be greater than 0")
	}
	if !r.Type.IsValid() {
		return validation.NewValidationFailedError("type is invalid")
	}
	if len(r.Message) > MaxStepRunEventMessageLength {
		return validation.NewValidationFailedError("message is too long")
	}
	if r.ProgressPercent != nil && (*r.ProgressPercent < 0 || *r.ProgressPercent > 100) {
		return validation.NewValidationFailedError("progressPercent must be between 0 and 100")
	}
	if len(r.Logs) > MaxStepRunEventLogChunkCount {
		return validation.NewValidationFailedError("logs contains too many entries")
	}
	if len(r.Outputs) > MaxStepRunEventOutputItemCount {
		return validation.NewValidationFailedError("outputs contains too many entries")
	}
	if len(r.Details) > 0 {
		if data, err := json.Marshal(r.Details); err != nil {
			return validation.NewValidationFailedError("details must be valid JSON")
		} else if len(data) > MaxStepRunEventDetailsBytes {
			return validation.NewValidationFailedError("details is too large")
		}
	}
	for i, log := range r.Logs {
		if !log.Stream.IsValid() {
			return validation.NewValidationFailedError(fmt.Sprintf("logs[%d].stream is invalid", i))
		}
		if !log.Severity.IsValid() {
			return validation.NewValidationFailedError(fmt.Sprintf("logs[%d].severity is invalid", i))
		}
		if strings.TrimSpace(log.Body) == "" {
			return validation.NewValidationFailedError(fmt.Sprintf("logs[%d].body is required", i))
		}
		if len(log.Body) > MaxStepRunLogChunkBodyLength {
			return validation.NewValidationFailedError(fmt.Sprintf("logs[%d].body is too long", i))
		}
	}
	seenOutputs := map[string]struct{}{}
	for i := range r.Outputs {
		r.Outputs[i].Name = strings.TrimSpace(r.Outputs[i].Name)
		if r.Outputs[i].Name == "" {
			return validation.NewValidationFailedError(fmt.Sprintf("outputs[%d].name is required", i))
		}
		if len(r.Outputs[i].Name) > MaxStepRunOutputNameLength {
			return validation.NewValidationFailedError(fmt.Sprintf("outputs[%d].name is too long", i))
		}
		if data, err := json.Marshal(r.Outputs[i].Value); err != nil {
			return validation.NewValidationFailedError(fmt.Sprintf("outputs[%d].value must be valid JSON", i))
		} else if len(data) > MaxStepRunOutputValueBytes {
			return validation.NewValidationFailedError(fmt.Sprintf("outputs[%d].value is too large", i))
		}
		if _, ok := seenOutputs[r.Outputs[i].Name]; ok {
			return validation.NewValidationFailedError("outputs contains duplicate name")
		}
		seenOutputs[r.Outputs[i].Name] = struct{}{}
	}
	return nil
}

type TaskTimeline struct {
	OrganizationID uuid.UUID      `json:"organizationId"`
	TaskID         uuid.UUID      `json:"taskId"`
	Events         []StepRunEvent `json:"events"`
}

type StepRunEvent struct {
	ID              uuid.UUID              `json:"id"`
	CreatedAt       time.Time              `json:"createdAt"`
	OccurredAt      time.Time              `json:"occurredAt"`
	OrganizationID  uuid.UUID              `json:"organizationId"`
	TaskID          uuid.UUID              `json:"taskId"`
	StepRunID       uuid.UUID              `json:"stepRunId"`
	TaskLeaseID     uuid.UUID              `json:"taskLeaseId"`
	AgentID         uuid.UUID              `json:"agentId"`
	Sequence        int64                  `json:"sequence"`
	Type            types.StepRunEventType `json:"type"`
	Message         string                 `json:"message,omitempty"`
	ProgressPercent *int                   `json:"progressPercent,omitempty"`
	Details         map[string]any         `json:"details,omitempty"`
	Redacted        bool                   `json:"redacted"`
	Logs            []StepRunLogChunk      `json:"logs"`
	Outputs         []StepRunOutput        `json:"outputs"`
}

type StepRunLogChunk struct {
	ID             uuid.UUID                `json:"id"`
	CreatedAt      time.Time                `json:"createdAt"`
	OccurredAt     time.Time                `json:"occurredAt"`
	EventID        uuid.UUID                `json:"eventId"`
	OrganizationID uuid.UUID                `json:"organizationId"`
	TaskID         uuid.UUID                `json:"taskId"`
	StepRunID      uuid.UUID                `json:"stepRunId"`
	TaskLeaseID    uuid.UUID                `json:"taskLeaseId"`
	AgentID        uuid.UUID                `json:"agentId"`
	ChunkIndex     int                      `json:"chunkIndex"`
	Stream         types.StepRunLogStream   `json:"stream"`
	Severity       types.StepRunLogSeverity `json:"severity"`
	Body           string                   `json:"body"`
	Redacted       bool                     `json:"redacted"`
}

type StepRunOutput struct {
	ID             uuid.UUID       `json:"id"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	EventID        uuid.UUID       `json:"eventId"`
	OrganizationID uuid.UUID       `json:"organizationId"`
	TaskID         uuid.UUID       `json:"taskId"`
	StepRunID      uuid.UUID       `json:"stepRunId"`
	TaskLeaseID    uuid.UUID       `json:"taskLeaseId"`
	AgentID        uuid.UUID       `json:"agentId"`
	Name           string          `json:"name"`
	Value          json.RawMessage `json:"value,omitempty"`
	Sensitive      bool            `json:"sensitive"`
	Redacted       bool            `json:"redacted"`
}
