package db

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var intentChecksumPatternDB = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

const executionAttemptSelect = `
	SELECT
		ea.id, ea.created_at, ea.updated_at, ea.organization_id,
		ea.task_id, ea.step_run_id, ea.execution_id, ea.attempt_number,
		ea.step_key, ea.status, ea.claimed_by, ea.plan_checksum,
		ea.artifact_digest, ea.config_checksum, ea.adapter_revision,
		ea.intent_issued_at, ea.intent_expires_at, ea.last_event_sequence,
		ea.acknowledged_at, ea.completed_at, ea.cancellable, ea.retry_safe,
		ea.failure_reason, ef.resource_key, ef.generation,
		ef.lease_expires_at
	FROM ExecutionAttempt ea
	JOIN ExecutionFence ef
		ON ef.execution_attempt_id = ea.id
		AND ef.organization_id = ea.organization_id
`

type rowScanner interface {
	Scan(...any) error
}

func CreateExecutionAttempt(
	ctx context.Context,
	attempt types.ExecutionAttempt,
	intent types.SignedExecutionIntent,
) (*types.ExecutionAttempt, error) {
	if err := validateNewExecutionAttempt(attempt, intent); err != nil {
		return nil, err
	}
	var result *types.ExecutionAttempt
	err := RunTx(ctx, func(ctx context.Context) error {
		db := internalctx.GetDb(ctx)
		_, err := db.Exec(ctx, `
			INSERT INTO ExecutionAttempt (
				id, organization_id, task_id, step_run_id, execution_id,
				attempt_number, step_key, status, plan_checksum, artifact_digest,
				config_checksum, adapter_revision, intent_issued_at,
				intent_expires_at, cancellable, retry_safe
			) VALUES (
				@id, @organizationId, @taskId, @stepRunId, @executionId,
				@attemptNumber, @stepKey, 'PENDING', @planChecksum, @artifactDigest,
				@configChecksum, @adapterRevision, @intentIssuedAt,
				@intentExpiresAt, @cancellable, @retrySafe
			)`,
			pgx.NamedArgs{
				"id": attempt.ID, "organizationId": attempt.OrganizationID,
				"taskId": attempt.TaskID, "stepRunId": attempt.StepRunID,
				"executionId":   attempt.Identity.ExecutionID,
				"attemptNumber": attempt.Identity.AttemptNumber, "stepKey": attempt.Identity.StepKey,
				"planChecksum": attempt.PlanChecksum, "artifactDigest": attempt.ArtifactDigest,
				"configChecksum": attempt.ConfigChecksum, "adapterRevision": attempt.AdapterRevision,
				"intentIssuedAt":  attempt.IntentIssuedAt.UTC(),
				"intentExpiresAt": attempt.IntentExpiresAt.UTC(),
				"cancellable":     attempt.Cancellable, "retrySafe": attempt.RetrySafe,
			},
		)
		if err != nil {
			return fmt.Errorf("insert ExecutionAttempt: %w", err)
		}
		_, err = db.Exec(ctx, `
			INSERT INTO ExecutionFence (
				execution_attempt_id, organization_id, resource_key, generation
			) VALUES (
				@attemptId, @organizationId, @resourceKey, @generation
			)`,
			pgx.NamedArgs{
				"attemptId": attempt.ID, "organizationId": attempt.OrganizationID,
				"resourceKey": attempt.Fence.ResourceKey, "generation": attempt.Fence.Generation,
			},
		)
		if err != nil {
			return fmt.Errorf("insert ExecutionFence: %w", err)
		}
		_, err = db.Exec(ctx, `
			INSERT INTO ExecutionIntent (
				organization_id, execution_attempt_id, payload,
				checksum, key_id, signature
			) VALUES (
				@organizationId, @attemptId, @payload,
				@checksum, @keyId, @signature
			)`,
			pgx.NamedArgs{
				"organizationId": attempt.OrganizationID, "attemptId": attempt.ID,
				"payload": intent.Payload, "checksum": intent.Checksum,
				"keyId": intent.KeyID, "signature": intent.Signature,
			},
		)
		if err != nil {
			return fmt.Errorf("insert ExecutionIntent: %w", err)
		}
		result, err = getExecutionAttemptForUpdate(ctx, attempt.ID, attempt.OrganizationID)
		return err
	})
	return result, err
}

