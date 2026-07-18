package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auditexport"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const controlPlaneAuditPayloadLimit = 32 * 1024

var (
	controlPlaneAuditEventTypePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
	controlPlaneAuditOutcomePattern   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	controlPlaneAuditChecksumPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const controlPlaneAuditEventColumns = `
	id,
	organization_id,
	sequence,
	event_type,
	actor_id,
	outcome,
	release_id,
	target_config_id,
	deployment_plan_id,
	approval_id,
	campaign_id,
	wave_id,
	execution_id,
	adapter_revision_id,
	observation_id,
	reconciliation_id,
	release_checksum,
	target_config_checksum,
	deployment_plan_checksum,
	approval_checksum,
	campaign_checksum,
	execution_checksum,
	observation_checksum,
	payload,
	payload_redacted,
	payload_truncated,
	created_at`

func AppendControlPlaneAuditEvent(
	ctx context.Context,
	input types.ControlPlaneAuditEventInput,
) (*types.ControlPlaneAuditEvent, error) {
	if err := validateControlPlaneAuditEventInput(input); err != nil {
		return nil, err
	}
	payload, payloadRedacted, payloadTruncated, err := auditexport.RedactAuditPayload(
		input.Payload,
		controlPlaneAuditPayloadLimit,
	)
	if err != nil {
		return nil, apierrors.NewBadRequest("audit payload is invalid: " + err.Error())
	}

	var event *types.ControlPlaneAuditEvent
	err = RunTx(ctx, func(ctx context.Context) error {
		database := internalctx.GetDb(ctx)
		if _, err := database.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtextextended(@organizationId::text, 160))`,
			pgx.NamedArgs{"organizationId": input.OrganizationID},
		); err != nil {
			return fmt.Errorf("could not lock control-plane audit sequence: %w", err)
		}
		rows, err := database.Query(ctx, `
			INSERT INTO ControlPlaneAuditEvent (
				organization_id,
				sequence,
				event_type,
				actor_id,
				outcome,
				release_id,
				target_config_id,
				deployment_plan_id,
				approval_id,
				campaign_id,
				wave_id,
				execution_id,
				adapter_revision_id,
				observation_id,
				reconciliation_id,
				release_checksum,
				target_config_checksum,
				deployment_plan_checksum,
				approval_checksum,
				campaign_checksum,
				execution_checksum,
				observation_checksum,
				payload,
				payload_redacted,
				payload_truncated
			)
			VALUES (
				@organizationId,
				(
					SELECT COALESCE(MAX(sequence), 0) + 1
					FROM ControlPlaneAuditEvent
					WHERE organization_id = @organizationId
				),
				@eventType,
				@actorId,
				@outcome,
				@releaseId,
				@targetConfigId,
				@deploymentPlanId,
				@approvalId,
				@campaignId,
				@waveId,
				@executionId,
				@adapterRevisionId,
				@observationId,
				@reconciliationId,
				@releaseChecksum,
				@targetConfigChecksum,
				@deploymentPlanChecksum,
				@approvalChecksum,
				@campaignChecksum,
				@executionChecksum,
				@observationChecksum,
				@payload,
				@payloadRedacted,
				@payloadTruncated
			)
			RETURNING `+controlPlaneAuditEventColumns,
			pgx.NamedArgs{
				"organizationId":         input.OrganizationID,
				"eventType":              strings.TrimSpace(input.EventType),
				"actorId":                input.ActorID,
				"outcome":                strings.TrimSpace(input.Outcome),
				"releaseId":              input.ReleaseID,
				"targetConfigId":         input.TargetConfigID,
				"deploymentPlanId":       input.DeploymentPlanID,
				"approvalId":             input.ApprovalID,
				"campaignId":             input.CampaignID,
				"waveId":                 input.WaveID,
				"executionId":            input.ExecutionID,
				"adapterRevisionId":      input.AdapterRevisionID,
				"observationId":          input.ObservationID,
				"reconciliationId":       input.ReconciliationID,
				"releaseChecksum":        input.ReleaseChecksum,
				"targetConfigChecksum":   input.TargetConfigChecksum,
				"deploymentPlanChecksum": input.DeploymentPlanChecksum,
				"approvalChecksum":       input.ApprovalChecksum,
				"campaignChecksum":       input.CampaignChecksum,
				"executionChecksum":      input.ExecutionChecksum,
				"observationChecksum":    input.ObservationChecksum,
				"payload":                nullableJSON(payload),
				"payloadRedacted":        payloadRedacted,
				"payloadTruncated":       payloadTruncated,
			},
		)
		if err != nil {
			return fmt.Errorf("could not append control-plane audit event: %w", err)
		}
		value, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ControlPlaneAuditEvent])
		if err != nil {
			return fmt.Errorf("could not scan control-plane audit event: %w", err)
		}
		event = &value
		return nil
	})
	return event, err
}

func GetControlPlaneAuditEvents(
	ctx context.Context,
	organizationID uuid.UUID,
	afterSequence int64,
	limit int,
) ([]types.ControlPlaneAuditEvent, error) {
	if organizationID == uuid.Nil {
		return nil, apierrors.NewForbidden("audit organization scope is required")
	}
	if afterSequence < 0 || limit < 1 || limit > 100 {
		return nil, apierrors.NewBadRequest("audit cursor or page size is invalid")
	}
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx, `
		SELECT `+controlPlaneAuditEventColumns+`
		FROM ControlPlaneAuditEvent
		WHERE organization_id = @organizationId
		  AND sequence > @afterSequence
		ORDER BY sequence, id
		LIMIT @limit`,
		pgx.NamedArgs{
			"organizationId": organizationID,
			"afterSequence":  afterSequence,
			"limit":          limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query control-plane audit events: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[types.ControlPlaneAuditEvent])
}

func BuildDeploymentEvidenceBundle(
	ctx context.Context,
	query types.EvidenceBundleQuery,
) (*types.EvidenceBundle, error) {
	if query.OrganizationID == uuid.Nil || query.DeploymentPlanID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organization and deployment plan are required")
	}
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx, `
		SELECT `+controlPlaneAuditEventColumns+`
		FROM ControlPlaneAuditEvent
		WHERE organization_id = @organizationId
		  AND deployment_plan_id = @deploymentPlanId
		ORDER BY sequence, id`,
		pgx.NamedArgs{
			"organizationId":   query.OrganizationID,
			"deploymentPlanId": query.DeploymentPlanID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query deployment evidence: %w", err)
	}
	events, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ControlPlaneAuditEvent])
	if err != nil {
		return nil, fmt.Errorf("could not collect deployment evidence: %w", err)
	}
	if len(events) == 0 {
		return nil, apierrors.ErrNotFound
	}
	return auditexport.BuildDeploymentEvidenceBundle(query, events)
}

func CreateAuditExportSink(
	ctx context.Context,
	input types.CreateAuditExportSinkInput,
) (*types.AuditExportSink, error) {
	if err := validateCreateAuditExportSinkInput(input); err != nil {
		return nil, err
	}
	var sink *types.AuditExportSink
	err := RunTx(ctx, func(ctx context.Context) error {
		database := internalctx.GetDb(ctx)
		rows, err := database.Query(ctx, `
			INSERT INTO AuditExportSink (
				organization_id,
				name,
				kind,
				endpoint_reference,
				config_checksum,
				enabled
			)
			VALUES (
				@organizationId,
				@name,
				@kind,
				@endpointReference,
				@configChecksum,
				@enabled
			)
			RETURNING
				id,
				organization_id,
				name,
				kind,
				endpoint_reference,
				config_checksum,
				enabled,
				last_success_at,
				last_failure_at,
				consecutive_failures,
				created_at,
				updated_at`,
			pgx.NamedArgs{
				"organizationId":    input.OrganizationID,
				"name":              strings.TrimSpace(input.Name),
				"kind":              input.Kind,
				"endpointReference": strings.TrimSpace(input.EndpointReference),
				"configChecksum":    input.ConfigChecksum,
				"enabled":           input.Enabled,
			},
		)
		if err != nil {
			return mapAuditExportWriteError("create sink", err)
		}
		value, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.AuditExportSink])
		if err != nil {
			return mapAuditExportWriteError("scan created sink", err)
		}
		sink = &value
		if _, err := database.Exec(ctx, `
			INSERT INTO AuditExportCheckpoint (sink_id, organization_id)
			VALUES (@sinkId, @organizationId)`,
			pgx.NamedArgs{"sinkId": sink.ID, "organizationId": sink.OrganizationID},
		); err != nil {
			return mapAuditExportWriteError("create checkpoint", err)
		}
		return nil
	})
	return sink, err
}

func GetAuditExportSinks(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.AuditExportSink, error) {
	if organizationID == uuid.Nil {
		return nil, apierrors.NewForbidden("audit export organization scope is required")
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
			id,
			organization_id,
			name,
			kind,
			endpoint_reference,
			config_checksum,
			enabled,
			last_success_at,
			last_failure_at,
			consecutive_failures,
			created_at,
			updated_at
		FROM AuditExportSink
		WHERE organization_id = @organizationId
		ORDER BY name, id`,
		pgx.NamedArgs{"organizationId": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query audit export sinks: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[types.AuditExportSink])
}

func GetAuditExportStatuses(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.AuditExportStatus, error) {
	if organizationID == uuid.Nil {
		return nil, apierrors.NewForbidden("audit export organization scope is required")
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
			s.id,
			s.organization_id,
			s.name,
			s.kind,
			s.endpoint_reference,
			s.config_checksum,
			s.enabled,
			s.last_success_at,
			s.last_failure_at,
			s.consecutive_failures,
			s.created_at,
			s.updated_at,
			COALESCE(c.last_sequence, 0),
			c.last_event_id,
			COALESCE(latest.sequence, 0),
			attempt.status,
			attempt.error_summary,
			attempt.completed_at
		FROM AuditExportSink s
		LEFT JOIN AuditExportCheckpoint c
		  ON c.sink_id = s.id
		 AND c.organization_id = s.organization_id
		LEFT JOIN LATERAL (
			SELECT MAX(e.sequence) AS sequence
			FROM ControlPlaneAuditEvent e
			WHERE e.organization_id = s.organization_id
		) latest ON true
		LEFT JOIN LATERAL (
			SELECT a.status, a.error_summary, a.completed_at
			FROM AuditExportAttempt a
			WHERE a.sink_id = s.id
			  AND a.organization_id = s.organization_id
			ORDER BY a.started_at DESC, a.id DESC
			LIMIT 1
		) attempt ON true
		WHERE s.organization_id = @organizationId
		ORDER BY s.name, s.id`,
		pgx.NamedArgs{"organizationId": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query audit export status: %w", err)
	}
	defer rows.Close()

	statuses := make([]types.AuditExportStatus, 0)
	for rows.Next() {
		var status types.AuditExportStatus
		var attemptStatus *string
		var attemptError *string
		if err := rows.Scan(
			&status.Sink.ID,
			&status.Sink.OrganizationID,
			&status.Sink.Name,
			&status.Sink.Kind,
			&status.Sink.EndpointReference,
			&status.Sink.ConfigChecksum,
			&status.Sink.Enabled,
			&status.Sink.LastSuccessAt,
			&status.Sink.LastFailureAt,
			&status.Sink.ConsecutiveFailures,
			&status.Sink.CreatedAt,
			&status.Sink.UpdatedAt,
			&status.LastExportedSequence,
			&status.LastExportedEventID,
			&status.LatestSequence,
			&attemptStatus,
			&attemptError,
			&status.LastAttemptCompletedAt,
		); err != nil {
			return nil, fmt.Errorf("could not scan audit export status: %w", err)
		}
		status.CheckpointLag = max(status.LatestSequence-status.LastExportedSequence, 0)
		status.Alert = status.Sink.Enabled &&
			(status.CheckpointLag > 0 || status.Sink.ConsecutiveFailures > 0)
		if attemptStatus != nil {
			status.LastAttemptStatus = *attemptStatus
		}
		if attemptError != nil {
			status.LastAttemptError = *attemptError
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not iterate audit export status: %w", err)
	}
	return statuses, nil
}

type ControlPlaneAuditExportStore struct{}

func (ControlPlaneAuditExportStore) LoadAuditBatch(
	ctx context.Context,
	sinkID uuid.UUID,
	limit int,
) ([]types.ControlPlaneAuditEvent, error) {
	if sinkID == uuid.Nil || limit < 1 || limit > 1000 {
		return nil, apierrors.NewBadRequest("audit export sink or batch size is invalid")
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+controlPlaneAuditEventColumns+`
		FROM ControlPlaneAuditEvent
		WHERE organization_id = (
			SELECT organization_id
			FROM AuditExportSink
			WHERE id = @sinkId
			  AND enabled
		)
		  AND sequence > COALESCE((
			SELECT last_sequence
			FROM AuditExportCheckpoint
			WHERE sink_id = @sinkId
		  ), 0)
		ORDER BY sequence, id
		LIMIT @limit`,
		pgx.NamedArgs{"sinkId": sinkID, "limit": limit},
	)
	if err != nil {
		return nil, fmt.Errorf("could not load audit export batch: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[types.ControlPlaneAuditEvent])
}

func (ControlPlaneAuditExportStore) CommitAuditExport(
	ctx context.Context,
	sinkID uuid.UUID,
	lastSequence int64,
	eventCount int,
) error {
	if sinkID == uuid.Nil || lastSequence < 1 || eventCount < 1 {
		return apierrors.NewBadRequest("audit export checkpoint is invalid")
	}
	return RunTx(ctx, func(ctx context.Context) error {
		database := internalctx.GetDb(ctx)
		var organizationID uuid.UUID
		var checkpoint int64
		err := database.QueryRow(ctx, `
			SELECT s.organization_id, c.last_sequence
			FROM AuditExportSink s
			JOIN AuditExportCheckpoint c
			  ON c.sink_id = s.id
			 AND c.organization_id = s.organization_id
			WHERE s.id = @sinkId
			  AND s.enabled
			FOR UPDATE OF s, c`,
			pgx.NamedArgs{"sinkId": sinkID},
		).Scan(&organizationID, &checkpoint)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("could not lock audit export checkpoint: %w", err)
		}
		if checkpoint >= lastSequence {
			return nil
		}
		if lastSequence-checkpoint != int64(eventCount) {
			return apierrors.NewConflict("audit export checkpoint changed or batch is not contiguous")
		}

		var lastEventID uuid.UUID
		err = database.QueryRow(ctx, `
			SELECT id
			FROM ControlPlaneAuditEvent
			WHERE organization_id = @organizationId
			  AND sequence = @lastSequence`,
			pgx.NamedArgs{"organizationId": organizationID, "lastSequence": lastSequence},
		).Scan(&lastEventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.NewConflict("audit export checkpoint event is missing")
		}
		if err != nil {
			return fmt.Errorf("could not load audit export checkpoint event: %w", err)
		}

		firstSequence := checkpoint + 1
		if _, err := database.Exec(ctx, `
			INSERT INTO AuditExportAttempt (
				sink_id,
				organization_id,
				first_sequence,
				last_sequence,
				event_count,
				status,
				idempotency_key,
				completed_at
			)
			VALUES (
				@sinkId,
				@organizationId,
				@firstSequence,
				@lastSequence,
				@eventCount,
				'SUCCEEDED',
				@idempotencyKey,
				now()
			)
			ON CONFLICT (sink_id, idempotency_key) DO NOTHING`,
			pgx.NamedArgs{
				"sinkId":         sinkID,
				"organizationId": organizationID,
				"firstSequence":  firstSequence,
				"lastSequence":   lastSequence,
				"eventCount":     eventCount,
				"idempotencyKey": auditExportAttemptKey(
					sinkID, firstSequence, lastSequence, eventCount, "SUCCEEDED",
				),
			},
		); err != nil {
			return fmt.Errorf("could not record successful audit export: %w", err)
		}
		if _, err := database.Exec(ctx, `
			UPDATE AuditExportCheckpoint
			SET last_sequence = @lastSequence,
				last_event_id = @lastEventId,
				updated_at = now()
			WHERE sink_id = @sinkId
			  AND organization_id = @organizationId`,
			pgx.NamedArgs{
				"sinkId":         sinkID,
				"organizationId": organizationID,
				"lastSequence":   lastSequence,
				"lastEventId":    lastEventID,
			},
		); err != nil {
			return fmt.Errorf("could not advance audit export checkpoint: %w", err)
		}
		if _, err := database.Exec(ctx, `
			UPDATE AuditExportSink
			SET last_success_at = now(),
				consecutive_failures = 0,
				updated_at = now()
			WHERE id = @sinkId
			  AND organization_id = @organizationId`,
			pgx.NamedArgs{"sinkId": sinkID, "organizationId": organizationID},
		); err != nil {
			return fmt.Errorf("could not update successful audit export sink: %w", err)
		}
		return nil
	})
}

func (ControlPlaneAuditExportStore) RecordAuditExportFailure(
	ctx context.Context,
	sinkID uuid.UUID,
	failedSequence int64,
	exportErr error,
) error {
	if sinkID == uuid.Nil || failedSequence < 1 || exportErr == nil {
		return apierrors.NewBadRequest("audit export failure is invalid")
	}
	return RunTx(ctx, func(ctx context.Context) error {
		database := internalctx.GetDb(ctx)
		var organizationID uuid.UUID
		var checkpoint int64
		err := database.QueryRow(ctx, `
			SELECT s.organization_id, c.last_sequence
			FROM AuditExportSink s
			JOIN AuditExportCheckpoint c
			  ON c.sink_id = s.id
			 AND c.organization_id = s.organization_id
			WHERE s.id = @sinkId
			FOR UPDATE OF s, c`,
			pgx.NamedArgs{"sinkId": sinkID},
		).Scan(&organizationID, &checkpoint)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("could not lock failed audit export: %w", err)
		}
		if failedSequence <= checkpoint {
			return nil
		}

		firstSequence := checkpoint + 1
		eventCount := int(failedSequence - checkpoint)
		errorSummary := auditExportErrorSummary(exportErr)
		if _, err := database.Exec(ctx, `
			INSERT INTO AuditExportAttempt (
				sink_id,
				organization_id,
				first_sequence,
				last_sequence,
				event_count,
				status,
				idempotency_key,
				error_summary,
				completed_at
			)
			VALUES (
				@sinkId,
				@organizationId,
				@firstSequence,
				@failedSequence,
				@eventCount,
				'FAILED',
				@idempotencyKey,
				@errorSummary,
				now()
			)
			ON CONFLICT (sink_id, idempotency_key) DO UPDATE
			SET error_summary = EXCLUDED.error_summary,
				completed_at = EXCLUDED.completed_at`,
			pgx.NamedArgs{
				"sinkId":         sinkID,
				"organizationId": organizationID,
				"firstSequence":  firstSequence,
				"failedSequence": failedSequence,
				"eventCount":     eventCount,
				"idempotencyKey": auditExportAttemptKey(
					sinkID, firstSequence, failedSequence, eventCount, "FAILED",
				),
				"errorSummary": errorSummary,
			},
		); err != nil {
			return fmt.Errorf("could not record failed audit export: %w", err)
		}
		if _, err := database.Exec(ctx, `
			UPDATE AuditExportSink
			SET last_failure_at = now(),
				consecutive_failures = consecutive_failures + 1,
				updated_at = now()
			WHERE id = @sinkId
			  AND organization_id = @organizationId`,
			pgx.NamedArgs{"sinkId": sinkID, "organizationId": organizationID},
		); err != nil {
			return fmt.Errorf("could not update failed audit export sink: %w", err)
		}
		return nil
	})
}

func (ControlPlaneAuditExportStore) AuditExportLag(
	ctx context.Context,
	sinkID uuid.UUID,
) (int64, error) {
	if sinkID == uuid.Nil {
		return 0, apierrors.NewBadRequest("audit export sink is required")
	}
	var lag int64
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT GREATEST(
			COALESCE((
				SELECT MAX(e.sequence)
				FROM ControlPlaneAuditEvent e
				WHERE e.organization_id = s.organization_id
			), 0) - c.last_sequence,
			0
		)
		FROM AuditExportSink s
		JOIN AuditExportCheckpoint c
		  ON c.sink_id = s.id
		 AND c.organization_id = s.organization_id
		WHERE s.id = @sinkId`,
		pgx.NamedArgs{"sinkId": sinkID},
	).Scan(&lag)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, apierrors.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("could not query audit export lag: %w", err)
	}
	return lag, nil
}

func NewControlPlaneAuditExportWorker(resolveSink auditexport.SinkResolver) *auditexport.Worker {
	return auditexport.NewWorker(ControlPlaneAuditExportStore{}, resolveSink)
}

func validateControlPlaneAuditEventInput(input types.ControlPlaneAuditEventInput) error {
	switch {
	case input.OrganizationID == uuid.Nil:
		return apierrors.NewBadRequest("audit organization is required")
	case strings.TrimSpace(input.EventType) == "":
		return apierrors.NewBadRequest("audit event type is required")
	case len(strings.TrimSpace(input.EventType)) > 128:
		return apierrors.NewBadRequest("audit event type is too long")
	case !controlPlaneAuditEventTypePattern.MatchString(strings.TrimSpace(input.EventType)):
		return apierrors.NewBadRequest("audit event type is invalid")
	case strings.TrimSpace(input.Outcome) == "":
		return apierrors.NewBadRequest("audit outcome is required")
	case len(strings.TrimSpace(input.Outcome)) > 64:
		return apierrors.NewBadRequest("audit outcome is too long")
	case !controlPlaneAuditOutcomePattern.MatchString(strings.TrimSpace(input.Outcome)):
		return apierrors.NewBadRequest("audit outcome is invalid")
	case len(input.Payload) > 0 && !json.Valid(input.Payload):
		return apierrors.NewBadRequest("audit payload must be valid JSON")
	case !controlPlaneAuditInputHasCorrelation(input):
		return apierrors.NewBadRequest("audit correlation is required")
	case !controlPlaneAuditChecksumsValid(input):
		return apierrors.NewBadRequest("audit checksum must be a canonical SHA-256 checksum")
	default:
		return nil
	}
}

func controlPlaneAuditInputHasCorrelation(input types.ControlPlaneAuditEventInput) bool {
	return input.ReleaseID != nil ||
		input.TargetConfigID != nil ||
		input.DeploymentPlanID != nil ||
		input.ApprovalID != nil ||
		input.CampaignID != nil ||
		input.WaveID != nil ||
		input.ExecutionID != nil ||
		input.AdapterRevisionID != nil ||
		input.ObservationID != nil ||
		input.ReconciliationID != nil
}

func controlPlaneAuditChecksumsValid(input types.ControlPlaneAuditEventInput) bool {
	for _, checksum := range []string{
		input.ReleaseChecksum,
		input.TargetConfigChecksum,
		input.DeploymentPlanChecksum,
		input.ApprovalChecksum,
		input.CampaignChecksum,
		input.ExecutionChecksum,
		input.ObservationChecksum,
	} {
		if checksum != "" && !controlPlaneAuditChecksumPattern.MatchString(checksum) {
			return false
		}
	}
	return true
}

func validateCreateAuditExportSinkInput(input types.CreateAuditExportSinkInput) error {
	name := strings.TrimSpace(input.Name)
	reference := strings.TrimSpace(input.EndpointReference)
	switch {
	case input.OrganizationID == uuid.Nil:
		return apierrors.NewBadRequest("audit export organization is required")
	case len(name) < 1 || len(name) > 128:
		return apierrors.NewBadRequest("audit export sink name is invalid")
	case !input.Kind.Valid():
		return apierrors.NewBadRequest("audit export sink kind is invalid")
	case len(reference) < 1 || len(reference) > 1024 || strings.ContainsAny(reference, "\r\n"):
		return apierrors.NewBadRequest("audit export endpoint reference is invalid")
	case !controlPlaneAuditChecksumPattern.MatchString(input.ConfigChecksum):
		return apierrors.NewBadRequest("audit export config checksum is invalid")
	default:
		return nil
	}
}

func auditExportAttemptKey(
	sinkID uuid.UUID,
	firstSequence int64,
	lastSequence int64,
	eventCount int,
	status string,
) string {
	value := fmt.Sprintf("%s:%d:%d:%d:%s", sinkID, firstSequence, lastSequence, eventCount, status)
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum)
}

func auditExportErrorSummary(err error) string {
	value := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(err.Error()))
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"authorization:",
		"bearer ",
		"password=",
		"token=",
		"api_key=",
		"apikey=",
		"secret=",
	} {
		if strings.Contains(lower, marker) {
			return "[REDACTED]"
		}
	}
	if len(value) > 2048 {
		value = value[:2048]
	}
	return value
}

func mapAuditExportWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s AuditExport: %w", action, apierrors.ErrAlreadyExists)
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s AuditExport: %w", action, apierrors.ErrNotFound)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s AuditExport: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s AuditExport: %w", action, err)
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
