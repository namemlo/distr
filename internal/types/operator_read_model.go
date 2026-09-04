package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	OperatorDefaultPageLimit = 50
	OperatorMaximumPageLimit = 100
)

type OperatorCollection string

const (
	OperatorCollectionFleet          OperatorCollection = "fleet"
	OperatorCollectionReleases       OperatorCollection = "releases"
	OperatorCollectionPlans          OperatorCollection = "plans"
	OperatorCollectionCampaigns      OperatorCollection = "campaigns"
	OperatorCollectionExecutions     OperatorCollection = "executions"
	OperatorCollectionReconciliation OperatorCollection = "reconciliation"
	OperatorCollectionAudit          OperatorCollection = "audit"
)

func (collection OperatorCollection) Valid() bool {
	switch collection {
	case OperatorCollectionFleet,
		OperatorCollectionReleases,
		OperatorCollectionPlans,
		OperatorCollectionCampaigns,
		OperatorCollectionExecutions,
		OperatorCollectionReconciliation,
		OperatorCollectionAudit:
		return true
	default:
		return false
	}
}

type PageRequest struct {
	Cursor string
	Limit  int
}

type OperatorPage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
	Total      *int64 `json:"total,omitempty"`
}

type OperatorScopeFilter struct {
	OrganizationID    uuid.UUID   `json:"organizationId"`
	DecisionAt        time.Time   `json:"decisionAt"`
	OrganizationWide  bool        `json:"organizationWide"`
	CustomerIDs       []uuid.UUID `json:"customerIds"`
	EnvironmentIDs    []uuid.UUID `json:"environmentIds"`
	DeploymentUnitIDs []uuid.UUID `json:"deploymentUnitIds"`
	ComponentIDs      []uuid.UUID `json:"componentIds"`
	CampaignIDs       []uuid.UUID `json:"campaignIds"`
}

type FleetFilter struct {
	OperatorScopeFilter
	CustomerOrganizationID *uuid.UUID `json:"customerOrganizationId,omitempty"`
	EnvironmentID          *uuid.UUID `json:"environmentId,omitempty"`
	DeploymentTargetID     *uuid.UUID `json:"deploymentTargetId,omitempty"`
	DeploymentUnitID       *uuid.UUID `json:"deploymentUnitId,omitempty"`
	Component              string     `json:"component,omitempty"`
	ObservedState          string     `json:"observedState,omitempty"`
	Drift                  string     `json:"drift,omitempty"`
	Enrollment             string     `json:"enrollment,omitempty"`
	Search                 string     `json:"search,omitempty"`
}

type ReleaseFilter struct {
	OperatorScopeFilter
	CustomerOrganizationID *uuid.UUID `json:"customerOrganizationId,omitempty"`
	ApplicationID          *uuid.UUID `json:"applicationId,omitempty"`
	DeploymentUnitID       *uuid.UUID `json:"deploymentUnitId,omitempty"`
	Kind                   string     `json:"kind,omitempty"`
	Status                 string     `json:"status,omitempty"`
	Search                 string     `json:"search,omitempty"`
}

type OperatorPlanFilter struct {
	OperatorScopeFilter
	Status           string     `json:"status,omitempty"`
	EnvironmentID    *uuid.UUID `json:"environmentId,omitempty"`
	DeploymentUnitID *uuid.UUID `json:"deploymentUnitId,omitempty"`
	ProductReleaseID *uuid.UUID `json:"productReleaseId,omitempty"`
}

type CampaignFilter struct {
	OperatorScopeFilter
	Status           string     `json:"status,omitempty"`
	EnvironmentID    *uuid.UUID `json:"environmentId,omitempty"`
	DeploymentPlanID *uuid.UUID `json:"deploymentPlanId,omitempty"`
}

type ExecutionFilter struct {
	OperatorScopeFilter
	Status             string     `json:"status,omitempty"`
	CampaignID         *uuid.UUID `json:"campaignId,omitempty"`
	DeploymentPlanID   *uuid.UUID `json:"deploymentPlanId,omitempty"`
	DeploymentTargetID *uuid.UUID `json:"deploymentTargetId,omitempty"`
	From               *time.Time `json:"from,omitempty"`
	To                 *time.Time `json:"to,omitempty"`
}

