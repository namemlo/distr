package db

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	obsermetrics "github.com/distr-sh/distr/internal/observability/metrics"
	obsertracing "github.com/distr-sh/distr/internal/observability/tracing"
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
	t.task_type,
	t.deployment_plan_id,
	t.deployment_plan_target_id,
	t.deployment_target_id,
	t.application_id,
	t.release_bundle_id,
	t.channel_id,
	t.environment_id,
	t.actor_user_account_id,
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

const taskResourceLockOutputExpr = `
	trl.id,
	trl.created_at,
	trl.updated_at,
	trl.acquired_at,
	trl.released_at,
	trl.organization_id,
	trl.task_id,
	trl.resource_type,
	trl.resource_key,
	trl.concurrency_policy
`

const taskResourceLockAdvisoryNamespace = "distr-task-resource-lock"

type taskResourceLockGroup struct {
	OrganizationID uuid.UUID
	ResourceType   types.TaskLockResourceType
	ResourceKey    string
}

type taskResourceLockSource struct {
	ResourceType types.TaskLockResourceType `db:"resource_type"`
	ResourceKey  string                     `db:"resource_key"`
}

func CreateTasksForDeploymentPlan(
	ctx context.Context,
	request types.CreateTasksForDeploymentPlanRequest,
) ([]types.Task, error) {
	if err := validateCreateTasksForDeploymentPlanRequest(request); err != nil {
		return nil, err
	}
	defaultPolicy, lockResources := normalizeTaskLockResourceRequests(request)
	var tasks []types.Task
	var failedPreflight *types.DeploymentPreflightRun
	err := RunTx(ctx, func(ctx context.Context) error {
		plan, err := GetDeploymentPlan(ctx, request.DeploymentPlanID, request.OrganizationID)
		if err != nil {
			return err
		}
		existing, err := getTasksByDeploymentPlanID(ctx, request.DeploymentPlanID, request.OrganizationID)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			tasks = existing
			return nil
		}
		if plan.Status != types.DeploymentPlanStatusReady {
			return apierrors.NewConflict("deployment plan must be READY before tasks can be created")
		}
		lockGroups, err := getDeploymentPlanTaskResourceLockGroups(
			ctx,
			request.DeploymentPlanID,
			request.OrganizationID,
			lockResources,
		)
		if err != nil {
			return err
		}
		if err := lockTaskResourceAdvisoryGroups(ctx, lockGroups); err != nil {
			return err
		}
		existing, err = getTasksByDeploymentPlanID(ctx, request.DeploymentPlanID, request.OrganizationID)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			tasks = existing
			return nil
		}
		preflight, passed, err := evaluateAndPersistDeploymentPreflight(ctx, *plan, request.ActorUserAccountID)
		if err != nil {
			return err
		}
		if !passed {
			failedPreflight = preflight
			return nil
		}
		created, err := insertTasksForDeploymentPlan(
			ctx,
			request.DeploymentPlanID,
			request.OrganizationID,
			uuidOrNil(request.ActorUserAccountID),
		)
		if err != nil {
			return err
		}
		if err := insertTaskResourceLocksForTasks(
			ctx, created, plan.TargetComponents, defaultPolicy, lockResources,
		); err != nil {
			return err
		}
		if err := applyTaskConcurrencyPolicies(ctx, created); err != nil {
			return err
		}
		if err := insertStepRunsForTasks(ctx, created); err != nil {
			return err
		}
		if err := attachDeploymentPreflightTasks(ctx, preflight.ID, request.OrganizationID); err != nil {
			return err
		}
		if err := markDeploymentPlanExecuted(ctx, plan.ID, plan.OrganizationID); err != nil {
			return err
		}
		tasks, err = getTasksByDeploymentPlanID(ctx, request.DeploymentPlanID, request.OrganizationID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if failedPreflight != nil {
		return nil, apierrors.NewConflict(deploymentPreflightFailureMessage(*failedPreflight))
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
		if request.Status == types.TaskStatusRunning {
			if err := acquireTaskResourceLocks(ctx, request.TaskID, request.OrganizationID); err != nil {
				return err
			}
		}
		if err := updateTaskStatus(ctx, request.TaskID, request.OrganizationID, request.Status); err != nil {
			return err
		}
		if request.Status.IsTerminal() {
			if err := releaseTaskResourceLocks(ctx, request.TaskID, request.OrganizationID); err != nil {
				return err
			}
		}
		task, err = getTask(ctx, request.TaskID, request.OrganizationID)
		return err
	})
	if err != nil {
		return nil, err
	}
	recordTaskTransitionMetric(ctx, task)
	recordTaskTransitionSpan(ctx, task)
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
	if request.ConcurrencyPolicy != "" && !request.ConcurrencyPolicy.IsValid() {
		return apierrors.NewBadRequest("concurrencyPolicy is invalid")
	}
	seen := map[string]struct{}{}
	for i, resource := range request.AdditionalResources {
		if !resource.ResourceType.IsValid() {
			return apierrors.NewBadRequest(fmt.Sprintf("additionalResources[%d].resourceType is invalid", i))
		}
		key := strings.TrimSpace(resource.ResourceKey)
		if key == "" {
			return apierrors.NewBadRequest(fmt.Sprintf("additionalResources[%d].resourceKey is required", i))
		}
		if resource.ConcurrencyPolicy != "" && !resource.ConcurrencyPolicy.IsValid() {
			return apierrors.NewBadRequest(fmt.Sprintf("additionalResources[%d].concurrencyPolicy is invalid", i))
		}
		duplicateKey := string(resource.ResourceType) + "\x00" + key
		if _, ok := seen[duplicateKey]; ok {
			return apierrors.NewBadRequest("additionalResources contains duplicate resource")
		}
		seen[duplicateKey] = struct{}{}
	}
	return nil
}

