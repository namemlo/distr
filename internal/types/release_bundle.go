package types

import (
	"time"

	"github.com/google/uuid"
)

type ReleaseBundleStatus string

const (
	ReleaseBundleStatusDraft      ReleaseBundleStatus = "DRAFT"
	ReleaseBundleStatusValidating ReleaseBundleStatus = "VALIDATING"
	ReleaseBundleStatusPublished  ReleaseBundleStatus = "PUBLISHED"
	ReleaseBundleStatusBlocked    ReleaseBundleStatus = "BLOCKED"
	ReleaseBundleStatusArchived   ReleaseBundleStatus = "ARCHIVED"
)

type ReleaseBundleComponentType string

const (
	ReleaseBundleComponentTypeApplicationVersion ReleaseBundleComponentType = "application_version"
	ReleaseBundleComponentTypeOCIImage           ReleaseBundleComponentType = "oci_image"
	ReleaseBundleComponentTypeOCIArtifact        ReleaseBundleComponentType = "oci_artifact"
	ReleaseBundleComponentTypeHelmChart          ReleaseBundleComponentType = "helm_chart"
	ReleaseBundleComponentTypeChildReleaseBundle ReleaseBundleComponentType = "child_release_bundle"
	ReleaseBundleComponentTypeExternalArtifact   ReleaseBundleComponentType = "external_artifact"
)

func (t ReleaseBundleComponentType) IsValid() bool {
	switch t {
	case ReleaseBundleComponentTypeApplicationVersion,
		ReleaseBundleComponentTypeOCIImage,
		ReleaseBundleComponentTypeOCIArtifact,
		ReleaseBundleComponentTypeHelmChart,
		ReleaseBundleComponentTypeChildReleaseBundle,
		ReleaseBundleComponentTypeExternalArtifact:
		return true
	default:
		return false
	}
}

type ReleaseBundle struct {
	ID                       uuid.UUID                `db:"id" json:"id"`
	CreatedAt                time.Time                `db:"created_at" json:"createdAt"`
	UpdatedAt                time.Time                `db:"updated_at" json:"updatedAt"`
	OrganizationID           uuid.UUID                `db:"organization_id" json:"organizationId"`
	ApplicationID            uuid.UUID                `db:"application_id" json:"applicationId"`
	ChannelID                uuid.UUID                `db:"channel_id" json:"channelId"`
	ReleaseNumber            string                   `db:"release_number" json:"releaseNumber"`
	ReleaseNotes             string                   `db:"release_notes" json:"releaseNotes"`
	SourceRevision           string                   `db:"source_revision" json:"sourceRevision"`
	Status                   ReleaseBundleStatus      `db:"status" json:"status"`
	PublishedByUserAccountID *uuid.UUID               `db:"published_by_user_account_id" json:"publishedByUserAccountId,omitempty"` //nolint:lll
	PublishedAt              *time.Time               `db:"published_at" json:"publishedAt,omitempty"`
	CanonicalChecksum        string                   `db:"canonical_checksum" json:"canonicalChecksum"`
	CanonicalPayload         []byte                   `db:"canonical_payload" json:"-"`
	Components               []ReleaseBundleComponent `db:"-" json:"components"`
}

type ReleaseBundleComponent struct {
	ID                   uuid.UUID                  `db:"id" json:"id"`
	ReleaseBundleID      uuid.UUID                  `db:"release_bundle_id" json:"releaseBundleId"`
	Key                  string                     `db:"key" json:"key"`
	Name                 string                     `db:"name" json:"name"`
	Type                 ReleaseBundleComponentType `db:"component_type" json:"type"`
	Version              string                     `db:"version" json:"version"`
	ApplicationVersionID *uuid.UUID                 `db:"application_version_id" json:"applicationVersionId,omitempty"`
	PackageRef           string                     `db:"package_ref" json:"packageRef"`
	Digest               string                     `db:"digest" json:"digest"`
	Checksum             string                     `db:"checksum" json:"checksum"`
	ChildReleaseBundleID *uuid.UUID                 `db:"child_release_bundle_id" json:"childReleaseBundleId,omitempty"`
}

type ReleaseBundleAuditEventType string

const (
	ReleaseBundleAuditEventTypePublished               ReleaseBundleAuditEventType = "published"
	ReleaseBundleAuditEventTypeBlocked                 ReleaseBundleAuditEventType = "blocked"
	ReleaseBundleAuditEventTypeArchived                ReleaseBundleAuditEventType = "archived"
	ReleaseBundleAuditEventTypeStateTransitionRejected ReleaseBundleAuditEventType = "state_transition_rejected"
)

type ReleaseBundleAuditEvent struct {
	ID                 uuid.UUID                   `db:"id" json:"id"`
	CreatedAt          time.Time                   `db:"created_at" json:"createdAt"`
	OrganizationID     uuid.UUID                   `db:"organization_id" json:"organizationId"`
	ReleaseBundleID    uuid.UUID                   `db:"release_bundle_id" json:"releaseBundleId"`
	ActorUserAccountID *uuid.UUID                  `db:"actor_user_account_id" json:"actorUserAccountId,omitempty"`
	EventType          ReleaseBundleAuditEventType `db:"event_type" json:"eventType"`
	FromStatus         ReleaseBundleStatus         `db:"from_status" json:"fromStatus"`
	ToStatus           *ReleaseBundleStatus        `db:"to_status" json:"toStatus,omitempty"`
	Reason             string                      `db:"reason" json:"reason"`
}
