package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

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

const auditExportAttemptLeaseSeconds = 15 * 60

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
	component_release_id,
	product_release_id,
	target_config_id,
	deployment_plan_id,
	deployment_policy_id,
	deployment_policy_version_id,
	approval_id,
	maintenance_calendar_id,
	deployment_freeze_id,
	admission_decision_id,
	emergency_override_id,
	campaign_draft_id,
	campaign_revision_id,
	campaign_run_id,
	campaign_wave_definition_id,
	campaign_wave_run_id,
	campaign_member_id,
	campaign_member_run_id,
	campaign_control_request_id,
	campaign_exclusion_id,
	campaign_prerequisite_evaluation_id,
	campaign_threshold_evaluation_id,
	execution_id,
	execution_attempt_id,
	adapter_revision_id,
	desired_state_id,
	observation_id,
	drift_case_id,
	reconciliation_id,
	deployment_target_id,
	environment_id,
	customer_organization_id,
	deployment_unit_id,
	component_id,
	task_id,
	step_run_id,
	audit_export_sink_id,
	audit_export_attempt_id,
	release_checksum,
	component_release_checksum,
	product_release_checksum,
	artifact_digest,
	manifest_digest,
	target_config_checksum,
	deployment_plan_checksum,
	policy_checksum,
	approval_checksum,
	calendar_checksum,
	admission_checksum,
	campaign_revision_checksum,
	campaign_control_checksum,
	execution_checksum,
	desired_state_checksum,
	observation_checksum,
	drift_checksum,
	reconciliation_checksum,
	audit_export_config_checksum,
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
	var event *types.ControlPlaneAuditEvent
	err := RunTx(ctx, func(ctx context.Context) error {
		var err error
		event, err = AppendControlPlaneAuditEventInCurrentBoundary(ctx, input)
		return err
	})
	return event, err
}

