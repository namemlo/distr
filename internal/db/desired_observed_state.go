package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/desiredstate"
	"github.com/distr-sh/distr/internal/observation"
	"github.com/distr-sh/distr/internal/reconciliation"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const pendingDesiredOutputExpr = `
	p.id, p.created_at, p.updated_at, p.organization_id,
	p.deployment_plan_id, p.execution_id, p.execution_attempt_id,
	p.deployment_unit_id,
	p.component_instance_id, p.component_key, p.revision,
	p.artifact_digest, p.config_checksum, p.schema_version,
	p.capability_checksum, p.platform, p.topology_checksum,
	p.observation_deadline, p.status, p.terminal_reason,
	COALESCE(
		p.verified_observation_id,
		'00000000-0000-0000-0000-000000000000'::UUID
	) AS verified_observation_id,
	COALESCE(
		p.terminal_observation_id,
		'00000000-0000-0000-0000-000000000000'::UUID
	) AS terminal_observation_id,
	p.terminal_at
`

const activeDesiredOutputExpr = `
	a.id, a.created_at, a.organization_id, a.pending_revision_id,
	a.deployment_plan_id, a.execution_id, a.deployment_unit_id,
	a.component_instance_id, a.component_key, a.revision,
	a.artifact_digest, a.config_checksum, a.schema_version,
	a.capability_checksum, a.platform, a.topology_checksum,
	a.verified_observation_id
`

const observedStateOutputExpr = `
	o.id, o.created_at, o.organization_id, o.observer_id,
	o.deployment_unit_id, o.component_instance_id, o.component_key,
	o.source_sequence, o.captured_at, o.received_at, o.fresh_until,
	o.evidence_checksum, o.evidence_reference, o.artifact_digest,
	o.config_checksum, o.schema_version, o.capability_checksum,
	o.platform, o.topology_checksum, o.health, o.outcome,
	o.disposition, o.trusted, o.is_current, o.state_checksum,
	o.runtime_state_checksum,
	o.executor_outcome
`

type observerRegistrationRow struct {
	ID                    uuid.UUID  `db:"id"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	OrganizationID        uuid.UUID  `db:"organization_id"`
	DeploymentUnitID      uuid.UUID  `db:"deployment_unit_id"`
	ComponentInstanceID   *uuid.UUID `db:"component_instance_id"`
	ObserverKey           string     `db:"observer_key"`
	AdapterImplementation string     `db:"adapter_implementation"`
	AdapterVersion        string     `db:"adapter_version"`
	Enabled               bool       `db:"enabled"`
	CredentialFingerprint string     `db:"credential_fingerprint"`
	MaxFreshnessSeconds   int64      `db:"max_freshness_seconds"`
	MaxClockSkewSeconds   int64      `db:"max_clock_skew_seconds"`
	Measurements          []string   `db:"measurements"`
}

func (row observerRegistrationRow) toType() types.ObserverRegistration {
	return types.ObserverRegistration{
		ID: row.ID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		OrganizationID: row.OrganizationID, DeploymentUnitID: row.DeploymentUnitID,
		ComponentInstanceID: row.ComponentInstanceID, ObserverKey: row.ObserverKey,
		AdapterImplementation: row.AdapterImplementation, AdapterVersion: row.AdapterVersion,
		Enabled: row.Enabled, CredentialFingerprint: row.CredentialFingerprint,
		MaxFreshness: time.Duration(row.MaxFreshnessSeconds) * time.Second,
		MaxClockSkew: time.Duration(row.MaxClockSkewSeconds) * time.Second,
		Measurements: row.Measurements,
	}
}

const observerRegistrationOutputExpr = `
	r.id, r.created_at, r.updated_at, r.organization_id,
	r.deployment_unit_id, r.component_instance_id, r.observer_key,
	r.adapter_implementation, r.adapter_version, r.enabled,
	r.credential_fingerprint, r.max_freshness_seconds,
	r.max_clock_skew_seconds, r.measurements
`

func appendDesiredObservedAudit(
	ctx context.Context,
	mutated bool,
	input types.ControlPlaneAuditEventInput,
) error {
	if !mutated {
		return nil
	}
	return RecordControlPlaneAuditMutation(ctx, controlPlaneDomainAuditHook(ctx), input)
}

func CreateObserverRegistration(
	ctx context.Context,
	registration *types.ObserverRegistration,
) (*types.ObserverRegistration, error) {
	if registration.OrganizationID == uuid.Nil || registration.DeploymentUnitID == uuid.Nil ||
		strings.TrimSpace(registration.ObserverKey) == "" ||
		registration.MaxFreshness < time.Second ||
		registration.CredentialFingerprint == "" {
		return nil, apierrors.NewBadRequest("observer registration is invalid")
	}
	if registration.ID == uuid.Nil {
		registration.ID = uuid.New()
	}
	var result *types.ObserverRegistration
	err := RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		var err error
		result, err = createObserverRegistrationTx(txCtx, registration)
		return err
	})
	return result, err
}

func createObserverRegistrationTx(
	ctx context.Context,
	registration *types.ObserverRegistration,
) (*types.ObserverRegistration, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO ObserverRegistration AS r (
			id, organization_id, deployment_unit_id, component_instance_id,
			observer_key, adapter_implementation, adapter_version, enabled,
			credential_fingerprint, max_freshness_seconds,
			max_clock_skew_seconds, measurements
		) VALUES (
			@id, @organizationID, @deploymentUnitID, @componentInstanceID,
			@observerKey, @adapterImplementation, @adapterVersion, TRUE,
			@credentialFingerprint, @maxFreshnessSeconds,
			@maxClockSkewSeconds, @measurements
		)
		RETURNING `+observerRegistrationOutputExpr,
		pgx.NamedArgs{
			"id": registration.ID, "organizationID": registration.OrganizationID,
			"deploymentUnitID":      registration.DeploymentUnitID,
			"componentInstanceID":   registration.ComponentInstanceID,
			"observerKey":           strings.TrimSpace(registration.ObserverKey),
			"adapterImplementation": strings.TrimSpace(registration.AdapterImplementation),
			"adapterVersion":        strings.TrimSpace(registration.AdapterVersion),
			"credentialFingerprint": registration.CredentialFingerprint,
			"maxFreshnessSeconds":   int64(registration.MaxFreshness.Seconds()),
			"maxClockSkewSeconds":   int64(registration.MaxClockSkew.Seconds()),
			"measurements":          registration.Measurements,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not create ObserverRegistration: %w", err)
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[observerRegistrationRow])
	if err != nil {
		return nil, fmt.Errorf("could not collect ObserverRegistration: %w", err)
	}
	result := row.toType()
	if err := appendDesiredObservedAudit(ctx, true,
		types.ControlPlaneAuditEventInput{
			OrganizationID:   result.OrganizationID,
			EventType:        "observer.registered",
			Outcome:          "SUCCEEDED",
			DeploymentUnitID: &result.DeploymentUnitID,
		},
	); err != nil {
		return nil, fmt.Errorf("could not audit ObserverRegistration: %w", err)
	}
	return &result, nil
}

func ListObserverRegistrations(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.ObserverRegistration, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+observerRegistrationOutputExpr+`
		FROM ObserverRegistration r
		WHERE r.organization_id = @organizationID
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT 200`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list ObserverRegistration: %w", err)
	}
	records, err := pgx.CollectRows(rows, pgx.RowToStructByName[observerRegistrationRow])
	if err != nil {
		return nil, fmt.Errorf("could not collect ObserverRegistration: %w", err)
	}
	result := make([]types.ObserverRegistration, len(records))
	for i, record := range records {
		result[i] = record.toType()
	}
	return result, nil
}

func RecordExecutorReport(
	ctx context.Context,
	report types.ExecutorReport,
) (*types.ExecutorReport, error) {
	value, _, err := RecordExecutorReportWithTask(ctx, report)
	return value, err
}

func RecordExecutorReportWithTask(
	ctx context.Context,
	report types.ExecutorReport,
) (*types.ExecutorReport, *types.Task, error) {
	if report.OrganizationID == uuid.Nil || report.PendingRevisionID == uuid.Nil ||
		report.ExecutionID == uuid.Nil || len(report.EvidenceReference) > 2048 {
		return nil, nil, apierrors.NewBadRequest("executor report lineage is invalid")
	}
	switch report.Outcome {
	case types.ExecutorOutcomeSucceeded,
		types.ExecutorOutcomeFailed,
		types.ExecutorOutcomeCancelled,
		types.ExecutorOutcomeUnknown:
	default:
		return nil, nil, apierrors.NewBadRequest("executor report outcome is invalid")
	}
	if report.ReportedStateChecksum != "" &&
		!isLowerSHA256(report.ReportedStateChecksum) {
		return nil, nil, apierrors.NewBadRequest("executor report checksum is invalid")
	}
	if report.ID == uuid.Nil {
		report.ID = uuid.New()
	}
	var value *types.ExecutorReport
	err := RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		var err error
		value, err = recordExecutorReportTx(txCtx, report)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	task, err := ReconcilePendingDesiredRevisionWithTask(ctx, value.PendingRevisionID)
	if err != nil {
		return value, nil, fmt.Errorf(
			"could not reconcile desired state after executor report: %w",
			err,
		)
	}
	return value, task, nil
}

