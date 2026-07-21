package db

import (
	"context"
	"fmt"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const executionV2ReadyStepRunsSQL = `
SELECT
  step_run.id, step_run.created_at, step_run.updated_at,
  step_run.started_at, step_run.completed_at,
  step_run.organization_id, step_run.task_id,
  step_run.deployment_plan_id, step_run.deployment_plan_step_id,
  step_run.step_key, step_run.name, step_run.action_type,
  step_run.status, step_run.sort_order, step_run.skipped_reason
FROM StepRun step_run
JOIN Task task
  ON task.id = step_run.task_id
 AND task.organization_id = step_run.organization_id
JOIN DeploymentPlanStep dps
  ON dps.id = step_run.deployment_plan_step_id
 AND dps.deployment_plan_id = step_run.deployment_plan_id
 AND dps.organization_id = step_run.organization_id
LEFT JOIN ExecutionAttempt attempt
  ON attempt.organization_id = step_run.organization_id
 AND attempt.task_id = step_run.task_id
 AND attempt.step_run_id = step_run.id
WHERE step_run.organization_id = @organizationId
  AND step_run.task_id = @taskId
  AND step_run.status = 'PENDING'
  AND task.status IN ('QUEUED', 'RUNNING')
  AND dps.included
  AND lower(btrim(dps.execution_location)) = 'target'
  AND attempt.id IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM unnest(dps.dependencies) dependency(step_key)
    LEFT JOIN StepRun dependency_run
      ON dependency_run.organization_id = step_run.organization_id
     AND dependency_run.task_id = step_run.task_id
     AND dependency_run.step_key = dependency.step_key
    WHERE dependency_run.id IS NULL
       OR dependency_run.status NOT IN ('SUCCEEDED', 'SKIPPED')
  )
ORDER BY step_run.sort_order, step_run.step_key, step_run.id`

const executionV2RunningProjectionSQL = `
WITH attempt AS (
  SELECT organization_id, task_id, step_run_id
  FROM ExecutionAttempt attempt
  WHERE attempt.organization_id = @organizationId
    AND attempt.task_id = @taskId
    AND attempt.step_run_id = @stepRunId
    AND attempt.id = @attemptId
), projected_step AS (
  UPDATE StepRun step_run
  SET status = 'RUNNING',
      started_at = COALESCE(step_run.started_at, clock_timestamp()),
      updated_at = clock_timestamp()
  FROM attempt
  WHERE step_run.id = attempt.step_run_id
    AND step_run.task_id = attempt.task_id
    AND step_run.organization_id = attempt.organization_id
    AND step_run.status IN ('PENDING', 'RUNNING')
  RETURNING step_run.task_id
)
UPDATE Task task
SET status = 'RUNNING',
    started_at = COALESCE(task.started_at, clock_timestamp()),
    updated_at = clock_timestamp()
FROM attempt
WHERE task.id = attempt.task_id
  AND task.organization_id = attempt.organization_id
  AND task.status IN ('QUEUED', 'RUNNING')`

const executionV2TerminalProjectionSQL = `
UPDATE StepRun step_run
SET status = @stepStatus,
    started_at = COALESCE(step_run.started_at, clock_timestamp()),
    completed_at = COALESCE(step_run.completed_at, clock_timestamp()),
    updated_at = clock_timestamp()
FROM ExecutionAttempt attempt
WHERE attempt.organization_id = @organizationId
  AND attempt.task_id = @taskId
  AND attempt.step_run_id = @stepRunId
  AND attempt.id = @attemptId
  AND step_run.id = attempt.step_run_id
  AND step_run.task_id = attempt.task_id
  AND step_run.organization_id = attempt.organization_id
  AND step_run.status IN ('PENDING', 'RUNNING', @stepStatus)`

const executionV2UncertainMemberProjectionSQL = `
UPDATE DeploymentCampaignMemberRun member
SET execution_uncertain = TRUE,
    updated_at = clock_timestamp()
FROM CampaignMemberTaskExecution lineage
JOIN ExecutionAttempt attempt
  ON attempt.organization_id = lineage.organization_id
 AND attempt.task_id = lineage.task_id
WHERE attempt.organization_id = @organizationId
  AND attempt.id = @attemptId
  AND lineage.organization_id = @organizationId
  AND lineage.task_id = @taskId
  AND member.id = lineage.campaign_member_run_id
  AND member.organization_id = lineage.organization_id`

const executionV2UncertainRunProjectionSQL = `
UPDATE DeploymentCampaignRun campaign_run
SET reconciliation_required = TRUE,
    admissions_blocked = TRUE,
    updated_at = clock_timestamp()
FROM CampaignMemberTaskExecution lineage
JOIN ExecutionAttempt attempt
  ON attempt.organization_id = lineage.organization_id
 AND attempt.task_id = lineage.task_id
WHERE attempt.organization_id = @organizationId
  AND attempt.id = @attemptId
  AND lineage.organization_id = @organizationId
  AND lineage.task_id = @taskId
  AND campaign_run.id = lineage.campaign_run_id
  AND campaign_run.organization_id = lineage.organization_id`

const executionV2UncertainProjectionSQL = executionV2UncertainMemberProjectionSQL + ";\n" +
	executionV2UncertainRunProjectionSQL

func GetExecutionV2ReadyStepRuns(
	ctx context.Context,
	organizationID, taskID uuid.UUID,
) ([]types.StepRun, error) {
	rows, err := internalctx.GetDb(ctx).Query(
		ctx,
		executionV2ReadyStepRunsSQL,
		pgx.NamedArgs{"organizationId": organizationID, "taskId": taskID},
	)
	if err != nil {
		return nil, fmt.Errorf("query execution v2 ready step runs: %w", err)
	}
	steps, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.StepRun])
	if err != nil {
		return nil, fmt.Errorf("collect execution v2 ready step runs: %w", err)
	}
	return steps, nil
}

