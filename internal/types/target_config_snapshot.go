package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const TargetConfigSnapshotSchema = "distr.target-config/v1"

type TargetConfigObjectKind string

const (
	TargetConfigObjectKindDeploymentDescriptor TargetConfigObjectKind = "deployment_descriptor"
	TargetConfigObjectKindServiceConfig        TargetConfigObjectKind = "service_config"
	TargetConfigObjectKindAdapterInput         TargetConfigObjectKind = "adapter_input"
)

func (kind TargetConfigObjectKind) IsValid() bool {
	switch kind {
	case TargetConfigObjectKindDeploymentDescriptor,
		TargetConfigObjectKindServiceConfig,
		TargetConfigObjectKindAdapterInput:
		return true
	default:
		return false
	}
}

type TargetConfigSnapshotObjectDraft struct {
	Key       string                 `json:"key"`
	Kind      TargetConfigObjectKind `json:"kind"`
	Reference string                 `json:"reference"`
	VersionID string                 `json:"versionId,omitempty"`
	MediaType string                 `json:"mediaType"`
	SizeBytes int64                  `json:"sizeBytes"`
	Checksum  string                 `json:"checksum"`
}

type TargetConfigSnapshotComponentDraft struct {
	PhysicalName        string    `json:"physicalName"`
	ComponentInstanceID uuid.UUID `json:"componentInstanceId"`
	DeploymentUnitID    uuid.UUID `json:"deploymentUnitId"`
}

type TargetConfigSnapshotSecretReferenceDraft struct {
	Key                string `json:"key"`
	Provider           string `json:"provider"`
	Reference          string `json:"reference"`
	VersionFingerprint string `json:"versionFingerprint"`
}

type TargetConfigSnapshotFeatureFlagDraft struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}

type TargetConfigSnapshotDraft struct {
	OrganizationID                uuid.UUID `json:"-"`
	CreatedByUserAccountID        uuid.UUID `json:"-"`
	DeploymentUnitID              uuid.UUID `json:"deploymentUnitId"`
	TargetEnvironmentAssignmentID uuid.UUID `json:"targetEnvironmentAssignmentId"`
	EnvironmentID                 uuid.UUID `json:"environmentId"`

	SourceRepository   string            `json:"sourceRepository"`
	SourceCommit       string            `json:"sourceCommit"`
	SourceAdapter      string            `json:"sourceAdapter"`
	AdapterVersion     string            `json:"adapterVersion"`
	TargetPlatform     string            `json:"targetPlatform"`
	RuntimeConstraints map[string]string `json:"runtimeConstraints"`

	Objects          []TargetConfigSnapshotObjectDraft          `json:"objects"`
	Components       []TargetConfigSnapshotComponentDraft       `json:"components"`
	SecretReferences []TargetConfigSnapshotSecretReferenceDraft `json:"secretReferences"`
	FeatureFlags     []TargetConfigSnapshotFeatureFlagDraft     `json:"featureFlags"`
}

type TargetConfigSnapshot struct {
	ID                     uuid.UUID `db:"id" json:"id"`
	CreatedAt              time.Time `db:"created_at" json:"createdAt"`
	CreatedByUserAccountID uuid.UUID `db:"created_by_user_account_id" json:"createdByUserAccountId"`

	OrganizationID                uuid.UUID `db:"organization_id" json:"organizationId"`
	DeploymentUnitID              uuid.UUID `db:"deployment_unit_id" json:"deploymentUnitId"`
	TargetEnvironmentAssignmentID uuid.UUID `db:"target_environment_assignment_id" json:"targetEnvironmentAssignmentId"`
	EnvironmentID                 uuid.UUID `db:"environment_id" json:"environmentId"`

	SourceRepository   string          `db:"source_repository" json:"sourceRepository"`
	SourceCommit       string          `db:"source_commit" json:"sourceCommit"`
	SourceAdapter      string          `db:"source_adapter" json:"sourceAdapter"`
	AdapterVersion     string          `db:"adapter_version" json:"adapterVersion"`
	TargetPlatform     string          `db:"target_platform" json:"targetPlatform"`
	RuntimeConstraints json.RawMessage `db:"runtime_constraints" json:"runtimeConstraints"`
	Schema             string          `db:"schema" json:"-"`
	CanonicalPayload   []byte          `db:"canonical_payload" json:"canonicalPayload"`
	CanonicalChecksum  string          `db:"canonical_checksum" json:"canonicalChecksum"`

	Objects          []TargetConfigSnapshotObject          `json:"objects"`
	Components       []TargetConfigSnapshotComponent       `json:"components"`
	SecretReferences []TargetConfigSnapshotSecretReference `json:"secretReferences"`
	FeatureFlags     []TargetConfigSnapshotFeatureFlag     `json:"featureFlags"`
}