type ReconciliationFilter struct {
	OperatorScopeFilter
	Status             string     `json:"status,omitempty"`
	Drift              string     `json:"drift,omitempty"`
	EnvironmentID      *uuid.UUID `json:"environmentId,omitempty"`
	DeploymentTargetID *uuid.UUID `json:"deploymentTargetId,omitempty"`
}

type AuditFilter struct {
	OperatorScopeFilter
	Action             string     `json:"action,omitempty"`
	SubjectType        string     `json:"subjectType,omitempty"`
	SubjectID          *uuid.UUID `json:"subjectId,omitempty"`
	ActorUserAccountID *uuid.UUID `json:"actorUserAccountId,omitempty"`
	From               *time.Time `json:"from,omitempty"`
	To                 *time.Time `json:"to,omitempty"`
	Search             string     `json:"search,omitempty"`
}

type FleetRow struct {
	ID                         uuid.UUID  `db:"id" json:"id"`
	CreatedAt                  time.Time  `db:"created_at" json:"createdAt"`
	CustomerOrganizationID     *uuid.UUID `db:"customer_organization_id" json:"customerOrganizationId,omitempty"`
	Customer                   string     `db:"customer" json:"customer"`
	EnvironmentID              uuid.UUID  `db:"environment_id" json:"environmentId"`
	Environment                string     `db:"environment" json:"environment"`
	DeploymentTargetID         uuid.UUID  `db:"deployment_target_id" json:"deploymentTargetId"`
	Target                     string     `db:"target" json:"target"`
	DeploymentUnitID           uuid.UUID  `db:"deployment_unit_id" json:"deploymentUnitId"`
	Unit                       string     `db:"unit" json:"unit"`
	ComponentID                *uuid.UUID `db:"component_id" json:"componentId,omitempty"`
	Component                  string     `db:"component" json:"component"`
	ActiveReleaseID            *uuid.UUID `db:"active_release_id" json:"activeReleaseId,omitempty"`
	ActiveRelease              string     `db:"active_release" json:"activeRelease"`
	PendingReleaseID           *uuid.UUID `db:"pending_release_id" json:"pendingReleaseId,omitempty"`
	PendingRelease             string     `db:"pending_release" json:"pendingRelease"`
	ObservedState              string     `db:"observed_state" json:"observedState"`
	ObservedEvidenceChecksum   string     `db:"observed_evidence_checksum" json:"observedEvidenceChecksum"`
	ObservedArtifactDigest     string     `db:"observed_artifact_digest" json:"observedArtifactDigest"`
	ObservedConfigChecksum     string     `db:"observed_config_checksum" json:"observedConfigChecksum"`
	ObservedSchemaVersion      string     `db:"observed_schema_version" json:"observedSchemaVersion"`
	ObservedCapabilityChecksum string     `db:"observed_capability_checksum" json:"observedCapabilityChecksum"`
	ObservedPlatform           string     `db:"observed_platform" json:"observedPlatform"`
	ObservedHealth             string     `db:"observed_health" json:"observedHealth"`
	Drift                      string     `db:"drift" json:"drift"`
	LastExecutionID            *uuid.UUID `db:"last_execution_id" json:"lastExecutionId,omitempty"`
	LastExecution              string     `db:"last_execution" json:"lastExecution"`
	Enrollment                 string     `db:"enrollment" json:"enrollment"`
}

type OperatorReleaseRow struct {
	ID             uuid.UUID               `db:"id" json:"id"`
	CreatedAt      time.Time               `db:"created_at" json:"createdAt"`
	Kind           string                  `db:"kind" json:"kind"`
	ApplicationID  uuid.UUID               `db:"application_id" json:"applicationId"`
	Application    string                  `db:"application" json:"application"`
	Clients        []OperatorReleaseClient `db:"clients" json:"clients"`
	ReleaseNumber  *int64                  `db:"release_number" json:"releaseNumber,omitempty"`
	Version        string                  `db:"version" json:"version"`
	Status         string                  `db:"status" json:"status"`
	Checksum       string                  `db:"checksum" json:"checksum"`
	SourceRevision string                  `db:"source_revision" json:"sourceRevision"`
	PublishedAt    *time.Time              `db:"published_at" json:"publishedAt,omitempty"`
	ArtifactCount  int                     `db:"artifact_count" json:"artifactCount"`
	EvidenceCount  int                     `db:"evidence_count" json:"evidenceCount"`
	ComponentCount int                     `db:"component_count" json:"componentCount"`
	GraphEdgeCount int                     `db:"graph_edge_count" json:"graphEdgeCount"`
}

