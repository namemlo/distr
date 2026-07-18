package scheduling

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var (
	errCalendarInstantRequired = errors.New("UTC instant is required")
	errCalendarZoneRequired    = errors.New("IANA zone is required")
	errCalendarRuleRequired    = errors.New("timezone rule version is required")
)

type canonicalWindowRule struct {
	Name        string  `json:"name"`
	Weekdays    []int32 `json:"weekdays"`
	StartMinute int32   `json:"startMinute"`
	EndMinute   int32   `json:"endMinute"`
	SortOrder   int32   `json:"sortOrder"`
}

type canonicalCalendarVersion struct {
	SchemaVersion string                `json:"schemaVersion"`
	CalendarID    uuid.UUID             `json:"calendarId"`
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	IANAZone      string                `json:"ianaZone"`
	RuleVersion   string                `json:"ruleVersion"`
	WindowRules   []canonicalWindowRule `json:"windowRules"`
}

type canonicalFreezeRevision struct {
	SchemaVersion string                 `json:"schemaVersion"`
	FreezeID      uuid.UUID              `json:"freezeId"`
	Name          string                 `json:"name"`
	StartAt       string                 `json:"startAt"`
	EndAt         string                 `json:"endAt"`
	IANAZone      string                 `json:"ianaZone"`
	RuleVersion   string                 `json:"ruleVersion"`
	Scope         types.CalendarScopeRef `json:"scope"`
	Priority      int32                  `json:"priority"`
	Reason        string                 `json:"reason"`
}

type calendarEvaluationIdentity struct {
	Kind               string                   `json:"kind"`
	UTCInstant         string                   `json:"utcInstant"`
	LocalTime          string                   `json:"localTime"`
	UTCOffsetSeconds   int                      `json:"utcOffsetSeconds"`
	IANAZone           string                   `json:"ianaZone"`
	RuleVersion        string                   `json:"ruleVersion"`
	CalendarVersionID  *uuid.UUID               `json:"calendarVersionId,omitempty"`
	WindowRuleID       *uuid.UUID               `json:"windowRuleId,omitempty"`
	SelectedRevisionID *uuid.UUID               `json:"selectedRevisionId,omitempty"`
	ActiveRevisionIDs  []uuid.UUID              `json:"activeRevisionIds,omitempty"`
	ReasonCode         types.CalendarReasonCode `json:"reasonCode"`
}

func CanonicalizeCalendarVersion(
	version types.MaintenanceCalendarVersion,
) ([]byte, string, error) {
	if version.CalendarID == uuid.Nil {
		return nil, "", errors.New("calendar ID is required")
	}
	if strings.TrimSpace(version.Name) == "" {
		return nil, "", errors.New("calendar name is required")
	}
	if len(strings.TrimSpace(version.Name)) > 200 || len(version.Description) > 4000 {
		return nil, "", errors.New("calendar name or description is too large")
	}
	if err := validateZoneAndRuleVersion(version.IANAZone, version.RuleVersion); err != nil {
		return nil, "", err
	}
	if len(version.WindowRules) == 0 {
		return nil, "", errors.New("at least one maintenance window rule is required")
	}

	rules := make([]canonicalWindowRule, len(version.WindowRules))
	seenNames := make(map[string]struct{}, len(version.WindowRules))
	for index, rule := range version.WindowRules {
		normalized, err := normalizeWindowRule(rule)
		if err != nil {
			return nil, "", fmt.Errorf("window rule %d: %w", index, err)
		}
		if _, exists := seenNames[normalized.Name]; exists {
			return nil, "", errors.New("maintenance window rule names must be unique")
		}
		seenNames[normalized.Name] = struct{}{}
		rules[index] = canonicalWindowRule{
			Name:        normalized.Name,
			Weekdays:    normalized.Weekdays,
			StartMinute: normalized.StartMinute,
			EndMinute:   normalized.EndMinute,
			SortOrder:   normalized.SortOrder,
		}
	}
	slices.SortFunc(rules, compareCanonicalWindowRules)

	payload, err := json.Marshal(canonicalCalendarVersion{
		SchemaVersion: types.MaintenanceCalendarSchemaV1,
		CalendarID:    version.CalendarID,
		Name:          strings.TrimSpace(version.Name),
		Description:   version.Description,
		IANAZone:      strings.TrimSpace(version.IANAZone),
		RuleVersion:   strings.TrimSpace(version.RuleVersion),
		WindowRules:   rules,
	})
	if err != nil {
		return nil, "", fmt.Errorf("marshal canonical maintenance calendar: %w", err)
	}
	return payload, checksum(payload), nil
}

