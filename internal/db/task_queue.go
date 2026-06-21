package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const taskOutputExpr = `
	t.id,
	t.created_at,
	t.updated_at,
	t.queued_at,
	t.started_at,
	t.completed_at,
	t.organization_id,
	t.deployment_plan_id,
	t.deployment_plan_target_id,
	t.deployment_target_id,
	t.application_id,
	t.release_bundle_id,
	t.channel_id,
	t.environment_id,
	t.status,
	t.queue_order
`

const stepRunOutputExpr = `
	sr.id,
	sr.created_at,
	sr.updated_at,
	sr.started_at,
	sr.completed_at,
	sr.organization_id,
	sr.task_id,
	sr.deployment_plan_id,
	sr.deployment_plan_step_id,
	sr.step_key,
	sr.name,
	sr.action_type,
	sr.status,
	sr.sort_order,
	sr.skipped_reason
`

func CreateTasksForDeploymentPlan(
	ctx context.Context,
	request types.CreateTasksForDeploymentPlanRequest,
) ([]types.Task, error) {
	if err := validateCreateTasksForDeploymentPlanRequest(request); err != nil {
		return nil, err
	}
	var tasks []types.Task
	err := RunTx(ctx, func(ctx context.Context) error {
		plan, err := GetDeploymentPlan(ctx, request.DeploymentPlanID, request.OrganizationID)
		if err != nil {
			return err
		}
		if plan.Status != types.DeploymentPlanStatusReady {
			return apierrors.NewConflict("deployment plan must be READY before tasks can be created")
		}
		existing, err := getTasksByDeploymentPlanID(ctx, request.DeploymentPlanID, request.OrganizationID)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			tasks = existing
			return nil
		}
		if err := ensureDeploymentPlanReleaseBundlePublishedForTaskCreation(ctx, *plan); err != nil {
			return err
		}
		created, err := insertTasksForDeploymentPlan(ctx, request.DeploymentPlanID, request.OrganizationID)
		if err != nil {
			return err
		}
		if err := insertStepRunsForTasks(ctx, created); err != nil {
			return err
		}
		tasks, err = getTasksByDeploymentPlanID(ctx, request.DeploymentPlanID, request.OrganizationID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func GetTasksByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]types.Task, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+taskOutputExpr+`
		FROM Task t
		WHERE t.organization_id = @organizationId
		ORDER BY t.queue_order, t.id`,
		pgx.NamedArgs{"organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Task: %w", err)
	}
	tasks, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.Task])
	if err != nil {
		return nil, fmt.Errorf("could not collect Task: %w", err)
	}
	for i := range tasks {
		if err := hydrateTask(ctx, &tasks[i]); err != nil {
			return nil, err
		}
	}
	return tasks, nil
}

func GetTask(ctx context.Context, id, orgID uuid.UUID) (*types.Task, error) {
	return getTask(ctx, id, orgID)
}