type OperatorReleaseClient struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type OperatorReleaseArtifact struct {
	ID              uuid.UUID         `json:"id"`
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	ManifestDigest  string            `json:"manifestDigest"`
	PlatformDigests map[string]string `json:"platformDigests"`
}

type OperatorReleaseComponentPin struct {
	ComponentReleaseID uuid.UUID `json:"componentReleaseId"`
	Component          string    `json:"component"`
	Version            string    `json:"version"`
	Checksum           string    `json:"checksum"`
	Digest             string    `json:"digest"`
}

type OperatorReleaseGraphEdge struct {
	From              string                                    `json:"from"`
	To                string                                    `json:"to"`
	Kind              string                                    `json:"kind"`
	ConsumerComponent string                                    `json:"consumerComponent"`
	ProviderComponent string                                    `json:"providerComponent,omitempty"`
	Capability        string                                    `json:"capability"`
	VersionRange      string                                    `json:"versionRange"`
	ProviderVersion   string                                    `json:"providerVersion,omitempty"`
	ProviderArtifacts []OperatorReleaseProviderArtifactIdentity `json:"providerArtifacts"`
	ResolutionStage   string                                    `json:"resolutionStage"`
	AllowedModes      []string                                  `json:"allowedModes"`
	Ordering          string                                    `json:"ordering,omitempty"`
}

type OperatorReleaseProviderArtifactIdentity struct {
	ArtifactKey    string `json:"artifactKey"`
	ArtifactType   string `json:"artifactType"`
	ManifestDigest string `json:"manifestDigest"`
	Platform       string `json:"platform"`
	PlatformDigest string `json:"platformDigest"`
}

type OperatorReleaseSourceBuildProof struct {
	Component            string `json:"component"`
	Schema               string `json:"schema"`
	DeclaredRepository   string `json:"declaredRepository"`
	DeclaredRequestedRef string `json:"declaredRequestedRef"`
	DeclaredSourceCommit string `json:"declaredSourceCommit"`
	DeclaredBuilderID    string `json:"declaredBuilderId"`
	DeclaredBuildID      string `json:"declaredBuildId"`
	VerifiedSourceURI    string `json:"verifiedSourceUri,omitempty"`
	VerifiedSourceCommit string `json:"verifiedSourceCommit,omitempty"`
	VerifiedBuilderID    string `json:"verifiedBuilderId,omitempty"`
	VerifiedBuildID      string `json:"verifiedBuildId,omitempty"`
	VerifiedBuildType    string `json:"verifiedBuildType,omitempty"`
	ProvenanceReference  string `json:"provenanceReference,omitempty"`
	ProvenanceDigest     string `json:"provenanceDigest,omitempty"`
	VerificationMode     string `json:"verificationMode,omitempty"`
	TrustRootID          string `json:"trustRootId,omitempty"`
	KeyID                string `json:"keyId,omitempty"`
	KeyFingerprint       string `json:"keyFingerprint,omitempty"`
	SBOMReference        string `json:"sbomReference,omitempty"`
	SBOMDigest           string `json:"sbomDigest,omitempty"`
	VerificationState    string `json:"verificationState"`
}

type OperatorReleaseChange struct {
	Category  string `json:"category"`
	Component string `json:"component"`
	Summary   string `json:"summary"`
	Reference string `json:"reference,omitempty"`
}

type OperatorReleaseSkippedRelease struct {
	Component      string    `json:"component"`
	ReleaseID      uuid.UUID `json:"releaseId"`
	Version        string    `json:"version"`
	SourceRevision string    `json:"sourceRevision"`
	Summary        string    `json:"summary"`
}

type OperatorReleaseChangeContextState string

const (
	OperatorReleaseChangeContextReady              OperatorReleaseChangeContextState = "READY"
	OperatorReleaseChangeContextRequired           OperatorReleaseChangeContextState = "CONTEXT_REQUIRED"
	OperatorReleaseChangeContextNotFound           OperatorReleaseChangeContextState = "NOT_FOUND"
	OperatorReleaseChangeContextBaselineUnverified OperatorReleaseChangeContextState = "BASELINE_UNVERIFIED"
	OperatorReleaseChangeContextDivergentHistory   OperatorReleaseChangeContextState = "DIVERGENT_HISTORY"
)

