package types

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
)

type DeploymentPlanStatus string

const (
	DeploymentPlanStatusDraft      DeploymentPlanStatus = "DRAFT"
	DeploymentPlanStatusValidating DeploymentPlanStatus = "VALIDATING"
	DeploymentPlanStatusBlocked    DeploymentPlanStatus = "BLOCKED"
	DeploymentPlanStatusReady      DeploymentPlanStatus = "READY"
	DeploymentPlanStatusExpired    DeploymentPlanStatus = "EXPIRED"
	DeploymentPlanStatusExecuted   DeploymentPlanStatus = "EXECUTED"
)

type DeploymentPlanIssueSeverity string

const (
	DeploymentPlanIssueSeverityBlocker DeploymentPlanIssueSeverity = "blocker"
	DeploymentPlanIssueSeverityWarning DeploymentPlanIssueSeverity = "warning"
)

type CreateDeploymentPlanRequest struct {
	OrganizationID   uuid.UUID
	ReleaseBundleID  uuid.UUID
	EnvironmentID    uuid.UUID
	TargetIDs        []uuid.UUID
	DeploymentUnitID *uuid.UUID
}

type DeploymentPlan struct {
	ID                         uuid.UUID                       `db:"id" json:"id"`
	CreatedAt                  time.Time                       `db:"created_at" json:"createdAt"`
	SealedAt                   *time.Time                      `db:"sealed_at" json:"sealedAt,omitempty"`
	OrganizationID             uuid.UUID                       `db:"organization_id" json:"organizationId"`
	PublishedByUserAccountID   *uuid.UUID                      `db:"published_by_user_account_id" json:"publishedByUserAccountId,omitempty"`
	ApplicationID              uuid.UUID                       `db:"application_id" json:"applicationId"`
	ReleaseBundleID            uuid.UUID                       `db:"release_bundle_id" json:"releaseBundleId"`
	ChannelID                  uuid.UUID                       `db:"channel_id" json:"channelId"`
	EnvironmentID              uuid.UUID                       `db:"environment_id" json:"environmentId"`
	ProcessSnapshotID          *uuid.UUID                      `db:"process_snapshot_id" json:"processSnapshotId,omitempty"`
	VariableSnapshotID         *uuid.UUID                      `db:"variable_snapshot_id" json:"variableSnapshotId,omitempty"`
	ReleaseContract            *ReleaseContract                `db:"release_contract" json:"releaseContract,omitempty"`
	PlanSchema                 string                          `db:"plan_schema" json:"planSchema"`
	DraftID                    *uuid.UUID                      `db:"draft_id" json:"draftId,omitempty"`
	DeploymentUnitID           *uuid.UUID                      `db:"deployment_unit_id" json:"deploymentUnitId,omitempty"`
	EffectivePolicy            *EffectivePolicy                `db:"effective_policy" json:"effectivePolicy,omitempty"`
	EffectivePolicyChecksum    string                          `db:"effective_policy_checksum" json:"effectivePolicyChecksum,omitempty"`
	SubscriberSetChecksum      string                          `db:"subscriber_set_checksum" json:"subscriberSetChecksum,omitempty"`
	TargetConfigSnapshotID     *uuid.UUID                      `db:"target_config_snapshot_id" json:"targetConfigSnapshotId,omitempty"`
	ProtocolVersion            string                          `db:"protocol_version" json:"protocolVersion"`
	SupersedesDeploymentPlanID *uuid.UUID                      `db:"supersedes_deployment_plan_id" json:"supersedesDeploymentPlanId,omitempty"`
	SupersedeReason            string                          `db:"supersede_reason" json:"supersedeReason,omitempty"`
	PreviousStateSourcePlanID  *uuid.UUID                      `db:"previous_state_source_plan_id" json:"previousStateSourcePlanId,omitempty"`
	Status                     DeploymentPlanStatus            `db:"status" json:"status"`
	CanonicalChecksum          string                          `db:"canonical_checksum" json:"canonicalChecksum"`
	CanonicalPayload           []byte                          `db:"canonical_payload" json:"-"`
	Targets                    []DeploymentPlanTarget          `db:"-" json:"targets"`
	TargetComponents           []DeploymentPlanTargetComponent `db:"-" json:"targetComponents"`
	PreflightRuns              []DeploymentPreflightRun        `db:"-" json:"preflightRuns"`
	Steps                      []DeploymentPlanStep            `db:"-" json:"steps"`
	Variables                  []DeploymentPlanVariable        `db:"-" json:"variables"`
	Issues                     []DeploymentPlanIssue           `db:"-" json:"issues"`
	ResolvedRequirements       []RequirementResolution         `db:"-" json:"resolvedRequirements,omitempty"`
	StepEdges                  []DeploymentPlanStepEdge        `db:"-" json:"stepEdges,omitempty"`
	Baselines                  []DeploymentPlanBaseline        `db:"-" json:"baselines,omitempty"`
	Changes                    []DeploymentPlanChangeEntry     `db:"-" json:"changes,omitempty"`
	Risks                      []DeploymentPlanRiskEntry       `db:"-" json:"risks,omitempty"`
	Bootstrap                  bool                            `db:"bootstrap" json:"bootstrap"`
	Migrations                 []DeploymentPlanMigration       `db:"-" json:"migrations,omitempty"`
}

