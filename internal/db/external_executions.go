package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/externalexecution"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/stepredaction"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const externalExecutionOutputExpr = `
	ee.id,
	ee.created_at,
	ee.updated_at,
	ee.started_at,
	ee.completed_at,
	ee.callback_deadline_at,
	ee.organization_id,
	ee.step_run_id,
	ee.task_id,
	ee.deployment_plan_id,
	ee.deployment_plan_target_id,
	ee.deployment_target_id,
	ee.application_id,
	ee.release_bundle_id,
	ee.component,
	ee.plan_checksum,
	ee.idempotency_key,
	ee.expected_state_version,
	ee.expected_state_checksum,
	ee.expected_version,
	ee.expected_image,
	ee.expected_platform,
	ee.expected_contracts,
	ee.expected_config_reference,
	ee.expected_config_checksum,
	ee.expected_compose_reference,
	ee.expected_compose_checksum,
	ee.status,
	ee.provider_reference,
	ee.provider_url,
	ee.trigger_attempts,
	ee.last_callback_sequence,
	ee.last_message,
	ee.error_summary,
	ee.actual_version,
	ee.actual_image,
	ee.actual_platform,
	ee.actual_contracts,
	ee.actual_config_reference,
	ee.actual_config_checksum,
	ee.actual_health,
	ee.observed_state_checksum
`

type externalExecutionSource struct {
	OrganizationID         uuid.UUID                      `db:"organization_id"`
	StepRunID              uuid.UUID                      `db:"step_run_id"`
	TaskID                 uuid.UUID                      `db:"task_id"`
	TaskStatus             types.TaskStatus               `db:"task_status"`
	DeploymentPlanID       uuid.UUID                      `db:"deployment_plan_id"`
	DeploymentPlanTargetID uuid.UUID                      `db:"deployment_plan_target_id"`
	DeploymentTargetID     uuid.UUID                      `db:"deployment_target_id"`
	ApplicationID          uuid.UUID                      `db:"application_id"`
	ReleaseBundleID        uuid.UUID                      `db:"release_bundle_id"`
	ActionType             string                         `db:"action_type"`
	Component              string                         `db:"component"`
	PlanChecksum           string                         `db:"plan_checksum"`
	ExpectedStateVersion   int64                          `db:"expected_state_version"`
	ExpectedStateChecksum  string                         `db:"expected_state_checksum"`
	ExpectedVersion        string                         `db:"expected_version"`
	ExpectedImage          string                         `db:"expected_image"`
	ExpectedPlatform       types.DeploymentTargetPlatform `db:"expected_platform"`
	ExpectedContracts      []string                       `db:"expected_contracts"`
	ExpectedConfigChecksum string                         `db:"expected_config_checksum"`
}

func PrepareExternalExecution(
	ctx context.Context,
	request types.PrepareExternalExecutionRequest,
) (*types.ExternalExecution, error) {
	if request.OrganizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if request.StepRunID == uuid.Nil {
		return nil, apierrors.NewBadRequest("stepRunId is required")
	}
	request.Component = strings.TrimSpace(request.Component)
	if request.Component == "" {
		return nil, apierrors.NewBadRequest("component is required")
	}
	if request.CallbackTimeoutSeconds < 1 || request.CallbackTimeoutSeconds > 86400 {
		return nil, apierrors.NewBadRequest("callbackTimeoutSeconds must be between 1 and 86400")
	}

	var prepared *types.ExternalExecution
	var failedPreflight *types.DeploymentPreflightRun
	err := RunTx(ctx, func(ctx context.Context) error {
		source, err := getExternalExecutionSourceForUpdate(ctx, request)
		if err != nil {
			return err
		}
		existing, err := getExternalExecutionByStepRun(ctx, request.StepRunID, request.OrganizationID)
		if err == nil {
			if existing.Component != request.Component {
				return apierrors.NewConflict("step run already belongs to a different external execution component")
			}
			prepared = existing
			return nil
		}
		if !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if source.ActionType != "distr.webhook" {
			return apierrors.NewBadRequest("external execution requires a distr.webhook step")
		}
		if source.TaskStatus != types.TaskStatusRunning {
			return apierrors.NewConflict("external execution task is not running")
		}
		if err := requireExternalExecutionComponentLock(ctx, source); err != nil {
			return err
		}
		plan, err := getDeploymentPlan(ctx, source.DeploymentPlanID, request.OrganizationID)
		if err != nil {
			return err
		}
		task := types.Task{
			ID: source.TaskID, OrganizationID: request.OrganizationID,
			DeploymentPlanID: source.DeploymentPlanID, DeploymentPlanTargetID: source.DeploymentPlanTargetID,
			DeploymentTargetID: source.DeploymentTargetID,
		}
		preflight, passed, err := evaluateAndPersistDeploymentPreflightForTask(ctx, *plan, task)
		if err != nil {
			return err
		}
		if err := attachDeploymentPreflightTasks(ctx, preflight.ID, request.OrganizationID); err != nil {
			return err
		}
		if !passed {
			failedPreflight = preflight
			return nil
		}
		configReference, err := externalExecutionObjectReference(plan.ReleaseContract, source.ExpectedConfigChecksum)
		if err != nil {
			return err
		}
		composeChecksum := strings.TrimSpace(plan.ReleaseContract.Config.ComposeChecksum)
		composeReference, err := externalExecutionObjectReference(plan.ReleaseContract, composeChecksum)
		if err != nil {
			return err
		}
		prepared = &types.ExternalExecution{
			ID: uuid.New(), OrganizationID: request.OrganizationID, StepRunID: source.StepRunID,
			TaskID: source.TaskID, DeploymentPlanID: source.DeploymentPlanID,
			DeploymentPlanTargetID: source.DeploymentPlanTargetID, DeploymentTargetID: source.DeploymentTargetID,
			ApplicationID: source.ApplicationID, ReleaseBundleID: source.ReleaseBundleID,
			Component: source.Component, PlanChecksum: source.PlanChecksum,
			IdempotencyKey:       externalExecutionIdempotencyKey(source),
			ExpectedStateVersion: source.ExpectedStateVersion, ExpectedStateChecksum: source.ExpectedStateChecksum,
			ExpectedVersion: source.ExpectedVersion, ExpectedImage: source.ExpectedImage,
			ExpectedPlatform: source.ExpectedPlatform, ExpectedContracts: slices.Clone(source.ExpectedContracts),
			ExpectedConfigReference: configReference, ExpectedConfigChecksum: source.ExpectedConfigChecksum,
			ExpectedComposeReference: composeReference, ExpectedComposeChecksum: composeChecksum,
			Status:             types.ExternalExecutionStatusQueued,
			CallbackDeadlineAt: time.Now().UTC().Add(time.Duration(request.CallbackTimeoutSeconds) * time.Second),
		}
		return insertExternalExecution(ctx, prepared)
	})
	if err != nil {
		return nil, err
	}
	if failedPreflight != nil {
		return nil, apierrors.NewConflict(deploymentPreflightFailureMessage(*failedPreflight))
	}
	return prepared, nil
}