func AppendControlPlaneAuditEventInCurrentBoundary(
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

	database := internalctx.GetDb(ctx)
	if _, err := database.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended(@organizationId::text, 160))`,
		pgx.NamedArgs{"organizationId": input.OrganizationID},
	); err != nil {
		return nil, fmt.Errorf("could not lock control-plane audit sequence: %w", err)
	}
	rows, err := database.Query(ctx, `
			INSERT INTO ControlPlaneAuditEvent (
				organization_id,
				sequence,
				event_type,
				actor_id,
				outcome,
				release_id,
				component_release_id,
				product_release_id,
				target_config_id,
				deployment_plan_id,
				deployment_policy_id,
				deployment_policy_version_id,
				approval_id,
				maintenance_calendar_id,
				deployment_freeze_id,
				admission_decision_id,
				emergency_override_id,
				campaign_draft_id,
				campaign_revision_id,
				campaign_run_id,
				campaign_wave_definition_id,
				campaign_wave_run_id,
				campaign_member_id,
				campaign_member_run_id,
				campaign_control_request_id,
				campaign_exclusion_id,
				campaign_prerequisite_evaluation_id,
				campaign_threshold_evaluation_id,
				execution_id,
				execution_attempt_id,
				adapter_revision_id,
				desired_state_id,
				observation_id,
				drift_case_id,
				reconciliation_id,
				deployment_target_id,
				environment_id,
				customer_organization_id,
				deployment_unit_id,
				component_id,
				task_id,
				step_run_id,
				audit_export_sink_id,
				audit_export_attempt_id,
				release_checksum,
				component_release_checksum,
				product_release_checksum,
				artifact_digest,
				manifest_digest,
				target_config_checksum,
				deployment_plan_checksum,
				policy_checksum,
				approval_checksum,
				calendar_checksum,
				admission_checksum,
				campaign_revision_checksum,
				campaign_control_checksum,
				execution_checksum,
				desired_state_checksum,
				observation_checksum,
				drift_checksum,
				reconciliation_checksum,
				audit_export_config_checksum,
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
				@componentReleaseId,
				@productReleaseId,
				@targetConfigId,
				@deploymentPlanId,
				@deploymentPolicyId,
				@deploymentPolicyVersionId,
				@approvalId,
				@maintenanceCalendarId,
				@deploymentFreezeId,
				@admissionDecisionId,
				@emergencyOverrideId,
				@campaignDraftId,
				@campaignRevisionId,
				@campaignRunId,
				@campaignWaveDefinitionId,
				@campaignWaveRunId,
				@campaignMemberId,
				@campaignMemberRunId,
				@campaignControlRequestId,
				@campaignExclusionId,
				@campaignPrerequisiteEvaluationId,
				@campaignThresholdEvaluationId,
				@executionId,
				@executionAttemptId,
				@adapterRevisionId,
				@desiredStateId,
				@observationId,
				@driftCaseId,
				@reconciliationId,
				@deploymentTargetId,
				@environmentId,
				@customerOrganizationId,
				@deploymentUnitId,
				@componentId,
				@taskId,
				@stepRunId,
				@auditExportSinkId,
				@auditExportAttemptId,
				@releaseChecksum,
				@componentReleaseChecksum,
				@productReleaseChecksum,
				@artifactDigest,
				@manifestDigest,
				@targetConfigChecksum,
				@deploymentPlanChecksum,
				@policyChecksum,
				@approvalChecksum,
				@calendarChecksum,
				@admissionChecksum,
				@campaignRevisionChecksum,
				@campaignControlChecksum,
				@executionChecksum,
				@desiredStateChecksum,
				@observationChecksum,
				@driftChecksum,
				@reconciliationChecksum,
				@auditExportConfigChecksum,
				@payload,
				@payloadRedacted,
				@payloadTruncated
			)
			ON CONFLICT (organization_id, event_type, execution_attempt_id)
			WHERE execution_attempt_id IS NOT NULL
			  AND event_type IN (
			    'campaign.execution.running',
			    'campaign.execution.terminal',
			    'campaign.execution.uncertain',
			    'campaign.execution.reconciled'
			  )
			DO NOTHING
			RETURNING `+controlPlaneAuditEventColumns,
		pgx.NamedArgs{
			"organizationId":                   input.OrganizationID,
			"eventType":                        strings.TrimSpace(input.EventType),
			"actorId":                          input.ActorID,
			"outcome":                          strings.TrimSpace(input.Outcome),
			"releaseId":                        input.ReleaseID,
			"componentReleaseId":               input.ComponentReleaseID,
			"productReleaseId":                 input.ProductReleaseID,
			"targetConfigId":                   input.TargetConfigID,
			"deploymentPlanId":                 input.DeploymentPlanID,
			"deploymentPolicyId":               input.DeploymentPolicyID,
			"deploymentPolicyVersionId":        input.DeploymentPolicyVersionID,
			"approvalId":                       input.ApprovalID,
			"maintenanceCalendarId":            input.MaintenanceCalendarID,
			"deploymentFreezeId":               input.DeploymentFreezeID,
			"admissionDecisionId":              input.AdmissionDecisionID,
			"emergencyOverrideId":              input.EmergencyOverrideID,
			"campaignDraftId":                  input.CampaignDraftID,
			"campaignRevisionId":               input.CampaignRevisionID,
			"campaignRunId":                    input.CampaignRunID,
			"campaignWaveDefinitionId":         input.CampaignWaveDefinitionID,
			"campaignWaveRunId":                input.CampaignWaveRunID,
			"campaignMemberId":                 input.CampaignMemberID,
			"campaignMemberRunId":              input.CampaignMemberRunID,
			"campaignControlRequestId":         input.CampaignControlRequestID,
			"campaignExclusionId":              input.CampaignExclusionID,
			"campaignPrerequisiteEvaluationId": input.CampaignPrerequisiteEvaluationID,
			"campaignThresholdEvaluationId":    input.CampaignThresholdEvaluationID,
			"executionId":                      input.ExecutionID,
			"executionAttemptId":               input.ExecutionAttemptID,
			"adapterRevisionId":                input.AdapterRevisionID,
			"desiredStateId":                   input.DesiredStateID,
			"observationId":                    input.ObservationID,
			"driftCaseId":                      input.DriftCaseID,
			"reconciliationId":                 input.ReconciliationID,
			"deploymentTargetId":               input.DeploymentTargetID,
			"environmentId":                    input.EnvironmentID,
			"customerOrganizationId":           input.CustomerOrganizationID,
			"deploymentUnitId":                 input.DeploymentUnitID,
			"componentId":                      input.ComponentID,
			"taskId":                           input.TaskID,
			"stepRunId":                        input.StepRunID,
			"auditExportSinkId":                input.AuditExportSinkID,
			"auditExportAttemptId":             input.AuditExportAttemptID,
			"releaseChecksum":                  input.ReleaseChecksum,
			"componentReleaseChecksum":         input.ComponentReleaseChecksum,
			"productReleaseChecksum":           input.ProductReleaseChecksum,
			"artifactDigest":                   input.ArtifactDigest,
			"manifestDigest":                   input.ManifestDigest,
			"targetConfigChecksum":             input.TargetConfigChecksum,
			"deploymentPlanChecksum":           input.DeploymentPlanChecksum,
			"policyChecksum":                   input.PolicyChecksum,
			"approvalChecksum":                 input.ApprovalChecksum,
			"calendarChecksum":                 input.CalendarChecksum,
			"admissionChecksum":                input.AdmissionChecksum,
			"campaignRevisionChecksum":         input.CampaignRevisionChecksum,
			"campaignControlChecksum":          input.CampaignControlChecksum,
			"executionChecksum":                input.ExecutionChecksum,
			"desiredStateChecksum":             input.DesiredStateChecksum,
			"observationChecksum":              input.ObservationChecksum,
			"driftChecksum":                    input.DriftChecksum,
			"reconciliationChecksum":           input.ReconciliationChecksum,
			"auditExportConfigChecksum":        input.AuditExportConfigChecksum,
			"payload":                          nullableJSON(payload),
			"payloadRedacted":                  payloadRedacted,
			"payloadTruncated":                 payloadTruncated,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not append control-plane audit event: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ControlPlaneAuditEvent])
	if errors.Is(err, pgx.ErrNoRows) && input.ExecutionAttemptID != nil {
		return getControlPlaneAuditAttemptEvent(
			ctx, input.OrganizationID, strings.TrimSpace(input.EventType), *input.ExecutionAttemptID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("could not scan control-plane audit event: %w", err)
	}
	if err := claimControlPlaneAuditCorrelations(ctx, value, input.Correlations()); err != nil {
		return nil, err
	}
	return &value, nil
}

func getControlPlaneAuditAttemptEvent(
	ctx context.Context,
	organizationID uuid.UUID,
	eventType string,
	executionAttemptID uuid.UUID,
) (*types.ControlPlaneAuditEvent, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+controlPlaneAuditEventColumns+`
		FROM ControlPlaneAuditEvent
		WHERE organization_id = @organizationId
		  AND event_type = @eventType
		  AND execution_attempt_id = @executionAttemptId`, pgx.NamedArgs{
		"organizationId": organizationID, "eventType": eventType,
		"executionAttemptId": executionAttemptID,
	})
	if err != nil {
		return nil, fmt.Errorf("could not query replayed control-plane audit event: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ControlPlaneAuditEvent])
	if err != nil {
		return nil, fmt.Errorf("could not scan replayed control-plane audit event: %w", err)
	}
	return &value, nil
}

func claimControlPlaneAuditCorrelations(
	ctx context.Context,
	event types.ControlPlaneAuditEvent,
	correlations []types.AuditCorrelation,
) error {
	if len(correlations) == 0 {
		return apierrors.NewBadRequest("audit correlation is required")
	}
	kinds := make([]string, len(correlations))
	ids := make([]uuid.UUID, len(correlations))
	for i, correlation := range correlations {
		kinds[i] = string(correlation.Kind)
		ids[i] = correlation.ID
	}
	database := internalctx.GetDb(ctx)
	args := pgx.NamedArgs{
		"eventId":        event.ID,
		"organizationId": event.OrganizationID,
		"kinds":          kinds,
		"ids":            ids,
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO ControlPlaneAuditSubject (
			correlation_kind,
			subject_id,
			organization_id,
			first_event_id
		)
		SELECT correlation.kind, correlation.id, @organizationId, @eventId
		FROM unnest(@kinds::text[], @ids::uuid[]) AS correlation(kind, id)
		ON CONFLICT (correlation_kind, subject_id) DO NOTHING`,
		args,
	); err != nil {
		return fmt.Errorf("could not claim control-plane audit correlation: %w", err)
	}
	var foreignCount int
	if err := database.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM unnest(@kinds::text[], @ids::uuid[]) AS correlation(kind, id)
		JOIN ControlPlaneAuditSubject subject
		  ON subject.correlation_kind = correlation.kind
		 AND subject.subject_id = correlation.id
		WHERE subject.organization_id <> @organizationId`,
		args,
	).Scan(&foreignCount); err != nil {
		return fmt.Errorf("could not validate control-plane audit tenant correlation: %w", err)
	}
	if foreignCount > 0 {
		return apierrors.NewForbidden("audit correlation belongs to another organization")
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO ControlPlaneAuditEventSubject (
			event_id,
			organization_id,
			correlation_kind,
			subject_id
		)
		SELECT @eventId, @organizationId, correlation.kind, correlation.id
		FROM unnest(@kinds::text[], @ids::uuid[]) AS correlation(kind, id)`,
		args,
	); err != nil {
		return fmt.Errorf("could not link control-plane audit correlation: %w", err)
	}
	return nil
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
		WITH RECURSIVE evidence_event(event_id) AS (
			SELECT id
			FROM ControlPlaneAuditEvent
			WHERE organization_id = @organizationId
			  AND deployment_plan_id = @deploymentPlanId
			UNION
			SELECT linked.event_id
			FROM evidence_event current_event
			JOIN ControlPlaneAuditEventSubject current_subject
			  ON current_subject.event_id = current_event.event_id
			 AND current_subject.organization_id = @organizationId
			JOIN ControlPlaneAuditEventSubject linked
			  ON linked.organization_id = current_subject.organization_id
			 AND linked.correlation_kind = current_subject.correlation_kind
			 AND linked.subject_id = current_subject.subject_id
			JOIN ControlPlaneAuditEvent candidate
			  ON candidate.id = linked.event_id
			 AND candidate.organization_id = linked.organization_id
			WHERE candidate.deployment_plan_id IS NULL
			   OR candidate.deployment_plan_id = @deploymentPlanId
		)
		SELECT `+controlPlaneAuditEventColumns+`
		FROM ControlPlaneAuditEvent
		WHERE organization_id = @organizationId
		  AND id IN (SELECT event_id FROM evidence_event)
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
		return RecordControlPlaneAuditMutation(
			ctx,
			DirectControlPlaneAuditAppendHook(),
			auditExportSinkCreatedEvent(input, *sink),
		)
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

