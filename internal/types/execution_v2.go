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
	ExecutionAttemptStatusUnknown   ExecutionAttemptStatus = "UNKNOWN"
)

func (s ExecutionAttemptStatus) IsTerminal() bool {
	switch s {
	case ExecutionAttemptStatusSucceeded, ExecutionAttemptStatusFailed,
		ExecutionAttemptStatusCanceled, ExecutionAttemptStatusTimedOut,
		ExecutionAttemptStatusFenced, ExecutionAttemptStatusUnknown:
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

type CancelRequest struct {
	OrganizationID uuid.UUID
	ExecutionID    uuid.UUID
	RequestedBy    uuid.UUID
	IdempotencyKey string
	Reason         string
	RequestedAt    time.Time
}

type CancelRequestStatus string

const (
	CancelRequestStatusRequested    CancelRequestStatus = "REQUESTED"
	CancelRequestStatusAcknowledged CancelRequestStatus = "ACKNOWLEDGED"
	CancelRequestStatusRejected     CancelRequestStatus = "REJECTED"
)

type ExecutionCancelRequest struct {
	ID                 uuid.UUID           `db:"id" json:"id"`
	CreatedAt          time.Time           `db:"created_at" json:"createdAt"`
	OrganizationID     uuid.UUID           `db:"organization_id" json:"organizationId"`
	ExecutionID        uuid.UUID           `db:"execution_id" json:"executionId"`
	ExecutionAttemptID uuid.UUID           `db:"execution_attempt_id" json:"executionAttemptId"`
	RequestedBy        uuid.UUID           `db:"requested_by" json:"requestedBy"`
	IdempotencyKey     string              `db:"idempotency_key" json:"idempotencyKey"`
	Reason             string              `db:"reason" json:"reason"`
	Status             CancelRequestStatus `db:"status" json:"status"`
	AcknowledgedAt     *time.Time          `db:"acknowledged_at" json:"acknowledgedAt,omitempty"`
	AcknowledgedBy     string              `db:"acknowledged_by" json:"acknowledgedBy,omitempty"`
}

type CancelAcknowledgement struct {
	OrganizationID  uuid.UUID
	CancelRequestID uuid.UUID
	AttemptID       uuid.UUID
	ExecutorID      string
	FenceGeneration int64
	Accepted        bool
	AcknowledgedAt  time.Time
}

type StatusRequest struct {
	OrganizationID uuid.UUID
	ExecutionID    uuid.UUID
	RequestedBy    uuid.UUID
	IdempotencyKey string
	Reason         string
	RequestedAt    time.Time
	ExpiresAt      time.Time
}

type StatusQueryStatus string

const (
	StatusQueryStatusPending  StatusQueryStatus = "PENDING"
	StatusQueryStatusReported StatusQueryStatus = "REPORTED"
	StatusQueryStatusExpired  StatusQueryStatus = "EXPIRED"
)

type ExecutionStatusQuery struct {
	ID                 uuid.UUID         `db:"id" json:"id"`
	CreatedAt          time.Time         `db:"created_at" json:"createdAt"`
	OrganizationID     uuid.UUID         `db:"organization_id" json:"organizationId"`
	ExecutionID        uuid.UUID         `db:"execution_id" json:"executionId"`
	ExecutionAttemptID uuid.UUID         `db:"execution_attempt_id" json:"executionAttemptId"`
	RequestedBy        uuid.UUID         `db:"requested_by" json:"requestedBy"`
	IdempotencyKey     string            `db:"idempotency_key" json:"idempotencyKey"`
	Reason             string            `db:"reason" json:"reason"`
	Status             StatusQueryStatus `db:"status" json:"status"`
	ExpiresAt          time.Time         `db:"expires_at" json:"expiresAt"`
	ReportedAt         *time.Time        `db:"reported_at" json:"reportedAt,omitempty"`
}

type ReconciliationOutcome string

const (
	ReconciliationOutcomeProvenSucceeded ReconciliationOutcome = "PROVEN_SUCCEEDED"
	ReconciliationOutcomeProvenFailed    ReconciliationOutcome = "PROVEN_FAILED"
	ReconciliationOutcomeUnknown         ReconciliationOutcome = "UNKNOWN"
)

func (o ReconciliationOutcome) IsValid() bool {
	return o == ReconciliationOutcomeProvenSucceeded ||
		o == ReconciliationOutcomeProvenFailed ||
		o == ReconciliationOutcomeUnknown
}

type RetryDisposition string

const (
	RetryDispositionAllowed      RetryDisposition = "ALLOWED"
	RetryDispositionForbidden    RetryDisposition = "FORBIDDEN"
	RetryDispositionNotRequested RetryDisposition = "NOT_REQUESTED"
)

type ReconciliationStatusInput struct {
	OrganizationID      uuid.UUID
	ExecutionID         uuid.UUID
	StatusQueryID       uuid.UUID
	EventIdentity       uuid.UUID
	Outcome             ReconciliationOutcome
	EvidenceChecksum    string
	ObservedAt          time.Time
	OperationIncomplete bool
	RetryRequested      bool
}

type ReconciliationDecision struct {
	Status           ExecutionAttemptStatus
	RetryDisposition RetryDisposition
}

type ExecutionReconciliationEvent struct {
	ID                  uuid.UUID             `db:"id" json:"id"`
	CreatedAt           time.Time             `db:"created_at" json:"createdAt"`
	OrganizationID      uuid.UUID             `db:"organization_id" json:"organizationId"`
	ExecutionID         uuid.UUID             `db:"execution_id" json:"executionId"`
	ExecutionAttemptID  uuid.UUID             `db:"execution_attempt_id" json:"executionAttemptId"`
	StatusQueryID       uuid.UUID             `db:"status_query_id" json:"statusQueryId"`
	EventIdentity       uuid.UUID             `db:"event_identity" json:"eventIdentity"`
	Outcome             ReconciliationOutcome `db:"outcome" json:"outcome"`
	EvidenceChecksum    string                `db:"evidence_checksum" json:"evidenceChecksum"`
	ObservedAt          time.Time             `db:"observed_at" json:"observedAt"`
	OperationIncomplete bool                  `db:"operation_incomplete" json:"operationIncomplete"`
	RetryRequested      bool                  `db:"retry_requested" json:"retryRequested"`
	RetryDisposition    RetryDisposition      `db:"retry_disposition" json:"retryDisposition"`
}
