package mapping

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/scheduling"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestMaintenanceCalendarMappingsHideTenantAndCanonicalPayload(t *testing.T) {
	g := NewWithT(t)
	now := time.Now().UTC()
	organizationID := uuid.New()
	calendarID := uuid.New()
	versionID := uuid.New()
	ruleID := uuid.New()

	calendar := types.MaintenanceCalendar{
		ID:                     calendarID,
		CreatedAt:              now,
		UpdatedAt:              now,
		OrganizationID:         organizationID,
		Name:                   "Retail production",
		Description:            "Asia deployment windows",
		DraftIANAZone:          "Asia/Bangkok",
		DraftRuleVersion:       "tzdb-2026a",
		DraftRevision:          3,
		LastPublishedVersionID: &versionID,
		CreatedBy:              uuid.New(),
		UpdatedBy:              uuid.New(),
		DraftRules: []types.MaintenanceWindowRule{{
			ID:          ruleID,
			Name:        "weekday evening",
			Weekdays:    []int32{1, 2, 3, 4, 5},
			StartMinute: 20 * 60,
			EndMinute:   22 * 60,
			SortOrder:   1,
		}},
	}
	version := types.MaintenanceCalendarVersion{
		ID:                  versionID,
		CalendarID:          calendarID,
		OrganizationID:      organizationID,
		VersionNumber:       2,
		SourceDraftRevision: 3,
		Name:                calendar.Name,
		Description:         calendar.Description,
		IANAZone:            calendar.DraftIANAZone,
		RuleVersion:         calendar.DraftRuleVersion,
		CanonicalPayload:    []byte(`{"secret":"must-not-leak"}`),
		Checksum:            "sha256:calendar",
		PublishedAt:         now,
		PublishedBy:         uuid.New(),
		WindowRules: append(
			[]types.MaintenanceWindowRule(nil),
			calendar.DraftRules...,
		),
	}
	version.WindowRules[0].VersionRuleID = uuid.New()
	calendar.DraftRules[0].VersionRuleID = uuid.New()
	nextVersion := version
	nextVersion.ID = uuid.New()
	nextVersion.WindowRules = append(
		[]types.MaintenanceWindowRule(nil),
		version.WindowRules...,
	)
	nextVersion.WindowRules[0].VersionRuleID = uuid.New()

	calendarResponse := MaintenanceCalendarToAPI(calendar)
	versionResponse := MaintenanceCalendarVersionToAPI(version)
	pageResponse := MaintenanceCalendarPageToAPI(types.Page[types.MaintenanceCalendar]{
		Items:      []types.MaintenanceCalendar{calendar},
		NextCursor: "next",
	})

	g.Expect(calendarResponse.DraftRules).To(HaveLen(1))
	g.Expect(calendarResponse.DraftRules[0].ID).To(Equal(ruleID))
	g.Expect(calendarResponse.DraftRules[0].VersionRuleID).To(BeNil())
	g.Expect(versionResponse.WindowRules).To(HaveLen(1))
	g.Expect(MaintenanceCalendarVersionToAPI(nextVersion).WindowRules[0].ID).To(
		Equal(ruleID),
	)
	g.Expect(nextVersion.WindowRules[0].VersionRuleID).NotTo(
		Equal(version.WindowRules[0].VersionRuleID),
	)
	g.Expect(versionResponse.Checksum).To(Equal("sha256:calendar"))
	g.Expect(pageResponse.NextCursor).To(Equal("next"))

	payload, err := json.Marshal(struct {
		Calendar any `json:"calendar"`
		Version  any `json:"version"`
		Page     any `json:"page"`
	}{calendarResponse, versionResponse, pageResponse})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring(organizationID.String()))
	g.Expect(string(payload)).NotTo(ContainSubstring("must-not-leak"))
}