func CanonicalizeFreezeRevision(
	revision types.DeploymentFreezeRevision,
) ([]byte, string, error) {
	if revision.FreezeID == uuid.Nil {
		return nil, "", errors.New("freeze ID is required")
	}
	if strings.TrimSpace(revision.Name) == "" {
		return nil, "", errors.New("freeze name is required")
	}
	if len(strings.TrimSpace(revision.Name)) > 200 {
		return nil, "", errors.New("freeze name is too large")
	}
	if err := validateFreezeInterval(revision.StartAt, revision.EndAt); err != nil {
		return nil, "", err
	}
	if err := validateZoneAndRuleVersion(revision.IANAZone, revision.RuleVersion); err != nil {
		return nil, "", err
	}
	if !revision.ScopeKind.IsValid() || revision.ScopeID == uuid.Nil {
		return nil, "", errors.New("freeze scope is invalid")
	}
	if revision.Priority < 0 {
		return nil, "", errors.New("freeze priority must not be negative")
	}
	if strings.TrimSpace(revision.Reason) == "" {
		return nil, "", errors.New("freeze reason is required")
	}
	if len(strings.TrimSpace(revision.Reason)) > 4000 {
		return nil, "", errors.New("freeze reason is too large")
	}

	payload, err := json.Marshal(canonicalFreezeRevision{
		SchemaVersion: types.DeploymentFreezeSchemaV1,
		FreezeID:      revision.FreezeID,
		Name:          strings.TrimSpace(revision.Name),
		StartAt:       revision.StartAt.UTC().Format(time.RFC3339Nano),
		EndAt:         revision.EndAt.UTC().Format(time.RFC3339Nano),
		IANAZone:      strings.TrimSpace(revision.IANAZone),
		RuleVersion:   strings.TrimSpace(revision.RuleVersion),
		Scope: types.CalendarScopeRef{
			Kind: revision.ScopeKind,
			ID:   revision.ScopeID,
		},
		Priority: revision.Priority,
		Reason:   strings.TrimSpace(revision.Reason),
	})
	if err != nil {
		return nil, "", fmt.Errorf("marshal canonical deployment freeze: %w", err)
	}
	return payload, checksum(payload), nil
}

