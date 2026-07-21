package db

import (
	"context"
	"fmt"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxExecutionV2ReadyDispatchTaskBatchSize = 100

const executionV2ReadyDispatchTasksSQL = `
WITH ready_task AS (
  SELECT
    t.*,
    row_number() OVER (
      PARTITION BY t.organization_id
      ORDER BY t.queue_order, t.id
    ) AS tenant_position
  FROM Task AS t
  WHERE t.protocol_version = 'v2'
    AND t.status IN ('QUEUED', 'RUNNING')
    AND EXISTS (
      SELECT 1
      FROM StepRun AS step_run
      JOIN DeploymentPlanStep AS plan_step
        ON plan_step.id = step_run.deployment_plan_step_id
       AND plan_step.deployment_plan_id = step_run.deployment_plan_id
       AND plan_step.organization_id = step_run.organization_id
      LEFT JOIN ExecutionAttempt AS attempt
        ON attempt.organization_id = step_run.organization_id
       AND attempt.task_id = step_run.task_id
       AND attempt.step_run_id = step_run.id
      WHERE step_run.organization_id = t.organization_id
        AND step_run.task_id = t.id
        AND step_run.status = 'PENDING'
        AND plan_step.included
        AND lower(btrim(plan_step.execution_location)) = 'target'
        AND attempt.id IS NULL
        AND NOT EXISTS (
          SELECT 1
          FROM unnest(plan_step.dependencies) dependency(step_key)
          LEFT JOIN StepRun AS dependency_run
            ON dependency_run.organization_id = step_run.organization_id
           AND dependency_run.task_id = step_run.task_id
           AND dependency_run.step_key = dependency.step_key
          WHERE dependency_run.id IS NULL
             OR dependency_run.status NOT IN ('SUCCEEDED', 'SKIPPED')
        )
    )
)
SELECT ` + taskOutputExpr + `
FROM ready_task AS t
ORDER BY t.tenant_position, t.queue_order, t.organization_id, t.id
LIMIT @limit`

const executionV2ReadyDispatchStepRunsSQL = `
SELECT
  step_run.id, step_run.created_at, step_run.updated_at,
  step_run.started_at, step_run.completed_at,
  step_run.organization_id, step_run.task_id,
  step_run.deployment_plan_id, step_run.deployment_plan_step_id,
  step_run.step_key, step_run.name, step_run.action_type,
  step_run.status, step_run.sort_order, step_run.skipped_reason
FROM StepRun AS step_run
JOIN Task AS task
  ON task.id = step_run.task_id
 AND task.organization_id = step_run.organization_id
JOIN DeploymentPlanStep AS plan_step
  ON plan_step.id = step_run.deployment_plan_step_id
 AND plan_step.deployment_plan_id = step_run.deployment_plan_id
 AND plan_step.organization_id = step_run.organization_id
LEFT JOIN ExecutionAttempt AS attempt
  ON attempt.organization_id = step_run.organization_id
 AND attempt.task_id = step_run.task_id
 AND attempt.step_run_id = step_run.id
WHERE step_run.task_id = ANY(@taskIds)
  AND step_run.status = 'PENDING'
  AND task.status IN ('QUEUED', 'RUNNING')
  AND task.protocol_version = 'v2'
  AND plan_step.included
  AND lower(btrim(plan_step.execution_location)) = 'target'
  AND attempt.id IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM unnest(plan_step.dependencies) dependency(step_key)
    LEFT JOIN StepRun AS dependency_run
      ON dependency_run.organization_id = step_run.organization_id
     AND dependency_run.task_id = step_run.task_id
     AND dependency_run.step_key = dependency.step_key
    WHERE dependency_run.id IS NULL
       OR dependency_run.status NOT IN ('SUCCEEDED', 'SKIPPED')
  )
ORDER BY step_run.task_id, step_run.sort_order, step_run.step_key, step_run.id`

// ListExecutionV2ReadyDispatchTasks returns a bounded, tenant-fair batch of
// durable tasks that contain at least one dependency-ready step without an
// exact execution attempt. The dispatcher rechecks the same step predicate,
// which makes recovery safe to replay after a lost post-commit handoff.
func ListExecutionV2ReadyDispatchTasks(ctx context.Context, limit int) ([]types.Task, error) {
	if limit <= 0 || limit > maxExecutionV2ReadyDispatchTaskBatchSize {
		return nil, fmt.Errorf(
			"execution v2 ready-task batch limit must be between 1 and %d",
			maxExecutionV2ReadyDispatchTaskBatchSize,
		)
	}
	rows, err := internalctx.GetDb(ctx).Query(
		ctx,
		executionV2ReadyDispatchTasksSQL,
		pgx.NamedArgs{"limit": limit},
	)
	if err != nil {
		return nil, fmt.Errorf("list execution v2 ready dispatch tasks: %w", err)
	}
	tasks, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.Task])
	if err != nil {
		return nil, fmt.Errorf("collect execution v2 ready dispatch tasks: %w", err)
	}
	if len(tasks) == 0 {
		return tasks, nil
	}
	taskIDs := make([]uuid.UUID, len(tasks))
	byID := make(map[uuid.UUID]int, len(tasks))
	for index := range tasks {
		taskIDs[index] = tasks[index].ID
		byID[tasks[index].ID] = index
	}
	stepRows, err := internalctx.GetDb(ctx).Query(
		ctx,
		executionV2ReadyDispatchStepRunsSQL,
		pgx.NamedArgs{"taskIds": taskIDs},
	)
	if err != nil {
		return nil, fmt.Errorf("list execution v2 ready dispatch steps: %w", err)
	}
	steps, err := pgx.CollectRows(stepRows, pgx.RowToStructByName[types.StepRun])
	if err != nil {
		return nil, fmt.Errorf("collect execution v2 ready dispatch steps: %w", err)
	}
	for _, step := range steps {
		index, ok := byID[step.TaskID]
		if !ok {
			return nil, fmt.Errorf("ready execution step does not belong to the selected task batch")
		}
		tasks[index].StepRuns = append(tasks[index].StepRuns, step)
	}
	return tasks, nil
}