func scanExecutionAttempt(row rowScanner) (*types.ExecutionAttempt, error) {
	var attempt types.ExecutionAttempt
	var executionID uuid.UUID
	var attemptNumber int
	var stepKey, resourceKey string
	var generation int64
	var leaseExpiresAt *time.Time
	err := row.Scan(
		&attempt.ID, &attempt.CreatedAt, &attempt.UpdatedAt, &attempt.OrganizationID,
		&attempt.TaskID, &attempt.StepRunID, &executionID, &attemptNumber,
		&stepKey, &attempt.Status, &attempt.ClaimedBy, &attempt.PlanChecksum,
		&attempt.ArtifactDigest, &attempt.ConfigChecksum, &attempt.AdapterRevision,
		&attempt.IntentIssuedAt, &attempt.IntentExpiresAt, &attempt.LastEventSequence,
		&attempt.AcknowledgedAt, &attempt.CompletedAt, &attempt.Cancellable, &attempt.RetrySafe,
		&attempt.FailureReason, &resourceKey, &generation, &leaseExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	attempt.Identity = types.ExecutionIdentity{
		ExecutionID: executionID, AttemptNumber: attemptNumber, StepKey: stepKey,
	}
	attempt.Fence = types.ExecutionFence{ResourceKey: resourceKey, Generation: generation}
	if leaseExpiresAt != nil {
		attempt.Fence.LeaseExpiresAt = leaseExpiresAt.UTC()
	}
	return &attempt, nil
}

func ClaimExecutionAttempt(
	ctx context.Context,
	request types.ClaimRequest,
) (*types.ExecutionAttempt, error) {
	if err := validateExecutionV2ClaimRequest(request); err != nil {
		return nil, err
	}
	var result *types.ExecutionAttempt
	err := RunTx(ctx, func(ctx context.Context) error {
		current, err := getExecutionAttemptForUpdate(ctx, request.AttemptID, request.OrganizationID)
		if err != nil {
			return err
		}
		if current.Fence.Generation != request.ExpectedGeneration {
			return apierrors.NewConflict("stale execution fence generation")
		}
		if current.Status == types.ExecutionAttemptStatusClaimed && current.ClaimedBy == request.ExecutorID {
			result = current
			return nil
		}
		if current.Status != types.ExecutionAttemptStatusPending {
			return apierrors.NewConflict("execution attempt is not claimable")
		}
		db := internalctx.GetDb(ctx)
		command, err := db.Exec(ctx, `
			UPDATE ExecutionAttempt
			SET status = 'CLAIMED', claimed_by = @executorId,
				updated_at = @now, acknowledged_at = COALESCE(acknowledged_at, @now)
			WHERE id = @attemptId AND organization_id = @organizationId
				AND status = 'PENDING'`,
			pgx.NamedArgs{
				"attemptId": request.AttemptID, "organizationId": request.OrganizationID,
				"executorId": request.ExecutorID, "now": request.Now.UTC(),
			},
		)
		if err != nil {
			return fmt.Errorf("claim ExecutionAttempt: %w", err)
		}
		if command.RowsAffected() != 1 {
			return apierrors.NewConflict("execution attempt claim raced")
		}
		_, err = db.Exec(ctx, `
			UPDATE ExecutionFence
			SET lease_expires_at = @leaseExpiresAt, released_at = NULL
			WHERE execution_attempt_id = @attemptId
				AND organization_id = @organizationId
				AND generation = @generation`,
			pgx.NamedArgs{
				"attemptId": request.AttemptID, "organizationId": request.OrganizationID,
				"generation":     request.ExpectedGeneration,
				"leaseExpiresAt": request.Now.UTC().Add(request.LeaseDuration),
			},
		)
		if err != nil {
			return fmt.Errorf("claim ExecutionFence: %w", err)
		}
		result, err = getExecutionAttemptForUpdate(ctx, request.AttemptID, request.OrganizationID)
		return err
	})
	return result, err
}

func GetExecutionIntent(
	ctx context.Context,
	attemptID, orgID uuid.UUID,
) (*types.SignedExecutionIntent, error) {
	db := internalctx.GetDb(ctx)
	var intent types.SignedExecutionIntent
	err := db.QueryRow(ctx, `
		SELECT payload, checksum, key_id, signature
		FROM ExecutionIntent
		WHERE execution_attempt_id = @attemptId
			AND organization_id = @organizationId`,
		pgx.NamedArgs{"attemptId": attemptID, "organizationId": orgID},
	).Scan(&intent.Payload, &intent.Checksum, &intent.KeyID, &intent.Signature)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get ExecutionIntent: %w", err)
	}
	return &intent, nil
}

func HeartbeatExecutionAttempt(ctx context.Context, request types.HeartbeatRequest) error {
	if request.OrganizationID == uuid.Nil || request.AttemptID == uuid.Nil ||
		strings.TrimSpace(request.ExecutorID) == "" || request.FenceGeneration <= 0 ||
		request.Now.IsZero() || request.LeaseDuration <= 0 {
		return apierrors.NewBadRequest("execution heartbeat request is invalid")
	}
	db := internalctx.GetDb(ctx)
	command, err := db.Exec(ctx, `
		UPDATE ExecutionFence ef
		SET lease_expires_at = @leaseExpiresAt
		FROM ExecutionAttempt ea
		WHERE ef.execution_attempt_id = @attemptId
			AND ef.organization_id = @organizationId
			AND ef.generation = @generation
			AND ef.lease_expires_at > @now
			AND ef.released_at IS NULL
			AND ea.id = ef.execution_attempt_id
			AND ea.organization_id = ef.organization_id
			AND ea.claimed_by = @executorId
			AND ea.status IN ('CLAIMED', 'RUNNING')`,
		pgx.NamedArgs{
			"attemptId": request.AttemptID, "organizationId": request.OrganizationID,
			"generation": request.FenceGeneration, "executorId": request.ExecutorID,
			"now":            request.Now.UTC(),
			"leaseExpiresAt": request.Now.UTC().Add(request.LeaseDuration),
		},
	)
	if err != nil {
		return fmt.Errorf("heartbeat ExecutionAttempt: %w", err)
	}
	if command.RowsAffected() != 1 {
		return apierrors.NewConflict("execution heartbeat rejected by lease or fence")
	}
	return nil
}

