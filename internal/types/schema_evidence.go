package types

import (
	"time"

	"github.com/google/uuid"
)

const (
	SchemaReportSchemaV1         = "distr.schema-report/v1"
	MigrationEvidenceSchemaV1    = "distr.migration-evidence/v1"
	SchemaReportMediaTypeV1      = "application/vnd.distr.schema-report.v1+json"
	MigrationEvidenceMediaTypeV1 = "application/vnd.distr.migration-evidence.v1+json"
	SchemaDecisionNoMigration    = "COMPATIBLE_NO_MIGRATION_REQUIRED"
	SchemaDecisionMigrationBound = "MIGRATION_BOUND"
)

type SchemaEvidenceScope struct {
	OrganizationID          uuid.UUID `json:"organizationId"`
	DeploymentScopeID       uuid.UUID `json:"deploymentScopeId"`
	DeploymentUnitID        uuid.UUID `json:"deploymentUnitId"`
	EnvironmentAssignmentID uuid.UUID `json:"environmentAssignmentId"`
	EnvironmentID           uuid.UUID `json:"environmentId"`
	DeploymentTargetID      uuid.UUID `json:"deploymentTargetId"`
	TargetConfigSnapshotID  uuid.UUID `json:"targetConfigSnapshotId"`
}

type SchemaEvidenceComponent struct {
	ComponentKey       string    `json:"componentKey"`
	ComponentReleaseID uuid.UUID `json:"componentReleaseId"`
	ReleaseChecksum    string    `json:"releaseChecksum"`
	Version            string    `json:"version"`
}

type SchemaReport struct {
	Schema              string                  `json:"schema"`
	Scope               SchemaEvidenceScope     `json:"scope"`
	Component           SchemaEvidenceComponent `json:"component"`
	DatabaseResourceKey string                  `json:"databaseResourceKey"`
	Current             SchemaState             `json:"current"`
	IssuedAt            time.Time               `json:"issuedAt"`
	ExpiresAt           time.Time               `json:"expiresAt"`
	Checksum            string                  `json:"checksum"`
}

type SchemaMigrationBinding struct {
	MigrationID             string `json:"migrationId"`
	ContractChecksum        string `json:"contractChecksum"`
	ExpectedSourceVersion   string `json:"expectedSourceVersion"`
	ExpectedSourceChecksum  string `json:"expectedSourceChecksum"`
	ResultingVersion        string `json:"resultingVersion"`
	ResultingSchemaChecksum string `json:"resultingSchemaChecksum"`
}

type MixedVersionSchemaEvidence struct {
	ApplicationVersion string `json:"applicationVersion"`
	SchemaVersion      string `json:"schemaVersion"`
	SchemaChecksum     string `json:"schemaChecksum"`
	Compatible         bool   `json:"compatible"`
}

type MigrationEvidence struct {
	Schema               string                       `json:"schema"`
	Scope                SchemaEvidenceScope          `json:"scope"`
	Component            SchemaEvidenceComponent      `json:"component"`
	DatabaseResourceKey  string                       `json:"databaseResourceKey"`
	SchemaReportChecksum string                       `json:"schemaReportChecksum"`
	Decision             string                       `json:"decision"`
	ExpectedCurrent      SchemaState                  `json:"expectedCurrent"`
	Migrations           []SchemaMigrationBinding     `json:"migrations"`
	MixedVersionEvidence []MixedVersionSchemaEvidence `json:"mixedVersionEvidence"`
	IssuedAt             time.Time                    `json:"issuedAt"`
	ExpiresAt            time.Time                    `json:"expiresAt"`
	Checksum             string                       `json:"checksum"`
}

type SchemaEvidenceObject struct {
	ObjectKey string `json:"objectKey"`
	Reference string `json:"reference"`
	VersionID string `json:"versionId,omitempty"`
	MediaType string `json:"mediaType"`
	SizeBytes int64  `json:"sizeBytes"`
	Checksum  string `json:"checksum"`
}

type SchemaReportRecord struct {
	Object SchemaEvidenceObject `json:"object"`
	Report SchemaReport         `json:"report"`
}

type MigrationEvidenceRecord struct {
	Object   SchemaEvidenceObject `json:"object"`
	Evidence MigrationEvidence    `json:"evidence"`
}

type SchemaEvidenceRequirement struct {
	ComponentKey        string `json:"componentKey"`
	DatabaseResourceKey string `json:"databaseResourceKey"`
}

type SchemaEvidenceBundle struct {
	Requirement             SchemaEvidenceRequirement `json:"requirement"`
	SchemaReportObject      SchemaEvidenceObject      `json:"schemaReportObject"`
	MigrationEvidenceObject SchemaEvidenceObject      `json:"migrationEvidenceObject"`
	SchemaReport            SchemaReport              `json:"schemaReport"`
	MigrationEvidence       MigrationEvidence         `json:"migrationEvidence"`
}
