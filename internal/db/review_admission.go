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
		observedChecksum, err := currentPlanObservedStateChecksum(txCtx, request.OrganizationID, request.DeploymentPlanID)
		if err != nil {
			return err
		}
		materialChecksum := reviewadmission.ReviewMaterialChecksum(
			snapshot.Plan.CanonicalChecksum, observedChecksum,
		)
		if observedChecksum != request.ObservedStateChecksum || materialChecksum != request.ReviewMaterialChecksum {
			return apierrors.NewConflict("observed state changed before review decision")
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
			ID: uuid.New(), OrganizationID: request.OrganizationID,
			DeploymentPlanID: request.DeploymentPlanID, PlanRevision: snapshot.PlanRevision,
			PlanChecksum: snapshot.Plan.CanonicalChecksum, ReviewMaterialChecksum: materialChecksum,
			ObservedStateChecksum: observedChecksum, Decision: request.Decision,
			Reason: strings.TrimSpace(request.Reason), ActorUserAccountID: request.ActorUserAccountID,
			ExpiresAt: request.ExpiresAt.UTC(), SupersedesDecisionID: request.SupersedesDecisionID,
			RevokesDecisionID: request.RevokesDecisionID,
			AuthorizationEvidence: reviewAuthorizationEvidence(
				request.OrganizationID, request.ActorUserAccountID, request.DeploymentPlanID, decisionAt,
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
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT 100`, pgx.NamedArgs{"organizationID": organizationID, "planID": planID})
	if err != nil {
		return nil, fmt.Errorf("list review admission decisions: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[types.ReviewAdmissionDecisionRecord])
}

func requireCurrentReviewAdmissionGo(
	ctx context.Context,
	request types.CreateTasksForDeploymentPlanRequest,
	plan types.DeploymentPlan,
) error {
	if err := lockReviewAdmissionPlan(
		ctx, request.OrganizationID, request.DeploymentPlanID,
	); err != nil {
		return err
	}
	decision, err := getLatestReviewAdmissionDecisionForUpdate(
		ctx, request.OrganizationID, request.DeploymentPlanID,
	)
	if err != nil {
		if errors.Is(err, apierrors.ErrNotFound) {
			return apierrors.NewConflict("deployment plan has no persistent GO review decision")
		}
		return err
	}
	now, err := admissionDatabaseTime(ctx)
	if err != nil {
		return err
	}
	observedChecksum, err := currentPlanObservedStateChecksum(
		ctx, request.OrganizationID, request.DeploymentPlanID,
	)
	if err != nil {
		return err
	}
	if err := reviewadmission.ValidateCurrentGo(
		*decision, plan.CanonicalChecksum, observedChecksum, now,
	); err != nil {
		return apierrors.NewConflict(err.Error())
	}
	if request.ReviewAuthorize == nil {
		return apierrors.ErrForbidden
	}
	return request.ReviewAuthorize(ctx, types.ReviewAdmissionExecutionContext{
		OrganizationID: request.OrganizationID, DeploymentPlanID: request.DeploymentPlanID,
		ActorUserAccountID: request.ActorUserAccountID, EnvironmentID: plan.EnvironmentID,
		DeploymentUnitID: plan.DeploymentUnitID, DecisionAt: now,
	})
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
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT b.component_key, o.id, o.state_checksum
		FROM DeploymentPlanBaseline b
		JOIN ObservedComponentState o
		  ON o.organization_id = b.organization_id
		 AND o.component_instance_id = b.component_instance_id
		 AND o.is_current
		WHERE b.organization_id = @organizationID
		  AND b.deployment_plan_id = @planID
		ORDER BY b.component_key, o.id
		FOR SHARE OF o`, pgx.NamedArgs{"organizationID": organizationID, "planID": planID})
	if err != nil {
		return "", fmt.Errorf("lock current observed state: %w", err)
	}
	defer rows.Close()
	type item struct {
		ComponentKey  string    `json:"componentKey"`
		ObservationID uuid.UUID `json:"observationId"`
		StateChecksum string    `json:"stateChecksum"`
	}
	items := []item{}
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.ComponentKey, &value.ObservationID, &value.StateChecksum); err != nil {
			return "", fmt.Errorf("scan current observed state: %w", err)
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate current observed state: %w", err)
	}
	payload, _ := json.Marshal(items)
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
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
		  id, organization_id, deployment_plan_id, plan_revision,
		  plan_checksum, review_material_checksum, observed_state_checksum,
		  decision, reason, actor_useraccount_id, expires_at,
		  supersedes_decision_id, revokes_decision_id,
		  authorization_evidence, canonical_checksum, idempotency_key
		) VALUES (
		  @id, @organizationID, @deploymentPlanID, @planRevision,
		  @planChecksum, @reviewMaterialChecksum, @observedStateChecksum,
		  @decision, @reason, @actorUserAccountID, @expiresAt,
		  @supersedesDecisionID, @revokesDecisionID,
		  @authorizationEvidence, @canonicalChecksum, @idempotencyKey
		) RETURNING `+reviewAdmissionOutputExpr,
		pgx.NamedArgs{
			"id": decision.ID, "organizationID": decision.OrganizationID,
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
	organizationID, actorID, planID uuid.UUID, decisionAt time.Time,
) string {
	payload, _ := json.Marshal(struct {
		OrganizationID uuid.UUID `json:"organizationId"`
		ActorID        uuid.UUID `json:"actorId"`
		PlanID         uuid.UUID `json:"planId"`
		Action         string    `json:"action"`
		DecisionAt     string    `json:"decisionAt"`
	}{organizationID, actorID, planID, "plan.execute", decisionAt.UTC().Format(time.RFC3339Nano)})
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
