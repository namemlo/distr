package api

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

const (
	maintenanceCalendarMaximumPageLimit  = 100
	maintenanceCalendarMaximumCursorSize = 2048
)

var maintenanceCalendarCursorPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type MaintenanceCalendarListRequest struct {
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit"`
}

func (request MaintenanceCalendarListRequest) Validate() error {
	if request.Limit < 0 || request.Limit > maintenanceCalendarMaximumPageLimit {
		return validation.NewValidationFailedError(
			"limit must be between 1 and 100 when provided",
		)
	}
	if len(request.Cursor) > maintenanceCalendarMaximumCursorSize {
		return validation.NewValidationFailedError("cursor is too large")
	}
	if request.Cursor != "" && !maintenanceCalendarCursorPattern.MatchString(request.Cursor) {
		return validation.NewValidationFailedError("cursor must be an opaque URL-safe token")
	}
	return nil
}

type MaintenanceWindowRuleRequest struct {
	ID          *uuid.UUID `json:"id,omitempty"`
	Name        string     `json:"name"`
	Weekdays    []int32    `json:"weekdays"`
	StartMinute int32      `json:"startMinute"`
	EndMinute   int32      `json:"endMinute"`
	SortOrder   int32      `json:"sortOrder"`
}

func (request MaintenanceWindowRuleRequest) Validate() error {
	if name := strings.TrimSpace(request.Name); name == "" || len(name) > 200 {
		return validation.NewValidationFailedError(
			"window rule name is required and must not exceed 200 characters",
		)
	}
	if len(request.Weekdays) == 0 {
		return validation.NewValidationFailedError(
			"window rule requires at least one weekday",
		)
	}
	weekdays := append([]int32(nil), request.Weekdays...)
	slices.Sort(weekdays)
	for index, weekday := range weekdays {
		if weekday < int32(time.Sunday) || weekday > int32(time.Saturday) {
			return validation.NewValidationFailedError(
				"window rule weekday must be between 0 and 6",
			)
		}
		if index > 0 && weekday == weekdays[index-1] {
			return validation.NewValidationFailedError(
				"window rule weekdays must be unique",
			)
		}
	}
	if request.StartMinute < 0 || request.StartMinute >= 24*60 {
		return validation.NewValidationFailedError(
			"window rule startMinute must be between 0 and 1439",
		)
	}
	if request.EndMinute < 0 || request.EndMinute > 24*60 {
		return validation.NewValidationFailedError(
			"window rule endMinute must be between 0 and 1440",
		)
	}
	if request.StartMinute == request.EndMinute {
		return validation.NewValidationFailedError(
			"window rule startMinute and endMinute must differ",
		)
	}
	if request.SortOrder < 0 {
		return validation.NewValidationFailedError(
			"window rule sortOrder must not be negative",
		)
	}
	return nil
}

func (request MaintenanceWindowRuleRequest) ToDomain() types.MaintenanceWindowRule {
	id := uuid.New()
	if request.ID != nil && *request.ID != uuid.Nil {
		id = *request.ID
	}
	weekdays := append([]int32(nil), request.Weekdays...)
	slices.Sort(weekdays)
	return types.MaintenanceWindowRule{
		ID:          id,
		Name:        strings.TrimSpace(request.Name),
		Weekdays:    weekdays,
		StartMinute: request.StartMinute,
		EndMinute:   request.EndMinute,
		SortOrder:   request.SortOrder,
	}
}

type CreateMaintenanceCalendarRequest struct {
	Name        string                         `json:"name"`
	Description string                         `json:"description"`
	IANAZone    string                         `json:"ianaZone"`
	RuleVersion string                         `json:"ruleVersion"`
	WindowRules []MaintenanceWindowRuleRequest `json:"windowRules"`
}