func (ControlPlaneAuditExportStore) StartAuditExportAttempt(
	ctx context.Context,
	sinkID uuid.UUID,
	events []types.ControlPlaneAuditEvent,
) (uuid.UUID, error) {
	if sinkID == uuid.Nil || len(events) == 0 {
		return uuid.Nil, apierrors.NewBadRequest("audit export attempt requires a sink and events")
	}
	var attemptID uuid.UUID
	err := RunTx(ctx, func(ctx context.Context) error {
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
			return fmt.Errorf("could not lock audit export attempt: %w", err)
		}
		for i, event := range events {
			if event.OrganizationID != organizationID || event.Sequence != checkpoint+int64(i)+1 {
				return apierrors.NewConflict("audit export batch is cross-tenant or non-contiguous")
			}
		}
		staleAttempts, err := database.Exec(ctx, `
			UPDATE AuditExportAttempt
			SET status = 'FAILED',
				error_summary = 'audit export attempt lease expired',
				completed_at = now()
			WHERE sink_id = @sinkId
			  AND organization_id = @organizationId
			  AND status = 'RUNNING'
			  AND lease_expires_at <= now()`,
			pgx.NamedArgs{"sinkId": sinkID, "organizationId": organizationID},
		)
		if err != nil {
			return fmt.Errorf("could not reconcile stale audit export attempt: %w", err)
		}
		if staleAttempts.RowsAffected() > 0 {
			if _, err := database.Exec(ctx, `
				UPDATE AuditExportSink
				SET last_failure_at = now(),
					consecutive_failures = consecutive_failures + @failureCount,
					updated_at = now()
				WHERE id = @sinkId
				  AND organization_id = @organizationId`,
				pgx.NamedArgs{
					"sinkId":         sinkID,
					"organizationId": organizationID,
					"failureCount":   staleAttempts.RowsAffected(),
				},
			); err != nil {
				return fmt.Errorf("could not record stale audit export failure: %w", err)
			}
		}
		firstSequence := events[0].Sequence
		lastSequence := events[len(events)-1].Sequence
		var running bool
		var priorAttempts int
		err = database.QueryRow(ctx, `
			SELECT
				EXISTS (
					SELECT 1
					FROM AuditExportAttempt
					WHERE sink_id = @sinkId
					  AND organization_id = @organizationId
					  AND status = 'RUNNING'
				),
				COUNT(*)
			FROM AuditExportAttempt
			WHERE sink_id = @sinkId
			  AND organization_id = @organizationId
			  AND first_sequence = @firstSequence
			  AND last_sequence = @lastSequence`,
			pgx.NamedArgs{
				"sinkId":         sinkID,
				"organizationId": organizationID,
				"firstSequence":  firstSequence,
				"lastSequence":   lastSequence,
			},
		).Scan(&running, &priorAttempts)
		if err != nil {
			return fmt.Errorf("could not inspect audit export retry history: %w", err)
		}
		if running {
			return apierrors.NewConflict("audit export batch already has a running attempt")
		}
		attemptID = uuid.New()
		if _, err := database.Exec(ctx, `
			INSERT INTO AuditExportAttempt (
				id,
				sink_id,
				organization_id,
				first_sequence,
				last_sequence,
				event_count,
				status,
				idempotency_key,
				lease_expires_at
			)
			VALUES (
				@attemptId,
				@sinkId,
				@organizationId,
				@firstSequence,
				@lastSequence,
				@eventCount,
				'RUNNING',
				@idempotencyKey,
				now() + make_interval(secs => @leaseSeconds)
			)`,
			pgx.NamedArgs{
				"attemptId":      attemptID,
				"sinkId":         sinkID,
				"organizationId": organizationID,
				"firstSequence":  firstSequence,
				"lastSequence":   lastSequence,
				"eventCount":     len(events),
				"leaseSeconds":   auditExportAttemptLeaseSeconds,
				"idempotencyKey": auditExportAttemptKey(
					sinkID, firstSequence, lastSequence, len(events),
					fmt.Sprintf("ATTEMPT:%d", priorAttempts+1),
				),
			},
		); err != nil {
			return mapAuditExportWriteError("start attempt", err)
		}
		return nil
	})
	return attemptID, err
}

