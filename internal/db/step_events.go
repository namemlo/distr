package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/stepredaction"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type stepRunEventTarget struct {
	TaskID     uuid.UUID
	AgentID    uuid.UUID
	TaskStatus types.TaskStatus
	StepStatus types.StepRunStatus
}

type stepRunEventLease struct {
	ID       uuid.UUID
	Expired  bool
	Released bool
}

type preparedStepRunEventRequest struct {
	types.RecordAgentStepRunEventRequest
	DetailsJSON []byte
	PayloadHash string
	Redacted    bool
	Logs        []preparedStepRunLogChunk
	Outputs     []preparedStepRunOutput
}

type preparedStepRunLogChunk struct {
	types.RecordStepRunLogChunkRequest
	Redacted bool
}

type preparedStepRunOutput struct {
	types.RecordStepRunOutputRequest
	ValueJSON []byte
	Redacted  bool
}

func RecordAgentStepRunEvent(
	ctx context.Context,
	request types.RecordAgentStepRunEventRequest,
) (*types.StepRunEvent, error) {
	if err := validateRecordAgentStepRunEventRequest(&request); err != nil {
		return nil, err
	}
	var event *types.StepRunEvent
	err := RunTx(ctx, func(ctx context.Context) error {
		target, err := getStepRunEventTargetForUpdate(ctx, request.OrganizationID, request.StepRunID)
		if err != nil {
			return err
		}
		if target.AgentID != request.AgentID {
			return apierrors.ErrNotFound
		}
		lease, err := getTaskLeaseForStepRunEvent(ctx, request, target.TaskID)
		if err != nil {
			return err
		}
		secretValues, err := getStepRunSecretValuesForRedaction(ctx, request.OrganizationID, request.StepRunID)
		if err != nil {
			return err
		}
		prepared, err := prepareStepRunEventRequest(request, secretValues)
		if err != nil {
			return err
		}
		existing, err := getStepRunEventBySequence(
			ctx, prepared.OrganizationID, prepared.StepRunID, lease.ID, prepared.Sequence,
		)
		if err == nil {
			if existing.PayloadHash != prepared.PayloadHash {
				return apierrors.NewConflict("step event sequence already recorded with different payload")
			}
			event = existing.StepRunEvent
			return nil
		}
		if !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if lease.Released {
			return apierrors.NewConflict("task lease has been released")
		}
		if lease.Expired {
			return apierrors.NewConflict("task lease has expired")
		}
		maxSequence, err := getMaxStepRunEventSequence(ctx, prepared.OrganizationID, prepared.StepRunID, lease.ID)
		if err != nil {
			return err
		}
		if prepared.Sequence != maxSequence+1 {
			return apierrors.NewConflict("step event sequence is out of order")
		}
		if err := validateStepRunOutputNameLimit(
			ctx, prepared.OrganizationID, prepared.StepRunID, prepared.Outputs,
		); err != nil {
			return err
		}
		if err := applyStepRunEventTransition(ctx, target, prepared); err != nil {
			return err
		}
		eventID, err := insertStepRunEvent(ctx, prepared, target.TaskID, lease.ID)
		if err != nil {
			return err
		}
		if err := insertStepRunLogChunks(ctx, eventID, target.TaskID, lease.ID, prepared); err != nil {
			return err
		}
		if err := insertStepRunOutputs(ctx, eventID, target.TaskID, lease.ID, prepared); err != nil {
			return err
		}
		event, err = getStepRunEvent(ctx, eventID, prepared.OrganizationID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return event, nil
}

func GetTaskTimeline(ctx context.Context, taskID, orgID uuid.UUID) (*types.TaskTimeline, error) {
	if _, err := getTask(ctx, taskID, orgID); err != nil {
		return nil, err
	}
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT sre.id
		FROM StepRunEvent sre
		JOIN StepRun sr
			ON sr.id = sre.step_run_id
			AND sr.organization_id = sre.organization_id
		JOIN TaskLease tl
			ON tl.id = sre.task_lease_id
			AND tl.organization_id = sre.organization_id
		WHERE sre.task_id = @taskId
			AND sre.organization_id = @organizationId
		ORDER BY sr.sort_order, tl.attempt, sre.sequence, sre.id`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query StepRunEvent timeline: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("could not collect StepRunEvent timeline: %w", err)
	}
	events := make([]types.StepRunEvent, 0, len(ids))
	for _, id := range ids {
		event, err := getStepRunEvent(ctx, id, orgID)
		if err != nil {
			return nil, err
		}
		events = append(events, *event)
	}
	return &types.TaskTimeline{
		OrganizationID: orgID,
		TaskID:         taskID,
		Events:         events,
	}, nil
}

func GetTaskLogs(ctx context.Context, taskID, orgID uuid.UUID) ([]types.StepRunLogChunk, error) {
	if _, err := getTask(ctx, taskID, orgID); err != nil {
		return nil, err
	}
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			lc.id,
			lc.created_at,
			lc.occurred_at,
			lc.event_id,
			lc.organization_id,
			lc.task_id,
			lc.step_run_id,
			lc.task_lease_id,
			lc.agent_id,
			lc.chunk_index,
			lc.stream,
			lc.severity,
			lc.body,
			lc.redacted
		FROM StepRunLogChunk lc
		JOIN StepRunEvent sre
			ON sre.id = lc.event_id
			AND sre.organization_id = lc.organization_id
		JOIN StepRun sr
			ON sr.id = lc.step_run_id
			AND sr.organization_id = lc.organization_id
		JOIN TaskLease tl
			ON tl.id = lc.task_lease_id
			AND tl.organization_id = lc.organization_id
		WHERE lc.task_id = @taskId
			AND lc.organization_id = @organizationId
		ORDER BY sr.sort_order, tl.attempt, sre.sequence, lc.chunk_index, lc.id`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query StepRunLogChunk: %w", err)
	}
	logs, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.StepRunLogChunk])
	if err != nil {
		return nil, fmt.Errorf("could not collect StepRunLogChunk: %w", err)
	}
	return logs, nil
}

