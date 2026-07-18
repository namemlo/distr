package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	LegacyDeploymentPlanSchemaV1 = "distr.deployment-plan/v1"
	TargetDeploymentPlanSchemaV2 = "distr.target-deployment-plan/v2"
	DeploymentPlanProtocolV1     = "v1"
	DeploymentPlanProtocolV2     = "v2"
)

type PlanDraft struct {
	ID                            uuid.UUID            `db:"id" json:"id"`
	CreatedAt                     time.Time            `db:"created_at" json:"createdAt"`
	UpdatedAt                     time.Time            `db:"updated_at" json:"updatedAt"`
	OrganizationID                uuid.UUID            `db:"organization_id" json:"organizationId"`
	Revision                      int64                `db:"revision" json:"revision"`
	ProductReleaseID              uuid.UUID            `db:"product_release_id" json:"productReleaseId"`
	DeploymentUnitID              uuid.UUID            `db:"deployment_unit_id" json:"deploymentUnitId"`
	EnvironmentAssignmentID       uuid.UUID            `db:"environment_assignment_id" json:"environmentAssignmentId"`
	TargetConfigSnapshotID        uuid.UUID            `db:"target_config_snapshot_id" json:"targetConfigSnapshotId"`
	ProtocolVersion               string               `db:"protocol_version" json:"protocolVersion"`
	SupersedesDeploymentPlanID    *uuid.UUID           `db:"supersedes_deployment_plan_id" json:"supersedesDeploymentPlanId,omitempty"`
	SupersedeReason               string               `db:"supersede_reason" json:"supersedeReason,omitempty"`
	ExpectedPreviewChecksum       string               `db:"-" json:"expectedPreviewChecksum,omitempty"`
	PreviewChecksum               string               `db:"preview_checksum" json:"previewChecksum"`
	PreviewPayload                []byte               `db:"preview_payload" json:"-"`
	PublishedDeploymentPlanID     *uuid.UUID           `db:"-" json:"publishedDeploymentPlanId,omitempty"`
	PublishedDeploymentPlanStatus string               `db:"-" json:"publishedDeploymentPlanStatus,omitempty"`
	ResolutionInput               *PlanResolutionInput `db:"-" json:"-"`
}

type PlanDraftValidation struct {
	Draft           PlanDraft               `json:"draft"`
	Resolutions     []RequirementResolution `json:"resolutions"`
	Graph           TargetPlanGraph         `json:"graph"`
	Issues          []ValidationIssue       `json:"issues"`
	PreviewChecksum string                  `json:"previewChecksum"`
}

type PlanResolutionInput struct {
	EffectiveAt       time.Time                     `json:"effectiveAt"`
	Assignment        TargetEnvironmentAssignment   `json:"assignment"`
	ActiveAssignments []TargetEnvironmentAssignment `json:"activeAssignments"`
	Unit              DeploymentUnit                `json:"unit"`
	ActiveUnits       []DeploymentUnit              `json:"activeUnits"`
	Scope             DeploymentScope               `json:"scope"`
	TargetPlatform    DeploymentTargetPlatform      `json:"targetPlatform"`

	ProductReleaseID  uuid.UUID   `json:"productReleaseId"`
	ProductChecksum   string      `json:"productChecksum"`
	ProductPublished  bool        `json:"productPublished"`
	RequiredPlatforms []string    `json:"requiredPlatforms"`
	ProductEdges      []GraphEdge `json:"productEdges"`

	Config             TargetConfigBinding            `json:"config"`
	Requirements       []TargetRequirement            `json:"requirements"`
	Candidates         []RequirementProviderCandidate `json:"candidates"`
	ReleasePins        []ComponentReleasePin          `json:"releasePins"`
	ComponentInstances []ComponentInstance            `json:"componentInstances"`
}

type TargetConfigBinding struct {
	ID                      uuid.UUID                `json:"id"`
	OrganizationID          uuid.UUID                `json:"organizationId"`
	DeploymentUnitID        uuid.UUID                `json:"deploymentUnitId"`
	EnvironmentAssignmentID uuid.UUID                `json:"environmentAssignmentId"`
	EnvironmentID           uuid.UUID                `json:"environmentId"`
	CanonicalChecksum       string                   `json:"canonicalChecksum"`
	TargetPlatform          string                   `json:"targetPlatform"`
	VerificationFacts       []ConfigVerificationFact `json:"verificationFacts"`
	ComponentBindings       []ConfigComponentBinding `json:"componentBindings"`
	FeatureFlags            map[string]bool          `json:"featureFlags"`
}

type ConfigVerificationFact struct {
	ObjectKey        string `json:"objectKey"`
	Checksum         string `json:"checksum"`
	ObservedChecksum string `json:"observedChecksum,omitempty"`
	Verified         bool   `json:"verified"`
}

