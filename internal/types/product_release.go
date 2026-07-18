package types

import (
	"time"

	"github.com/google/uuid"
)

const (
	ProductReleaseSchemaV1 = "distr.product-release/v1"

	// Product Release bounds keep graph construction and indexed database
	// values predictable for every tenant. They are contract limits, not
	// pagination defaults.
	ProductReleaseMaxComponents           = 256
	ProductReleaseMaxRequirements         = 256
	ProductReleaseMaxGraphRequirements    = 4096
	ProductReleaseMaxRequiredPlatforms    = 16
	ProductReleaseMaxVersionBytes         = 128
	ProductReleaseMaxCapabilityRangeBytes = 256
	ProductReleaseMaxResolutionModes      = 5
)

type CapabilityResolutionStage string

const (
	CapabilityResolutionStageProduct CapabilityResolutionStage = "product"
	CapabilityResolutionStageTarget  CapabilityResolutionStage = "target"
)

func (s CapabilityResolutionStage) IsValid() bool {
	return s == CapabilityResolutionStageProduct || s == CapabilityResolutionStageTarget
}

type RequirementResolutionMode string

const (
	RequirementResolutionModeIncluded         RequirementResolutionMode = "included"
	RequirementResolutionModePinnedExisting   RequirementResolutionMode = "pinned_existing"
	RequirementResolutionModeSharedProvider   RequirementResolutionMode = "shared_provider"
	RequirementResolutionModeApprovedExternal RequirementResolutionMode = "approved_external"
	RequirementResolutionModeFeatureDisabled  RequirementResolutionMode = "feature_disabled"
)

func (m RequirementResolutionMode) IsValid() bool {
	switch m {
	case RequirementResolutionModeIncluded,
		RequirementResolutionModePinnedExisting,
		RequirementResolutionModeSharedProvider,
		RequirementResolutionModeApprovedExternal,
		RequirementResolutionModeFeatureDisabled:
		return true
	default:
		return false
	}
}

// ProductReleaseManifest is target-neutral. Database and resolution facts use
// json:"-" so the durable release contract contains only stable public fields.
type ProductReleaseManifest struct {
	Schema                   string                    `json:"schema"`
	ReleaseBundleID          uuid.UUID                 `json:"-"`
	OrganizationID           uuid.UUID                 `json:"-"`
	ApplicationID            uuid.UUID                 `json:"-"`
	ChannelID                uuid.UUID                 `json:"-"`
	Product                  string                    `json:"product"`
	Version                  string                    `json:"version"`
	DependencyPolicyVersion  uuid.UUID                 `json:"dependencyPolicyVersion"`
	ReleaseNotes             string                    `json:"releaseNotes"`
	RequiredPlatforms        []string                  `json:"requiredPlatforms"`
	Components               []ProductReleaseComponent `json:"components"`
	Requirements             []CapabilityRequirement   `json:"requirements"`
	GraphChecksum            string                    `json:"graphChecksum"`
	CreatedAt                time.Time                 `json:"-"`
	Status                   ReleaseBundleStatus       `json:"-"`
	CanonicalChecksum        string                    `json:"-"`
	PublishedByUserAccountID *uuid.UUID                `json:"-"`
	PublishedAt              *time.Time                `json:"-"`
}

type ProductReleaseComponent struct {
	ID                       uuid.UUID                   `json:"-"`
	ProductReleaseBundleID   uuid.UUID                   `json:"-"`
	OrganizationID           uuid.UUID                   `json:"-"`
	ComponentReleaseID       uuid.UUID                   `json:"componentReleaseId"`
	ComponentReleaseChecksum string                      `json:"componentReleaseChecksum"`
	ComponentKey             string                      `json:"componentKey"`
	Version                  string                      `json:"version"`
	Published                bool                        `json:"-"`
	Provides                 []CapabilityDeclaration     `json:"-"`
	Requires                 []CapabilityRequirement     `json:"-"`
	Migrations               []MigrationDeclaration      `json:"-"`
	Platforms                []string                    `json:"-"`
	Contract                 *ComponentReleaseContractV2 `json:"-"`
}

type ProductReleaseGraph struct {
	Nodes            []GraphNode `json:"nodes"`
	Edges            []GraphEdge `json:"edges"`
	TopologicalOrder []string    `json:"topologicalOrder"`
	Checksum         string      `json:"checksum"`
}

type GraphNode struct {
	Key                string                      `json:"key"`
	Kind               string                      `json:"kind"`
	ComponentReleaseID *uuid.UUID                  `json:"componentReleaseId,omitempty"`
	ComponentKey       string                      `json:"componentKey,omitempty"`
	Version            string                      `json:"version,omitempty"`
	Capability         string                      `json:"capability,omitempty"`
	VersionRange       string                      `json:"versionRange,omitempty"`
	ResolutionStage    CapabilityResolutionStage   `json:"resolutionStage,omitempty"`
	AllowedModes       []RequirementResolutionMode `json:"allowedModes,omitempty"`
	Unresolved         bool                        `json:"unresolved,omitempty"`
}

type GraphEdge struct {
	Key             string                      `json:"key"`
	From            string                      `json:"from"`
	To              string                      `json:"to"`
	Capability      string                      `json:"capability"`
	VersionRange    string                      `json:"versionRange"`
	ProviderVersion string                      `json:"providerVersion,omitempty"`
	ResolutionStage CapabilityResolutionStage   `json:"resolutionStage"`
	AllowedModes    []RequirementResolutionMode `json:"allowedModes,omitempty"`
	Ordering        string                      `json:"ordering,omitempty"`
}

type ProductReleaseValidationIssue struct {
	Field   string   `json:"field"`
	Rule    string   `json:"rule"`
	Message string   `json:"message"`
	Path    []string `json:"path,omitempty"`
}
