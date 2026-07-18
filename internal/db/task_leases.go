package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	taskLeaseDuration   = 2 * time.Minute
	taskLeaseTokenBytes = 32
)

const taskLeaseOutputExpr = `
	tl.id,
	tl.created_at,
	tl.updated_at,
	tl.organization_id,
	tl.task_id,
	tl.agent_id,
	tl.executor_type,
	tl.lease_token_hash,
	tl.leased_at,
	tl.expires_at,
	tl.heartbeat_at,
	tl.attempt,
	tl.released_at,
	dp.canonical_checksum AS plan_checksum
`

type taskLeaseCandidate struct {
	TaskID             uuid.UUID        `db:"id"`
	OrganizationID     uuid.UUID        `db:"organization_id"`
	DeploymentTargetID uuid.UUID        `db:"deployment_target_id"`
	Status             types.TaskStatus `db:"status"`
}

type taskLeaseStepCandidate struct {
	StepRunID         uuid.UUID           `db:"step_run_id"`
	StepKey           string              `db:"step_key"`
	Name              string              `db:"name"`
	ActionType        string              `db:"action_type"`
	InputBindings     map[string]any      `db:"input_bindings"`
	SortOrder         int                 `db:"sort_order"`
	Status            types.StepRunStatus `db:"status"`
	ExecutionLocation string              `db:"execution_location"`
	Included          bool                `db:"included"`
	Dependencies      []string            `db:"dependencies"`
}

func LeaseAgentTask(ctx context.Context, request types.LeaseAgentTaskRequest) (*types.TaskLease, error) {
	if err := validateLeaseAgentTaskRequest(request); err != nil {
		return nil, err
	}
	var lease *types.TaskLease
	err := RunTx(ctx, func(ctx context.Context) error {
		candidate, err := getNextTaskLeaseCandidateForUpdate(ctx, request)
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		lease, err = leaseTaskCandidate(ctx, *candidate, types.TaskExecutorTypeAgent)
		return err
	})
	if err != nil {
		return nil, err
	}
	return lease, nil
}

