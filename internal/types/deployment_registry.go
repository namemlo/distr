package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type DeliveryModel string

const (
	DeliveryModelDedicated DeliveryModel = "dedicated"
	DeliveryModelShared    DeliveryModel = "shared"
	DeliveryModelExternal  DeliveryModel = "external"
)

func AllDeliveryModels() []DeliveryModel {
	return []DeliveryModel{
		DeliveryModelDedicated,
		DeliveryModelShared,
		DeliveryModelExternal,
	}
}

func (model DeliveryModel) IsValid() bool {
	switch model {
	case DeliveryModelDedicated, DeliveryModelShared, DeliveryModelExternal:
		return true
	default:
		return false
	}
}

type RegistryManagementState string

const (
	RegistryManagementStateManaged       RegistryManagementState = "managed"
	RegistryManagementStateObserveOnly   RegistryManagementState = "observe_only"
	RegistryManagementStateExternal      RegistryManagementState = "external"
	RegistryManagementStateLegacyCutover RegistryManagementState = "legacy_cutover"
	RegistryManagementStateBackup        RegistryManagementState = "backup"
	RegistryManagementStateRetired       RegistryManagementState = "retired"
	RegistryManagementStateUnclassified  RegistryManagementState = "unclassified"
)

func AllRegistryManagementStates() []RegistryManagementState {
	return []RegistryManagementState{
		RegistryManagementStateManaged,
		RegistryManagementStateObserveOnly,
		RegistryManagementStateExternal,
		RegistryManagementStateLegacyCutover,
		RegistryManagementStateBackup,
		RegistryManagementStateRetired,
		RegistryManagementStateUnclassified,
	}
}

func (state RegistryManagementState) IsValid() bool {
	switch state {
	case RegistryManagementStateManaged,
		RegistryManagementStateObserveOnly,
		RegistryManagementStateExternal,
		RegistryManagementStateLegacyCutover,
		RegistryManagementStateBackup,
		RegistryManagementStateRetired,
		RegistryManagementStateUnclassified:
		return true
	default:
		return false
	}
}

type ValidationIssue struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type DeploymentScope struct {
	ID                     uuid.UUID               `db:"id" json:"id"`
	CreatedAt              time.Time               `db:"created_at" json:"createdAt"`
	UpdatedAt              time.Time               `db:"updated_at" json:"updatedAt"`
	OrganizationID         uuid.UUID               `db:"organization_id" json:"organizationId"`
	CustomerOrganizationID *uuid.UUID              `db:"customer_organization_id" json:"customerOrganizationId,omitempty"`
	Key                    string                  `db:"key" json:"key"`
	Name                   string                  `db:"name" json:"name"`
	Description            string                  `db:"description" json:"description"`
	DeliveryModel          DeliveryModel           `db:"delivery_model" json:"deliveryModel"`
	ManagementState        RegistryManagementState `db:"management_state" json:"managementState"`
	RetiredAt              *time.Time              `db:"retired_at" json:"retiredAt,omitempty"`
}

type TargetEnvironmentAssignment struct {
	ID                 uuid.UUID       `db:"id" json:"id"`
	CreatedAt          time.Time       `db:"created_at" json:"createdAt"`
	UpdatedAt          time.Time       `db:"updated_at" json:"updatedAt"`
	OrganizationID     uuid.UUID       `db:"organization_id" json:"organizationId"`
	DeploymentTargetID uuid.UUID       `db:"deployment_target_id" json:"deploymentTargetId"`
	EnvironmentID      uuid.UUID       `db:"environment_id" json:"environmentId"`
	ActiveFrom         time.Time       `db:"active_from" json:"activeFrom"`
	ActiveUntil        *time.Time      `db:"active_until" json:"activeUntil,omitempty"`
	PolicyConstraints  json.RawMessage `db:"policy_constraints" json:"policyConstraints"`
}

type DeploymentUnit struct {
	ID                uuid.UUID `db:"id" json:"id"`
	CreatedAt         time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt         time.Time `db:"updated_at" json:"updatedAt"`
	OrganizationID    uuid.UUID `db:"organization_id" json:"organizationId"`
	DeploymentScopeID uuid.UUID `db:"deployment_scope_id" json:"deploymentScopeId"`
	//nolint:lll // The database and public API names intentionally remain explicit.
	TargetEnvironmentAssignmentID uuid.UUID               `db:"target_environment_assignment_id" json:"targetEnvironmentAssignmentId"`
	DeploymentTargetID            uuid.UUID               `db:"deployment_target_id" json:"deploymentTargetId"`
	Key                           string                  `db:"key" json:"key"`
	Name                          string                  `db:"name" json:"name"`
	PhysicalIdentity              string                  `db:"physical_identity" json:"physicalIdentity"`
	ManagementState               RegistryManagementState `db:"management_state" json:"managementState"`
	SubscriberSetChecksum         string                  `db:"subscriber_set_checksum" json:"subscriberSetChecksum"`
	RetiredAt                     *time.Time              `db:"retired_at" json:"retiredAt,omitempty"`
}