type TargetConfigSnapshotObject struct {
	ID                     uuid.UUID              `db:"id" json:"id"`
	TargetConfigSnapshotID uuid.UUID              `db:"target_config_snapshot_id" json:"targetConfigSnapshotId"`
	OrganizationID         uuid.UUID              `db:"organization_id" json:"organizationId"`
	Key                    string                 `db:"key" json:"key"`
	Kind                   TargetConfigObjectKind `db:"kind" json:"kind"`
	Reference              string                 `db:"reference" json:"reference"`
	VersionID              string                 `db:"version_id" json:"versionId,omitempty"`
	MediaType              string                 `db:"media_type" json:"mediaType"`
	SizeBytes              int64                  `db:"size_bytes" json:"sizeBytes"`
	Checksum               string                 `db:"checksum" json:"checksum"`
}

type TargetConfigSnapshotComponent struct {
	ID                     uuid.UUID `db:"id" json:"id"`
	TargetConfigSnapshotID uuid.UUID `db:"target_config_snapshot_id" json:"targetConfigSnapshotId"`
	OrganizationID         uuid.UUID `db:"organization_id" json:"organizationId"`
	DeploymentUnitID       uuid.UUID `db:"deployment_unit_id" json:"deploymentUnitId"`
	ComponentInstanceID    uuid.UUID `db:"component_instance_id" json:"componentInstanceId"`
	PhysicalName           string    `db:"physical_name" json:"physicalName"`
}

type TargetConfigSnapshotSecretReference struct {
	ID                     uuid.UUID `db:"id" json:"id"`
	TargetConfigSnapshotID uuid.UUID `db:"target_config_snapshot_id" json:"targetConfigSnapshotId"`
	OrganizationID         uuid.UUID `db:"organization_id" json:"organizationId"`
	Key                    string    `db:"key" json:"key"`
	Provider               string    `db:"provider" json:"provider"`
	Reference              string    `db:"reference" json:"reference"`
	VersionFingerprint     string    `db:"version_fingerprint" json:"versionFingerprint"`
}

type TargetConfigSnapshotFeatureFlag struct {
	ID                     uuid.UUID `db:"id" json:"id"`
	TargetConfigSnapshotID uuid.UUID `db:"target_config_snapshot_id" json:"targetConfigSnapshotId"`
	OrganizationID         uuid.UUID `db:"organization_id" json:"organizationId"`
	Key                    string    `db:"key" json:"key"`
	Enabled                bool      `db:"enabled" json:"enabled"`
}

type TargetConfigListFilter struct {
	OrganizationID                uuid.UUID
	DeploymentUnitID              *uuid.UUID
	TargetEnvironmentAssignmentID *uuid.UUID
	Cursor                        string
	Limit                         int
}

type VerifiedTargetConfigObject struct {
	Reference string
	VersionID string
	MediaType string
	SizeBytes int64
	Checksum  string
}

type ObjectVerificationFact struct {
	Key               string `json:"key"`
	Verified          bool   `json:"verified"`
	Code              string `json:"code"`
	Message           string `json:"message"`
	ObservedVersionID string `json:"observedVersionId,omitempty"`
	ObservedMediaType string `json:"observedMediaType,omitempty"`
	ObservedSizeBytes *int64 `json:"observedSizeBytes,omitempty"`
	ObservedChecksum  string `json:"observedChecksum,omitempty"`
}

type ObjectVerificationResult struct {
	SnapshotID uuid.UUID                `json:"snapshotId"`
	Verified   bool                     `json:"verified"`
	Objects    []ObjectVerificationFact `json:"objects"`
}

type V1ExtractionStatus string

