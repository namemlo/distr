package api

import (
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/channelrules"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type CreateUpdateChannelRequest struct {
	ApplicationID               uuid.UUID `json:"applicationId"`
	LifecycleID                 uuid.UUID `json:"lifecycleId"`
	Name                        string    `json:"name"`
	Description                 string    `json:"description"`
	SortOrder                   int       `json:"sortOrder"`
	IsDefault                   bool      `json:"isDefault"`
	AllowedVersionRanges        []string  `json:"allowedVersionRanges"`
	AllowedPrereleasePatterns   []string  `json:"allowedPrereleasePatterns"`
	AllowedSourceBranchPatterns []string  `json:"allowedSourceBranches"`
	AllowedSourceTagPatterns    []string  `json:"allowedSourceTags"`
}

func (r *CreateUpdateChannelRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if r.ApplicationID == uuid.Nil {
		return validation.NewValidationFailedError("applicationId is required")
	}
	if r.LifecycleID == uuid.Nil {
		return validation.NewValidationFailedError("lifecycleId is required")
	}
	if r.SortOrder < 0 {
		return validation.NewValidationFailedError("sortOrder must be non-negative")
	}
	rules, err := channelrules.NormalizeRules(channelRulesFromCreateUpdateRequest(*r))
	if err != nil {
		return validation.NewValidationFailedError(err.Error())
	}
	r.AllowedVersionRanges = rules.AllowedVersionRanges
	r.AllowedPrereleasePatterns = rules.AllowedPrereleasePatterns
	r.AllowedSourceBranchPatterns = rules.AllowedSourceBranchPatterns
	r.AllowedSourceTagPatterns = rules.AllowedSourceTagPatterns
	return nil
}

type Channel struct {
	ID                          uuid.UUID `json:"id"`
	CreatedAt                   time.Time `json:"createdAt"`
	UpdatedAt                   time.Time `json:"updatedAt"`
	ApplicationID               uuid.UUID `json:"applicationId"`
	LifecycleID                 uuid.UUID `json:"lifecycleId"`
	Name                        string    `json:"name"`
	Description                 string    `json:"description"`
	SortOrder                   int       `json:"sortOrder"`
	IsDefault                   bool      `json:"isDefault"`
	AllowedVersionRanges        []string  `json:"allowedVersionRanges"`
	AllowedPrereleasePatterns   []string  `json:"allowedPrereleasePatterns"`
	AllowedSourceBranchPatterns []string  `json:"allowedSourceBranches"`
	AllowedSourceTagPatterns    []string  `json:"allowedSourceTags"`
}

type ValidateChannelVersionRequest struct {
	Version      string `json:"version"`
	SourceBranch string `json:"sourceBranch"`
	SourceTag    string `json:"sourceTag"`
}

func (r *ValidateChannelVersionRequest) Validate() error {
	r.Version = strings.TrimSpace(r.Version)
	r.SourceBranch = strings.TrimSpace(r.SourceBranch)
	r.SourceTag = strings.TrimSpace(r.SourceTag)
	if r.Version == "" {
		return validation.NewValidationFailedError("version is required")
	}
	return nil
}

type ChannelVersionValidationResponse struct {
	Valid  bool                     `json:"valid"`
	Errors []ChannelValidationError `json:"errors"`
}

type ChannelValidationError struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

func channelRulesFromCreateUpdateRequest(request CreateUpdateChannelRequest) channelrules.Rules {
	return channelrules.Rules{
		AllowedVersionRanges:        request.AllowedVersionRanges,
		AllowedPrereleasePatterns:   request.AllowedPrereleasePatterns,
		AllowedSourceBranchPatterns: request.AllowedSourceBranchPatterns,
		AllowedSourceTagPatterns:    request.AllowedSourceTagPatterns,
	}
}