type DeploymentUnitSubscriber struct {
	ID                     uuid.UUID  `db:"id" json:"id"`
	CreatedAt              time.Time  `db:"created_at" json:"createdAt"`
	OrganizationID         uuid.UUID  `db:"organization_id" json:"organizationId"`
	DeploymentUnitID       uuid.UUID  `db:"deployment_unit_id" json:"deploymentUnitId"`
	CustomerOrganizationID uuid.UUID  `db:"customer_organization_id" json:"customerOrganizationId"`
	RetiredAt              *time.Time `db:"retired_at" json:"retiredAt,omitempty"`
}

type ComponentDefinition struct {
	ID              uuid.UUID               `db:"id" json:"id"`
	CreatedAt       time.Time               `db:"created_at" json:"createdAt"`
	UpdatedAt       time.Time               `db:"updated_at" json:"updatedAt"`
	OrganizationID  uuid.UUID               `db:"organization_id" json:"organizationId"`
	Key             string                  `db:"key" json:"key"`
	Name            string                  `db:"name" json:"name"`
	Description     string                  `db:"description" json:"description"`
	CapabilityScope string                  `db:"capability_scope" json:"capabilityScope"`
	ManagementState RegistryManagementState `db:"management_state" json:"managementState"`
	RetiredAt       *time.Time              `db:"retired_at" json:"retiredAt,omitempty"`
}

type ComponentAlias struct {
	ID                    uuid.UUID  `db:"id" json:"id"`
	CreatedAt             time.Time  `db:"created_at" json:"createdAt"`
	OrganizationID        uuid.UUID  `db:"organization_id" json:"organizationId"`
	ComponentDefinitionID uuid.UUID  `db:"component_definition_id" json:"componentDefinitionId"`
	Alias                 string     `db:"alias" json:"alias"`
	RetiredAt             *time.Time `db:"retired_at" json:"retiredAt,omitempty"`
}

type ComponentInstance struct {
	ID                    uuid.UUID               `db:"id" json:"id"`
	CreatedAt             time.Time               `db:"created_at" json:"createdAt"`
	UpdatedAt             time.Time               `db:"updated_at" json:"updatedAt"`
	OrganizationID        uuid.UUID               `db:"organization_id" json:"organizationId"`
	DeploymentUnitID      uuid.UUID               `db:"deployment_unit_id" json:"deploymentUnitId"`
	ComponentDefinitionID uuid.UUID               `db:"component_definition_id" json:"componentDefinitionId"`
	PhysicalName          string                  `db:"physical_name" json:"physicalName"`
	ConfigNamespace       string                  `db:"config_namespace" json:"configNamespace"`
	DatabaseBoundary      string                  `db:"database_boundary" json:"databaseBoundary"`
	HealthAdapter         string                  `db:"health_adapter" json:"healthAdapter"`
	ManagementState       RegistryManagementState `db:"management_state" json:"managementState"`
	// RenamedFrom is a write command precondition. Durable rename hops stay private
	// in ComponentInstanceRename and are never projected into the public resource.
	RenamedFrom string     `db:"-" json:"renamedFrom,omitempty"`
	RetiredAt   *time.Time `db:"retired_at" json:"retiredAt,omitempty"`
}

type DeploymentRegistryPlacement struct {
	EffectiveAt time.Time `json:"-"`

	Scope      DeploymentScope             `json:"scope"`
	Assignment TargetEnvironmentAssignment `json:"assignment"`
	Unit       DeploymentUnit              `json:"unit"`

	Assignments []TargetEnvironmentAssignment `json:"-"`
	Units       []DeploymentUnit              `json:"-"`
	Subscribers []DeploymentUnitSubscriber    `json:"subscribers"`
	Definitions []ComponentDefinition         `json:"definitions"`
	Aliases     []ComponentAlias              `json:"aliases"`
	Instances   []ComponentInstance           `json:"instances"`
}

type RegistryListFilter struct {
	OrganizationID uuid.UUID
	Cursor         string
	Limit          int
}

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type ImportMode string

const (
	ImportModePreview ImportMode = "preview"
	ImportModeApply   ImportMode = "apply"
)

type ImportClassification string

const (
	ImportClassificationStandard      ImportClassification = "standard"
	ImportClassificationShared        ImportClassification = "shared"
	ImportClassificationExternal      ImportClassification = "external"
	ImportClassificationObserveOnly   ImportClassification = "observe_only"
	ImportClassificationIgnored       ImportClassification = "ignored"
	ImportClassificationNeedsDecision ImportClassification = "needs_decision"
)

func (classification ImportClassification) IsValid() bool {
	switch classification {
	case ImportClassificationStandard,
		ImportClassificationShared,
		ImportClassificationExternal,
		ImportClassificationObserveOnly,
		ImportClassificationIgnored,
		ImportClassificationNeedsDecision:
		return true
	default:
		return false
	}
}

type RegistryImportCandidatePlacement struct {
	ComponentKey     string `json:"componentKey"`
	PhysicalName     string `json:"physicalName"`
	ConfigNamespace  string `json:"configNamespace,omitempty"`
	DatabaseBoundary string `json:"databaseBoundary,omitempty"`
	HealthAdapter    string `json:"healthAdapter,omitempty"`
	RenamedFrom      string `json:"renamedFrom,omitempty"`
}