func recordExecutorReportTx(
	ctx context.Context,
	report types.ExecutorReport,
) (*types.ExecutorReport, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO ExecutorReport AS e (
			id, organization_id, pending_revision_id, execution_id,
			outcome, reported_state_checksum, evidence_reference
		) VALUES (
			@id, @organizationID, @pendingRevisionID, @executionID,
			@outcome, @reportedStateChecksum, @evidenceReference
		)
		RETURNING e.id, e.created_at, e.organization_id,
			e.pending_revision_id, e.execution_id, e.outcome,
			e.reported_state_checksum, e.evidence_reference`,
		pgx.NamedArgs{
			"id": report.ID, "organizationID": report.OrganizationID,
			"pendingRevisionID": report.PendingRevisionID,
			"executionID":       report.ExecutionID, "outcome": report.Outcome,
			"reportedStateChecksum": report.ReportedStateChecksum,
			"evidenceReference":     strings.TrimSpace(report.EvidenceReference),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not insert ExecutorReport: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.ExecutorReport],
	)
	if err != nil {
		return nil, fmt.Errorf("could not collect ExecutorReport: %w", err)
	}
	if err := appendDesiredObservedAudit(ctx, true,
		types.ControlPlaneAuditEventInput{
			OrganizationID:    value.OrganizationID,
			EventType:         "executor_report.recorded",
			Outcome:           string(value.Outcome),
			DesiredStateID:    &value.PendingRevisionID,
			ExecutionID:       &value.ExecutionID,
			ExecutionChecksum: value.ReportedStateChecksum,
		},
	); err != nil {
		return nil, fmt.Errorf("could not audit ExecutorReport: %w", err)
	}
	return &value, nil
}

func isLowerSHA256(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 ||
		!strings.HasPrefix(value, "sha256:") ||
		value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == sha256.Size
}

func validateExecutionV2Lineage(
	ctx context.Context,
	input types.PendingDesiredRevisionInput,
) error {
	if input.ExecutionAttemptID == uuid.Nil {
		return apierrors.NewBadRequest("execution-v2 attempt lineage is required")
	}
	var valid bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM ExecutionAttempt ea
			JOIN Task t
			  ON t.id = ea.task_id
			 AND t.organization_id = ea.organization_id
			JOIN DeploymentUnit du
			  ON du.id = @deploymentUnitID
			 AND du.organization_id = ea.organization_id
			WHERE ea.id = @executionAttemptID
			  AND ea.organization_id = @organizationID
			  AND ea.execution_id = @executionID
			  AND t.protocol_version = 'v2'
			  AND t.deployment_plan_id = @deploymentPlanID
			  AND ea.deployment_target_id = du.deployment_target_id
		)`,
		pgx.NamedArgs{
			"executionAttemptID": input.ExecutionAttemptID,
			"organizationID":     input.OrganizationID,
			"executionID":        input.ExecutionID,
			"deploymentPlanID":   input.DeploymentPlanID,
			"deploymentUnitID":   input.DeploymentUnitID,
		},
	).Scan(&valid)
	if err != nil {
		return fmt.Errorf("could not validate execution-v2 lineage: %w", err)
	}
	if !valid {
		return apierrors.NewConflict("pending desired revision does not match execution-v2 lineage")
	}
	return nil
}

func AdmitPendingDesiredRevision(
	ctx context.Context,
	input types.PendingDesiredRevisionInput,
) (*types.PendingDesiredRevision, error) {
	var admitted *types.PendingDesiredRevision
	err := RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		var err error
		admitted, err = admitPendingDesiredRevisionTx(txCtx, input, time.Now().UTC())
		return err
	})
	return admitted, err
}

func admitPendingDesiredRevisionTx(
	ctx context.Context,
	input types.PendingDesiredRevisionInput,
	now time.Time,
) (*types.PendingDesiredRevision, error) {
	if err := validateExecutionV2Lineage(ctx, input); err != nil {
		return nil, err
	}
	head, err := getDesiredHeadForUpdate(
		ctx, input.OrganizationID, input.DeploymentUnitID, input.ComponentInstanceID,
	)
	if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
		return nil, err
	}
	var active *types.ActiveDesiredRevision
	if head != nil && head.ActiveRevisionID != nil {
		active, err = getActiveDesiredRevision(
			ctx, input.OrganizationID, *head.ActiveRevisionID,
		)
		if err != nil {
			return nil, err
		}
	}
	pending, _, err := desiredstate.Admit(input, active, now.UTC())
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
			INSERT INTO PendingDesiredRevision AS p (
				id, organization_id, deployment_plan_id, execution_id,
				execution_attempt_id,
				deployment_unit_id, component_instance_id, component_key, revision,
				artifact_digest, config_checksum, schema_version,
				capability_checksum, platform, topology_checksum,
				observation_deadline, status
			) VALUES (
				@id, @organizationID, @deploymentPlanID, @executionID,
				@executionAttemptID,
				@deploymentUnitID, @componentInstanceID, @componentKey, @revision,
				@artifactDigest, @configChecksum, @schemaVersion,
				@capabilityChecksum, @platform, @topologyChecksum,
				@observationDeadline, @status
			)
			RETURNING `+pendingDesiredOutputExpr,
		pgx.NamedArgs{
			"id": pending.ID, "organizationID": pending.OrganizationID,
			"deploymentPlanID": pending.DeploymentPlanID, "executionID": pending.ExecutionID,
			"executionAttemptID":  pending.ExecutionAttemptID,
			"deploymentUnitID":    pending.DeploymentUnitID,
			"componentInstanceID": pending.ComponentInstanceID,
			"componentKey":        pending.ComponentKey, "revision": pending.Revision,
			"artifactDigest": pending.ArtifactDigest, "configChecksum": pending.ConfigChecksum,
			"schemaVersion":      pending.SchemaVersion,
			"capabilityChecksum": pending.CapabilityChecksum,
			"platform":           pending.Platform, "topologyChecksum": pending.TopologyChecksum,
			"observationDeadline": pending.ObservationDeadline, "status": pending.Status,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not insert PendingDesiredRevision: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.PendingDesiredRevision],
	)
	if err != nil {
		return nil, fmt.Errorf("could not collect PendingDesiredRevision: %w", err)
	}
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
			INSERT INTO ComponentDesiredStateHead (
				organization_id, deployment_unit_id, component_instance_id,
				component_key, pending_revision_id, active_revision_id
			) VALUES (
				@organizationID, @deploymentUnitID, @componentInstanceID,
				@componentKey, @pendingRevisionID, @activeRevisionID
			)
			ON CONFLICT (organization_id, deployment_unit_id, component_instance_id)
			DO UPDATE SET pending_revision_id = EXCLUDED.pending_revision_id,
				component_key = EXCLUDED.component_key, updated_at = now()`,
		pgx.NamedArgs{
			"organizationID":      value.OrganizationID,
			"deploymentUnitID":    value.DeploymentUnitID,
			"componentInstanceID": value.ComponentInstanceID,
			"componentKey":        value.ComponentKey, "pendingRevisionID": value.ID,
			"activeRevisionID": func() *uuid.UUID {
				if active == nil {
					return nil
				}
				return &active.ID
			}(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not update ComponentDesiredStateHead: %w", err)
	}
	if err := appendDesiredObservedAudit(ctx, true,
		types.ControlPlaneAuditEventInput{
			OrganizationID:       value.OrganizationID,
			EventType:            "desired_revision.pending",
			Outcome:              string(value.Status),
			DeploymentPlanID:     &value.DeploymentPlanID,
			ExecutionID:          &value.ExecutionID,
			DesiredStateID:       &value.ID,
			DeploymentUnitID:     &value.DeploymentUnitID,
			ArtifactDigest:       value.ArtifactDigest,
			TargetConfigChecksum: value.ConfigChecksum,
		},
	); err != nil {
		return nil, fmt.Errorf("could not audit PendingDesiredRevision: %w", err)
	}
	return &value, nil
}

func AdvanceActiveDesiredRevision(
	ctx context.Context,
	pendingRevisionID uuid.UUID,
	gate types.ObservationGateResult,
) error {
	_, err := AdvanceActiveDesiredRevisionWithTask(ctx, pendingRevisionID, gate)
	return err
}

// AdvanceActiveDesiredRevisionWithTask returns the exact task whose step projection
// became terminal. Callers must dispatch newly-ready dependent steps only after this
// function returns, because the returned task is produced inside a serializable
// transaction and exposed after commit.
func AdvanceActiveDesiredRevisionWithTask(
	ctx context.Context,
	pendingRevisionID uuid.UUID,
	gate types.ObservationGateResult,
) (*types.Task, error) {
	var task *types.Task
	err := RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		var err error
		task, err = advanceActiveDesiredRevisionTx(
			txCtx, pendingRevisionID, gate, time.Now().UTC(),
		)
		return err
	})
	return task, err
}

