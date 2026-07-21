package types

import (
	"time"

	"github.com/google/uuid"
)

type PendingDesiredStatus string

const (
	PendingDesiredStatusPending   PendingDesiredStatus = "PENDING"
	PendingDesiredStatusVerified  PendingDesiredStatus = "VERIFIED"
	PendingDesiredStatusPartial   PendingDesiredStatus = "PARTIAL"
	PendingDesiredStatusFailed    PendingDesiredStatus = "FAILED"
	PendingDesiredStatusCancelled PendingDesiredStatus = "CANCELLED"
	PendingDesiredStatusUnknown   PendingDesiredStatus = "UNKNOWN"
	PendingDesiredStatusTimedOut  PendingDesiredStatus = "TIMED_OUT"
	PendingDesiredStatusConflict  PendingDesiredStatus = "CONFLICT"
)

type PendingDesiredRevisionInput struct {
	OrganizationID      uuid.UUID `json:"organizationId"`
	DeploymentPlanID    uuid.UUID `json:"deploymentPlanId"`
	ExecutionID         uuid.UUID `json:"executionId"`
	ExecutionAttemptID  uuid.UUID `json:"executionAttemptId"`
	DeploymentUnitID    uuid.UUID `json:"deploymentUnitId"`
	ComponentInstanceID uuid.UUID `json:"componentInstanceId"`
	ComponentKey        string    `json:"componentKey"`
	ArtifactDigest      string    `json:"artifactDigest"`
	ConfigChecksum      string    `json:"configChecksum"`
	SchemaVersion       string    `json:"schemaVersion"`
	CapabilityChecksum  string    `json:"capabilityChecksum"`
	Platform            string    `json:"platform"`
	TopologyChecksum    string    `json:"topologyChecksum"`
	ObservationDeadline time.Time `json:"observationDeadline"`
}

type PendingDesiredRevision struct {
	ID                    uuid.UUID            `db:"id" json:"id"`
	CreatedAt             time.Time            `db:"created_at" json:"createdAt"`
	UpdatedAt             time.Time            `db:"updated_at" json:"updatedAt"`
	OrganizationID        uuid.UUID            `db:"organization_id" json:"organizationId"`
	DeploymentPlanID      uuid.UUID            `db:"deployment_plan_id" json:"deploymentPlanId"`
	ExecutionID           uuid.UUID            `db:"execution_id" json:"executionId"`
	ExecutionAttemptID    uuid.UUID            `db:"execution_attempt_id" json:"executionAttemptId"`
	DeploymentUnitID      uuid.UUID            `db:"deployment_unit_id" json:"deploymentUnitId"`
	ComponentInstanceID   uuid.UUID            `db:"component_instance_id" json:"componentInstanceId"`
	ComponentKey          string               `db:"component_key" json:"componentKey"`
	Revision              int64                `db:"revision" json:"revision"`
	ArtifactDigest        string               `db:"artifact_digest" json:"artifactDigest"`
	ConfigChecksum        string               `db:"config_checksum" json:"configChecksum"`
	SchemaVersion         string               `db:"schema_version" json:"schemaVersion"`
	CapabilityChecksum    string               `db:"capability_checksum" json:"capabilityChecksum"`
	Platform              string               `db:"platform" json:"platform"`
	TopologyChecksum      string               `db:"topology_checksum" json:"topologyChecksum"`
	ObservationDeadline   time.Time            `db:"observation_deadline" json:"observationDeadline"`
	Status                PendingDesiredStatus `db:"status" json:"status"`
	TerminalReason        string               `db:"terminal_reason" json:"terminalReason,omitempty"`
	VerifiedObservationID uuid.UUID            `db:"verified_observation_id" json:"verifiedObservationId,omitempty"`
	TerminalObservationID uuid.UUID            `db:"terminal_observation_id" json:"terminalObservationId,omitempty"`
	TerminalAt            *time.Time           `db:"terminal_at" json:"terminalAt,omitempty"`
}

type ActiveDesiredRevision struct {
	ID                    uuid.UUID `db:"id" json:"id"`
	CreatedAt             time.Time `db:"created_at" json:"createdAt"`
	OrganizationID        uuid.UUID `db:"organization_id" json:"organizationId"`
	PendingRevisionID     uuid.UUID `db:"pending_revision_id" json:"pendingRevisionId"`
	DeploymentPlanID      uuid.UUID `db:"deployment_plan_id" json:"deploymentPlanId"`
	ExecutionID           uuid.UUID `db:"execution_id" json:"executionId"`
	DeploymentUnitID      uuid.UUID `db:"deployment_unit_id" json:"deploymentUnitId"`
	ComponentInstanceID   uuid.UUID `db:"component_instance_id" json:"componentInstanceId"`
	ComponentKey          string    `db:"component_key" json:"componentKey"`
	Revision              int64     `db:"revision" json:"revision"`
	ArtifactDigest        string    `db:"artifact_digest" json:"artifactDigest"`
	ConfigChecksum        string    `db:"config_checksum" json:"configChecksum"`
	SchemaVersion         string    `db:"schema_version" json:"schemaVersion"`
	CapabilityChecksum    string    `db:"capability_checksum" json:"capabilityChecksum"`
	Platform              string    `db:"platform" json:"platform"`
	TopologyChecksum      string    `db:"topology_checksum" json:"topologyChecksum"`
	VerifiedObservationID uuid.UUID `db:"verified_observation_id" json:"verifiedObservationId"`
}

type ComponentDesiredStateHead struct {
	OrganizationID      uuid.UUID  `db:"organization_id" json:"organizationId"`
	DeploymentUnitID    uuid.UUID  `db:"deployment_unit_id" json:"deploymentUnitId"`
	ComponentInstanceID uuid.UUID  `db:"component_instance_id" json:"componentInstanceId"`
	ComponentKey        string     `db:"component_key" json:"componentKey"`
	PendingRevisionID   *uuid.UUID `db:"pending_revision_id" json:"pendingRevisionId,omitempty"`
	ActiveRevisionID    *uuid.UUID `db:"active_revision_id" json:"activeRevisionId,omitempty"`
	Quarantined         bool       `db:"quarantined" json:"quarantined"`
	QuarantineReason    string     `db:"quarantine_reason" json:"quarantineReason,omitempty"`
	UpdatedAt           time.Time  `db:"updated_at" json:"updatedAt"`
}

type ExecutorOutcome string

const (
	ExecutorOutcomeSucceeded ExecutorOutcome = "SUCCEEDED"
	ExecutorOutcomeFailed    ExecutorOutcome = "FAILED"
	ExecutorOutcomeCancelled ExecutorOutcome = "CANCELLED"
	ExecutorOutcomeUnknown   ExecutorOutcome = "UNKNOWN"
)

type ExecutorReport struct {
	ID                    uuid.UUID       `db:"id" json:"id"`
	CreatedAt             time.Time       `db:"created_at" json:"createdAt"`
	OrganizationID        uuid.UUID       `db:"organization_id" json:"organizationId"`
	PendingRevisionID     uuid.UUID       `db:"pending_revision_id" json:"pendingRevisionId"`
	ExecutionID           uuid.UUID       `db:"execution_id" json:"executionId"`
	Outcome               ExecutorOutcome `db:"outcome" json:"outcome"`
	ReportedStateChecksum string          `db:"reported_state_checksum" json:"reportedStateChecksum,omitempty"`
	EvidenceReference     string          `db:"evidence_reference" json:"evidenceReference,omitempty"`
}