func RecordExecutionEvent(
	ctx context.Context,
	input types.ExecutionEventInput,
) (*types.ExecutionEvent, error) {
	if err := validateExecutionV2EventInput(input); err != nil {
		return nil, err
	}
	var result *types.ExecutionEvent
	err := RunTx(ctx, func(ctx context.Context) error {
		attempt, err := getExecutionAttemptForUpdate(ctx, input.AttemptID, input.OrganizationID)
		if err != nil {
			return err
		}
		if attempt.Identity != input.Identity {
			return apierrors.NewConflict("execution event identity mismatch")
		}
		if attempt.Fence.Generation != input.FenceGeneration {
			return apierrors.NewConflict("stale execution fence generation")
		}
		if err := executionprotocol.ValidateCallbackWindow(*attempt, input.OccurredAt); err != nil {
			return apierrors.NewConflict(err.Error())
		}
		existing, err := getExecutionEvent(ctx, input.AttemptID, input.EventSequence)
		if err == nil {
			if existing.PayloadChecksum != input.PayloadChecksum || existing.Status != input.Status {
				return apierrors.NewConflict("conflicting duplicate execution event")
			}
			result = existing
			return nil
		}
		if !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if input.EventSequence != attempt.LastEventSequence+1 {
			return apierrors.NewConflict("execution events must be ordered")
		}
		db := internalctx.GetDb(ctx)
		row := db.QueryRow(ctx, `
			INSERT INTO ExecutionEvent (
				organization_id, execution_attempt_id, execution_id,
				attempt_number, step_key, fence_generation, event_sequence,
				status, payload_checksum, message, occurred_at
			) VALUES (
				@organizationId, @attemptId, @executionId,
				@attemptNumber, @stepKey, @fenceGeneration, @eventSequence,
				@status, @payloadChecksum, @message, @occurredAt
			)
			RETURNING id, created_at, organization_id, execution_attempt_id,
				execution_id, attempt_number, step_key, fence_generation,
				event_sequence, status, payload_checksum, message, occurred_at`,
			pgx.NamedArgs{
				"organizationId": input.OrganizationID, "attemptId": input.AttemptID,
				"executionId": input.Identity.ExecutionID, "attemptNumber": input.Identity.AttemptNumber,
				"stepKey": input.Identity.StepKey, "fenceGeneration": input.FenceGeneration,
				"eventSequence": input.EventSequence, "status": input.Status,
				"payloadChecksum": input.PayloadChecksum, "message": input.Message,
				"occurredAt": input.OccurredAt.UTC(),
			},
		)
		result, err = scanExecutionEvent(row)
		if err != nil {
			return fmt.Errorf("insert ExecutionEvent: %w", err)
		}
		status := attempt.Status
		if input.Status == types.ExecutionEventStatusRunning {
			status = types.ExecutionAttemptStatusRunning
		}
		_, err = db.Exec(ctx, `
			UPDATE ExecutionAttempt
			SET last_event_sequence = @sequence, status = @status, updated_at = now()
			WHERE id = @attemptId AND organization_id = @organizationId`,
			pgx.NamedArgs{
				"sequence": input.EventSequence, "status": status,
				"attemptId": input.AttemptID, "organizationId": input.OrganizationID,
			},
		)
		return err
	})
	return result, err
}

func CompleteExecutionAttempt(ctx context.Context, input types.CompletionInput) error {
	if input.OrganizationID == uuid.Nil || input.AttemptID == uuid.Nil ||
		strings.TrimSpace(input.ExecutorID) == "" || input.FenceGeneration <= 0 ||
		input.CompletedAt.IsZero() || !input.Status.IsTerminal() ||
		input.Status == types.ExecutionAttemptStatusFenced {
		return apierrors.NewBadRequest("execution completion request is invalid")
	}
	return RunTx(ctx, func(ctx context.Context) error {
		db := internalctx.GetDb(ctx)
		command, err := db.Exec(ctx, `
			UPDATE ExecutionAttempt ea
			SET status = @status, completed_at = @completedAt,
				updated_at = @completedAt, failure_reason = @failureReason,
				claimed_by = ''
			FROM ExecutionFence ef
			WHERE ea.id = @attemptId AND ea.organization_id = @organizationId
				AND ea.claimed_by = @executorId
				AND ea.status IN ('CLAIMED', 'RUNNING')
				AND ef.execution_attempt_id = ea.id
				AND ef.organization_id = ea.organization_id
				AND ef.generation = @generation
				AND ef.released_at IS NULL`,
			pgx.NamedArgs{
				"attemptId": input.AttemptID, "organizationId": input.OrganizationID,
				"executorId": input.ExecutorID, "generation": input.FenceGeneration,
				"status": input.Status, "completedAt": input.CompletedAt.UTC(),
				"failureReason": strings.TrimSpace(input.FailureReason),
			},
		)
		if err != nil {
			return fmt.Errorf("complete ExecutionAttempt: %w", err)
		}
		if command.RowsAffected() != 1 {
			return apierrors.NewConflict("execution completion rejected by lease or fence")
		}
		_, err = db.Exec(ctx, `
			UPDATE ExecutionFence
			SET lease_expires_at = NULL, released_at = @completedAt
			WHERE execution_attempt_id = @attemptId
				AND organization_id = @organizationId
				AND generation = @generation`,
			pgx.NamedArgs{
				"attemptId": input.AttemptID, "organizationId": input.OrganizationID,
				"generation": input.FenceGeneration, "completedAt": input.CompletedAt.UTC(),
			},
		)
		return err
	})
}

