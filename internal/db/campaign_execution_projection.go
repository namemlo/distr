package db

import (
	"context"
	"errors"
	"fmt"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const campaignMemberExecutionProjectionSQL = `
WITH target_member AS (
  SELECT lineage.organization_id, lineage.campaign_member_run_id
  FROM CampaignMemberTaskExecution lineage
  WHERE lineage.organization_id = @organizationId
    AND lineage.task_id = @taskId
), active_attempts AS (
  SELECT lineage.organization_id, lineage.campaign_member_run_id,
		 COALESCE(bool_and(attempt.cancellable), FALSE) AS all_cancellable
  FROM CampaignMemberTaskExecution lineage
  JOIN ExecutionAttempt attempt
    ON attempt.organization_id = lineage.organization_id
   AND attempt.task_id = lineage.task_id
  JOIN target_member target
    ON target.organization_id = lineage.organization_id
   AND target.campaign_member_run_id = lineage.campaign_member_run_id
  WHERE attempt.status IN ('CLAIMED', 'RUNNING')
  GROUP BY lineage.organization_id, lineage.campaign_member_run_id
), aggregate AS (
  SELECT lineage.organization_id, lineage.campaign_member_run_id,
         bool_and(task.status = 'SUCCEEDED') AS all_succeeded,
         bool_or(task.status = 'FAILED') AS any_failed,
         bool_or(task.status = 'CANCELED') AS any_canceled,
         count(*) FILTER (WHERE task.status NOT IN ('SUCCEEDED', 'FAILED', 'CANCELED')) AS active
  FROM CampaignMemberTaskExecution lineage
  JOIN Task task
    ON task.id = lineage.task_id
   AND task.organization_id = lineage.organization_id
  JOIN target_member target
    ON target.organization_id = lineage.organization_id
   AND target.campaign_member_run_id = lineage.campaign_member_run_id
  GROUP BY lineage.organization_id, lineage.campaign_member_run_id
)
UPDATE DeploymentCampaignMemberRun member
SET status = CASE
	  WHEN aggregate.active > 0 THEN 'RUNNING'
	  WHEN aggregate.any_failed THEN 'FAILED'
	  WHEN aggregate.any_canceled THEN 'CANCELED'
      WHEN aggregate.all_succeeded THEN 'SUCCEEDED'
      ELSE member.status
    END,
    completed_at = CASE
      WHEN aggregate.active = 0 THEN COALESCE(member.completed_at, clock_timestamp())
      ELSE NULL
    END,
    active_steps_cancellable = COALESCE(active_attempts.all_cancellable, FALSE)
FROM aggregate
LEFT JOIN active_attempts
  ON active_attempts.organization_id = aggregate.organization_id
 AND active_attempts.campaign_member_run_id = aggregate.campaign_member_run_id
WHERE member.id = aggregate.campaign_member_run_id
  AND member.organization_id = aggregate.organization_id
  AND member.status IN ('ADMITTED', 'RUNNING')`

const campaignWaveExecutionProjectionSQL = `
WITH aggregate AS (
  SELECT member.organization_id, member.wave_run_id,
         bool_and(member.status IN ('SUCCEEDED', 'FAILED', 'EXCLUDED', 'CANCELED')) AS all_terminal,
         bool_and(member.status IN ('SUCCEEDED', 'EXCLUDED')) AS all_succeeded,
         bool_or(member.status = 'FAILED') AS any_failed,
         bool_or(member.status = 'CANCELED') AS any_canceled
  FROM DeploymentCampaignMemberRun member
  WHERE member.organization_id = @organizationId
    AND member.wave_run_id = @waveRunId
  GROUP BY member.organization_id, member.wave_run_id
)
UPDATE DeploymentCampaignWaveRun wave
SET status = CASE
	  WHEN NOT aggregate.all_terminal THEN 'RUNNING'
	  WHEN aggregate.any_failed THEN 'FAILED'
	  WHEN aggregate.any_canceled THEN 'CANCELED'
	  WHEN aggregate.all_succeeded THEN 'COMPLETED'
      ELSE 'RUNNING'
    END,
    completed_at = CASE
      WHEN aggregate.all_terminal THEN COALESCE(wave.completed_at, clock_timestamp())
      ELSE NULL
    END
FROM aggregate
WHERE wave.id = aggregate.wave_run_id
  AND wave.organization_id = aggregate.organization_id
  AND wave.status IN ('PENDING', 'RUNNING', 'BAKING', 'FAILED', 'COMPLETED', 'CANCELED')`

const campaignExecutionRunningProjectionSQL = `
WITH target AS (
  SELECT lineage.organization_id, lineage.campaign_member_run_id,
		 member.wave_run_id
  FROM CampaignMemberTaskExecution lineage
  JOIN ExecutionAttempt attempt
    ON attempt.organization_id = lineage.organization_id
   AND attempt.task_id = lineage.task_id
  JOIN DeploymentCampaignMemberRun member
    ON member.id = lineage.campaign_member_run_id
   AND member.organization_id = lineage.organization_id
  WHERE lineage.organization_id = @organizationId
    AND lineage.task_id = @taskId
    AND attempt.id = @attemptId
), active_attempts AS (
  SELECT lineage.organization_id, lineage.campaign_member_run_id,
		 COALESCE(bool_and(attempt.cancellable), FALSE) AS all_cancellable
  FROM CampaignMemberTaskExecution lineage
  JOIN ExecutionAttempt attempt
    ON attempt.organization_id = lineage.organization_id
   AND attempt.task_id = lineage.task_id
  JOIN target
    ON target.organization_id = lineage.organization_id
   AND target.campaign_member_run_id = lineage.campaign_member_run_id
  WHERE attempt.status IN ('CLAIMED', 'RUNNING')
  GROUP BY lineage.organization_id, lineage.campaign_member_run_id
), projected_member AS (
  UPDATE DeploymentCampaignMemberRun member
  SET status = 'RUNNING',
	  active_steps_cancellable = COALESCE(active_attempts.all_cancellable, FALSE)
  FROM target
  LEFT JOIN active_attempts
    ON active_attempts.organization_id = target.organization_id
   AND active_attempts.campaign_member_run_id = target.campaign_member_run_id
  WHERE member.id = target.campaign_member_run_id
    AND member.organization_id = target.organization_id
    AND member.status IN ('ADMITTED', 'RUNNING')
  RETURNING target.organization_id, target.wave_run_id
)
UPDATE DeploymentCampaignWaveRun wave
SET status = 'RUNNING',
    started_at = COALESCE(wave.started_at, clock_timestamp())
FROM projected_member member
WHERE wave.id = member.wave_run_id
  AND wave.organization_id = member.organization_id
  AND wave.status IN ('PENDING', 'RUNNING')`

func projectCampaignExecutionRunning(
	ctx context.Context,
	attempt types.ExecutionAttempt,
) error {
	_, err := internalctx.GetDb(ctx).Exec(ctx, campaignExecutionRunningProjectionSQL, pgx.NamedArgs{
		"organizationId": attempt.OrganizationID,
		"taskId":         attempt.TaskID,
		"attemptId":      attempt.ID,
	})
	if err != nil {
		return fmt.Errorf("project running campaign execution: %w", err)
	}
	return nil
}

func projectCampaignExecutionTerminal(
	ctx context.Context,
	attempt types.ExecutionAttempt,
) error {
	args := pgx.NamedArgs{
		"organizationId": attempt.OrganizationID,
		"taskId":         attempt.TaskID,
	}
	if _, err := internalctx.GetDb(ctx).Exec(ctx, campaignMemberExecutionProjectionSQL, args); err != nil {
		return fmt.Errorf("project campaign member execution: %w", err)
	}
	waveRunID, found, err := campaignWaveRunIDForTask(
		ctx, attempt.OrganizationID, attempt.TaskID,
	)
	if err != nil || !found {
		return err
	}
	return projectCampaignWaveExecution(ctx, attempt.OrganizationID, waveRunID)
}

func campaignWaveRunIDForTask(
	ctx context.Context,
	organizationID, taskID uuid.UUID,
) (uuid.UUID, bool, error) {
	var waveRunID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT member.wave_run_id
		FROM CampaignMemberTaskExecution lineage
		JOIN DeploymentCampaignMemberRun member
		  ON member.id = lineage.campaign_member_run_id
		 AND member.organization_id = lineage.organization_id
		WHERE lineage.organization_id = @organizationId
		  AND lineage.task_id = @taskId`, pgx.NamedArgs{
		"organizationId": organizationID,
		"taskId":         taskID,
	}).Scan(&waveRunID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("resolve campaign wave for task: %w", err)
	}
	return waveRunID, true, nil
}

func projectCampaignWaveExecution(
	ctx context.Context,
	organizationID, waveRunID uuid.UUID,
) error {
	if organizationID == uuid.Nil || waveRunID == uuid.Nil {
		return fmt.Errorf("project campaign wave execution: organization and wave run are required")
	}
	command, err := internalctx.GetDb(ctx).Exec(ctx, campaignWaveExecutionProjectionSQL, pgx.NamedArgs{
		"organizationId": organizationID,
		"waveRunId":      waveRunID,
	})
	if err != nil {
		return fmt.Errorf("project campaign wave execution: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("project campaign wave execution: wave aggregate is missing")
	}
	return nil
}
