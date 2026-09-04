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
	"github.com/distr-sh/distr/internal/reviewadmission"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const reviewAdmissionOutputExpr = `
	d.id, d.created_at, d.organization_id, d.deployment_plan_id,
	d.plan_revision, d.plan_checksum, d.review_material_checksum,
	d.observed_state_checksum, d.decision, d.reason,
	d.actor_useraccount_id, d.expires_at, d.supersedes_decision_id,
	d.revokes_decision_id, d.authorization_evidence,
	d.canonical_checksum, d.idempotency_key
`

func CreateReviewAdmissionDecision(
	ctx context.Context,
	request types.CreateReviewAdmissionDecisionRequest,
) (*types.ReviewAdmissionDecisionRecord, error) {
	if err := validateReviewAdmissionRequest(request); err != nil {
		return nil, err
	}
	var result *types.ReviewAdmissionDecisionRecord
	err := RunTxIso(ctx, pgx.Serializable, func(txCtx context.Context) error {
		if err := lockReviewAdmissionPlan(
			txCtx, request.OrganizationID, request.DeploymentPlanID,
		); err != nil {
			return err
		}
		snapshot, err := getAdmissionPlanSnapshot(
			txCtx, request.DeploymentPlanID, request.OrganizationID,
		)
		if err != nil {
			return err
		}
		decisionAt, err := admissionDatabaseTime(txCtx)
		if err != nil {
			return err
		}
		if err := authorizeAdmission(txCtx, request.Authorize, types.AdmissionAuthorizationContext{
			OrganizationID: request.OrganizationID, ActorUserAccountID: request.ActorUserAccountID,
			DeploymentPlanID: request.DeploymentPlanID, EnvironmentID: snapshot.EnvironmentID,
			DeploymentUnitID: snapshot.DeploymentUnitID, Action: "plan.execute", DecisionAt: decisionAt,
		}); err != nil {
			return err
		}
		if snapshot.Plan.CanonicalChecksum != request.ExpectedPlanChecksum {
			return apierrors.NewConflict("deployment plan checksum changed before review decision")
		}
		observed, err := loadCurrentPlanObservedStateMaterial(
			txCtx, request.OrganizationID, request.DeploymentPlanID, decisionAt, true,
		)
		if err != nil {
			return err
		}
		if !observed.Complete {
			return apierrors.NewConflict("deployment plan current observed state set is incomplete")
		}
		observedChecksum := observed.Checksum
		materialChecksum := reviewadmission.ReviewMaterialChecksum(
			snapshot.Plan.CanonicalChecksum, observedChecksum,
		)
		if observedChecksum != request.ObservedStateChecksum || materialChecksum != request.ReviewMaterialChecksum {
			return apierrors.NewConflict("observed state changed before review decision")
		}
		admission, err := getLatestReviewAdmissionEvaluationMaterial(
			txCtx, request.OrganizationID, request.DeploymentPlanID,
		)
		if err != nil {
			return apierrors.NewConflict("deployment plan has no current ADMIT evaluation")
		}
		approval, err := requireCurrentDeploymentPlanApprovalForExecution(
			txCtx, request.OrganizationID, request.DeploymentPlanID, admission.ActorUserAccountID,
		)
		if err != nil {
			return err
		}
		if !currentReviewAdmissionEvaluation(*admission, snapshot.Plan, observed.LatestReceivedAt, true) {
			return apierrors.NewConflict("latest deployment admission is stale or not ADMIT")
		}
		if err := validateExactReviewAdmissionBinding(
			*admission, admission.ID, admission.DecisionChecksum, approval.ID, approval.Revision,
		); err != nil {
			return err
		}
		existing, existingErr := getReviewDecisionByIdempotencyKey(
			txCtx, request.OrganizationID, request.DeploymentPlanID, request.IdempotencyKey,
		)
		if existingErr == nil {
			if reviewDecisionMatchesRequest(*existing, request, snapshot, materialChecksum, observedChecksum) {
				result = existing
				return nil
			}
			return apierrors.NewConflict("review decision idempotency key was reused for different material")
		}
		if !errors.Is(existingErr, pgx.ErrNoRows) {
			return existingErr
		}
		latest, err := getLatestReviewAdmissionDecisionForUpdate(
			txCtx, request.OrganizationID, request.DeploymentPlanID,
		)
		if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if err := validateReviewDecisionChain(latest, request); err != nil {
			return err
		}
		record := types.ReviewAdmissionDecisionRecord{
			ID: uuid.New(), CreatedAt: decisionAt, OrganizationID: request.OrganizationID,
			DeploymentPlanID: request.DeploymentPlanID, PlanRevision: snapshot.PlanRevision,
			PlanChecksum: snapshot.Plan.CanonicalChecksum, ReviewMaterialChecksum: materialChecksum,
			ObservedStateChecksum: observedChecksum, Decision: request.Decision,
			Reason: strings.TrimSpace(request.Reason), ActorUserAccountID: request.ActorUserAccountID,
			ExpiresAt: request.ExpiresAt.UTC(), SupersedesDecisionID: request.SupersedesDecisionID,
			RevokesDecisionID: request.RevokesDecisionID,
			AuthorizationEvidence: reviewAuthorizationEvidence(
				request.OrganizationID, request.ActorUserAccountID, request.DeploymentPlanID, decisionAt,
				admission.ID, admission.DecisionChecksum, approval.ID, approval.Revision,
			),
			IdempotencyKey: strings.TrimSpace(request.IdempotencyKey),
		}
		record.CanonicalChecksum = reviewadmission.CanonicalChecksum(record)
		result, err = insertReviewAdmissionDecision(txCtx, record)
		if err != nil {
			return err
		}
		return recordReviewAdmissionAudit(txCtx, *result)
	})
	return result, err
}