func advanceActiveDesiredRevisionTx(
	txCtx context.Context,
	pendingRevisionID uuid.UUID,
	gate types.ObservationGateResult,
	now time.Time,
) (*types.Task, error) {
	pending, err := getPendingDesiredRevisionForUpdate(txCtx, pendingRevisionID)
	if err != nil {
		return nil, err
	}
	if pending.Status != types.PendingDesiredStatusPending {
		return finalizeExecutionV2Mutation(txCtx, *pending)
	}
	evaluated, err := evaluateObservationGate(
		txCtx,
		*pending,
		now,
	)
	if err != nil {
		return nil, err
	}
	if err := validatePromotionGateHint(gate, evaluated); err != nil {
		return nil, err
	}
	gate = evaluated
	head, err := getDesiredHeadForUpdate(
		txCtx, pending.OrganizationID, pending.DeploymentUnitID,
		pending.ComponentInstanceID,
	)
	if err != nil {
		return nil, err
	}
	var active *types.ActiveDesiredRevision
	if head.ActiveRevisionID != nil {
		active, err = getActiveDesiredRevision(
			txCtx, pending.OrganizationID, *head.ActiveRevisionID,
		)
		if err != nil {
			return nil, err
		}
	}
	next, terminal, err := desiredstate.Advance(
		active, *pending, gate, now,
	)
	if err != nil {
		return nil, apierrors.NewConflict(err.Error())
	}
	if terminal.Status == types.PendingDesiredStatusPending {
		return nil, nil
	}
	_, err = internalctx.GetDb(txCtx).Exec(txCtx, `
			UPDATE PendingDesiredRevision
			SET updated_at = @updatedAt, status = @status,
				terminal_reason = @terminalReason,
				verified_observation_id = NULLIF(@verifiedObservationID, @nilUUID),
				terminal_observation_id = NULLIF(@terminalObservationID, @nilUUID),
				terminal_at = @terminalAt
			WHERE id = @pendingRevisionID
			  AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"updatedAt": terminal.UpdatedAt, "status": terminal.Status,
			"terminalReason":        terminal.TerminalReason,
			"verifiedObservationID": terminal.VerifiedObservationID,
			"terminalObservationID": terminal.TerminalObservationID,
			"nilUUID":               uuid.Nil, "terminalAt": terminal.TerminalAt,
			"pendingRevisionID": terminal.ID, "organizationID": terminal.OrganizationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not terminalize PendingDesiredRevision: %w", err)
	}
	var activeID *uuid.UUID
	if next != nil && terminal.Status == types.PendingDesiredStatusVerified {
		_, err = internalctx.GetDb(txCtx).Exec(txCtx, `
				INSERT INTO ActiveDesiredRevision (
					id, organization_id, pending_revision_id, deployment_plan_id,
					execution_id, deployment_unit_id, component_instance_id,
					component_key, revision, artifact_digest, config_checksum,
					schema_version, capability_checksum, platform,
					topology_checksum, verified_observation_id
				) VALUES (
					@id, @organizationID, @pendingRevisionID, @deploymentPlanID,
					@executionID, @deploymentUnitID, @componentInstanceID,
					@componentKey, @revision, @artifactDigest, @configChecksum,
					@schemaVersion, @capabilityChecksum, @platform,
					@topologyChecksum, @verifiedObservationID
				)`,
			pgx.NamedArgs{
				"id": next.ID, "organizationID": next.OrganizationID,
				"pendingRevisionID": next.PendingRevisionID,
				"deploymentPlanID":  next.DeploymentPlanID, "executionID": next.ExecutionID,
				"deploymentUnitID":    next.DeploymentUnitID,
				"componentInstanceID": next.ComponentInstanceID,
				"componentKey":        next.ComponentKey, "revision": next.Revision,
				"artifactDigest": next.ArtifactDigest, "configChecksum": next.ConfigChecksum,
				"schemaVersion":      next.SchemaVersion,
				"capabilityChecksum": next.CapabilityChecksum,
				"platform":           next.Platform, "topologyChecksum": next.TopologyChecksum,
				"verifiedObservationID": next.VerifiedObservationID,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("could not insert ActiveDesiredRevision: %w", err)
		}
		activeID = &next.ID
	} else if active != nil {
		activeID = &active.ID
	}
	_, err = internalctx.GetDb(txCtx).Exec(txCtx, `
			UPDATE ComponentDesiredStateHead
			SET pending_revision_id = NULL, active_revision_id = @activeRevisionID,
				quarantined = @quarantined, quarantine_reason = @quarantineReason,
				updated_at = now()
			WHERE organization_id = @organizationID
			  AND deployment_unit_id = @deploymentUnitID
			  AND component_instance_id = @componentInstanceID`,
		pgx.NamedArgs{
			"activeRevisionID": activeID, "quarantined": gate.Quarantine,
			"quarantineReason": gate.Reason, "organizationID": pending.OrganizationID,
			"deploymentUnitID":    pending.DeploymentUnitID,
			"componentInstanceID": pending.ComponentInstanceID,
		},
	)
	if err != nil {
		return nil, err
	}
	terminalEventType := "desired_revision.terminalized"
	if !now.Before(pending.ObservationDeadline) &&
		terminal.Status != types.PendingDesiredStatusVerified {
		terminalEventType = "desired_revision.deadline_terminalized"
	}
	if err := appendDesiredObservedAudit(txCtx, true,
		types.ControlPlaneAuditEventInput{
			OrganizationID:       terminal.OrganizationID,
			EventType:            terminalEventType,
			Outcome:              string(terminal.Status),
			DeploymentPlanID:     &terminal.DeploymentPlanID,
			ExecutionID:          &terminal.ExecutionID,
			DesiredStateID:       &terminal.ID,
			ObservationID:        nonNilUUIDPointer(terminal.TerminalObservationID),
			DeploymentUnitID:     &terminal.DeploymentUnitID,
			ArtifactDigest:       terminal.ArtifactDigest,
			TargetConfigChecksum: terminal.ConfigChecksum,
		},
	); err != nil {
		return nil, fmt.Errorf("could not audit terminal desired revision: %w", err)
	}
	if next != nil && terminal.Status == types.PendingDesiredStatusVerified {
		if err := appendDesiredObservedAudit(txCtx, true,
			types.ControlPlaneAuditEventInput{
				OrganizationID:       next.OrganizationID,
				EventType:            "active_desired_revision.advanced",
				Outcome:              "VERIFIED",
				DeploymentPlanID:     &next.DeploymentPlanID,
				ExecutionID:          &next.ExecutionID,
				DesiredStateID:       &next.ID,
				ObservationID:        &next.VerifiedObservationID,
				DeploymentUnitID:     &next.DeploymentUnitID,
				ArtifactDigest:       next.ArtifactDigest,
				TargetConfigChecksum: next.ConfigChecksum,
			},
		); err != nil {
			return nil, fmt.Errorf("could not audit active desired revision: %w", err)
		}
	}
	if active != nil && terminal.Status != types.PendingDesiredStatusVerified &&
		gate.ObservationID != uuid.Nil {
		if err := openTerminalMismatchDriftCaseTx(
			txCtx, *active, *pending, gate, now,
		); err != nil {
			return nil, err
		}
	}
	return finalizeExecutionV2Mutation(txCtx, terminal)
}

func nonNilUUIDPointer(value uuid.UUID) *uuid.UUID {
	if value == uuid.Nil {
		return nil
	}
	return &value
}

func SweepExpiredPendingDesiredRevisions(
	ctx context.Context,
	limit int,
) ([]types.Task, error) {
	if limit < 1 || limit > 1000 {
		return nil, apierrors.NewBadRequest("deadline sweep limit must be between 1 and 1000")
	}
	tasks := make([]types.Task, 0, limit)
	err := RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		rows, err := internalctx.GetDb(txCtx).Query(txCtx, `
			SELECT id
			FROM PendingDesiredRevision
			WHERE status = 'PENDING'
			  AND observation_deadline <= clock_timestamp()
			ORDER BY observation_deadline, id
			LIMIT @limit
			FOR UPDATE SKIP LOCKED`,
			pgx.NamedArgs{"limit": limit},
		)
		if err != nil {
			return fmt.Errorf("could not select expired desired revisions: %w", err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return fmt.Errorf("could not collect expired desired revisions: %w", err)
		}
		for _, id := range ids {
			pending, err := getPendingDesiredRevisionForUpdate(txCtx, id)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			if now.Before(pending.ObservationDeadline) {
				now = pending.ObservationDeadline
			}
			gate, err := evaluateObservationGate(txCtx, *pending, now)
			if err != nil {
				return err
			}
			if gate.Status == types.ObservationGateStatusPending {
				return fmt.Errorf("expired desired revision remained pending")
			}
			task, err := advanceActiveDesiredRevisionTx(txCtx, id, gate, now)
			if err != nil {
				return err
			}
			if task == nil {
				return fmt.Errorf("expired desired revision produced no execution task projection")
			}
			tasks = append(tasks, *task)
		}
		return nil
	})
	return tasks, err
}

func RunDesiredObservationDeadlineSweep(ctx context.Context) error {
	_, err := RunDesiredObservationDeadlineSweepWithTasks(ctx)
	return err
}

// RunDesiredObservationDeadlineSweepWithTasks returns committed task projections
// so the scheduler layer can dispatch dependency-ready steps after each batch.
func RunDesiredObservationDeadlineSweepWithTasks(ctx context.Context) ([]types.Task, error) {
	const batchSize = 100
	var projected []types.Task
	for {
		tasks, err := SweepExpiredPendingDesiredRevisions(ctx, batchSize)
		if err != nil {
			return nil, err
		}
		projected = append(projected, tasks...)
		if len(tasks) < batchSize {
			return projected, nil
		}
	}
}

func finalizeExecutionV2Mutation(
	ctx context.Context,
	pending types.PendingDesiredRevision,
) (*types.Task, error) {
	projectionStatus, err := executionProjectionStatusForDesiredTerminal(pending.Status)
	if err != nil {
		return nil, err
	}
	attempt, err := getExecutionAttemptForUpdate(
		ctx, pending.ExecutionAttemptID, pending.OrganizationID,
	)
	if err != nil {
		return nil, err
	}
	if attempt.Identity.ExecutionID != pending.ExecutionID {
		return nil, apierrors.NewConflict("desired-state execution attempt lineage changed")
	}
	if projectionStatus == types.ExecutionAttemptStatusUnknown {
		return nil, projectExecutionV2Uncertain(
			ctx, attempt.ID, attempt.OrganizationID,
		)
	}
	return projectExecutionV2Terminal(ctx, *attempt, projectionStatus)
}

func openTerminalMismatchDriftCaseTx(
	ctx context.Context,
	active types.ActiveDesiredRevision,
	pending types.PendingDesiredRevision,
	gate types.ObservationGateResult,
	now time.Time,
) error {
	observed, err := getObservedEvidence(ctx, pending.OrganizationID, gate.ObservationID)
	if err != nil {
		return err
	}
	observed.ExecutorOutcome, err = getLatestExecutorOutcome(
		ctx, pending.OrganizationID, pending.ID, pending.ExecutionID,
	)
	if err != nil {
		return err
	}
	classification := reconciliation.ClassifyDriftAt(active, *observed, now)
	if observed.ExecutorOutcome == types.ExecutorOutcomeSucceeded {
		classification.Drifted = true
		classification.Classes = appendUniqueDriftClass(
			classification.Classes, types.DriftClassExecutorMismatch,
		)
		classification.Summary = "executor success differs from independent runtime evidence"
	}
	if gate.Status == types.ObservationGateStatusConflict {
		classification.Drifted = true
		classification.Classes = appendUniqueDriftClass(
			classification.Classes, types.DriftClassConflict,
		)
		classification.Summary = "trusted observer evidence conflicts"
	}
	if !classification.Drifted {
		return nil
	}
	return openAutomaticDriftCaseTx(ctx, types.DriftInput{
		OrganizationID:          pending.OrganizationID,
		ActiveDesiredRevisionID: active.ID,
		ObservationID:           observed.ID,
		Classification:          classification,
		Reason:                  gate.Reason,
	})
}

func appendUniqueDriftClass(
	classes []types.DriftClass,
	class types.DriftClass,
) []types.DriftClass {
	for _, current := range classes {
		if current == class {
			return classes
		}
	}
	return append(classes, class)
}

func openAutomaticDriftCaseTx(ctx context.Context, input types.DriftInput) error {
	classes := make([]string, len(input.Classification.Classes))
	for i, class := range input.Classification.Classes {
		classes[i] = string(class)
	}
	var caseID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		INSERT INTO DriftCase (
			id, organization_id, active_desired_revision_id,
			observation_id, deployment_unit_id, component_instance_id,
			status, classes, summary
		)
		SELECT @id, @organizationID, @activeDesiredRevisionID,
			@observationID, a.deployment_unit_id, a.component_instance_id,
			@status, @classes, @summary
		FROM ActiveDesiredRevision a
		JOIN ObservedComponentState o
		  ON o.id = @observationID
		 AND o.organization_id = a.organization_id
		 AND o.deployment_unit_id = a.deployment_unit_id
		 AND o.component_instance_id = a.component_instance_id
		WHERE a.id = @activeDesiredRevisionID
		  AND a.organization_id = @organizationID
		ON CONFLICT (
			organization_id, active_desired_revision_id, observation_id
		) WHERE status IN ('OPEN', 'ASSIGNED', 'EXCEPTION')
		DO NOTHING
		RETURNING id`,
		pgx.NamedArgs{
			"id": uuid.New(), "organizationID": input.OrganizationID,
			"activeDesiredRevisionID": input.ActiveDesiredRevisionID,
			"observationID":           input.ObservationID,
			"status":                  types.DriftCaseStatusOpen, "classes": classes,
			"summary": input.Classification.Summary,
		},
	).Scan(&caseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("could not automatically open DriftCase: %w", err)
	}
	_, err = internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO DriftCaseEvent (
			id, organization_id, drift_case_id, status, reason
		) VALUES (
			@id, @organizationID, @driftCaseID, @status, @reason
		)`,
		pgx.NamedArgs{
			"id": uuid.New(), "organizationID": input.OrganizationID,
			"driftCaseID": caseID, "status": types.DriftCaseStatusOpen,
			"reason": strings.TrimSpace(input.Reason),
		},
	)
	if err != nil {
		return fmt.Errorf("could not append automatic DriftCaseEvent: %w", err)
	}
	return appendDesiredObservedAudit(ctx, true,
		types.ControlPlaneAuditEventInput{
			OrganizationID: input.OrganizationID,
			EventType:      "drift_case.opened",
			Outcome:        string(types.DriftCaseStatusOpen),
			DesiredStateID: &input.ActiveDesiredRevisionID,
			ObservationID:  &input.ObservationID,
			DriftCaseID:    &caseID,
		},
	)
}

func validateVerifiedGate(
	pending types.PendingDesiredRevision,
	observed types.ObservedComponentState,
	supplied types.ObservationGateResult,
	now time.Time,
) (types.ObservationGateResult, error) {
	verified := observation.EvaluateGate(
		pending,
		[]types.ObservedComponentState{observed},
		now,
	)
	if verified.Status != types.ObservationGateStatusVerified ||
		verified.ObservationID != supplied.ObservationID ||
		verified.ObservationChecksum != supplied.ObservationChecksum {
		return types.ObservationGateResult{}, apierrors.NewConflict(
			"verified observation no longer matches pending desired state",
		)
	}
	return verified, nil
}

func validatePromotionGateHint(
	supplied types.ObservationGateResult,
	evaluated types.ObservationGateResult,
) error {
	if supplied.Status != evaluated.Status ||
		supplied.ObservationID != evaluated.ObservationID ||
		supplied.ObservationChecksum != evaluated.ObservationChecksum {
		return apierrors.NewConflict(
			"observation gate changed before desired-state promotion",
		)
	}
	return nil
}

func IngestObservation(
	ctx context.Context,
	envelope types.ObservationEnvelope,
) (*types.ObservedComponentState, error) {
	state, _, err := IngestObservationWithTask(ctx, envelope)
	return state, err
}

func IngestObservationWithTask(
	ctx context.Context,
	envelope types.ObservationEnvelope,
) (*types.ObservedComponentState, *types.Task, error) {
	var ingested *types.ObservedComponentState
	err := RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		registration, err := getObserverRegistrationForUpdate(
			txCtx, envelope.OrganizationID, envelope.ObserverID,
		)
		if err != nil {
			if errors.Is(err, apierrors.ErrNotFound) {
				return fmt.Errorf("%w: observer registration not found", apierrors.ErrUnauthorized)
			}
			return err
		}
		head, err := getObservationHeadForUpdate(
			txCtx, envelope.OrganizationID, envelope.ObserverID,
			envelope.DeploymentUnitID, envelope.ComponentInstanceID,
		)
		if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		var retained *types.ObservedComponentState
		if head != nil {
			retained, err = getObservedComponentState(
				txCtx,
				envelope.OrganizationID,
				head.ObservationID,
			)
			if err != nil {
				return err
			}
		}
		receivedAt := time.Now().UTC()
		decision, admissionErr := observation.EvaluateAdmission(
			*registration, head, retained, envelope, receivedAt,
		)
		if admissionErr != nil && !decision.RetainEvidence {
			if errors.Is(admissionErr, observation.ErrUntrustedObservation) ||
				errors.Is(admissionErr, observation.ErrObserverMismatch) {
				return fmt.Errorf("%w: observation authentication failed", apierrors.ErrUnauthorized)
			}
			return apierrors.NewBadRequest(admissionErr.Error())
		}
		if decision.Disposition == types.ObservationDispositionReplay {
			ingested = retained
			return nil
		}
		stateChecksum, err := observationStateChecksum(envelope)
		if err != nil {
			return err
		}
		runtimeStateChecksum, err := observation.RuntimeStateChecksum(envelope)
		if err != nil {
			return apierrors.NewBadRequest(err.Error())
		}
		if decision.AdvanceHead {
			_, err = internalctx.GetDb(txCtx).Exec(txCtx, `
				UPDATE ObservedComponentState
				SET is_current = FALSE
				WHERE organization_id = @organizationID
				  AND observer_id = @observerID
				  AND deployment_unit_id = @deploymentUnitID
				  AND component_instance_id = @componentInstanceID
				  AND is_current = TRUE`,
				pgx.NamedArgs{
					"organizationID": envelope.OrganizationID, "observerID": envelope.ObserverID,
					"deploymentUnitID":    envelope.DeploymentUnitID,
					"componentInstanceID": envelope.ComponentInstanceID,
				},
			)
			if err != nil {
				return fmt.Errorf("could not retire observation head: %w", err)
			}
		}
		rows, err := internalctx.GetDb(txCtx).Query(txCtx, `
			INSERT INTO ObservedComponentState AS o (
				id, organization_id, observer_id, deployment_unit_id,
				component_instance_id, component_key, source_sequence,
				captured_at, received_at, fresh_until,
				evidence_checksum, evidence_reference,
				artifact_digest, config_checksum, schema_version,
				capability_checksum, platform, topology_checksum, health,
				outcome, disposition, trusted, is_current, state_checksum,
				runtime_state_checksum
			) VALUES (
				@id, @organizationID, @observerID, @deploymentUnitID,
				@componentInstanceID, @componentKey, @sourceSequence,
				@capturedAt, @receivedAt, @freshUntil,
				@evidenceChecksum, @evidenceReference,
				@artifactDigest, @configChecksum, @schemaVersion,
				@capabilityChecksum, @platform, @topologyChecksum, @health,
				@outcome, @disposition, @trusted, @isCurrent, @stateChecksum,
				@runtimeStateChecksum
			)
			RETURNING `+observedStateOutputExpr,
			pgx.NamedArgs{
				"id": uuid.New(), "organizationID": envelope.OrganizationID,
				"observerID": envelope.ObserverID, "deploymentUnitID": envelope.DeploymentUnitID,
				"componentInstanceID": envelope.ComponentInstanceID,
				"componentKey":        strings.TrimSpace(envelope.ComponentKey),
				"sourceSequence":      envelope.SourceSequence, "capturedAt": envelope.CapturedAt,
				"receivedAt":        receivedAt,
				"freshUntil":        envelope.CapturedAt.Add(registration.MaxFreshness),
				"evidenceChecksum":  envelope.EvidenceChecksum,
				"evidenceReference": envelope.EvidenceReference,
				"artifactDigest":    envelope.ArtifactDigest,
				"configChecksum":    envelope.ConfigChecksum, "schemaVersion": envelope.SchemaVersion,
				"capabilityChecksum": envelope.CapabilityChecksum, "platform": envelope.Platform,
				"topologyChecksum": envelope.TopologyChecksum, "health": envelope.Health,
				"outcome": envelope.Outcome, "disposition": decision.Disposition,
				"trusted": decision.Trusted, "isCurrent": decision.AdvanceHead,
				"stateChecksum":        stateChecksum,
				"runtimeStateChecksum": runtimeStateChecksum,
			},
		)
		if err != nil {
			return fmt.Errorf("could not insert ObservedComponentState: %w", err)
		}
		value, err := pgx.CollectExactlyOneRow(
			rows, pgx.RowToStructByName[types.ObservedComponentState],
		)
		if err != nil {
			return fmt.Errorf("could not collect ObservedComponentState: %w", err)
		}
		if decision.AdvanceHead {
			_, err = internalctx.GetDb(txCtx).Exec(txCtx, `
				INSERT INTO ComponentObservationHead (
					organization_id, observer_id, deployment_unit_id,
					component_instance_id, source_sequence, observation_id,
					evidence_checksum, captured_at
				) VALUES (
					@organizationID, @observerID, @deploymentUnitID,
					@componentInstanceID, @sourceSequence, @observationID,
					@evidenceChecksum, @capturedAt
				)
				ON CONFLICT (
					organization_id, observer_id, deployment_unit_id, component_instance_id
				) DO UPDATE SET source_sequence = EXCLUDED.source_sequence,
					observation_id = EXCLUDED.observation_id,
					evidence_checksum = EXCLUDED.evidence_checksum,
					captured_at = EXCLUDED.captured_at`,
				pgx.NamedArgs{
					"organizationID": value.OrganizationID, "observerID": value.ObserverID,
					"deploymentUnitID":    value.DeploymentUnitID,
					"componentInstanceID": value.ComponentInstanceID,
					"sourceSequence":      value.SourceSequence, "observationID": value.ID,
					"evidenceChecksum": value.EvidenceChecksum, "capturedAt": value.CapturedAt,
				},
			)
			if err != nil {
				return fmt.Errorf("could not advance ComponentObservationHead: %w", err)
			}
		}
		if decision.Quarantine {
			_, err = internalctx.GetDb(txCtx).Exec(txCtx, `
				UPDATE ComponentDesiredStateHead
				SET quarantined = TRUE, quarantine_reason = @reason, updated_at = now()
				WHERE organization_id = @organizationID
				  AND deployment_unit_id = @deploymentUnitID
				  AND component_instance_id = @componentInstanceID`,
				pgx.NamedArgs{
					"reason": decision.Reason, "organizationID": value.OrganizationID,
					"deploymentUnitID":    value.DeploymentUnitID,
					"componentInstanceID": value.ComponentInstanceID,
				},
			)
			if err != nil {
				return fmt.Errorf("could not quarantine desired state head: %w", err)
			}
		}
		eventType := "observation.rejected"
		if value.Disposition == types.ObservationDispositionAccepted {
			eventType = "observation.accepted"
		}
		if err := appendDesiredObservedAudit(txCtx, true,
			types.ControlPlaneAuditEventInput{
				OrganizationID:       value.OrganizationID,
				EventType:            eventType,
				Outcome:              string(value.Disposition),
				ObservationID:        &value.ID,
				DeploymentUnitID:     &value.DeploymentUnitID,
				ArtifactDigest:       value.ArtifactDigest,
				TargetConfigChecksum: value.ConfigChecksum,
				ObservationChecksum:  value.StateChecksum,
			},
		); err != nil {
			return fmt.Errorf("could not audit observation ingestion: %w", err)
		}
		ingested = &value
		return nil
	})
	if err != nil || ingested == nil {
		return ingested, nil, err
	}
	task, err := ReconcilePendingDesiredRevisionForComponentWithTask(
		ctx,
		ingested.OrganizationID,
		ingested.DeploymentUnitID,
		ingested.ComponentInstanceID,
	)
	if err != nil {
		return ingested, nil, fmt.Errorf(
			"could not reconcile desired state after observation ingestion: %w",
			err,
		)
	}
	if ingested.Disposition == types.ObservationDispositionAccepted {
		if err := openAutomaticDriftCaseForObservation(ctx, *ingested); err != nil {
			return ingested, nil, fmt.Errorf(
				"could not classify observation drift: %w", err,
			)
		}
	}
	return ingested, task, nil
}

func openAutomaticDriftCaseForObservation(
	ctx context.Context,
	observed types.ObservedComponentState,
) error {
	return RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		var activeRevisionID *uuid.UUID
		err := internalctx.GetDb(txCtx).QueryRow(txCtx, `
		SELECT active_revision_id
		FROM ComponentDesiredStateHead
		WHERE organization_id = @organizationID
		  AND deployment_unit_id = @deploymentUnitID
		  AND component_instance_id = @componentInstanceID`,
			pgx.NamedArgs{
				"organizationID":      observed.OrganizationID,
				"deploymentUnitID":    observed.DeploymentUnitID,
				"componentInstanceID": observed.ComponentInstanceID,
			},
		).Scan(&activeRevisionID)
		if errors.Is(err, pgx.ErrNoRows) || activeRevisionID == nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("could not query active desired state for drift: %w", err)
		}
		active, err := getActiveDesiredRevision(
			txCtx, observed.OrganizationID, *activeRevisionID,
		)
		if err != nil {
			return err
		}
		classification := reconciliation.ClassifyDriftAt(
			*active, observed, time.Now().UTC(),
		)
		if !classification.Drifted {
			return nil
		}
		return openAutomaticDriftCaseTx(txCtx, types.DriftInput{
			OrganizationID:          observed.OrganizationID,
			ActiveDesiredRevisionID: active.ID, ObservationID: observed.ID,
			Classification: classification,
			Reason:         "accepted runtime observation differs from active desired state",
		})
	})
}

func ReconcilePendingDesiredRevision(
	ctx context.Context,
	pendingRevisionID uuid.UUID,
) error {
	_, err := ReconcilePendingDesiredRevisionWithTask(ctx, pendingRevisionID)
	return err
}

func ReconcilePendingDesiredRevisionWithTask(
	ctx context.Context,
	pendingRevisionID uuid.UUID,
) (*types.Task, error) {
	gate, err := EvaluateObservationGate(ctx, pendingRevisionID)
	if err != nil {
		return nil, err
	}
	if gate.Status == types.ObservationGateStatusPending {
		return nil, nil
	}
	return AdvanceActiveDesiredRevisionWithTask(ctx, pendingRevisionID, gate)
}

func ReconcilePendingDesiredRevisionForComponent(
	ctx context.Context,
	organizationID, deploymentUnitID, componentInstanceID uuid.UUID,
) error {
	_, err := ReconcilePendingDesiredRevisionForComponentWithTask(
		ctx, organizationID, deploymentUnitID, componentInstanceID,
	)
	return err
}

func ReconcilePendingDesiredRevisionForComponentWithTask(
	ctx context.Context,
	organizationID, deploymentUnitID, componentInstanceID uuid.UUID,
) (*types.Task, error) {
	var pendingRevisionID *uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT pending_revision_id
		FROM ComponentDesiredStateHead
		WHERE organization_id = @organizationID
		  AND deployment_unit_id = @deploymentUnitID
		  AND component_instance_id = @componentInstanceID`,
		pgx.NamedArgs{
			"organizationID": organizationID, "deploymentUnitID": deploymentUnitID,
			"componentInstanceID": componentInstanceID,
		},
	).Scan(&pendingRevisionID)
	if errors.Is(err, pgx.ErrNoRows) || pendingRevisionID == nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not query component pending desired revision: %w", err)
	}
	return ReconcilePendingDesiredRevisionWithTask(ctx, *pendingRevisionID)
}