func prepareStepRunEventRequest(
	request types.RecordAgentStepRunEventRequest,
	secretValues []string,
) (preparedStepRunEventRequest, error) {
	if err := validateRecordAgentStepRunEventRequest(&request); err != nil {
		return preparedStepRunEventRequest{}, err
	}
	message, messageRedacted := stepredaction.RedactStringWithValues(request.Message, secretValues)
	details := map[string]any{}
	if request.Details != nil {
		redactedDetails, changed := stepredaction.RedactValueWithValues(request.Details, secretValues)
		messageRedacted = messageRedacted || changed
		if typed, ok := redactedDetails.(map[string]any); ok {
			details = typed
		}
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return preparedStepRunEventRequest{}, apierrors.NewBadRequest("details must be valid JSON")
	}
	if len(detailsJSON) > types.MaxStepRunEventDetailsBytes {
		return preparedStepRunEventRequest{}, apierrors.NewBadRequest("details is too large")
	}
	prepared := preparedStepRunEventRequest{
		RecordAgentStepRunEventRequest: request,
		DetailsJSON:                    detailsJSON,
		Redacted:                       messageRedacted,
		Logs:                           make([]preparedStepRunLogChunk, 0, len(request.Logs)),
		Outputs:                        make([]preparedStepRunOutput, 0, len(request.Outputs)),
	}
	prepared.Message = message
	prepared.Details = details
	for _, log := range request.Logs {
		body, redacted := stepredaction.RedactStringWithValues(log.Body, secretValues)
		log.Body = body
		prepared.Redacted = prepared.Redacted || redacted
		prepared.Logs = append(prepared.Logs, preparedStepRunLogChunk{
			RecordStepRunLogChunkRequest: log,
			Redacted:                     redacted,
		})
	}
	for _, output := range request.Outputs {
		output.Name = strings.TrimSpace(output.Name)
		preparedOutput := preparedStepRunOutput{
			RecordStepRunOutputRequest: output,
		}
		if output.Sensitive {
			preparedOutput.Redacted = true
			prepared.Redacted = true
		} else {
			value, redacted := stepredaction.RedactValueWithValues(output.Value, secretValues)
			preparedOutput.Value = value
			preparedOutput.Redacted = redacted
			prepared.Redacted = prepared.Redacted || redacted
			valueJSON, err := json.Marshal(value)
			if err != nil {
				return preparedStepRunEventRequest{}, apierrors.NewBadRequest("output value must be valid JSON")
			}
			if len(valueJSON) > types.MaxStepRunOutputValueBytes {
				return preparedStepRunEventRequest{}, apierrors.NewBadRequest("output value is too large")
			}
			preparedOutput.ValueJSON = valueJSON
		}
		prepared.Outputs = append(prepared.Outputs, preparedOutput)
	}
	payloadHash, err := stepRunEventPayloadHash(prepared)
	if err != nil {
		return preparedStepRunEventRequest{}, err
	}
	prepared.PayloadHash = payloadHash
	return prepared, nil
}

