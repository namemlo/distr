package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ControlPlaneAuditEventInput struct {
	OrganizationID         uuid.UUID
	EventType              string
	ActorID                *uuid.UUID
	Outcome                string
	ReleaseID              *uuid.UUID
	TargetConfigID         *uuid.UUID
	DeploymentPlanID       *uuid.UUID
	ApprovalID             *uuid.UUID
	CampaignID             *uuid.UUID
	WaveID                 *uuid.UUID
	ExecutionID            *uuid.UUID
	AdapterRevisionID      *uuid.UUID
	ObservationID          *uuid.UUID
	ReconciliationID       *uuid.UUID
	ReleaseChecksum        string
	TargetConfigChecksum   string
	DeploymentPlanChecksum string
	ApprovalChecksum       string
	CampaignChecksum       string
	ExecutionChecksum      string
	ObservationChecksum    string
	Payload                json.RawMessage
}

type ControlPlaneAuditEvent struct {
	ID                     uuid.UUID       `db:"id" json:"id"`
	OrganizationID         uuid.UUID       `db:"organization_id" json:"organizationId"`
	Sequence               int64           `db:"sequence" json:"sequence"`
	EventType              string          `db:"event_type" json:"eventType"`
	ActorID                *uuid.UUID      `db:"actor_id" json:"actorId,omitempty"`
	Outcome                string          `db:"outcome" json:"outcome"`
	ReleaseID              *uuid.UUID      `db:"release_id" json:"releaseId,omitempty"`
	TargetConfigID         *uuid.UUID      `db:"target_config_id" json:"targetConfigId,omitempty"`
	DeploymentPlanID       *uuid.UUID      `db:"deployment_plan_id" json:"deploymentPlanId,omitempty"`
	ApprovalID             *uuid.UUID      `db:"approval_id" json:"approvalId,omitempty"`
	CampaignID             *uuid.UUID      `db:"campaign_id" json:"campaignId,omitempty"`
	WaveID                 *uuid.UUID      `db:"wave_id" json:"waveId,omitempty"`
	ExecutionID            *uuid.UUID      `db:"execution_id" json:"executionId,omitempty"`
	AdapterRevisionID      *uuid.UUID      `db:"adapter_revision_id" json:"adapterRevisionId,omitempty"`
	ObservationID          *uuid.UUID      `db:"observation_id" json:"observationId,omitempty"`
	ReconciliationID       *uuid.UUID      `db:"reconciliation_id" json:"reconciliationId,omitempty"`
	ReleaseChecksum        string          `db:"release_checksum" json:"releaseChecksum,omitempty"`
	TargetConfigChecksum   string          `db:"target_config_checksum" json:"targetConfigChecksum,omitempty"`
	DeploymentPlanChecksum string          `db:"deployment_plan_checksum" json:"deploymentPlanChecksum,omitempty"`
	ApprovalChecksum       string          `db:"approval_checksum" json:"approvalChecksum,omitempty"`
	CampaignChecksum       string          `db:"campaign_checksum" json:"campaignChecksum,omitempty"`
	ExecutionChecksum      string          `db:"execution_checksum" json:"executionChecksum,omitempty"`
	ObservationChecksum    string          `db:"observation_checksum" json:"observationChecksum,omitempty"`
	Payload                json.RawMessage `db:"payload" json:"payload,omitempty"`
	PayloadRedacted        bool            `db:"payload_redacted" json:"payloadRedacted"`
	PayloadTruncated       bool            `db:"payload_truncated" json:"payloadTruncated"`
	CreatedAt              time.Time       `db:"created_at" json:"createdAt"`
}

type EvidenceBundleQuery struct {
	OrganizationID   uuid.UUID
	DeploymentPlanID uuid.UUID
}

type EvidenceBundle struct {
	OrganizationID   uuid.UUID                `json:"organizationId"`
	DeploymentPlanID uuid.UUID                `json:"deploymentPlanId"`
	Events           []ControlPlaneAuditEvent `json:"events"`
	Checksum         string                   `json:"checksum"`
}

type ExportBatchResult struct {
	SinkID        uuid.UUID `json:"sinkId"`
	Exported      int       `json:"exported"`
	LastSequence  int64     `json:"lastSequence"`
	CheckpointLag int64     `json:"checkpointLag"`
}

type AuditExportSinkKind string

const (
	AuditExportSinkKindWebhook     AuditExportSinkKind = "webhook"
	AuditExportSinkKindObjectStore AuditExportSinkKind = "object_store"
	AuditExportSinkKindSIEM        AuditExportSinkKind = "siem"
)

func (kind AuditExportSinkKind) Valid() bool {
	switch kind {
	case AuditExportSinkKindWebhook, AuditExportSinkKindObjectStore, AuditExportSinkKindSIEM:
		return true
	default:
		return false
	}
}

type CreateAuditExportSinkInput struct {
	OrganizationID    uuid.UUID
	Name              string
	Kind              AuditExportSinkKind
	EndpointReference string
	ConfigChecksum    string
	Enabled           bool
}

type AuditExportSink struct {
	ID                  uuid.UUID           `db:"id" json:"id"`
	OrganizationID      uuid.UUID           `db:"organization_id" json:"organizationId"`
	Name                string              `db:"name" json:"name"`
	Kind                AuditExportSinkKind `db:"kind" json:"kind"`
	EndpointReference   string              `db:"endpoint_reference" json:"endpointReference"`
	ConfigChecksum      string              `db:"config_checksum" json:"configChecksum"`
	Enabled             bool                `db:"enabled" json:"enabled"`
	LastSuccessAt       *time.Time          `db:"last_success_at" json:"lastSuccessAt,omitempty"`
	LastFailureAt       *time.Time          `db:"last_failure_at" json:"lastFailureAt,omitempty"`
	ConsecutiveFailures int                 `db:"consecutive_failures" json:"consecutiveFailures"`
	CreatedAt           time.Time           `db:"created_at" json:"createdAt"`
	UpdatedAt           time.Time           `db:"updated_at" json:"updatedAt"`
}

type AuditExportStatus struct {
	Sink                   AuditExportSink `json:"sink"`
	LastExportedSequence   int64           `json:"lastExportedSequence"`
	LastExportedEventID    *uuid.UUID      `json:"lastExportedEventId,omitempty"`
	LatestSequence         int64           `json:"latestSequence"`
	CheckpointLag          int64           `json:"checkpointLag"`
	Alert                  bool            `json:"alert"`
	LastAttemptStatus      string          `json:"lastAttemptStatus,omitempty"`
	LastAttemptError       string          `json:"lastAttemptError,omitempty"`
	LastAttemptCompletedAt *time.Time      `json:"lastAttemptCompletedAt,omitempty"`
}