func LeaseHubTask(ctx context.Context) (*types.TaskLease, error) {
	var lease *types.TaskLease
	err := RunTx(ctx, func(ctx context.Context) error {
		candidate, err := getNextHubTaskLeaseCandidateForUpdate(ctx)
		if errors.Is(err, apierrors.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		lease, err = leaseTaskCandidate(ctx, *candidate, types.TaskExecutorTypeHub)
		return err
	})
	if err != nil {
		return nil, err
	}
	return lease, nil
}

func leaseTaskCandidate(
	ctx context.Context,
	candidate taskLeaseCandidate,
	executorType types.TaskExecutorType,
) (*types.TaskLease, error) {
	published, err := lockTaskReleaseBundlePublishedForLease(
		ctx, candidate.TaskID, candidate.OrganizationID,
	)
	if err != nil || !published {
		return nil, err
	}
	attempt := 1
	if candidate.Status == types.TaskStatusRunning {
		attempt, err = releaseExpiredTaskLeasesAndNextAttempt(
			ctx, candidate.TaskID, candidate.OrganizationID,
		)
		if err != nil {
			return nil, err
		}
	}
	steps, err := getReadyTaskLeaseStepCandidates(
		ctx, candidate.TaskID, candidate.OrganizationID, executorType.ExecutionLocation(),
	)
	if err != nil || len(steps) == 0 {
		return nil, err
	}
	if candidate.Status == types.TaskStatusQueued {
		err := acquireTaskResourceLocks(ctx, candidate.TaskID, candidate.OrganizationID)
		if errors.Is(err, apierrors.ErrConflict) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if err := updateTaskStatus(
			ctx, candidate.TaskID, candidate.OrganizationID, types.TaskStatusRunning,
		); err != nil {
			return nil, err
		}
		attempt, err = releaseExpiredTaskLeasesAndNextAttempt(
			ctx, candidate.TaskID, candidate.OrganizationID,
		)
		if err != nil {
			return nil, err
		}
	}
	token, tokenHash, err := newTaskLeaseToken()
	if err != nil {
		return nil, err
	}
	leaseID, err := insertTaskLease(
		ctx,
		candidate.OrganizationID,
		candidate.DeploymentTargetID,
		candidate.TaskID,
		executorType,
		tokenHash,
		attempt,
	)
	if err != nil {
		return nil, err
	}
	lease, err := getTaskLease(ctx, leaseID, candidate.OrganizationID)
	if err != nil {
		return nil, err
	}
	lease.LeaseToken = token
	return lease, nil
}

func HeartbeatAgentTaskLease(
	ctx context.Context,
	request types.HeartbeatAgentTaskLeaseRequest,
) (*types.TaskLease, error) {
	if err := validateHeartbeatAgentTaskLeaseRequest(request); err != nil {
		return nil, err
	}
	return heartbeatTaskLease(
		ctx,
		request.OrganizationID,
		request.AgentID,
		request.TaskID,
		request.LeaseToken,
		types.TaskExecutorTypeAgent,
	)
}

func HeartbeatHubTaskLease(
	ctx context.Context,
	request types.HeartbeatHubTaskLeaseRequest,
) (*types.TaskLease, error) {
	if request.OrganizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if request.DeploymentTargetID == uuid.Nil {
		return nil, apierrors.NewBadRequest("deploymentTargetId is required")
	}
	if request.TaskID == uuid.Nil {
		return nil, apierrors.NewBadRequest("taskId is required")
	}
	if strings.TrimSpace(request.LeaseToken) == "" {
		return nil, apierrors.NewBadRequest("leaseToken is required")
	}
	return heartbeatTaskLease(
		ctx,
		request.OrganizationID,
		request.DeploymentTargetID,
		request.TaskID,
		strings.TrimSpace(request.LeaseToken),
		types.TaskExecutorTypeHub,
	)
}

func heartbeatTaskLease(
	ctx context.Context,
	organizationID, deploymentTargetID, taskID uuid.UUID,
	leaseToken string,
	executorType types.TaskExecutorType,
) (*types.TaskLease, error) {
	var lease *types.TaskLease
	err := RunTx(ctx, func(ctx context.Context) error {
		status, err := getTaskStatusForUpdate(ctx, taskID, organizationID)
		if err != nil {
			return err
		}
		if status != types.TaskStatusRunning {
			return apierrors.ErrNotFound
		}
		leaseID, expired, err := getActiveTaskLeaseIDForHeartbeat(
			ctx,
			organizationID,
			deploymentTargetID,
			taskID,
			leaseToken,
			executorType,
		)
		if err != nil {
			return err
		}
		if expired {
			return apierrors.NewConflict("task lease has expired")
		}
		if err := updateTaskLeaseHeartbeat(ctx, leaseID, organizationID); err != nil {
			return err
		}
		lease, err = getTaskLease(ctx, leaseID, organizationID)
		if err != nil {
			return err
		}
		lease.LeaseToken = leaseToken
		return nil
	})
	if err != nil {
		return nil, err
	}
	return lease, nil
}

func validateLeaseAgentTaskRequest(request types.LeaseAgentTaskRequest) error {
	if request.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if request.AgentID == uuid.Nil {
		return apierrors.NewBadRequest("agentId is required")
	}
	return nil
}

func validateHeartbeatAgentTaskLeaseRequest(request types.HeartbeatAgentTaskLeaseRequest) error {
	if request.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if request.AgentID == uuid.Nil {
		return apierrors.NewBadRequest("agentId is required")
	}
	if request.TaskID == uuid.Nil {
		return apierrors.NewBadRequest("taskId is required")
	}
	if request.LeaseToken == "" {
		return apierrors.NewBadRequest("leaseToken is required")
	}
	return nil
}

func getNextTaskLeaseCandidateForUpdate(
	ctx context.Context,
	request types.LeaseAgentTaskRequest,
) (*taskLeaseCandidate, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			t.id,
			t.organization_id,
			t.deployment_target_id,
			t.status
		FROM Task t
		LEFT JOIN TaskLease active_lease
			ON active_lease.task_id = t.id
			AND active_lease.organization_id = t.organization_id
			AND active_lease.released_at IS NULL
		WHERE t.organization_id = @organizationId
			AND t.deployment_target_id = @agentId
			AND t.protocol_version = 'v1'
			AND (
			t.status = @queuedStatus
				OR (
					t.status = @runningStatus
					AND (
						active_lease.id IS NULL
						OR active_lease.expires_at <= now()
					)
				)
			)
			AND EXISTS (
				SELECT 1
				FROM ReleaseBundle rb
				WHERE rb.id = t.release_bundle_id
					AND rb.organization_id = t.organization_id
					AND rb.status = @publishedStatus
			)
			AND EXISTS (
				SELECT 1
				FROM StepRun sr
				JOIN DeploymentPlanStep dps
					ON dps.id = sr.deployment_plan_step_id
					AND dps.deployment_plan_id = sr.deployment_plan_id
					AND dps.organization_id = sr.organization_id
				WHERE sr.task_id = t.id
					AND sr.organization_id = t.organization_id
					AND sr.status IN (@pendingStepStatus, @runningStepStatus)
					AND dps.included
					AND lower(trim(dps.execution_location)) = 'target'
					AND NOT EXISTS (
						SELECT 1
						FROM unnest(dps.dependencies) dependency(step_key)
						LEFT JOIN StepRun dependency_run
							ON dependency_run.task_id = sr.task_id
							AND dependency_run.organization_id = sr.organization_id
							AND dependency_run.step_key = dependency.step_key
						WHERE dependency_run.id IS NULL
							OR dependency_run.status NOT IN (@succeededStepStatus, @skippedStepStatus)
					)
			)
		ORDER BY t.queue_order, t.id
		LIMIT 1
		FOR UPDATE OF t SKIP LOCKED`,
		pgx.NamedArgs{
			"organizationId":      request.OrganizationID,
			"agentId":             request.AgentID,
			"queuedStatus":        types.TaskStatusQueued,
			"runningStatus":       types.TaskStatusRunning,
			"pendingStepStatus":   types.StepRunStatusPending,
			"runningStepStatus":   types.StepRunStatusRunning,
			"succeededStepStatus": types.StepRunStatusSucceeded,
			"skippedStepStatus":   types.StepRunStatusSkipped,
			"publishedStatus":     types.ReleaseBundleStatusPublished,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query next Task lease candidate: %w", err)
	}
	candidate, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[taskLeaseCandidate])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect next Task lease candidate: %w", err)
	}
	return &candidate, nil
}

func getNextHubTaskLeaseCandidateForUpdate(ctx context.Context) (*taskLeaseCandidate, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			t.id,
			t.organization_id,
			t.deployment_target_id,
			t.status
		FROM Task t
		LEFT JOIN TaskLease active_lease
			ON active_lease.task_id = t.id
			AND active_lease.organization_id = t.organization_id
			AND active_lease.released_at IS NULL
		WHERE t.protocol_version = 'v1'
		AND (
			t.status = @queuedStatus
				OR (
					t.status = @runningStatus
					AND (
						active_lease.id IS NULL
						OR active_lease.expires_at <= now()
					)
				)
		)
		AND EXISTS (
			SELECT 1
			FROM ReleaseBundle rb
			WHERE rb.id = t.release_bundle_id
				AND rb.organization_id = t.organization_id
				AND rb.status = @publishedStatus
		)
		AND EXISTS (
			SELECT 1
			FROM StepRun sr
			JOIN DeploymentPlanStep dps
				ON dps.id = sr.deployment_plan_step_id
				AND dps.deployment_plan_id = sr.deployment_plan_id
				AND dps.organization_id = sr.organization_id
			WHERE sr.task_id = t.id
				AND sr.organization_id = t.organization_id
				AND sr.status IN (@pendingStepStatus, @runningStepStatus)
				AND dps.included
				AND lower(trim(dps.execution_location)) = 'hub'
				AND NOT EXISTS (
					SELECT 1
					FROM unnest(dps.dependencies) dependency(step_key)
					LEFT JOIN StepRun dependency_run
						ON dependency_run.task_id = sr.task_id
						AND dependency_run.organization_id = sr.organization_id
						AND dependency_run.step_key = dependency.step_key
					WHERE dependency_run.id IS NULL
						OR dependency_run.status NOT IN (@succeededStepStatus, @skippedStepStatus)
				)
		)
		ORDER BY t.queue_order, t.id
		LIMIT 1
		FOR UPDATE OF t SKIP LOCKED`,
		pgx.NamedArgs{
			"queuedStatus":        types.TaskStatusQueued,
			"runningStatus":       types.TaskStatusRunning,
			"pendingStepStatus":   types.StepRunStatusPending,
			"runningStepStatus":   types.StepRunStatusRunning,
			"succeededStepStatus": types.StepRunStatusSucceeded,
			"skippedStepStatus":   types.StepRunStatusSkipped,
			"publishedStatus":     types.ReleaseBundleStatusPublished,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query next Hub Task lease candidate: %w", err)
	}
	candidate, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[taskLeaseCandidate])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect next Hub Task lease candidate: %w", err)
	}
	return &candidate, nil
}

