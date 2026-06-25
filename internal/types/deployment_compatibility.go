package types

import (
	"time"

	"github.com/google/uuid"
)

type DeploymentCompatibilitySource string

const (
	DeploymentCompatibilitySourceLegacyDirectDeployment DeploymentCompatibilitySource = "legacy_direct_deployment"
)

type DeploymentCompatibilityAvailability struct {
	ProcessSnapshot  bool `db:"process_snapshot_available" json:"processSnapshot"`
	VariableSnapshot bool `db:"variable_snapshot_available" json:"variableSnapshot"`
	Channel          bool `db:"channel_available" json:"channel"`
	Environment      bool `db:"environment_available" json:"environment"`
	TaskLogs         bool `db:"task_logs_available" json:"taskLogs"`
	RedeployPlan     bool `db:"redeploy_plan_available" json:"redeployPlan"`
}

type LegacyDeploymentProjection struct {
	OrganizationID             uuid.UUID                           `json:"organizationId"`
	LegacyDeploymentID         uuid.UUID                           `json:"legacyDeploymentId"`
	LegacyDeploymentRevisionID uuid.UUID                           `json:"legacyDeploymentRevisionId"`
	DeploymentTargetID         uuid.UUID                           `json:"deploymentTargetId"`
	ApplicationID              uuid.UUID                           `json:"applicationId"`
	ApplicationVersionID       uuid.UUID                           `json:"applicationVersionId"`
	ApplicationName            string                              `json:"applicationName"`
	ApplicationVersionName     string                              `json:"applicationVersionName"`
	SyntheticReleaseID         uuid.UUID                           `json:"syntheticReleaseId"`
	CanonicalChecksum          string                              `json:"canonicalChecksum"`
	CanonicalPayload           []byte                              `json:"-"`
	Components                 []DeploymentTimelineComponent       `json:"components"`
	Source                     DeploymentCompatibilitySource       `json:"source"`
	Availability               DeploymentCompatibilityAvailability `json:"availability"`
}

type DeploymentCompatibilityMetadata struct {
	ID                         uuid.UUID                           `db:"id" json:"id"`
	CreatedAt                  time.Time                           `db:"created_at" json:"createdAt"`
	OrganizationID             uuid.UUID                           `db:"organization_id" json:"organizationId"`
	LegacyDeploymentID         uuid.UUID                           `db:"legacy_deployment_id" json:"legacyDeploymentId"`
	LegacyDeploymentRevisionID uuid.UUID                           `db:"legacy_deployment_revision_id" json:"legacyDeploymentRevisionId"`
	DeploymentTargetID         uuid.UUID                           `db:"deployment_target_id" json:"deploymentTargetId"`
	ApplicationID              uuid.UUID                           `db:"application_id" json:"applicationId"`
	ApplicationVersionID       uuid.UUID                           `db:"application_version_id" json:"applicationVersionId"`
	SyntheticReleaseID         uuid.UUID                           `db:"synthetic_release_id" json:"syntheticReleaseId"`
	Source                     DeploymentCompatibilitySource       `db:"source" json:"source"`
	CanonicalChecksum          string                              `db:"canonical_checksum" json:"canonicalChecksum"`
	CanonicalPayload           []byte                              `db:"canonical_payload" json:"-"`
	Availability               DeploymentCompatibilityAvailability `db:"-" json:"availability"`
}

type DeploymentCompatibilityCursor struct {
	CreatedAt                  time.Time `json:"createdAt"`
	LegacyDeploymentRevisionID uuid.UUID `json:"legacyDeploymentRevisionId"`
}

type DeploymentCompatibilityBackfillRequest struct {
	OrganizationID uuid.UUID
	Apply          bool
	BatchSize      int
	Cursor         *DeploymentCompatibilityCursor
}

type DeploymentCompatibilityBackfillReport struct {
	Scanned        int
	Eligible       int
	Projected      int
	AlreadyPresent int
	Skipped        int
	Failed         int
	LastCursor     *DeploymentCompatibilityCursor
}