type OperatorReleaseChangeContext struct {
	DeploymentPlanID *uuid.UUID                        `json:"deploymentPlanId,omitempty"`
	DeploymentUnitID *uuid.UUID                        `json:"deploymentUnitId,omitempty"`
	State            OperatorReleaseChangeContextState `json:"state"`
	Message          string                            `json:"message,omitempty"`
}

type OperatorReleaseDetailContext struct {
	DeploymentUnitID *uuid.UUID `json:"deploymentUnitId,omitempty"`
}

type OperatorEvidenceRef struct {
	ID                                uuid.UUID `json:"id"`
	Kind                              string    `json:"kind"`
	Label                             string    `json:"label"`
	Href                              string    `json:"href"`
	Checksum                          string    `json:"checksum"`
	PreExecutionConfigChecksum        string    `json:"preExecutionConfigChecksum,omitempty"`
	PreExecutionServiceConfigChecksum string    `json:"preExecutionServiceConfigChecksum,omitempty"`
	ResultConfigChecksum              string    `json:"resultConfigChecksum,omitempty"`
	ResultServiceConfigChecksum       string    `json:"resultServiceConfigChecksum,omitempty"`
	CreatedAt                         time.Time `json:"createdAt"`
}

type OperatorReleaseDetail struct {
	Release          OperatorReleaseRow                `json:"release"`
	Artifacts        []OperatorReleaseArtifact         `json:"artifacts"`
	ComponentPins    []OperatorReleaseComponentPin     `json:"componentPins"`
	GraphEdges       []OperatorReleaseGraphEdge        `json:"graphEdges"`
	SourceBuildProof []OperatorReleaseSourceBuildProof `json:"sourceBuildProof"`
	Changelog        []OperatorReleaseChange           `json:"changelog"`
	SkippedReleases  []OperatorReleaseSkippedRelease   `json:"skippedReleases"`
	ChangeContext    OperatorReleaseChangeContext      `json:"changeContext"`
	Evidence         []OperatorEvidenceRef             `json:"evidence"`
}

type OperatorReleaseCompareFact struct {
	Component     string `json:"component"`
	Change        string `json:"change"`
	LeftChecksum  string `json:"leftChecksum,omitempty"`
	RightChecksum string `json:"rightChecksum,omitempty"`
	LeftDigest    string `json:"leftDigest,omitempty"`
	RightDigest   string `json:"rightDigest,omitempty"`
}

type OperatorReleaseCompare struct {
	Left    OperatorReleaseRow           `json:"left"`
	Right   OperatorReleaseRow           `json:"right"`
	Changes []OperatorReleaseCompareFact `json:"changes"`
}

type OperatorPlanRow struct {
	ID                     uuid.UUID  `db:"id" json:"id"`
	CreatedAt              time.Time  `db:"created_at" json:"createdAt"`
	Status                 string     `db:"status" json:"status"`
	PlanSchema             string     `db:"plan_schema" json:"planSchema"`
	ProtocolVersion        string     `db:"protocol_version" json:"protocolVersion"`
	ProductReleaseID       uuid.UUID  `db:"product_release_id" json:"productReleaseId"`
	ProductReleaseVersion  string     `db:"product_release_version" json:"productReleaseVersion"`
	EnvironmentID          uuid.UUID  `db:"environment_id" json:"environmentId"`
	Environment            string     `db:"environment" json:"environment"`
	DeploymentUnitID       *uuid.UUID `db:"deployment_unit_id" json:"deploymentUnitId,omitempty"`
	DeploymentUnit         string     `db:"deployment_unit" json:"deploymentUnit"`
	TargetConfigSnapshotID *uuid.UUID `db:"target_config_snapshot_id" json:"targetConfigSnapshotId,omitempty"`
	CanonicalChecksum      string     `db:"canonical_checksum" json:"canonicalChecksum"`
	TargetCount            int        `db:"target_count" json:"targetCount"`
	StepCount              int        `db:"step_count" json:"stepCount"`
	IssueCount             int        `db:"issue_count" json:"issueCount"`
	BlockingIssueCount     int        `db:"blocking_issue_count" json:"blockingIssueCount"`
	ApprovalBlockerCount   int        `db:"approval_blocker_count" json:"approvalBlockerCount"`
	PreflightBlockerCount  int        `db:"preflight_blocker_count" json:"preflightBlockerCount"`
	Bootstrap              bool       `db:"bootstrap" json:"bootstrap"`
}

