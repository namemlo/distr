package db

import (
	"os"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestUncertainProjectionRecordsCampaignAuditAfterBothStateUpdates(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("execution_v2_projection.go")
	g.Expect(err).NotTo(HaveOccurred())
	body := string(source)
	memberUpdate := strings.Index(body, "executionV2UncertainMemberProjectionSQL")
	runUpdate := strings.Index(body[memberUpdate+1:], "executionV2UncertainRunProjectionSQL") + memberUpdate + 1
	audit := strings.Index(body[runUpdate+1:], "recordCampaignExecutionUncertainAudit") + runUpdate + 1
	g.Expect(memberUpdate).To(BeNumerically(">=", 0))
	g.Expect(runUpdate).To(BeNumerically(">", memberUpdate))
	g.Expect(audit).To(BeNumerically(">", runUpdate))
}

func TestExecutionV2TerminalProjectionMapsAttemptResultsWithoutInventingSuccess(t *testing.T) {
	g := NewWithT(t)

	cases := []struct {
		attempt types.ExecutionAttemptStatus
		step    types.StepRunStatus
	}{
		{types.ExecutionAttemptStatusSucceeded, types.StepRunStatusSucceeded},
		{types.ExecutionAttemptStatusFailed, types.StepRunStatusFailed},
		{types.ExecutionAttemptStatusCanceled, types.StepRunStatusFailed},
		{types.ExecutionAttemptStatusTimedOut, types.StepRunStatusFailed},
	}
	for _, tc := range cases {
		step, terminal := executionV2StepProjection(tc.attempt)
		g.Expect(terminal).To(BeTrue())
		g.Expect(step).To(Equal(tc.step))
	}

	for _, uncertain := range []types.ExecutionAttemptStatus{
		types.ExecutionAttemptStatusUnknown,
		types.ExecutionAttemptStatusFenced,
	} {
		_, terminal := executionV2StepProjection(uncertain)
		g.Expect(terminal).To(BeFalse())
	}
}

func TestExecutionProjectionSQLUsesExactTenantTaskAndCampaignLineage(t *testing.T) {
	g := NewWithT(t)

	for _, fragment := range []string{
		"attempt.organization_id = @organizationId",
		"attempt.task_id = @taskId",
		"attempt.step_run_id = @stepRunId",
		"CampaignMemberTaskExecution",
		"lineage.organization_id = @organizationId",
		"lineage.task_id = @taskId",
		"bool_and(task.status = 'SUCCEEDED')",
		"execution_uncertain = TRUE",
		"reconciliation_required = TRUE",
		"admissions_blocked = TRUE",
	} {
		g.Expect(strings.Join([]string{
			executionV2RunningProjectionSQL,
			executionV2TerminalProjectionSQL,
			executionV2UncertainProjectionSQL,
			campaignMemberExecutionProjectionSQL,
		}, "\n")).To(ContainSubstring(fragment))
	}
}

func TestExecutionV2ReconciledProjectionRecomputesMemberAndRunUncertainty(t *testing.T) {
	g := NewWithT(t)

	for _, fragment := range []string{
		"member.id = unresolved_member.campaign_member_run_id",
		"attempt.status IN ('UNKNOWN', 'FENCED')",
		"execution_uncertain = unresolved_member.has_uncertain_attempt",
		"COALESCE(bool_or(member.execution_uncertain), FALSE) AS any_uncertain",
		"reconciliation_required = aggregate.any_uncertain",
		"WHEN aggregate.any_uncertain THEN TRUE",
	} {
		g.Expect(executionV2ReconciledProjectionSQL).To(ContainSubstring(fragment))
	}
	g.Expect(executionV2ReconciledProjectionSQL).NotTo(ContainSubstring(
		"UPDATE DeploymentCampaignRun SET state = 'COMPLETED'",
	))
}

func TestExecutionV2ReconciledProjectionDoesNotRemoveGovernanceBlocks(t *testing.T) {
	g := NewWithT(t)

	for _, fragment := range []string{
		"campaign_run.state NOT IN ('SCHEDULED', 'RUNNING')",
		"campaign_run.pause_requested",
		"control.status = 'PENDING_RECONCILIATION'",
		"THEN TRUE",
	} {
		g.Expect(executionV2ReconciledProjectionSQL).To(ContainSubstring(fragment))
	}
}

func TestReadyStepQueryRequiresSatisfiedDependenciesAndNoExactAttempt(t *testing.T) {
	g := NewWithT(t)

	g.Expect(executionV2ReadyStepRunsSQL).To(ContainSubstring("dps.dependencies"))
	g.Expect(executionV2ReadyStepRunsSQL).To(ContainSubstring("dependency_run.status NOT IN"))
	g.Expect(executionV2ReadyStepRunsSQL).To(ContainSubstring("ExecutionAttempt"))
	g.Expect(executionV2ReadyStepRunsSQL).To(ContainSubstring("attempt.step_run_id = step_run.id"))
	g.Expect(executionV2ReadyStepRunsSQL).To(ContainSubstring("attempt.id IS NULL"))
}