func (ControlPlaneAuditExportStore) CommitAuditExport(
	ctx context.Context,
	sinkID uuid.UUID,
	attemptID uuid.UUID,
	lastSequence int64,
	eventCount int,
) error {
	if sinkID == uuid.Nil || attemptID == uuid.Nil || lastSequence < 1 || eventCount < 1 {
		return apierrors.NewBadRequest("audit export checkpoint is invalid")
	}
	return RunTx(ctx, func(ctx context.Context) error {
		database := internalctx.GetDb(ctx)
		var organizationID uuid.UUID
		var checkpoint int64
		var attemptFirstSequence int64
		var attemptLastSequence int64
		var attemptEventCount int
		err := database.QueryRow(ctx, `
			SELECT
				s.organization_id,
				c.last_sequence,
				a.first_sequence,
				a.last_sequence,
				a.event_count
			FROM AuditExportSink s
			JOIN AuditExportCheckpoint c
			  ON c.sink_id = s.id
			 AND c.organization_id = s.organization_id
			JOIN AuditExportAttempt a
			  ON a.id = @attemptId
			 AND a.sink_id = s.id
			 AND a.organization_id = s.organization_id
			 AND a.status = 'RUNNING'
			 AND a.lease_expires_at > now()
			WHERE s.id = @sinkId
			  AND s.enabled
			FOR UPDATE OF s, c, a`,
			pgx.NamedArgs{"sinkId": sinkID, "attemptId": attemptID},
		).Scan(
			&organizationID,
			&checkpoint,
			&attemptFirstSequence,
			&attemptLastSequence,
			&attemptEventCount,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("could not lock audit export checkpoint: %w", err)
		}
		if attemptFirstSequence != checkpoint+1 ||
			attemptLastSequence != lastSequence ||
			attemptEventCount != eventCount {
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

		if _, err := database.Exec(ctx, `
			UPDATE AuditExportAttempt
			SET status = 'SUCCEEDED',
				completed_at = now()
			WHERE id = @attemptId
			  AND sink_id = @sinkId
			  AND organization_id = @organizationId
			  AND status = 'RUNNING'`,
			pgx.NamedArgs{
				"attemptId":      attemptID,
				"sinkId":         sinkID,
				"organizationId": organizationID,
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
	attemptID uuid.UUID,
	failedSequence int64,
	exportErr error,
) error {
	if sinkID == uuid.Nil || attemptID == uuid.Nil || failedSequence < 1 || exportErr == nil {
		return apierrors.NewBadRequest("audit export failure is invalid")
	}
	return RunTx(ctx, func(ctx context.Context) error {
		database := internalctx.GetDb(ctx)
		var organizationID uuid.UUID
		var firstSequence int64
		var lastSequence int64
		var status string
		err := database.QueryRow(ctx, `
			SELECT s.organization_id, a.first_sequence, a.last_sequence, a.status
			FROM AuditExportSink s
			JOIN AuditExportAttempt a
			  ON a.id = @attemptId
			 AND a.sink_id = s.id
			 AND a.organization_id = s.organization_id
			WHERE s.id = @sinkId
			FOR UPDATE OF s, a`,
			pgx.NamedArgs{"sinkId": sinkID, "attemptId": attemptID},
		).Scan(&organizationID, &firstSequence, &lastSequence, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("could not lock failed audit export: %w", err)
		}
		if status == "FAILED" {
			return nil
		}
		if status != "RUNNING" || failedSequence < firstSequence || failedSequence > lastSequence {
			return apierrors.NewConflict("audit export attempt is not running or failure is outside its batch")
		}
		errorSummary := auditExportErrorSummary(exportErr)
		if _, err := database.Exec(ctx, `
			UPDATE AuditExportAttempt
			SET status = 'FAILED',
				error_summary = @errorSummary,
				completed_at = now()
			WHERE id = @attemptId
			  AND sink_id = @sinkId
			  AND organization_id = @organizationId
			  AND status = 'RUNNING'`,
			pgx.NamedArgs{
				"attemptId":      attemptID,
				"sinkId":         sinkID,
				"organizationId": organizationID,
				"errorSummary":   errorSummary,
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
	return len(input.Correlations()) > 0
}

func controlPlaneAuditChecksumsValid(input types.ControlPlaneAuditEventInput) bool {
	for _, checksum := range []string{
		input.ReleaseChecksum,
		input.ComponentReleaseChecksum,
		input.ProductReleaseChecksum,
		input.ArtifactDigest,
		input.ManifestDigest,
		input.TargetConfigChecksum,
		input.DeploymentPlanChecksum,
		input.PolicyChecksum,
		input.ApprovalChecksum,
		input.CalendarChecksum,
		input.AdmissionChecksum,
		input.CampaignRevisionChecksum,
		input.CampaignControlChecksum,
		input.ExecutionChecksum,
		input.DesiredStateChecksum,
		input.ObservationChecksum,
		input.DriftChecksum,
		input.ReconciliationChecksum,
		input.AuditExportConfigChecksum,
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
	case input.ActorID == uuid.Nil:
		return apierrors.NewBadRequest("audit export actor is required")
	case len(name) < 1 || len(name) > 128:
		return apierrors.NewBadRequest("audit export sink name is invalid")
	case !input.Kind.Valid():
		return apierrors.NewBadRequest("audit export sink kind is invalid")
	case !types.ValidAuditExportEndpointReference(reference):
		return apierrors.NewBadRequest("audit export endpoint reference is invalid")
	case !controlPlaneAuditChecksumPattern.MatchString(input.ConfigChecksum):
		return apierrors.NewBadRequest("audit export config checksum is invalid")
	default:
		return nil
	}
}

func auditExportSinkCreatedEvent(
	input types.CreateAuditExportSinkInput,
	sink types.AuditExportSink,
) types.ControlPlaneAuditEventInput {
	return types.ControlPlaneAuditEventInput{
		OrganizationID:            input.OrganizationID,
		EventType:                 "audit_export_sink.created",
		ActorID:                   &input.ActorID,
		Outcome:                   "SUCCEEDED",
		AuditExportSinkID:         &sink.ID,
		AuditExportConfigChecksum: input.ConfigChecksum,
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
	if redacted, changed := auditexport.RedactAuditText(value); changed {
		return redacted
	}
	if len(value) > 2048 {
		value = value[:2048]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
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