type OperatorPlanFact struct {
	ID                 *uuid.UUID `json:"id,omitempty"`
	Key                string     `json:"key"`
	Kind               string     `json:"kind,omitempty"`
	Status             string     `json:"status,omitempty"`
	Expected           string     `json:"expected,omitempty"`
	Actual             string     `json:"actual,omitempty"`
	Checksum           string     `json:"checksum,omitempty"`
	Message            string     `json:"message,omitempty"`
	KeyID              string     `json:"keyId,omitempty"`
	ArtifactDigest     string     `json:"artifactDigest,omitempty"`
	ConfigChecksum     string     `json:"configChecksum,omitempty"`
	Platform           string     `json:"platform,omitempty"`
	SchemaVersion      string     `json:"schemaVersion,omitempty"`
	CapabilityChecksum string     `json:"capabilityChecksum,omitempty"`
	Health             string     `json:"health,omitempty"`
	Blocking           bool       `json:"blocking"`
	Order              int        `json:"order"`
}

type OperatorPlanDetail struct {
	Plan                       OperatorPlanRow         `json:"plan"`
	ProductReleaseChecksum     string                  `json:"productReleaseChecksum"`
	TargetConfigChecksum       string                  `json:"targetConfigChecksum"`
	EffectivePolicyChecksum    string                  `json:"effectivePolicyChecksum"`
	SubscriberSetChecksum      string                  `json:"subscriberSetChecksum"`
	GraphChecksum              string                  `json:"graphChecksum"`
	ChangeChecksum             string                  `json:"changeChecksum"`
	BaselineChecksum           string                  `json:"baselineChecksum"`
	ProviderResolutionChecksum string                  `json:"providerResolutionChecksum"`
	MigrationChecksum          string                  `json:"migrationChecksum"`
	RiskChecksum               string                  `json:"riskChecksum"`
	ApprovalChecksum           string                  `json:"approvalChecksum"`
	WindowChecksum             string                  `json:"windowChecksum"`
	AdapterChecksum            string                  `json:"adapterChecksum"`
	IntentChecksum             string                  `json:"intentChecksum"`
	Targets                    []OperatorPlanFact      `json:"targets"`
	Baselines                  []OperatorPlanFact      `json:"baselines"`
	Config                     []OperatorPlanFact      `json:"config"`
	Requirements               []OperatorPlanFact      `json:"requirements"`
	RequirementResolutions     []RequirementResolution `json:"requirementResolutions"`
	Migrations                 []OperatorPlanFact      `json:"migrations"`
	Changes                    []OperatorPlanFact      `json:"changes"`
	Risks                      []OperatorPlanFact      `json:"risks"`
	Approvals                  []OperatorPlanFact      `json:"approvals"`
	Windows                    []OperatorPlanFact      `json:"windows"`
	Adapters                   []OperatorPlanFact      `json:"adapters"`
	Steps                      []OperatorPlanFact      `json:"steps"`
	Edges                      []OperatorPlanFact      `json:"edges"`
	Issues                     []OperatorPlanFact      `json:"issues"`
	IntentBlockers             []OperatorPlanFact      `json:"intentBlockers"`
	Evidence                   []OperatorEvidenceRef   `json:"evidence"`
}

type OperatorPlanCompare struct {
	Left    OperatorPlanRow    `json:"left"`
	Right   OperatorPlanRow    `json:"right"`
	Changes []OperatorPlanFact `json:"changes"`
}

type OperatorCampaignRow struct {
	ID                uuid.UUID  `db:"id" json:"id"`
	CreatedAt         time.Time  `db:"created_at" json:"createdAt"`
	DraftID           uuid.UUID  `db:"draft_id" json:"draftId"`
	RevisionID        *uuid.UUID `db:"revision_id" json:"revisionId,omitempty"`
	RunID             *uuid.UUID `db:"run_id" json:"runId,omitempty"`
	Name              string     `db:"name" json:"name"`
	Status            string     `db:"status" json:"status"`
	CanonicalChecksum string     `db:"canonical_checksum" json:"canonicalChecksum"`
	WaveCount         int        `db:"wave_count" json:"waveCount"`
	MemberCount       int        `db:"member_count" json:"memberCount"`
	PendingCount      int        `db:"pending_count" json:"pendingCount"`
	RunningCount      int        `db:"running_count" json:"runningCount"`
	SucceededCount    int        `db:"succeeded_count" json:"succeededCount"`
	FailedCount       int        `db:"failed_count" json:"failedCount"`
	BlockedCount      int        `db:"blocked_count" json:"blockedCount"`
}