func lockTaskReleaseBundlePublishedForLease(ctx context.Context, taskID, orgID uuid.UUID) (bool, error) {
	db := internalctx.GetDb(ctx)
	var status types.ReleaseBundleStatus
	err := db.QueryRow(ctx,
		`SELECT rb.status
		FROM Task t
		JOIN ReleaseBundle rb
			ON rb.id = t.release_bundle_id
			AND rb.organization_id = t.organization_id
		WHERE t.id = @taskId
			AND t.organization_id = @organizationId
		FOR UPDATE OF rb`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, apierrors.ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("could not lock Task ReleaseBundle for lease: %w", err)
	}
	return status == types.ReleaseBundleStatusPublished, nil
}

func releaseExpiredTaskLeasesAndNextAttempt(ctx context.Context, taskID, orgID uuid.UUID) (int, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT attempt
		FROM TaskLease
		WHERE task_id = @taskId
			AND organization_id = @organizationId
		FOR UPDATE`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	)
	if err != nil {
		return 0, fmt.Errorf("could not lock TaskLease attempts: %w", err)
	}
	attempts, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return 0, fmt.Errorf("could not collect TaskLease attempts: %w", err)
	}
	nextAttempt := 1
	for _, attempt := range attempts {
		if attempt >= nextAttempt {
			nextAttempt = attempt + 1
		}
	}
	rows, err = db.Query(ctx,
		`UPDATE TaskLease
		SET
			released_at = COALESCE(released_at, now()),
			updated_at = now()
		WHERE task_id = @taskId
			AND organization_id = @organizationId
			AND released_at IS NULL
			AND expires_at <= now()
		RETURNING executor_type`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	)
	if err != nil {
		return 0, fmt.Errorf("could not release expired TaskLease: %w", err)
	}
	executorTypes, err := pgx.CollectRows(rows, pgx.RowTo[types.TaskExecutorType])
	if err != nil {
		return 0, fmt.Errorf("could not collect released TaskLease executor types: %w", err)
	}
	for _, executorType := range executorTypes {
		if !executorType.IsValid() {
			return 0, fmt.Errorf("could not reset interrupted StepRuns: invalid TaskLease executor type %q", executorType)
		}
		if err := resetInterruptedStepRunsForTaskExecutor(ctx, taskID, orgID, executorType); err != nil {
			return 0, err
		}
	}
	return nextAttempt, nil
}

func resetInterruptedStepRunsForTaskExecutor(
	ctx context.Context,
	taskID, orgID uuid.UUID,
	executorType types.TaskExecutorType,
) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`UPDATE StepRun sr
		SET
			status = @pendingStatus,
			updated_at = now()
		FROM DeploymentPlanStep dps
		WHERE sr.task_id = @taskId
			AND sr.organization_id = @organizationId
			AND sr.status = @runningStatus
			AND dps.id = sr.deployment_plan_step_id
			AND dps.deployment_plan_id = sr.deployment_plan_id
			AND dps.organization_id = sr.organization_id
			AND lower(trim(dps.execution_location)) = @executionLocation`,
		pgx.NamedArgs{
			"taskId":            taskID,
			"organizationId":    orgID,
			"executionLocation": executorType.ExecutionLocation(),
			"pendingStatus":     types.StepRunStatusPending,
			"runningStatus":     types.StepRunStatusRunning,
		},
	)
	if err != nil {
		return mapTaskWriteError("reset interrupted step runs", err)
	}
	return nil
}

func insertTaskLease(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentTargetID uuid.UUID,
	taskID uuid.UUID,
	executorType types.TaskExecutorType,
	tokenHash string,
	attempt int,
) (uuid.UUID, error) {
	db := internalctx.GetDb(ctx)
	var leaseID uuid.UUID
	err := db.QueryRow(ctx,
		`INSERT INTO TaskLease (
			organization_id,
			task_id,
			agent_id,
			executor_type,
			lease_token_hash,
			expires_at,
			attempt
		)
		VALUES (
			@organizationId,
			@taskId,
			@agentId,
			@executorType,
			@leaseTokenHash,
			now() + @leaseDuration::interval,
			@attempt
		)
		RETURNING id`,
		pgx.NamedArgs{
			"organizationId": organizationID,
			"taskId":         taskID,
			"agentId":        deploymentTargetID,
			"executorType":   executorType,
			"leaseTokenHash": tokenHash,
			"leaseDuration":  taskLeaseDuration.String(),
			"attempt":        attempt,
		},
	).Scan(&leaseID)
	if err != nil {
		return uuid.Nil, mapTaskWriteError("insert lease", err)
	}
	return leaseID, nil
}

func getActiveTaskLeaseIDForHeartbeat(
	ctx context.Context,
	organizationID, deploymentTargetID, taskID uuid.UUID,
	leaseToken string,
	executorType types.TaskExecutorType,
) (uuid.UUID, bool, error) {
	db := internalctx.GetDb(ctx)
	var leaseID uuid.UUID
	var expired bool
	err := db.QueryRow(ctx,
		`SELECT tl.id, tl.expires_at <= now() AS expired
		FROM TaskLease tl
		WHERE tl.organization_id = @organizationId
			AND tl.agent_id = @agentId
			AND tl.task_id = @taskId
			AND tl.executor_type = @executorType
			AND tl.lease_token_hash = @leaseTokenHash
			AND tl.released_at IS NULL
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationId": organizationID,
			"agentId":        deploymentTargetID,
			"taskId":         taskID,
			"executorType":   executorType,
			"leaseTokenHash": hashTaskLeaseToken(leaseToken),
		},
	).Scan(&leaseID, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, apierrors.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("could not lock TaskLease heartbeat: %w", err)
	}
	return leaseID, expired, nil
}