func FenceExecutionAttempt(ctx context.Context, attemptID uuid.UUID, reason string) error {
	if attemptID == uuid.Nil || strings.TrimSpace(reason) == "" {
		return apierrors.NewBadRequest("attemptId and reason are required")
	}
	return RunTx(ctx, func(ctx context.Context) error {
		db := internalctx.GetDb(ctx)
		command, err := db.Exec(ctx, `
			UPDATE ExecutionAttempt
			SET status = 'FENCED', claimed_by = '', completed_at = now(),
				updated_at = now(), failure_reason = @reason
			WHERE id = @attemptId
				AND status IN ('PENDING', 'CLAIMED', 'RUNNING')`,
			pgx.NamedArgs{"attemptId": attemptID, "reason": strings.TrimSpace(reason)},
		)
		if err != nil {
			return fmt.Errorf("fence ExecutionAttempt: %w", err)
		}
		if command.RowsAffected() != 1 {
			return apierrors.NewConflict("execution attempt cannot be fenced")
		}
		_, err = db.Exec(ctx, `
			UPDATE ExecutionFence
			SET generation = generation + 1, lease_expires_at = NULL, released_at = now()
			WHERE execution_attempt_id = @attemptId`,
			pgx.NamedArgs{"attemptId": attemptID},
		)
		return err
	})
}

func validateExecutionV2ClaimRequest(request types.ClaimRequest) error {
	if request.OrganizationID == uuid.Nil || request.AttemptID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId and attemptId are required")
	}
	if strings.TrimSpace(request.ExecutorID) == "" {
		return apierrors.NewBadRequest("executorId is required")
	}
	if request.ExpectedGeneration <= 0 {
		return apierrors.NewBadRequest("expected generation must be greater than 0")
	}
	if request.Now.IsZero() || request.LeaseDuration < 15*time.Second ||
		request.LeaseDuration > 5*time.Minute {
		return apierrors.NewBadRequest("execution lease duration is invalid")
	}
	return nil
}

func validateNewExecutionAttempt(
	attempt types.ExecutionAttempt,
	intent types.SignedExecutionIntent,
) error {
	if attempt.ID == uuid.Nil || attempt.OrganizationID == uuid.Nil ||
		attempt.TaskID == uuid.Nil || attempt.StepRunID == uuid.Nil ||
		attempt.Identity.ExecutionID == uuid.Nil || attempt.Identity.AttemptNumber <= 0 ||
		strings.TrimSpace(attempt.Identity.StepKey) == "" {
		return apierrors.NewBadRequest("execution attempt identity is invalid")
	}
	if attempt.Status != types.ExecutionAttemptStatusPending {
		return apierrors.NewBadRequest("new execution attempt status must be PENDING")
	}
	if !intentChecksumPatternDB.MatchString(attempt.PlanChecksum) ||
		!intentChecksumPatternDB.MatchString(attempt.ArtifactDigest) ||
		!intentChecksumPatternDB.MatchString(attempt.ConfigChecksum) {
		return apierrors.NewBadRequest("execution attempt frozen checksums are invalid")
	}
	if strings.TrimSpace(attempt.AdapterRevision) == "" ||
		attempt.IntentIssuedAt.IsZero() || !attempt.IntentExpiresAt.After(attempt.IntentIssuedAt) ||
		strings.TrimSpace(attempt.Fence.ResourceKey) == "" || attempt.Fence.Generation <= 0 {
		return apierrors.NewBadRequest("execution attempt frozen inputs are invalid")
	}
	sum := sha256.Sum256(intent.Payload)
	if intent.Checksum != "sha256:"+hex.EncodeToString(sum[:]) {
		return apierrors.NewBadRequest("execution intent checksum mismatch")
	}
	if !intentChecksumPatternDB.MatchString(intent.KeyID) {
		return apierrors.NewBadRequest("execution intent keyId is invalid")
	}
	signature, err := base64.RawStdEncoding.DecodeString(intent.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return apierrors.NewBadRequest("execution intent signature is invalid")
	}
	return nil
}

func validateExecutionV2EventInput(input types.ExecutionEventInput) error {
	if input.OrganizationID == uuid.Nil || input.AttemptID == uuid.Nil ||
		input.Identity.ExecutionID == uuid.Nil || input.Identity.AttemptNumber <= 0 ||
		strings.TrimSpace(input.Identity.StepKey) == "" || input.FenceGeneration <= 0 ||
		input.EventSequence <= 0 || !input.Status.IsValid() || input.OccurredAt.IsZero() {
		return apierrors.NewBadRequest("execution event input is invalid")
	}
	if !intentChecksumPatternDB.MatchString(input.PayloadChecksum) {
		return apierrors.NewBadRequest("payload checksum is invalid")
	}
	if len(input.Message) > 2048 || strings.ContainsAny(input.Message, "\r\n") {
		return apierrors.NewBadRequest("execution event message is invalid")
	}
	return nil
}

