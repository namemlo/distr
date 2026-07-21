package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const loadCampaignCancelAttemptsSQL = `
SELECT
  attempt.id,
  attempt.execution_id,
  attempt.task_id,
  lineage.id,
  attempt.attempt_number,
  attempt.cancellable
FROM CampaignMemberTaskExecution AS lineage
JOIN ExecutionAttempt AS attempt
  ON attempt.task_id = lineage.task_id
 AND attempt.organization_id = lineage.organization_id
WHERE lineage.organization_id = @organization_id
  AND lineage.campaign_run_id = @campaign_run_id
  AND attempt.status IN ('PENDING', 'CLAIMED', 'RUNNING')
ORDER BY lineage.task_id, attempt.attempt_number, attempt.id
FOR UPDATE OF attempt`

const loadCampaignCancelTasksSQL = `
SELECT
  task.id,
  task.status,
  lineage.campaign_member_run_id,
  EXISTS (
    SELECT 1
    FROM ExecutionAttempt AS any_attempt
    WHERE any_attempt.organization_id = lineage.organization_id
      AND any_attempt.task_id = lineage.task_id
  ) AS has_execution_attempt
FROM CampaignMemberTaskExecution AS lineage
JOIN Task AS task
  ON task.id = lineage.task_id
 AND task.organization_id = lineage.organization_id
WHERE lineage.organization_id = @organization_id
  AND lineage.campaign_run_id = @campaign_run_id
ORDER BY task.id
FOR UPDATE OF task`

type campaignCancelAttempt struct {
	id          uuid.UUID
	execution   uuid.UUID
	task        uuid.UUID
	lineage     uuid.UUID
	number      int
	cancellable bool
}

type campaignCancelTask struct {
	id            uuid.UUID
	status        types.TaskStatus
	memberRunID   uuid.UUID
	hasAnyAttempt bool
}

func shouldFanoutCampaignCancel(
	input types.CampaignControlInput,
	decision types.CampaignControlDecision,
) bool {
	return input.Kind == types.CampaignControlKindCancel &&
		decision.Status == types.CampaignControlStatusApplied &&
		decision.Run.State == types.CampaignRunStateCanceled
}

func campaignCancelIdempotencyKey(runID, requestID, attemptID uuid.UUID) string {
	return fmt.Sprintf("cc:%s:%s:%s", runID, requestID, attemptID)
}

// applyCampaignCancelFanout is called inside the campaign-control transaction.
// It deliberately follows only the immutable campaign-member/task bridge; a
// deployment plan can participate in more than one campaign run.
func applyCampaignCancelFanout(
	ctx context.Context,
	input types.CampaignControlInput,
	controlID uuid.UUID,
	controlChecksum string,
	auditInputs *[]types.ControlPlaneAuditEventInput,
) error {
	if input.ActorID == uuid.Nil {
		return apierrors.NewBadRequest("campaign cancel actor is required")
	}

	// Execution completion locks the attempt before projecting the Task. Match
	// that order, then lock Tasks and reload attempts so a concurrent dispatcher
	// that won a Task lock cannot create an attempt in the gap.
	if _, err := lockCampaignCancelAttempts(ctx, input); err != nil {
		return err
	}
	tasks, err := lockCampaignCancelTasks(ctx, input)
	if err != nil {
		return err
	}
	attempts, err := lockCampaignCancelAttempts(ctx, input)
	if err != nil {
		return err
	}

	activeTask := make(map[uuid.UUID]struct{}, len(attempts))
	for _, attempt := range attempts {
		activeTask[attempt.task] = struct{}{}
		if !attempt.cancellable {
			return apierrors.NewConflict("campaign has active non-cancellable executor attempt")
		}
		cancelID, err := insertCampaignExecutionCancel(ctx, input, attempt)
		if err != nil {
			return err
		}
		if err := insertCampaignExecutionCancelHandoff(
			ctx,
			input.OrganizationID,
			cancelID,
			attempt,
		); err != nil {
			return err
		}
		lineage, err := loadCampaignMemberAuditLineageForTask(
			ctx, input.OrganizationID, attempt.task,
		)
		if err != nil {
			return err
		}
		auditInput := campaignMemberAuditInput(
			lineage, "campaign.execution.cancel_requested", &input.ActorID,
		)
		auditInput.CampaignControlRequestID = &controlID
		auditInput.CampaignControlChecksum = controlChecksum
		auditInput.TaskID = &attempt.task
		auditInput.ExecutionID = &attempt.execution
		*auditInputs = append(*auditInputs, auditInput)
	}

	for _, task := range tasks {
		if _, active := activeTask[task.id]; active || task.status.IsTerminal() || task.hasAnyAttempt {
			continue
		}
		if err := updateTaskStatus(
			ctx,
			task.id,
			input.OrganizationID,
			types.TaskStatusCanceled,
		); err != nil {
			return fmt.Errorf("cancel campaign task before executor dispatch: %w", err)
		}
		lineage, err := loadCampaignMemberAuditLineage(
			ctx, input.OrganizationID, task.memberRunID,
		)
		if err != nil {
			return err
		}
		auditInput := campaignMemberAuditInput(
			lineage, "campaign.task.canceled_before_dispatch", &input.ActorID,
		)
		auditInput.CampaignControlRequestID = &controlID
		auditInput.CampaignControlChecksum = controlChecksum
		auditInput.TaskID = &task.id
		*auditInputs = append(*auditInputs, auditInput)
	}

	if err := markFullyCanceledCampaignMembers(
		ctx, input, controlID, controlChecksum, auditInputs,
	); err != nil {
		return err
	}
	return nil
}

