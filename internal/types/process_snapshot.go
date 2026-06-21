package types

import (
	"time"

	"github.com/google/uuid"
)

type ProcessSnapshot struct {
	ID                          uuid.UUID                 `db:"id" json:"id"`
	CreatedAt                   time.Time                 `db:"created_at" json:"createdAt"`
	OrganizationID              uuid.UUID                 `db:"organization_id" json:"organizationId"`
	ApplicationID               uuid.UUID                 `db:"application_id" json:"applicationId"`
	DeploymentProcessID         uuid.UUID                 `db:"deployment_process_id" json:"deploymentProcessId"`
	DeploymentProcessRevisionID uuid.UUID                 `db:"deployment_process_revision_id" json:"deploymentProcessRevisionId"` //nolint:lll
	RevisionNumber              int                       `db:"revision_number" json:"revisionNumber"`
	CanonicalChecksum           string                    `db:"canonical_checksum" json:"canonicalChecksum"`
	CanonicalPayload            []byte                    `db:"canonical_payload" json:"-"`
	Revision                    DeploymentProcessRevision `db:"-" json:"revision"`
}