func EvaluateCalendar(
	version types.MaintenanceCalendarVersion,
	input types.CalendarEvaluationInput,
) (types.CalendarEvaluation, error) {
	if err := validateEvaluationInput(input); err != nil {
		return types.CalendarEvaluation{}, err
	}
	if strings.TrimSpace(version.IANAZone) != strings.TrimSpace(input.IANAZone) {
		return types.CalendarEvaluation{}, errors.New(
			"calendar IANA zone does not match the evaluated IANA zone",
		)
	}
	if strings.TrimSpace(version.RuleVersion) != strings.TrimSpace(input.RuleVersion) {
		return types.CalendarEvaluation{}, errors.New(
			"calendar rule version does not match the evaluated rule version",
		)
	}
	if version.ID == uuid.Nil {
		return types.CalendarEvaluation{}, errors.New("calendar version ID is required")
	}
	if len(version.WindowRules) == 0 {
		return types.CalendarEvaluation{}, errors.New(
			"calendar version has no maintenance window rules",
		)
	}

	instant, local, offset, err := calendarEvidence(input)
	if err != nil {
		return types.CalendarEvaluation{}, err
	}
	rules := append([]types.MaintenanceWindowRule(nil), version.WindowRules...)
	for index := range rules {
		normalized, normalizeErr := normalizeWindowRule(rules[index])
		if normalizeErr != nil {
			return types.CalendarEvaluation{}, fmt.Errorf(
				"window rule %d: %w",
				index,
				normalizeErr,
			)
		}
		rules[index] = normalized
	}
	slices.SortFunc(rules, compareWindowRules)

	result := types.CalendarEvaluation{
		Allowed:           false,
		UTCInstant:        instant,
		LocalTime:         local,
		UTCOffsetSeconds:  offset,
		IANAZone:          strings.TrimSpace(input.IANAZone),
		RuleVersion:       strings.TrimSpace(input.RuleVersion),
		CalendarVersionID: copyUUID(version.ID),
		ReasonCode:        types.CalendarReasonWindowClosed,
	}
	for _, rule := range rules {
		if windowRuleMatches(rule, local) {
			result.Allowed = true
			result.WindowRuleID = copyUUID(rule.ID)
			result.ReasonCode = types.CalendarReasonWindowOpen
			break
		}
	}
	result.EvaluationIdentity, err = buildEvaluationIdentity(
		"calendar",
		result.UTCInstant,
		result.LocalTime,
		result.UTCOffsetSeconds,
		result.IANAZone,
		result.RuleVersion,
		result.CalendarVersionID,
		result.WindowRuleID,
		nil,
		nil,
		result.ReasonCode,
	)
	if err != nil {
		return types.CalendarEvaluation{}, err
	}
	return result, nil
}

func EvaluateFreeze(
	revisions []types.DeploymentFreezeRevision,
	input types.CalendarEvaluationInput,
) types.FreezeEvaluation {
	instant := input.UTCInstant.UTC()
	result := types.FreezeEvaluation{
		Allowed:           true,
		UTCInstant:        instant,
		IANAZone:          strings.TrimSpace(input.IANAZone),
		RuleVersion:       strings.TrimSpace(input.RuleVersion),
		ActiveRevisionIDs: []uuid.UUID{},
		ReasonCode:        types.CalendarReasonNoActiveFreeze,
	}
	if err := validateEvaluationInput(input); err != nil {
		result.Allowed = false
		result.Blocked = true
		result.ReasonCode = types.CalendarReasonRuleBindingDrift
		result.LocalTime = instant
		result.EvaluationIdentity = fallbackEvaluationIdentity(result)
		return result
	}

	_, local, offset, err := calendarEvidence(input)
	if err != nil {
		result.Allowed = false
		result.Blocked = true
		result.ReasonCode = types.CalendarReasonRuleBindingDrift
		result.LocalTime = instant
		result.EvaluationIdentity = fallbackEvaluationIdentity(result)
		return result
	}
	result.LocalTime = local
	result.UTCOffsetSeconds = offset

	active := make([]types.DeploymentFreezeRevision, 0, len(revisions))
	for _, revision := range revisions {
		if revision.ID == uuid.Nil ||
			revision.StartAt.IsZero() ||
			revision.EndAt.IsZero() ||
			instant.Before(revision.StartAt.UTC()) ||
			!instant.Before(revision.EndAt.UTC()) {
			continue
		}
		active = append(active, revision)
	}
	slices.SortFunc(active, compareFreezeRevisions)
	for _, revision := range active {
		result.ActiveRevisionIDs = append(result.ActiveRevisionIDs, revision.ID)
	}

	if len(active) > 0 {
		selected := active[0]
		result.Allowed = false
		result.Blocked = true
		result.SelectedRevisionID = copyUUID(selected.ID)
		result.ReasonCode = types.CalendarReasonFreezeActive
		if strings.TrimSpace(selected.IANAZone) != result.IANAZone ||
			strings.TrimSpace(selected.RuleVersion) != result.RuleVersion {
			result.ReasonCode = types.CalendarReasonRuleBindingDrift
		}
	}

	result.EvaluationIdentity, err = buildEvaluationIdentity(
		"freeze",
		result.UTCInstant,
		result.LocalTime,
		result.UTCOffsetSeconds,
		result.IANAZone,
		result.RuleVersion,
		nil,
		nil,
		result.SelectedRevisionID,
		result.ActiveRevisionIDs,
		result.ReasonCode,
	)
	if err != nil {
		result.Allowed = false
		result.Blocked = true
		result.ReasonCode = types.CalendarReasonRuleBindingDrift
		result.EvaluationIdentity = fallbackEvaluationIdentity(result)
	}
	return result
}