func ListReviewAdmissionDecisions(
	ctx context.Context, organizationID, planID uuid.UUID,
) ([]types.ReviewAdmissionDecisionRecord, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+reviewAdmissionOutputExpr+`
		FROM ReviewAdmissionDecision d
		WHERE d.organization_id = @organizationID
		  AND d.deployment_plan_id = @planID
		ORDER BY d.created_at DESC, d.id DESC`, pgx.NamedArgs{
		"organizationID": organizationID,
		"planID":         planID,
	})
	if err != nil {
		return nil, fmt.Errorf("list review admission decisions: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[types.ReviewAdmissionDecisionRecord])
}

func getLatestReviewAdmissionDecision(
	ctx context.Context, organizationID, planID uuid.UUID,
) (*types.ReviewAdmissionDecisionRecord, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+reviewAdmissionOutputExpr+`
		FROM ReviewAdmissionDecision d
		WHERE d.organization_id = @organizationID
		  AND d.deployment_plan_id = @planID
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT 1`, pgx.NamedArgs{"organizationID": organizationID, "planID": planID})
	if err != nil {
		return nil, fmt.Errorf("get latest review admission decision: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.ReviewAdmissionDecisionRecord],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect latest review admission decision: %w", err)
	}
	return &value, nil
}

type reviewAdmissionEvaluationMaterial struct {
	ID                      uuid.UUID
	PlanRevision            int64
	PlanChecksum            string
	EffectivePolicyChecksum string
	Decision                types.AdmissionDecision
	EvaluatedAt             time.Time
	MaterialChecksum        string
	DecisionChecksum        string
	ActorUserAccountID      uuid.UUID
	ApprovalRequestID       *uuid.UUID
	ApprovalRequestRevision *int64
}