func normalizeTaskLockResourceRequests(
	request types.CreateTasksForDeploymentPlanRequest,
) (types.TaskConcurrencyPolicy, []types.TaskLockResourceRequest) {
	defaultPolicy := request.ConcurrencyPolicy
	if defaultPolicy == "" {
		defaultPolicy = types.TaskConcurrencyPolicyQueue
	}
	resources := make([]types.TaskLockResourceRequest, 0, len(request.AdditionalResources))
	for _, resource := range request.AdditionalResources {
		policy := resource.ConcurrencyPolicy
		if policy == "" {
			policy = defaultPolicy
		}
		resources = append(resources, types.TaskLockResourceRequest{
			ResourceType:      resource.ResourceType,
			ResourceKey:       strings.TrimSpace(resource.ResourceKey),
			ConcurrencyPolicy: policy,
		})
	}
	return defaultPolicy, resources
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

func uuidOrNil(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func insertTasksForDeploymentPlan(ctx context.Context, planID, orgID uuid.UUID, actorUserAccountID any) ([]types.Task, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`INSERT INTO Task AS t (
			organization_id,
			task_type,
			deployment_plan_id,
			deployment_plan_target_id,
			deployment_target_id,
			application_id,
			release_bundle_id,
			channel_id,
			environment_id,
			actor_user_account_id,
			status
		)
		SELECT
			dp.organization_id,
			@taskType,
			dp.id,
			dpt.id,
			dpt.deployment_target_id,
			dp.application_id,
			dp.release_bundle_id,
			dp.channel_id,
			dp.environment_id,
			@actorUserAccountId,
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
			"deploymentPlanId":   planID,
			"organizationId":     orgID,
			"taskType":           types.TaskTypeDeployment,
			"actorUserAccountId": actorUserAccountID,
			"status":             types.TaskStatusQueued,
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

func getDeploymentPlanTaskResourceLockGroups(
	ctx context.Context,
	planID uuid.UUID,
	orgID uuid.UUID,
	additionalResources []types.TaskLockResourceRequest,
) ([]taskResourceLockGroup, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT CAST(@deploymentTargetType AS text) AS resource_type, dpt.deployment_target_id::text AS resource_key
		FROM DeploymentPlanTarget dpt
		WHERE dpt.deployment_plan_id = @deploymentPlanId
			AND dpt.organization_id = @organizationId
		UNION ALL
		SELECT CAST(@targetComponentType AS text) AS resource_type,
			dptc.deployment_target_id::text || ':' || dptc.component AS resource_key
		FROM DeploymentPlanTargetComponent dptc
		WHERE dptc.deployment_plan_id = @deploymentPlanId
			AND dptc.organization_id = @organizationId
		ORDER BY resource_type, resource_key`,
		pgx.NamedArgs{
			"deploymentPlanId":     planID,
			"organizationId":       orgID,
			"deploymentTargetType": types.TaskLockResourceDeploymentTarget,
			"targetComponentType":  types.TaskLockResourceTargetComponent,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query deployment plan task locks: %w", err)
	}
	sources, err := pgx.CollectRows(rows, pgx.RowToStructByName[taskResourceLockSource])
	if err != nil {
		return nil, fmt.Errorf("could not collect deployment plan task locks: %w", err)
	}
	groups := make([]taskResourceLockGroup, 0, len(sources)+len(additionalResources))
	for _, source := range sources {
		groups = append(groups, taskResourceLockGroup{
			OrganizationID: orgID,
			ResourceType:   source.ResourceType,
			ResourceKey:    source.ResourceKey,
		})
	}
	for _, resource := range additionalResources {
		groups = append(groups, taskResourceLockGroup{
			OrganizationID: orgID,
			ResourceType:   resource.ResourceType,
			ResourceKey:    resource.ResourceKey,
		})
	}
	return distinctTaskResourceLockGroups(groups), nil
}

func insertTaskResourceLocksForTasks(
	ctx context.Context,
	tasks []types.Task,
	targetComponents []types.DeploymentPlanTargetComponent,
	defaultPolicy types.TaskConcurrencyPolicy,
	additionalResources []types.TaskLockResourceRequest,
) error {
	if len(tasks) == 0 {
		return nil
	}
	locks := make([]types.TaskResourceLock, 0, len(tasks)*(1+len(additionalResources))+len(targetComponents))
	for _, task := range tasks {
		locks = append(locks, types.TaskResourceLock{
			OrganizationID:    task.OrganizationID,
			TaskID:            task.ID,
			ResourceType:      types.TaskLockResourceDeploymentTarget,
			ResourceKey:       task.DeploymentTargetID.String(),
			ConcurrencyPolicy: defaultPolicy,
		})
		for _, component := range targetComponents {
			if component.DeploymentPlanTargetID != task.DeploymentPlanTargetID {
				continue
			}
			locks = append(locks, types.TaskResourceLock{
				OrganizationID:    task.OrganizationID,
				TaskID:            task.ID,
				ResourceType:      types.TaskLockResourceTargetComponent,
				ResourceKey:       task.DeploymentTargetID.String() + ":" + component.Component,
				ConcurrencyPolicy: defaultPolicy,
			})
		}
		for _, resource := range additionalResources {
			locks = append(locks, types.TaskResourceLock{
				OrganizationID:    task.OrganizationID,
				TaskID:            task.ID,
				ResourceType:      resource.ResourceType,
				ResourceKey:       resource.ResourceKey,
				ConcurrencyPolicy: resource.ConcurrencyPolicy,
			})
		}
	}
	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"taskresourcelock"},
		[]string{
			"organization_id",
			"task_id",
			"resource_type",
			"resource_key",
			"concurrency_policy",
		},
		pgx.CopyFromSlice(len(locks), func(i int) ([]any, error) {
			lock := locks[i]
			return []any{
				lock.OrganizationID,
				lock.TaskID,
				lock.ResourceType,
				lock.ResourceKey,
				lock.ConcurrencyPolicy,
			}, nil
		}),
	)
	if err != nil {
		return mapTaskWriteError("insert resource locks", err)
	}
	return nil
}