func (request CreateMaintenanceCalendarRequest) Validate() error {
	if name := strings.TrimSpace(request.Name); name == "" || len(name) > 200 {
		return validation.NewValidationFailedError(
			"name is required and must not exceed 200 characters",
		)
	}
	if len(request.Description) > 4000 {
		return validation.NewValidationFailedError(
			"description must not exceed 4000 characters",
		)
	}
	if err := validateCalendarZoneBinding(request.IANAZone, request.RuleVersion); err != nil {
		return err
	}
	seenIDs := make(map[uuid.UUID]struct{}, len(request.WindowRules))
	seenNames := make(map[string]struct{}, len(request.WindowRules))
	for index, rule := range request.WindowRules {
		if err := rule.Validate(); err != nil {
			return validation.NewValidationFailedError(
				"windowRules[" + strconv.Itoa(index) + "]: " + err.Error(),
			)
		}
		if rule.ID != nil {
			if *rule.ID == uuid.Nil {
				return validation.NewValidationFailedError(
					"window rule ID must not be empty",
				)
			}
			if _, exists := seenIDs[*rule.ID]; exists {
				return validation.NewValidationFailedError(
					"window rule IDs must be unique",
				)
			}
			seenIDs[*rule.ID] = struct{}{}
		}
		name := strings.TrimSpace(rule.Name)
		if _, exists := seenNames[name]; exists {
			return validation.NewValidationFailedError(
				"window rule names must be unique",
			)
		}
		seenNames[name] = struct{}{}
	}
	return nil
}

func (request CreateMaintenanceCalendarRequest) ToDomain(
	organizationID, actorID uuid.UUID,
) types.MaintenanceCalendar {
	rules := make([]types.MaintenanceWindowRule, len(request.WindowRules))
	for index, rule := range request.WindowRules {
		rules[index] = rule.ToDomain()
	}
	return types.MaintenanceCalendar{
		OrganizationID:   organizationID,
		Name:             strings.TrimSpace(request.Name),
		Description:      request.Description,
		DraftIANAZone:    strings.TrimSpace(request.IANAZone),
		DraftRuleVersion: strings.TrimSpace(request.RuleVersion),
		DraftRules:       rules,
		CreatedBy:        actorID,
		UpdatedBy:        actorID,
	}
}

type UpdateMaintenanceCalendarRequest struct {
	ExpectedDraftRevision int64                          `json:"expectedDraftRevision"`
	Name                  string                         `json:"name"`
	Description           string                         `json:"description"`
	IANAZone              string                         `json:"ianaZone"`
	RuleVersion           string                         `json:"ruleVersion"`
	WindowRules           []MaintenanceWindowRuleRequest `json:"windowRules"`
}

func (request UpdateMaintenanceCalendarRequest) Validate() error {
	if request.ExpectedDraftRevision < 1 {
		return validation.NewValidationFailedError("expectedDraftRevision must be positive")
	}
	return (CreateMaintenanceCalendarRequest{
		Name:        request.Name,
		Description: request.Description,
		IANAZone:    request.IANAZone,
		RuleVersion: request.RuleVersion,
		WindowRules: request.WindowRules,
	}).Validate()
}

type PublishMaintenanceCalendarRequest struct {
	ExpectedDraftRevision int64 `json:"expectedDraftRevision"`
}

func (request PublishMaintenanceCalendarRequest) Validate() error {
	if request.ExpectedDraftRevision < 1 {
		return validation.NewValidationFailedError("expectedDraftRevision must be positive")
	}
	return nil
}

type CreateDeploymentFreezeRequest struct {
	Name        string                  `json:"name"`
	StartAt     time.Time               `json:"startAt"`
	EndAt       time.Time               `json:"endAt"`
	IANAZone    string                  `json:"ianaZone"`
	RuleVersion string                  `json:"ruleVersion"`
	ScopeKind   types.CalendarScopeKind `json:"scopeKind"`
	ScopeID     uuid.UUID               `json:"scopeId"`
	Priority    int32                   `json:"priority"`
	Reason      string                  `json:"reason"`
}

