package api

import (
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

var (
	productReleaseChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	productReleaseKeyPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
)

type CreateProductReleaseRequest struct {
	Schema                  string                           `json:"schema"`
	ApplicationID           uuid.UUID                        `json:"applicationId"`
	ChannelID               uuid.UUID                        `json:"channelId"`
	Product                 string                           `json:"product"`
	Version                 string                           `json:"version"`
	DependencyPolicyVersion uuid.UUID                        `json:"dependencyPolicyVersion"`
	ReleaseNotes            string                           `json:"releaseNotes"`
	RequiredPlatforms       []string                         `json:"requiredPlatforms"`
	Components              []ProductReleaseComponentRequest `json:"components"`
	Requirements            []types.CapabilityRequirement    `json:"requirements"`
}

type ProductReleaseComponentRequest struct {
	ComponentReleaseID       uuid.UUID `json:"componentReleaseId"`
	ComponentReleaseChecksum string    `json:"componentReleaseChecksum"`
}

func (r *CreateProductReleaseRequest) Validate() error {
	r.Schema = strings.TrimSpace(r.Schema)
	if r.Schema == "" {
		r.Schema = types.ProductReleaseSchemaV1
	}
	r.Product = strings.TrimSpace(r.Product)
	r.Version = strings.TrimSpace(r.Version)
	r.ReleaseNotes = strings.TrimSpace(r.ReleaseNotes)
	if r.Schema != types.ProductReleaseSchemaV1 {
		return validation.NewValidationFailedError("schema must be distr.product-release/v1")
	}
	if r.ApplicationID == uuid.Nil {
		return validation.NewValidationFailedError("applicationId is required")
	}
	if r.ChannelID == uuid.Nil {
		return validation.NewValidationFailedError("channelId is required")
	}
	if !productReleaseKeyPattern.MatchString(r.Product) {
		return validation.NewValidationFailedError("product must be a lowercase stable key")
	}
	if r.Version == "" {
		return validation.NewValidationFailedError("version is required")
	}
	if len(r.Version) > types.ProductReleaseMaxVersionBytes {
		return validation.NewValidationFailedError("version is too long")
	}
	if r.DependencyPolicyVersion == uuid.Nil {
		return validation.NewValidationFailedError("dependencyPolicyVersion is required")
	}
	if len(r.ReleaseNotes) > 8192 {
		return validation.NewValidationFailedError("releaseNotes is too long")
	}
	if len(r.Components) == 0 {
		return validation.NewValidationFailedError("at least one component release is required")
	}
	if len(r.Components) > types.ProductReleaseMaxComponents {
		return validation.NewValidationFailedError("too many component releases")
	}
	if len(r.Requirements) > types.ProductReleaseMaxRequirements {
		return validation.NewValidationFailedError("too many product requirements")
	}
	if len(r.RequiredPlatforms) > types.ProductReleaseMaxRequiredPlatforms {
		return validation.NewValidationFailedError("too many required platforms")
	}

	seenComponentReleaseIDs := make(map[uuid.UUID]struct{}, len(r.Components))
	for index := range r.Components {
		component := &r.Components[index]
		component.ComponentReleaseChecksum = strings.TrimSpace(component.ComponentReleaseChecksum)
		if component.ComponentReleaseID == uuid.Nil {
			return validation.NewValidationFailedError("componentReleaseId is required")
		}
		if _, duplicate := seenComponentReleaseIDs[component.ComponentReleaseID]; duplicate {
			return validation.NewValidationFailedError("component release ids must be unique")
		}
		seenComponentReleaseIDs[component.ComponentReleaseID] = struct{}{}
		if !productReleaseChecksumPattern.MatchString(component.ComponentReleaseChecksum) {
			return validation.NewValidationFailedError(
				"componentReleaseChecksum must be a lowercase sha256 digest",
			)
		}
	}
	seenPlatforms := make(map[string]struct{}, len(r.RequiredPlatforms))
	for index := range r.RequiredPlatforms {
		platform := strings.TrimSpace(r.RequiredPlatforms[index])
		switch platform {
		case "linux/amd64", "linux/arm64":
		default:
			return validation.NewValidationFailedError("required platform is not supported")
		}
		if _, duplicate := seenPlatforms[platform]; duplicate {
			return validation.NewValidationFailedError("required platforms must be unique")
		}
		seenPlatforms[platform] = struct{}{}
		r.RequiredPlatforms[index] = platform
	}
	for index := range r.Requirements {
		r.Requirements[index].Name = strings.TrimSpace(r.Requirements[index].Name)
		r.Requirements[index].Range = strings.TrimSpace(r.Requirements[index].Range)
		r.Requirements[index].ResolutionStage = strings.TrimSpace(r.Requirements[index].ResolutionStage)
		if !productReleaseKeyPattern.MatchString(r.Requirements[index].Name) {
			return validation.NewValidationFailedError(
				"requirement name must be a lowercase stable key",
			)
		}
		if r.Requirements[index].Range == "" {
			return validation.NewValidationFailedError("requirement range is required")
		}
		if len(r.Requirements[index].Range) > types.ProductReleaseMaxCapabilityRangeBytes {
			return validation.NewValidationFailedError("requirement range is too long")
		}
		if len(r.Requirements[index].AllowedModes) > types.ProductReleaseMaxResolutionModes {
			return validation.NewValidationFailedError("too many requirement resolution modes")
		}
		for modeIndex := range r.Requirements[index].AllowedModes {
			r.Requirements[index].AllowedModes[modeIndex] = strings.TrimSpace(r.Requirements[index].AllowedModes[modeIndex])
		}
	}
	return nil
}

type ProductRelease struct {
	ID                       uuid.UUID                 `json:"id"`
	CreatedAt                time.Time                 `json:"createdAt"`
	UpdatedAt                time.Time                 `json:"updatedAt"`
	ApplicationID            uuid.UUID                 `json:"applicationId"`
	ChannelID                uuid.UUID                 `json:"channelId"`
	Status                   types.ReleaseBundleStatus `json:"status"`
	CanonicalChecksum        string                    `json:"canonicalChecksum"`
	GraphChecksum            string                    `json:"graphChecksum"`
	PublishedByUserAccountID *uuid.UUID                `json:"publishedByUserAccountId,omitempty"`
	PublishedAt              *time.Time                `json:"publishedAt,omitempty"`
	Manifest                 ProductReleaseManifest    `json:"manifest"`
}

type ProductReleaseManifest struct {
	Schema                  string                        `json:"schema"`
	Product                 string                        `json:"product"`
	Version                 string                        `json:"version"`
	DependencyPolicyVersion uuid.UUID                     `json:"dependencyPolicyVersion"`
	ReleaseNotes            string                        `json:"releaseNotes"`
	RequiredPlatforms       []string                      `json:"requiredPlatforms"`
	Components              []ProductReleaseComponent     `json:"components"`
	Requirements            []types.CapabilityRequirement `json:"requirements"`
}

type ProductReleaseComponent struct {
	ComponentReleaseID       uuid.UUID `json:"componentReleaseId"`
	ComponentReleaseChecksum string    `json:"componentReleaseChecksum"`
	ComponentKey             string    `json:"componentKey"`
	Version                  string    `json:"version"`
}

type ProductReleaseValidationResponse struct {
	Valid  bool                            `json:"valid"`
	Issues []ProductReleaseValidationIssue `json:"issues"`
}

type ProductReleaseValidationIssue struct {
	Field   string   `json:"field"`
	Rule    string   `json:"rule"`
	Message string   `json:"message"`
	Path    []string `json:"path,omitempty"`
}

type ProductReleaseGraphResponse struct {
	ReleaseBundleID uuid.UUID                 `json:"releaseBundleId"`
	Graph           types.ProductReleaseGraph `json:"graph"`
}