type BaselineProjection string

const (
	BaselineProjectionVerifiedV2 BaselineProjection = "verified_v2"
	BaselineProjectionLegacy     BaselineProjection = "legacy_projection"
	BaselineProjectionBootstrap  BaselineProjection = "bootstrap"
)

type BaselineQuery struct {
	OrganizationID          uuid.UUID
	DeploymentUnitID        uuid.UUID
	ComponentInstanceID     uuid.UUID
	ComponentKey            string
	ExpectedDesiredRevision int64
	ExpectedDesiredChecksum string
	Candidates              []BaselineCandidate
}

type BaselineCandidate struct {
	SourceDeploymentPlanID  *uuid.UUID
	ExternalExecutionID     *uuid.UUID
	ObservationID           uuid.UUID
	ObservedAt              time.Time
	Health                  TargetComponentHealth
	DesiredRevision         int64
	DesiredChecksum         string
	ObservedRevision        int64
	ObservedChecksum        string
	PlanSchema              string
	ProtocolVersion         string
	PlanFactsMatch          bool
	ReleaseBundleID         uuid.UUID
	Version                 string
	Image                   string
	Platform                string
	ConfigSnapshotID        *uuid.UUID
	ConfigChecksum          string
	ProviderBindingChecksum string
	SchemaState             string
	SchemaChecksum          string
	TopologyChecksum        string
}

type DeploymentPlanBaseline struct {
	ID                      uuid.UUID          `db:"id" json:"id,omitempty"`
	CreatedAt               time.Time          `db:"created_at" json:"createdAt,omitempty"`
	DeploymentPlanID        uuid.UUID          `db:"deployment_plan_id" json:"deploymentPlanId,omitempty"`
	OrganizationID          uuid.UUID          `db:"organization_id" json:"organizationId,omitempty"`
	ComponentInstanceID     uuid.UUID          `db:"component_instance_id" json:"componentInstanceId"`
	ComponentKey            string             `db:"component_key" json:"componentKey"`
	SourceDeploymentPlanID  *uuid.UUID         `db:"source_deployment_plan_id" json:"sourceDeploymentPlanId,omitempty"`
	ExternalExecutionID     *uuid.UUID         `db:"external_execution_id" json:"externalExecutionId,omitempty"`
	ObservationID           *uuid.UUID         `db:"observation_id" json:"observationId,omitempty"`
	ObservedAt              *time.Time         `db:"observed_at" json:"observedAt,omitempty"`
	DesiredRevision         int64              `db:"desired_revision" json:"desiredRevision"`
	DesiredChecksum         string             `db:"desired_checksum" json:"desiredChecksum"`
	ObservationChecksum     string             `db:"observation_checksum" json:"observationChecksum"`
	ReleaseBundleID         *uuid.UUID         `db:"release_bundle_id" json:"releaseBundleId,omitempty"`
	Version                 string             `db:"version" json:"version"`
	Image                   string             `db:"image" json:"image"`
	Platform                string             `db:"platform" json:"platform"`
	ConfigSnapshotID        *uuid.UUID         `db:"target_config_snapshot_id" json:"targetConfigSnapshotId,omitempty"`
	ConfigChecksum          string             `db:"config_checksum" json:"configChecksum"`
	ProviderBindingChecksum string             `db:"provider_binding_checksum" json:"providerBindingChecksum"`
	SchemaState             string             `db:"schema_state" json:"schemaState"`
	SchemaChecksum          string             `db:"schema_checksum" json:"schemaChecksum"`
	TopologyChecksum        string             `db:"topology_checksum" json:"topologyChecksum"`
	Projection              BaselineProjection `db:"projection" json:"projection"`
	AuthorizesV2Execution   bool               `db:"authorizes_v2_execution" json:"authorizesV2Execution"`
	Bootstrap               bool               `db:"bootstrap" json:"bootstrap"`
	ActorUserAccountID      uuid.UUID          `db:"actor_user_account_id" json:"actorUserAccountId,omitempty"`
	CanonicalChecksum       string             `db:"canonical_checksum" json:"canonicalChecksum"`
	SortOrder               int                `db:"sort_order" json:"sortOrder"`
}