func GetReviewAdmissionMaterial(
	ctx context.Context,
	organizationID, planID uuid.UUID,
) (*types.ReviewAdmissionMaterial, error) {
	if organizationID == uuid.Nil || planID == uuid.Nil {
		return nil, apierrors.NewBadRequest(
			"organizationId and deploymentPlanId are required",
		)
	}
	var result *types.ReviewAdmissionMaterial
	err := RunReadOnlyTxRR(ctx, func(txCtx context.Context) error {
		plan, err := GetDeploymentPlan(txCtx, planID, organizationID)
		if err != nil {
			return err
		}
		now, err := admissionDatabaseTime(txCtx)
		if err != nil {
			return err
		}
		observed, err := loadCurrentPlanObservedStateMaterial(
			txCtx,
			organizationID,
			planID,
			now,
			false,
		)
		if err != nil {
			return err
		}
		latestDecision, err := getLatestReviewAdmissionDecision(
			txCtx, organizationID, planID,
		)
		if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if errors.Is(err, apierrors.ErrNotFound) {
			latestDecision = nil
		}
		admission, err := getLatestReviewAdmissionEvaluationMaterial(
			txCtx,
			organizationID,
			planID,
		)
		if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		material := &types.ReviewAdmissionMaterial{
			DeploymentPlanID:      plan.ID,
			PlanRevision:          1,
			PlanChecksum:          plan.CanonicalChecksum,
			ObservedStateChecksum: observed.Checksum,
			ReviewMaterialChecksum: reviewadmission.ReviewMaterialChecksum(
				plan.CanonicalChecksum,
				observed.Checksum,
			),
			State: reviewadmission.MaterialState(
				latestDecision,
				plan.CanonicalChecksum,
				observed.Checksum,
				now,
			),
			Blockers:       []string{},
			LatestDecision: latestDecision,
		}
		material.ReviewMaterialValid =
			plan.Status == types.DeploymentPlanStatusReady &&
				plan.PlanSchema == types.TargetDeploymentPlanSchemaV2 &&
				plan.ProtocolVersion == string(types.ExecutionProtocolVersionV2) &&
				isLowerSHA256(plan.CanonicalChecksum) &&
				isLowerSHA256(observed.Checksum) &&
				isLowerSHA256(material.ReviewMaterialChecksum) &&
				observed.Complete
		if !material.ReviewMaterialValid {
			material.Blockers = append(
				material.Blockers,
				"review material is incomplete or the deployment plan is not READY native v2",
			)
		}
		if admission != nil {
			approvalEligible, err := currentDeploymentPlanApprovalEligibleForAdmission(
				txCtx,
				organizationID,
				planID,
				admission.ActorUserAccountID,
				now,
			)
			if err != nil {
				return err
			}
			material.AdmissionEvaluationID = &admission.ID
			material.AdmissionDecision = admission.Decision
			material.AdmissionDecisionChecksum = admission.DecisionChecksum
			material.AdmissionValid = currentReviewAdmissionEvaluation(
				*admission,
				*plan,
				observed.LatestReceivedAt,
				approvalEligible,
			)
		}
		if !material.AdmissionValid {
			material.Blockers = append(
				material.Blockers,
				"latest deployment admission is missing, stale, or not ADMIT",
			)
		}
		material.CanDecide = material.ReviewMaterialValid && material.AdmissionValid
		result = material
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func getLatestReviewAdmissionEvaluationMaterial(
	ctx context.Context,
	organizationID, planID uuid.UUID,
) (*reviewAdmissionEvaluationMaterial, error) {
	var value reviewAdmissionEvaluationMaterial
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id, plan_revision, plan_checksum, effective_policy_checksum,
		       decision, evaluated_at, material_checksum, decision_checksum,
		       actor_useraccount_id, approval_request_id, approval_request_revision
		FROM AdmissionEvaluation
		WHERE organization_id = @organizationID
		  AND deployment_plan_id = @planID
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, pgx.NamedArgs{
		"organizationID": organizationID,
		"planID":         planID,
	}).Scan(
		&value.ID,
		&value.PlanRevision,
		&value.PlanChecksum,
		&value.EffectivePolicyChecksum,
		&value.Decision,
		&value.EvaluatedAt,
		&value.MaterialChecksum,
		&value.DecisionChecksum,
		&value.ActorUserAccountID,
		&value.ApprovalRequestID,
		&value.ApprovalRequestRevision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get latest review admission evaluation: %w", err)
	}
	return &value, nil
}

func validateExactReviewAdmissionBinding(
	evaluation reviewAdmissionEvaluationMaterial,
	expectedID uuid.UUID,
	expectedDecisionChecksum string,
	approvalRequestID uuid.UUID,
	approvalRequestRevision int64,
) error {
	if expectedID == uuid.Nil || evaluation.ID != expectedID ||
		evaluation.DecisionChecksum != expectedDecisionChecksum {
		return apierrors.NewConflict("exact admission evaluation changed before task creation")
	}
	if evaluation.ApprovalRequestID == nil || evaluation.ApprovalRequestRevision == nil ||
		*evaluation.ApprovalRequestID != approvalRequestID ||
		*evaluation.ApprovalRequestRevision != approvalRequestRevision {
		return apierrors.NewConflict("admission evaluation does not bind the current approval request")
	}
	return nil
}

func currentReviewAdmissionEvaluation(
	evaluation reviewAdmissionEvaluationMaterial,
	plan types.DeploymentPlan,
	latestObservedAt time.Time,
	approvalEligible bool,
) bool {
	return approvalEligible &&
		evaluation.PlanRevision == 1 &&
		evaluation.PlanChecksum == plan.CanonicalChecksum &&
		evaluation.EffectivePolicyChecksum == plan.EffectivePolicyChecksum &&
		evaluation.Decision == types.AdmissionDecisionAdmit &&
		isLowerSHA256(evaluation.MaterialChecksum) &&
		isLowerSHA256(evaluation.DecisionChecksum) &&
		(latestObservedAt.IsZero() || !evaluation.EvaluatedAt.Before(latestObservedAt))
}

type currentReviewAdmissionAuthorization struct {
	ApprovalRequestID               uuid.UUID
	ApprovalRequestRevision         int64
	AdmissionEvaluationID           uuid.UUID
	AdmissionDecisionChecksum       string
	ReviewAdmissionDecisionID       uuid.UUID
	ReviewAdmissionDecisionChecksum string
}

func requireCurrentReviewAdmissionGo(
	ctx context.Context,
	request types.CreateTasksForDeploymentPlanRequest,
	plan types.DeploymentPlan,
	approval currentDeploymentPlanApproval,
) (currentReviewAdmissionAuthorization, error) {
	if err := lockReviewAdmissionPlan(
		ctx, request.OrganizationID, request.DeploymentPlanID,
	); err != nil {
		return currentReviewAdmissionAuthorization{}, err
	}
	decision, err := getLatestReviewAdmissionDecisionForUpdate(
		ctx, request.OrganizationID, request.DeploymentPlanID,
	)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return currentReviewAdmissionAuthorization{}, apierrors.NewConflict("deployment plan has no persistent GO review decision")
		}
		return currentReviewAdmissionAuthorization{}, err
	}
	now, err := admissionDatabaseTime(ctx)
	if err != nil {
		return currentReviewAdmissionAuthorization{}, err
	}
	observed, err := loadCurrentPlanObservedStateMaterial(
		ctx, request.OrganizationID, request.DeploymentPlanID, now, true,
	)
	if err != nil {
		return currentReviewAdmissionAuthorization{}, err
	}
	if !observed.Complete {
		return currentReviewAdmissionAuthorization{}, apierrors.NewConflict("deployment plan current observed state set is incomplete")
	}
	if err := reviewadmission.ValidateCurrentGo(
		*decision, plan.CanonicalChecksum, observed.Checksum, now,
	); err != nil {
		return currentReviewAdmissionAuthorization{}, apierrors.NewConflict(err.Error())
	}
	boundAdmission, err := getReviewAdmissionEvaluationMaterialAt(
		ctx, request.OrganizationID, request.DeploymentPlanID, decision.CreatedAt,
	)
	if err != nil {
		return currentReviewAdmissionAuthorization{}, apierrors.NewConflict("GO review decision has no bound admission evaluation")
	}
	if boundAdmission.ApprovalRequestID == nil || boundAdmission.ApprovalRequestRevision == nil ||
		decision.AuthorizationEvidence != reviewAuthorizationEvidence(
			request.OrganizationID, decision.ActorUserAccountID, request.DeploymentPlanID,
			decision.CreatedAt, boundAdmission.ID, boundAdmission.DecisionChecksum,
			*boundAdmission.ApprovalRequestID, *boundAdmission.ApprovalRequestRevision,
		) {
		return currentReviewAdmissionAuthorization{}, apierrors.NewConflict("GO authorization evidence is not bound to its admission and approval")
	}
	currentAdmission, err := getLatestReviewAdmissionEvaluationMaterial(
		ctx, request.OrganizationID, request.DeploymentPlanID,
	)
	if err != nil {
		return currentReviewAdmissionAuthorization{}, apierrors.NewConflict("deployment plan has no current ADMIT evaluation")
	}
	if !currentReviewAdmissionEvaluation(*currentAdmission, plan, observed.LatestReceivedAt, true) {
		return currentReviewAdmissionAuthorization{}, apierrors.NewConflict("latest deployment admission is stale or not ADMIT")
	}
	if err := validateExactReviewAdmissionBinding(
		*currentAdmission, request.AdmissionEvaluationID, request.AdmissionDecisionChecksum,
		approval.ID, approval.Revision,
	); err != nil {
		return currentReviewAdmissionAuthorization{}, err
	}
	if request.ReviewAuthorize == nil {
		return currentReviewAdmissionAuthorization{}, apierrors.ErrForbidden
	}
	if err := request.ReviewAuthorize(ctx, types.ReviewAdmissionExecutionContext{
		OrganizationID: request.OrganizationID, DeploymentPlanID: request.DeploymentPlanID,
		ActorUserAccountID: request.ActorUserAccountID, EnvironmentID: plan.EnvironmentID,
		DeploymentUnitID: plan.DeploymentUnitID, DecisionAt: now,
	}); err != nil {
		return currentReviewAdmissionAuthorization{}, err
	}
	return currentReviewAdmissionAuthorization{
		ApprovalRequestID: approval.ID, ApprovalRequestRevision: approval.Revision,
		AdmissionEvaluationID:           currentAdmission.ID,
		AdmissionDecisionChecksum:       currentAdmission.DecisionChecksum,
		ReviewAdmissionDecisionID:       decision.ID,
		ReviewAdmissionDecisionChecksum: decision.CanonicalChecksum,
	}, nil
}