func markDeploymentPlanExecuted(ctx context.Context, planID, orgID uuid.UUID) error {
	database := internalctx.GetDb(ctx)
	result, err := database.Exec(ctx,
		`UPDATE DeploymentPlan
		SET status = @executedStatus
		WHERE id = @deploymentPlanId
			AND organization_id = @organizationId
			AND status = @readyStatus`,
		pgx.NamedArgs{
			"deploymentPlanId": planID,
			"organizationId":   orgID,
			"executedStatus":   types.DeploymentPlanStatusExecuted,
			"readyStatus":      types.DeploymentPlanStatusReady,
		},
	)
	if err != nil {
		return mapTaskWriteError("mark deployment plan executed", err)
	}
	if result.RowsAffected() != 1 {
		return apierrors.NewConflict("deployment plan must be READY before tasks can be created")
	}
	return nil
}

func applyTaskConcurrencyPolicies(ctx context.Context, tasks []types.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	taskIDs := taskIDs(tasks)
	locks, err := getTaskResourceLocksByTaskIDs(ctx, taskIDs)
	if err != nil {
		return err
	}
	groups := taskResourceLockGroupsFromLocks(locks)
	for _, group := range groups {
		if err := lockTaskResourceGroup(ctx, group); err != nil {
			return err
		}
	}
	for _, lock := range distinctTaskResourceLocks(locks) {
		switch lock.ConcurrencyPolicy {
		case types.TaskConcurrencyPolicyRejectNew:
			exists, err := hasNonTerminalTaskResourceConflict(ctx, lock, taskIDs)
			if err != nil {
				return err
			}
			if exists {
				return apierrors.NewConflict("task resource lock conflicts with an existing queued or running task")
			}
		case types.TaskConcurrencyPolicyCancelOlder:
			if err := cancelOlderQueuedTasksForResource(ctx, lock, taskIDs); err != nil {
				return err
			}
		}
	}
	return nil
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
	locks, err := getTaskResourceLocksByTaskID(ctx, task.ID, task.OrganizationID)
	if err != nil {
		return err
	}
	task.Locks = locks
	stepRuns, err := getStepRunsByTaskID(ctx, task.ID, task.OrganizationID)
	if err != nil {
		return err
	}
	task.StepRuns = stepRuns
	return nil
}