type BaselineState struct {
	ComponentInstanceID     uuid.UUID
	ComponentKey            string
	ReleaseBundleID         uuid.UUID
	Version                 string
	Image                   string
	Platform                string
	ConfigSnapshotID        *uuid.UUID
	ConfigChecksum          string
	ProviderBindingChecksum string
	SchemaState             string
	SchemaChecksum          string
	TopologyChecksum        string
	Projection              BaselineProjection
	Bootstrap               bool
}

type PlannedState struct {
	ComponentInstanceID     uuid.UUID
	ComponentKey            string
	ReleaseBundleID         uuid.UUID
	Version                 string
	Image                   string
	Platform                string
	ConfigSnapshotID        *uuid.UUID
	ConfigChecksum          string
	ProviderBindingChecksum string
	DatabaseResourceKey     string
	SchemaState             string
	SchemaChecksum          string
	TopologyChecksum        string
	ForwardOnly             bool
}

type ReleaseNote struct {
	ReleaseBundleID uuid.UUID `json:"releaseBundleId"`
	Version         string    `json:"version"`
	PublishedAt     time.Time `json:"publishedAt"`
	SourceRevision  string    `json:"sourceRevision"`
	Summary         string    `json:"summary"`
}

type DeploymentPlanChangeKind string

const (
	DeploymentPlanChangeBootstrap         DeploymentPlanChangeKind = "bootstrap"
	DeploymentPlanChangeBaselineAuthority DeploymentPlanChangeKind = "baseline_authority"
	DeploymentPlanChangeImage             DeploymentPlanChangeKind = "image"
	DeploymentPlanChangeConfig            DeploymentPlanChangeKind = "config"
	DeploymentPlanChangeProvider          DeploymentPlanChangeKind = "provider"
	DeploymentPlanChangeSchema            DeploymentPlanChangeKind = "schema"
	DeploymentPlanChangeTopology          DeploymentPlanChangeKind = "topology"
	DeploymentPlanChangeSourceNotes       DeploymentPlanChangeKind = "source_notes"
	DeploymentPlanChangePreviousState     DeploymentPlanChangeKind = "previous_state"
	DeploymentPlanChangeLimitExceeded     DeploymentPlanChangeKind = "planning_limit_exceeded"
)

type DeploymentPlanChangeEntry struct {
	ID                  uuid.UUID                `db:"id" json:"id,omitempty"`
	CreatedAt           time.Time                `db:"created_at" json:"createdAt,omitempty"`
	DeploymentPlanID    uuid.UUID                `db:"deployment_plan_id" json:"deploymentPlanId,omitempty"`
	OrganizationID      uuid.UUID                `db:"organization_id" json:"organizationId,omitempty"`
	ComponentInstanceID uuid.UUID                `db:"component_instance_id" json:"componentInstanceId,omitempty"`
	ComponentKey        string                   `db:"component_key" json:"componentKey"`
	Kind                DeploymentPlanChangeKind `db:"kind" json:"kind"`
	Before              string                   `db:"before_value" json:"before"`
	After               string                   `db:"after_value" json:"after"`
	ReleaseNotes        []ReleaseNote            `db:"release_notes" json:"releaseNotes,omitempty"`
	ForwardOnly         bool                     `db:"forward_only" json:"forwardOnly"`
	ActorUserAccountID  uuid.UUID                `db:"actor_user_account_id" json:"actorUserAccountId,omitempty"`
	CanonicalChecksum   string                   `db:"canonical_checksum" json:"canonicalChecksum"`
	SortOrder           int                      `db:"sort_order" json:"sortOrder"`
}

