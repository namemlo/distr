package types

import (
	"time"

	"github.com/google/uuid"
)

type TargetComponentHealth string

const (
	TargetComponentHealthUnknown   TargetComponentHealth = "UNKNOWN"
	TargetComponentHealthHealthy   TargetComponentHealth = "HEALTHY"
	TargetComponentHealthUnhealthy TargetComponentHealth = "UNHEALTHY"
)

func (h TargetComponentHealth) IsValid() bool {
	return h == TargetComponentHealthUnknown || h == TargetComponentHealthHealthy || h == TargetComponentHealthUnhealthy
}

type TargetComponentState struct {
	ID                 uuid.UUID                `db:"id" json:"id"`
	CreatedAt          time.Time                `db:"created_at" json:"createdAt"`
	UpdatedAt          time.Time                `db:"updated_at" json:"updatedAt"`
	OrganizationID     uuid.UUID                `db:"organization_id" json:"organizationId"`
	DeploymentTargetID uuid.UUID                `db:"deployment_target_id" json:"deploymentTargetId"`
	ApplicationID      uuid.UUID                `db:"application_id" json:"applicationId"`
	Component          string                   `db:"component" json:"component"`
	StateVersion       int64                    `db:"state_version" json:"stateVersion"`
	StateChecksum      string                   `db:"state_checksum" json:"stateChecksum"`
	ReleaseBundleID    uuid.UUID                `db:"release_bundle_id" json:"releaseBundleId"`
	Version            string                   `db:"version" json:"version"`
	Image              string                   `db:"image" json:"image"`
	Platform           DeploymentTargetPlatform `db:"platform" json:"platform"`
	Contracts          []string                 `db:"contracts" json:"contracts"`
	ConfigReference    string                   `db:"config_reference" json:"configReference"`
	ConfigChecksum     string                   `db:"config_checksum" json:"configChecksum"`
	Health             TargetComponentHealth    `db:"health" json:"health"`
	ObservedAt         time.Time                `db:"observed_at" json:"observedAt"`
}

type TargetComponentObservation struct {
	ID                     uuid.UUID                `db:"id" json:"id"`
	CreatedAt              time.Time                `db:"created_at" json:"createdAt"`
	OrganizationID         uuid.UUID                `db:"organization_id" json:"organizationId"`
	TargetComponentStateID uuid.UUID                `db:"target_component_state_id" json:"targetComponentStateId"`
	DeploymentTargetID     uuid.UUID                `db:"deployment_target_id" json:"deploymentTargetId"`
	ApplicationID          uuid.UUID                `db:"application_id" json:"applicationId"`
	ComponentInstanceID    *uuid.UUID               `db:"component_instance_id" json:"componentInstanceId,omitempty"`
	Component              string                   `db:"component" json:"component"`
	StateVersion           int64                    `db:"state_version" json:"stateVersion"`
	StateChecksum          string                   `db:"state_checksum" json:"stateChecksum"`
	ReleaseBundleID        uuid.UUID                `db:"release_bundle_id" json:"releaseBundleId"`
	Version                string                   `db:"version" json:"version"`
	Image                  string                   `db:"image" json:"image"`
	Platform               DeploymentTargetPlatform `db:"platform" json:"platform"`
	Contracts              []string                 `db:"contracts" json:"contracts"`
	ConfigReference        string                   `db:"config_reference" json:"configReference"`
	ConfigChecksum         string                   `db:"config_checksum" json:"configChecksum"`
	Health                 TargetComponentHealth    `db:"health" json:"health"`
	ObservedAt             time.Time                `db:"observed_at" json:"observedAt"`
	ExternalExecutionID    *uuid.UUID               `db:"external_execution_id" json:"externalExecutionId,omitempty"`
}
