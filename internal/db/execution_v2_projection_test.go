package db

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

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

func TestReadyStepQueryRequiresSatisfiedDependenciesAndNoExactAttempt(t *testing.T) {
	g := NewWithT(t)

	g.Expect(executionV2ReadyStepRunsSQL).To(ContainSubstring("dps.dependencies"))
	g.Expect(executionV2ReadyStepRunsSQL).To(ContainSubstring("dependency_run.status NOT IN"))
	g.Expect(executionV2ReadyStepRunsSQL).To(ContainSubstring("ExecutionAttempt"))
	g.Expect(executionV2ReadyStepRunsSQL).To(ContainSubstring("attempt.step_run_id = step_run.id"))
	g.Expect(executionV2ReadyStepRunsSQL).To(ContainSubstring("attempt.id IS NULL"))
}