func getReviewAdmissionEvaluationMaterialAt(
	ctx context.Context,
	organizationID, planID uuid.UUID,
	evaluatedAt time.Time,
) (*reviewAdmissionEvaluationMaterial, error) {
	var value reviewAdmissionEvaluationMaterial
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT id, plan_revision, plan_checksum, effective_policy_checksum,
		       decision, evaluated_at, material_checksum, decision_checksum,
		       actor_useraccount_id, approval_request_id, approval_request_revision
		FROM AdmissionEvaluation
		WHERE organization_id = @organizationID
		  AND deployment_plan_id = @planID
		  AND created_at <= @evaluatedAt
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, pgx.NamedArgs{
		"organizationID": organizationID,
		"planID":         planID,
		"evaluatedAt":    evaluatedAt,
	}).Scan(
		&value.ID, &value.PlanRevision, &value.PlanChecksum,
		&value.EffectivePolicyChecksum, &value.Decision, &value.EvaluatedAt,
		&value.MaterialChecksum, &value.DecisionChecksum, &value.ActorUserAccountID,
		&value.ApprovalRequestID, &value.ApprovalRequestRevision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get review admission evaluation at decision: %w", err)
	}
	return &value, nil
}

func lockReviewAdmissionPlan(
	ctx context.Context, organizationID, planID uuid.UUID,
) error {
	var locked int
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT 1
		FROM DeploymentPlan
		WHERE organization_id = @organizationID
		  AND id = @planID
		FOR UPDATE`, pgx.NamedArgs{
		"organizationID": organizationID, "planID": planID,
	}).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierrors.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock deployment plan review decision chain: %w", err)
	}
	return nil
}

func currentPlanObservedStateChecksum(
	ctx context.Context, organizationID, planID uuid.UUID,
) (string, error) {
	now, err := admissionDatabaseTime(ctx)
	if err != nil {
		return "", err
	}
	material, err := loadCurrentPlanObservedStateMaterial(
		ctx,
		organizationID,
		planID,
		now,
		true,
	)
	if err != nil {
		return "", err
	}
	if !material.Complete {
		return "", apierrors.NewConflict(
			"deployment plan current observed state set is incomplete",
		)
	}
	return material.Checksum, nil
}

type currentPlanObservedStateMaterial struct {
	Checksum         string
	Complete         bool
	LatestReceivedAt time.Time
}

func loadCurrentPlanObservedStateMaterial(
	ctx context.Context,
	organizationID, planID uuid.UUID,
	now time.Time,
	lockRows bool,
) (currentPlanObservedStateMaterial, error) {
	var result currentPlanObservedStateMaterial
	var baselineCount int
	if err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT count(*)
		FROM DeploymentPlanBaseline b
		WHERE b.organization_id = @organizationID
		  AND b.deployment_plan_id = @planID
	`, pgx.NamedArgs{
		"organizationID": organizationID,
		"planID":         planID,
	}).Scan(&baselineCount); err != nil {
		return result, fmt.Errorf("count deployment plan observed-state baselines: %w", err)
	}
	query := `
		SELECT b.component_key, active.id, active.revision,
		       o.id, o.state_checksum, o.runtime_state_checksum,
		       o.artifact_digest, o.config_checksum, o.schema_version,
		       o.capability_checksum, o.platform, o.topology_checksum,
		       o.received_at, o.fresh_until
		FROM DeploymentPlanBaseline b
		JOIN ComponentDesiredStateHead head
		  ON head.organization_id = b.organization_id
		 AND head.component_instance_id = b.component_instance_id
		 AND head.active_revision_id = b.active_desired_revision_id
		 AND head.pending_revision_id IS NULL
		 AND NOT head.quarantined
		JOIN ActiveDesiredRevision active
		  ON active.id = head.active_revision_id
		 AND active.organization_id = head.organization_id
		 AND active.deployment_unit_id = head.deployment_unit_id
		 AND active.component_instance_id = head.component_instance_id
		 AND active.component_key = b.component_key
		 AND active.revision = b.desired_revision
		JOIN ObservedComponentState o
		  ON o.id = b.observed_component_state_id
		 AND o.id = active.verified_observation_id
		 AND o.organization_id = active.organization_id
		 AND o.deployment_unit_id = active.deployment_unit_id
		 AND o.component_instance_id = active.component_instance_id
		 AND o.component_key = active.component_key
		 AND o.artifact_digest = active.artifact_digest
		 AND o.config_checksum = active.config_checksum
		 AND o.schema_version = active.schema_version
		 AND o.capability_checksum = active.capability_checksum
		 AND o.platform = active.platform
		 AND o.topology_checksum = active.topology_checksum
		 AND o.state_checksum = b.observation_checksum
		 AND o.is_current
		 AND o.trusted
		 AND o.disposition = 'ACCEPTED'
		 AND o.health = 'HEALTHY'
		 AND o.outcome = 'COMPLETE'
		 AND o.fresh_until >= @now
		JOIN ComponentObservationHead observation_head
		  ON observation_head.organization_id = o.organization_id
		 AND observation_head.observer_id = o.observer_id
		 AND observation_head.deployment_unit_id = o.deployment_unit_id
		 AND observation_head.component_instance_id = o.component_instance_id
		 AND observation_head.observation_id = o.id
		 AND observation_head.evidence_checksum = o.evidence_checksum
		 AND observation_head.captured_at = o.captured_at
		WHERE b.organization_id = @organizationID
		  AND b.deployment_plan_id = @planID
		  AND b.projection = 'verified_v2'
		  AND b.authorizes_v2_execution
		  AND NOT b.bootstrap
		  AND b.active_desired_revision_id IS NOT NULL
		  AND b.observed_component_state_id IS NOT NULL
		ORDER BY b.component_key, active.id, o.id`
	if lockRows {
		query += " FOR SHARE OF b, head, active, o, observation_head"
	}
	rows, err := internalctx.GetDb(ctx).Query(
		ctx,
		query,
		pgx.NamedArgs{"organizationID": organizationID, "planID": planID, "now": now},
	)
	if err != nil {
		return result, fmt.Errorf("load current observed state: %w", err)
	}
	defer rows.Close()
	type item struct {
		ComponentKey         string    `json:"componentKey"`
		ActiveRevisionID     uuid.UUID `json:"activeRevisionId"`
		DesiredRevision      int64     `json:"desiredRevision"`
		ObservationID        uuid.UUID `json:"observationId"`
		StateChecksum        string    `json:"stateChecksum"`
		RuntimeStateChecksum string    `json:"runtimeStateChecksum"`
		ArtifactDigest       string    `json:"artifactDigest"`
		ConfigChecksum       string    `json:"configChecksum"`
		SchemaVersion        string    `json:"schemaVersion"`
		CapabilityChecksum   string    `json:"capabilityChecksum"`
		Platform             string    `json:"platform"`
		TopologyChecksum     string    `json:"topologyChecksum"`
		ReceivedAt           time.Time `json:"receivedAt"`
		FreshUntil           time.Time `json:"freshUntil"`
	}
	items := []item{}
	for rows.Next() {
		var value item
		if err := rows.Scan(
			&value.ComponentKey, &value.ActiveRevisionID, &value.DesiredRevision,
			&value.ObservationID, &value.StateChecksum, &value.RuntimeStateChecksum,
			&value.ArtifactDigest, &value.ConfigChecksum, &value.SchemaVersion,
			&value.CapabilityChecksum, &value.Platform, &value.TopologyChecksum,
			&value.ReceivedAt, &value.FreshUntil,
		); err != nil {
			return result, fmt.Errorf("scan current observed state: %w", err)
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("iterate current observed state: %w", err)
	}
	payload, _ := json.Marshal(items)
	sum := sha256.Sum256(payload)
	result.Checksum = "sha256:" + hex.EncodeToString(sum[:])
	result.Complete = baselineCount > 0 && len(items) == baselineCount
	for _, value := range items {
		if value.ReceivedAt.After(result.LatestReceivedAt) {
			result.LatestReceivedAt = value.ReceivedAt
		}
	}
	return result, nil
}