func releaseActiveTaskLeasesForTask(ctx context.Context, taskID, orgID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`UPDATE TaskLease
		SET
			released_at = COALESCE(released_at, now()),
			updated_at = now()
		WHERE task_id = @taskId
			AND organization_id = @organizationId
			AND released_at IS NULL`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	)
	if err != nil {
		return mapTaskWriteError("release task leases", err)
	}
	return nil
}

func releaseTaskLeaseIfExecutorBatchComplete(
	ctx context.Context,
	taskID, orgID, leaseID uuid.UUID,
	executionLocation string,
) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`UPDATE TaskLease
		SET
			released_at = COALESCE(released_at, now()),
			updated_at = now()
		WHERE id = @leaseId
			AND task_id = @taskId
			AND organization_id = @organizationId
			AND released_at IS NULL
			AND NOT EXISTS (
				SELECT 1
				FROM StepRun sr
				JOIN DeploymentPlanStep dps
					ON dps.id = sr.deployment_plan_step_id
					AND dps.deployment_plan_id = sr.deployment_plan_id
					AND dps.organization_id = sr.organization_id
				WHERE sr.task_id = @taskId
					AND sr.organization_id = @organizationId
					AND sr.status IN (@pendingStatus, @runningStatus)
					AND dps.included
					AND lower(trim(dps.execution_location)) = @executionLocation
			)`,
		pgx.NamedArgs{
			"leaseId":           leaseID,
			"taskId":            taskID,
			"organizationId":    orgID,
			"executionLocation": executionLocation,
			"pendingStatus":     types.StepRunStatusPending,
			"runningStatus":     types.StepRunStatusRunning,
		},
	)
	if err != nil {
		return mapTaskWriteError("release completed executor batch lease", err)
	}
	return nil
}