func stepRunEventPayloadHash(request preparedStepRunEventRequest) (string, error) {
	type canonicalLog struct {
		OccurredAt any                      `json:"occurredAt,omitempty"`
		Stream     types.StepRunLogStream   `json:"stream"`
		Severity   types.StepRunLogSeverity `json:"severity"`
		Body       string                   `json:"body"`
		Redacted   bool                     `json:"redacted"`
	}
	type canonicalOutput struct {
		Name      string          `json:"name"`
		Value     json.RawMessage `json:"value"`
		Sensitive bool            `json:"sensitive"`
		Redacted  bool            `json:"redacted"`
	}
	logs := make([]canonicalLog, 0, len(request.Logs))
	for _, log := range request.Logs {
		logs = append(logs, canonicalLog{
			OccurredAt: log.OccurredAt,
			Stream:     log.Stream,
			Severity:   log.Severity,
			Body:       log.Body,
			Redacted:   log.Redacted,
		})
	}
	outputs := make([]canonicalOutput, 0, len(request.Outputs))
	for _, output := range request.Outputs {
		outputs = append(outputs, canonicalOutput{
			Name:      output.Name,
			Value:     json.RawMessage(output.ValueJSON),
			Sensitive: output.Sensitive,
			Redacted:  output.Redacted,
		})
	}
	sort.Slice(outputs, func(i, j int) bool {
		return outputs[i].Name < outputs[j].Name
	})
	payload := struct {
		Sequence        int64                  `json:"sequence"`
		Type            types.StepRunEventType `json:"type"`
		OccurredAt      any                    `json:"occurredAt,omitempty"`
		Message         string                 `json:"message"`
		ProgressPercent *int                   `json:"progressPercent,omitempty"`
		Details         json.RawMessage        `json:"details"`
		Redacted        bool                   `json:"redacted"`
		Logs            []canonicalLog         `json:"logs"`
		Outputs         []canonicalOutput      `json:"outputs"`
	}{
		Sequence:        request.Sequence,
		Type:            request.Type,
		OccurredAt:      request.OccurredAt,
		Message:         request.Message,
		ProgressPercent: request.ProgressPercent,
		Details:         json.RawMessage(request.DetailsJSON),
		Redacted:        request.Redacted,
		Logs:            logs,
		Outputs:         outputs,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", apierrors.NewBadRequest("step event payload must be valid JSON")
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateRecordAgentStepRunEventRequest(request *types.RecordAgentStepRunEventRequest) error {
	request.LeaseToken = strings.TrimSpace(request.LeaseToken)
	request.Message = strings.TrimSpace(request.Message)
	if request.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if request.AgentID == uuid.Nil {
		return apierrors.NewBadRequest("agentId is required")
	}
	if request.StepRunID == uuid.Nil {
		return apierrors.NewBadRequest("stepRunId is required")
	}
	if request.LeaseToken == "" {
		return apierrors.NewBadRequest("leaseToken is required")
	}
	if request.Sequence <= 0 {
		return apierrors.NewBadRequest("sequence must be greater than 0")
	}
	if !request.Type.IsValid() {
		return apierrors.NewBadRequest("type is invalid")
	}
	if len(request.Message) > types.MaxStepRunEventMessageLength {
		return apierrors.NewBadRequest("message is too long")
	}
	if request.ProgressPercent != nil && (*request.ProgressPercent < 0 || *request.ProgressPercent > 100) {
		return apierrors.NewBadRequest("progressPercent must be between 0 and 100")
	}
	if len(request.Logs) > types.MaxStepRunEventLogChunkCount {
		return apierrors.NewBadRequest("logs contains too many entries")
	}
	for i, log := range request.Logs {
		if !log.Stream.IsValid() {
			return apierrors.NewBadRequest(fmt.Sprintf("logs[%d].stream is invalid", i))
		}
		if !log.Severity.IsValid() {
			return apierrors.NewBadRequest(fmt.Sprintf("logs[%d].severity is invalid", i))
		}
		if strings.TrimSpace(log.Body) == "" {
			return apierrors.NewBadRequest(fmt.Sprintf("logs[%d].body is required", i))
		}
		if len(log.Body) > types.MaxStepRunLogChunkBodyLength {
			return apierrors.NewBadRequest(fmt.Sprintf("logs[%d].body is too long", i))
		}
	}
	if len(request.Outputs) > types.MaxStepRunEventOutputItemCount {
		return apierrors.NewBadRequest("outputs contains too many entries")
	}
	seenOutputs := map[string]struct{}{}
	for i := range request.Outputs {
		request.Outputs[i].Name = strings.TrimSpace(request.Outputs[i].Name)
		if request.Outputs[i].Name == "" {
			return apierrors.NewBadRequest(fmt.Sprintf("outputs[%d].name is required", i))
		}
		if len(request.Outputs[i].Name) > types.MaxStepRunOutputNameLength {
			return apierrors.NewBadRequest(fmt.Sprintf("outputs[%d].name is too long", i))
		}
		if _, ok := seenOutputs[request.Outputs[i].Name]; ok {
			return apierrors.NewBadRequest("outputs contains duplicate name")
		}
		seenOutputs[request.Outputs[i].Name] = struct{}{}
	}
	return nil
}

func getStepRunEventTargetForUpdate(ctx context.Context, orgID, stepRunID uuid.UUID) (stepRunEventTarget, error) {
	db := internalctx.GetDb(ctx)
	var target stepRunEventTarget
	err := db.QueryRow(ctx,
		`SELECT
			t.id,
			t.deployment_target_id,
			t.status,
			sr.status
		FROM StepRun sr
		JOIN Task t
			ON t.id = sr.task_id
			AND t.organization_id = sr.organization_id
		WHERE sr.id = @stepRunId
			AND sr.organization_id = @organizationId
		FOR UPDATE OF sr, t`,
		pgx.NamedArgs{"stepRunId": stepRunID, "organizationId": orgID},
	).Scan(&target.TaskID, &target.AgentID, &target.TaskStatus, &target.StepStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return stepRunEventTarget{}, apierrors.ErrNotFound
	}
	if err != nil {
		return stepRunEventTarget{}, fmt.Errorf("could not lock StepRun for event: %w", err)
	}
	return target, nil
}

func getStepRunSecretValuesForRedaction(ctx context.Context, orgID, stepRunID uuid.UUID) ([]string, error) {
	db := internalctx.GetDb(ctx)
	var deploymentPlanTargetID uuid.UUID
	var actionType string
	var inputBindings map[string]any
	err := db.QueryRow(ctx,
		`SELECT
			t.deployment_plan_target_id,
			dps.action_type,
			dps.input_bindings
		FROM StepRun sr
		JOIN Task t
			ON t.id = sr.task_id
			AND t.organization_id = sr.organization_id
		JOIN DeploymentPlanStep dps
			ON dps.id = sr.deployment_plan_step_id
			AND dps.deployment_plan_id = sr.deployment_plan_id
			AND dps.organization_id = sr.organization_id
		WHERE sr.id = @stepRunId
			AND sr.organization_id = @organizationId`,
		pgx.NamedArgs{"stepRunId": stepRunID, "organizationId": orgID},
	).Scan(&deploymentPlanTargetID, &actionType, &inputBindings)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not query StepRun secret values for redaction: %w", err)
	}
	if actionType != "distr.compose.deploy" {
		return nil, nil
	}
	return getComposeRegistrySecretValuesForRedaction(ctx, types.Task{
		OrganizationID:         orgID,
		DeploymentPlanTargetID: deploymentPlanTargetID,
	}, inputBindings)
}

func getComposeRegistrySecretValuesForRedaction(
	ctx context.Context,
	task types.Task,
	inputBindings map[string]any,
) ([]string, error) {
	applicationVersion, ok := mapStringAny(inputBindings["applicationVersion"])
	if !ok {
		return nil, nil
	}
	registryAuth, ok := mapStringAny(applicationVersion["registryAuth"])
	if !ok {
		return nil, nil
	}
	values := make([]string, 0, len(registryAuth))
	for _, rawAuth := range registryAuth {
		auth, ok := mapStringAny(rawAuth)
		if !ok {
			continue
		}
		if value, ok := stringValue(auth["password"]); ok && value != "" {
			values = append(values, value)
		}
		reference, ok := stringValue(auth["passwordSecretRef"])
		if !ok || strings.TrimSpace(reference) == "" {
			continue
		}
		value, err := getTaskLeaseSecretValue(ctx, task, strings.TrimSpace(reference))
		if err != nil {
			return nil, err
		}
		if value != "" {
			values = append(values, value)
		}
	}
	return values, nil
}

func getTaskLeaseForStepRunEvent(
	ctx context.Context,
	request types.RecordAgentStepRunEventRequest,
	taskID uuid.UUID,
) (stepRunEventLease, error) {
	db := internalctx.GetDb(ctx)
	var lease stepRunEventLease
	err := db.QueryRow(ctx,
		`SELECT
			id,
			expires_at <= now() AS expired,
			released_at IS NOT NULL AS released
		FROM TaskLease
		WHERE organization_id = @organizationId
			AND agent_id = @agentId
			AND task_id = @taskId
			AND lease_token_hash = @leaseTokenHash
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationId": request.OrganizationID,
			"agentId":        request.AgentID,
			"taskId":         taskID,
			"leaseTokenHash": hashTaskLeaseToken(request.LeaseToken),
		},
	).Scan(&lease.ID, &lease.Expired, &lease.Released)
	if errors.Is(err, pgx.ErrNoRows) {
		return stepRunEventLease{}, apierrors.ErrNotFound
	}
	if err != nil {
		return stepRunEventLease{}, fmt.Errorf("could not lock TaskLease for step event: %w", err)
	}
	return lease, nil
}

type stepRunEventReplay struct {
	*types.StepRunEvent
	PayloadHash string
}

func getStepRunEventBySequence(
	ctx context.Context,
	orgID, stepRunID, leaseID uuid.UUID,
	sequence int64,
) (*stepRunEventReplay, error) {
	db := internalctx.GetDb(ctx)
	var eventID uuid.UUID
	var payloadHash string
	err := db.QueryRow(ctx,
		`SELECT id, payload_hash
		FROM StepRunEvent
		WHERE organization_id = @organizationId
			AND step_run_id = @stepRunId
			AND task_lease_id = @taskLeaseId
			AND sequence = @sequence`,
		pgx.NamedArgs{
			"organizationId": orgID,
			"stepRunId":      stepRunID,
			"taskLeaseId":    leaseID,
			"sequence":       sequence,
		},
	).Scan(&eventID, &payloadHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not query StepRunEvent by sequence: %w", err)
	}
	event, err := getStepRunEvent(ctx, eventID, orgID)
	if err != nil {
		return nil, err
	}
	return &stepRunEventReplay{
		StepRunEvent: event,
		PayloadHash:  payloadHash,
	}, nil
}

func getMaxStepRunEventSequence(ctx context.Context, orgID, stepRunID, leaseID uuid.UUID) (int64, error) {
	db := internalctx.GetDb(ctx)
	var sequence int64
	err := db.QueryRow(ctx,
		`SELECT COALESCE(max(sequence), 0)
		FROM StepRunEvent
		WHERE organization_id = @organizationId
			AND step_run_id = @stepRunId
			AND task_lease_id = @taskLeaseId`,
		pgx.NamedArgs{
			"organizationId": orgID,
			"stepRunId":      stepRunID,
			"taskLeaseId":    leaseID,
		},
	).Scan(&sequence)
	if err != nil {
		return 0, fmt.Errorf("could not query StepRunEvent max sequence: %w", err)
	}
	return sequence, nil
}

func applyStepRunEventTransition(
	ctx context.Context,
	target stepRunEventTarget,
	request preparedStepRunEventRequest,
) error {
	if target.TaskStatus != types.TaskStatusRunning {
		return apierrors.NewConflict("task must be RUNNING to record step events")
	}
	switch request.Type {
	case types.StepRunEventTypeStarted:
		if target.StepStatus != types.StepRunStatusPending {
			return apierrors.NewConflict("step run must be PENDING to start")
		}
		return updateStepRunStatus(ctx, request.StepRunID, request.OrganizationID, types.StepRunStatusRunning)
	case types.StepRunEventTypeProgress, types.StepRunEventTypeLog, types.StepRunEventTypeOutput:
		if target.StepStatus != types.StepRunStatusRunning {
			return apierrors.NewConflict("step run must be RUNNING to record progress")
		}
		return nil
	case types.StepRunEventTypeSucceeded:
		if target.StepStatus != types.StepRunStatusRunning {
			return apierrors.NewConflict("step run must be RUNNING to succeed")
		}
		if err := updateStepRunStatus(
			ctx, request.StepRunID, request.OrganizationID, types.StepRunStatusSucceeded,
		); err != nil {
			return err
		}
		return completeTaskIfStepRunsTerminal(ctx, target.TaskID, request.OrganizationID)
	case types.StepRunEventTypeFailed:
		if target.StepStatus != types.StepRunStatusRunning {
			return apierrors.NewConflict("step run must be RUNNING to fail")
		}
		if err := updateStepRunStatus(ctx, request.StepRunID, request.OrganizationID, types.StepRunStatusFailed); err != nil {
			return err
		}
		return updateTaskStatus(ctx, target.TaskID, request.OrganizationID, types.TaskStatusFailed)
	default:
		return apierrors.NewBadRequest("type is invalid")
	}
}

func completeTaskIfStepRunsTerminal(ctx context.Context, taskID, orgID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	var incomplete int
	var failed int
	err := db.QueryRow(ctx,
		`SELECT
			count(*) FILTER (WHERE status NOT IN (@succeededStatus, @skippedStatus, @failedStatus)),
			count(*) FILTER (WHERE status = @failedStatus)
		FROM StepRun
		WHERE task_id = @taskId
			AND organization_id = @organizationId`,
		pgx.NamedArgs{
			"taskId":          taskID,
			"organizationId":  orgID,
			"succeededStatus": types.StepRunStatusSucceeded,
			"skippedStatus":   types.StepRunStatusSkipped,
			"failedStatus":    types.StepRunStatusFailed,
		},
	).Scan(&incomplete, &failed)
	if err != nil {
		return fmt.Errorf("could not query StepRun terminal status: %w", err)
	}
	if failed > 0 {
		return updateTaskStatus(ctx, taskID, orgID, types.TaskStatusFailed)
	}
	if incomplete == 0 {
		return updateTaskStatus(ctx, taskID, orgID, types.TaskStatusSucceeded)
	}
	return nil
}

func insertStepRunEvent(
	ctx context.Context,
	request preparedStepRunEventRequest,
	taskID, leaseID uuid.UUID,
) (uuid.UUID, error) {
	db := internalctx.GetDb(ctx)
	var eventID uuid.UUID
	err := db.QueryRow(ctx,
		`INSERT INTO StepRunEvent (
			organization_id,
			task_id,
			step_run_id,
			task_lease_id,
			agent_id,
			sequence,
			event_type,
			occurred_at,
			message,
			progress_percent,
			details,
			payload_hash,
			redacted
		)
		VALUES (
			@organizationId,
			@taskId,
			@stepRunId,
			@taskLeaseId,
			@agentId,
			@sequence,
			@eventType,
			COALESCE(@occurredAt::timestamp, now()),
			@message,
			@progressPercent,
			@details::jsonb,
			@payloadHash,
			@redacted
		)
		RETURNING id`,
		pgx.NamedArgs{
			"organizationId":  request.OrganizationID,
			"taskId":          taskID,
			"stepRunId":       request.StepRunID,
			"taskLeaseId":     leaseID,
			"agentId":         request.AgentID,
			"sequence":        request.Sequence,
			"eventType":       request.Type,
			"occurredAt":      request.OccurredAt,
			"message":         request.Message,
			"progressPercent": request.ProgressPercent,
			"details":         request.DetailsJSON,
			"payloadHash":     request.PayloadHash,
			"redacted":        request.Redacted,
		},
	).Scan(&eventID)
	if err != nil {
		return uuid.Nil, mapStepEventWriteError("insert event", err)
	}
	return eventID, nil
}

func insertStepRunLogChunks(
	ctx context.Context,
	eventID, taskID, leaseID uuid.UUID,
	request preparedStepRunEventRequest,
) error {
	if len(request.Logs) == 0 {
		return nil
	}
	db := internalctx.GetDb(ctx)
	for i, log := range request.Logs {
		_, err := db.Exec(ctx,
			`INSERT INTO StepRunLogChunk (
				event_id,
				organization_id,
				task_id,
				step_run_id,
				task_lease_id,
				agent_id,
				chunk_index,
				occurred_at,
				stream,
				severity,
				body,
				redacted
			)
			VALUES (
				@eventId,
				@organizationId,
				@taskId,
				@stepRunId,
				@taskLeaseId,
				@agentId,
				@chunkIndex,
				COALESCE(@occurredAt::timestamp, (SELECT occurred_at FROM StepRunEvent WHERE id = @eventId)),
				@stream,
				@severity,
				@body,
				@redacted
			)`,
			pgx.NamedArgs{
				"eventId":        eventID,
				"organizationId": request.OrganizationID,
				"taskId":         taskID,
				"stepRunId":      request.StepRunID,
				"taskLeaseId":    leaseID,
				"agentId":        request.AgentID,
				"chunkIndex":     i,
				"occurredAt":     log.OccurredAt,
				"stream":         log.Stream,
				"severity":       log.Severity,
				"body":           log.Body,
				"redacted":       log.Redacted,
			},
		)
		if err != nil {
			return mapStepEventWriteError("insert log chunk", err)
		}
	}
	return nil
}

func validateStepRunOutputNameLimit(
	ctx context.Context,
	orgID, stepRunID uuid.UUID,
	outputs []preparedStepRunOutput,
) error {
	if len(outputs) == 0 {
		return nil
	}
	names := make(map[string]struct{}, len(outputs))
	for _, output := range outputs {
		names[output.Name] = struct{}{}
	}
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT DISTINCT name
		FROM StepRunOutput
		WHERE organization_id = @organizationId
			AND step_run_id = @stepRunId`,
		pgx.NamedArgs{"organizationId": orgID, "stepRunId": stepRunID},
	)
	if err != nil {
		return fmt.Errorf("could not query StepRunOutput names: %w", err)
	}
	existing, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("could not collect StepRunOutput names: %w", err)
	}
	for _, name := range existing {
		names[name] = struct{}{}
	}
	if len(names) > types.MaxStepRunEventOutputItemCount {
		return apierrors.NewConflict("step run output name limit exceeded")
	}
	return nil
}

