package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	MaintenanceCalendarSchemaV1 = "distr.maintenance-calendar/v1"
	DeploymentFreezeSchemaV1    = "distr.deployment-freeze/v1"
)

type CalendarReasonCode string

const (
	CalendarReasonWindowOpen       CalendarReasonCode = "calendar_window_open"
	CalendarReasonWindowClosed     CalendarReasonCode = "calendar_window_closed"
	CalendarReasonFreezeActive     CalendarReasonCode = "deployment_freeze_active"
	CalendarReasonNoActiveFreeze   CalendarReasonCode = "no_active_deployment_freeze"
	CalendarReasonRuleBindingDrift CalendarReasonCode = "timezone_rule_binding_drift"
)

type CalendarScopeKind string

const (
	CalendarScopeOrganization   CalendarScopeKind = "organization"
	CalendarScopeCustomer       CalendarScopeKind = "customer"
	CalendarScopeEnvironment    CalendarScopeKind = "environment"
	CalendarScopeDeploymentUnit CalendarScopeKind = "deployment_unit"
	CalendarScopeComponent      CalendarScopeKind = "component"
	CalendarScopeCampaign       CalendarScopeKind = "campaign"
)

func (kind CalendarScopeKind) IsValid() bool {
	switch kind {
	case CalendarScopeOrganization,
		CalendarScopeCustomer,
		CalendarScopeEnvironment,
		CalendarScopeDeploymentUnit,
		CalendarScopeComponent,
		CalendarScopeCampaign:
		return true
	default:
		return false
	}
}

type CalendarScopeRef struct {
	Kind CalendarScopeKind `json:"kind"`
	ID   uuid.UUID         `json:"id"`
}

type MaintenanceWindowRule struct {
	// ID is the stable logical rule identity used by draft editing and API round trips.
	ID uuid.UUID `db:"id" json:"id"`
	// VersionRuleID is the immutable row identity of this rule in one published version.
	VersionRuleID     uuid.UUID `db:"version_rule_id" json:"-"`
	OrganizationID    uuid.UUID `db:"organization_id" json:"-"`
	CalendarVersionID uuid.UUID `db:"calendar_version_id" json:"-"`
	Name              string    `db:"name" json:"name"`
	Weekdays          []int32   `db:"weekdays" json:"weekdays"`
	StartMinute       int32     `db:"start_minute" json:"startMinute"`
	EndMinute         int32     `db:"end_minute" json:"endMinute"`
	SortOrder         int32     `db:"sort_order" json:"sortOrder"`
}

type MaintenanceCalendar struct {
	ID                     uuid.UUID               `db:"id" json:"id"`
	CreatedAt              time.Time               `db:"created_at" json:"createdAt"`
	UpdatedAt              time.Time               `db:"updated_at" json:"updatedAt"`
	OrganizationID         uuid.UUID               `db:"organization_id" json:"-"`
	Name                   string                  `db:"name" json:"name"`
	Description            string                  `db:"description" json:"description"`
	DraftIANAZone          string                  `db:"draft_iana_zone" json:"draftIanaZone"`
	DraftRuleVersion       string                  `db:"draft_rule_version" json:"draftRuleVersion"`
	DraftRulesJSON         json.RawMessage         `db:"draft_rules" json:"-"`
	DraftRules             []MaintenanceWindowRule `db:"-" json:"draftRules"`
	DraftRevision          int64                   `db:"draft_revision" json:"draftRevision"`
	LastPublishedVersionID *uuid.UUID              `db:"last_published_version_id" json:"lastPublishedVersionId,omitempty"`
	CreatedBy              uuid.UUID               `db:"created_by_useraccount_id" json:"createdBy"`
	UpdatedBy              uuid.UUID               `db:"updated_by_useraccount_id" json:"updatedBy"`
}

type MaintenanceCalendarVersion struct {
	ID                  uuid.UUID               `db:"id" json:"id"`
	CalendarID          uuid.UUID               `db:"maintenance_calendar_id" json:"calendarId"`
	OrganizationID      uuid.UUID               `db:"organization_id" json:"-"`
	VersionNumber       int64                   `db:"version_number" json:"versionNumber"`
	SourceDraftRevision int64                   `db:"source_draft_revision" json:"sourceDraftRevision"`
	Name                string                  `db:"name" json:"name"`
	Description         string                  `db:"description" json:"description"`
	IANAZone            string                  `db:"iana_zone" json:"ianaZone"`
	RuleVersion         string                  `db:"rule_version" json:"ruleVersion"`
	CanonicalPayload    []byte                  `db:"canonical_payload" json:"-"`
	Checksum            string                  `db:"checksum" json:"checksum"`
	PublishedAt         time.Time               `db:"published_at" json:"publishedAt"`
	PublishedBy         uuid.UUID               `db:"published_by_useraccount_id" json:"publishedBy"`
	WindowRules         []MaintenanceWindowRule `db:"-" json:"windowRules"`
}