func getLatestReviewAdmissionDecisionForUpdate(
	ctx context.Context, organizationID, planID uuid.UUID,
) (*types.ReviewAdmissionDecisionRecord, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+reviewAdmissionOutputExpr+`
		FROM ReviewAdmissionDecision d
		WHERE d.organization_id = @organizationID
		  AND d.deployment_plan_id = @planID
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT 1 FOR UPDATE`, pgx.NamedArgs{"organizationID": organizationID, "planID": planID})
	if err != nil {
		return nil, fmt.Errorf("lock latest review admission decision: %w", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.ReviewAdmissionDecisionRecord],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect latest review admission decision: %w", err)
	}
	return &value, nil
}

func insertReviewAdmissionDecision(
	ctx context.Context, decision types.ReviewAdmissionDecisionRecord,
) (*types.ReviewAdmissionDecisionRecord, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO ReviewAdmissionDecision (
		  id, created_at, organization_id, deployment_plan_id, plan_revision,
		  plan_checksum, review_material_checksum, observed_state_checksum,
		  decision, reason, actor_useraccount_id, expires_at,
		  supersedes_decision_id, revokes_decision_id,
		  authorization_evidence, canonical_checksum, idempotency_key
		) VALUES (
		  @id, @createdAt, @organizationID, @deploymentPlanID, @planRevision,
		  @planChecksum, @reviewMaterialChecksum, @observedStateChecksum,
		  @decision, @reason, @actorUserAccountID, @expiresAt,
		  @supersedesDecisionID, @revokesDecisionID,
		  @authorizationEvidence, @canonicalChecksum, @idempotencyKey
		) RETURNING `+reviewAdmissionOutputExpr,
		pgx.NamedArgs{
			"id": decision.ID, "createdAt": decision.CreatedAt, "organizationID": decision.OrganizationID,
			"deploymentPlanID": decision.DeploymentPlanID, "planRevision": decision.PlanRevision,
			"planChecksum": decision.PlanChecksum, "reviewMaterialChecksum": decision.ReviewMaterialChecksum,
			"observedStateChecksum": decision.ObservedStateChecksum, "decision": decision.Decision,
			"reason": decision.Reason, "actorUserAccountID": decision.ActorUserAccountID,
			"expiresAt": decision.ExpiresAt, "supersedesDecisionID": decision.SupersedesDecisionID,
			"revokesDecisionID":     decision.RevokesDecisionID,
			"authorizationEvidence": decision.AuthorizationEvidence,
			"canonicalChecksum":     decision.CanonicalChecksum, "idempotencyKey": decision.IdempotencyKey,
		})
	if err != nil {
		return nil, mapAdmissionWriteError("insert review admission decision", err)
	}
	value, err := pgx.CollectExactlyOneRow(
		rows, pgx.RowToStructByName[types.ReviewAdmissionDecisionRecord],
	)
	if err != nil {
		return nil, fmt.Errorf("collect review admission decision: %w", err)
	}
	return &value, nil
}

func getReviewDecisionByIdempotencyKey(
	ctx context.Context, organizationID, planID uuid.UUID, key string,
) (*types.ReviewAdmissionDecisionRecord, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+reviewAdmissionOutputExpr+`
		FROM ReviewAdmissionDecision d
		WHERE d.organization_id = @organizationID
		  AND d.deployment_plan_id = @planID
		  AND d.idempotency_key = @key`,
		pgx.NamedArgs{"organizationID": organizationID, "planID": planID, "key": strings.TrimSpace(key)})
	if err != nil {
		return nil, err
	}
	value, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ReviewAdmissionDecisionRecord])
	return &value, err
}

func reviewDecisionMatchesRequest(
	existing types.ReviewAdmissionDecisionRecord,
	request types.CreateReviewAdmissionDecisionRequest,
	snapshot types.AdmissionPlanSnapshot,
	materialChecksum, observedChecksum string,
) bool {
	return existing.OrganizationID == request.OrganizationID &&
		existing.DeploymentPlanID == request.DeploymentPlanID &&
		existing.PlanRevision == snapshot.PlanRevision &&
		existing.PlanChecksum == snapshot.Plan.CanonicalChecksum &&
		existing.ReviewMaterialChecksum == materialChecksum &&
		existing.ObservedStateChecksum == observedChecksum &&
		existing.Decision == request.Decision &&
		existing.Reason == strings.TrimSpace(request.Reason) &&
		existing.ActorUserAccountID == request.ActorUserAccountID &&
		existing.ExpiresAt.Equal(request.ExpiresAt.UTC()) &&
		reviewUUIDPointersEqual(existing.SupersedesDecisionID, request.SupersedesDecisionID) &&
		reviewUUIDPointersEqual(existing.RevokesDecisionID, request.RevokesDecisionID) &&
		existing.IdempotencyKey == strings.TrimSpace(request.IdempotencyKey) &&
		existing.CanonicalChecksum == reviewadmission.CanonicalChecksum(existing)
}

func reviewUUIDPointersEqual(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validateReviewDecisionChain(
	latest *types.ReviewAdmissionDecisionRecord,
	request types.CreateReviewAdmissionDecisionRequest,
) error {
	if latest == nil {
		if request.SupersedesDecisionID != nil || request.RevokesDecisionID != nil {
			return apierrors.NewConflict("first review decision cannot supersede or revoke another decision")
		}
		return nil
	}
	if request.SupersedesDecisionID == nil || *request.SupersedesDecisionID != latest.ID {
		return apierrors.NewConflict("review decision must supersede the current decision tip")
	}
	if request.RevokesDecisionID != nil &&
		(*request.RevokesDecisionID != latest.ID || request.Decision != types.ReviewAdmissionDecisionNoGo) {
		return apierrors.NewConflict("only NO_GO may revoke the current review decision tip")
	}
	return nil
}

func validateReviewAdmissionRequest(request types.CreateReviewAdmissionDecisionRequest) error {
	if request.OrganizationID == uuid.Nil || request.DeploymentPlanID == uuid.Nil ||
		request.ActorUserAccountID == uuid.Nil || !request.Decision.IsValid() || request.Authorize == nil {
		return apierrors.NewBadRequest("review admission decision identity is invalid")
	}
	if !isLowerSHA256(request.ExpectedPlanChecksum) ||
		!isLowerSHA256(request.ReviewMaterialChecksum) || !isLowerSHA256(request.ObservedStateChecksum) {
		return apierrors.NewBadRequest("review admission checksums are invalid")
	}
	reason := strings.TrimSpace(request.Reason)
	if len(reason) < 1 || len(reason) > 4096 || strings.ContainsAny(reason, "\r\n") {
		return apierrors.NewBadRequest("review admission reason is invalid")
	}
	if request.ExpiresAt.IsZero() || !admissionIdempotencyKeyValid(request.IdempotencyKey) {
		return apierrors.NewBadRequest("review admission expiry or idempotency key is invalid")
	}
	return nil
}

func reviewAuthorizationEvidence(
	organizationID, actorID, planID uuid.UUID,
	decisionAt time.Time,
	admissionEvaluationID uuid.UUID,
	admissionDecisionChecksum string,
	approvalRequestID uuid.UUID,
	approvalRequestRevision int64,
) string {
	payload, _ := json.Marshal(struct {
		OrganizationID            uuid.UUID `json:"organizationId"`
		ActorID                   uuid.UUID `json:"actorId"`
		PlanID                    uuid.UUID `json:"planId"`
		Action                    string    `json:"action"`
		DecisionAt                string    `json:"decisionAt"`
		AdmissionEvaluationID     uuid.UUID `json:"admissionEvaluationId"`
		AdmissionDecisionChecksum string    `json:"admissionDecisionChecksum"`
		ApprovalRequestID         uuid.UUID `json:"approvalRequestId"`
		ApprovalRequestRevision   int64     `json:"approvalRequestRevision"`
	}{
		organizationID, actorID, planID, "plan.execute", decisionAt.UTC().Format(time.RFC3339Nano),
		admissionEvaluationID, admissionDecisionChecksum, approvalRequestID, approvalRequestRevision,
	})
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func recordReviewAdmissionAudit(
	ctx context.Context, decision types.ReviewAdmissionDecisionRecord,
) error {
	planID, actorID := decision.DeploymentPlanID, decision.ActorUserAccountID
	return recordGovernanceAuditMutation(ctx, types.ControlPlaneAuditEventInput{
		OrganizationID: decision.OrganizationID, EventType: "review_admission.decided",
		ActorID: &actorID, Outcome: string(decision.Decision), DeploymentPlanID: &planID,
		DeploymentPlanChecksum: decision.PlanChecksum, AdmissionChecksum: decision.CanonicalChecksum,
		ObservationChecksum: decision.ObservedStateChecksum,
		Payload: governanceAuditPayload(map[string]any{
			"reviewMaterialChecksum": decision.ReviewMaterialChecksum,
			"expiresAt":              decision.ExpiresAt, "supersedesDecisionId": decision.SupersedesDecisionID,
			"revokesDecisionId": decision.RevokesDecisionID,
		}),
	})
}
