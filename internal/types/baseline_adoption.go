package types

import (
	"time"

	"github.com/google/uuid"
)

type BaselineAdoptionStatus string

const BaselineAdoptionStatusAdopted BaselineAdoptionStatus = "ADOPTED"

type BaselineAdoptionHealthEvidenceKind string

const (
	BaselineAdoptionHealthStandardReadiness BaselineAdoptionHealthEvidenceKind = "STANDARD_READINESS"
	BaselineAdoptionHealthLegacyLiveness    BaselineAdoptionHealthEvidenceKind = "LEGACY_LIVENESS_ONLY"
)

type BaselineAdoptionHealthEvidenceUse string

const (
	BaselineAdoptionHealthUsePromotionEligible BaselineAdoptionHealthEvidenceUse = "STANDARD_PROMOTION_ELIGIBLE"
	BaselineAdoptionHealthUseBaselineRollback  BaselineAdoptionHealthEvidenceUse = "BASELINE_OR_ROLLBACK_ONLY"
)

type CreateBaselineAdoptionInput struct {
	OrganizationID                 uuid.UUID
	DeploymentPlanID               uuid.UUID
	ActorUserAccountID             uuid.UUID
	IdempotencyKey                 string
	Reason                         string
	ExpectedPlanChecksum           string
	ExpectedProductReleaseChecksum string
	ExpectedTargetConfigChecksum   string
	Components                     []BaselineAdoptionComponentInput
}

type BaselineAdoptionComponentInput struct {
	ComponentInstanceID             uuid.UUID `json:"componentInstanceId"`
	ComponentKey                    string    `json:"componentKey"`
	ComponentReleaseID              uuid.UUID `json:"componentReleaseId"`
	ComponentReleaseChecksum        string    `json:"componentReleaseChecksum"`
	SourceCommit                    string    `json:"sourceCommit"`
	BuildID                         string    `json:"buildId"`
	ProvenanceVerificationID        uuid.UUID `json:"provenanceVerificationId"`
	ProvenanceEvidenceDigest        string    `json:"provenanceEvidenceDigest"`
	ProvenancePolicyChecksum        string    `json:"provenancePolicyChecksum"`
	ArtifactDigest                  string    `json:"artifactDigest"`
	Platform                        string    `json:"platform"`
	ConfigChecksum                  string    `json:"configChecksum"`
	SchemaVersion                   string    `json:"schemaVersion"`
	CapabilityChecksum              string    `json:"capabilityChecksum"`
	TopologyChecksum                string    `json:"topologyChecksum"`
	ObservationID                   uuid.UUID `json:"observationId"`
	ObserverID                      uuid.UUID `json:"observerId"`
	ObservationEvidenceChecksum     string    `json:"observationEvidenceChecksum"`
	ObservationStateChecksum        string    `json:"observationStateChecksum"`
	ObservationRuntimeStateChecksum string    `json:"observationRuntimeStateChecksum"`
}

type BaselineAdoption struct {
	ID                     uuid.UUID                   `db:"id" json:"id"`
	CreatedAt              time.Time                   `db:"created_at" json:"createdAt"`
	OrganizationID         uuid.UUID                   `db:"organization_id" json:"organizationId"`
	DeploymentPlanID       uuid.UUID                   `db:"deployment_plan_id" json:"deploymentPlanId"`
	ProductReleaseID       uuid.UUID                   `db:"product_release_id" json:"productReleaseId"`
	TargetConfigSnapshotID uuid.UUID                   `db:"target_config_snapshot_id" json:"targetConfigSnapshotId"`
	DeploymentUnitID       uuid.UUID                   `db:"deployment_unit_id" json:"deploymentUnitId"`
	EnvironmentID          uuid.UUID                   `db:"environment_id" json:"environmentId"`
	DeploymentTargetID     uuid.UUID                   `db:"deployment_target_id" json:"deploymentTargetId"`
	ActorUserAccountID     uuid.UUID                   `db:"actor_user_account_id" json:"actorUserAccountId"`
	AuthorizationAction    string                      `db:"authorization_action" json:"authorizationAction"`
	IdempotencyKey         string                      `db:"idempotency_key" json:"idempotencyKey"`
	Reason                 string                      `db:"reason" json:"reason"`
	PlanChecksum           string                      `db:"plan_checksum" json:"planChecksum"`
	ProductReleaseChecksum string                      `db:"product_release_checksum" json:"productReleaseChecksum"`
	TargetConfigChecksum   string                      `db:"target_config_checksum" json:"targetConfigChecksum"`
	RequestChecksum        string                      `db:"request_checksum" json:"requestChecksum"`
	OutcomeChecksum        string                      `db:"outcome_checksum" json:"outcomeChecksum"`
	Status                 BaselineAdoptionStatus      `db:"status" json:"status"`
	DeploymentPerformed    bool                        `db:"deployment_performed" json:"deploymentPerformed"`
	TaskCount              int                         `db:"task_count" json:"taskCount"`
	LockCount              int                         `db:"lock_count" json:"lockCount"`
	ExecutionCount         int                         `db:"execution_count" json:"executionCount"`
	Components             []BaselineAdoptionComponent `db:"-" json:"components"`
}