func TransitionTaskState(ctx context.Context, request types.TransitionTaskStateRequest) (*types.Task, error) {
	if err := validateTransitionTaskStateRequest(request); err != nil {
		return nil, err
	}
	var task *types.Task
	err := RunTx(ctx, func(ctx context.Context) error {
		current, err := getTaskStatusForUpdate(ctx, request.TaskID, request.OrganizationID)
		if err != nil {
			return err
		}
		if !isAllowedTaskTransition(current, request.Status) {
			return apierrors.NewConflict(
				fmt.Sprintf("cannot transition task from %s to %s", current, request.Status),
			)
		}
		if err := updateTaskStatus(ctx, request.TaskID, request.OrganizationID, request.Status); err != nil {
			return err
		}
		task, err = getTask(ctx, request.TaskID, request.OrganizationID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

func TransitionStepRunState(ctx context.Context, request types.TransitionStepRunStateRequest) (*types.StepRun, error) {
	if err := validateTransitionStepRunStateRequest(request); err != nil {
		return nil, err
	}
	var stepRun *types.StepRun
	err := RunTx(ctx, func(ctx context.Context) error {
		current, err := getStepRunStatusForUpdate(ctx, request.StepRunID, request.OrganizationID)
		if err != nil {
			return err
		}
		if !isAllowedStepRunTransition(current, request.Status) {
			return apierrors.NewConflict(
				fmt.Sprintf("cannot transition step run from %s to %s", current, request.Status),
			)
		}
		if err := updateStepRunStatus(ctx, request.StepRunID, request.OrganizationID, request.Status); err != nil {
			return err
		}
		stepRun, err = getStepRun(ctx, request.StepRunID, request.OrganizationID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return stepRun, nil
}

func validateCreateTasksForDeploymentPlanRequest(request types.CreateTasksForDeploymentPlanRequest) error {
	if request.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if request.DeploymentPlanID == uuid.Nil {
		return apierrors.NewBadRequest("deploymentPlanId is required")
	}
	return nil
}

func ensureDeploymentPlanReleaseBundlePublishedForTaskCreation(
	ctx context.Context,
	plan types.DeploymentPlan,
) error {
	bundle, err := getReleaseBundle(ctx, plan.ReleaseBundleID, plan.OrganizationID, true)
	if err != nil {
		return err
	}
	if bundle.Status != types.ReleaseBundleStatusPublished {
		return apierrors.NewConflict("release bundle must be PUBLISHED before tasks can be created")
	}
	return nil
}

func validateTransitionTaskStateRequest(request types.TransitionTaskStateRequest) error {
	if request.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if request.TaskID == uuid.Nil {
		return apierrors.NewBadRequest("taskId is required")
	}
	if request.Status == "" {
		return apierrors.NewBadRequest("status is required")
	}
	if !request.Status.IsValid() {
		return apierrors.NewBadRequest("status is invalid")
	}
	return nil
}

func validateTransitionStepRunStateRequest(request types.TransitionStepRunStateRequest) error {
	if request.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if request.StepRunID == uuid.Nil {
		return apierrors.NewBadRequest("stepRunId is required")
	}
	if request.Status == "" {
		return apierrors.NewBadRequest("status is required")
	}
	if !request.Status.IsValid() {
		return apierrors.NewBadRequest("status is invalid")
	}
	return nil
}

func isAllowedTaskTransition(from, to types.TaskStatus) bool {
	switch from {
	case types.TaskStatusQueued:
		return to == types.TaskStatusRunning
	case types.TaskStatusRunning:
		return to == types.TaskStatusSucceeded || to == types.TaskStatusFailed
	default:
		return false
	}
}

func isAllowedStepRunTransition(from, to types.StepRunStatus) bool {
	switch from {
	case types.StepRunStatusPending:
		return to == types.StepRunStatusRunning || to == types.StepRunStatusSkipped
	case types.StepRunStatusRunning:
		return to == types.StepRunStatusSucceeded || to == types.StepRunStatusFailed
	default:
		return false
	}
}

func insertTasksForDeploymentPlan(ctx context.Context, planID, orgID uuid.UUID) ([]types.Task, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`INSERT INTO Task AS t (
			organization_id,
			deployment_plan_id,
			deployment_plan_target_id,
			deployment_target_id,
			application_id,
			release_bundle_id,
			channel_id,
			environment_id,
			status
		)
		SELECT
			dp.organization_id,
			dp.id,
			dpt.id,
			dpt.deployment_target_id,
			dp.application_id,
			dp.release_bundle_id,
			dp.channel_id,
			dp.environment_id,
			@status
		FROM DeploymentPlan dp
		JOIN DeploymentPlanTarget dpt
			ON dpt.deployment_plan_id = dp.id
			AND dpt.organization_id = dp.organization_id
		WHERE dp.id = @deploymentPlanId
			AND dp.organization_id = @organizationId
		ORDER BY dpt.sort_order, dpt.deployment_target_id
		ON CONFLICT (deployment_plan_id, deployment_plan_target_id) DO NOTHING
		RETURNING `+taskOutputExpr,
		pgx.NamedArgs{
			"deploymentPlanId": planID,
			"organizationId":   orgID,
			"status":           types.TaskStatusQueued,
		},
	)
	if err != nil {
		return nil, mapTaskWriteError("insert", err)
	}
	tasks, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.Task])
	if err != nil {
		return nil, mapTaskWriteError("collect inserted", err)
	}
	return tasks, nil
}

func insertStepRunsForTasks(ctx context.Context, tasks []types.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	taskIDs := make([]uuid.UUID, 0, len(tasks))
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.ID)
	}
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`INSERT INTO StepRun (
			organization_id,
			task_id,
			deployment_plan_id,
			deployment_plan_step_id,
			step_key,
			name,
			action_type,
			status,
			sort_order,
			skipped_reason,
			completed_at
		)
		SELECT
			t.organization_id,
			t.id,
			t.deployment_plan_id,
			dps.id,
			dps.step_key,
			dps.name,
			dps.action_type,
			CASE WHEN dps.included THEN @pendingStatus ELSE @skippedStatus END,
			dps.sort_order,
			CASE WHEN dps.included THEN '' ELSE dps.excluded_reason END,
			CASE WHEN dps.included THEN NULL ELSE now() END
		FROM Task t
		JOIN DeploymentPlanStep dps
			ON dps.deployment_plan_id = t.deployment_plan_id
			AND dps.organization_id = t.organization_id
		WHERE t.id = ANY(@taskIds)
		ORDER BY t.queue_order, dps.sort_order, dps.step_key
		ON CONFLICT (task_id, deployment_plan_step_id) DO NOTHING`,
		pgx.NamedArgs{
			"taskIds":       taskIDs,
			"pendingStatus": types.StepRunStatusPending,
			"skippedStatus": types.StepRunStatusSkipped,
		},
	)
	if err != nil {
		return mapTaskWriteError("insert step runs", err)
	}
	return nil
}

func getTasksByDeploymentPlanID(ctx context.Context, planID, orgID uuid.UUID) ([]types.Task, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+taskOutputExpr+`
		FROM Task t
		WHERE t.deployment_plan_id = @deploymentPlanId
			AND t.organization_id = @organizationId
		ORDER BY t.queue_order, t.id`,
		pgx.NamedArgs{"deploymentPlanId": planID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Task by deployment plan: %w", err)
	}
	tasks, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.Task])
	if err != nil {
		return nil, fmt.Errorf("could not collect Task by deployment plan: %w", err)
	}
	for i := range tasks {
		if err := hydrateTask(ctx, &tasks[i]); err != nil {
			return nil, err
		}
	}
	return tasks, nil
}