func loadCampaignMemberAuditLineageForTask(
	ctx context.Context,
	organizationID, taskID uuid.UUID,
) (campaignAuditLineage, error) {
	var memberRunID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT campaign_member_run_id
FROM CampaignMemberTaskExecution
WHERE organization_id = @organization_id
  AND task_id = @task_id`, pgx.NamedArgs{
		"organization_id": organizationID, "task_id": taskID,
	}).Scan(&memberRunID)
	if errors.Is(err, pgx.ErrNoRows) {
		return campaignAuditLineage{}, apierrors.ErrNotFound
	}
	if err != nil {
		return campaignAuditLineage{}, err
	}
	return loadCampaignMemberAuditLineage(ctx, organizationID, memberRunID)
}

func lockCampaignCancelAttempts(
	ctx context.Context,
	input types.CampaignControlInput,
) ([]campaignCancelAttempt, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, loadCampaignCancelAttemptsSQL, pgx.NamedArgs{
		"organization_id": input.OrganizationID,
		"campaign_run_id": input.RunID,
	})
	if err != nil {
		return nil, fmt.Errorf("lock campaign executor attempts: %w", err)
	}
	defer rows.Close()
	attempts := make([]campaignCancelAttempt, 0)
	for rows.Next() {
		var attempt campaignCancelAttempt
		if err := rows.Scan(
			&attempt.id,
			&attempt.execution,
			&attempt.task,
			&attempt.lineage,
			&attempt.number,
			&attempt.cancellable,
		); err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return attempts, nil
}

func lockCampaignCancelTasks(
	ctx context.Context,
	input types.CampaignControlInput,
) ([]campaignCancelTask, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, loadCampaignCancelTasksSQL, pgx.NamedArgs{
		"organization_id": input.OrganizationID,
		"campaign_run_id": input.RunID,
	})
	if err != nil {
		return nil, fmt.Errorf("lock campaign tasks for cancellation: %w", err)
	}
	defer rows.Close()
	tasks := make([]campaignCancelTask, 0)
	for rows.Next() {
		var task campaignCancelTask
		if err := rows.Scan(&task.id, &task.status, &task.memberRunID, &task.hasAnyAttempt); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func insertCampaignExecutionCancel(
	ctx context.Context,
	input types.CampaignControlInput,
	attempt campaignCancelAttempt,
) (uuid.UUID, error) {
	idempotencyKey := campaignCancelIdempotencyKey(input.RunID, input.RequestID, attempt.id)
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx, `
INSERT INTO ExecutionCancelRequest (
  organization_id, execution_id, execution_attempt_id,
  requested_by, idempotency_key, reason, created_at
) VALUES (
  @organization_id, @execution_id, @execution_attempt_id,
  @requested_by, @idempotency_key, @reason, clock_timestamp()
)
ON CONFLICT (organization_id, execution_attempt_id, idempotency_key) DO NOTHING`, pgx.NamedArgs{
		"organization_id":      input.OrganizationID,
		"execution_id":         attempt.execution,
		"execution_attempt_id": attempt.id,
		"requested_by":         input.ActorID,
		"idempotency_key":      idempotencyKey,
		"reason":               input.Reason,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert campaign execution cancel request: %w", err)
	}

	var cancelID, executionID, attemptID, requestedBy uuid.UUID
	var reason string
	err = db.QueryRow(ctx, `
SELECT id, execution_id, execution_attempt_id, requested_by, reason
FROM ExecutionCancelRequest
WHERE organization_id = @organization_id
  AND execution_attempt_id = @execution_attempt_id
  AND idempotency_key = @idempotency_key
FOR UPDATE`, pgx.NamedArgs{
		"organization_id":      input.OrganizationID,
		"execution_attempt_id": attempt.id,
		"idempotency_key":      idempotencyKey,
	}).Scan(&cancelID, &executionID, &attemptID, &requestedBy, &reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apierrors.NewConflict("campaign execution cancel request was not persisted")
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("load campaign execution cancel request: %w", err)
	}
	if executionID != attempt.execution || attemptID != attempt.id ||
		requestedBy != input.ActorID || reason != input.Reason {
		return uuid.Nil, apierrors.NewConflict("conflicting campaign execution cancel replay")
	}
	return cancelID, nil
}

func insertCampaignExecutionCancelHandoff(
	ctx context.Context,
	organizationID, cancelID uuid.UUID,
	attempt campaignCancelAttempt,
) error {
	db := internalctx.GetDb(ctx)
	tag, err := db.Exec(ctx, `
INSERT INTO ExecutionCampaignControlHandoff (
  organization_id, execution_cancel_request_id, execution_id,
  execution_attempt_id, campaign_member_task_execution_id,
  task_id, control_kind
) VALUES (
  @organization_id, @cancel_id, @execution_id,
  @execution_attempt_id, @lineage_id,
  @task_id, 'CANCEL_REQUESTED'
)
ON CONFLICT (organization_id, execution_cancel_request_id) DO NOTHING`, pgx.NamedArgs{
		"organization_id":      organizationID,
		"cancel_id":            cancelID,
		"execution_id":         attempt.execution,
		"execution_attempt_id": attempt.id,
		"lineage_id":           attempt.lineage,
		"task_id":              attempt.task,
	})
	if err != nil {
		return fmt.Errorf("insert campaign execution cancel handoff: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var exact bool
	err = db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM ExecutionCampaignControlHandoff
  WHERE organization_id = @organization_id
    AND execution_cancel_request_id = @cancel_id
    AND execution_id = @execution_id
    AND execution_attempt_id = @execution_attempt_id
    AND campaign_member_task_execution_id = @lineage_id
    AND task_id = @task_id
    AND control_kind = 'CANCEL_REQUESTED'
)`, pgx.NamedArgs{
		"organization_id":      organizationID,
		"cancel_id":            cancelID,
		"execution_id":         attempt.execution,
		"execution_attempt_id": attempt.id,
		"lineage_id":           attempt.lineage,
		"task_id":              attempt.task,
	}).Scan(&exact)
	if err != nil {
		return fmt.Errorf("verify campaign execution cancel handoff: %w", err)
	}
	if !exact {
		return apierrors.NewConflict("conflicting campaign execution cancel handoff replay")
	}
	return nil
}

