package scheduling

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestEvaluateCalendarOrdinaryAllowAndDeny(t *testing.T) {
	g := NewWithT(t)
	version := calendarVersion(
		"Europe/Berlin",
		types.MaintenanceWindowRule{
			ID:          uuid.New(),
			Name:        "weekday morning",
			Weekdays:    []int32{int32(time.Monday)},
			StartMinute: 9 * 60,
			EndMinute:   11 * 60,
			SortOrder:   10,
		},
	)

	allowed, err := EvaluateCalendar(version, types.CalendarEvaluationInput{
		UTCInstant:  mustTime(t, "2026-07-20T08:30:00Z"),
		IANAZone:    "Europe/Berlin",
		RuleVersion: "2026a",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(allowed.Allowed).To(BeTrue())
	g.Expect(allowed.ReasonCode).To(Equal(types.CalendarReasonWindowOpen))
	g.Expect(allowed.WindowRuleID).To(Equal(&version.WindowRules[0].ID))
	g.Expect(allowed.UTCInstant).To(Equal(mustTime(t, "2026-07-20T08:30:00Z")))
	g.Expect(allowed.LocalTime.Format("2006-01-02T15:04:05")).To(Equal("2026-07-20T10:30:00"))
	g.Expect(allowed.UTCOffsetSeconds).To(Equal(7200))
	g.Expect(allowed.IANAZone).To(Equal("Europe/Berlin"))
	g.Expect(allowed.RuleVersion).To(Equal("2026a"))
	g.Expect(allowed.EvaluationIdentity).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))

	denied, err := EvaluateCalendar(version, types.CalendarEvaluationInput{
		UTCInstant:  mustTime(t, "2026-07-20T12:30:00Z"),
		IANAZone:    "Europe/Berlin",
		RuleVersion: "2026a",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(denied.Allowed).To(BeFalse())
	g.Expect(denied.ReasonCode).To(Equal(types.CalendarReasonWindowClosed))
	g.Expect(denied.WindowRuleID).To(BeNil())
}

func TestRemainingCalendarWaitSecondsUsesNextTrustedOpenMinute(t *testing.T) {
	g := NewWithT(t)
	version := calendarVersion("UTC", types.MaintenanceWindowRule{
		ID:          uuid.New(),
		Name:        "monday morning",
		Weekdays:    []int32{int32(time.Monday)},
		StartMinute: 9 * 60,
		EndMinute:   11 * 60,
	})

	remaining, err := RemainingCalendarWaitSeconds(
		version,
		mustTime(t, "2026-07-20T08:54:30Z"),
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(remaining).To(Equal(int64(330)))
}

func TestEvaluateCalendarEvidenceUsesImmutableVersionRuleIdentity(t *testing.T) {
	g := NewWithT(t)
	logicalID := uuid.New()
	versionRuleID := uuid.New()
	version := calendarVersion("UTC", types.MaintenanceWindowRule{
		ID: logicalID, VersionRuleID: versionRuleID, Name: "monday",
		Weekdays: []int32{int32(time.Monday)}, StartMinute: 0, EndMinute: 24 * 60,
	})

	result, err := EvaluateCalendar(version, types.CalendarEvaluationInput{
		UTCInstant: mustTime(t, "2026-07-20T10:00:00Z"),
		IANAZone:   "UTC", RuleVersion: "2026a",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.WindowRuleID).To(Equal(&versionRuleID))
	g.Expect(result.WindowRuleID).NotTo(Equal(&logicalID))
}

func TestEvaluateCalendarOvernightWindowUsesPreviousWeekday(t *testing.T) {
	g := NewWithT(t)
	rule := types.MaintenanceWindowRule{
		ID:          uuid.New(),
		Name:        "monday overnight",
		Weekdays:    []int32{int32(time.Monday)},
		StartMinute: 22 * 60,
		EndMinute:   2 * 60,
	}
	version := calendarVersion("UTC", rule)

	for _, instant := range []string{
		"2026-07-20T23:30:00Z",
		"2026-07-21T01:30:00Z",
	} {
		result, err := EvaluateCalendar(version, types.CalendarEvaluationInput{
			UTCInstant: mustTime(t, instant), IANAZone: "UTC", RuleVersion: "2026a",
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.Allowed).To(BeTrue(), instant)
		g.Expect(result.WindowRuleID).To(Equal(&rule.ID))
	}

	result, err := EvaluateCalendar(version, types.CalendarEvaluationInput{
		UTCInstant:  mustTime(t, "2026-07-21T02:00:00Z"),
		IANAZone:    "UTC",
		RuleVersion: "2026a",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Allowed).To(BeFalse())
}

func TestEvaluateCalendarDSTGapRecordsActualLocalEvidence(t *testing.T) {
	g := NewWithT(t)
	version := calendarVersion(
		"America/New_York",
		types.MaintenanceWindowRule{
			ID: uuid.New(), Name: "gap window", Weekdays: []int32{int32(time.Sunday)},
			StartMinute: 2 * 60, EndMinute: 4 * 60,
		},
	)

	result, err := EvaluateCalendar(version, types.CalendarEvaluationInput{
		UTCInstant:  mustTime(t, "2026-03-08T07:15:00Z"),
		IANAZone:    "America/New_York",
		RuleVersion: "2026a",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Allowed).To(BeTrue())
	g.Expect(result.LocalTime.Format("2006-01-02T15:04:05")).To(Equal("2026-03-08T03:15:00"))
	g.Expect(result.UTCOffsetSeconds).To(Equal(-4 * 60 * 60))
}

func TestEvaluateCalendarRepeatedHourIsDeterministicWithoutIdentityCollision(t *testing.T) {
	g := NewWithT(t)
	version := calendarVersion(
		"America/New_York",
		types.MaintenanceWindowRule{
			ID: uuid.New(), Name: "fold window", Weekdays: []int32{int32(time.Sunday)},
			StartMinute: 60, EndMinute: 2 * 60,
		},
	)
	inputs := []types.CalendarEvaluationInput{
		{
			UTCInstant: mustTime(t, "2026-11-01T05:30:00Z"),
			IANAZone:   "America/New_York", RuleVersion: "2026a",
		},
		{
			UTCInstant: mustTime(t, "2026-11-01T06:30:00Z"),
			IANAZone:   "America/New_York", RuleVersion: "2026a",
		},
	}

	first, err := EvaluateCalendar(version, inputs[0])
	g.Expect(err).NotTo(HaveOccurred())
	repeated, err := EvaluateCalendar(version, inputs[0])
	g.Expect(err).NotTo(HaveOccurred())
	second, err := EvaluateCalendar(version, inputs[1])
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(first.Allowed).To(BeTrue())
	g.Expect(second.Allowed).To(BeTrue())
	g.Expect(first.LocalTime.Format("2006-01-02T15:04:05")).To(
		Equal(second.LocalTime.Format("2006-01-02T15:04:05")),
	)
	g.Expect(first.UTCOffsetSeconds).To(Equal(-4 * 60 * 60))
	g.Expect(second.UTCOffsetSeconds).To(Equal(-5 * 60 * 60))
	g.Expect(repeated.EvaluationIdentity).To(Equal(first.EvaluationIdentity))
	g.Expect(second.EvaluationIdentity).NotTo(Equal(first.EvaluationIdentity))
}

func TestEvaluateCalendarRejectsRuleBindingDrift(t *testing.T) {
	g := NewWithT(t)
	version := calendarVersion(
		"Asia/Bangkok",
		types.MaintenanceWindowRule{
			ID: uuid.New(), Name: "business hours", Weekdays: []int32{int32(time.Monday)},
			StartMinute: 9 * 60, EndMinute: 17 * 60,
		},
	)

	_, err := EvaluateCalendar(version, types.CalendarEvaluationInput{
		UTCInstant: mustTime(t, "2026-07-20T03:00:00Z"),
		IANAZone:   "Asia/Bangkok", RuleVersion: "2026b",
	})
	g.Expect(err).To(MatchError(ContainSubstring("rule version")))

	_, err = EvaluateCalendar(version, types.CalendarEvaluationInput{
		UTCInstant: mustTime(t, "2026-07-20T03:00:00Z"),
		IANAZone:   "UTC", RuleVersion: "2026a",
	})
	g.Expect(err).To(MatchError(ContainSubstring("IANA zone")))
}

func TestEvaluateCalendarRuleVersionUpdateChangesDecisionIdentity(t *testing.T) {
	g := NewWithT(t)
	rule := types.MaintenanceWindowRule{
		ID: uuid.New(), Name: "business hours", Weekdays: []int32{int32(time.Monday)},
		StartMinute: 9 * 60, EndMinute: 17 * 60,
	}
	firstVersion := calendarVersion("Asia/Bangkok", rule)
	secondVersion := firstVersion
	secondVersion.ID = uuid.New()
	secondVersion.RuleVersion = "2026b"
	instant := mustTime(t, "2026-07-20T03:00:00Z")

	first, err := EvaluateCalendar(firstVersion, types.CalendarEvaluationInput{
		UTCInstant: instant, IANAZone: "Asia/Bangkok", RuleVersion: "2026a",
	})
	g.Expect(err).NotTo(HaveOccurred())
	second, err := EvaluateCalendarWithZoneRules(testZoneRulesProvider{
		version:  "2026b",
		identity: "test-2026b",
		location: time.FixedZone("Asia/Bangkok", 7*60*60),
	}, secondVersion, types.CalendarEvaluationInput{
		UTCInstant: instant, IANAZone: "Asia/Bangkok", RuleVersion: "2026b",
	})
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(first.Allowed).To(BeTrue())
	g.Expect(second.Allowed).To(BeTrue())
	g.Expect(second.EvaluationIdentity).NotTo(Equal(first.EvaluationIdentity))
}

func TestCanonicalizeCalendarVersionIsOrderStableAndContentBound(t *testing.T) {
	g := NewWithT(t)
	firstRule := types.MaintenanceWindowRule{
		ID: uuid.New(), Name: "morning", Weekdays: []int32{1, 2, 3, 4, 5},
		StartMinute: 9 * 60, EndMinute: 12 * 60, SortOrder: 10,
	}
	secondRule := types.MaintenanceWindowRule{
		ID: uuid.New(), Name: "evening", Weekdays: []int32{1, 2, 3, 4, 5},
		StartMinute: 18 * 60, EndMinute: 21 * 60, SortOrder: 20,
	}
	version := calendarVersion("Asia/Bangkok", firstRule, secondRule)
	version.Name = "Production windows"
	version.Description = "Ordinary release admission"

	firstPayload, firstChecksum, err := CanonicalizeCalendarVersion(version)
	g.Expect(err).NotTo(HaveOccurred())

	reordered := version
	reordered.WindowRules = []types.MaintenanceWindowRule{secondRule, firstRule}
	secondPayload, secondChecksum, err := CanonicalizeCalendarVersion(reordered)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondPayload).To(Equal(firstPayload))
	g.Expect(secondChecksum).To(Equal(firstChecksum))

	versionScopedIDs := version
	versionScopedIDs.WindowRules = append(
		[]types.MaintenanceWindowRule(nil),
		version.WindowRules...,
	)
	for index := range versionScopedIDs.WindowRules {
		versionScopedIDs.WindowRules[index].VersionRuleID = uuid.New()
	}
	versionScopedPayload, versionScopedChecksum, err := CanonicalizeCalendarVersion(
		versionScopedIDs,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(versionScopedPayload).To(Equal(firstPayload))
	g.Expect(versionScopedChecksum).To(Equal(firstChecksum))

	reordered.WindowRules[0].EndMinute++
	_, changedChecksum, err := CanonicalizeCalendarVersion(reordered)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(changedChecksum).NotTo(Equal(firstChecksum))

	invalidOrder := version
	invalidOrder.WindowRules = append(
		[]types.MaintenanceWindowRule(nil),
		version.WindowRules...,
	)
	invalidOrder.WindowRules[0].SortOrder = -1
	_, _, err = CanonicalizeCalendarVersion(invalidOrder)
	g.Expect(err).To(MatchError(ContainSubstring("sort order")))

	duplicateName := version
	duplicateName.WindowRules = append(
		append([]types.MaintenanceWindowRule(nil), version.WindowRules...),
		version.WindowRules[0],
	)
	duplicateName.WindowRules[len(duplicateName.WindowRules)-1].ID = uuid.New()
	_, _, err = CanonicalizeCalendarVersion(duplicateName)
	g.Expect(err).To(MatchError(ContainSubstring("names")))
}

func TestEvaluateFreezeOverlapUsesHighestPriorityAndStableTieBreak(t *testing.T) {
	g := NewWithT(t)
	lowID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	highID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	tieID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	revisions := []types.DeploymentFreezeRevision{
		freezeRevision(lowID, 10, "ordinary freeze"),
		freezeRevision(highID, 20, "incident freeze"),
		freezeRevision(tieID, 20, "earlier stable identity"),
	}

	result := EvaluateFreeze(revisions, types.CalendarEvaluationInput{
		UTCInstant:  mustTime(t, "2026-07-20T10:00:00Z"),
		IANAZone:    "Asia/Bangkok",
		RuleVersion: "2026a",
	})

	g.Expect(result.Blocked).To(BeTrue())
	g.Expect(result.Allowed).To(BeFalse())
	g.Expect(result.ReasonCode).To(Equal(types.CalendarReasonFreezeActive))
	g.Expect(result.SelectedRevisionID).To(Equal(&tieID))
	g.Expect(result.ActiveRevisionIDs).To(Equal([]uuid.UUID{tieID, highID, lowID}))
	g.Expect(result.LocalTime.Format("2006-01-02T15:04:05")).To(Equal("2026-07-20T17:00:00"))
	g.Expect(result.UTCOffsetSeconds).To(Equal(7 * 60 * 60))
	g.Expect(result.EvaluationIdentity).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))

	inactive := EvaluateFreeze(revisions, types.CalendarEvaluationInput{
		UTCInstant: mustTime(t, "2026-07-21T10:00:00Z"),
		IANAZone:   "Asia/Bangkok", RuleVersion: "2026a",
	})
	g.Expect(inactive.Blocked).To(BeFalse())
	g.Expect(inactive.Allowed).To(BeTrue())
	g.Expect(inactive.ReasonCode).To(Equal(types.CalendarReasonNoActiveFreeze))
}

func TestCanonicalizeFreezeRevisionNormalizesExactInstants(t *testing.T) {
	g := NewWithT(t)
	revision := freezeRevision(uuid.New(), 20, "planned release freeze")
	revision.FreezeID = uuid.New()
	revision.ScopeKind = types.CalendarScopeEnvironment
	revision.ScopeID = uuid.New()
	revision.StartAt = mustTime(t, "2026-07-20T09:00:00+07:00")
	revision.EndAt = mustTime(t, "2026-07-20T11:00:00+07:00")

	firstPayload, firstChecksum, err := CanonicalizeFreezeRevision(revision)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(firstChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))

	sameInstants := revision
	sameInstants.StartAt = revision.StartAt.UTC()
	sameInstants.EndAt = revision.EndAt.UTC()
	secondPayload, secondChecksum, err := CanonicalizeFreezeRevision(sameInstants)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondPayload).To(Equal(firstPayload))
	g.Expect(secondChecksum).To(Equal(firstChecksum))

	sameInstants.Priority++
	_, changedChecksum, err := CanonicalizeFreezeRevision(sameInstants)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(changedChecksum).NotTo(Equal(firstChecksum))
}

func calendarVersion(
	zone string,
	rules ...types.MaintenanceWindowRule,
) types.MaintenanceCalendarVersion {
	return types.MaintenanceCalendarVersion{
		ID:             uuid.New(),
		CalendarID:     uuid.New(),
		OrganizationID: uuid.New(),
		VersionNumber:  1,
		Name:           "Calendar",
		IANAZone:       zone,
		RuleVersion:    "2026a",
		WindowRules:    rules,
	}
}

func freezeRevision(id uuid.UUID, priority int32, reason string) types.DeploymentFreezeRevision {
	return types.DeploymentFreezeRevision{
		ID:             id,
		FreezeID:       uuid.New(),
		OrganizationID: uuid.New(),
		VersionNumber:  1,
		Name:           "Freeze",
		StartAt:        mustTime(nil, "2026-07-20T09:00:00Z"),
		EndAt:          mustTime(nil, "2026-07-20T11:00:00Z"),
		IANAZone:       "Asia/Bangkok",
		RuleVersion:    "2026a",
		Priority:       priority,
		Reason:         reason,
	}
}

func mustTime(t *testing.T, value string) time.Time {
	if t != nil {
		t.Helper()
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		if t != nil {
			t.Fatalf("parse time: %v", err)
		}
		panic(err)
	}
	return parsed
}

type testZoneRulesProvider struct {
	version  string
	identity string
	location *time.Location
}

func (provider testZoneRulesProvider) RuleVersion() string {
	return provider.version
}

func (provider testZoneRulesProvider) Identity() string {
	return provider.identity
}

func (provider testZoneRulesProvider) LoadLocation(string) (*time.Location, error) {
	return provider.location, nil
}