func getTaskResourceLocksByTaskID(ctx context.Context, taskID, orgID uuid.UUID) ([]types.TaskResourceLock, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+taskResourceLockOutputExpr+`
		FROM TaskResourceLock trl
		WHERE trl.task_id = @taskId AND trl.organization_id = @organizationId
		ORDER BY trl.resource_type, trl.resource_key, trl.id`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query TaskResourceLock: %w", err)
	}
	locks, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.TaskResourceLock])
	if err != nil {
		return nil, fmt.Errorf("could not collect TaskResourceLock: %w", err)
	}
	return locks, nil
}

func getTaskResourceLocksByTaskIDs(ctx context.Context, taskIDs []uuid.UUID) ([]types.TaskResourceLock, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+taskResourceLockOutputExpr+`
		FROM TaskResourceLock trl
		WHERE trl.task_id = ANY(@taskIds)
		ORDER BY trl.resource_type, trl.resource_key, trl.task_id, trl.id`,
		pgx.NamedArgs{"taskIds": taskIDs},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query TaskResourceLock by tasks: %w", err)
	}
	locks, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.TaskResourceLock])
	if err != nil {
		return nil, fmt.Errorf("could not collect TaskResourceLock by tasks: %w", err)
	}
	return locks, nil
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

func acquireTaskResourceLocks(ctx context.Context, taskID, orgID uuid.UUID) error {
	locks, err := getTaskResourceLocksByTaskID(ctx, taskID, orgID)
	if err != nil {
		return err
	}
	groups := taskResourceLockGroupsFromLocks(locks)
	if err := lockTaskResourceAdvisoryGroups(ctx, groups); err != nil {
		return err
	}
	for _, group := range groups {
		if err := lockTaskResourceGroup(ctx, group); err != nil {
			return err
		}
	}
	locks, err = getTaskResourceLocksForUpdate(ctx, taskID, orgID)
	if err != nil {
		return err
	}
	for _, lock := range locks {
		if lock.ConcurrencyPolicy == types.TaskConcurrencyPolicyAllowParallel {
			continue
		}
		exists, err := hasActiveTaskResourceConflict(ctx, lock, taskID)
		if err != nil {
			return err
		}
		if exists {
			return apierrors.NewConflict("task resource lock is held by another running task")
		}
	}
	if len(locks) == 0 {
		return nil
	}
	db := internalctx.GetDb(ctx)
	_, err = db.Exec(ctx,
		`UPDATE TaskResourceLock
		SET
			acquired_at = COALESCE(acquired_at, now()),
			updated_at = now()
		WHERE task_id = @taskId
			AND organization_id = @organizationId
			AND released_at IS NULL`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	)
	if err != nil {
		return mapTaskWriteError("acquire resource locks", err)
	}
	return nil
}

