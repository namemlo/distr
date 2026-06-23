package types

import (
	"time"

	"github.com/google/uuid"
)

type ConfigAsCodeResourceKind string

const (
	ConfigAsCodeResourceKindDeploymentProcess     ConfigAsCodeResourceKind = "DeploymentProcess"
	ConfigAsCodeResourceKindChannel               ConfigAsCodeResourceKind = "Channel"
	ConfigAsCodeResourceKindLifecycle             ConfigAsCodeResourceKind = "Lifecycle"
	ConfigAsCodeResourceKindVariableSetDefinition ConfigAsCodeResourceKind = "VariableSetDefinition"
	ConfigAsCodeResourceKindStepTemplateReference ConfigAsCodeResourceKind = "StepTemplateReference"
	ConfigAsCodeResourceKindRunbook               ConfigAsCodeResourceKind = "Runbook"
)

type ConfigAsCodeAuthorityValue string

const (
	ConfigAsCodeAuthorityDatabaseManaged ConfigAsCodeAuthorityValue = "DATABASE_MANAGED"
	ConfigAsCodeAuthorityGitManaged      ConfigAsCodeAuthorityValue = "GIT_MANAGED"
)

type ConfigAsCodeAuthority struct {
	ID               uuid.UUID                  `db:"id" json:"id"`
	OrganizationID   uuid.UUID                  `db:"organization_id" json:"organizationId"`
	ResourceKind     ConfigAsCodeResourceKind   `db:"resource_kind" json:"resourceKind"`
	ResourceID       uuid.UUID                  `db:"resource_id" json:"resourceId"`
	Authority        ConfigAsCodeAuthorityValue `db:"authority" json:"authority"`
	RepositoryPath   string                     `db:"repository_path" json:"repositoryPath"`
	SourceRevision   string                     `db:"source_revision" json:"sourceRevision"`
	DocumentChecksum string                     `db:"document_checksum" json:"documentChecksum"`
	UpdatedByUserID  *uuid.UUID                 `db:"updated_by_useraccount_id" json:"updatedByUserId,omitempty"`
	UpdatedAt        time.Time                  `db:"updated_at" json:"updatedAt"`
}