func EvaluateObservationGate(
	ctx context.Context,
	pendingRevisionID uuid.UUID,
) (types.ObservationGateResult, error) {
	pending, err := getPendingDesiredRevision(ctx, pendingRevisionID)
	if err != nil {
		return types.ObservationGateResult{}, err
	}
	return evaluateObservationGate(ctx, *pending, time.Now().UTC())
}

func evaluateObservationGate(
	ctx context.Context,
	pending types.PendingDesiredRevision,
	now time.Time,
) (types.ObservationGateResult, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+observedStateOutputExpr+`
		FROM ObservedComponentState o
		JOIN ObserverRegistration r
		  ON r.id = o.observer_id
		 AND r.organization_id = o.organization_id
		WHERE o.organization_id = @organizationID
		  AND o.deployment_unit_id = @deploymentUnitID
		  AND o.component_instance_id = @componentInstanceID
		  AND o.trusted = TRUE
		  AND o.disposition IN ('ACCEPTED', 'CONFLICT')
		  AND r.enabled = TRUE
		  AND o.captured_at >= @admittedAt
		  AND o.captured_at <= @observationDeadline
		ORDER BY o.observer_id, o.captured_at DESC, o.id DESC`,
		pgx.NamedArgs{
			"organizationID": pending.OrganizationID, "deploymentUnitID": pending.DeploymentUnitID,
			"componentInstanceID": pending.ComponentInstanceID, "admittedAt": pending.CreatedAt,
			"observationDeadline": pending.ObservationDeadline,
		},
	)
	if err != nil {
		return types.ObservationGateResult{}, fmt.Errorf(
			"could not query gate observations: %w", err,
		)
	}
	observations, err := pgx.CollectRows(
		rows, pgx.RowToStructByName[types.ObservedComponentState],
	)
	if err != nil {
		return types.ObservationGateResult{}, fmt.Errorf(
			"could not collect gate observations: %w", err,
		)
	}
	executorOutcome, err := getLatestExecutorOutcome(
		ctx,
		pending.OrganizationID,
		pending.ID,
		pending.ExecutionID,
	)
	if err != nil {
		return types.ObservationGateResult{}, err
	}
	for i := range observations {
		observations[i].ExecutorOutcome = executorOutcome
	}
	return observation.EvaluateGate(pending, observations, now), nil
}