func insertStepRunOutputs(
	ctx context.Context,
	eventID, taskID, leaseID uuid.UUID,
	request preparedStepRunEventRequest,
) error {
	if len(request.Outputs) == 0 {
		return nil
	}
	db := internalctx.GetDb(ctx)
	for _, output := range request.Outputs {
		_, err := db.Exec(ctx,
			`INSERT INTO StepRunOutput (
				event_id,
				organization_id,
				task_id,
				step_run_id,
				task_lease_id,
				agent_id,
				name,
				value,
				sensitive,
				redacted
			)
			VALUES (
				@eventId,
				@organizationId,
				@taskId,
				@stepRunId,
				@taskLeaseId,
				@agentId,
				@name,
				@value::jsonb,
				@sensitive,
				@redacted
			)`,
			pgx.NamedArgs{
				"eventId":        eventID,
				"organizationId": request.OrganizationID,
				"taskId":         taskID,
				"stepRunId":      request.StepRunID,
				"taskLeaseId":    leaseID,
				"agentId":        request.AgentID,
				"name":           output.Name,
				"value":          output.ValueJSON,
				"sensitive":      output.Sensitive,
				"redacted":       output.Redacted,
			},
		)
		if err != nil {
			return mapStepEventWriteError("upsert output", err)
		}
	}
	return nil
}

