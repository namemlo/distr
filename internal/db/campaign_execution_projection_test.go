package db

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestCampaignExecutionProjectionAggregatesAllBoundTasksBeforeMemberSuccess(t *testing.T) {
	g := NewWithT(t)

	g.Expect(campaignMemberExecutionProjectionSQL).To(ContainSubstring(
		"GROUP BY lineage.organization_id, lineage.campaign_member_run_id",
	))
	g.Expect(campaignMemberExecutionProjectionSQL).To(ContainSubstring(
		"bool_and(task.status = 'SUCCEEDED')",
	))
	g.Expect(campaignMemberExecutionProjectionSQL).To(ContainSubstring(
		"count(*) FILTER (WHERE task.status NOT IN ('SUCCEEDED', 'FAILED', 'CANCELED'))",
	))
	g.Expect(campaignMemberExecutionProjectionSQL).To(ContainSubstring(
		"member.id = aggregate.campaign_member_run_id",
	))
	g.Expect(campaignMemberExecutionProjectionSQL).To(ContainSubstring(
		"bool_and(attempt.cancellable)",
	))
	activeAt := strings.Index(campaignMemberExecutionProjectionSQL, "WHEN aggregate.active > 0")
	failedAt := strings.Index(campaignMemberExecutionProjectionSQL, "WHEN aggregate.any_failed")
	canceledAt := strings.Index(campaignMemberExecutionProjectionSQL, "WHEN aggregate.any_canceled")
	g.Expect(activeAt).To(BeNumerically(">=", 0))
	g.Expect(failedAt).To(BeNumerically(">", activeAt))
	g.Expect(canceledAt).To(BeNumerically(">", activeAt))
}

func TestCampaignWaveProjectionDoesNotCompleteUntilEveryMemberIsTerminal(t *testing.T) {
	g := NewWithT(t)

	g.Expect(campaignWaveExecutionProjectionSQL).To(ContainSubstring(
		"bool_and(member.status IN ('SUCCEEDED', 'FAILED', 'EXCLUDED', 'CANCELED'))",
	))
	g.Expect(campaignWaveExecutionProjectionSQL).To(ContainSubstring(
		"bool_and(member.status IN ('SUCCEEDED', 'EXCLUDED'))",
	))
	g.Expect(campaignWaveExecutionProjectionSQL).NotTo(ContainSubstring(
		"UPDATE DeploymentCampaignRun SET state = 'COMPLETED'",
	))
	g.Expect(campaignWaveExecutionProjectionSQL).To(ContainSubstring(
		"member.organization_id = @organizationId",
	))
	g.Expect(campaignWaveExecutionProjectionSQL).To(ContainSubstring(
		"member.wave_run_id = @waveRunId",
	))
}
