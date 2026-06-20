package types

import (
	"time"

	"github.com/google/uuid"
)

type Channel struct {
	ID                          uuid.UUID `db:"id" json:"id"`
	CreatedAt                   time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt                   time.Time `db:"updated_at" json:"updatedAt"`
	OrganizationID              uuid.UUID `db:"organization_id" json:"organizationId"`
	ApplicationID               uuid.UUID `db:"application_id" json:"applicationId"`
	LifecycleID                 uuid.UUID `db:"lifecycle_id" json:"lifecycleId"`
	Name                        string    `db:"name" json:"name"`
	Description                 string    `db:"description" json:"description"`
	SortOrder                   int       `db:"sort_order" json:"sortOrder"`
	IsDefault                   bool      `db:"is_default" json:"isDefault"`
	AllowedVersionRanges        []string  `db:"allowed_version_ranges" json:"allowedVersionRanges"`
	AllowedPrereleasePatterns   []string  `db:"allowed_prerelease_patterns" json:"allowedPrereleasePatterns"`
	AllowedSourceBranchPatterns []string  `db:"allowed_source_branches" json:"allowedSourceBranches"`
	AllowedSourceTagPatterns    []string  `db:"allowed_source_tags" json:"allowedSourceTags"`
}
