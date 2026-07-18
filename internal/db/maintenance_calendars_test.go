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