func (request CreateDeploymentFreezeRequest) Validate() error {
	if name := strings.TrimSpace(request.Name); name == "" || len(name) > 200 {
		return validation.NewValidationFailedError(
			"name is required and must not exceed 200 characters",
		)
	}
	if request.StartAt.IsZero() || request.EndAt.IsZero() {
		return validation.NewValidationFailedError("startAt and endAt are required")
	}
	if !request.EndAt.After(request.StartAt) {
		return validation.NewValidationFailedError("endAt must be after startAt")
	}
	if err := validateCalendarZoneBinding(request.IANAZone, request.RuleVersion); err != nil {
		return err
	}
	if !request.ScopeKind.IsValid() || request.ScopeID == uuid.Nil {
		return validation.NewValidationFailedError("scope is invalid")
	}
	if request.ScopeKind == types.CalendarScopeCampaign {
		return validation.NewValidationFailedError(
			"campaign scope is unavailable until immutable campaign revisions exist",
		)
	}
	if request.Priority < 0 {
		return validation.NewValidationFailedError("priority must not be negative")
	}
	if reason := strings.TrimSpace(request.Reason); reason == "" || len(reason) > 4000 {
		return validation.NewValidationFailedError(
			"reason is required and must not exceed 4000 characters",
		)
	}
	return nil
}

func (request CreateDeploymentFreezeRequest) ToDomain(
	organizationID, actorID uuid.UUID,
) types.DeploymentFreeze {
	return types.DeploymentFreeze{
		OrganizationID:   organizationID,
		Name:             strings.TrimSpace(request.Name),
		DraftStartAt:     request.StartAt.UTC(),
		DraftEndAt:       request.EndAt.UTC(),
		DraftIANAZone:    strings.TrimSpace(request.IANAZone),
		DraftRuleVersion: strings.TrimSpace(request.RuleVersion),
		DraftScopeKind:   request.ScopeKind,
		DraftScopeID:     request.ScopeID,
		DraftPriority:    request.Priority,
		DraftReason:      strings.TrimSpace(request.Reason),
		CreatedBy:        actorID,
		UpdatedBy:        actorID,
	}
}

type UpdateDeploymentFreezeRequest struct {
	ExpectedDraftRevision int64                   `json:"expectedDraftRevision"`
	Name                  string                  `json:"name"`
	StartAt               time.Time               `json:"startAt"`
	EndAt                 time.Time               `json:"endAt"`
	IANAZone              string                  `json:"ianaZone"`
	RuleVersion           string                  `json:"ruleVersion"`
	ScopeKind             types.CalendarScopeKind `json:"scopeKind"`
	ScopeID               uuid.UUID               `json:"scopeId"`
	Priority              int32                   `json:"priority"`
	Reason                string                  `json:"reason"`
}

func (request UpdateDeploymentFreezeRequest) Validate() error {
	if request.ExpectedDraftRevision < 1 {
		return validation.NewValidationFailedError("expectedDraftRevision must be positive")
	}
	return (CreateDeploymentFreezeRequest{
		Name:        request.Name,
		StartAt:     request.StartAt,
		EndAt:       request.EndAt,
		IANAZone:    request.IANAZone,
		RuleVersion: request.RuleVersion,
		ScopeKind:   request.ScopeKind,
		ScopeID:     request.ScopeID,
		Priority:    request.Priority,
		Reason:      request.Reason,
	}).Validate()
}

type PublishDeploymentFreezeRequest struct {
	ExpectedDraftRevision int64 `json:"expectedDraftRevision"`
}

func (request PublishDeploymentFreezeRequest) Validate() error {
	if request.ExpectedDraftRevision < 1 {
		return validation.NewValidationFailedError("expectedDraftRevision must be positive")
	}
	return nil
}

type MaintenanceWindowRule struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Weekdays    []int32   `json:"weekdays"`
	StartMinute int32     `json:"startMinute"`
	EndMinute   int32     `json:"endMinute"`
	SortOrder   int32     `json:"sortOrder"`
}

type MaintenanceCalendar struct {
	ID                     uuid.UUID               `json:"id"`
	CreatedAt              time.Time               `json:"createdAt"`
	UpdatedAt              time.Time               `json:"updatedAt"`
	Name                   string                  `json:"name"`
	Description            string                  `json:"description"`
	DraftIANAZone          string                  `json:"draftIanaZone"`
	DraftRuleVersion       string                  `json:"draftRuleVersion"`
	DraftRules             []MaintenanceWindowRule `json:"draftRules"`
	DraftRevision          int64                   `json:"draftRevision"`
	LastPublishedVersionID *uuid.UUID              `json:"lastPublishedVersionId,omitempty"`
	CreatedBy              uuid.UUID               `json:"createdBy"`
	UpdatedBy              uuid.UUID               `json:"updatedBy"`
}