const (
	V1ExtractionStatusCandidate V1ExtractionStatus = "candidate"
	V1ExtractionStatusApplied   V1ExtractionStatus = "applied"
	V1ExtractionStatusBlocked   V1ExtractionStatus = "blocked"
)

type V1ExtractionCheckpoint struct {
	ID                           uuid.UUID  `db:"id" json:"id"`
	CreatedAt                    time.Time  `db:"created_at" json:"createdAt"`
	OrganizationID               uuid.UUID  `db:"organization_id" json:"organizationId"`
	ActorUserAccountID           uuid.UUID  `db:"actor_user_account_id" json:"actorUserAccountId"`
	ExtractorVersion             string     `db:"extractor_version" json:"extractorVersion"`
	SourceStateChecksum          string     `db:"source_state_checksum" json:"sourceStateChecksum"`
	DryRunChecksum               string     `db:"dry_run_checksum" json:"dryRunChecksum"`
	PredecessorCheckpointID      *uuid.UUID `db:"predecessor_checkpoint_id" json:"predecessorCheckpointId,omitempty"`            //nolint:lll
	SourceMembershipCheckpointID *uuid.UUID `db:"source_membership_checkpoint_id" json:"sourceMembershipCheckpointId,omitempty"` //nolint:lll
	SourceMembershipChecksum     string     `db:"source_membership_checksum" json:"sourceMembershipChecksum"`
	SourceAfterCreatedAt         *time.Time `db:"source_after_created_at" json:"sourceAfterCreatedAt,omitempty"`
	SourceAfterPlanID            *uuid.UUID `db:"source_after_plan_id" json:"sourceAfterPlanId,omitempty"`
	SourceThroughCreatedAt       *time.Time `db:"source_through_created_at" json:"sourceThroughCreatedAt,omitempty"`
	SourceThroughPlanID          *uuid.UUID `db:"source_through_plan_id" json:"sourceThroughPlanId,omitempty"`
	SourceHighWaterCreatedAt     *time.Time `db:"source_high_water_created_at" json:"sourceHighWaterCreatedAt,omitempty"` //nolint:lll
	SourceHighWaterPlanID        *uuid.UUID `db:"source_high_water_plan_id" json:"sourceHighWaterPlanId,omitempty"`       //nolint:lll
	HasMore                      bool       `db:"has_more" json:"hasMore"`
	SourceCount                  int        `db:"source_count" json:"sourceCount"`
	CandidateCount               int        `db:"candidate_count" json:"candidateCount"`
	BlockedCount                 int        `db:"blocked_count" json:"blockedCount"`
	BatchSize                    int        `db:"batch_size" json:"batchSize"`
}

type V1ExtractionLineage struct {
	ID                      uuid.UUID          `db:"id" json:"id"`
	CreatedAt               time.Time          `db:"created_at" json:"createdAt"`
	OrganizationID          uuid.UUID          `db:"organization_id" json:"organizationId"`
	CheckpointID            uuid.UUID          `db:"checkpoint_id" json:"checkpointId"`
	OriginalReleaseBundleID uuid.UUID          `db:"original_release_bundle_id" json:"originalReleaseBundleId"`
	OriginalReleaseChecksum string             `db:"original_release_checksum" json:"originalReleaseChecksum"`
	OriginalPlanID          uuid.UUID          `db:"original_plan_id" json:"originalPlanId"`
	OriginalPlanChecksum    string             `db:"original_plan_checksum" json:"originalPlanChecksum"`
	DerivedSnapshotID       *uuid.UUID         `db:"derived_snapshot_id" json:"derivedSnapshotId,omitempty"`
	DerivedSnapshotChecksum string             `db:"derived_snapshot_checksum" json:"derivedSnapshotChecksum,omitempty"`
	ExtractorVersion        string             `db:"extractor_version" json:"extractorVersion"`
	Status                  V1ExtractionStatus `db:"status" json:"status"`
	BlockedReasonCode       string             `db:"blocked_reason_code" json:"blockedReasonCode,omitempty"`
}

type V1ExtractionReport struct {
	Checkpoint V1ExtractionCheckpoint `json:"checkpoint"`
	Items      []V1ExtractionLineage  `json:"items"`
	Applied    int                    `json:"applied"`
	Pending    int                    `json:"pending"`
	Blocked    int                    `json:"blocked"`
	NoOp       int                    `json:"noOp"`
}