func getStepRunEvent(ctx context.Context, eventID, orgID uuid.UUID) (*types.StepRunEvent, error) {
	db := internalctx.GetDb(ctx)
	event, err := scanStepRunEvent(db.QueryRow(ctx,
		`SELECT
			id,
			created_at,
			occurred_at,
			organization_id,
			task_id,
			step_run_id,
			task_lease_id,
			agent_id,
			sequence,
			event_type,
			message,
			progress_percent,
			details,
			redacted
		FROM StepRunEvent
		WHERE id = @id
			AND organization_id = @organizationId`,
		pgx.NamedArgs{"id": eventID, "organizationId": orgID},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not query StepRunEvent: %w", err)
	}
	logs, err := getStepRunEventLogs(ctx, event.ID, orgID)
	if err != nil {
		return nil, err
	}
	outputs, err := getStepRunEventOutputs(ctx, event.ID, orgID)
	if err != nil {
		return nil, err
	}
	event.Logs = logs
	event.Outputs = outputs
	return event, nil
}

func scanStepRunEvent(row pgx.Row) (*types.StepRunEvent, error) {
	var event types.StepRunEvent
	var details []byte
	if err := row.Scan(
		&event.ID,
		&event.CreatedAt,
		&event.OccurredAt,
		&event.OrganizationID,
		&event.TaskID,
		&event.StepRunID,
		&event.TaskLeaseID,
		&event.AgentID,
		&event.Sequence,
		&event.Type,
		&event.Message,
		&event.ProgressPercent,
		&details,
		&event.Redacted,
	); err != nil {
		return nil, err
	}
	if len(details) > 0 {
		if err := json.Unmarshal(details, &event.Details); err != nil {
			return nil, fmt.Errorf("could not decode StepRunEvent details: %w", err)
		}
	}
	return &event, nil
}

func getStepRunEventLogs(ctx context.Context, eventID, orgID uuid.UUID) ([]types.StepRunLogChunk, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			id,
			created_at,
			occurred_at,
			event_id,
			organization_id,
			task_id,
			step_run_id,
			task_lease_id,
			agent_id,
			chunk_index,
			stream,
			severity,
			body,
			redacted
		FROM StepRunLogChunk
		WHERE event_id = @eventId
			AND organization_id = @organizationId
		ORDER BY chunk_index, id`,
		pgx.NamedArgs{"eventId": eventID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query StepRunLogChunk by event: %w", err)
	}
	logs, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.StepRunLogChunk])
	if err != nil {
		return nil, fmt.Errorf("could not collect StepRunLogChunk by event: %w", err)
	}
	return logs, nil
}

func getStepRunEventOutputs(ctx context.Context, eventID, orgID uuid.UUID) ([]types.StepRunOutput, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			id,
			created_at,
			updated_at,
			event_id,
			organization_id,
			task_id,
			step_run_id,
			task_lease_id,
			agent_id,
			name,
			value,
			sensitive,
			redacted
		FROM StepRunOutput
		WHERE event_id = @eventId
			AND organization_id = @organizationId
		ORDER BY name, id`,
		pgx.NamedArgs{"eventId": eventID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query StepRunOutput by event: %w", err)
	}
	defer rows.Close()
	var outputs []types.StepRunOutput
	for rows.Next() {
		output, err := scanStepRunOutput(rows)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, output)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("could not collect StepRunOutput by event: %w", rows.Err())
	}
	return outputs, nil
}

func scanStepRunOutput(row pgx.Row) (types.StepRunOutput, error) {
	var output types.StepRunOutput
	var value []byte
	err := row.Scan(
		&output.ID,
		&output.CreatedAt,
		&output.UpdatedAt,
		&output.EventID,
		&output.OrganizationID,
		&output.TaskID,
		&output.StepRunID,
		&output.TaskLeaseID,
		&output.AgentID,
		&output.Name,
		&value,
		&output.Sensitive,
		&output.Redacted,
	)
	if err != nil {
		return types.StepRunOutput{}, fmt.Errorf("could not scan StepRunOutput: %w", err)
	}
	if len(value) > 0 {
		output.Value = append(output.Value[:0], value...)
	}
	return output, nil
}

func mapStepEventWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s StepRunEvent: %w", action, apierrors.ErrNotFound)
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s StepRunEvent: %w", action, apierrors.ErrConflict)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s StepRunEvent: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s StepRunEvent: %w", action, err)
}