func getLatestExecutorOutcome(
	ctx context.Context,
	organizationID, pendingRevisionID, executionID uuid.UUID,
) (types.ExecutorOutcome, error) {
	var outcome types.ExecutorOutcome
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT outcome
		FROM ExecutorReport
		WHERE organization_id = @organizationID
		  AND pending_revision_id = @pendingRevisionID
		  AND execution_id = @executionID
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
		pgx.NamedArgs{
			"organizationID":    organizationID,
			"pendingRevisionID": pendingRevisionID,
			"executionID":       executionID,
		},
	).Scan(&outcome)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("could not query latest ExecutorReport: %w", err)
	}
	return outcome, nil
}

func VerifyTrustedObservation(
	ctx context.Context,
	organizationID, observationID uuid.UUID,
	checksum string,
) error {
	var exists bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM ObservedComponentState
			WHERE organization_id = @organizationID
			  AND id = @observationID
			  AND runtime_state_checksum = @checksum
			  AND trusted = TRUE
			  AND is_current = TRUE
			  AND disposition = 'ACCEPTED'
			  AND outcome = 'COMPLETE'
			  AND health = 'HEALTHY'
			  AND fresh_until >= now()
			  AND length(btrim(evidence_reference)) > 0
		)`,
		pgx.NamedArgs{
			"organizationID": organizationID, "observationID": observationID,
			"checksum": checksum,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not verify trusted observation: %w", err)
	}
	if !exists {
		return apierrors.NewConflict("trusted observation identity or checksum does not match")
	}
	return nil
}

func ResolveTrustedCampaignObservation(
	ctx context.Context,
	organizationID, componentInstanceID uuid.UUID,
	expectedChecksum string,
) (uuid.UUID, string, error) {
	var observationID uuid.UUID
	var actualChecksum string
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id, runtime_state_checksum
		FROM ObservedComponentState
		WHERE organization_id = @organizationID
		  AND component_instance_id = @componentInstanceID
		  AND runtime_state_checksum = @expectedChecksum
		  AND trusted = TRUE
		  AND is_current = TRUE
		  AND disposition = 'ACCEPTED'
		  AND outcome = 'COMPLETE'
		  AND health = 'HEALTHY'
		  AND fresh_until >= now()
		  AND length(btrim(evidence_reference)) > 0
		ORDER BY captured_at DESC, id DESC
		LIMIT 1`,
		pgx.NamedArgs{
			"organizationID":      organizationID,
			"componentInstanceID": componentInstanceID,
			"expectedChecksum":    expectedChecksum,
		},
	).Scan(&observationID, &actualChecksum)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", apierrors.NewConflict(
			"trusted observation for canonical component placement does not match frozen checksum",
		)
	}
	if err != nil {
		return uuid.Nil, "", fmt.Errorf(
			"could not resolve trusted campaign observation: %w",
			err,
		)
	}
	return observationID, actualChecksum, nil
}