type ConfigComponentBinding struct {
	ComponentKey        string    `json:"componentKey"`
	ComponentInstanceID uuid.UUID `json:"componentInstanceId"`
	PhysicalName        string    `json:"physicalName"`
}

type ComponentReleasePin struct {
	ComponentKey              string                    `json:"componentKey"`
	ComponentReleaseID        uuid.UUID                 `json:"componentReleaseId"`
	ReleaseChecksum           string                    `json:"releaseChecksum"`
	Version                   string                    `json:"version,omitempty"`
	Platforms                 []string                  `json:"platforms"`
	PlatformDigest            string                    `json:"platformDigest,omitempty"`
	Artifacts                 []PinnedReleaseArtifact   `json:"artifacts"`
	ProvenanceVerified        bool                      `json:"provenanceVerified"`
	ProvenanceBindingChecksum string                    `json:"provenanceBindingChecksum"`
	ProvenanceFacts           []ComponentProvenanceFact `json:"provenanceFacts"`
	Migrations                []MigrationDeclaration    `json:"migrations"`
}

type ComponentProvenanceFact struct {
	VerificationID uuid.UUID `json:"verificationId"`
	ArtifactKey    string    `json:"artifactKey"`
	Platform       string    `json:"platform"`
	ArtifactDigest string    `json:"artifactDigest"`
	EvidenceDigest string    `json:"evidenceDigest"`
	PolicyChecksum string    `json:"policyChecksum"`
	TrustRootID    string    `json:"trustRootId"`
}

type PinnedReleaseArtifact struct {
	Key            string `json:"key"`
	Type           string `json:"type"`
	MediaType      string `json:"mediaType"`
	ManifestDigest string `json:"manifestDigest"`
	Platform       string `json:"platform"`
	PlatformDigest string `json:"platformDigest"`
}

type TargetRequirement struct {
	Key          string                      `json:"key"`
	ConsumerKey  string                      `json:"consumerKey"`
	Capability   string                      `json:"capability"`
	VersionRange string                      `json:"versionRange"`
	AllowedModes []RequirementResolutionMode `json:"allowedModes"`
}

type RequirementProviderCandidate struct {
	RequirementKey            string                    `json:"requirementKey"`
	Mode                      RequirementResolutionMode `json:"mode"`
	ProviderReleaseID         *uuid.UUID                `json:"providerReleaseId,omitempty"`
	ObservationID             *uuid.UUID                `json:"observationId,omitempty"`
	ProviderVersion           string                    `json:"providerVersion"`
	ProviderPlatform          string                    `json:"providerPlatform"`
	ProviderReleaseChecksum   string                    `json:"providerReleaseChecksum,omitempty"`
	ProvenanceBindingChecksum string                    `json:"provenanceBindingChecksum,omitempty"`
	DeploymentUnitID          uuid.UUID                 `json:"deploymentUnitId,omitempty"`
	ComponentInstanceID       *uuid.UUID                `json:"componentInstanceId,omitempty"`
	SubscriberSetChecksum     string                    `json:"subscriberSetChecksum,omitempty"`
	ExpectedStateVersion      int64                     `json:"expectedStateVersion"`
	ExpectedStateChecksum     string                    `json:"expectedStateChecksum"`
	ObservedStateVersion      int64                     `json:"observedStateVersion"`
	ObservedStateChecksum     string                    `json:"observedStateChecksum"`
	ProvenanceVerified        bool                      `json:"provenanceVerified"`
	FeatureFlagKey            string                    `json:"featureFlagKey,omitempty"`
	FeatureFlagEnabled        bool                      `json:"featureFlagEnabled"`
	V1Compatible              bool                      `json:"v1Compatible"`
}

type RequirementResolution struct {
	ID                        uuid.UUID                 `db:"id" json:"id,omitempty"`
	DeploymentPlanID          uuid.UUID                 `db:"deployment_plan_id" json:"deploymentPlanId,omitempty"`
	OrganizationID            uuid.UUID                 `db:"organization_id" json:"organizationId,omitempty"`
	RequirementKey            string                    `db:"requirement_key" json:"requirementKey"`
	ConsumerKey               string                    `db:"consumer_key" json:"consumerKey"`
	Capability                string                    `db:"capability" json:"capability"`
	VersionRange              string                    `db:"version_range" json:"versionRange"`
	Mode                      RequirementResolutionMode `db:"mode" json:"mode"`
	ProviderReleaseID         *uuid.UUID                `db:"provider_release_id" json:"providerReleaseId,omitempty"`
	ObservationID             *uuid.UUID                `db:"observation_id" json:"observationId,omitempty"`
	ProviderVersion           string                    `db:"provider_version" json:"providerVersion"`
	ProviderPlatform          string                    `db:"provider_platform" json:"providerPlatform"`
	ProviderReleaseChecksum   string                    `db:"provider_release_checksum" json:"providerReleaseChecksum,omitempty"`
	ProvenanceBindingChecksum string                    `db:"provenance_binding_checksum" json:"provenanceBindingChecksum,omitempty"`
	ProviderDeploymentUnitID  *uuid.UUID                `db:"provider_deployment_unit_id" json:"providerDeploymentUnitId,omitempty"`
	ComponentInstanceID       *uuid.UUID                `db:"component_instance_id" json:"componentInstanceId,omitempty"`
	SubscriberSetChecksum     string                    `db:"subscriber_set_checksum" json:"subscriberSetChecksum,omitempty"`
	ExpectedStateVersion      int64                     `db:"expected_state_version" json:"expectedStateVersion"`
	ExpectedStateChecksum     string                    `db:"expected_state_checksum" json:"expectedStateChecksum"`
	BindingChecksum           string                    `db:"binding_checksum" json:"bindingChecksum"`
	SortOrder                 int                       `db:"sort_order" json:"sortOrder"`
	V1Compatible              bool                      `db:"-" json:"v1Compatible"`
}

