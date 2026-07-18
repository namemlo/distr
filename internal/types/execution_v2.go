package types

import (
	"crypto/ed25519"
	"time"

	"github.com/google/uuid"
)

type ExecutionProtocolVersion string

const (
	ExecutionProtocolVersionV1 ExecutionProtocolVersion = "v1"
	ExecutionProtocolVersionV2 ExecutionProtocolVersion = "v2"
)

func (v ExecutionProtocolVersion) IsValid() bool {
	return v == ExecutionProtocolVersionV1 || v == ExecutionProtocolVersionV2
}

type ExecutionIdentity struct {
	ExecutionID   uuid.UUID `json:"executionId"`
	AttemptNumber int       `json:"attemptNumber"`
	StepKey       string    `json:"stepKey"`
}

type ExecutionFence struct {
	ResourceKey    string    `json:"resourceKey"`
	Generation     int64     `json:"generation"`
	LeaseExpiresAt time.Time `json:"leaseExpiresAt,omitempty"`
}

type ExecutionAttemptStatus string

const (
	ExecutionAttemptStatusPending   ExecutionAttemptStatus = "PENDING"
	ExecutionAttemptStatusClaimed   ExecutionAttemptStatus = "CLAIMED"
	ExecutionAttemptStatusRunning   ExecutionAttemptStatus = "RUNNING"
	ExecutionAttemptStatusSucceeded ExecutionAttemptStatus = "SUCCEEDED"
	ExecutionAttemptStatusFailed    ExecutionAttemptStatus = "FAILED"
	ExecutionAttemptStatusCanceled  ExecutionAttemptStatus = "CANCELED"
	ExecutionAttemptStatusTimedOut  ExecutionAttemptStatus = "TIMED_OUT"
	ExecutionAttemptStatusFenced    ExecutionAttemptStatus = "FENCED"
)

func (s ExecutionAttemptStatus) IsTerminal() bool {
	switch s {
	case ExecutionAttemptStatusSucceeded, ExecutionAttemptStatusFailed,
		ExecutionAttemptStatusCanceled, ExecutionAttemptStatusTimedOut, ExecutionAttemptStatusFenced:
		return true
	default:
		return false
	}
}

type ExecutionAttempt struct {
	ID                uuid.UUID              `db:"id" json:"id"`
	CreatedAt         time.Time              `db:"created_at" json:"createdAt"`
	UpdatedAt         time.Time              `db:"updated_at" json:"updatedAt"`
	OrganizationID    uuid.UUID              `db:"organization_id" json:"organizationId"`
	TaskID            uuid.UUID              `db:"task_id" json:"taskId"`
	StepRunID         uuid.UUID              `db:"step_run_id" json:"stepRunId"`
	Identity          ExecutionIdentity      `db:"-" json:"identity"`
	Status            ExecutionAttemptStatus `db:"status" json:"status"`
	ClaimedBy         string                 `db:"claimed_by" json:"claimedBy,omitempty"`
	PlanChecksum      string                 `db:"plan_checksum" json:"planChecksum"`
	ArtifactDigest    string                 `db:"artifact_digest" json:"artifactDigest"`
	ConfigChecksum    string                 `db:"config_checksum" json:"configChecksum"`
	AdapterRevision   string                 `db:"adapter_revision" json:"adapterRevision"`
	IntentIssuedAt    time.Time              `db:"intent_issued_at" json:"intentIssuedAt"`
	IntentExpiresAt   time.Time              `db:"intent_expires_at" json:"intentExpiresAt"`
	LastEventSequence int64                  `db:"last_event_sequence" json:"lastEventSequence"`
	AcknowledgedAt    *time.Time             `db:"acknowledged_at" json:"acknowledgedAt,omitempty"`
	CompletedAt       *time.Time             `db:"completed_at" json:"completedAt,omitempty"`
	Cancellable       bool                   `db:"cancellable" json:"cancellable"`
	RetrySafe         bool                   `db:"retry_safe" json:"retrySafe"`
	FailureReason     string                 `db:"failure_reason" json:"failureReason,omitempty"`
	Fence             ExecutionFence         `db:"-" json:"fence"`
}

type SignedExecutionIntent struct {
	Payload   []byte `json:"payload"`
	Checksum  string `json:"checksum"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}

type TrustPolicy struct {
	Keys                   map[string]ed25519.PublicKey
	RevokedKeyIDs          map[string]time.Time
	Now                    func() time.Time
	ExpectedArtifactDigest string
	ExpectedConfigChecksum string
}

type ClaimRequest struct {
	OrganizationID     uuid.UUID
	AttemptID          uuid.UUID
	ExecutorID         string
	ExpectedGeneration int64
	Now                time.Time
	LeaseDuration      time.Duration
}

type HeartbeatRequest struct {
	OrganizationID  uuid.UUID
	AttemptID       uuid.UUID
	ExecutorID      string
	FenceGeneration int64
	Now             time.Time
	LeaseDuration   time.Duration
}

type ExecutionEventStatus string

const (
	ExecutionEventStatusRunning   ExecutionEventStatus = "RUNNING"
	ExecutionEventStatusSucceeded ExecutionEventStatus = "SUCCEEDED"
	ExecutionEventStatusFailed    ExecutionEventStatus = "FAILED"
	ExecutionEventStatusCanceled  ExecutionEventStatus = "CANCELED"
)

func (s ExecutionEventStatus) IsValid() bool {
	switch s {
	case ExecutionEventStatusRunning, ExecutionEventStatusSucceeded,
		ExecutionEventStatusFailed, ExecutionEventStatusCanceled:
		return true
	default:
		return false
	}
}

type ExecutionEventInput struct {
	OrganizationID  uuid.UUID
	AttemptID       uuid.UUID
	Identity        ExecutionIdentity
	FenceGeneration int64
	EventSequence   int64
	Status          ExecutionEventStatus
	PayloadChecksum string
	Message         string
	OccurredAt      time.Time
}

type ExecutionEvent struct {
	ID              uuid.UUID            `db:"id" json:"id"`
	CreatedAt       time.Time            `db:"created_at" json:"createdAt"`
	OrganizationID  uuid.UUID            `db:"organization_id" json:"organizationId"`
	AttemptID       uuid.UUID            `db:"attempt_id" json:"attemptId"`
	Identity        ExecutionIdentity    `db:"-" json:"identity"`
	FenceGeneration int64                `db:"fence_generation" json:"fenceGeneration"`
	EventSequence   int64                `db:"event_sequence" json:"eventSequence"`
	Status          ExecutionEventStatus `db:"status" json:"status"`
	PayloadChecksum string               `db:"payload_checksum" json:"payloadChecksum"`
	Message         string               `db:"message" json:"message,omitempty"`
	OccurredAt      time.Time            `db:"occurred_at" json:"occurredAt"`
}

type CompletionInput struct {
	OrganizationID  uuid.UUID
	AttemptID       uuid.UUID
	ExecutorID      string
	FenceGeneration int64
	Status          ExecutionAttemptStatus
	CompletedAt     time.Time
	FailureReason   string
}