func updateTaskLeaseHeartbeat(ctx context.Context, leaseID, orgID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`UPDATE TaskLease
		SET
			heartbeat_at = now(),
			expires_at = now() + @leaseDuration::interval,
			updated_at = now()
		WHERE id = @leaseId
			AND organization_id = @organizationId
			AND released_at IS NULL`,
		pgx.NamedArgs{
			"leaseId":        leaseID,
			"organizationId": orgID,
			"leaseDuration":  taskLeaseDuration.String(),
		},
	)
	if err != nil {
		return mapTaskWriteError("heartbeat lease", err)
	}
	return nil
}

func getTaskLease(ctx context.Context, leaseID, orgID uuid.UUID) (*types.TaskLease, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+taskLeaseOutputExpr+`
		FROM TaskLease tl
		JOIN Task t
			ON t.id = tl.task_id
			AND t.organization_id = tl.organization_id
		JOIN DeploymentPlan dp
			ON dp.id = t.deployment_plan_id
			AND dp.organization_id = t.organization_id
		WHERE tl.id = @leaseId
			AND tl.organization_id = @organizationId`,
		pgx.NamedArgs{"leaseId": leaseID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query TaskLease: %w", err)
	}
	lease, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.TaskLease])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect TaskLease: %w", err)
	}
	task, err := getTask(ctx, lease.TaskID, lease.OrganizationID)
	if err != nil {
		return nil, err
	}
	lease.Task = *task
	steps, err := getTaskLeaseSteps(ctx, lease.Task, lease.ExecutorType.ExecutionLocation())
	if err != nil {
		return nil, err
	}
	lease.Steps = steps
	return &lease, nil
}

func getTaskLeaseSteps(ctx context.Context, task types.Task, executionLocation string) ([]types.TaskLeaseStep, error) {
	candidates, err := getReadyTaskLeaseStepCandidates(
		ctx, task.ID, task.OrganizationID, executionLocation,
	)
	if err != nil {
		return nil, err
	}
	steps := make([]types.TaskLeaseStep, 0, len(candidates))
	for _, candidate := range candidates {
		step := types.TaskLeaseStep{
			StepRunID:     candidate.StepRunID,
			StepKey:       candidate.StepKey,
			Name:          candidate.Name,
			ActionType:    candidate.ActionType,
			InputBindings: candidate.InputBindings,
			SortOrder:     candidate.SortOrder,
		}
		if step.InputBindings == nil {
			step.InputBindings = map[string]any{}
		}
		step.ActionVersion = types.AgentActionVersionV1
		secretReferences, err := resolveTaskLeaseStepSecrets(ctx, task, &step)
		if err != nil {
			return nil, err
		}
		step.SecretReferences = secretReferences
		step.IdempotencyKey = taskLeaseStepIdempotencyKey(task, step)
		steps = append(steps, step)
	}
	return steps, nil
}