type TargetPlanGraph struct {
	Steps            []TargetPlanStep         `json:"steps"`
	Edges            []DeploymentPlanStepEdge `json:"edges"`
	TopologicalOrder []string                 `json:"topologicalOrder"`
	Checksum         string                   `json:"checksum"`
}

type TargetPlanStep struct {
	StepKey                string          `json:"stepKey"`
	Name                   string          `json:"name"`
	Kind                   string          `json:"kind"`
	ComponentKey           string          `json:"componentKey,omitempty"`
	ComponentReleaseID     *uuid.UUID      `json:"componentReleaseId,omitempty"`
	ComponentInstanceID    *uuid.UUID      `json:"componentInstanceId,omitempty"`
	ActionType             string          `json:"actionType"`
	ActionName             string          `json:"actionName"`
	ExecutionLocation      string          `json:"executionLocation"`
	InputBindings          json.RawMessage `json:"inputBindings"`
	TargetLockKey          string          `json:"targetLockKey"`
	DatabaseLockKey        string          `json:"databaseLockKey,omitempty"`
	TimeoutSeconds         int             `json:"timeoutSeconds"`
	RetryClass             string          `json:"retryClass"`
	CancellationBehavior   string          `json:"cancellationBehavior"`
	ExpectedInputChecksum  string          `json:"expectedInputChecksum"`
	ObservationRequirement string          `json:"observationRequirement"`
	V1Compatible           bool            `json:"v1Compatible"`
	SortOrder              int             `json:"sortOrder"`
}

type DeploymentPlanStepEdge struct {
	ID               uuid.UUID `db:"id" json:"id,omitempty"`
	DeploymentPlanID uuid.UUID `db:"deployment_plan_id" json:"deploymentPlanId,omitempty"`
	OrganizationID   uuid.UUID `db:"organization_id" json:"organizationId,omitempty"`
	Key              string    `db:"edge_key" json:"key"`
	FromStepKey      string    `db:"from_step_key" json:"fromStepKey"`
	ToStepKey        string    `db:"to_step_key" json:"toStepKey"`
}

type TargetDeploymentPlanCanonical struct {
	Schema                       string                   `json:"schema"`
	ProductReleaseID             uuid.UUID                `json:"productReleaseId"`
	ProductReleaseChecksum       string                   `json:"productReleaseChecksum"`
	DeploymentUnitID             uuid.UUID                `json:"deploymentUnitId"`
	DeploymentScopeID            uuid.UUID                `json:"deploymentScopeId,omitempty"`
	SubscriberSetChecksum        string                   `json:"subscriberSetChecksum"`
	EnvironmentAssignmentID      uuid.UUID                `json:"environmentAssignmentId"`
	EnvironmentID                uuid.UUID                `json:"environmentId"`
	DeploymentTargetID           uuid.UUID                `json:"deploymentTargetId"`
	TargetConfigSnapshotID       uuid.UUID                `json:"targetConfigSnapshotId"`
	TargetConfigSnapshotChecksum string                   `json:"targetConfigSnapshotChecksum"`
	TargetPlatform               string                   `json:"targetPlatform"`
	ConfigVerificationFacts      []ConfigVerificationFact `json:"configVerificationFacts"`
	ComponentReleasePins         []ComponentReleasePin    `json:"componentReleasePins"`
	ComponentBindings            []ConfigComponentBinding `json:"componentBindings"`
	RequirementResolutions       []RequirementResolution  `json:"requirementResolutions"`
	Graph                        TargetPlanGraph          `json:"graph"`
	ProtocolVersion              string                   `json:"protocolVersion"`
	SupersedesDeploymentPlanID   *uuid.UUID               `json:"supersedesDeploymentPlanId,omitempty"`
	SupersedeReason              string                   `json:"supersedeReason,omitempty"`
}