type MaintenanceCalendarVersion struct {
	ID                  uuid.UUID               `json:"id"`
	CalendarID          uuid.UUID               `json:"calendarId"`
	VersionNumber       int64                   `json:"versionNumber"`
	SourceDraftRevision int64                   `json:"sourceDraftRevision"`
	Name                string                  `json:"name"`
	Description         string                  `json:"description"`
	IANAZone            string                  `json:"ianaZone"`
	RuleVersion         string                  `json:"ruleVersion"`
	Checksum            string                  `json:"checksum"`
	PublishedAt         time.Time               `json:"publishedAt"`
	PublishedBy         uuid.UUID               `json:"publishedBy"`
	WindowRules         []MaintenanceWindowRule `json:"windowRules"`
}

type DeploymentFreeze struct {
	ID                      uuid.UUID              `json:"id"`
	CreatedAt               time.Time              `json:"createdAt"`
	UpdatedAt               time.Time              `json:"updatedAt"`
	Name                    string                 `json:"name"`
	DraftStartAt            time.Time              `json:"draftStartAt"`
	DraftEndAt              time.Time              `json:"draftEndAt"`
	DraftIANAZone           string                 `json:"draftIanaZone"`
	DraftRuleVersion        string                 `json:"draftRuleVersion"`
	DraftScope              types.CalendarScopeRef `json:"draftScope"`
	DraftPriority           int32                  `json:"draftPriority"`
	DraftReason             string                 `json:"draftReason"`
	DraftRevision           int64                  `json:"draftRevision"`
	LastPublishedRevisionID *uuid.UUID             `json:"lastPublishedRevisionId,omitempty"`
	CreatedBy               uuid.UUID              `json:"createdBy"`
	UpdatedBy               uuid.UUID              `json:"updatedBy"`
}

type DeploymentFreezeRevision struct {
	ID                  uuid.UUID              `json:"id"`
	FreezeID            uuid.UUID              `json:"freezeId"`
	VersionNumber       int64                  `json:"versionNumber"`
	SourceDraftRevision int64                  `json:"sourceDraftRevision"`
	Name                string                 `json:"name"`
	StartAt             time.Time              `json:"startAt"`
	EndAt               time.Time              `json:"endAt"`
	IANAZone            string                 `json:"ianaZone"`
	RuleVersion         string                 `json:"ruleVersion"`
	Scope               types.CalendarScopeRef `json:"scope"`
	Priority            int32                  `json:"priority"`
	Reason              string                 `json:"reason"`
	Checksum            string                 `json:"checksum"`
	PublishedAt         time.Time              `json:"publishedAt"`
	PublishedBy         uuid.UUID              `json:"publishedBy"`
}

type MaintenanceCalendarPage struct {
	Items      []MaintenanceCalendar `json:"items"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

type MaintenanceCalendarVersionPage struct {
	Items      []MaintenanceCalendarVersion `json:"items"`
	NextCursor string                       `json:"nextCursor,omitempty"`
}

type DeploymentFreezePage struct {
	Items      []DeploymentFreeze `json:"items"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

type DeploymentFreezeRevisionPage struct {
	Items      []DeploymentFreezeRevision `json:"items"`
	NextCursor string                     `json:"nextCursor,omitempty"`
}

func validateCalendarZoneBinding(ianaZone, ruleVersion string) error {
	ianaZone = strings.TrimSpace(ianaZone)
	if ianaZone == "" {
		return validation.NewValidationFailedError("IANA zone is required")
	}
	if _, err := time.LoadLocation(ianaZone); err != nil {
		return validation.NewValidationFailedError("IANA zone is invalid")
	}
	if strings.TrimSpace(ruleVersion) == "" {
		return validation.NewValidationFailedError("ruleVersion is required")
	}
	if len(ianaZone) > 128 || len(strings.TrimSpace(ruleVersion)) > 128 {
		return validation.NewValidationFailedError(
			"IANA zone and ruleVersion must not exceed 128 characters",
		)
	}
	return nil
}