func resolveTaskLeaseStepSecrets(
	ctx context.Context,
	task types.Task,
	step *types.TaskLeaseStep,
) ([]string, error) {
	switch step.ActionType {
	case "distr.compose.deploy":
		return resolveComposeRegistryAuthSecrets(ctx, task, step.InputBindings)
	case "distr.oci.job":
		return resolveOCIJobSecretEnvironment(ctx, task, step.InputBindings)
	case "distr.file.render":
		return resolveFileRenderSecretVariables(ctx, task, step.InputBindings)
	case "distr.webhook":
		return resolveWebhookSecrets(ctx, task, step.InputBindings)
	default:
		return []string{}, nil
	}
}

func resolveComposeRegistryAuthSecrets(
	ctx context.Context,
	task types.Task,
	inputBindings map[string]any,
) ([]string, error) {
	applicationVersion, ok := mapStringAny(inputBindings["applicationVersion"])
	if !ok {
		return []string{}, nil
	}
	registryAuth, ok := mapStringAny(applicationVersion["registryAuth"])
	if !ok {
		return []string{}, nil
	}
	references := make([]string, 0, len(registryAuth))
	for registry, rawAuth := range registryAuth {
		auth, ok := mapStringAny(rawAuth)
		if !ok {
			continue
		}
		if _, hasPlaintext := auth["password"]; hasPlaintext {
			return nil, apierrors.NewBadRequest("compose registryAuth password must use passwordSecretRef")
		}
		reference, ok := stringValue(auth["passwordSecretRef"])
		if !ok || strings.TrimSpace(reference) == "" {
			return nil, apierrors.NewBadRequest("compose registryAuth passwordSecretRef is required")
		}
		reference = strings.TrimSpace(reference)
		value, err := getTaskLeaseSecretValue(ctx, task, reference)
		if err != nil {
			return nil, err
		}
		auth["password"] = value
		delete(auth, "passwordSecretRef")
		registryAuth[registry] = auth
		references = append(references, "secret:"+reference)
	}
	sort.Strings(references)
	return references, nil
}

func resolveOCIJobSecretEnvironment(
	ctx context.Context,
	task types.Task,
	inputBindings map[string]any,
) ([]string, error) {
	secretEnvironment, ok := mapStringAny(inputBindings["secretEnvironment"])
	if !ok {
		if _, exists := inputBindings["secretEnvironment"]; exists {
			return nil, apierrors.NewBadRequest("oci job secretEnvironment must be an object")
		}
		return []string{}, nil
	}
	environment, ok := mapStringAny(inputBindings["environment"])
	if !ok {
		if _, exists := inputBindings["environment"]; exists {
			return nil, apierrors.NewBadRequest("oci job environment must be an object")
		}
		environment = map[string]any{}
	}
	resolvedSecretEnvironment := map[string]any{}
	references := make([]string, 0, len(secretEnvironment))
	for rawName, rawReference := range secretEnvironment {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, apierrors.NewBadRequest("oci job secretEnvironment name is required")
		}
		if _, exists := environment[name]; exists {
			return nil, apierrors.NewBadRequest("oci job environment conflicts with secretEnvironment")
		}
		reference, ok := stringValue(rawReference)
		if !ok || strings.TrimSpace(reference) == "" {
			return nil, apierrors.NewBadRequest("oci job secretEnvironment value must be a secret reference")
		}
		reference = strings.TrimSpace(reference)
		value, err := getTaskLeaseSecretValue(ctx, task, reference)
		if err != nil {
			return nil, err
		}
		resolvedSecretEnvironment[name] = value
		references = append(references, "secret:"+reference)
	}
	inputBindings["environment"] = environment
	inputBindings["secretEnvironment"] = resolvedSecretEnvironment
	sort.Strings(references)
	return references, nil
}

func resolveFileRenderSecretVariables(
	ctx context.Context,
	task types.Task,
	inputBindings map[string]any,
) ([]string, error) {
	secretVariables, ok := mapStringAny(inputBindings["secretVariables"])
	if !ok {
		if _, exists := inputBindings["secretVariables"]; exists {
			return nil, apierrors.NewBadRequest("file render secretVariables must be an object")
		}
		return []string{}, nil
	}
	variables, ok := mapStringAny(inputBindings["variables"])
	if !ok {
		if _, exists := inputBindings["variables"]; exists {
			return nil, apierrors.NewBadRequest("file render variables must be an object")
		}
		variables = map[string]any{}
	}
	resolvedSecretVariables := map[string]any{}
	references := make([]string, 0, len(secretVariables))
	for rawName, rawReference := range secretVariables {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, apierrors.NewBadRequest("file render secretVariables name is required")
		}
		if _, exists := variables[name]; exists {
			return nil, apierrors.NewBadRequest("file render variables conflicts with secretVariables")
		}
		reference, ok := stringValue(rawReference)
		if !ok || strings.TrimSpace(reference) == "" {
			return nil, apierrors.NewBadRequest("file render secretVariables value must be a secret reference")
		}
		reference = strings.TrimSpace(reference)
		value, err := getTaskLeaseSecretValue(ctx, task, reference)
		if err != nil {
			return nil, err
		}
		resolvedSecretVariables[name] = value
		references = append(references, "secret:"+reference)
	}
	inputBindings["variables"] = variables
	inputBindings["secretVariables"] = resolvedSecretVariables
	sort.Strings(references)
	return references, nil
}

