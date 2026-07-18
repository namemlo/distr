package db

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestMigration151DefinesVersionedCalendarsAndFreezes(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile("../migrations/sql/151_maintenance_calendars_freezes.up.sql")
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)

	for _, table := range []string{
		"MaintenanceCalendar",
		"MaintenanceCalendarVersion",
		"MaintenanceWindowRule",
		"DeploymentFreeze",
		"DeploymentFreezeRevision",
	} {
		g.Expect(sql).To(ContainSubstring("CREATE TABLE " + table))
	}
	g.Expect(sql).To(ContainSubstring("TIMESTAMPTZ"))
	g.Expect(sql).To(ContainSubstring("draft_revision BIGINT NOT NULL"))
	g.Expect(sql).To(ContainSubstring("canonical_payload BYTEA NOT NULL"))
	g.Expect(sql).To(ContainSubstring("checksum TEXT NOT NULL CHECK"))
	g.Expect(sql).To(ContainSubstring("iana_zone TEXT NOT NULL"))
	g.Expect(sql).To(ContainSubstring("rule_version TEXT NOT NULL"))
	g.Expect(sql).To(ContainSubstring("MaintenanceCalendarVersion_immutable"))
	g.Expect(sql).To(ContainSubstring("MaintenanceWindowRule_immutable"))
	g.Expect(sql).To(ContainSubstring("DeploymentFreezeRevision_immutable"))
	g.Expect(sql).To(ContainSubstring(
		"'distr.deployment_registry_deletion_reason'",
	))
	g.Expect(sql).To(ContainSubstring("'ORGANIZATION_RETENTION'"))
	g.Expect(sql).To(ContainSubstring("organization_id, created_at DESC, id DESC"))
	g.Expect(sql).To(ContainSubstring("maintenancecalendarversion_draft_unique"))
	g.Expect(sql).To(ContainSubstring("maintenancewindowrule_name_unique"))
	g.Expect(sql).To(ContainSubstring("logical_rule_id UUID NOT NULL"))
	g.Expect(sql).To(ContainSubstring("maintenancewindowrule_logical_unique"))
	g.Expect(sql).To(ContainSubstring("deploymentfreezerevision_draft_unique"))
	g.Expect(sql).To(ContainSubstring(
		"FOREIGN KEY (last_published_version_id, organization_id, id)",
	))
	g.Expect(sql).To(ContainSubstring(
		"FOREIGN KEY (last_published_revision_id, organization_id, id)",
	))
	g.Expect(strings.ToLower(sql)).NotTo(ContainSubstring("secret_value"))

	down, err := os.ReadFile("../migrations/sql/151_maintenance_calendars_freezes.down.sql")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("downgrade crossing 151 is forbidden"))
}

func TestPublishedWindowRuleIdentityIsVersionScopedAndLogicalIDIsStable(t *testing.T) {
	g := NewWithT(t)
	logicalID := uuid.New()
	first := types.MaintenanceCalendarVersion{
		ID: uuid.New(), OrganizationID: uuid.New(),
		WindowRules: []types.MaintenanceWindowRule{{ID: logicalID}},
	}
	second := first
	second.ID = uuid.New()
	second.WindowRules = append([]types.MaintenanceWindowRule(nil), first.WindowRules...)

	assignVersionScopedWindowRuleIDs(&first)
	assignVersionScopedWindowRuleIDs(&second)
	g.Expect(first.WindowRules[0].ID).To(Equal(logicalID))
	g.Expect(second.WindowRules[0].ID).To(Equal(logicalID))
	g.Expect(first.WindowRules[0].VersionRuleID).NotTo(Equal(uuid.Nil))
	g.Expect(second.WindowRules[0].VersionRuleID).NotTo(Equal(
		first.WindowRules[0].VersionRuleID,
	))

	repeated := first
	repeated.WindowRules = []types.MaintenanceWindowRule{{ID: logicalID}}
	assignVersionScopedWindowRuleIDs(&repeated)
	g.Expect(repeated.WindowRules[0].VersionRuleID).To(Equal(
		first.WindowRules[0].VersionRuleID,
	))
}

func TestMaintenanceWindowRulesAreGroupedForOneBatchQuery(t *testing.T) {
	g := NewWithT(t)
	firstID := uuid.New()
	secondID := uuid.New()
	firstRule := types.MaintenanceWindowRule{
		ID: uuid.New(), CalendarVersionID: firstID, SortOrder: 1,
	}
	secondRule := types.MaintenanceWindowRule{
		ID: uuid.New(), CalendarVersionID: secondID, SortOrder: 2,
	}

	grouped := groupMaintenanceWindowRules(
		[]uuid.UUID{firstID, secondID},
		[]types.MaintenanceWindowRule{firstRule, secondRule},
	)
	g.Expect(grouped[firstID]).To(Equal([]types.MaintenanceWindowRule{firstRule}))
	g.Expect(grouped[secondID]).To(Equal([]types.MaintenanceWindowRule{secondRule}))

	source, err := os.ReadFile("maintenance_calendars.go")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(source)).To(ContainSubstring(
		"r.calendar_version_id = ANY(@versionIDs)",
	))
}