type OperatorCampaignWave struct {
	ID                 uuid.UUID `json:"id"`
	Order              int       `json:"order"`
	Name               string    `json:"name"`
	Status             string    `json:"status"`
	BakeSeconds        int       `json:"bakeSeconds"`
	MaximumConcurrency int       `json:"maximumConcurrency"`
	MemberCount        int       `json:"memberCount"`
	SucceededCount     int       `json:"succeededCount"`
	FailedCount        int       `json:"failedCount"`
}

type OperatorCampaignMember struct {
	ID               uuid.UUID  `json:"id"`
	MemberRunID      *uuid.UUID `json:"memberRunId,omitempty"`
	DeploymentPlanID uuid.UUID  `json:"deploymentPlanId"`
	DeploymentUnitID uuid.UUID  `json:"deploymentUnitId"`
	WaveOrder        int        `json:"waveOrder"`
	MemberOrder      int        `json:"memberOrder"`
	Status           string     `json:"status"`
	PlanChecksum     string     `json:"planChecksum"`
}

type OperatorCampaignCoordinationSummary struct {
	AdmissionsBlocked     bool       `json:"admissionsBlocked"`
	PausePending          bool       `json:"pausePending"`
	NoNewExposure         bool       `json:"noNewExposure"`
	InFlightMemberCount   int        `json:"inFlightMemberCount"`
	ReconciliationNeeded  bool       `json:"reconciliationRequired"`
	SchedulerFence        int64      `json:"schedulerFenceGeneration"`
	SchedulerLeaseStatus  string     `json:"schedulerLeaseStatus"`
	SchedulerLeaseExpires *time.Time `json:"schedulerLeaseExpiresAt,omitempty"`
	ActiveLockCount       int        `json:"activeLockCount"`
	UnreleasedLockCount   int        `json:"unreleasedLockCount"`
	ActiveLeaseCount      int        `json:"activeLeaseCount"`
	UnreleasedLeaseCount  int        `json:"unreleasedLeaseCount"`
	ZeroLockClosure       bool       `json:"zeroLockClosure"`
}

type OperatorCampaignDetail struct {
	Campaign             OperatorCampaignRow                 `json:"campaign"`
	RunVersion           *int64                              `json:"runVersion,omitempty"`
	RevisionChecksum     string                              `json:"revisionChecksum"`
	MembershipChecksum   string                              `json:"membershipChecksum"`
	PrerequisiteChecksum string                              `json:"prerequisiteChecksum"`
	ThresholdChecksum    string                              `json:"thresholdChecksum"`
	ControlChecksum      string                              `json:"controlChecksum"`
	AdmissionChecksum    string                              `json:"admissionChecksum"`
	Waves                []OperatorCampaignWave              `json:"waves"`
	Members              []OperatorCampaignMember            `json:"members"`
	Prerequisites        []OperatorPlanFact                  `json:"prerequisites"`
	Thresholds           []OperatorPlanFact                  `json:"thresholds"`
	Controls             []OperatorPlanFact                  `json:"controls"`
	UncertaintyBlockers  []OperatorPlanFact                  `json:"uncertaintyBlockers"`
	AdmissionBlockers    []OperatorPlanFact                  `json:"admissionBlockers"`
	Coordination         OperatorCampaignCoordinationSummary `json:"coordination"`
	Evidence             []OperatorEvidenceRef               `json:"evidence"`
}

