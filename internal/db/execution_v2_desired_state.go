package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func admitExecutionDesiredStateTx(
	ctx context.Context,
	attempt types.ExecutionAttempt,
	now time.Time,
) (*types.PendingDesiredRevision, error) {
	var planID uuid.UUID
	var planChecksum string
	var canonicalPayload []byte
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT t.deployment_plan_id, dp.canonical_checksum, dp.canonical_payload
		FROM Task t
		JOIN DeploymentPlan dp
		  ON dp.id = t.deployment_plan_id
		 AND dp.organization_id = t.organization_id
		WHERE t.id = @taskID
		  AND t.organization_id = @organizationID
		  AND t.deployment_target_id = @deploymentTargetID
		  AND t.protocol_version = 'v2'
		  AND dp.protocol_version = 'v2'
		  AND dp.plan_schema = @planSchema
		FOR SHARE`,
		pgx.NamedArgs{
			"taskID": attempt.TaskID, "organizationID": attempt.OrganizationID,
			"deploymentTargetID": attempt.DeploymentTargetID,
			"planSchema":         types.TargetDeploymentPlanSchemaV2,
		},
	).Scan(&planID, &planChecksum, &canonicalPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("execution-v2 attempt is not backed by a target deployment plan")
	}
	if err != nil {
		return nil, fmt.Errorf("load execution-v2 target deployment plan: %w", err)
	}
	if attempt.PlanChecksum != planChecksum {
		return nil, fmt.Errorf("execution-v2 attempt plan checksum does not match the frozen plan")
	}
	var canonical types.TargetDeploymentPlanCanonical
	if err := json.Unmarshal(canonicalPayload, &canonical); err != nil {
		return nil, fmt.Errorf("decode frozen target deployment plan: %w", err)
	}
	input, err := pendingDesiredInputForExecutionAttempt(attempt, planID, canonical, now)
	if err != nil || input == nil {
		return nil, err
	}
	return admitPendingDesiredRevisionTx(ctx, *input, now)
}

func pendingDesiredInputForExecutionAttempt(
	attempt types.ExecutionAttempt,
	planID uuid.UUID,
	canonical types.TargetDeploymentPlanCanonical,
	now time.Time,
) (*types.PendingDesiredRevisionInput, error) {
	if canonical.Schema != types.TargetDeploymentPlanSchemaV2 ||
		canonical.ProtocolVersion != types.DeploymentPlanProtocolV2 {
		return nil, fmt.Errorf("execution-v2 desired state requires a canonical target plan")
	}
	if canonical.DeploymentUnitID == uuid.Nil ||
		canonical.DeploymentTargetID != attempt.DeploymentTargetID {
		return nil, fmt.Errorf("execution-v2 desired state placement does not match the attempt")
	}
	if canonical.TargetConfigSnapshotChecksum != attempt.ConfigChecksum {
		return nil, fmt.Errorf("execution-v2 desired state config checksum does not match the attempt")
	}
	step, ok := findTargetPlanStep(canonical.Graph.Steps, attempt.Identity.StepKey)
	if !ok {
		return nil, fmt.Errorf("execution-v2 attempt step is absent from the frozen plan")
	}
	if step.ActionName != "component.deploy" {
		return nil, nil
	}
	if step.ComponentInstanceID == nil || *step.ComponentInstanceID == uuid.Nil ||
		strings.TrimSpace(step.ComponentKey) == "" || step.TimeoutSeconds <= 0 {
		return nil, fmt.Errorf("component deploy step has incomplete desired-state identity")
	}
	pin, ok := findComponentReleasePin(canonical.ComponentReleasePins, step.ComponentKey)
	if !ok {
		return nil, fmt.Errorf("component deploy step has no frozen release pin")
	}
	binding, ok := findComponentBinding(canonical.ComponentBindings, step.ComponentKey)
	if !ok || binding.ComponentInstanceID != *step.ComponentInstanceID {
		return nil, fmt.Errorf("component deploy step has no matching frozen component binding")
	}
	if pin.PlatformDigest != attempt.ArtifactDigest {
		return nil, fmt.Errorf("component deploy artifact digest does not match the attempt")
	}
	if !slices.Contains(pin.Platforms, canonical.TargetPlatform) {
		return nil, fmt.Errorf("component deploy platform is absent from the frozen release pin")
	}
	topologyChecksum, err := desiredTopologyChecksum(canonical, binding)
	if err != nil {
		return nil, err
	}
	return &types.PendingDesiredRevisionInput{
		OrganizationID: attempt.OrganizationID, DeploymentPlanID: planID,
		ExecutionID: attempt.Identity.ExecutionID, ExecutionAttemptID: attempt.ID,
		DeploymentUnitID:    canonical.DeploymentUnitID,
		ComponentInstanceID: binding.ComponentInstanceID,
		ComponentKey:        strings.TrimSpace(step.ComponentKey),
		ArtifactDigest:      pin.PlatformDigest, ConfigChecksum: attempt.ConfigChecksum,
		SchemaVersion:      strings.TrimSpace(pin.Version),
		CapabilityChecksum: pin.ReleaseChecksum,
		Platform:           canonical.TargetPlatform, TopologyChecksum: topologyChecksum,
		ObservationDeadline: now.UTC().Add(time.Duration(step.TimeoutSeconds) * time.Second),
	}, nil
}

func findTargetPlanStep(steps []types.TargetPlanStep, key string) (types.TargetPlanStep, bool) {
	for _, step := range steps {
		if step.StepKey == key {
			return step, true
		}
	}
	return types.TargetPlanStep{}, false
}

func findComponentReleasePin(
	pins []types.ComponentReleasePin,
	componentKey string,
) (types.ComponentReleasePin, bool) {
	for _, pin := range pins {
		if pin.ComponentKey == componentKey {
			return pin, true
		}
	}
	return types.ComponentReleasePin{}, false
}

func findComponentBinding(
	bindings []types.ConfigComponentBinding,
	componentKey string,
) (types.ConfigComponentBinding, bool) {
	for _, binding := range bindings {
		if binding.ComponentKey == componentKey {
			return binding, true
		}
	}
	return types.ConfigComponentBinding{}, false
}

func desiredTopologyChecksum(
	canonical types.TargetDeploymentPlanCanonical,
	binding types.ConfigComponentBinding,
) (string, error) {
	payload, err := json.Marshal(struct {
		DeploymentScopeID       uuid.UUID `json:"deploymentScopeId"`
		DeploymentUnitID        uuid.UUID `json:"deploymentUnitId"`
		EnvironmentAssignmentID uuid.UUID `json:"environmentAssignmentId"`
		DeploymentTargetID      uuid.UUID `json:"deploymentTargetId"`
		ComponentKey            string    `json:"componentKey"`
		ComponentInstanceID     uuid.UUID `json:"componentInstanceId"`
		PhysicalName            string    `json:"physicalName"`
		Platform                string    `json:"platform"`
	}{
		canonical.DeploymentScopeID, canonical.DeploymentUnitID,
		canonical.EnvironmentAssignmentID, canonical.DeploymentTargetID,
		binding.ComponentKey, binding.ComponentInstanceID,
		strings.TrimSpace(binding.PhysicalName), canonical.TargetPlatform,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize desired topology: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func executorOutcomeForAttemptStatus(
	status types.ExecutionAttemptStatus,
) (types.ExecutorOutcome, error) {
	switch status {
	case types.ExecutionAttemptStatusSucceeded:
		return types.ExecutorOutcomeSucceeded, nil
	case types.ExecutionAttemptStatusFailed:
		return types.ExecutorOutcomeFailed, nil
	case types.ExecutionAttemptStatusCanceled:
		return types.ExecutorOutcomeCancelled, nil
	case types.ExecutionAttemptStatusTimedOut:
		return types.ExecutorOutcomeFailed, nil
	case types.ExecutionAttemptStatusFenced,
		types.ExecutionAttemptStatusUnknown:
		return types.ExecutorOutcomeUnknown, nil
	default:
		return "", fmt.Errorf("execution attempt status %q is not terminal", status)
	}
}

func executionAttemptStatusForReconciliationOutcome(
	outcome types.ReconciliationOutcome,
) (types.ExecutionAttemptStatus, error) {
	switch outcome {
	case types.ReconciliationOutcomeProvenSucceeded:
		return types.ExecutionAttemptStatusSucceeded, nil
	case types.ReconciliationOutcomeProvenFailed:
		return types.ExecutionAttemptStatusFailed, nil
	case types.ReconciliationOutcomeUnknown:
		return types.ExecutionAttemptStatusUnknown, nil
	default:
		return "", fmt.Errorf("reconciliation outcome %q is invalid", outcome)
	}
}

func executionProjectionStatusForDesiredTerminal(
	status types.PendingDesiredStatus,
) (types.ExecutionAttemptStatus, error) {
	switch status {
	case types.PendingDesiredStatusVerified:
		return types.ExecutionAttemptStatusSucceeded, nil
	case types.PendingDesiredStatusCancelled:
		return types.ExecutionAttemptStatusCanceled, nil
	case types.PendingDesiredStatusUnknown:
		return types.ExecutionAttemptStatusUnknown, nil
	case types.PendingDesiredStatusPartial,
		types.PendingDesiredStatusFailed,
		types.PendingDesiredStatusTimedOut,
		types.PendingDesiredStatusConflict:
		return types.ExecutionAttemptStatusFailed, nil
	default:
		return "", fmt.Errorf("desired revision status %q is not terminal", status)
	}
}

func recordExecutionAttemptReportsTx(
	ctx context.Context,
	attemptID, organizationID uuid.UUID,
	outcome types.ExecutorOutcome,
	evidenceReference string,
) ([]uuid.UUID, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT id, execution_id
		FROM PendingDesiredRevision
		WHERE execution_attempt_id = @attemptID
		  AND organization_id = @organizationID
		ORDER BY id
		FOR UPDATE`,
		pgx.NamedArgs{"attemptID": attemptID, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("load pending desired revisions for executor report: %w", err)
	}
	type pendingLineage struct {
		ID          uuid.UUID `db:"id"`
		ExecutionID uuid.UUID `db:"execution_id"`
	}
	pending, err := pgx.CollectRows(rows, pgx.RowToStructByName[pendingLineage])
	if err != nil {
		return nil, fmt.Errorf("collect pending desired revisions for executor report: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(pending))
	for _, revision := range pending {
		reportID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(
			"distr:executor-report:"+attemptID.String()+":"+revision.ID.String(),
		))
		command, err := internalctx.GetDb(ctx).Exec(ctx, `
			INSERT INTO ExecutorReport (
				id, organization_id, pending_revision_id, execution_id,
				outcome, evidence_reference
			) VALUES (
				@id, @organizationID, @pendingRevisionID, @executionID,
				@outcome, @evidenceReference
			)
			ON CONFLICT (id) DO NOTHING`,
			pgx.NamedArgs{
				"id": reportID, "organizationID": organizationID,
				"pendingRevisionID": revision.ID, "executionID": revision.ExecutionID,
				"outcome": outcome, "evidenceReference": strings.TrimSpace(evidenceReference),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("record execution-v2 executor report: %w", err)
		}
		if command.RowsAffected() == 0 {
			var existingOutcome types.ExecutorOutcome
			err = internalctx.GetDb(ctx).QueryRow(ctx, `
				SELECT outcome
				FROM ExecutorReport
				WHERE id = @id AND organization_id = @organizationID`,
				pgx.NamedArgs{"id": reportID, "organizationID": organizationID},
			).Scan(&existingOutcome)
			if err != nil {
				return nil, fmt.Errorf("validate existing execution-v2 executor report: %w", err)
			}
			if existingOutcome != outcome {
				return nil, fmt.Errorf("execution-v2 terminal outcome conflicts with its existing report")
			}
		}
		ids = append(ids, revision.ID)
	}
	return ids, nil
}

func reconcilePendingDesiredRevisionIDs(
	ctx context.Context,
	ids []uuid.UUID,
) (*types.Task, error) {
	var projected *types.Task
	for _, id := range ids {
		task, err := ReconcilePendingDesiredRevisionWithTask(ctx, id)
		if err != nil {
			return nil, err
		}
		if task == nil {
			continue
		}
		if projected != nil && projected.ID != task.ID {
			return nil, fmt.Errorf("desired revisions projected more than one execution task")
		}
		projected = task
	}
	return projected, nil
}