type DeploymentPlanRiskLevel string

const (
	DeploymentPlanRiskLow      DeploymentPlanRiskLevel = "low"
	DeploymentPlanRiskMedium   DeploymentPlanRiskLevel = "medium"
	DeploymentPlanRiskHigh     DeploymentPlanRiskLevel = "high"
	DeploymentPlanRiskCritical DeploymentPlanRiskLevel = "critical"
)

type EffectivePolicy struct {
	AllowForwardOnlyMigration      bool
	RequireBootstrapApproval       bool
	RequireAuthoritativeV2Baseline bool
}

type DeploymentPlanRiskEntry struct {
	ID                 uuid.UUID               `db:"id" json:"id,omitempty"`
	CreatedAt          time.Time               `db:"created_at" json:"createdAt,omitempty"`
	DeploymentPlanID   uuid.UUID               `db:"deployment_plan_id" json:"deploymentPlanId,omitempty"`
	OrganizationID     uuid.UUID               `db:"organization_id" json:"organizationId,omitempty"`
	ComponentKey       string                  `db:"component_key" json:"componentKey"`
	Code               string                  `db:"code" json:"code"`
	Level              DeploymentPlanRiskLevel `db:"level" json:"level"`
	Blocking           bool                    `db:"blocking" json:"blocking"`
	Message            string                  `db:"message" json:"message"`
	ActorUserAccountID uuid.UUID               `db:"actor_user_account_id" json:"actorUserAccountId,omitempty"`
	CanonicalChecksum  string                  `db:"canonical_checksum" json:"canonicalChecksum"`
	SortOrder          int                     `db:"sort_order" json:"sortOrder"`
}

type DeploymentPlanTarget struct {
	ID                     uuid.UUID                `db:"id" json:"id"`
	DeploymentPlanID       uuid.UUID                `db:"deployment_plan_id" json:"deploymentPlanId"`
	OrganizationID         uuid.UUID                `db:"organization_id" json:"organizationId"`
	DeploymentTargetID     uuid.UUID                `db:"deployment_target_id" json:"deploymentTargetId"`
	Name                   string                   `db:"name" json:"name"`
	Type                   DeploymentType           `db:"type" json:"type"`
	Platform               DeploymentTargetPlatform `db:"platform" json:"platform"`
	CustomerOrganizationID *uuid.UUID               `db:"customer_organization_id" json:"customerOrganizationId,omitempty"`
	SortOrder              int                      `db:"sort_order" json:"sortOrder"`
}

type DeploymentPlanTargetComponent struct {
	ID                      uuid.UUID                `db:"id" json:"id"`
	DeploymentPlanID        uuid.UUID                `db:"deployment_plan_id" json:"deploymentPlanId"`
	DeploymentPlanTargetID  uuid.UUID                `db:"deployment_plan_target_id" json:"deploymentPlanTargetId"`
	OrganizationID          uuid.UUID                `db:"organization_id" json:"organizationId"`
	DeploymentTargetID      uuid.UUID                `db:"deployment_target_id" json:"deploymentTargetId"`
	Component               string                   `db:"component" json:"component"`
	Version                 string                   `db:"version" json:"version"`
	Image                   string                   `db:"image" json:"image"`
	Platform                DeploymentTargetPlatform `db:"platform" json:"platform"`
	Contracts               []string                 `db:"contracts" json:"contracts"`
	ConfigChecksum          string                   `db:"config_checksum" json:"configChecksum"`
	ExpectedStateVersion    int64                    `db:"expected_state_version" json:"expectedStateVersion"`
	ExpectedStateChecksum   string                   `db:"expected_state_checksum" json:"expectedStateChecksum"`
	ExpectedReleaseBundleID *uuid.UUID               `db:"expected_release_bundle_id" json:"expectedReleaseBundleId,omitempty"` //nolint:lll
	SortOrder               int                      `db:"sort_order" json:"sortOrder"`
}