func executionV2StepProjection(
	status types.ExecutionAttemptStatus,
) (types.StepRunStatus, bool) {
	switch status {
	case types.ExecutionAttemptStatusSucceeded:
		return types.StepRunStatusSucceeded, true
	case types.ExecutionAttemptStatusFailed,
		types.ExecutionAttemptStatusCanceled,
		types.ExecutionAttemptStatusTimedOut:
		return types.StepRunStatusFailed, true
	default:
		return "", false
	}
}

func projectExecutionV2Running(ctx context.Context, attempt types.ExecutionAttempt) error {
	command, err := internalctx.GetDb(ctx).Exec(ctx, executionV2RunningProjectionSQL, pgx.NamedArgs{
		"organizationId": attempt.OrganizationID,
		"taskId":         attempt.TaskID,
		"stepRunId":      attempt.StepRunID,
		"attemptId":      attempt.ID,
	})
	if err != nil {
		return fmt.Errorf("project running execution attempt: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("project running execution attempt: task or step lineage is invalid")
	}
	return projectCampaignExecutionRunning(ctx, attempt)
}

func projectExecutionV2Terminal(
	ctx context.Context,
	attempt types.ExecutionAttempt,
	status types.ExecutionAttemptStatus,
) (*types.Task, error) {
	stepStatus, terminal := executionV2StepProjection(status)
	if !terminal {
		return nil, fmt.Errorf("execution attempt status %s is not projectable as terminal", status)
	}
	command, err := internalctx.GetDb(ctx).Exec(ctx, executionV2TerminalProjectionSQL, pgx.NamedArgs{
		"organizationId": attempt.OrganizationID,
		"taskId":         attempt.TaskID,
		"stepRunId":      attempt.StepRunID,
		"attemptId":      attempt.ID,
		"stepStatus":     stepStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("project terminal execution step: %w", err)
	}
	if command.RowsAffected() != 1 {
		return nil, fmt.Errorf("project terminal execution step: task or step lineage is invalid")
	}

	task, err := getTask(ctx, attempt.TaskID, attempt.OrganizationID)
	if err != nil {
		return nil, err
	}
	taskStatus := types.TaskStatusRunning
	if status == types.ExecutionAttemptStatusCanceled {
		taskStatus = types.TaskStatusCanceled
	} else {
		allTerminal := true
		allSucceeded := true
		anyFailed := false
		for _, step := range task.StepRuns {
			allTerminal = allTerminal && step.Status.IsTerminal()
			allSucceeded = allSucceeded && (step.Status == types.StepRunStatusSucceeded || step.Status == types.StepRunStatusSkipped)
			anyFailed = anyFailed || step.Status == types.StepRunStatusFailed
		}
		switch {
		case anyFailed:
			taskStatus = types.TaskStatusFailed
		case allTerminal && allSucceeded:
			taskStatus = types.TaskStatusSucceeded
		}
	}
	if task.Status != taskStatus {
		if err := updateTaskStatus(ctx, task.ID, task.OrganizationID, taskStatus); err != nil {
			return nil, err
		}
	}
	if err := projectCampaignExecutionTerminal(ctx, attempt); err != nil {
		return nil, err
	}
	return getTask(ctx, attempt.TaskID, attempt.OrganizationID)
}

func projectExecutionV2Uncertain(
	ctx context.Context,
	attemptID, organizationID uuid.UUID,
) error {
	attempt, err := getExecutionAttemptForUpdate(ctx, attemptID, organizationID)
	if err != nil {
		return err
	}
	args := pgx.NamedArgs{
		"organizationId": organizationID,
		"attemptId":      attemptID,
		"taskId":         attempt.TaskID,
	}
	if _, err = internalctx.GetDb(ctx).Exec(
		ctx, executionV2UncertainMemberProjectionSQL, args,
	); err != nil {
		return fmt.Errorf("project uncertain execution member: %w", err)
	}
	if _, err = internalctx.GetDb(ctx).Exec(
		ctx, executionV2UncertainRunProjectionSQL, args,
	); err != nil {
		return fmt.Errorf("project uncertain execution attempt: %w", err)
	}
	return nil
}