func validateEvaluationInput(input types.CalendarEvaluationInput) error {
	if input.UTCInstant.IsZero() {
		return errCalendarInstantRequired
	}
	return validateZoneAndRuleVersion(input.IANAZone, input.RuleVersion)
}

func validateZoneAndRuleVersion(ianaZone string, ruleVersion string) error {
	ianaZone = strings.TrimSpace(ianaZone)
	if ianaZone == "" {
		return errCalendarZoneRequired
	}
	if strings.TrimSpace(ruleVersion) == "" {
		return errCalendarRuleRequired
	}
	if len(ianaZone) > 128 || len(strings.TrimSpace(ruleVersion)) > 128 {
		return errors.New("IANA zone or timezone rule version is too large")
	}
	if _, err := time.LoadLocation(ianaZone); err != nil {
		return fmt.Errorf("IANA zone %q is invalid: %w", ianaZone, err)
	}
	return nil
}

func validateFreezeInterval(startAt, endAt time.Time) error {
	if startAt.IsZero() || endAt.IsZero() {
		return errors.New("freeze start and end instants are required")
	}
	if !endAt.After(startAt) {
		return errors.New("freeze end instant must be after the start instant")
	}
	return nil
}

func calendarEvidence(
	input types.CalendarEvaluationInput,
) (time.Time, time.Time, int, error) {
	location, err := time.LoadLocation(strings.TrimSpace(input.IANAZone))
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf(
			"load IANA zone %q: %w",
			input.IANAZone,
			err,
		)
	}
	instant := input.UTCInstant.UTC()
	local := instant.In(location)
	_, offset := local.Zone()
	return instant, local, offset, nil
}

func normalizeWindowRule(
	rule types.MaintenanceWindowRule,
) (types.MaintenanceWindowRule, error) {
	rule.Name = strings.TrimSpace(rule.Name)
	if rule.ID == uuid.Nil {
		return types.MaintenanceWindowRule{}, errors.New("ID is required")
	}
	if rule.Name == "" {
		return types.MaintenanceWindowRule{}, errors.New("name is required")
	}
	if len(rule.Name) > 200 {
		return types.MaintenanceWindowRule{}, errors.New("name is too large")
	}
	if len(rule.Weekdays) == 0 {
		return types.MaintenanceWindowRule{}, errors.New("at least one weekday is required")
	}
	weekdays := append([]int32(nil), rule.Weekdays...)
	slices.Sort(weekdays)
	for index, weekday := range weekdays {
		if weekday < int32(time.Sunday) || weekday > int32(time.Saturday) {
			return types.MaintenanceWindowRule{}, errors.New("weekday must be between 0 and 6")
		}
		if index > 0 && weekday == weekdays[index-1] {
			return types.MaintenanceWindowRule{}, errors.New("weekdays must be unique")
		}
	}
	if rule.StartMinute < 0 || rule.StartMinute >= 24*60 {
		return types.MaintenanceWindowRule{}, errors.New(
			"start minute must be between 0 and 1439",
		)
	}
	if rule.EndMinute < 0 || rule.EndMinute > 24*60 {
		return types.MaintenanceWindowRule{}, errors.New(
			"end minute must be between 0 and 1440",
		)
	}
	if rule.StartMinute == rule.EndMinute {
		return types.MaintenanceWindowRule{}, errors.New(
			"start and end minute must differ",
		)
	}
	if rule.SortOrder < 0 {
		return types.MaintenanceWindowRule{}, errors.New(
			"sort order must not be negative",
		)
	}
	rule.Weekdays = weekdays
	return rule, nil
}