type DeploymentPlanStep struct {
	ID                     uuid.UUID      `db:"id" json:"id"`
	DeploymentPlanID       uuid.UUID      `db:"deployment_plan_id" json:"deploymentPlanId"`
	OrganizationID         uuid.UUID      `db:"organization_id" json:"organizationId"`
	StepKey                string         `db:"step_key" json:"stepKey"`
	Name                   string         `db:"name" json:"name"`
	ActionType             string         `db:"action_type" json:"actionType"`
	ActionName             string         `db:"action_name" json:"actionName"`
	ExecutionLocation      string         `db:"execution_location" json:"executionLocation"`
	InputBindings          map[string]any `db:"input_bindings" json:"inputBindings"`
	StepInputChecksum      string         `db:"step_input_checksum" json:"stepInputChecksum,omitempty"`
	RetryClass             string         `db:"retry_class" json:"retryClass,omitempty"`
	CancellationBehavior   string         `db:"cancellation_behavior" json:"cancellationBehavior,omitempty"`
	ObservationRequirement string         `db:"observation_requirement" json:"observationRequirement,omitempty"`
	TargetLockKey          string         `db:"target_lock_key" json:"targetLockKey,omitempty"`
	DatabaseLockKey        string         `db:"database_lock_key" json:"databaseLockKey,omitempty"`
	Condition              string         `db:"condition" json:"condition"`
	TargetTags             []string       `db:"target_tags" json:"targetTags"`
	FailureMode            string         `db:"failure_mode" json:"failureMode"`
	TimeoutSeconds         int            `db:"timeout_seconds" json:"timeoutSeconds"`
	RetryMaxAttempts       int            `db:"retry_max_attempts" json:"retryMaxAttempts"`
	RetryIntervalSeconds   int            `db:"retry_interval_seconds" json:"retryIntervalSeconds"`
	RequiredPermissions    []string       `db:"required_permissions" json:"requiredPermissions"`
	SortOrder              int            `db:"sort_order" json:"sortOrder"`
	Dependencies           []string       `db:"dependencies" json:"dependencies"`
	Included               bool           `db:"included" json:"included"`
	ExcludedReason         string         `db:"excluded_reason" json:"excludedReason,omitempty"`
}

type DeploymentPlanMigration struct {
	ID                               uuid.UUID              `db:"id" json:"id"`
	CreatedAt                        time.Time              `db:"created_at" json:"createdAt"`
	DeploymentPlanID                 uuid.UUID              `db:"deployment_plan_id" json:"deploymentPlanId"`
	OrganizationID                   uuid.UUID              `db:"organization_id" json:"organizationId"`
	MigrationID                      string                 `db:"migration_id" json:"migrationId"`
	ContractChecksum                 string                 `db:"contract_checksum" json:"contractChecksum"`
	ComponentKey                     string                 `db:"component_key" json:"componentKey"`
	DatabaseResourceKey              string                 `db:"database_resource_key" json:"databaseResourceKey"`
	ExpectedSourceVersion            string                 `db:"expected_source_version" json:"expectedSourceVersion"`
	ExpectedSourceChecksum           string                 `db:"expected_source_checksum" json:"expectedSourceChecksum"`
	ResultingVersion                 string                 `db:"resulting_version" json:"resultingVersion"`
	ResultingSchemaChecksum          string                 `db:"resulting_schema_checksum" json:"resultingSchemaChecksum"`
	Phase                            MigrationPhase         `db:"phase" json:"phase"`
	DependsOn                        []string               `db:"depends_on" json:"dependsOn,omitempty"`
	LockType                         string                 `db:"lock_type" json:"lockType"`
	LockTimeoutSeconds               int                    `db:"lock_timeout_seconds" json:"lockTimeoutSeconds"`
	OperationalImpact                string                 `db:"operational_impact" json:"operationalImpact"`
	BackupRequired                   bool                   `db:"backup_required" json:"backupRequired"`
	BackupVerifier                   string                 `db:"backup_verifier" json:"backupVerifier,omitempty"`
	RetryClass                       MigrationRetryClass    `db:"retry_class" json:"retryClass"`
	IdempotencyKey                   string                 `db:"idempotency_key" json:"idempotencyKey"`
	Reversibility                    MigrationReversibility `db:"reversibility" json:"reversibility"`
	PreviousApplicationCompatibility string                 `db:"previous_application_compatibility" json:"previousApplicationCompatibility"` //nolint:lll
	RecoveryProcedureReference       string                 `db:"recovery_procedure_reference" json:"recoveryProcedureReference"`             //nolint:lll
	RequiresForwardFix               bool                   `db:"requires_forward_fix" json:"requiresForwardFix"`
	AdapterType                      string                 `db:"adapter_type" json:"adapterType"`
	ArtifactDigest                   string                 `db:"artifact_digest" json:"artifactDigest"`
	PreconditionProbes               []MigrationProbe       `db:"precondition_probes" json:"preconditionProbes"`
	PostconditionProbes              []MigrationProbe       `db:"postcondition_probes" json:"postconditionProbes"`
	EvidenceRetentionDays            int                    `db:"evidence_retention_days" json:"evidenceRetentionDays"`
	ApplyStepKey                     string                 `db:"apply_step_key" json:"applyStepKey"`
	ValidateStepKey                  string                 `db:"validate_step_key" json:"validateStepKey"`
	SortOrder                        int                    `db:"sort_order" json:"sortOrder"`
}