type RegistryImportCandidateRoot struct {
	Key                    string               `json:"key"`
	Name                   string               `json:"name"`
	DeliveryModel          DeliveryModel        `json:"deliveryModel"`
	Classification         ImportClassification `json:"classification"`
	CustomerOrganizationID *uuid.UUID           `json:"customerOrganizationId,omitempty"`
	DeploymentTargetID     uuid.UUID            `json:"deploymentTargetId"`
	EnvironmentID          uuid.UUID            `json:"environmentId"`
	//nolint:lll // Keep the canonical contract field name explicit.
	SubscriberCustomerOrganizationIDs []uuid.UUID                        `json:"subscriberCustomerOrganizationIds,omitempty"`
	PhysicalIdentity                  string                             `json:"physicalIdentity"`
	SourcePath                        string                             `json:"sourcePath,omitempty"`
	Placements                        []RegistryImportCandidatePlacement `json:"placements"`
}

type RegistryImportSourcePlacement struct {
	RootKey      string `json:"rootKey"`
	PhysicalName string `json:"physicalName"`
}

type RegistryImportRequest struct {
	OrganizationID    uuid.UUID                       `json:"-"`
	SourceKind        string                          `json:"sourceKind"`
	ToolName          string                          `json:"toolName"`
	ToolVersion       string                          `json:"toolVersion"`
	SourceCommit      string                          `json:"sourceCommit,omitempty"`
	Parameters        map[string]string               `json:"parameters"`
	EvidenceReference string                          `json:"evidenceReference"`
	EvidenceChecksum  string                          `json:"evidenceChecksum"`
	ActorID           uuid.UUID                       `json:"-"`
	SourcePlacements  []RegistryImportSourcePlacement `json:"sourcePlacements,omitempty"`
	Roots             []RegistryImportCandidateRoot   `json:"roots"`
	ExistingRoots     []RegistryImportCandidateRoot   `json:"-"`
}

type RegistryImportChange struct {
	Kind         string `json:"kind"`
	RootKey      string `json:"rootKey"`
	PlacementKey string `json:"placementKey,omitempty"`
	PhysicalName string `json:"physicalName,omitempty"`
	Message      string `json:"message"`
}

type RegistryImportDiff struct {
	Creates     []RegistryImportChange `json:"creates"`
	Updates     []RegistryImportChange `json:"updates"`
	Retirements []RegistryImportChange `json:"retirements"`
	Conflicts   []RegistryImportChange `json:"conflicts"`
}

type RegistryImportCounts struct {
	DiscoveredRoots      int `json:"discoveredRoots"`
	ClassifiedRoots      int `json:"classifiedRoots"`
	DiscoveredPlacements int `json:"discoveredPlacements"`
	OmittedPlacements    int `json:"omittedPlacements"`
	Creates              int `json:"creates"`
	Updates              int `json:"updates"`
	Retirements          int `json:"retirements"`
	Conflicts            int `json:"conflicts"`
}

type RegistryImportPreview struct {
	ID                   uuid.UUID                     `json:"id"`
	PreviewChecksum      string                        `json:"previewChecksum"`
	Counts               RegistryImportCounts          `json:"counts"`
	Diff                 RegistryImportDiff            `json:"diff"`
	Omissions            []string                      `json:"omissions"`
	Diagnostics          []ValidationIssue             `json:"diagnostics"`
	DiagnosticsTruncated bool                          `json:"diagnosticsTruncated"`
	Roots                []RegistryImportCandidateRoot `json:"roots"`
}

type RegistryImportDecision struct {
	ImportID       uuid.UUID            `json:"-"`
	RootKey        string               `json:"rootKey"`
	Classification ImportClassification `json:"classification"`
	ActorID        uuid.UUID            `json:"-"`
}

type RegistryImportResult struct {
	ID              uuid.UUID            `json:"id"`
	PreviewChecksum string               `json:"previewChecksum"`
	State           string               `json:"state"`
	Applied         bool                 `json:"applied"`
	Counts          RegistryImportCounts `json:"counts"`
	Checkpoint      int                  `json:"checkpoint"`
}

type RegistryCoverageReport struct {
	ImportID               uuid.UUID `json:"importId"`
	DiscoveredRoots        int       `json:"discoveredRoots"`
	ClassifiedRoots        int       `json:"classifiedRoots"`
	ActionableManagedRoots int       `json:"actionableManagedRoots"`
	ObserveOnlyRoots       int       `json:"observeOnlyRoots"`
	ExternalRoots          int       `json:"externalRoots"`
	IgnoredRoots           int       `json:"ignoredRoots"`
	UnresolvedRoots        int       `json:"unresolvedRoots"`
	DiscoveredPlacements   int       `json:"discoveredPlacements"`
	Services               int       `json:"services"`
	OmittedPlacements      int       `json:"omittedPlacements"`
	Omissions              []string  `json:"omissions"`
	Complete               bool      `json:"complete"`
}