type OperatorExecutionRow struct {
	ID                                   uuid.UUID  `db:"id" json:"id"`
	CreatedAt                            time.Time  `db:"created_at" json:"createdAt"`
	CampaignID                           *uuid.UUID `db:"campaign_id" json:"campaignId,omitempty"`
	DeploymentPlanID                     uuid.UUID  `db:"deployment_plan_id" json:"deploymentPlanId"`
	DeploymentTargetID                   uuid.UUID  `db:"deployment_target_id" json:"deploymentTargetId"`
	TaskID                               uuid.UUID  `db:"task_id" json:"taskId"`
	StepRunID                            uuid.UUID  `db:"step_run_id" json:"stepRunId"`
	StepKey                              string     `db:"step_key" json:"stepKey"`
	AttemptNumber                        int        `db:"attempt_number" json:"attemptNumber"`
	ProtocolVersion                      string     `db:"protocol_version" json:"protocolVersion"`
	Status                               string     `db:"status" json:"status"`
	PlanChecksum                         string     `db:"plan_checksum" json:"planChecksum"`
	ArtifactDigest                       string     `db:"artifact_digest" json:"artifactDigest"`
	ConfigChecksum                       string     `db:"config_checksum" json:"configChecksum"`
	RuntimeManifestChecksum              *string    `db:"runtime_manifest_checksum" json:"runtimeManifestChecksum,omitempty"`
	DesiredServiceConfigChecksum         *string    `db:"desired_service_config_checksum" json:"desiredServiceConfigChecksum,omitempty"`
	ExpectedCurrentConfigChecksum        *string    `db:"expected_current_config_checksum" json:"expectedCurrentConfigChecksum,omitempty"`
	ExpectedCurrentServiceConfigChecksum *string    `db:"expected_current_service_config_checksum" json:"expectedCurrentServiceConfigChecksum,omitempty"`
	AdapterRevision                      string     `db:"adapter_revision" json:"adapterRevision"`
	CompletedAt                          *time.Time `db:"completed_at" json:"completedAt,omitempty"`
	Cancellable                          bool       `db:"cancellable" json:"cancellable"`
	Reconciliation                       string     `db:"reconciliation" json:"reconciliation"`
	Observation                          string     `db:"observation" json:"observation"`
	FenceGeneration                      int64      `db:"fence_generation" json:"fenceGeneration,omitempty"`
	FenceResourceKey                     string     `db:"fence_resource_key" json:"fenceResourceKey,omitempty"`
	IdempotencyKey                       string     `db:"idempotency_key" json:"idempotencyKey,omitempty"`
}

type OperatorExecutionAttemptFact struct {
	ID                                   uuid.UUID `json:"id"`
	StepKey                              string    `json:"stepKey"`
	Status                               string    `json:"status"`
	AttemptNumber                        int       `json:"attemptNumber"`
	PlanChecksum                         string    `json:"planChecksum"`
	ArtifactDigest                       string    `json:"artifactDigest"`
	ConfigChecksum                       string    `json:"configChecksum"`
	RuntimeManifestChecksum              *string   `json:"runtimeManifestChecksum,omitempty"`
	DesiredServiceConfigChecksum         *string   `json:"desiredServiceConfigChecksum,omitempty"`
	ExpectedCurrentConfigChecksum        *string   `json:"expectedCurrentConfigChecksum,omitempty"`
	ExpectedCurrentServiceConfigChecksum *string   `json:"expectedCurrentServiceConfigChecksum,omitempty"`
	FenceGeneration                      int64     `json:"fenceGeneration"`
	FenceResourceKey                     string    `json:"fenceResourceKey"`
	IdempotencyKey                       string    `json:"idempotencyKey,omitempty"`
	Message                              string    `json:"message,omitempty"`
	Blocking                             bool      `json:"blocking"`
}

type OperatorExecutionLockFact struct {
	ID                uuid.UUID  `json:"id"`
	ResourceType      string     `json:"resourceType"`
	ResourceKey       string     `json:"resourceKey"`
	ConcurrencyPolicy string     `json:"concurrencyPolicy"`
	Status            string     `json:"status"`
	CreatedAt         time.Time  `json:"createdAt"`
	AcquiredAt        *time.Time `json:"acquiredAt,omitempty"`
	ReleasedAt        *time.Time `json:"releasedAt,omitempty"`
	CurrentConflict   bool       `json:"currentConflict"`
	ReleaseReason     string     `json:"releaseReason,omitempty"`
}