func (migration DeploymentPlanMigration) MigrationContract() MigrationContract {
	return MigrationContract{
		ID:                               migration.MigrationID,
		Checksum:                         migration.ContractChecksum,
		ComponentKey:                     migration.ComponentKey,
		DatabaseResourceKey:              migration.DatabaseResourceKey,
		ExpectedSourceVersion:            migration.ExpectedSourceVersion,
		ExpectedSourceChecksum:           migration.ExpectedSourceChecksum,
		ResultingVersion:                 migration.ResultingVersion,
		ResultingSchemaChecksum:          migration.ResultingSchemaChecksum,
		Phase:                            migration.Phase,
		DependsOn:                        slices.Clone(migration.DependsOn),
		LockType:                         migration.LockType,
		LockTimeoutSeconds:               migration.LockTimeoutSeconds,
		OperationalImpact:                migration.OperationalImpact,
		BackupRequired:                   migration.BackupRequired,
		BackupVerifier:                   migration.BackupVerifier,
		PreconditionProbes:               slices.Clone(migration.PreconditionProbes),
		PostconditionProbes:              slices.Clone(migration.PostconditionProbes),
		RetryClass:                       migration.RetryClass,
		IdempotencyKey:                   migration.IdempotencyKey,
		Reversibility:                    migration.Reversibility,
		PreviousApplicationCompatibility: migration.PreviousApplicationCompatibility,
		RecoveryProcedureReference:       migration.RecoveryProcedureReference,
		RequiresForwardFix:               migration.RequiresForwardFix,
		AdapterType:                      migration.AdapterType,
		ArtifactDigest:                   migration.ArtifactDigest,
		EvidenceRetentionDays:            migration.EvidenceRetentionDays,
	}
}

type DeploymentPlanVariable struct {
	ID               uuid.UUID                      `db:"id" json:"id"`
	DeploymentPlanID uuid.UUID                      `db:"deployment_plan_id" json:"deploymentPlanId"`
	OrganizationID   uuid.UUID                      `db:"organization_id" json:"organizationId"`
	VariableSetID    uuid.UUID                      `db:"variable_set_id" json:"variableSetId"`
	VariableID       uuid.UUID                      `db:"variable_id" json:"variableId"`
	Key              string                         `db:"key" json:"key"`
	Type             VariableType                   `db:"type" json:"type"`
	IsRequired       bool                           `db:"is_required" json:"isRequired"`
	Status           VariableResolutionStatus       `db:"status" json:"status"`
	Source           VariableResolutionSource       `db:"source" json:"source"`
	Value            json.RawMessage                `db:"value" json:"value,omitempty"`
	ReferenceID      string                         `db:"reference_id" json:"referenceId,omitempty"`
	ReferenceName    string                         `db:"reference_name" json:"referenceName,omitempty"`
	Redacted         bool                           `db:"redacted" json:"redacted"`
	Trace            []VariableResolutionTraceEntry `db:"-" json:"trace"`
}

type DeploymentPlanIssue struct {
	ID               uuid.UUID                   `db:"id" json:"id"`
	DeploymentPlanID uuid.UUID                   `db:"deployment_plan_id" json:"deploymentPlanId"`
	OrganizationID   uuid.UUID                   `db:"organization_id" json:"organizationId"`
	Severity         DeploymentPlanIssueSeverity `db:"severity" json:"severity"`
	Code             string                      `db:"code" json:"code"`
	Field            string                      `db:"field" json:"field"`
	Message          string                      `db:"message" json:"message"`
	SortOrder        int                         `db:"sort_order" json:"sortOrder"`
}
