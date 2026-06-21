package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type VariableSnapshot struct {
	ID                uuid.UUID               `db:"id" json:"id"`
	CreatedAt         time.Time               `db:"created_at" json:"createdAt"`
	OrganizationID    uuid.UUID               `db:"organization_id" json:"organizationId"`
	ReleaseBundleID   uuid.UUID               `db:"release_bundle_id" json:"releaseBundleId"`
	ApplicationID     uuid.UUID               `db:"application_id" json:"applicationId"`
	ChannelID         uuid.UUID               `db:"channel_id" json:"channelId"`
	CanonicalChecksum string                  `db:"canonical_checksum" json:"canonicalChecksum"`
	CanonicalPayload  []byte                  `db:"canonical_payload" json:"-"`
	Values            []VariableSnapshotValue `db:"-" json:"values"`
}

type VariableSnapshotValue struct {
	ID                 uuid.UUID                      `json:"id"`
	VariableSnapshotID uuid.UUID                      `json:"variableSnapshotId"`
	OrganizationID     uuid.UUID                      `json:"organizationId"`
	VariableSetID      uuid.UUID                      `json:"variableSetId"`
	VariableID         uuid.UUID                      `json:"variableId"`
	Key                string                         `json:"key"`
	Type               VariableType                   `json:"type"`
	IsRequired         bool                           `json:"isRequired"`
	Status             VariableResolutionStatus       `json:"status"`
	Source             VariableResolutionSource       `json:"source"`
	Value              json.RawMessage                `json:"value,omitempty"`
	ReferenceID        string                         `json:"referenceId,omitempty"`
	ReferenceName      string                         `json:"referenceName,omitempty"`
	Redacted           bool                           `json:"redacted"`
	Trace              []VariableResolutionTraceEntry `json:"trace"`
}
