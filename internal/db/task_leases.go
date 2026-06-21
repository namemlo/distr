package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
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
	tl.lease_token_hash,
	tl.leased_at,
	tl.expires_at,
	tl.heartbeat_at,
	tl.attempt,
	tl.released_at,
	dp.canonical_checksum AS plan_checksum
`

type taskLeaseCandidate struct {
	TaskID uuid.UUID        `db:"id"`
	Status types.TaskStatus `db:"status"`
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
		published, err := lockTaskReleaseBundlePublishedForLease(ctx, candidate.TaskID, request.OrganizationID)
		if err != nil {
			return err
		}
		if !published {
			return nil
		}
		if candidate.Status == types.TaskStatusQueued {
			err := acquireTaskResourceLocks(ctx, candidate.TaskID, request.OrganizationID)
			if errors.Is(err, apierrors.ErrConflict) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := updateTaskStatus(ctx, candidate.TaskID, request.OrganizationID, types.TaskStatusRunning); err != nil {
				return err
			}
		}
		attempt, err := releaseExpiredTaskLeasesAndNextAttempt(ctx, candidate.TaskID, request.OrganizationID)
		if err != nil {
			return err
		}
		token, tokenHash, err := newTaskLeaseToken()
		if err != nil {
			return err
		}
		leaseID, err := insertTaskLease(ctx, request, candidate.TaskID, tokenHash, attempt)
		if err != nil {
			return err
		}
		lease, err = getTaskLease(ctx, leaseID, request.OrganizationID)
		if err != nil {
			return err
		}
		lease.LeaseToken = token
		return nil
	})
	if err != nil {
		return nil, err
	}
	return lease, nil
}

func HeartbeatAgentTaskLease(
	ctx context.Context,
	request types.HeartbeatAgentTaskLeaseRequest,
) (*types.TaskLease, error) {
	if err := validateHeartbeatAgentTaskLeaseRequest(request); err != nil {
		return nil, err
	}
	var lease *types.TaskLease
	err := RunTx(ctx, func(ctx context.Context) error {
		status, err := getTaskStatusForUpdate(ctx, request.TaskID, request.OrganizationID)
		if err != nil {
			return err
		}
		if status != types.TaskStatusRunning {
			return apierrors.ErrNotFound
		}
		leaseID, expired, err := getActiveTaskLeaseIDForHeartbeat(ctx, request)
		if err != nil {
			return err
		}
		if expired {
			return apierrors.NewConflict("task lease has expired")
		}
		if err := updateTaskLeaseHeartbeat(ctx, leaseID, request.OrganizationID); err != nil {
			return err
		}
		lease, err = getTaskLease(ctx, leaseID, request.OrganizationID)
		if err != nil {
			return err
		}
		lease.LeaseToken = request.LeaseToken
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
			t.status
		FROM Task t
		LEFT JOIN TaskLease active_lease
			ON active_lease.task_id = t.id
			AND active_lease.organization_id = t.organization_id
			AND active_lease.released_at IS NULL
		WHERE t.organization_id = @organizationId
			AND t.deployment_target_id = @agentId
			AND (
			t.status = @queuedStatus
				OR (
					t.status = @runningStatus
					AND active_lease.id IS NOT NULL
					AND active_lease.expires_at <= now()
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
			)
		ORDER BY t.queue_order, t.id
		LIMIT 1
		FOR UPDATE OF t SKIP LOCKED`,
		pgx.NamedArgs{
			"organizationId":    request.OrganizationID,
			"agentId":           request.AgentID,
			"queuedStatus":      types.TaskStatusQueued,
			"runningStatus":     types.TaskStatusRunning,
			"pendingStepStatus": types.StepRunStatusPending,
			"runningStepStatus": types.StepRunStatusRunning,
			"publishedStatus":   types.ReleaseBundleStatusPublished,
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
	tag, err := db.Exec(ctx,
		`UPDATE TaskLease
		SET
			released_at = COALESCE(released_at, now()),
			updated_at = now()
		WHERE task_id = @taskId
			AND organization_id = @organizationId
			AND released_at IS NULL
			AND expires_at <= now()`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	)
	if err != nil {
		return 0, fmt.Errorf("could not release expired TaskLease: %w", err)
	}
	if tag.RowsAffected() > 0 {
		if err := resetInterruptedStepRunsForTask(ctx, taskID, orgID); err != nil {
			return 0, err
		}
	}
	return nextAttempt, nil
}