type OperatorExecutionLeaseFact struct {
	ID            uuid.UUID  `json:"id"`
	ExecutorType  string     `json:"executorType"`
	Attempt       int        `json:"attempt"`
	Status        string     `json:"status"`
	LeasedAt      time.Time  `json:"leasedAt"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	HeartbeatAt   time.Time  `json:"heartbeatAt"`
	ReleasedAt    *time.Time `json:"releasedAt,omitempty"`
	ReleaseReason string     `json:"releaseReason,omitempty"`
}

type OperatorExecutionCoordinationSummary struct {
	InFlight             bool       `json:"inFlight"`
	ActiveLockCount      int        `json:"activeLockCount"`
	UnreleasedLockCount  int        `json:"unreleasedLockCount"`
	ActiveLeaseCount     int        `json:"activeLeaseCount"`
	UnreleasedLeaseCount int        `json:"unreleasedLeaseCount"`
	FenceStatus          string     `json:"fenceStatus"`
	FenceGeneration      int64      `json:"fenceGeneration"`
	FenceLeaseExpiresAt  *time.Time `json:"fenceLeaseExpiresAt,omitempty"`
	FenceReleasedAt      *time.Time `json:"fenceReleasedAt,omitempty"`
	TimedOut             bool       `json:"timedOut"`
	ReconciliationNeeded bool       `json:"reconciliationRequired"`
	ZeroLockClosure      bool       `json:"zeroLockClosure"`
}

type OperatorExecutionDetail struct {
	Execution      OperatorExecutionRow                 `json:"execution"`
	Intent         *OperatorPlanFact                    `json:"intent,omitempty"`
	Adapter        *OperatorPlanFact                    `json:"adapter,omitempty"`
	Cancellation   *OperatorPlanFact                    `json:"cancellation,omitempty"`
	Reconciliation *OperatorPlanFact                    `json:"reconciliation,omitempty"`
	PreviousState  *OperatorPlanFact                    `json:"previousState,omitempty"`
	Tasks          []OperatorPlanFact                   `json:"tasks"`
	Steps          []OperatorPlanFact                   `json:"steps"`
	Attempts       []OperatorExecutionAttemptFact       `json:"attempts"`
	Locks          []OperatorExecutionLockFact          `json:"locks"`
	Leases         []OperatorExecutionLeaseFact         `json:"leases"`
	Coordination   OperatorExecutionCoordinationSummary `json:"coordination"`
	Observations   []OperatorPlanFact                   `json:"observations"`
	Evidence       []OperatorEvidenceRef                `json:"evidence"`
}

type OperatorReconciliationRow struct {
	ID                 uuid.UUID  `db:"id" json:"id"`
	CreatedAt          time.Time  `db:"created_at" json:"createdAt"`
	DriftCaseID        uuid.UUID  `db:"drift_case_id" json:"driftCaseId"`
	ExecutionID        *uuid.UUID `db:"execution_id" json:"executionId,omitempty"`
	DeploymentPlanID   *uuid.UUID `db:"deployment_plan_id" json:"deploymentPlanId,omitempty"`
	EnvironmentID      uuid.UUID  `db:"environment_id" json:"environmentId"`
	DeploymentTargetID uuid.UUID  `db:"deployment_target_id" json:"deploymentTargetId"`
	Component          string     `db:"component" json:"component"`
	Drift              string     `db:"drift" json:"drift"`
	Status             string     `db:"status" json:"status"`
	Outcome            string     `db:"outcome" json:"outcome"`
	ObservedAt         *time.Time `db:"observed_at" json:"observedAt,omitempty"`
	EvidenceChecksum   string     `db:"evidence_checksum" json:"evidenceChecksum"`
}

type OperatorReconciliationDetail struct {
	Reconciliation OperatorReconciliationRow `json:"reconciliation"`
	DesiredState   *OperatorPlanFact         `json:"desiredState,omitempty"`
	Observation    *OperatorPlanFact         `json:"observation,omitempty"`
	Decision       *OperatorPlanFact         `json:"decision,omitempty"`
	Evidence       []OperatorEvidenceRef     `json:"evidence"`
}

type OperatorAuditRow struct {
	ID                 uuid.UUID  `db:"id" json:"id"`
	CreatedAt          time.Time  `db:"created_at" json:"createdAt"`
	Sequence           int64      `db:"sequence" json:"sequence"`
	Action             string     `db:"action" json:"action"`
	SubjectType        string     `db:"subject_type" json:"subjectType"`
	SubjectID          uuid.UUID  `db:"subject_id" json:"subjectId"`
	ActorUserAccountID *uuid.UUID `db:"actor_useraccount_id" json:"actorUserAccountId,omitempty"`
	Outcome            string     `db:"outcome" json:"outcome"`
	CorrelationCount   int        `db:"correlation_count" json:"correlationCount"`
	PayloadChecksum    string     `db:"payload_checksum" json:"payloadChecksum"`
}

type OperatorAuditDetail struct {
	Event        OperatorAuditRow      `json:"event"`
	Correlations []AuditCorrelation    `json:"correlations"`
	Payload      json.RawMessage       `json:"payload,omitempty"`
	Evidence     []OperatorEvidenceRef `json:"evidence"`
}