func windowRuleMatches(rule types.MaintenanceWindowRule, local time.Time) bool {
	minute := int32(local.Hour()*60 + local.Minute())
	weekday := int32(local.Weekday())
	if rule.StartMinute < rule.EndMinute {
		return containsWeekday(rule.Weekdays, weekday) &&
			minute >= rule.StartMinute &&
			minute < rule.EndMinute
	}
	if containsWeekday(rule.Weekdays, weekday) && minute >= rule.StartMinute {
		return true
	}
	previousWeekday := (weekday + 6) % 7
	return containsWeekday(rule.Weekdays, previousWeekday) && minute < rule.EndMinute
}

func containsWeekday(weekdays []int32, weekday int32) bool {
	_, found := slices.BinarySearch(weekdays, weekday)
	return found
}

func compareWindowRules(left, right types.MaintenanceWindowRule) int {
	if left.SortOrder != right.SortOrder {
		return int(left.SortOrder - right.SortOrder)
	}
	if comparison := strings.Compare(left.Name, right.Name); comparison != 0 {
		return comparison
	}
	return strings.Compare(left.ID.String(), right.ID.String())
}

func compareCanonicalWindowRules(left, right canonicalWindowRule) int {
	if left.SortOrder != right.SortOrder {
		return int(left.SortOrder - right.SortOrder)
	}
	if comparison := strings.Compare(left.Name, right.Name); comparison != 0 {
		return comparison
	}
	if comparison := slices.Compare(left.Weekdays, right.Weekdays); comparison != 0 {
		return comparison
	}
	if left.StartMinute != right.StartMinute {
		return int(left.StartMinute - right.StartMinute)
	}
	return int(left.EndMinute - right.EndMinute)
}

func compareFreezeRevisions(
	left, right types.DeploymentFreezeRevision,
) int {
	if left.Priority != right.Priority {
		if left.Priority > right.Priority {
			return -1
		}
		return 1
	}
	return strings.Compare(left.ID.String(), right.ID.String())
}

func buildEvaluationIdentity(
	kind string,
	utcInstant time.Time,
	localTime time.Time,
	utcOffsetSeconds int,
	ianaZone string,
	ruleVersion string,
	calendarVersionID *uuid.UUID,
	windowRuleID *uuid.UUID,
	selectedRevisionID *uuid.UUID,
	activeRevisionIDs []uuid.UUID,
	reasonCode types.CalendarReasonCode,
) (string, error) {
	payload, err := json.Marshal(calendarEvaluationIdentity{
		Kind:               kind,
		UTCInstant:         utcInstant.UTC().Format(time.RFC3339Nano),
		LocalTime:          localTime.Format(time.RFC3339Nano),
		UTCOffsetSeconds:   utcOffsetSeconds,
		IANAZone:           ianaZone,
		RuleVersion:        ruleVersion,
		CalendarVersionID:  calendarVersionID,
		WindowRuleID:       windowRuleID,
		SelectedRevisionID: selectedRevisionID,
		ActiveRevisionIDs:  activeRevisionIDs,
		ReasonCode:         reasonCode,
	})
	if err != nil {
		return "", fmt.Errorf("marshal calendar evaluation identity: %w", err)
	}
	return checksum(payload), nil
}

func fallbackEvaluationIdentity(result types.FreezeEvaluation) string {
	payload := fmt.Sprintf(
		"freeze|%s|%s|%s|%s",
		result.UTCInstant.UTC().Format(time.RFC3339Nano),
		result.IANAZone,
		result.RuleVersion,
		result.ReasonCode,
	)
	return checksum([]byte(payload))
}

func checksum(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func copyUUID(value uuid.UUID) *uuid.UUID {
	copy := value
	return &copy
}