type DeploymentFreeze struct {
	ID                      uuid.UUID         `db:"id" json:"id"`
	CreatedAt               time.Time         `db:"created_at" json:"createdAt"`
	UpdatedAt               time.Time         `db:"updated_at" json:"updatedAt"`
	OrganizationID          uuid.UUID         `db:"organization_id" json:"-"`
	Name                    string            `db:"name" json:"name"`
	DraftStartAt            time.Time         `db:"draft_start_at" json:"draftStartAt"`
	DraftEndAt              time.Time         `db:"draft_end_at" json:"draftEndAt"`
	DraftIANAZone           string            `db:"draft_iana_zone" json:"draftIanaZone"`
	DraftRuleVersion        string            `db:"draft_rule_version" json:"draftRuleVersion"`
	DraftScopeKind          CalendarScopeKind `db:"draft_scope_kind" json:"draftScopeKind"`
	DraftScopeID            uuid.UUID         `db:"draft_scope_id" json:"draftScopeId"`
	DraftPriority           int32             `db:"draft_priority" json:"draftPriority"`
	DraftReason             string            `db:"draft_reason" json:"draftReason"`
	DraftRevision           int64             `db:"draft_revision" json:"draftRevision"`
	LastPublishedRevisionID *uuid.UUID        `db:"last_published_revision_id" json:"lastPublishedRevisionId,omitempty"`
	CreatedBy               uuid.UUID         `db:"created_by_useraccount_id" json:"createdBy"`
	UpdatedBy               uuid.UUID         `db:"updated_by_useraccount_id" json:"updatedBy"`
}

type DeploymentFreezeRevision struct {
	ID                  uuid.UUID         `db:"id" json:"id"`
	FreezeID            uuid.UUID         `db:"deployment_freeze_id" json:"freezeId"`
	OrganizationID      uuid.UUID         `db:"organization_id" json:"-"`
	VersionNumber       int64             `db:"version_number" json:"versionNumber"`
	SourceDraftRevision int64             `db:"source_draft_revision" json:"sourceDraftRevision"`
	Name                string            `db:"name" json:"name"`
	StartAt             time.Time         `db:"start_at" json:"startAt"`
	EndAt               time.Time         `db:"end_at" json:"endAt"`
	IANAZone            string            `db:"iana_zone" json:"ianaZone"`
	RuleVersion         string            `db:"rule_version" json:"ruleVersion"`
	ScopeKind           CalendarScopeKind `db:"scope_kind" json:"scopeKind"`
	ScopeID             uuid.UUID         `db:"scope_id" json:"scopeId"`
	Priority            int32             `db:"priority" json:"priority"`
	Reason              string            `db:"reason" json:"reason"`
	CanonicalPayload    []byte            `db:"canonical_payload" json:"-"`
	Checksum            string            `db:"checksum" json:"checksum"`
	PublishedAt         time.Time         `db:"published_at" json:"publishedAt"`
	PublishedBy         uuid.UUID         `db:"published_by_useraccount_id" json:"publishedBy"`
}

type CalendarEvaluationInput struct {
	UTCInstant  time.Time `json:"utcInstant"`
	IANAZone    string    `json:"ianaZone"`
	RuleVersion string    `json:"ruleVersion"`
}

type CalendarEvaluation struct {
	Allowed            bool               `json:"allowed"`
	UTCInstant         time.Time          `json:"utcInstant"`
	LocalTime          time.Time          `json:"localTime"`
	UTCOffsetSeconds   int                `json:"utcOffsetSeconds"`
	IANAZone           string             `json:"ianaZone"`
	RuleVersion        string             `json:"ruleVersion"`
	CalendarVersionID  *uuid.UUID         `json:"calendarVersionId,omitempty"`
	WindowRuleID       *uuid.UUID         `json:"windowRuleId,omitempty"`
	ReasonCode         CalendarReasonCode `json:"reasonCode"`
	EvaluationIdentity string             `json:"evaluationIdentity"`
}

type FreezeEvaluation struct {
	Allowed            bool               `json:"allowed"`
	Blocked            bool               `json:"blocked"`
	UTCInstant         time.Time          `json:"utcInstant"`
	LocalTime          time.Time          `json:"localTime"`
	UTCOffsetSeconds   int                `json:"utcOffsetSeconds"`
	IANAZone           string             `json:"ianaZone"`
	RuleVersion        string             `json:"ruleVersion"`
	SelectedRevisionID *uuid.UUID         `json:"selectedRevisionId,omitempty"`
	ActiveRevisionIDs  []uuid.UUID        `json:"activeRevisionIds"`
	ReasonCode         CalendarReasonCode `json:"reasonCode"`
	EvaluationIdentity string             `json:"evaluationIdentity"`
}

type CalendarListFilter struct {
	OrganizationID uuid.UUID
	Cursor         string
	Limit          int
}