func GetExternalExecution(ctx context.Context, id, orgID uuid.UUID) (*types.ExternalExecution, error) {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`SELECT `+externalExecutionOutputExpr+`
		FROM ExternalExecution ee
		WHERE ee.id = @id AND ee.organization_id = @organizationId`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ExternalExecution: %w", err)
	}
	return collectExternalExecution(rows)
}

func MarkExternalExecutionTriggered(
	ctx context.Context,
	request types.MarkExternalExecutionTriggeredRequest,
) (*types.ExternalExecution, error) {
	if request.OrganizationID == uuid.Nil || request.ExternalExecutionID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId and externalExecutionId are required")
	}
	if request.TriggerAttempts < 1 {
		return nil, apierrors.NewBadRequest("triggerAttempts must be greater than 0")
	}
	var updated *types.ExternalExecution
	err := RunTx(ctx, func(ctx context.Context) error {
		execution, err := getExternalExecutionForUpdate(ctx, request.ExternalExecutionID, request.OrganizationID)
		if err != nil {
			return err
		}
		if execution.Status != types.ExternalExecutionStatusQueued {
			return apierrors.NewConflict("external execution dispatch is already claimed")
		}
		database := internalctx.GetDb(ctx)
		rows, err := database.Query(ctx,
			`WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
			UPDATE ExternalExecution AS ee
			SET status = @status,
				started_at = COALESCE(
					ee.started_at, write_clock.instant AT TIME ZONE 'UTC'),
				started_at_instant = CASE WHEN ee.started_at IS NULL
					THEN write_clock.instant ELSE ee.started_at_instant END,
				trigger_attempts = GREATEST(ee.trigger_attempts, @triggerAttempts),
				updated_at = write_clock.instant AT TIME ZONE 'UTC',
				updated_at_instant = write_clock.instant
			FROM write_clock
			WHERE ee.id = @id AND ee.organization_id = @organizationId
			RETURNING `+externalExecutionOutputExpr,
			pgx.NamedArgs{
				"id": request.ExternalExecutionID, "organizationId": request.OrganizationID,
				"status": types.ExternalExecutionStatusRunning, "triggerAttempts": request.TriggerAttempts,
			},
		)
		if err != nil {
			return fmt.Errorf("could not mark ExternalExecution triggered: %w", err)
		}
		updated, err = collectExternalExecution(rows)
		return err
	})
	return updated, err
}

