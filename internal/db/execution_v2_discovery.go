package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const executionV2LeaseCandidateQuery = executionAttemptSelect + `
	JOIN ExecutionIntent ei
		ON ei.execution_attempt_id = ea.id
		AND ei.organization_id = ea.organization_id
	WHERE ea.organization_id = @organizationId
		AND ea.deployment_target_id = @deploymentTargetId
		AND ea.status = 'PENDING'
		AND ea.adapter_revision = @adapterRevision
		AND ei.key_id = @keyId
		AND ea.intent_issued_at <= clock_timestamp()
		AND ea.intent_expires_at > clock_timestamp()
		AND ef.released_at IS NULL
	ORDER BY ea.created_at, ea.id
	LIMIT 1
	FOR UPDATE OF ea, ef SKIP LOCKED`

const executionV2ExpiredPendingCandidateQuery = `
	SELECT ea.id, ea.task_id
	FROM ExecutionAttempt ea
	JOIN ExecutionFence ef
		ON ef.execution_attempt_id = ea.id
		AND ef.organization_id = ea.organization_id
	WHERE ea.organization_id = @organizationId
		AND ea.deployment_target_id = @deploymentTargetId
		AND ea.status = 'PENDING'
		AND ea.intent_expires_at <= clock_timestamp()
		AND ef.lease_expires_at IS NULL
		AND ef.released_at IS NULL
	ORDER BY ea.intent_expires_at, ea.created_at, ea.id
	LIMIT 1
	FOR UPDATE OF ea, ef SKIP LOCKED`

func LeaseExecutionV2Attempt(
	ctx context.Context,
	request types.LeaseExecutionV2Request,
) (*types.ExecutionV2Lease, error) {
	if err := validateLeaseExecutionV2Request(request); err != nil {
		return nil, err
	}
	var result *types.ExecutionV2Lease
	err := RunTx(ctx, func(ctx context.Context) error {
		database := internalctx.GetDb(ctx)
		if err := reapExpiredPendingExecutionV2Attempts(
			ctx, request.OrganizationID, request.DeploymentTargetID,
		); err != nil {
			return err
		}
		attempt, err := scanExecutionAttempt(database.QueryRow(
			ctx,
			executionV2LeaseCandidateQuery,
			pgx.NamedArgs{
				"organizationId":     request.OrganizationID,
				"deploymentTargetId": request.DeploymentTargetID,
				"adapterRevision":    request.AdapterRevision,
				"keyId":              request.KeyID,
			},
		))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get pending ExecutionAttempt lease candidate: %w", err)
		}

		command, err := database.Exec(ctx, `
			UPDATE ExecutionAttempt
			SET status = 'CLAIMED', claimed_by = @executorId,
				updated_at = clock_timestamp()
			WHERE id = @attemptId
				AND organization_id = @organizationId
				AND deployment_target_id = @deploymentTargetId
				AND status = 'PENDING'
				AND adapter_revision = @adapterRevision
				AND intent_issued_at <= clock_timestamp()
				AND intent_expires_at > clock_timestamp()`,
			pgx.NamedArgs{
				"attemptId": attempt.ID, "organizationId": request.OrganizationID,
				"deploymentTargetId": request.DeploymentTargetID,
				"adapterRevision":    request.AdapterRevision, "executorId": request.ExecutorID,
			},
		)
		if err != nil {
			return fmt.Errorf("claim discovered ExecutionAttempt: %w", err)
		}
		if command.RowsAffected() != 1 {
			return apierrors.NewConflict("execution attempt lease raced")
		}

		command, err = database.Exec(ctx, `
			UPDATE ExecutionFence
			SET lease_expires_at = LEAST(
					clock_timestamp() + @leaseDuration,
					@intentExpiresAt
				),
				released_at = NULL
			WHERE execution_attempt_id = @attemptId
				AND organization_id = @organizationId
				AND generation = @generation
				AND released_at IS NULL`,
			pgx.NamedArgs{
				"attemptId": attempt.ID, "organizationId": request.OrganizationID,
				"generation": attempt.Fence.Generation, "leaseDuration": request.LeaseDuration,
				"intentExpiresAt": attempt.IntentExpiresAt,
			},
		)
		if err != nil {
			return fmt.Errorf("lease discovered ExecutionFence: %w", err)
		}
		if command.RowsAffected() != 1 {
			return apierrors.NewConflict("execution fence is not leaseable")
		}

		claimed, err := getExecutionAttemptForTargetUpdate(
			ctx, attempt.ID, request.OrganizationID, request.DeploymentTargetID,
		)
		if err != nil {
			return err
		}
		intent, err := GetExecutionIntent(ctx, attempt.ID, request.OrganizationID)
		if err != nil {
			return err
		}
		if intent.KeyID != request.KeyID || claimed.AdapterRevision != request.AdapterRevision {
			return apierrors.NewConflict("execution attempt frozen identity changed")
		}
		result = &types.ExecutionV2Lease{Attempt: *claimed, Intent: *intent}
		return nil
	})
	return result, err
}

func reapExpiredPendingExecutionV2Attempts(
	ctx context.Context,
	organizationID, deploymentTargetID uuid.UUID,
) error {
	database := internalctx.GetDb(ctx)
	for {
		var attemptID, taskID uuid.UUID
		err := database.QueryRow(
			ctx,
			executionV2ExpiredPendingCandidateQuery,
			pgx.NamedArgs{
				"organizationId":     organizationID,
				"deploymentTargetId": deploymentTargetID,
			},
		).Scan(&attemptID, &taskID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get expired pending ExecutionAttempt: %w", err)
		}
		if err := fenceExecutionAttemptTx(
			ctx, organizationID, deploymentTargetID, attemptID,
			"execution intent expired before lease discovery",
		); err != nil {
			return fmt.Errorf("fence expired pending ExecutionAttempt: %w", err)
		}
		if err := releaseTaskResourceLocks(ctx, taskID, organizationID); err != nil {
			return fmt.Errorf("release expired execution task resource locks: %w", err)
		}
	}
}

func validateLeaseExecutionV2Request(request types.LeaseExecutionV2Request) error {
	if request.OrganizationID == uuid.Nil || request.DeploymentTargetID == uuid.Nil {
		return apierrors.NewBadRequest("execution lease credential scope is invalid")
	}
	if strings.TrimSpace(request.ExecutorID) == "" || len(request.ExecutorID) > 128 ||
		strings.ContainsAny(request.ExecutorID, "\r\n") {
		return apierrors.NewBadRequest("execution lease executor identity is invalid")
	}
	if strings.TrimSpace(request.AdapterRevision) == "" || len(request.AdapterRevision) > 256 ||
		strings.ContainsAny(request.AdapterRevision, "\r\n") {
		return apierrors.NewBadRequest("execution lease adapter revision is invalid")
	}
	if !intentChecksumPatternDB.MatchString(request.KeyID) {
		return apierrors.NewBadRequest("execution lease key identity is invalid")
	}
	if request.Now.IsZero() || request.LeaseDuration < 15*time.Second ||
		request.LeaseDuration > 5*time.Minute {
		return apierrors.NewBadRequest("execution lease duration is invalid")
	}
	return nil
}
