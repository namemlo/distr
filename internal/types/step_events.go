package types

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/google/uuid"
)

var stepRunOutputNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

const (
	MaxStepRunEventMessageLength   = 2048
	MaxStepRunEventDetailsBytes    = 16 * 1024
	MaxStepRunLogChunkBodyLength   = 8 * 1024
	MaxStepRunOutputNameLength     = 128
	MaxStepRunOutputValueBytes     = 16 * 1024
	MaxStepRunEventLogChunkCount   = 16
	MaxStepRunEventOutputItemCount = 32
)

type StepRunEventType string

const (
	StepRunEventTypeStarted   StepRunEventType = "STARTED"
	StepRunEventTypeProgress  StepRunEventType = "PROGRESS"
	StepRunEventTypeLog       StepRunEventType = "LOG"
	StepRunEventTypeOutput    StepRunEventType = "OUTPUT"
	StepRunEventTypeSucceeded StepRunEventType = "SUCCEEDED"
	StepRunEventTypeFailed    StepRunEventType = "FAILED"
)

func (t StepRunEventType) IsValid() bool {
	switch t {
	case StepRunEventTypeStarted,
		StepRunEventTypeProgress,
		StepRunEventTypeLog,
		StepRunEventTypeOutput,
		StepRunEventTypeSucceeded,
		StepRunEventTypeFailed:
		return true
	default:
		return false
	}
}

type StepRunLogStream string

const (
	StepRunLogStreamStdout StepRunLogStream = "stdout"
	StepRunLogStreamStderr StepRunLogStream = "stderr"
	StepRunLogStreamSystem StepRunLogStream = "system"
)

func (s StepRunLogStream) IsValid() bool {
	switch s {
	case StepRunLogStreamStdout, StepRunLogStreamStderr, StepRunLogStreamSystem:
		return true
	default:
		return false
	}
}

type StepRunLogSeverity string

const (
	StepRunLogSeverityDebug StepRunLogSeverity = "debug"
	StepRunLogSeverityInfo  StepRunLogSeverity = "info"
	StepRunLogSeverityWarn  StepRunLogSeverity = "warn"
	StepRunLogSeverityError StepRunLogSeverity = "error"
)

func (s StepRunLogSeverity) IsValid() bool {
	switch s {
	case StepRunLogSeverityDebug,
		StepRunLogSeverityInfo,
		StepRunLogSeverityWarn,
		StepRunLogSeverityError:
		return true
	default:
		return false
	}
}

type RecordAgentStepRunEventRequest struct {
	OrganizationID  uuid.UUID
	AgentID         uuid.UUID
	StepRunID       uuid.UUID
	LeaseToken      string
	Sequence        int64
	Type            StepRunEventType
	OccurredAt      *time.Time
	Message         string
	ProgressPercent *int
	Details         map[string]any
	Logs            []RecordStepRunLogChunkRequest
	Outputs         []RecordStepRunOutputRequest
}

type RecordStepRunLogChunkRequest struct {
	OccurredAt *time.Time
	Stream     StepRunLogStream
	Severity   StepRunLogSeverity
	Body       string
}

type RecordStepRunOutputRequest struct {
	Name      string
	Value     any
	Sensitive bool
}

func IsValidStepRunOutputName(name string) bool {
	return stepRunOutputNamePattern.MatchString(name)
}

type TaskTimeline struct {
	OrganizationID uuid.UUID      `json:"organizationId"`
	TaskID         uuid.UUID      `json:"taskId"`
	Events         []StepRunEvent `json:"events"`
}

type StepRunEvent struct {
	ID              uuid.UUID         `db:"id" json:"id"`
	CreatedAt       time.Time         `db:"created_at" json:"createdAt"`
	OccurredAt      time.Time         `db:"occurred_at" json:"occurredAt"`
	OrganizationID  uuid.UUID         `db:"organization_id" json:"organizationId"`
	TaskID          uuid.UUID         `db:"task_id" json:"taskId"`
	StepRunID       uuid.UUID         `db:"step_run_id" json:"stepRunId"`
	TaskLeaseID     uuid.UUID         `db:"task_lease_id" json:"taskLeaseId"`
	AgentID         uuid.UUID         `db:"agent_id" json:"agentId"`
	Sequence        int64             `db:"sequence" json:"sequence"`
	Type            StepRunEventType  `db:"event_type" json:"type"`
	Message         string            `db:"message" json:"message,omitempty"`
	ProgressPercent *int              `db:"progress_percent" json:"progressPercent,omitempty"`
	Details         map[string]any    `db:"details" json:"details,omitempty"`
	Redacted        bool              `db:"redacted" json:"redacted"`
	Logs            []StepRunLogChunk `db:"-" json:"logs"`
	Outputs         []StepRunOutput   `db:"-" json:"outputs"`
}

type StepRunLogChunk struct {
	ID             uuid.UUID          `db:"id" json:"id"`
	CreatedAt      time.Time          `db:"created_at" json:"createdAt"`
	OccurredAt     time.Time          `db:"occurred_at" json:"occurredAt"`
	EventID        uuid.UUID          `db:"event_id" json:"eventId"`
	OrganizationID uuid.UUID          `db:"organization_id" json:"organizationId"`
	TaskID         uuid.UUID          `db:"task_id" json:"taskId"`
	StepRunID      uuid.UUID          `db:"step_run_id" json:"stepRunId"`
	TaskLeaseID    uuid.UUID          `db:"task_lease_id" json:"taskLeaseId"`
	AgentID        uuid.UUID          `db:"agent_id" json:"agentId"`
	ChunkIndex     int                `db:"chunk_index" json:"chunkIndex"`
	Stream         StepRunLogStream   `db:"stream" json:"stream"`
	Severity       StepRunLogSeverity `db:"severity" json:"severity"`
	Body           string             `db:"body" json:"body"`
	Redacted       bool               `db:"redacted" json:"redacted"`
}

type StepRunOutput struct {
	ID             uuid.UUID       `db:"id" json:"id"`
	CreatedAt      time.Time       `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time       `db:"updated_at" json:"updatedAt"`
	EventID        uuid.UUID       `db:"event_id" json:"eventId"`
	OrganizationID uuid.UUID       `db:"organization_id" json:"organizationId"`
	TaskID         uuid.UUID       `db:"task_id" json:"taskId"`
	StepRunID      uuid.UUID       `db:"step_run_id" json:"stepRunId"`
	TaskLeaseID    uuid.UUID       `db:"task_lease_id" json:"taskLeaseId"`
	AgentID        uuid.UUID       `db:"agent_id" json:"agentId"`
	Name           string          `db:"name" json:"name"`
	Value          json.RawMessage `db:"value" json:"value,omitempty"`
	Sensitive      bool            `db:"sensitive" json:"sensitive"`
	Redacted       bool            `db:"redacted" json:"redacted"`
}