func TestHistoricalPublishReplayWinsAfterDraftAdvances(t *testing.T) {
	g := NewWithT(t)
	existing := types.MaintenanceCalendarVersion{
		ID: uuid.New(), SourceDraftRevision: 7, Checksum: "sha256:published",
	}

	replay, found, err := resolvePublicationReplay(
		&existing,
		nil,
		8,
		7,
		"maintenance calendar",
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(replay.ID).To(Equal(existing.ID))
	g.Expect(replay.Checksum).To(Equal(existing.Checksum))

	_, found, err = resolvePublicationReplay[types.MaintenanceCalendarVersion](
		nil,
		apierrors.ErrNotFound,
		8,
		7,
		"maintenance calendar",
	)
	g.Expect(found).To(BeFalse())
	g.Expect(err).To(MatchError(apierrors.ErrConflict))
}

func TestCalendarCursorRoundTripAndValidation(t *testing.T) {
	g := NewWithT(t)
	cursor := calendarCursor{
		Version:   calendarCursorVersion,
		CreatedAt: mustCalendarTime(t, "2026-07-20T09:00:00Z"),
		ID:        uuid.New(),
	}

	encoded, err := encodeCalendarCursor(cursor)
	g.Expect(err).NotTo(HaveOccurred())
	decoded, err := decodeCalendarCursor(encoded)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decoded).To(Equal(&cursor))

	_, err = decodeCalendarCursor("not-a-cursor")
	g.Expect(err).To(MatchError(ContainSubstring("cursor")))
	g.Expect(err).To(MatchError(apierrors.ErrBadRequest))
}

func TestNormalizeCalendarListFilterUsesBoundedServerDefaults(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()

	limit, cursor, err := normalizeCalendarListFilter(types.CalendarListFilter{
		OrganizationID: organizationID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(limit).To(Equal(calendarDefaultPageLimit))
	g.Expect(cursor).To(BeNil())

	_, _, err = normalizeCalendarListFilter(types.CalendarListFilter{
		OrganizationID: organizationID,
		Limit:          calendarMaximumPageLimit + 1,
	})
	g.Expect(err).To(MatchError(apierrors.ErrBadRequest))

	_, _, err = normalizeCalendarListFilter(types.CalendarListFilter{
		OrganizationID: organizationID,
		Cursor:         strings.Repeat("a", calendarMaximumCursorSize+1),
	})
	g.Expect(err).To(MatchError(apierrors.ErrBadRequest))

	_, _, err = normalizeCalendarListFilter(types.CalendarListFilter{})
	g.Expect(err).To(MatchError(apierrors.ErrBadRequest))
}

func TestCalendarDraftValidationProtectsImmutablePublishInputs(t *testing.T) {
	g := NewWithT(t)
	calendar := types.MaintenanceCalendar{
		OrganizationID:   uuid.New(),
		Name:             "Production",
		DraftIANAZone:    "Asia/Bangkok",
		DraftRuleVersion: "2026a",
		DraftRules: []types.MaintenanceWindowRule{{
			ID: uuid.New(), Name: "weekday", Weekdays: []int32{1, 2, 3, 4, 5},
			StartMinute: 9 * 60, EndMinute: 17 * 60,
		}},
		CreatedBy: uuid.New(),
		UpdatedBy: uuid.New(),
	}
	g.Expect(validateMaintenanceCalendarForWrite(calendar)).To(Succeed())

	calendar.DraftRuleVersion = ""
	g.Expect(validateMaintenanceCalendarForWrite(calendar)).To(
		MatchError(ContainSubstring("rule version")),
	)
}

func TestFreezeDraftValidationRejectsForeignOrganizationScopeIdentity(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	freeze := types.DeploymentFreeze{
		OrganizationID:   organizationID,
		Name:             "Quarter close",
		DraftStartAt:     mustCalendarTime(t, "2026-07-20T09:00:00Z"),
		DraftEndAt:       mustCalendarTime(t, "2026-07-20T11:00:00Z"),
		DraftIANAZone:    "Asia/Bangkok",
		DraftRuleVersion: "2026a",
		DraftScopeKind:   types.CalendarScopeOrganization,
		DraftScopeID:     uuid.New(),
		DraftPriority:    10,
		DraftReason:      "Financial close",
		CreatedBy:        uuid.New(),
		UpdatedBy:        uuid.New(),
	}
	g.Expect(validateDeploymentFreezeForWrite(freeze)).To(
		MatchError(ContainSubstring("organization scope")),
	)

	freeze.DraftScopeID = organizationID
	g.Expect(validateDeploymentFreezeForWrite(freeze)).To(Succeed())
}

func mustCalendarTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return parsed
}