type CampaignObservationRepository struct{}

func (CampaignObservationRepository) VerifyTrustedObservation(
	ctx context.Context,
	organizationID, observationID uuid.UUID,
	checksum string,
) error {
	return VerifyTrustedObservation(ctx, organizationID, observationID, checksum)
}

func (CampaignObservationRepository) ResolveTrustedObservation(
	ctx context.Context,
	organizationID, componentInstanceID uuid.UUID,
	expectedChecksum string,
) (uuid.UUID, string, error) {
	return ResolveTrustedCampaignObservation(
		ctx,
		organizationID,
		componentInstanceID,
		expectedChecksum,
	)
}

func ListObservedComponentStates(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.ObservedComponentState, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+observedStateOutputExpr+`
		FROM ObservedComponentState o
		WHERE o.organization_id = @organizationID
		ORDER BY o.received_at DESC, o.id DESC
		LIMIT 200`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list observations: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[types.ObservedComponentState])
}

func getObserverRegistrationForUpdate(
	ctx context.Context,
	organizationID, observerID uuid.UUID,
) (*types.ObserverRegistration, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+observerRegistrationOutputExpr+`
		FROM ObserverRegistration r
		WHERE r.organization_id = @organizationID
		  AND r.id = @observerID
		FOR UPDATE`,
		pgx.NamedArgs{"organizationID": organizationID, "observerID": observerID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ObserverRegistration: %w", err)
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[observerRegistrationRow])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect ObserverRegistration: %w", err)
	}
	result := row.toType()
	return &result, nil
}

func getObservationHeadForUpdate(
	ctx context.Context,
	organizationID, observerID, deploymentUnitID, componentInstanceID uuid.UUID,
) (*types.ComponentObservationHead, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT organization_id, observer_id, deployment_unit_id,
			component_instance_id, source_sequence, observation_id,
			evidence_checksum, captured_at
		FROM ComponentObservationHead
		WHERE organization_id = @organizationID
		  AND observer_id = @observerID
		  AND deployment_unit_id = @deploymentUnitID
		  AND component_instance_id = @componentInstanceID
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationID": organizationID, "observerID": observerID,
			"deploymentUnitID": deploymentUnitID, "componentInstanceID": componentInstanceID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ComponentObservationHead: %w", err)
	}
	head, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.ComponentObservationHead],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect ComponentObservationHead: %w", err)
	}
	return &head, nil
}

func getObservationReplay(
	ctx context.Context,
	envelope types.ObservationEnvelope,
) (*types.ObservedComponentState, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+observedStateOutputExpr+`
		FROM ObservedComponentState o
		WHERE o.organization_id = @organizationID
		  AND o.observer_id = @observerID
		  AND o.source_sequence = @sourceSequence
		  AND o.evidence_checksum = @evidenceChecksum`,
		pgx.NamedArgs{
			"organizationID": envelope.OrganizationID, "observerID": envelope.ObserverID,
			"sourceSequence":   envelope.SourceSequence,
			"evidenceChecksum": envelope.EvidenceChecksum,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query observation replay: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.ObservedComponentState],
	)
	if err != nil {
		return nil, fmt.Errorf("could not collect observation replay: %w", err)
	}
	return &value, nil
}

func getObservedComponentState(
	ctx context.Context,
	organizationID, observationID uuid.UUID,
) (*types.ObservedComponentState, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+observedStateOutputExpr+`
		FROM ObservedComponentState o
		WHERE o.organization_id = @organizationID
		  AND o.id = @observationID
		  AND o.trusted = TRUE
		  AND o.is_current = TRUE`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"observationID":  observationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query verified observation: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.ObservedComponentState],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.NewConflict("verified observation is not current and trusted")
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect verified observation: %w", err)
	}
	return &value, nil
}

func getObservedEvidence(
	ctx context.Context,
	organizationID, observationID uuid.UUID,
) (*types.ObservedComponentState, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+observedStateOutputExpr+`
		FROM ObservedComponentState o
		WHERE o.organization_id = @organizationID
		  AND o.id = @observationID`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"observationID":  observationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query observation evidence: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.ObservedComponentState],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect observation evidence: %w", err)
	}
	return &value, nil
}