func resolveWebhookSecrets(
	ctx context.Context,
	task types.Task,
	inputBindings map[string]any,
) ([]string, error) {
	headers, ok := mapStringAny(inputBindings["headers"])
	if !ok {
		if _, exists := inputBindings["headers"]; exists {
			return nil, apierrors.NewBadRequest("webhook headers must be an object")
		}
		headers = map[string]any{}
	}
	secretHeaders, ok := mapStringAny(inputBindings["secretHeaders"])
	if !ok {
		if _, exists := inputBindings["secretHeaders"]; exists {
			return nil, apierrors.NewBadRequest("webhook secretHeaders must be an object")
		}
		secretHeaders = map[string]any{}
	}
	signingReference, hasSigningReference := stringValue(inputBindings["signingSecret"])
	rawSigningReferences, hasSigningReferences := inputBindings["signingSecrets"]
	signingReferences, ok := stringSliceValue(rawSigningReferences)
	if hasSigningReferences && !ok {
		return nil, apierrors.NewBadRequest("webhook signingSecrets must be secret references")
	}
	if strings.TrimSpace(signingReference) != "" && len(signingReferences) > 0 {
		return nil, apierrors.NewBadRequest("webhook signingSecret and signingSecrets cannot both be set")
	}
	if !hasSigningReference && len(signingReferences) == 0 {
		return nil, apierrors.NewBadRequest("webhook signingSecret or signingSecrets must be a secret reference")
	}
	references := []string{}
	if len(signingReferences) > 0 {
		resolvedSigningSecrets := make([]any, 0, len(signingReferences))
		seenSigningReferences := map[string]struct{}{}
		for _, rawReference := range signingReferences {
			reference := strings.TrimSpace(rawReference)
			if reference == "" {
				return nil, apierrors.NewBadRequest("webhook signingSecrets must be secret references")
			}
			if _, exists := seenSigningReferences[reference]; exists {
				return nil, apierrors.NewBadRequest("webhook signingSecrets contains duplicate secret reference")
			}
			seenSigningReferences[reference] = struct{}{}
			value, err := getTaskLeaseSecretValue(ctx, task, reference)
			if err != nil {
				return nil, err
			}
			resolvedSigningSecrets = append(resolvedSigningSecrets, value)
			references = append(references, "secret:"+reference)
		}
		inputBindings["signingSecrets"] = resolvedSigningSecrets
		delete(inputBindings, "signingSecret")
	} else {
		if !hasSigningReference || strings.TrimSpace(signingReference) == "" {
			return nil, apierrors.NewBadRequest("webhook signingSecret must be a secret reference")
		}
		signingReference = strings.TrimSpace(signingReference)
		signingValue, err := getTaskLeaseSecretValue(ctx, task, signingReference)
		if err != nil {
			return nil, err
		}
		references = append(references, "secret:"+signingReference)
		inputBindings["signingSecret"] = signingValue
	}
	resolvedSecretHeaders := map[string]any{}
	for rawName, rawReference := range secretHeaders {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, apierrors.NewBadRequest("webhook secretHeaders name is required")
		}
		if _, exists := headers[name]; exists {
			return nil, apierrors.NewBadRequest("webhook headers conflicts with secretHeaders")
		}
		reference, ok := stringValue(rawReference)
		if !ok || strings.TrimSpace(reference) == "" {
			return nil, apierrors.NewBadRequest("webhook secretHeaders value must be a secret reference")
		}
		reference = strings.TrimSpace(reference)
		value, err := getTaskLeaseSecretValue(ctx, task, reference)
		if err != nil {
			return nil, err
		}
		resolvedSecretHeaders[name] = value
		references = append(references, "secret:"+reference)
	}
	inputBindings["headers"] = headers
	inputBindings["secretHeaders"] = resolvedSecretHeaders
	sort.Strings(references)
	return references, nil
}