func TestPublishedWindowRuleMappingCorrelatesEvaluationEvidence(t *testing.T) {
	g := NewWithT(t)
	logicalID := uuid.New()
	versionRuleID := uuid.New()
	version := types.MaintenanceCalendarVersion{
		ID: uuid.New(), CalendarID: uuid.New(), Name: "Production",
		IANAZone: "UTC", RuleVersion: "2026a",
		WindowRules: []types.MaintenanceWindowRule{{
			ID: logicalID, VersionRuleID: versionRuleID, Name: "Monday",
			Weekdays: []int32{int32(time.Monday)}, StartMinute: 0, EndMinute: 24 * 60,
		}},
	}

	response := MaintenanceCalendarVersionToAPI(version)
	evaluation, err := scheduling.EvaluateCalendar(
		version,
		types.CalendarEvaluationInput{
			UTCInstant:  time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC),
			IANAZone:    "UTC",
			RuleVersion: "2026a",
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(response.WindowRules[0].ID).To(Equal(logicalID))
	g.Expect(response.WindowRules[0].VersionRuleID).To(Equal(&versionRuleID))
	g.Expect(evaluation.WindowRuleID).To(Equal(response.WindowRules[0].VersionRuleID))

	payload, err := json.Marshal(response.WindowRules[0])
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).To(ContainSubstring(
		`"versionRuleId":"` + versionRuleID.String() + `"`,
	))
}

func TestDeploymentFreezeMappingsExposeScopeWithoutTenant(t *testing.T) {
	g := NewWithT(t)
	now := time.Now().UTC()
	organizationID := uuid.New()
	freezeID := uuid.New()
	revisionID := uuid.New()
	scopeID := uuid.New()

	freeze := types.DeploymentFreeze{
		ID:                      freezeID,
		CreatedAt:               now,
		UpdatedAt:               now,
		OrganizationID:          organizationID,
		Name:                    "Retail settlement close",
		DraftStartAt:            now,
		DraftEndAt:              now.Add(time.Hour),
		DraftIANAZone:           "Asia/Bangkok",
		DraftRuleVersion:        "tzdb-2026a",
		DraftScopeKind:          types.CalendarScopeEnvironment,
		DraftScopeID:            scopeID,
		DraftPriority:           100,
		DraftReason:             "settlement",
		DraftRevision:           4,
		LastPublishedRevisionID: &revisionID,
		CreatedBy:               uuid.New(),
		UpdatedBy:               uuid.New(),
	}
	revision := types.DeploymentFreezeRevision{
		ID:                  revisionID,
		FreezeID:            freezeID,
		OrganizationID:      organizationID,
		VersionNumber:       3,
		SourceDraftRevision: 4,
		Name:                freeze.Name,
		StartAt:             freeze.DraftStartAt,
		EndAt:               freeze.DraftEndAt,
		IANAZone:            freeze.DraftIANAZone,
		RuleVersion:         freeze.DraftRuleVersion,
		ScopeKind:           freeze.DraftScopeKind,
		ScopeID:             freeze.DraftScopeID,
		Priority:            freeze.DraftPriority,
		Reason:              freeze.DraftReason,
		CanonicalPayload:    []byte(`{"tenant":"must-not-leak"}`),
		Checksum:            "sha256:freeze",
		PublishedAt:         now,
		PublishedBy:         uuid.New(),
	}

	freezeResponse := DeploymentFreezeToAPI(freeze)
	revisionResponse := DeploymentFreezeRevisionToAPI(revision)
	pageResponse := DeploymentFreezeRevisionPageToAPI(
		types.Page[types.DeploymentFreezeRevision]{
			Items:      []types.DeploymentFreezeRevision{revision},
			NextCursor: "next",
		},
	)

	g.Expect(freezeResponse.DraftScope).To(Equal(types.CalendarScopeRef{
		Kind: types.CalendarScopeEnvironment,
		ID:   scopeID,
	}))
	g.Expect(revisionResponse.Scope).To(Equal(freezeResponse.DraftScope))
	g.Expect(pageResponse.NextCursor).To(Equal("next"))

	payload, err := json.Marshal(struct {
		Freeze   any `json:"freeze"`
		Revision any `json:"revision"`
		Page     any `json:"page"`
	}{freezeResponse, revisionResponse, pageResponse})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring(organizationID.String()))
	g.Expect(string(payload)).NotTo(ContainSubstring("must-not-leak"))
}