func observationStateChecksum(envelope types.ObservationEnvelope) (string, error) {
	payload, err := json.Marshal(struct {
		OrganizationID      uuid.UUID                `json:"organizationId"`
		ObserverID          uuid.UUID                `json:"observerId"`
		DeploymentUnitID    uuid.UUID                `json:"deploymentUnitId"`
		ComponentInstanceID uuid.UUID                `json:"componentInstanceId"`
		ComponentKey        string                   `json:"componentKey"`
		SourceSequence      int64                    `json:"sourceSequence"`
		CapturedAt          time.Time                `json:"capturedAt"`
		EvidenceChecksum    string                   `json:"evidenceChecksum"`
		EvidenceReference   string                   `json:"evidenceReference"`
		ArtifactDigest      string                   `json:"artifactDigest"`
		ConfigChecksum      string                   `json:"configChecksum"`
		SchemaVersion       string                   `json:"schemaVersion"`
		CapabilityChecksum  string                   `json:"capabilityChecksum"`
		Platform            string                   `json:"platform"`
		TopologyChecksum    string                   `json:"topologyChecksum"`
		Health              types.ObservedHealth     `json:"health"`
		Outcome             types.ObservationOutcome `json:"outcome"`
	}{
		envelope.OrganizationID, envelope.ObserverID, envelope.DeploymentUnitID,
		envelope.ComponentInstanceID, envelope.ComponentKey, envelope.SourceSequence,
		envelope.CapturedAt.UTC(), envelope.EvidenceChecksum, envelope.EvidenceReference,
		envelope.ArtifactDigest, envelope.ConfigChecksum, envelope.SchemaVersion,
		envelope.CapabilityChecksum, envelope.Platform, envelope.TopologyChecksum,
		envelope.Health, envelope.Outcome,
	})
	if err != nil {
		return "", fmt.Errorf("could not canonicalize observed state: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func getPendingDesiredRevision(
	ctx context.Context,
	id uuid.UUID,
) (*types.PendingDesiredRevision, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+pendingDesiredOutputExpr+`
		FROM PendingDesiredRevision p
		WHERE p.id = @pendingRevisionID`,
		pgx.NamedArgs{"pendingRevisionID": id},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query PendingDesiredRevision: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.PendingDesiredRevision],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect PendingDesiredRevision: %w", err)
	}
	return &value, nil
}

func getPendingDesiredRevisionForUpdate(
	ctx context.Context,
	id uuid.UUID,
) (*types.PendingDesiredRevision, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+pendingDesiredOutputExpr+`
		FROM PendingDesiredRevision p
		WHERE p.id = @pendingRevisionID
		FOR UPDATE`,
		pgx.NamedArgs{"pendingRevisionID": id},
	)
	if err != nil {
		return nil, fmt.Errorf("could not lock PendingDesiredRevision: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.PendingDesiredRevision],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect PendingDesiredRevision: %w", err)
	}
	return &value, nil
}

func getActiveDesiredRevision(
	ctx context.Context,
	organizationID, id uuid.UUID,
) (*types.ActiveDesiredRevision, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+activeDesiredOutputExpr+`
		FROM ActiveDesiredRevision a
		WHERE a.organization_id = @organizationID
		  AND a.id = @activeRevisionID`,
		pgx.NamedArgs{"organizationID": organizationID, "activeRevisionID": id},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ActiveDesiredRevision: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.ActiveDesiredRevision],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect ActiveDesiredRevision: %w", err)
	}
	return &value, nil
}

func getDesiredHeadForUpdate(
	ctx context.Context,
	organizationID, deploymentUnitID, componentInstanceID uuid.UUID,
) (*types.ComponentDesiredStateHead, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT organization_id, deployment_unit_id, component_instance_id,
			component_key, pending_revision_id, active_revision_id,
			quarantined, quarantine_reason, updated_at
		FROM ComponentDesiredStateHead
		WHERE organization_id = @organizationID
		  AND deployment_unit_id = @deploymentUnitID
		  AND component_instance_id = @componentInstanceID
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationID": organizationID, "deploymentUnitID": deploymentUnitID,
			"componentInstanceID": componentInstanceID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ComponentDesiredStateHead: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.ComponentDesiredStateHead],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect ComponentDesiredStateHead: %w", err)
	}
	return &value, nil
}

type driftCaseRow struct {
	ID                      uuid.UUID             `db:"id"`
	CreatedAt               time.Time             `db:"created_at"`
	UpdatedAt               time.Time             `db:"updated_at"`
	OrganizationID          uuid.UUID             `db:"organization_id"`
	ActiveDesiredRevisionID uuid.UUID             `db:"active_desired_revision_id"`
	ObservationID           uuid.UUID             `db:"observation_id"`
	DeploymentUnitID        uuid.UUID             `db:"deployment_unit_id"`
	ComponentInstanceID     uuid.UUID             `db:"component_instance_id"`
	Status                  types.DriftCaseStatus `db:"status"`
	Classes                 []string              `db:"classes"`
	Summary                 string                `db:"summary"`
	AssignedTo              *uuid.UUID            `db:"assigned_to"`
	ResolvedAt              *time.Time            `db:"resolved_at"`
}

func (row driftCaseRow) toType() types.DriftCase {
	classes := make([]types.DriftClass, len(row.Classes))
	for i, class := range row.Classes {
		classes[i] = types.DriftClass(class)
	}
	return types.DriftCase{
		ID: row.ID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		OrganizationID:          row.OrganizationID,
		ActiveDesiredRevisionID: row.ActiveDesiredRevisionID,
		ObservationID:           row.ObservationID, DeploymentUnitID: row.DeploymentUnitID,
		ComponentInstanceID: row.ComponentInstanceID, Status: row.Status, Classes: classes,
		Summary: row.Summary, AssignedTo: row.AssignedTo, ResolvedAt: row.ResolvedAt,
	}
}

const driftCaseOutputExpr = `
	d.id, d.created_at, d.updated_at, d.organization_id,
	d.active_desired_revision_id, d.observation_id,
	d.deployment_unit_id, d.component_instance_id, d.status,
	d.classes, d.summary, d.assigned_to, d.resolved_at
`

func OpenDriftCase(
	ctx context.Context,
	input types.DriftInput,
) (*types.DriftCase, error) {
	if input.OrganizationID == uuid.Nil || input.ActiveDesiredRevisionID == uuid.Nil ||
		input.ObservationID == uuid.Nil || !input.Classification.Drifted ||
		len(input.Classification.Classes) == 0 {
		return nil, apierrors.NewBadRequest("drift input is invalid")
	}
	classes := make([]string, len(input.Classification.Classes))
	for i, class := range input.Classification.Classes {
		classes[i] = string(class)
	}
	var result *types.DriftCase
	err := RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		rows, err := internalctx.GetDb(txCtx).Query(txCtx, `
			INSERT INTO DriftCase AS d (
				id, organization_id, active_desired_revision_id,
				observation_id, deployment_unit_id, component_instance_id,
				status, classes, summary
			)
			SELECT
				@id, @organizationID, @activeDesiredRevisionID,
				@observationID, a.deployment_unit_id, a.component_instance_id,
				@status, @classes, @summary
			FROM ActiveDesiredRevision a
			JOIN ObservedComponentState o
			  ON o.id = @observationID
			 AND o.organization_id = a.organization_id
			 AND o.deployment_unit_id = a.deployment_unit_id
			 AND o.component_instance_id = a.component_instance_id
			WHERE a.id = @activeDesiredRevisionID
			  AND a.organization_id = @organizationID
			ON CONFLICT (
				organization_id, active_desired_revision_id, observation_id
			) WHERE status IN ('OPEN', 'ASSIGNED', 'EXCEPTION')
			DO NOTHING
			RETURNING `+driftCaseOutputExpr,
			pgx.NamedArgs{
				"id": uuid.New(), "organizationID": input.OrganizationID,
				"activeDesiredRevisionID": input.ActiveDesiredRevisionID,
				"observationID":           input.ObservationID,
				"status":                  types.DriftCaseStatusOpen, "classes": classes,
				"summary": input.Classification.Summary,
			},
		)
		if err != nil {
			return fmt.Errorf("could not open DriftCase: %w", err)
		}
		row, err := pgx.CollectExactlyOneRow(
			rows, pgx.RowToStructByName[driftCaseRow],
		)
		if errors.Is(err, pgx.ErrNoRows) {
			existingRows, queryErr := internalctx.GetDb(txCtx).Query(txCtx, `
				SELECT `+driftCaseOutputExpr+`
				FROM DriftCase d
				WHERE d.organization_id = @organizationID
				  AND d.active_desired_revision_id = @activeDesiredRevisionID
				  AND d.observation_id = @observationID
				  AND d.status IN ('OPEN', 'ASSIGNED', 'EXCEPTION')`,
				pgx.NamedArgs{
					"organizationID":          input.OrganizationID,
					"activeDesiredRevisionID": input.ActiveDesiredRevisionID,
					"observationID":           input.ObservationID,
				},
			)
			if queryErr != nil {
				return fmt.Errorf("could not query existing DriftCase: %w", queryErr)
			}
			existing, queryErr := pgx.CollectExactlyOneRow(
				existingRows, pgx.RowToStructByName[driftCaseRow],
			)
			if queryErr == nil {
				value := existing.toType()
				result = &value
				return nil
			}
			if !errors.Is(queryErr, pgx.ErrNoRows) {
				return fmt.Errorf("could not collect existing DriftCase: %w", queryErr)
			}
			return apierrors.NewConflict("drift desired and observed state do not share a placement")
		}
		if err != nil {
			return fmt.Errorf("could not collect DriftCase: %w", err)
		}
		value := row.toType()
		_, err = internalctx.GetDb(txCtx).Exec(txCtx, `
			INSERT INTO DriftCaseEvent (
				id, organization_id, drift_case_id, status, reason
			) VALUES (
				@id, @organizationID, @driftCaseID, @status, @reason
			)`,
			pgx.NamedArgs{
				"id": uuid.New(), "organizationID": value.OrganizationID,
				"driftCaseID": value.ID, "status": value.Status,
				"reason": strings.TrimSpace(input.Reason),
			},
		)
		if err != nil {
			return fmt.Errorf("could not append DriftCaseEvent: %w", err)
		}
		if err := appendDesiredObservedAudit(txCtx, true,
			types.ControlPlaneAuditEventInput{
				OrganizationID:   value.OrganizationID,
				EventType:        "drift_case.opened",
				Outcome:          string(value.Status),
				DesiredStateID:   &value.ActiveDesiredRevisionID,
				ObservationID:    &value.ObservationID,
				DriftCaseID:      &value.ID,
				DeploymentUnitID: &value.DeploymentUnitID,
			},
		); err != nil {
			return fmt.Errorf("could not audit DriftCase: %w", err)
		}
		result = &value
		return nil
	})
	return result, err
}