func getTaskResourceLocksForUpdate(ctx context.Context, taskID, orgID uuid.UUID) ([]types.TaskResourceLock, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+taskResourceLockOutputExpr+`
		FROM TaskResourceLock trl
		WHERE trl.task_id = @taskId AND trl.organization_id = @organizationId
		ORDER BY trl.resource_type, trl.resource_key, trl.id
		FOR UPDATE`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not lock TaskResourceLock: %w", err)
	}
	locks, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.TaskResourceLock])
	if err != nil {
		return nil, fmt.Errorf("could not collect locked TaskResourceLock: %w", err)
	}
	return locks, nil
}

func lockTaskResourceAdvisoryGroups(ctx context.Context, groups []taskResourceLockGroup) error {
	groups = distinctTaskResourceLockGroups(groups)
	db := internalctx.GetDb(ctx)
	for _, group := range groups {
		_, err := db.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtext(@namespace), hashtext(@resourceKey))`,
			pgx.NamedArgs{
				"namespace":   taskResourceLockAdvisoryNamespace,
				"resourceKey": taskResourceLockGroupKey(group),
			},
		)
		if err != nil {
			return fmt.Errorf("could not acquire task resource advisory lock: %w", err)
		}
	}
	return nil
}

func lockTaskResourceGroup(ctx context.Context, group taskResourceLockGroup) error {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT id
		FROM TaskResourceLock
		WHERE organization_id = @organizationId
			AND resource_type = @resourceType
			AND resource_key = @resourceKey
		ORDER BY task_id, id
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationId": group.OrganizationID,
			"resourceType":   group.ResourceType,
			"resourceKey":    group.ResourceKey,
		},
	)
	if err != nil {
		return fmt.Errorf("could not lock TaskResourceLock resource group: %w", err)
	}
	_, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return fmt.Errorf("could not collect TaskResourceLock resource group: %w", err)
	}
	return nil
}

func hasActiveTaskResourceConflict(
	ctx context.Context,
	lock types.TaskResourceLock,
	taskID uuid.UUID,
) (bool, error) {
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM TaskResourceLock trl
			JOIN Task t
				ON t.id = trl.task_id
				AND t.organization_id = trl.organization_id
			WHERE trl.organization_id = @organizationId
				AND trl.resource_type = @resourceType
				AND trl.resource_key = @resourceKey
				AND trl.task_id <> @taskId
				AND trl.acquired_at IS NOT NULL
				AND trl.released_at IS NULL
				AND t.status = @runningStatus
		)`,
		pgx.NamedArgs{
			"organizationId": lock.OrganizationID,
			"resourceType":   lock.ResourceType,
			"resourceKey":    lock.ResourceKey,
			"taskId":         taskID,
			"runningStatus":  types.TaskStatusRunning,
		},
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("could not check active TaskResourceLock conflict: %w", err)
	}
	return exists, nil
}