func RecordExternalExecutionCallback(
	ctx context.Context,
	request types.RecordExternalExecutionCallbackRequest,
) (*types.ExternalExecution, error) {
	if err := normalizeExternalExecutionCallback(&request); err != nil {
		return nil, err
	}
	payloadHash, err := externalexecution.CallbackPayloadHash(request)
	if err != nil {
		return nil, err
	}
	var updated *types.ExternalExecution
	expired := false
	err = RunTx(ctx, func(ctx context.Context) error {
		execution, err := getExternalExecutionForUpdate(ctx, request.ExternalExecutionID, request.OrganizationID)
		if err != nil {
			return err
		}
		existingHash, err := getExternalExecutionEventPayloadHash(
			ctx, request.ExternalExecutionID, request.OrganizationID, request.Sequence,
		)
		if err == nil {
			if existingHash != payloadHash {
				return apierrors.NewConflict("external execution callback sequence already has a different payload")
			}
			updated = execution
			return nil
		}
		if !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if execution.Status.IsTerminal() {
			return apierrors.NewConflict("external execution is already terminal")
		}
		if !time.Now().UTC().Before(execution.CallbackDeadlineAt) {
			updated, err = timeoutExternalExecutionLocked(ctx, *execution, "external execution callback timed out")
			if err == nil {
				expired = true
			}
			return err
		}
		if request.Sequence != execution.LastCallbackSequence+1 {
			return apierrors.NewConflict("external execution callback sequence is out of order")
		}
		if err := externalexecution.ValidateCallbackSequence(request.Sequence, request.Status); err != nil {
			return apierrors.NewConflict(err.Error())
		}
		if !externalexecution.CanCallbackTransition(execution.Status, request.Status) {
			return apierrors.NewConflict("external execution callback state transition is invalid")
		}

		var observedStateJSON []byte
		observedChecksum := ""
		if request.Status == types.ExternalExecutionStatusSucceeded {
			if request.ObservedState == nil {
				return apierrors.NewBadRequest("observedState is required for a succeeded callback")
			}
			expected := types.ExternalExecutionExpectedState{
				Version: execution.ExpectedVersion, Image: execution.ExpectedImage,
				Platform: execution.ExpectedPlatform, Contracts: execution.ExpectedContracts,
				ConfigReference: execution.ExpectedConfigReference,
				ConfigChecksum:  execution.ExpectedConfigChecksum,
			}
			if err := externalexecution.ValidateObservedState(expected, *request.ObservedState); err != nil {
				return apierrors.NewConflict(err.Error())
			}
			observedChecksum, err = upsertExternalExecutionObservedState(ctx, *execution, *request.ObservedState)
			if err != nil {
				return err
			}
			observedStateJSON, err = json.Marshal(request.ObservedState)
			if err != nil {
				return apierrors.NewBadRequest("observedState must be valid JSON")
			}
		} else if request.ObservedState != nil {
			return apierrors.NewBadRequest("observedState is only allowed for a succeeded callback")
		}

		if err := insertExternalExecutionEvent(ctx, request, observedStateJSON, payloadHash); err != nil {
			return err
		}
		updated, err = updateExternalExecutionFromCallback(ctx, *execution, request, observedChecksum)
		return err
	})
	if err == nil && expired {
		return nil, apierrors.NewConflict("external execution callback deadline has elapsed")
	}
	return updated, err
}

func TimeoutExternalExecution(
	ctx context.Context,
	request types.TimeoutExternalExecutionRequest,
) (*types.ExternalExecution, error) {
	if request.OrganizationID == uuid.Nil || request.ExternalExecutionID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId and externalExecutionId are required")
	}
	request.Message, _ = stepredaction.RedactString(strings.TrimSpace(request.Message))
	request.Message = limitExternalExecutionMessage(request.Message)
	if request.Message == "" {
		request.Message = "external execution callback timed out"
	}
	var updated *types.ExternalExecution
	err := RunTx(ctx, func(ctx context.Context) error {
		execution, err := getExternalExecutionForUpdate(ctx, request.ExternalExecutionID, request.OrganizationID)
		if err != nil {
			return err
		}
		if execution.Status.IsTerminal() {
			updated = execution
			return nil
		}
		if time.Now().UTC().Before(execution.CallbackDeadlineAt) {
			return apierrors.NewConflict("external execution callback deadline has not elapsed")
		}
		updated, err = timeoutExternalExecutionLocked(ctx, *execution, request.Message)
		return err
	})
	return updated, err
}