func getExecutionAttemptForUpdate(
	ctx context.Context,
	attemptID, orgID uuid.UUID,
) (*types.ExecutionAttempt, error) {
	db := internalctx.GetDb(ctx)
	attempt, err := scanExecutionAttempt(db.QueryRow(ctx,
		executionAttemptSelect+`
			WHERE ea.id = @attemptId AND ea.organization_id = @organizationId
			FOR UPDATE OF ea, ef`,
		pgx.NamedArgs{"attemptId": attemptID, "organizationId": orgID},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get ExecutionAttempt: %w", err)
	}
	return attempt, nil
}

func getExecutionEvent(
	ctx context.Context,
	attemptID uuid.UUID,
	sequence int64,
) (*types.ExecutionEvent, error) {
	db := internalctx.GetDb(ctx)
	event, err := scanExecutionEvent(db.QueryRow(ctx, `
		SELECT id, created_at, organization_id, execution_attempt_id,
			execution_id, attempt_number, step_key, fence_generation,
			event_sequence, status, payload_checksum, message, occurred_at
		FROM ExecutionEvent
		WHERE execution_attempt_id = @attemptId AND event_sequence = @sequence`,
		pgx.NamedArgs{"attemptId": attemptID, "sequence": sequence},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get ExecutionEvent: %w", err)
	}
	return event, nil
}

func scanExecutionEvent(row rowScanner) (*types.ExecutionEvent, error) {
	var event types.ExecutionEvent
	var executionID uuid.UUID
	var attemptNumber int
	var stepKey string
	err := row.Scan(
		&event.ID, &event.CreatedAt, &event.OrganizationID, &event.AttemptID,
		&executionID, &attemptNumber, &stepKey, &event.FenceGeneration,
		&event.EventSequence, &event.Status, &event.PayloadChecksum, &event.Message,
		&event.OccurredAt,
	)
	if err != nil {
		return nil, err
	}
	event.Identity = types.ExecutionIdentity{
		ExecutionID: executionID, AttemptNumber: attemptNumber, StepKey: stepKey,
	}
	return &event, nil
}

func RequestExecutionCancel(ctx context.Context, request types.CancelRequest) error {
	if err := validateCancelRequest(request); err != nil {
		return err
	}
	return RunTx(ctx, func(ctx context.Context) error {
		attempt, err := getLatestExecutionAttemptByExecutionIDForUpdate(
			ctx, request.ExecutionID, request.OrganizationID,
		)
		if err != nil {
			return err
		}
		if err := executionprotocol.EvaluateCancelRequest(*attempt, request); err != nil {
			return apierrors.NewConflict(err.Error())
		}
		db := internalctx.GetDb(ctx)
		command, err := db.Exec(ctx, `
			INSERT INTO ExecutionCancelRequest (
				organization_id, execution_id, execution_attempt_id,
				requested_by, idempotency_key, reason, created_at
			) VALUES (
				@organizationId, @executionId, @attemptId,
				@requestedBy, @idempotencyKey, @reason, @requestedAt
			)
			ON CONFLICT (
				organization_id, execution_id, idempotency_key
			) DO NOTHING`,
			pgx.NamedArgs{
				"organizationId": request.OrganizationID, "executionId": request.ExecutionID,
				"attemptId": attempt.ID, "requestedBy": request.RequestedBy,
				"idempotencyKey": request.IdempotencyKey, "reason": request.Reason,
				"requestedAt": request.RequestedAt.UTC(),
			},
		)
		if err != nil {
			return fmt.Errorf("insert ExecutionCancelRequest: %w", err)
		}
		if command.RowsAffected() == 1 {
			return nil
		}
		existing, err := getExecutionCancelRequestByIdempotency(
			ctx, request.OrganizationID, request.ExecutionID, request.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if !executionprotocol.IsExactDuplicateCancel(*existing, request) {
			return apierrors.NewConflict("conflicting duplicate execution cancel request")
		}
		return nil
	})
}

func RecordCancelAcknowledgement(
	ctx context.Context,
	ack types.CancelAcknowledgement,
) error {
	if ack.OrganizationID == uuid.Nil || ack.CancelRequestID == uuid.Nil ||
		ack.AttemptID == uuid.Nil || strings.TrimSpace(ack.ExecutorID) == "" ||
		ack.FenceGeneration <= 0 || ack.AcknowledgedAt.IsZero() {
		return apierrors.NewBadRequest("cancel acknowledgement is invalid")
	}
	return RunTx(ctx, func(ctx context.Context) error {
		db := internalctx.GetDb(ctx)
		cancel, err := scanExecutionCancelRequest(db.QueryRow(ctx, `
			SELECT id, created_at, organization_id, execution_id,
				execution_attempt_id, requested_by, idempotency_key, reason,
				status, acknowledged_at, acknowledged_by
			FROM ExecutionCancelRequest
			WHERE id = @id AND organization_id = @organizationId
			FOR UPDATE`,
			pgx.NamedArgs{"id": ack.CancelRequestID, "organizationId": ack.OrganizationID},
		))
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get ExecutionCancelRequest: %w", err)
		}
		if cancel.ExecutionAttemptID != ack.AttemptID {
			return apierrors.NewConflict("cancel acknowledgement attempt mismatch")
		}
		attempt, err := getExecutionAttemptForUpdate(ctx, ack.AttemptID, ack.OrganizationID)
		if err != nil {
			return err
		}
		if attempt.Fence.Generation != ack.FenceGeneration ||
			attempt.ClaimedBy != ack.ExecutorID {
			return apierrors.NewConflict("cancel acknowledgement rejected by fence or executor identity")
		}
		status := types.CancelRequestStatusRejected
		if ack.Accepted {
			status = types.CancelRequestStatusAcknowledged
		}
		if cancel.Status == status && cancel.AcknowledgedBy == ack.ExecutorID {
			return nil
		}
		if cancel.Status != types.CancelRequestStatusRequested {
			return apierrors.NewConflict("cancel request was already acknowledged differently")
		}
		_, err = db.Exec(ctx, `
			UPDATE ExecutionCancelRequest
			SET status = @status, acknowledged_at = @acknowledgedAt,
				acknowledged_by = @executorId
			WHERE id = @id AND organization_id = @organizationId`,
			pgx.NamedArgs{
				"status": status, "acknowledgedAt": ack.AcknowledgedAt.UTC(),
				"executorId": ack.ExecutorID, "id": ack.CancelRequestID,
				"organizationId": ack.OrganizationID,
			},
		)
		return err
	})
}

func RequestExecutionStatus(
	ctx context.Context,
	request types.StatusRequest,
) (*types.ExecutionStatusQuery, error) {
	if err := validateStatusRequest(request); err != nil {
		return nil, err
	}
	var result *types.ExecutionStatusQuery
	err := RunTx(ctx, func(ctx context.Context) error {
		attempt, err := getLatestExecutionAttemptByExecutionIDForUpdate(
			ctx, request.ExecutionID, request.OrganizationID,
		)
		if err != nil {
			return err
		}
		db := internalctx.GetDb(ctx)
		command, err := db.Exec(ctx, `
			INSERT INTO ExecutionStatusQuery (
				organization_id, execution_id, execution_attempt_id,
				requested_by, idempotency_key, reason, created_at, expires_at
			) VALUES (
				@organizationId, @executionId, @attemptId,
				@requestedBy, @idempotencyKey, @reason, @requestedAt, @expiresAt
			)
			ON CONFLICT (
				organization_id, execution_id, idempotency_key
			) DO NOTHING`,
			pgx.NamedArgs{
				"organizationId": request.OrganizationID, "executionId": request.ExecutionID,
				"attemptId": attempt.ID, "requestedBy": request.RequestedBy,
				"idempotencyKey": request.IdempotencyKey, "reason": request.Reason,
				"requestedAt": request.RequestedAt.UTC(), "expiresAt": request.ExpiresAt.UTC(),
			},
		)
		if err != nil {
			return fmt.Errorf("insert ExecutionStatusQuery: %w", err)
		}
		result, err = getExecutionStatusQueryByIdempotency(
			ctx, request.OrganizationID, request.ExecutionID, request.IdempotencyKey, false,
		)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 &&
			(result.RequestedBy != request.RequestedBy || result.Reason != request.Reason ||
				!result.ExpiresAt.Equal(request.ExpiresAt.UTC())) {
			return apierrors.NewConflict("conflicting duplicate execution status query")
		}
		return nil
	})
	return result, err
}

func ImportReconciliationStatus(
	ctx context.Context,
	input types.ReconciliationStatusInput,
) error {
	if err := validateReconciliationStatusInput(input); err != nil {
		return err
	}
	return RunTx(ctx, func(ctx context.Context) error {
		query, err := getExecutionStatusQueryByIDForUpdate(
			ctx, input.StatusQueryID, input.OrganizationID,
		)
		if err != nil {
			return err
		}
		if query.ExecutionID != input.ExecutionID {
			return apierrors.NewConflict("reconciliation status query identity mismatch")
		}
		if !input.ObservedAt.UTC().Before(query.ExpiresAt.UTC()) {
			return apierrors.NewConflict("execution status query is expired")
		}
		if query.Status != types.StatusQueryStatusPending {
			return apierrors.NewConflict("execution status query is already resolved")
		}
		attempt, err := getExecutionAttemptForUpdate(
			ctx, query.ExecutionAttemptID, input.OrganizationID,
		)
		if err != nil {
			return err
		}
		decision, err := executionprotocol.ReconcileCallbackLoss(*attempt, input)
		if err != nil {
			return apierrors.NewConflict(err.Error())
		}
		db := internalctx.GetDb(ctx)
		_, err = db.Exec(ctx, `
			INSERT INTO ExecutionReconciliationEvent (
				organization_id, execution_id, execution_attempt_id,
				status_query_id, event_identity, outcome, evidence_checksum,
				observed_at, operation_incomplete, retry_requested,
				retry_disposition
			) VALUES (
				@organizationId, @executionId, @attemptId,
				@statusQueryId, @eventIdentity, @outcome, @evidenceChecksum,
				@observedAt, @operationIncomplete, @retryRequested,
				@retryDisposition
			)`,
			pgx.NamedArgs{
				"organizationId": input.OrganizationID, "executionId": input.ExecutionID,
				"attemptId": attempt.ID, "statusQueryId": input.StatusQueryID,
				"eventIdentity": input.EventIdentity, "outcome": input.Outcome,
				"evidenceChecksum": input.EvidenceChecksum, "observedAt": input.ObservedAt.UTC(),
				"operationIncomplete": input.OperationIncomplete,
				"retryRequested":      input.RetryRequested,
				"retryDisposition":    decision.RetryDisposition,
			},
		)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
				return apierrors.NewConflict("reconciliation event identity must be new")
			}
			return fmt.Errorf("insert ExecutionReconciliationEvent: %w", err)
		}
		_, err = db.Exec(ctx, `
			UPDATE ExecutionStatusQuery
			SET status = 'REPORTED', reported_at = @reportedAt
			WHERE id = @id AND organization_id = @organizationId`,
			pgx.NamedArgs{
				"id": query.ID, "organizationId": input.OrganizationID,
				"reportedAt": input.ObservedAt.UTC(),
			},
		)
		if err != nil {
			return fmt.Errorf("resolve ExecutionStatusQuery: %w", err)
		}
		_, err = db.Exec(ctx, `
			UPDATE ExecutionAttempt
			SET status = @status, completed_at = @completedAt,
				updated_at = @completedAt, claimed_by = ''
			WHERE id = @attemptId AND organization_id = @organizationId`,
			pgx.NamedArgs{
				"status": decision.Status, "completedAt": input.ObservedAt.UTC(),
				"attemptId": attempt.ID, "organizationId": input.OrganizationID,
			},
		)
		if err != nil {
			return fmt.Errorf("reconcile ExecutionAttempt: %w", err)
		}
		_, err = db.Exec(ctx, `
			UPDATE ExecutionFence
			SET lease_expires_at = NULL, released_at = @releasedAt
			WHERE execution_attempt_id = @attemptId
				AND organization_id = @organizationId`,
			pgx.NamedArgs{
				"attemptId": attempt.ID, "organizationId": input.OrganizationID,
				"releasedAt": input.ObservedAt.UTC(),
			},
		)
		return err
	})
}