func markFullyCanceledCampaignMembers(
	ctx context.Context,
	input types.CampaignControlInput,
	controlID uuid.UUID,
	controlChecksum string,
	auditInputs *[]types.ControlPlaneAuditEventInput,
) error {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
WITH canceled_members AS (
UPDATE DeploymentCampaignMemberRun AS member_run
SET status = 'CANCELED', completed_at = COALESCE(completed_at, @canceled_at)
WHERE member_run.organization_id = @organization_id
  AND member_run.campaign_run_id = @campaign_run_id
  AND member_run.status IN ('ADMITTED', 'RUNNING')
  AND EXISTS (
    SELECT 1
    FROM CampaignMemberTaskExecution AS lineage
    WHERE lineage.organization_id = member_run.organization_id
      AND lineage.campaign_run_id = member_run.campaign_run_id
      AND lineage.campaign_member_run_id = member_run.id
  )
  AND NOT EXISTS (
    SELECT 1
    FROM CampaignMemberTaskExecution AS lineage
    JOIN Task AS task
      ON task.id = lineage.task_id
     AND task.organization_id = lineage.organization_id
    WHERE lineage.organization_id = member_run.organization_id
      AND lineage.campaign_run_id = member_run.campaign_run_id
      AND lineage.campaign_member_run_id = member_run.id
      AND task.status <> 'CANCELED'
  )
RETURNING member_run.id, member_run.wave_run_id
)
SELECT id, wave_run_id
FROM canceled_members
ORDER BY wave_run_id, id`, pgx.NamedArgs{
		"organization_id": input.OrganizationID,
		"campaign_run_id": input.RunID,
		"canceled_at":     input.RequestedAt,
	})
	if err != nil {
		return fmt.Errorf("mark campaign members canceled: %w", err)
	}
	defer rows.Close()

	waveRunIDs := make(map[uuid.UUID]struct{})
	for rows.Next() {
		var memberRunID, waveRunID uuid.UUID
		if err := rows.Scan(&memberRunID, &waveRunID); err != nil {
			return fmt.Errorf("scan canceled campaign wave: %w", err)
		}
		waveRunIDs[waveRunID] = struct{}{}
		lineage, err := loadCampaignMemberAuditLineage(
			ctx, input.OrganizationID, memberRunID,
		)
		if err != nil {
			return err
		}
		auditInput := campaignMemberAuditInput(
			lineage, "campaign.member.execution.canceled", &input.ActorID,
		)
		auditInput.CampaignControlRequestID = &controlID
		auditInput.CampaignControlChecksum = controlChecksum
		*auditInputs = append(*auditInputs, auditInput)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("load canceled campaign waves: %w", err)
	}
	rows.Close()

	for waveRunID := range waveRunIDs {
		if err := projectCampaignWaveExecution(ctx, input.OrganizationID, waveRunID); err != nil {
			return fmt.Errorf("project canceled campaign wave: %w", err)
		}
	}
	return nil
}