func hasNonTerminalTaskResourceConflict(
	ctx context.Context,
	lock types.TaskResourceLock,
	taskIDs []uuid.UUID,
) (bool, error) {
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM TaskResourceLock trl
			JOIN Task t
				ON t.id = trl.task_id
				AND t.organization_id = trl.organization_id
			WHERE trl.organization_id = @organizationId
				AND trl.resource_type = @resourceType
				AND trl.resource_key = @resourceKey
				AND NOT (trl.task_id = ANY(@taskIds))
				AND trl.released_at IS NULL
				AND t.status IN (@queuedStatus, @runningStatus)
		)`,
		pgx.NamedArgs{
			"organizationId": lock.OrganizationID,
			"resourceType":   lock.ResourceType,
			"resourceKey":    lock.ResourceKey,
			"taskIds":        taskIDs,
			"queuedStatus":   types.TaskStatusQueued,
			"runningStatus":  types.TaskStatusRunning,
		},
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("could not check TaskResourceLock conflict: %w", err)
	}
	return exists, nil
}

func cancelOlderQueuedTasksForResource(
	ctx context.Context,
	lock types.TaskResourceLock,
	taskIDs []uuid.UUID,
) error {
	db := internalctx.GetDb(ctx)
	args := pgx.NamedArgs{
		"organizationId": lock.OrganizationID,
		"resourceType":   lock.ResourceType,
		"resourceKey":    lock.ResourceKey,
		"taskIds":        taskIDs,
		"queuedStatus":   types.TaskStatusQueued,
		"canceledStatus": types.TaskStatusCanceled,
	}
	_, err := db.Exec(ctx,
		`WITH older_tasks AS (
			SELECT DISTINCT t.id
			FROM TaskResourceLock trl
			JOIN Task t
				ON t.id = trl.task_id
				AND t.organization_id = trl.organization_id
			WHERE trl.organization_id = @organizationId
				AND trl.resource_type = @resourceType
				AND trl.resource_key = @resourceKey
				AND NOT (trl.task_id = ANY(@taskIds))
				AND trl.released_at IS NULL
				AND t.status = @queuedStatus
		)
		UPDATE Task t
		SET
			status = @canceledStatus,
			updated_at = now(),
			completed_at = COALESCE(completed_at, now())
		WHERE t.id IN (SELECT id FROM older_tasks)
			AND t.organization_id = @organizationId`,
		args,
	)
	if err != nil {
		return mapTaskWriteError("cancel older queued tasks", err)
	}
	_, err = db.Exec(ctx,
		`WITH older_tasks AS (
			SELECT DISTINCT t.id
			FROM TaskResourceLock trl
			JOIN Task t
				ON t.id = trl.task_id
				AND t.organization_id = trl.organization_id
			WHERE trl.organization_id = @organizationId
				AND trl.resource_type = @resourceType
				AND trl.resource_key = @resourceKey
				AND NOT (trl.task_id = ANY(@taskIds))
				AND t.status = @canceledStatus
		)
		UPDATE TaskResourceLock trl
		SET
			released_at = COALESCE(released_at, now()),
			updated_at = now()
		WHERE trl.task_id IN (SELECT id FROM older_tasks)
			AND trl.organization_id = @organizationId
			AND trl.released_at IS NULL`,
		args,
	)
	if err != nil {
		return mapTaskWriteError("release canceled task locks", err)
	}
	return nil
}

func releaseTaskResourceLocks(ctx context.Context, taskID, orgID uuid.UUID) error {
	locks, err := getTaskResourceLocksByTaskID(ctx, taskID, orgID)
	if err != nil {
		return err
	}
	if err := lockTaskResourceAdvisoryGroups(ctx, taskResourceLockGroupsFromLocks(locks)); err != nil {
		return err
	}
	db := internalctx.GetDb(ctx)
	_, err = db.Exec(ctx,
		`UPDATE TaskResourceLock
		SET
			released_at = COALESCE(released_at, now()),
			updated_at = now()
		WHERE task_id = @taskId
			AND organization_id = @organizationId
			AND released_at IS NULL`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	)
	if err != nil {
		return mapTaskWriteError("release resource locks", err)
	}
	return nil
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
				WHEN @status = @succeededStatus OR @status = @failedStatus OR @status = @canceledStatus THEN now()
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
			"canceledStatus":  types.TaskStatusCanceled,
		},
	)
	if err != nil {
		return mapTaskWriteError("update status", err)
	}
	if status.IsTerminal() {
		if err := releaseActiveTaskLeasesForTask(ctx, id, orgID); err != nil {
			return err
		}
	}
	return nil
}

func recordTaskTransitionMetric(ctx context.Context, task *types.Task) {
	recorder := internalctx.GetObservabilityMetricsRecorder(ctx)
	if recorder == nil || task == nil {
		return
	}
	observation := obsermetrics.TaskObservation{Status: strings.ToLower(string(task.Status))}
	if task.Status.IsTerminal() && task.StartedAt != nil && task.CompletedAt != nil {
		observation.Duration = task.CompletedAt.Sub(*task.StartedAt)
		if observation.Duration < 0 {
			observation.Duration = 0
		}
	}
	recorder.ObserveTask(observation)
}

func recordTaskTransitionSpan(ctx context.Context, task *types.Task) {
	tracer := internalctx.GetObservabilityTracer(ctx)
	if tracer == nil || task == nil {
		return
	}
	obsertracing.ObserveTaskTransition(ctx, tracer, obsertracing.TaskObservation{
		TaskID:      task.ID.String(),
		Status:      string(task.Status),
		StartedAt:   task.StartedAt,
		CompletedAt: task.CompletedAt,
	})
}

func taskIDs(tasks []types.Task) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func distinctTaskResourceLocks(locks []types.TaskResourceLock) []types.TaskResourceLock {
	seen := map[string]struct{}{}
	distinct := make([]types.TaskResourceLock, 0, len(locks))
	for _, lock := range locks {
		key := string(lock.ResourceType) + "\x00" + lock.ResourceKey
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		distinct = append(distinct, lock)
	}
	return distinct
}

func taskResourceLockGroupsFromLocks(locks []types.TaskResourceLock) []taskResourceLockGroup {
	groups := make([]taskResourceLockGroup, 0, len(locks))
	for _, lock := range locks {
		groups = append(groups, taskResourceLockGroup{
			OrganizationID: lock.OrganizationID,
			ResourceType:   lock.ResourceType,
			ResourceKey:    lock.ResourceKey,
		})
	}
	return distinctTaskResourceLockGroups(groups)
}

func distinctTaskResourceLockGroups(groups []taskResourceLockGroup) []taskResourceLockGroup {
	distinct := make([]taskResourceLockGroup, 0, len(groups))
	seen := map[string]struct{}{}
	for _, group := range groups {
		key := taskResourceLockGroupKey(group)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		distinct = append(distinct, group)
	}
	sort.Slice(distinct, func(i, j int) bool {
		return taskResourceLockGroupKey(distinct[i]) < taskResourceLockGroupKey(distinct[j])
	})
	return distinct
}

func taskResourceLockGroupKey(group taskResourceLockGroup) string {
	resourceType := string(group.ResourceType)
	return fmt.Sprintf(
		"%s:%d:%s:%d:%s",
		group.OrganizationID,
		len(resourceType),
		resourceType,
		len(group.ResourceKey),
		group.ResourceKey,
	)
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
