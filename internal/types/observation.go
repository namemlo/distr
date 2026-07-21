package types

import (
	"time"

	"github.com/google/uuid"
)

type ObservedHealth string

const (
	ObservedHealthUnknown   ObservedHealth = "UNKNOWN"
	ObservedHealthHealthy   ObservedHealth = "HEALTHY"
	ObservedHealthUnhealthy ObservedHealth = "UNHEALTHY"
)

type ObservationOutcome string

const (
	ObservationOutcomeComplete  ObservationOutcome = "COMPLETE"
	ObservationOutcomePartial   ObservationOutcome = "PARTIAL"
	ObservationOutcomeUnknown   ObservationOutcome = "UNKNOWN"
	ObservationOutcomeCancelled ObservationOutcome = "CANCELLED"
	ObservationOutcomeFailed    ObservationOutcome = "FAILED"
)

type ObservationDisposition string

const (
	ObservationDispositionAccepted   ObservationDisposition = "ACCEPTED"
	ObservationDispositionReplay     ObservationDisposition = "REPLAY"
	ObservationDispositionOutOfOrder ObservationDisposition = "OUT_OF_ORDER"
	ObservationDispositionConflict   ObservationDisposition = "CONFLICT"
	ObservationDispositionRejected   ObservationDisposition = "REJECTED"
)

type ObserverRegistration struct {
	ID                    uuid.UUID     `db:"id" json:"id"`
	CreatedAt             time.Time     `db:"created_at" json:"createdAt"`
	UpdatedAt             time.Time     `db:"updated_at" json:"updatedAt"`
	OrganizationID        uuid.UUID     `db:"organization_id" json:"organizationId"`
	DeploymentUnitID      uuid.UUID     `db:"deployment_unit_id" json:"deploymentUnitId"`
	ComponentInstanceID   *uuid.UUID    `db:"component_instance_id" json:"componentInstanceId,omitempty"`
	ObserverKey           string        `db:"observer_key" json:"observerKey"`
	AdapterImplementation string        `db:"adapter_implementation" json:"adapterImplementation"`
	AdapterVersion        string        `db:"adapter_version" json:"adapterVersion"`
	Enabled               bool          `db:"enabled" json:"enabled"`
	CredentialFingerprint string        `db:"credential_fingerprint" json:"credentialFingerprint,omitempty"`
	MaxFreshness          time.Duration `db:"-" json:"maxFreshness"`
	MaxClockSkew          time.Duration `db:"-" json:"maxClockSkew"`
	Measurements          []string      `db:"measurements" json:"measurements"`
}

type ObservationEnvelope struct {
	OrganizationID        uuid.UUID          `json:"organizationId"`
	ObserverID            uuid.UUID          `json:"observerId"`
	DeploymentUnitID      uuid.UUID          `json:"deploymentUnitId"`
	ComponentInstanceID   uuid.UUID          `json:"componentInstanceId"`
	ComponentKey          string             `json:"componentKey"`
	SourceSequence        int64              `json:"sourceSequence"`
	CapturedAt            time.Time          `json:"capturedAt"`
	CredentialFingerprint string             `json:"-"`
	EvidenceChecksum      string             `json:"evidenceChecksum"`
	EvidenceReference     string             `json:"evidenceReference,omitempty"`
	ArtifactDigest        string             `json:"artifactDigest"`
	ConfigChecksum        string             `json:"configChecksum"`
	SchemaVersion         string             `json:"schemaVersion"`
	CapabilityChecksum    string             `json:"capabilityChecksum"`
	Platform              string             `json:"platform"`
	TopologyChecksum      string             `json:"topologyChecksum"`
	Health                ObservedHealth     `json:"health"`
	Outcome               ObservationOutcome `json:"outcome"`
}

