package api

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestMaintenanceCalendarListRequestValidation(t *testing.T) {
	g := NewWithT(t)
	g.Expect((MaintenanceCalendarListRequest{}).Validate()).To(Succeed())
	g.Expect((MaintenanceCalendarListRequest{Limit: 100, Cursor: "eyJ2IjoxfQ"}).Validate()).
		To(Succeed())
	g.Expect((MaintenanceCalendarListRequest{Limit: -1}).Validate()).To(HaveOccurred())
	g.Expect((MaintenanceCalendarListRequest{Limit: 101}).Validate()).To(HaveOccurred())
	g.Expect((MaintenanceCalendarListRequest{Cursor: "invalid cursor!"}).Validate()).
		To(HaveOccurred())
	g.Expect((MaintenanceCalendarListRequest{Cursor: strings.Repeat("a", 2049)}).Validate()).
		To(HaveOccurred())
}

func TestMaintenanceCalendarDraftRequestsValidateExactRules(t *testing.T) {
	g := NewWithT(t)
	valid := CreateMaintenanceCalendarRequest{
		Name:        "Production windows",
		Description: "Approved weekdays",
		IANAZone:    "Asia/Bangkok",
		RuleVersion: "2026a",
		WindowRules: []MaintenanceWindowRuleRequest{{
			Name: "weekday", Weekdays: []int32{1, 2, 3, 4, 5},
			StartMinute: 9 * 60, EndMinute: 17 * 60, SortOrder: 10,
		}},
	}
	g.Expect(valid.Validate()).To(Succeed())
	g.Expect(valid.ToDomain(uuid.New(), uuid.New()).DraftRules).To(HaveLen(1))

	invalidZone := valid
	invalidZone.IANAZone = "Not/A_Zone"
	g.Expect(invalidZone.Validate()).To(MatchError(ContainSubstring("IANA")))

	duplicateWeekday := valid
	duplicateWeekday.WindowRules = append(
		[]MaintenanceWindowRuleRequest(nil),
		valid.WindowRules...,
	)
	duplicateWeekday.WindowRules[0].Weekdays = []int32{1, 1}
	g.Expect(duplicateWeekday.Validate()).To(MatchError(ContainSubstring("unique")))

	equalTimes := valid
	equalTimes.WindowRules = append([]MaintenanceWindowRuleRequest(nil), valid.WindowRules...)
	equalTimes.WindowRules[0].EndMinute = equalTimes.WindowRules[0].StartMinute
	g.Expect(equalTimes.Validate()).To(MatchError(ContainSubstring("differ")))

	negativeOrder := valid
	negativeOrder.WindowRules = append([]MaintenanceWindowRuleRequest(nil), valid.WindowRules...)
	negativeOrder.WindowRules[0].SortOrder = -1
	g.Expect(negativeOrder.Validate()).To(MatchError(ContainSubstring("sortOrder")))

	duplicateName := valid
	duplicateName.WindowRules = append(
		append([]MaintenanceWindowRuleRequest(nil), valid.WindowRules...),
		valid.WindowRules[0],
	)
	g.Expect(duplicateName.Validate()).To(MatchError(ContainSubstring("names")))

	tooLong := valid
	tooLong.Name = strings.Repeat("n", 201)
	g.Expect(tooLong.Validate()).To(MatchError(ContainSubstring("name")))

	update := UpdateMaintenanceCalendarRequest{
		ExpectedDraftRevision: 0,
		Name:                  valid.Name,
		IANAZone:              valid.IANAZone,
		RuleVersion:           valid.RuleVersion,
		WindowRules:           valid.WindowRules,
	}
	g.Expect(update.Validate()).To(MatchError(ContainSubstring("expectedDraftRevision")))
	update.ExpectedDraftRevision = 1
	g.Expect(update.Validate()).To(Succeed())
}

func TestPublishCalendarRequestRequiresOptimisticRevision(t *testing.T) {
	g := NewWithT(t)
	g.Expect((PublishMaintenanceCalendarRequest{}).Validate()).To(HaveOccurred())
	g.Expect((PublishMaintenanceCalendarRequest{ExpectedDraftRevision: 1}).Validate()).To(Succeed())
}

func TestDeploymentFreezeDraftRequestsValidateScopeAndInterval(t *testing.T) {
	g := NewWithT(t)
	start := calendarAPITime(t, "2026-07-20T09:00:00Z")
	end := calendarAPITime(t, "2026-07-20T11:00:00Z")
	valid := CreateDeploymentFreezeRequest{
		Name:        "Quarter close",
		StartAt:     start,
		EndAt:       end,
		IANAZone:    "Asia/Bangkok",
		RuleVersion: "2026a",
		ScopeKind:   types.CalendarScopeEnvironment,
		ScopeID:     uuid.New(),
		Priority:    20,
		Reason:      "Financial close",
	}
	g.Expect(valid.Validate()).To(Succeed())

	badInterval := valid
	badInterval.EndAt = start
	g.Expect(badInterval.Validate()).To(MatchError(ContainSubstring("after")))

	badScope := valid
	badScope.ScopeKind = "server"
	g.Expect(badScope.Validate()).To(MatchError(ContainSubstring("scope")))

	campaignScope := valid
	campaignScope.ScopeKind = types.CalendarScopeCampaign
	g.Expect(campaignScope.Validate()).To(MatchError(ContainSubstring("campaign")))

	badPriority := valid
	badPriority.Priority = -1
	g.Expect(badPriority.Validate()).To(MatchError(ContainSubstring("priority")))

	update := UpdateDeploymentFreezeRequest{
		ExpectedDraftRevision: 1,
		Name:                  valid.Name,
		StartAt:               valid.StartAt,
		EndAt:                 valid.EndAt,
		IANAZone:              valid.IANAZone,
		RuleVersion:           valid.RuleVersion,
		ScopeKind:             valid.ScopeKind,
		ScopeID:               valid.ScopeID,
		Priority:              valid.Priority,
		Reason:                valid.Reason,
	}
	g.Expect(update.Validate()).To(Succeed())
}

func calendarAPITime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return parsed
}