func timeoutExternalExecutionLocked(
	ctx context.Context,
	execution types.ExternalExecution,
	message string,
) (*types.ExternalExecution, error) {
	eventRequest := types.RecordExternalExecutionCallbackRequest{
		OrganizationID: execution.OrganizationID, ExternalExecutionID: execution.ID,
		Sequence: execution.LastCallbackSequence + 1, Status: types.ExternalExecutionStatusTimedOut,
		Message: message,
	}
	if err := externalexecution.ValidateCallbackSequence(eventRequest.Sequence, eventRequest.Status); err != nil {
		return nil, apierrors.NewConflict(err.Error())
	}
	payloadHash, err := externalexecution.CallbackPayloadHash(eventRequest)
	if err != nil {
		return nil, err
	}
	if err := insertExternalExecutionEvent(ctx, eventRequest, nil, payloadHash); err != nil {
		return nil, err
	}
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
		UPDATE ExternalExecution AS ee
		SET status = @status,
			completed_at = COALESCE(
				ee.completed_at, write_clock.instant AT TIME ZONE 'UTC'),
			completed_at_instant = CASE WHEN ee.completed_at IS NULL
				THEN write_clock.instant ELSE ee.completed_at_instant END,
			last_callback_sequence = @sequence,
			last_message = @message,
			error_summary = @message,
			updated_at = write_clock.instant AT TIME ZONE 'UTC',
			updated_at_instant = write_clock.instant
		FROM write_clock
		WHERE ee.id = @id AND ee.organization_id = @organizationId
		RETURNING `+externalExecutionOutputExpr,
		pgx.NamedArgs{
			"id": execution.ID, "organizationId": execution.OrganizationID,
			"status": types.ExternalExecutionStatusTimedOut, "sequence": eventRequest.Sequence,
			"message": message,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not time out ExternalExecution: %w", err)
	}
	return collectExternalExecution(rows)
}

func FailExternalExecution(
	ctx context.Context,
	request types.FailExternalExecutionRequest,
) (*types.ExternalExecution, error) {
	if request.OrganizationID == uuid.Nil || request.ExternalExecutionID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId and externalExecutionId are required")
	}
	request.Message, _ = stepredaction.RedactString(strings.TrimSpace(request.Message))
	request.Message = limitExternalExecutionMessage(request.Message)
	if request.Message == "" {
		request.Message = "external execution trigger failed"
	}
	var updated *types.ExternalExecution
	err := RunTx(ctx, func(ctx context.Context) error {
		execution, err := getExternalExecutionForUpdate(ctx, request.ExternalExecutionID, request.OrganizationID)
		if err != nil {
			return err
		}
		if execution.Status.IsTerminal() {
			updated = execution
			return nil
		}
		eventRequest := types.RecordExternalExecutionCallbackRequest{
			OrganizationID: request.OrganizationID, ExternalExecutionID: request.ExternalExecutionID,
			Sequence: execution.LastCallbackSequence + 1, Status: types.ExternalExecutionStatusFailed,
			Message: request.Message,
		}
		if err := externalexecution.ValidateCallbackSequence(eventRequest.Sequence, eventRequest.Status); err != nil {
			return apierrors.NewConflict(err.Error())
		}
		payloadHash, err := externalexecution.CallbackPayloadHash(eventRequest)
		if err != nil {
			return err
		}
		if err := insertExternalExecutionEvent(ctx, eventRequest, nil, payloadHash); err != nil {
			return err
		}
		database := internalctx.GetDb(ctx)
		rows, err := database.Query(ctx,
			`WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
			UPDATE ExternalExecution AS ee
			SET status = @status,
				completed_at = COALESCE(
					ee.completed_at, write_clock.instant AT TIME ZONE 'UTC'),
				completed_at_instant = CASE WHEN ee.completed_at IS NULL
					THEN write_clock.instant ELSE ee.completed_at_instant END,
				last_callback_sequence = @sequence,
				last_message = @message,
				error_summary = @message,
				updated_at = write_clock.instant AT TIME ZONE 'UTC',
				updated_at_instant = write_clock.instant
			FROM write_clock
			WHERE ee.id = @id AND ee.organization_id = @organizationId
			RETURNING `+externalExecutionOutputExpr,
			pgx.NamedArgs{
				"id": request.ExternalExecutionID, "organizationId": request.OrganizationID,
				"status": types.ExternalExecutionStatusFailed, "sequence": eventRequest.Sequence,
				"message": request.Message,
			},
		)
		if err != nil {
			return fmt.Errorf("could not fail ExternalExecution: %w", err)
		}
		updated, err = collectExternalExecution(rows)
		return err
	})
	return updated, err
}

func GetExternalExecutionEvents(
	ctx context.Context,
	executionID, orgID uuid.UUID,
) ([]types.ExternalExecutionEvent, error) {
	if _, err := GetExternalExecution(ctx, executionID, orgID); err != nil {
		return nil, err
	}
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`SELECT id, created_at, organization_id, external_execution_id, sequence, status,
			provider_reference, provider_url, message, payload_hash
		FROM ExternalExecutionEvent
		WHERE external_execution_id = @externalExecutionId AND organization_id = @organizationId
		ORDER BY sequence, id
		LIMIT 256`,
		pgx.NamedArgs{"externalExecutionId": executionID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ExternalExecutionEvent: %w", err)
	}
	events, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ExternalExecutionEvent])
	if err != nil {
		return nil, fmt.Errorf("could not collect ExternalExecutionEvent: %w", err)
	}
	return events, nil
}

func normalizeExternalExecutionCallback(request *types.RecordExternalExecutionCallbackRequest) error {
	if request.OrganizationID == uuid.Nil || request.ExternalExecutionID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId and externalExecutionId are required")
	}
	if request.Sequence <= 0 {
		return apierrors.NewBadRequest("sequence must be greater than 0")
	}
	if !request.Status.IsCallbackStatus() {
		return apierrors.NewBadRequest("status must be a callback state")
	}
	request.ProviderReference, _ = stepredaction.RedactString(strings.TrimSpace(request.ProviderReference))
	request.ProviderURL, _ = stepredaction.RedactString(strings.TrimSpace(request.ProviderURL))
	request.Message, _ = stepredaction.RedactString(strings.TrimSpace(request.Message))
	request.Message = limitExternalExecutionMessage(request.Message)
	if len(request.ProviderReference) > 512 || strings.ContainsAny(request.ProviderReference, "\r\n") {
		return apierrors.NewBadRequest("providerReference is invalid")
	}
	if err := externalexecution.ValidateProviderURL(request.ProviderURL); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	if (request.Status == types.ExternalExecutionStatusFailed ||
		request.Status == types.ExternalExecutionStatusCanceled) && request.Message == "" {
		return apierrors.NewBadRequest("message is required for failed or canceled callbacks")
	}
	if request.ObservedState != nil {
		request.ObservedState.Contracts = slices.Clone(request.ObservedState.Contracts)
		slices.Sort(request.ObservedState.Contracts)
	}
	return nil
}

func getExternalExecutionEventPayloadHash(
	ctx context.Context,
	executionID, orgID uuid.UUID,
	sequence int64,
) (string, error) {
	database := internalctx.GetDb(ctx)
	var payloadHash string
	err := database.QueryRow(ctx,
		`SELECT payload_hash FROM ExternalExecutionEvent
		WHERE external_execution_id = @externalExecutionId
			AND organization_id = @organizationId
			AND sequence = @sequence`,
		pgx.NamedArgs{
			"externalExecutionId": executionID, "organizationId": orgID, "sequence": sequence,
		},
	).Scan(&payloadHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apierrors.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("could not query ExternalExecutionEvent payload: %w", err)
	}
	return payloadHash, nil
}

func insertExternalExecutionEvent(
	ctx context.Context,
	request types.RecordExternalExecutionCallbackRequest,
	observedStateJSON []byte,
	payloadHash string,
) error {
	database := internalctx.GetDb(ctx)
	var observedState any
	if len(observedStateJSON) > 0 {
		observedState = observedStateJSON
	}
	_, err := database.Exec(ctx,
		`WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
		INSERT INTO ExternalExecutionEvent (
			created_at, created_at_instant, organization_id,
			external_execution_id, sequence, status, provider_reference,
			provider_url, message, observed_state, payload_hash
		)
		SELECT
			write_clock.instant AT TIME ZONE 'UTC', write_clock.instant,
			@organizationId, @externalExecutionId, @sequence, @status,
			@providerReference, @providerUrl, @message, @observedState,
			@payloadHash
		FROM write_clock`,
		pgx.NamedArgs{
			"organizationId": request.OrganizationID, "externalExecutionId": request.ExternalExecutionID,
			"sequence": request.Sequence, "status": request.Status,
			"providerReference": request.ProviderReference, "providerUrl": request.ProviderURL,
			"message": request.Message, "observedState": observedState, "payloadHash": payloadHash,
		},
	)
	if err != nil {
		return mapExternalExecutionWriteError("insert event for", err)
	}
	return nil
}

func updateExternalExecutionFromCallback(
	ctx context.Context,
	execution types.ExternalExecution,
	request types.RecordExternalExecutionCallbackRequest,
	observedChecksum string,
) (*types.ExternalExecution, error) {
	providerReference := execution.ProviderReference
	if request.ProviderReference != "" {
		providerReference = request.ProviderReference
	}
	providerURL := execution.ProviderURL
	if request.ProviderURL != "" {
		providerURL = request.ProviderURL
	}
	errorSummary := ""
	if request.Status == types.ExternalExecutionStatusFailed || request.Status == types.ExternalExecutionStatusCanceled {
		errorSummary = request.Message
	}
	var actualVersion, actualImage, actualConfigReference, actualConfigChecksum string
	var actualPlatform *types.DeploymentTargetPlatform
	var actualHealth *types.TargetComponentHealth
	actualContracts := []string{}
	if request.ObservedState != nil {
		actualVersion = request.ObservedState.Version
		actualImage = request.ObservedState.Image
		actualPlatform = &request.ObservedState.Platform
		actualContracts = slices.Clone(request.ObservedState.Contracts)
		actualConfigReference = request.ObservedState.ConfigReference
		actualConfigChecksum = request.ObservedState.ConfigChecksum
		actualHealth = &request.ObservedState.Health
	}
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
		UPDATE ExternalExecution AS ee
		SET status = @status,
			started_at = COALESCE(
				ee.started_at, write_clock.instant AT TIME ZONE 'UTC'),
			started_at_instant = CASE WHEN ee.started_at IS NULL
				THEN write_clock.instant ELSE ee.started_at_instant END,
			completed_at = CASE WHEN @terminal THEN COALESCE(
				ee.completed_at, write_clock.instant AT TIME ZONE 'UTC')
				ELSE ee.completed_at END,
			completed_at_instant = CASE
				WHEN NOT @terminal THEN ee.completed_at_instant
				WHEN ee.completed_at IS NULL THEN write_clock.instant
				ELSE ee.completed_at_instant END,
			provider_reference = @providerReference,
			provider_url = @providerUrl,
			last_callback_sequence = @sequence,
			last_message = @message,
			error_summary = @errorSummary,
			actual_version = @actualVersion,
			actual_image = @actualImage,
			actual_platform = @actualPlatform,
			actual_contracts = @actualContracts,
			actual_config_reference = @actualConfigReference,
			actual_config_checksum = @actualConfigChecksum,
			actual_health = @actualHealth,
			observed_state_checksum = @observedStateChecksum,
			updated_at = write_clock.instant AT TIME ZONE 'UTC',
			updated_at_instant = write_clock.instant
		FROM write_clock
		WHERE ee.id = @id AND ee.organization_id = @organizationId
		RETURNING `+externalExecutionOutputExpr,
		pgx.NamedArgs{
			"id": execution.ID, "organizationId": execution.OrganizationID, "status": request.Status,
			"terminal": request.Status.IsTerminal(), "providerReference": providerReference,
			"providerUrl": providerURL, "sequence": request.Sequence, "message": request.Message,
			"errorSummary": errorSummary, "actualVersion": actualVersion, "actualImage": actualImage,
			"actualPlatform": actualPlatform, "actualContracts": actualContracts,
			"actualConfigReference": actualConfigReference, "actualConfigChecksum": actualConfigChecksum,
			"actualHealth": actualHealth, "observedStateChecksum": observedChecksum,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not update ExternalExecution callback state: %w", err)
	}
	return collectExternalExecution(rows)
}