func ResolveDriftCase(
	ctx context.Context,
	decision types.ReconciliationDecision,
) error {
	if decision.OrganizationID == uuid.Nil || decision.DriftCaseID == uuid.Nil ||
		decision.ActorID == uuid.Nil {
		return apierrors.NewBadRequest("reconciliation decision is invalid")
	}
	now := time.Now().UTC()
	nextStatus, err := reconciliation.DecisionTargetStatus(decision, now)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	return RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		var status types.DriftCaseStatus
		var activeDesiredRevisionID uuid.UUID
		err := internalctx.GetDb(txCtx).QueryRow(txCtx, `
			SELECT status, active_desired_revision_id
			FROM DriftCase
			WHERE organization_id = @organizationID
			  AND id = @driftCaseID
			FOR UPDATE`,
			pgx.NamedArgs{
				"organizationID": decision.OrganizationID,
				"driftCaseID":    decision.DriftCaseID,
			},
		).Scan(&status, &activeDesiredRevisionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("could not lock DriftCase: %w", err)
		}
		var replay bool
		err = internalctx.GetDb(txCtx).QueryRow(txCtx, `
			SELECT EXISTS (
				SELECT 1
				FROM ReconciliationAction
				WHERE organization_id = @organizationID
				  AND drift_case_id = @driftCaseID
				  AND action = @action
				  AND reason = @reason
				  AND actor_id = @actorID
				  AND deployment_plan_id IS NOT DISTINCT FROM @deploymentPlanID
				  AND outcome_observation_id IS NOT DISTINCT FROM @outcomeObservationID
				  AND accepted_until IS NOT DISTINCT FROM @acceptedUntil
			)`, pgx.NamedArgs{
			"organizationID":       decision.OrganizationID,
			"driftCaseID":          decision.DriftCaseID,
			"action":               decision.Action,
			"reason":               strings.TrimSpace(decision.Reason),
			"actorID":              decision.ActorID,
			"deploymentPlanID":     decision.DeploymentPlanID,
			"outcomeObservationID": decision.OutcomeObservationID,
			"acceptedUntil":        decision.AcceptedUntil,
		}).Scan(&replay)
		if err != nil {
			return fmt.Errorf("could not check ReconciliationAction replay: %w", err)
		}
		if replay {
			return nil
		}
		if status == types.DriftCaseStatusResolved {
			return apierrors.NewConflict("drift case is already resolved")
		}
		if nextStatus == types.DriftCaseStatusResolved {
			active, err := getActiveDesiredRevision(
				txCtx,
				decision.OrganizationID,
				activeDesiredRevisionID,
			)
			if err != nil {
				return err
			}
			observed, err := getObservedComponentState(
				txCtx,
				decision.OrganizationID,
				*decision.OutcomeObservationID,
			)
			if err != nil {
				return err
			}
			if observed.DeploymentUnitID != active.DeploymentUnitID ||
				observed.ComponentInstanceID != active.ComponentInstanceID ||
				observed.Disposition != types.ObservationDispositionAccepted ||
				observed.Outcome != types.ObservationOutcomeComplete ||
				observed.Health != types.ObservedHealthHealthy ||
				reconciliation.ClassifyDriftAt(*active, *observed, now).Drifted {
				return apierrors.NewConflict(
					"reconciliation outcome does not prove restored active desired state",
				)
			}
		}
		actionID := uuid.New()
		_, err = internalctx.GetDb(txCtx).Exec(txCtx, `
			INSERT INTO ReconciliationAction (
				id, organization_id, drift_case_id, action, reason,
				actor_id, deployment_plan_id, outcome_observation_id,
				accepted_until
			) VALUES (
				@id, @organizationID, @driftCaseID, @action, @reason,
				@actorID, @deploymentPlanID, @outcomeObservationID,
				@acceptedUntil
			)`,
			pgx.NamedArgs{
				"id": actionID, "organizationID": decision.OrganizationID,
				"driftCaseID": decision.DriftCaseID, "action": decision.Action,
				"reason": strings.TrimSpace(decision.Reason), "actorID": decision.ActorID,
				"deploymentPlanID":     decision.DeploymentPlanID,
				"outcomeObservationID": decision.OutcomeObservationID,
				"acceptedUntil":        decision.AcceptedUntil,
			},
		)
		if err != nil {
			return fmt.Errorf("could not insert ReconciliationAction: %w", err)
		}
		_, err = internalctx.GetDb(txCtx).Exec(txCtx, `
			UPDATE DriftCase
			SET status = @status, updated_at = now(),
				assigned_to = CASE
					WHEN @status = 'ASSIGNED' THEN @actorID
					ELSE assigned_to
				END,
				resolved_at = CASE WHEN @status = 'RESOLVED' THEN now() ELSE NULL END
			WHERE organization_id = @organizationID
			  AND id = @driftCaseID`,
			pgx.NamedArgs{
				"status": nextStatus, "organizationID": decision.OrganizationID,
				"driftCaseID": decision.DriftCaseID, "actorID": decision.ActorID,
			},
		)
		if err != nil {
			return fmt.Errorf("could not update DriftCase: %w", err)
		}
		_, err = internalctx.GetDb(txCtx).Exec(txCtx, `
			INSERT INTO DriftCaseEvent (
				id, organization_id, drift_case_id, status, actor_id, reason
			) VALUES (
				@id, @organizationID, @driftCaseID, @status, @actorID, @reason
			)`,
			pgx.NamedArgs{
				"id": uuid.New(), "organizationID": decision.OrganizationID,
				"driftCaseID": decision.DriftCaseID, "status": nextStatus,
				"actorID": decision.ActorID, "reason": strings.TrimSpace(decision.Reason),
			},
		)
		if err != nil {
			return fmt.Errorf("could not append DriftCaseEvent: %w", err)
		}
		actionAudit := types.ControlPlaneAuditEventInput{
			OrganizationID:   decision.OrganizationID,
			EventType:        "reconciliation.action_recorded",
			ActorID:          &decision.ActorID,
			Outcome:          string(decision.Action),
			DeploymentPlanID: decision.DeploymentPlanID,
			DesiredStateID:   &activeDesiredRevisionID,
			ObservationID:    decision.OutcomeObservationID,
			DriftCaseID:      &decision.DriftCaseID,
			ReconciliationID: &actionID,
		}
		if err := appendDesiredObservedAudit(
			txCtx, true, actionAudit,
		); err != nil {
			return fmt.Errorf("could not audit ReconciliationAction: %w", err)
		}
		stateEventType := "drift_case.assigned"
		if nextStatus == types.DriftCaseStatusResolved {
			stateEventType = "drift_case.resolved"
		} else if nextStatus == types.DriftCaseStatusException {
			stateEventType = "drift_case.deviation_accepted"
		}
		stateAudit := actionAudit
		stateAudit.EventType = stateEventType
		stateAudit.Outcome = string(nextStatus)
		if err := appendDesiredObservedAudit(
			txCtx, true, stateAudit,
		); err != nil {
			return fmt.Errorf("could not audit DriftCase transition: %w", err)
		}
		return nil
	})
}

func ListDriftCases(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.DriftCase, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+driftCaseOutputExpr+`
		FROM DriftCase d
		WHERE d.organization_id = @organizationID
		ORDER BY d.updated_at DESC, d.id DESC
		LIMIT 200`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list DriftCase: %w", err)
	}
	records, err := pgx.CollectRows(rows, pgx.RowToStructByName[driftCaseRow])
	if err != nil {
		return nil, fmt.Errorf("could not collect DriftCase: %w", err)
	}
	result := make([]types.DriftCase, len(records))
	for i, record := range records {
		result[i] = record.toType()
	}
	return result, nil
}

func ListReconciliationActions(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.ReconciliationAction, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT id, created_at, organization_id, drift_case_id,
			action, reason, actor_id, deployment_plan_id,
			outcome_observation_id, accepted_until
		FROM ReconciliationAction
		WHERE organization_id = @organizationID
		ORDER BY created_at DESC, id DESC
		LIMIT 200`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list ReconciliationAction: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[types.ReconciliationAction])
}

func ClassifyAndOpenDriftCase(
	ctx context.Context,
	desired types.ActiveDesiredRevision,
	observed types.ObservedComponentState,
	reason string,
) (*types.DriftCase, error) {
	classification := reconciliation.ClassifyDriftAt(
		desired, observed, time.Now().UTC(),
	)
	if !classification.Drifted {
		return nil, nil
	}
	return OpenDriftCase(ctx, types.DriftInput{
		OrganizationID:          desired.OrganizationID,
		ActiveDesiredRevisionID: desired.ID,
		ObservationID:           observed.ID,
		Classification:          classification,
		Reason:                  reason,
	})
}