func resetInterruptedStepRunsForTask(ctx context.Context, taskID, orgID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`UPDATE StepRun
		SET
			status = @pendingStatus,
			updated_at = now()
		WHERE task_id = @taskId
			AND organization_id = @organizationId
			AND status = @runningStatus
			AND EXISTS (
				SELECT 1
				FROM DeploymentPlanStep dps
				WHERE dps.id = StepRun.deployment_plan_step_id
					AND dps.deployment_plan_id = StepRun.deployment_plan_id
					AND dps.organization_id = StepRun.organization_id
					AND dps.included
					AND lower(trim(dps.execution_location)) = 'target'
			)`,
		pgx.NamedArgs{
			"taskId":         taskID,
			"organizationId": orgID,
			"pendingStatus":  types.StepRunStatusPending,
			"runningStatus":  types.StepRunStatusRunning,
		},
	)
	if err != nil {
		return mapTaskWriteError("reset interrupted step runs", err)
	}
	return nil
}

func insertTaskLease(
	ctx context.Context,
	request types.LeaseAgentTaskRequest,
	taskID uuid.UUID,
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
			lease_token_hash,
			expires_at,
			attempt
		)
		VALUES (
			@organizationId,
			@taskId,
			@agentId,
			@leaseTokenHash,
			now() + @leaseDuration::interval,
			@attempt
		)
		RETURNING id`,
		pgx.NamedArgs{
			"organizationId": request.OrganizationID,
			"taskId":         taskID,
			"agentId":        request.AgentID,
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
	request types.HeartbeatAgentTaskLeaseRequest,
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
			AND tl.lease_token_hash = @leaseTokenHash
			AND tl.released_at IS NULL
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationId": request.OrganizationID,
			"agentId":        request.AgentID,
			"taskId":         request.TaskID,
			"leaseTokenHash": hashTaskLeaseToken(request.LeaseToken),
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
	steps, err := getTaskLeaseSteps(ctx, lease.Task)
	if err != nil {
		return nil, err
	}
	lease.Steps = steps
	return &lease, nil
}

func getTaskLeaseSteps(ctx context.Context, task types.Task) ([]types.TaskLeaseStep, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			sr.id AS step_run_id,
			sr.step_key,
			sr.name,
			sr.action_type,
			dps.input_bindings,
			sr.sort_order
		FROM StepRun sr
		JOIN DeploymentPlanStep dps
			ON dps.id = sr.deployment_plan_step_id
			AND dps.deployment_plan_id = sr.deployment_plan_id
			AND dps.organization_id = sr.organization_id
		WHERE sr.task_id = @taskId
			AND sr.organization_id = @organizationId
			AND sr.status = @pendingStatus
			AND dps.included
			AND lower(trim(dps.execution_location)) = 'target'
		ORDER BY sr.sort_order, sr.step_key`,
		pgx.NamedArgs{
			"taskId":         task.ID,
			"organizationId": task.OrganizationID,
			"pendingStatus":  types.StepRunStatusPending,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query TaskLease steps: %w", err)
	}
	steps, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.TaskLeaseStep])
	if err != nil {
		return nil, fmt.Errorf("could not collect TaskLease steps: %w", err)
	}
	for i := range steps {
		if steps[i].InputBindings == nil {
			steps[i].InputBindings = map[string]any{}
		}
		steps[i].ActionVersion = types.AgentActionVersionV1
		steps[i].SecretReferences = []string{}
		steps[i].IdempotencyKey = taskLeaseStepIdempotencyKey(task, steps[i])
	}
	return steps, nil
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