func upsertExternalExecutionObservedState(
	ctx context.Context,
	execution types.ExternalExecution,
	observed types.ExternalExecutionObservedState,
) (string, error) {
	source := externalExecutionSource{
		OrganizationID: execution.OrganizationID, TaskID: execution.TaskID,
		DeploymentTargetID: execution.DeploymentTargetID, Component: execution.Component,
	}
	if err := requireExternalExecutionComponentLock(ctx, source); err != nil {
		return "", err
	}
	checksum, err := externalexecution.ObservedStateChecksum(observed)
	if err != nil {
		return "", err
	}
	stateVersion := execution.ExpectedStateVersion + 1
	database := internalctx.GetDb(ctx)
	var rows pgx.Rows
	if execution.ExpectedStateVersion == 0 {
		rows, err = database.Query(ctx,
			`INSERT INTO TargetComponentState AS tcs (
				organization_id, deployment_target_id, application_id, component, state_version,
				state_checksum, release_bundle_id, version, image, platform, contracts,
				config_reference, config_checksum, health, observed_at
			) VALUES (
				@organizationId, @deploymentTargetId, @applicationId, @component, @stateVersion,
				@stateChecksum, @releaseBundleId, @version, @image, @platform, @contracts,
				@configReference, @configChecksum, @health, now()
			)
			RETURNING `+targetComponentStateOutputExpr,
			externalExecutionObservedStateArgs(execution, observed, checksum, stateVersion),
		)
	} else {
		args := externalExecutionObservedStateArgs(execution, observed, checksum, stateVersion)
		args["expectedStateVersion"] = execution.ExpectedStateVersion
		args["expectedStateChecksum"] = execution.ExpectedStateChecksum
		rows, err = database.Query(ctx,
			`UPDATE TargetComponentState AS tcs
			SET state_version = @stateVersion,
				state_checksum = @stateChecksum,
				release_bundle_id = @releaseBundleId,
				version = @version,
				image = @image,
				platform = @platform,
				contracts = @contracts,
				config_reference = @configReference,
				config_checksum = @configChecksum,
				health = @health,
				observed_at = now(),
				updated_at = now()
			WHERE tcs.organization_id = @organizationId
				AND tcs.deployment_target_id = @deploymentTargetId
				AND tcs.application_id = @applicationId
				AND tcs.component = @component
				AND tcs.state_version = @expectedStateVersion
				AND tcs.state_checksum = @expectedStateChecksum
			RETURNING `+targetComponentStateOutputExpr,
			args,
		)
	}
	if err != nil {
		return "", mapExternalObservedStateWriteError(err)
	}
	state, err := collectExternalObservedState(rows)
	if err != nil {
		return "", err
	}
	if err := insertTargetComponentObservation(
		ctx,
		*state,
		execution.ID,
		execution.DeploymentPlanID,
	); err != nil {
		return "", err
	}
	return checksum, nil
}