type BaselineAdoptionComponent struct {
	ID                              uuid.UUID                          `db:"id" json:"id"`
	CreatedAt                       time.Time                          `db:"created_at" json:"createdAt"`
	OrganizationID                  uuid.UUID                          `db:"organization_id" json:"organizationId"`
	BaselineAdoptionID              uuid.UUID                          `db:"baseline_adoption_id" json:"baselineAdoptionId"`
	DeploymentPlanID                uuid.UUID                          `db:"deployment_plan_id" json:"deploymentPlanId"`
	DeploymentUnitID                uuid.UUID                          `db:"deployment_unit_id" json:"deploymentUnitId"`
	ComponentInstanceID             uuid.UUID                          `db:"component_instance_id" json:"componentInstanceId"`
	ComponentKey                    string                             `db:"component_key" json:"componentKey"`
	ComponentReleaseID              uuid.UUID                          `db:"component_release_id" json:"componentReleaseId"`
	ComponentReleaseChecksum        string                             `db:"component_release_checksum" json:"componentReleaseChecksum"`
	ApplicationVersion              string                             `db:"application_version" json:"applicationVersion"`
	SourceCommit                    string                             `db:"source_commit" json:"sourceCommit"`
	BuildID                         string                             `db:"build_id" json:"buildId"`
	ProvenanceVerificationID        uuid.UUID                          `db:"provenance_verification_id" json:"provenanceVerificationId"`
	ProvenanceEvidenceDigest        string                             `db:"provenance_evidence_digest" json:"provenanceEvidenceDigest"`
	ProvenancePolicyChecksum        string                             `db:"provenance_policy_checksum" json:"provenancePolicyChecksum"`
	ArtifactDigest                  string                             `db:"artifact_digest" json:"artifactDigest"`
	Platform                        string                             `db:"platform" json:"platform"`
	TargetConfigSnapshotID          uuid.UUID                          `db:"target_config_snapshot_id" json:"targetConfigSnapshotId"`
	ConfigChecksum                  string                             `db:"config_checksum" json:"configChecksum"`
	SchemaVersion                   string                             `db:"schema_version" json:"schemaVersion"`
	CapabilityChecksum              string                             `db:"capability_checksum" json:"capabilityChecksum"`
	TopologyChecksum                string                             `db:"topology_checksum" json:"topologyChecksum"`
	ObservationID                   uuid.UUID                          `db:"observation_id" json:"observationId"`
	ObserverID                      uuid.UUID                          `db:"observer_id" json:"observerId"`
	ObservationEvidenceChecksum     string                             `db:"observation_evidence_checksum" json:"observationEvidenceChecksum"`
	ObservationEvidenceReference    string                             `db:"observation_evidence_reference" json:"observationEvidenceReference,omitempty"`
	ObservationStateChecksum        string                             `db:"observation_state_checksum" json:"observationStateChecksum"`
	ObservationRuntimeStateChecksum string                             `db:"observation_runtime_state_checksum" json:"observationRuntimeStateChecksum"`
	HealthEvidenceKind              BaselineAdoptionHealthEvidenceKind `db:"health_evidence_kind" json:"healthEvidenceKind"`
	HealthEvidenceUse               BaselineAdoptionHealthEvidenceUse  `db:"health_evidence_use" json:"healthEvidenceUse"`
	HealthPolicyChecksum            string                             `db:"health_policy_checksum" json:"healthPolicyChecksum"`
	ObservationCapturedAt           time.Time                          `db:"observation_captured_at" json:"observationCapturedAt"`
	ObservationFreshUntil           time.Time                          `db:"observation_fresh_until" json:"observationFreshUntil"`
	ActiveDesiredRevisionID         uuid.UUID                          `db:"active_desired_revision_id" json:"activeDesiredRevisionId"`
	DesiredRevision                 int64                              `db:"desired_revision" json:"desiredRevision"`
}