func getTaskLeaseSecretValue(ctx context.Context, task types.Task, key string) (string, error) {
	db := internalctx.GetDb(ctx)
	var value string
	err := db.QueryRow(ctx,
		`SELECT s.value
		FROM DeploymentPlanTarget dpt
		JOIN Secret s
			ON s.organization_id = dpt.organization_id
			AND s.key = @key
			AND (
				(dpt.customer_organization_id IS NULL AND s.customer_organization_id IS NULL)
				OR s.customer_organization_id = dpt.customer_organization_id
			)
		WHERE dpt.id = @deploymentPlanTargetId
			AND dpt.organization_id = @organizationId`,
		pgx.NamedArgs{
			"deploymentPlanTargetId": task.DeploymentPlanTargetID,
			"organizationId":         task.OrganizationID,
			"key":                    key,
		},
	).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apierrors.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("could not resolve task lease secret %q: %w", key, err)
	}
	return value, nil
}

func mapStringAny(value any) (map[string]any, bool) {
	typed, ok := value.(map[string]any)
	return typed, ok
}

func stringValue(value any) (string, bool) {
	typed, ok := value.(string)
	return typed, ok
}

func stringSliceValue(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), true
	case []any:
		values := make([]string, 0, len(typed))
		for _, rawValue := range typed {
			value, ok := stringValue(rawValue)
			if !ok {
				return nil, false
			}
			values = append(values, value)
		}
		return values, true
	default:
		return nil, false
	}
}

func getReadyTaskLeaseStepCandidates(
	ctx context.Context,
	taskID, orgID uuid.UUID,
	executionLocation string,
) ([]taskLeaseStepCandidate, error) {
	candidates, err := getTaskLeaseStepCandidates(ctx, taskID, orgID)
	if err != nil {
		return nil, err
	}
	return readyTaskLeaseStepCandidates(candidates, executionLocation), nil
}

func getTaskLeaseStepCandidates(ctx context.Context, taskID, orgID uuid.UUID) ([]taskLeaseStepCandidate, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			sr.id AS step_run_id,
			sr.step_key,
			sr.name,
			sr.action_type,
			dps.input_bindings,
			sr.sort_order,
			sr.status,
			lower(trim(dps.execution_location)) AS execution_location,
			dps.included,
			dps.dependencies
		FROM StepRun sr
		JOIN DeploymentPlanStep dps
			ON dps.id = sr.deployment_plan_step_id
			AND dps.deployment_plan_id = sr.deployment_plan_id
			AND dps.organization_id = sr.organization_id
		WHERE sr.task_id = @taskId
			AND sr.organization_id = @organizationId
		ORDER BY sr.sort_order, sr.step_key`,
		pgx.NamedArgs{
			"taskId":         taskID,
			"organizationId": orgID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query TaskLease step candidates: %w", err)
	}
	candidates, err := pgx.CollectRows(rows, pgx.RowToStructByName[taskLeaseStepCandidate])
	if err != nil {
		return nil, fmt.Errorf("could not collect TaskLease step candidates: %w", err)
	}
	return candidates, nil
}

func readyTaskLeaseStepCandidates(
	candidates []taskLeaseStepCandidate,
	executionLocation string,
) []taskLeaseStepCandidate {
	satisfied := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if taskLeaseDependencyStatusSatisfied(candidate.Status) {
			satisfied[candidate.StepKey] = true
		}
	}
	ready := make([]taskLeaseStepCandidate, 0, len(candidates))
	selected := make(map[string]bool, len(candidates))
	for {
		progressed := false
		for _, candidate := range candidates {
			if selected[candidate.StepKey] ||
				!candidate.Included ||
				candidate.ExecutionLocation != executionLocation ||
				candidate.Status != types.StepRunStatusPending ||
				!taskLeaseDependenciesSatisfied(candidate.Dependencies, satisfied) {
				continue
			}
			ready = append(ready, candidate)
			selected[candidate.StepKey] = true
			satisfied[candidate.StepKey] = true
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return ready
}

func taskLeaseDependencyStatusSatisfied(status types.StepRunStatus) bool {
	return status == types.StepRunStatusSucceeded || status == types.StepRunStatusSkipped
}

func taskLeaseDependenciesSatisfied(dependencies []string, satisfied map[string]bool) bool {
	for _, dependency := range dependencies {
		if !satisfied[dependency] {
			return false
		}
	}
	return true
}

func taskLeaseStepIdempotencyKey(task types.Task, step types.TaskLeaseStep) string {
	data := []byte(
		task.ReleaseBundleID.String() + "\x00" +
			task.DeploymentPlanID.String() + "\x00" +
			task.DeploymentTargetID.String() + "\x00" +
			task.ID.String() + "\x00" +
			step.StepKey,
	)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newTaskLeaseToken() (string, string, error) {
	data := make([]byte, taskLeaseTokenBytes)
	if _, err := rand.Read(data); err != nil {
		return "", "", fmt.Errorf("could not generate task lease token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(data)
	return token, hashTaskLeaseToken(token), nil
}

func hashTaskLeaseToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}