func getTask(ctx context.Context, id, orgID uuid.UUID) (*types.Task, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+taskOutputExpr+`
		FROM Task t
		WHERE t.id = @id AND t.organization_id = @organizationId`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Task: %w", err)
	}
	task, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.Task])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect Task: %w", err)
	}
	if err := hydrateTask(ctx, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func hydrateTask(ctx context.Context, task *types.Task) error {
	stepRuns, err := getStepRunsByTaskID(ctx, task.ID, task.OrganizationID)
	if err != nil {
		return err
	}
	task.StepRuns = stepRuns
	return nil
}

func getStepRunsByTaskID(ctx context.Context, taskID, orgID uuid.UUID) ([]types.StepRun, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+stepRunOutputExpr+`
		FROM StepRun sr
		WHERE sr.task_id = @taskId AND sr.organization_id = @organizationId
		ORDER BY sr.sort_order, sr.step_key`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query StepRun: %w", err)
	}
	stepRuns, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.StepRun])
	if err != nil {
		return nil, fmt.Errorf("could not collect StepRun: %w", err)
	}
	return stepRuns, nil
}

func getStepRun(ctx context.Context, id, orgID uuid.UUID) (*types.StepRun, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+stepRunOutputExpr+`
		FROM StepRun sr
		WHERE sr.id = @id AND sr.organization_id = @organizationId`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query StepRun: %w", err)
	}
	stepRun, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.StepRun])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect StepRun: %w", err)
	}
	return &stepRun, nil
}

func getTaskStatusForUpdate(ctx context.Context, id, orgID uuid.UUID) (types.TaskStatus, error) {
	db := internalctx.GetDb(ctx)
	var status types.TaskStatus
	err := db.QueryRow(ctx,
		`SELECT status
		FROM Task
		WHERE id = @id AND organization_id = @organizationId
		FOR UPDATE`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apierrors.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("could not lock Task: %w", err)
	}
	return status, nil
}

func getStepRunStatusForUpdate(ctx context.Context, id, orgID uuid.UUID) (types.StepRunStatus, error) {
	db := internalctx.GetDb(ctx)
	var status types.StepRunStatus
	err := db.QueryRow(ctx,
		`SELECT status
		FROM StepRun
		WHERE id = @id AND organization_id = @organizationId
		FOR UPDATE`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apierrors.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("could not lock StepRun: %w", err)
	}
	return status, nil
}

func updateTaskStatus(ctx context.Context, id, orgID uuid.UUID, status types.TaskStatus) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`UPDATE Task
		SET
			status = @status,
			updated_at = now(),
			started_at = CASE
				WHEN @status = @runningStatus AND started_at IS NULL THEN now()
				ELSE started_at
			END,
			completed_at = CASE
				WHEN @status = @succeededStatus OR @status = @failedStatus THEN now()
				ELSE completed_at
			END
		WHERE id = @id AND organization_id = @organizationId`,
		pgx.NamedArgs{
			"id":              id,
			"organizationId":  orgID,
			"status":          status,
			"runningStatus":   types.TaskStatusRunning,
			"succeededStatus": types.TaskStatusSucceeded,
			"failedStatus":    types.TaskStatusFailed,
		},
	)
	if err != nil {
		return mapTaskWriteError("update status", err)
	}
	return nil
}

func updateStepRunStatus(ctx context.Context, id, orgID uuid.UUID, status types.StepRunStatus) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`UPDATE StepRun
		SET
			status = @status,
			updated_at = now(),
			started_at = CASE
				WHEN @status = @runningStatus AND started_at IS NULL THEN now()
				ELSE started_at
			END,
			completed_at = CASE
				WHEN @status = @succeededStatus OR @status = @failedStatus OR @status = @skippedStatus THEN now()
				ELSE completed_at
			END
		WHERE id = @id AND organization_id = @organizationId`,
		pgx.NamedArgs{
			"id":              id,
			"organizationId":  orgID,
			"status":          status,
			"runningStatus":   types.StepRunStatusRunning,
			"succeededStatus": types.StepRunStatusSucceeded,
			"failedStatus":    types.StepRunStatusFailed,
			"skippedStatus":   types.StepRunStatusSkipped,
		},
	)
	if err != nil {
		return mapTaskWriteError("update step run status", err)
	}
	return nil
}

func mapTaskWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s Task: %w", action, apierrors.ErrNotFound)
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s Task: %w", action, apierrors.ErrConflict)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s Task: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s Task: %w", action, err)
}