type ObservedComponentState struct {
	ID                   uuid.UUID              `db:"id" json:"id"`
	CreatedAt            time.Time              `db:"created_at" json:"createdAt"`
	OrganizationID       uuid.UUID              `db:"organization_id" json:"organizationId"`
	ObserverID           uuid.UUID              `db:"observer_id" json:"observerId"`
	DeploymentUnitID     uuid.UUID              `db:"deployment_unit_id" json:"deploymentUnitId"`
	ComponentInstanceID  uuid.UUID              `db:"component_instance_id" json:"componentInstanceId"`
	ComponentKey         string                 `db:"component_key" json:"componentKey"`
	SourceSequence       int64                  `db:"source_sequence" json:"sourceSequence"`
	CapturedAt           time.Time              `db:"captured_at" json:"capturedAt"`
	ReceivedAt           time.Time              `db:"received_at" json:"receivedAt"`
	FreshUntil           time.Time              `db:"fresh_until" json:"freshUntil"`
	EvidenceChecksum     string                 `db:"evidence_checksum" json:"evidenceChecksum"`
	EvidenceReference    string                 `db:"evidence_reference" json:"evidenceReference,omitempty"`
	ArtifactDigest       string                 `db:"artifact_digest" json:"artifactDigest"`
	ConfigChecksum       string                 `db:"config_checksum" json:"configChecksum"`
	SchemaVersion        string                 `db:"schema_version" json:"schemaVersion"`
	CapabilityChecksum   string                 `db:"capability_checksum" json:"capabilityChecksum"`
	Platform             string                 `db:"platform" json:"platform"`
	TopologyChecksum     string                 `db:"topology_checksum" json:"topologyChecksum"`
	Health               ObservedHealth         `db:"health" json:"health"`
	Outcome              ObservationOutcome     `db:"outcome" json:"outcome"`
	Disposition          ObservationDisposition `db:"disposition" json:"disposition"`
	Trusted              bool                   `db:"trusted" json:"trusted"`
	Current              bool                   `db:"is_current" json:"current"`
	StateChecksum        string                 `db:"state_checksum" json:"stateChecksum"`
	RuntimeStateChecksum string                 `db:"runtime_state_checksum" json:"runtimeStateChecksum"`
	ExecutorOutcome      ExecutorOutcome        `db:"executor_outcome" json:"executorOutcome,omitempty"`
}

type ComponentObservationHead struct {
	OrganizationID      uuid.UUID `db:"organization_id" json:"organizationId"`
	ObserverID          uuid.UUID `db:"observer_id" json:"observerId"`
	DeploymentUnitID    uuid.UUID `db:"deployment_unit_id" json:"deploymentUnitId"`
	ComponentInstanceID uuid.UUID `db:"component_instance_id" json:"componentInstanceId"`
	SourceSequence      int64     `db:"source_sequence" json:"sourceSequence"`
	ObservationID       uuid.UUID `db:"observation_id" json:"observationId"`
	EvidenceChecksum    string    `db:"evidence_checksum" json:"evidenceChecksum"`
	CapturedAt          time.Time `db:"captured_at" json:"capturedAt"`
}

type ObservationAdmissionDecision struct {
	Disposition    ObservationDisposition `json:"disposition"`
	Trusted        bool                   `json:"trusted"`
	AdvanceHead    bool                   `json:"advanceHead"`
	RetainEvidence bool                   `json:"retainEvidence"`
	Quarantine     bool                   `json:"quarantine"`
	Reason         string                 `json:"reason,omitempty"`
}

type ObservationGateStatus string

const (
	ObservationGateStatusPending   ObservationGateStatus = "PENDING"
	ObservationGateStatusVerified  ObservationGateStatus = "VERIFIED"
	ObservationGateStatusPartial   ObservationGateStatus = "PARTIAL"
	ObservationGateStatusFailed    ObservationGateStatus = "FAILED"
	ObservationGateStatusCancelled ObservationGateStatus = "CANCELLED"
	ObservationGateStatusUnknown   ObservationGateStatus = "UNKNOWN"
	ObservationGateStatusTimedOut  ObservationGateStatus = "TIMED_OUT"
	ObservationGateStatusConflict  ObservationGateStatus = "CONFLICT"
)

type ObservationGateResult struct {
	Status              ObservationGateStatus `json:"status"`
	ObservationID       uuid.UUID             `json:"observationId,omitempty"`
	ObservationChecksum string                `json:"observationChecksum,omitempty"`
	Reason              string                `json:"reason,omitempty"`
	Quarantine          bool                  `json:"quarantine"`
	ReleaseMutationLock bool                  `json:"releaseMutationLock"`
}
