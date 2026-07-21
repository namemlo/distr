package db

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
)

func TestExecutionV2ReadyTaskRecoveryQueryIsBoundedStableFairAndExact(t *testing.T) {
	g := NewWithT(t)
	for _, fragment := range []string{
		"t.protocol_version = 'v2'",
		"t.status IN ('QUEUED', 'RUNNING')",
		"step_run.status = 'PENDING'",
		"attempt.step_run_id = step_run.id",
		"attempt.id IS NULL",
		"FROM unnest(plan_step.dependencies)",
		"dependency_run.status NOT IN ('SUCCEEDED', 'SKIPPED')",
		"row_number() OVER (",
		"PARTITION BY t.organization_id",
		"ORDER BY t.tenant_position, t.queue_order, t.organization_id, t.id",
		"LIMIT @limit",
	} {
		g.Expect(executionV2ReadyDispatchTasksSQL).To(ContainSubstring(fragment))
	}
}

func TestExecutionV2ReadyTaskRecoveryLoadsEverySelectedReadyStepInOneBatch(t *testing.T) {
	g := NewWithT(t)
	for _, fragment := range []string{
		"step_run.task_id = ANY(@taskIds)",
		"step_run.status = 'PENDING'",
		"attempt.step_run_id = step_run.id",
		"attempt.id IS NULL",
		"FROM unnest(plan_step.dependencies)",
		"ORDER BY step_run.task_id, step_run.sort_order, step_run.step_key, step_run.id",
	} {
		g.Expect(executionV2ReadyDispatchStepRunsSQL).To(ContainSubstring(fragment))
	}
}

func TestExecutionV2ReadyTaskRecoveryRejectsNonPositiveBatch(t *testing.T) {
	g := NewWithT(t)
	_, err := ListExecutionV2ReadyDispatchTasks(context.Background(), 0)
	g.Expect(err).To(MatchError(ContainSubstring("batch limit")))
}

func TestExecutionV2ReadyTaskRecoveryRejectsUnboundedBatch(t *testing.T) {
	g := NewWithT(t)
	_, err := ListExecutionV2ReadyDispatchTasks(
		context.Background(),
		maxExecutionV2ReadyDispatchTaskBatchSize+1,
	)
	g.Expect(err).To(MatchError(ContainSubstring("batch limit")))
}