func GetPendingExecutionCancel(
	ctx context.Context,
	attemptID, orgID uuid.UUID,
) (*types.ExecutionCancelRequest, error) {
	db := internalctx.GetDb(ctx)
	cancel, err := scanExecutionCancelRequest(db.QueryRow(ctx, `
		SELECT id, created_at, organization_id, execution_id,
			execution_attempt_id, requested_by, idempotency_key, reason,
			status, acknowledged_at, acknowledged_by
		FROM ExecutionCancelRequest
		WHERE execution_attempt_id = @attemptId
			AND organization_id = @organizationId
			AND status = 'REQUESTED'
		ORDER BY created_at, id
		LIMIT 1`,
		pgx.NamedArgs{"attemptId": attemptID, "organizationId": orgID},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get pending ExecutionCancelRequest: %w", err)
	}
	return cancel, nil
}

func GetPendingExecutionStatusQuery(
	ctx context.Context,
	attemptID, orgID uuid.UUID,
) (*types.ExecutionStatusQuery, error) {
	db := internalctx.GetDb(ctx)
	query, err := scanExecutionStatusQuery(db.QueryRow(ctx, `
		SELECT id, created_at, organization_id, execution_id,
			execution_attempt_id, requested_by, idempotency_key, reason,
			status, expires_at, reported_at
		FROM ExecutionStatusQuery
		WHERE execution_attempt_id = @attemptId
			AND organization_id = @organizationId
			AND status = 'PENDING'
			AND expires_at > now()
		ORDER BY created_at, id
		LIMIT 1`,
		pgx.NamedArgs{"attemptId": attemptID, "organizationId": orgID},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get pending ExecutionStatusQuery: %w", err)
	}
	return query, nil
}

func validateCancelRequest(request types.CancelRequest) error {
	if request.OrganizationID == uuid.Nil || request.ExecutionID == uuid.Nil ||
		request.RequestedBy == uuid.Nil || strings.TrimSpace(request.IdempotencyKey) == "" ||
		strings.TrimSpace(request.Reason) == "" || request.RequestedAt.IsZero() {
		return apierrors.NewBadRequest("cancel request idempotency and identity are required")
	}
	if len(request.IdempotencyKey) > 128 || len(request.Reason) > 2048 ||
		strings.ContainsAny(request.IdempotencyKey+request.Reason, "\r\n") {
		return apierrors.NewBadRequest("cancel request is invalid")
	}
	return nil
}

func validateStatusRequest(request types.StatusRequest) error {
	if request.OrganizationID == uuid.Nil || request.ExecutionID == uuid.Nil ||
		request.RequestedBy == uuid.Nil || strings.TrimSpace(request.IdempotencyKey) == "" ||
		strings.TrimSpace(request.Reason) == "" || request.RequestedAt.IsZero() ||
		!request.ExpiresAt.After(request.RequestedAt) {
		return apierrors.NewBadRequest("execution status request is invalid")
	}
	return nil
}

func validateReconciliationStatusInput(input types.ReconciliationStatusInput) error {
	if input.OrganizationID == uuid.Nil || input.ExecutionID == uuid.Nil ||
		input.StatusQueryID == uuid.Nil || input.EventIdentity == uuid.Nil ||
		!input.Outcome.IsValid() || !intentChecksumPatternDB.MatchString(input.EvidenceChecksum) ||
		input.ObservedAt.IsZero() {
		return apierrors.NewBadRequest("reconciliation status input is invalid")
	}
	if input.RetryRequested && input.Outcome != types.ReconciliationOutcomeUnknown {
		return apierrors.NewBadRequest("retry is allowed only for unknown outcomes")
	}
	return nil
}

func getLatestExecutionAttemptByExecutionIDForUpdate(
	ctx context.Context,
	executionID, orgID uuid.UUID,
) (*types.ExecutionAttempt, error) {
	db := internalctx.GetDb(ctx)
	attempt, err := scanExecutionAttempt(db.QueryRow(ctx,
		executionAttemptSelect+`
			WHERE ea.execution_id = @executionId
				AND ea.organization_id = @organizationId
			ORDER BY ea.attempt_number DESC, ea.created_at DESC, ea.id DESC
			LIMIT 1
			FOR UPDATE OF ea, ef`,
		pgx.NamedArgs{"executionId": executionID, "organizationId": orgID},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get latest ExecutionAttempt: %w", err)
	}
	return attempt, nil
}

func getExecutionCancelRequestByIdempotency(
	ctx context.Context,
	orgID, executionID uuid.UUID,
	idempotencyKey string,
) (*types.ExecutionCancelRequest, error) {
	db := internalctx.GetDb(ctx)
	cancel, err := scanExecutionCancelRequest(db.QueryRow(ctx, `
		SELECT id, created_at, organization_id, execution_id,
			execution_attempt_id, requested_by, idempotency_key, reason,
			status, acknowledged_at, acknowledged_by
		FROM ExecutionCancelRequest
		WHERE organization_id = @organizationId
			AND execution_id = @executionId
			AND idempotency_key = @idempotencyKey`,
		pgx.NamedArgs{
			"organizationId": orgID, "executionId": executionID,
			"idempotencyKey": idempotencyKey,
		},
	))
	if err != nil {
		return nil, fmt.Errorf("get ExecutionCancelRequest: %w", err)
	}
	return cancel, nil
}

func scanExecutionCancelRequest(row rowScanner) (*types.ExecutionCancelRequest, error) {
	var request types.ExecutionCancelRequest
	err := row.Scan(
		&request.ID, &request.CreatedAt, &request.OrganizationID, &request.ExecutionID,
		&request.ExecutionAttemptID, &request.RequestedBy, &request.IdempotencyKey,
		&request.Reason, &request.Status, &request.AcknowledgedAt, &request.AcknowledgedBy,
	)
	return &request, err
}

func getExecutionStatusQueryByIdempotency(
	ctx context.Context,
	orgID, executionID uuid.UUID,
	idempotencyKey string,
	forUpdate bool,
) (*types.ExecutionStatusQuery, error) {
	query := `
		SELECT id, created_at, organization_id, execution_id,
			execution_attempt_id, requested_by, idempotency_key, reason,
			status, expires_at, reported_at
		FROM ExecutionStatusQuery
		WHERE organization_id = @organizationId
			AND execution_id = @executionId
			AND idempotency_key = @idempotencyKey`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	db := internalctx.GetDb(ctx)
	result, err := scanExecutionStatusQuery(db.QueryRow(ctx, query, pgx.NamedArgs{
		"organizationId": orgID, "executionId": executionID,
		"idempotencyKey": idempotencyKey,
	}))
	if err != nil {
		return nil, fmt.Errorf("get ExecutionStatusQuery: %w", err)
	}
	return result, nil
}

func getExecutionStatusQueryByIDForUpdate(
	ctx context.Context,
	id, orgID uuid.UUID,
) (*types.ExecutionStatusQuery, error) {
	db := internalctx.GetDb(ctx)
	result, err := scanExecutionStatusQuery(db.QueryRow(ctx, `
		SELECT id, created_at, organization_id, execution_id,
			execution_attempt_id, requested_by, idempotency_key, reason,
			status, expires_at, reported_at
		FROM ExecutionStatusQuery
		WHERE id = @id AND organization_id = @organizationId
		FOR UPDATE`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get ExecutionStatusQuery: %w", err)
	}
	return result, nil
}

func scanExecutionStatusQuery(row rowScanner) (*types.ExecutionStatusQuery, error) {
	var query types.ExecutionStatusQuery
	err := row.Scan(
		&query.ID, &query.CreatedAt, &query.OrganizationID, &query.ExecutionID,
		&query.ExecutionAttemptID, &query.RequestedBy, &query.IdempotencyKey,
		&query.Reason, &query.Status, &query.ExpiresAt, &query.ReportedAt,
	)
	return &query, err
}