func externalExecutionObservedStateArgs(
	execution types.ExternalExecution,
	observed types.ExternalExecutionObservedState,
	checksum string,
	stateVersion int64,
) pgx.NamedArgs {
	return pgx.NamedArgs{
		"organizationId": execution.OrganizationID, "deploymentTargetId": execution.DeploymentTargetID,
		"applicationId": execution.ApplicationID, "component": execution.Component,
		"stateVersion": stateVersion, "stateChecksum": checksum,
		"releaseBundleId": execution.ReleaseBundleID, "version": observed.Version,
		"image": observed.Image, "platform": observed.Platform, "contracts": observed.Contracts,
		"configReference": observed.ConfigReference, "configChecksum": observed.ConfigChecksum,
		"health": observed.Health,
	}
}

func collectExternalObservedState(rows pgx.Rows) (*types.TargetComponentState, error) {
	state, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.TargetComponentState])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.NewConflict("target component state changed after external execution started")
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect external observed state: %w", err)
	}
	return &state, nil
}

func insertTargetComponentObservation(
	ctx context.Context,
	state types.TargetComponentState,
	externalExecutionID uuid.UUID,
	deploymentPlanID uuid.UUID,
) error {
	componentInstanceID, err := resolveObservationComponentInstanceID(
		ctx,
		state.OrganizationID,
		deploymentPlanID,
		state.Component,
	)
	if err != nil {
		return err
	}
	database := internalctx.GetDb(ctx)
	_, err = database.Exec(ctx,
		`INSERT INTO TargetComponentObservation (
			organization_id, target_component_state_id, deployment_target_id, application_id,
			component_instance_id, component, state_version, state_checksum,
			release_bundle_id, version, image, platform,
			contracts, config_reference, config_checksum, health, observed_at, external_execution_id
		) VALUES (
			@organizationId, @targetComponentStateId, @deploymentTargetId, @applicationId,
			@componentInstanceId,
			@component, @stateVersion, @stateChecksum,
			@releaseBundleId, @version, @image, @platform,
			@contracts, @configReference, @configChecksum, @health, @observedAt, @externalExecutionId
		)`,
		pgx.NamedArgs{
			"organizationId": state.OrganizationID, "targetComponentStateId": state.ID,
			"deploymentTargetId": state.DeploymentTargetID, "applicationId": state.ApplicationID,
			"component": state.Component, "stateVersion": state.StateVersion,
			"stateChecksum": state.StateChecksum, "releaseBundleId": state.ReleaseBundleID,
			"version": state.Version, "image": state.Image, "platform": state.Platform,
			"contracts": state.Contracts, "configReference": state.ConfigReference,
			"configChecksum": state.ConfigChecksum, "health": state.Health,
			"observedAt": state.ObservedAt, "externalExecutionId": externalExecutionID,
			"componentInstanceId": componentInstanceID,
		},
	)
	if err != nil {
		return fmt.Errorf("could not insert TargetComponentObservation: %w", err)
	}
	return nil
}

func resolveObservationComponentInstanceID(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentPlanID uuid.UUID,
	component string,
) (*uuid.UUID, error) {
	var planSchema string
	var candidateCount int
	var candidateID string
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		WITH candidate AS (
		  SELECT DISTINCT baseline.component_instance_id
		  FROM DeploymentPlanBaseline baseline
		  JOIN ComponentInstance instance
		    ON instance.id = baseline.component_instance_id
		   AND instance.organization_id = baseline.organization_id
		  WHERE baseline.organization_id = @organizationId
		    AND baseline.deployment_plan_id = @deploymentPlanId
		    AND (
		      baseline.component_key = @component
		      OR instance.physical_name = @component
		    )
		)
		SELECT plan.plan_schema,
		       COUNT(candidate.component_instance_id),
		       COALESCE(MIN(candidate.component_instance_id::TEXT), '')
		FROM DeploymentPlan plan
		LEFT JOIN candidate ON true
		WHERE plan.id = @deploymentPlanId
		  AND plan.organization_id = @organizationId
		GROUP BY plan.plan_schema`,
		pgx.NamedArgs{
			"organizationId": organizationID, "deploymentPlanId": deploymentPlanID,
			"component": component,
		},
	).Scan(&planSchema, &candidateCount, &candidateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("resolve observed component instance: %w", err)
	}
	if planSchema != types.TargetDeploymentPlanSchemaV2 {
		return nil, nil
	}
	if candidateCount != 1 {
		return nil, apierrors.NewConflict(
			"observed component does not resolve to exactly one physical component instance",
		)
	}
	id, err := uuid.Parse(candidateID)
	if err != nil {
		return nil, apierrors.NewConflict(
			"observed component instance identity is invalid",
		)
	}
	return &id, nil
}

func mapExternalObservedStateWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		return apierrors.NewConflict("target component state changed after external execution started")
	}
	return fmt.Errorf("could not project external observed state: %w", err)
}

func limitExternalExecutionMessage(message string) string {
	runes := []rune(message)
	if len(runes) > types.MaxStepRunEventMessageLength {
		return string(runes[:types.MaxStepRunEventMessageLength])
	}
	return message
}

func getExternalExecutionSourceForUpdate(
	ctx context.Context,
	request types.PrepareExternalExecutionRequest,
) (externalExecutionSource, error) {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`SELECT
			t.organization_id,
			sr.id AS step_run_id,
			t.id AS task_id,
			t.status AS task_status,
			t.deployment_plan_id,
			t.deployment_plan_target_id,
			t.deployment_target_id,
			t.application_id,
			t.release_bundle_id,
			sr.action_type,
			dptc.component,
			dp.canonical_checksum AS plan_checksum,
			dptc.expected_state_version,
			dptc.expected_state_checksum,
			dptc.version AS expected_version,
			dptc.image AS expected_image,
			dptc.platform AS expected_platform,
			dptc.contracts AS expected_contracts,
			dptc.config_checksum AS expected_config_checksum
		FROM StepRun sr
		JOIN Task t ON t.id = sr.task_id AND t.organization_id = sr.organization_id
		JOIN DeploymentPlan dp ON dp.id = t.deployment_plan_id AND dp.organization_id = t.organization_id
		JOIN DeploymentPlanTargetComponent dptc
			ON dptc.deployment_plan_id = t.deployment_plan_id
			AND dptc.deployment_plan_target_id = t.deployment_plan_target_id
			AND dptc.organization_id = t.organization_id
			AND dptc.component = @component
		WHERE sr.id = @stepRunId AND sr.organization_id = @organizationId
		FOR UPDATE OF sr, t, dp, dptc`,
		pgx.NamedArgs{
			"stepRunId": request.StepRunID, "organizationId": request.OrganizationID,
			"component": request.Component,
		},
	)
	if err != nil {
		return externalExecutionSource{}, fmt.Errorf("could not query external execution source: %w", err)
	}
	source, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[externalExecutionSource])
	if errors.Is(err, pgx.ErrNoRows) {
		return externalExecutionSource{}, apierrors.ErrNotFound
	}
	if err != nil {
		return externalExecutionSource{}, fmt.Errorf("could not collect external execution source: %w", err)
	}
	return source, nil
}

func getExternalExecutionByStepRun(
	ctx context.Context,
	stepRunID, orgID uuid.UUID,
) (*types.ExternalExecution, error) {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`SELECT `+externalExecutionOutputExpr+`
		FROM ExternalExecution ee
		WHERE ee.step_run_id = @stepRunId AND ee.organization_id = @organizationId`,
		pgx.NamedArgs{"stepRunId": stepRunID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ExternalExecution by step run: %w", err)
	}
	return collectExternalExecution(rows)
}

func getExternalExecutionForUpdate(
	ctx context.Context,
	id, orgID uuid.UUID,
) (*types.ExternalExecution, error) {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`SELECT `+externalExecutionOutputExpr+`
		FROM ExternalExecution ee
		WHERE ee.id = @id AND ee.organization_id = @organizationId
		FOR UPDATE OF ee`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not lock ExternalExecution: %w", err)
	}
	return collectExternalExecution(rows)
}

func insertExternalExecution(ctx context.Context, execution *types.ExternalExecution) error {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`WITH write_clock AS (SELECT CURRENT_TIMESTAMP AS instant)
		INSERT INTO ExternalExecution AS ee (
			id, callback_deadline_at, callback_deadline_at_instant,
			organization_id, step_run_id, task_id, deployment_plan_id,
			deployment_plan_target_id, deployment_target_id, application_id, release_bundle_id,
			component, plan_checksum, idempotency_key, expected_state_version, expected_state_checksum,
			expected_version, expected_image, expected_platform, expected_contracts,
			expected_config_reference, expected_config_checksum,
			expected_compose_reference, expected_compose_checksum, status,
			created_at, created_at_instant, updated_at, updated_at_instant
		)
		SELECT
			@id,
			CAST(@callbackDeadlineAt AS timestamptz) AT TIME ZONE 'UTC',
			CAST(@callbackDeadlineAt AS timestamptz),
			@organizationId, @stepRunId, @taskId, @deploymentPlanId,
			@deploymentPlanTargetId, @deploymentTargetId, @applicationId, @releaseBundleId,
			@component, @planChecksum, @idempotencyKey, @expectedStateVersion, @expectedStateChecksum,
			@expectedVersion, @expectedImage, @expectedPlatform, @expectedContracts,
			@expectedConfigReference, @expectedConfigChecksum,
			@expectedComposeReference, @expectedComposeChecksum, @status,
			write_clock.instant AT TIME ZONE 'UTC', write_clock.instant,
			write_clock.instant AT TIME ZONE 'UTC', write_clock.instant
		FROM write_clock
		RETURNING `+externalExecutionOutputExpr,
		pgx.NamedArgs{
			"id": execution.ID, "callbackDeadlineAt": execution.CallbackDeadlineAt.UTC(),
			"organizationId": execution.OrganizationID, "stepRunId": execution.StepRunID,
			"taskId": execution.TaskID, "deploymentPlanId": execution.DeploymentPlanID,
			"deploymentPlanTargetId": execution.DeploymentPlanTargetID,
			"deploymentTargetId":     execution.DeploymentTargetID, "applicationId": execution.ApplicationID,
			"releaseBundleId": execution.ReleaseBundleID, "component": execution.Component,
			"planChecksum": execution.PlanChecksum, "idempotencyKey": execution.IdempotencyKey,
			"expectedStateVersion":  execution.ExpectedStateVersion,
			"expectedStateChecksum": execution.ExpectedStateChecksum,
			"expectedVersion":       execution.ExpectedVersion, "expectedImage": execution.ExpectedImage,
			"expectedPlatform": execution.ExpectedPlatform, "expectedContracts": execution.ExpectedContracts,
			"expectedConfigReference":  execution.ExpectedConfigReference,
			"expectedConfigChecksum":   execution.ExpectedConfigChecksum,
			"expectedComposeReference": execution.ExpectedComposeReference,
			"expectedComposeChecksum":  execution.ExpectedComposeChecksum, "status": execution.Status,
		},
	)
	if err != nil {
		return mapExternalExecutionWriteError("insert", err)
	}
	created, err := collectExternalExecution(rows)
	if err != nil {
		return err
	}
	*execution = *created
	return nil
}

func collectExternalExecution(rows pgx.Rows) (*types.ExternalExecution, error) {
	execution, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ExternalExecution])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect ExternalExecution: %w", err)
	}
	return &execution, nil
}

func externalExecutionIdempotencyKey(source externalExecutionSource) string {
	data := strings.Join([]string{
		source.PlanChecksum, source.TaskID.String(), source.StepRunID.String(),
		source.DeploymentTargetID.String(), source.Component,
	}, "\n")
	sum := sha256.Sum256([]byte(data))
	return "ext:" + hex.EncodeToString(sum[:])
}

func externalExecutionObjectReference(
	contract *types.ReleaseContract,
	configChecksum string,
) (string, error) {
	normalized := releasebundles.NormalizedReleaseContract(contract)
	if normalized == nil {
		return "", apierrors.NewConflict("external execution requires a frozen release contract")
	}
	for _, object := range normalized.Config.ImmutableObjects {
		if !strings.EqualFold(object.Checksum, configChecksum) || object.URI == "" {
			continue
		}
		if object.VersionID == "" {
			if releasebundles.IsContentAddressedConfigObject(object) {
				return object.URI, nil
			}
			continue
		}
		parsed, err := url.Parse(object.URI)
		if err != nil {
			return "", apierrors.NewConflict("release contract contains an invalid immutable config URI")
		}
		query := parsed.Query()
		query.Set("versionId", object.VersionID)
		parsed.RawQuery = query.Encode()
		return parsed.String(), nil
	}
	return "", apierrors.NewConflict("release contract does not contain the required immutable config object")
}

func requireExternalExecutionComponentLock(ctx context.Context, source externalExecutionSource) error {
	resourceKey := source.DeploymentTargetID.String() + ":" + source.Component
	group := taskResourceLockGroup{
		OrganizationID: source.OrganizationID,
		ResourceType:   types.TaskLockResourceTargetComponent, ResourceKey: resourceKey,
	}
	if err := lockTaskResourceAdvisoryGroups(ctx, []taskResourceLockGroup{group}); err != nil {
		return err
	}
	database := internalctx.GetDb(ctx)
	var ownsLock bool
	err := database.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM TaskResourceLock trl
			WHERE trl.organization_id = @organizationId
				AND trl.task_id = @taskId
				AND trl.resource_type = @resourceType
				AND trl.resource_key = @resourceKey
				AND trl.acquired_at IS NOT NULL
				AND trl.released_at IS NULL
		)`,
		pgx.NamedArgs{
			"organizationId": group.OrganizationID, "taskId": source.TaskID,
			"resourceType": group.ResourceType, "resourceKey": group.ResourceKey,
		},
	).Scan(&ownsLock)
	if err != nil {
		return fmt.Errorf("could not verify external execution component lock: %w", err)
	}
	if !ownsLock {
		return apierrors.NewConflict("task does not own the active target-component lock")
	}
	return nil
}

func mapExternalExecutionWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgerrcode.UniqueViolation:
			return apierrors.NewConflict("external execution already exists")
		case pgerrcode.ForeignKeyViolation:
			return apierrors.ErrNotFound
		case pgerrcode.CheckViolation:
			return apierrors.NewBadRequest("external execution violates an immutable constraint")
		}
	}
	return fmt.Errorf("could not %s ExternalExecution: %w", operation, err)
}
