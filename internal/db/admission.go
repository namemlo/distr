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

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/scheduling"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const admissionEvaluationOutputExpr = `
	e.id,
	e.created_at,
	e.organization_id,
	e.deployment_plan_id,
	e.plan_revision,
	e.plan_checksum,
	e.plan_schema,
	e.protocol_version,
	e.campaign_id,
	e.campaign_revision,
	e.campaign_checksum,
	e.effective_policy_checksum,
	e.policy_version_ids,
	e.calendar_version_ids,
	e.freeze_revision_ids,
	e.approval_request_id,
	e.approval_request_revision,
	e.emergency_override_id,
	e.emergency_override_checksum,
	e.decision,
	e.reason_codes,
	e.evaluated_at,
	e.temporal_evidence,
	e.gate_evidence,
	e.material_checksum,
	e.decision_checksum,
	e.scheduler_idempotency_key,
	e.actor_useraccount_id
`

const emergencyOverrideOutputExpr = `
	o.id,
	o.created_at,
	o.organization_id,
	o.deployment_plan_id,
	o.plan_revision,
	o.plan_checksum,
	o.effective_policy_checksum,
	o.accelerations,
	o.reason,
	o.actor_useraccount_id,
	o.approval_evidence,
	o.expires_at,
	o.checksum,
	o.idempotency_key
`

type admissionGateEvidenceContext struct {
	OrganizationID          uuid.UUID
	DeploymentPlanID        uuid.UUID
	PlanRevision            int64
	PlanChecksum            string
	EffectivePolicyChecksum string
	EvaluatedAt             time.Time
}

type admissionGateEvidenceRepository interface {
	ResolveAdmissionGateEvidence(
		context.Context,
		admissionGateEvidenceContext,
	) ([]types.AdmissionGateEvidence, error)
}

type unavailableAdmissionGateEvidenceRepository struct{}

func (unavailableAdmissionGateEvidenceRepository) ResolveAdmissionGateEvidence(
	context.Context,
	admissionGateEvidenceContext,
) ([]types.AdmissionGateEvidence, error) {
	return nil, apierrors.NewConflict("trusted gate evidence repository is unavailable")
}

var trustedAdmissionGateEvidenceRepository admissionGateEvidenceRepository = unavailableAdmissionGateEvidenceRepository{}

type sealedAdmissionEvaluation struct {
	AdmissionRequest        types.AdmissionRequest
	Evaluation              types.AdmissionEvaluation
	ExpectedPlanChecksum    string
	ExpectedPolicyChecksum  string
	ActorUserAccountID      uuid.UUID
	SchedulerIdempotencyKey string
}

func CreateTasksForAdmittedV2Plan(
	ctx context.Context,
	request types.CreateTasksForAdmittedV2PlanRequest,
) ([]types.Task, error) {
	return scheduling.CreateTasksForAdmittedV2Plan(
		ctx,
		request,
		scheduling.AdmittedTaskCreationDependencies{
			LoadPlanSnapshot: func(
				ctx context.Context,
				planID, organizationID uuid.UUID,
			) (types.AdmissionPlanSnapshot, error) {
				return getAdmissionPlanSnapshot(ctx, planID, organizationID)
			},
			AdmitDeploymentPlan: AdmitDeploymentPlan,
			CreateTasks:         CreateTasksForDeploymentPlan,
		},
	)
}

func AdmitDeploymentPlan(
	ctx context.Context,
	request types.AdmitDeploymentPlanRequest,
) (*types.AdmissionEvaluation, error) {
	return admitDeploymentPlan(ctx, request, trustedAdmissionGateEvidenceRepository)
}

func admitDeploymentPlan(
	ctx context.Context,
	request types.AdmitDeploymentPlanRequest,
	gateEvidenceRepository admissionGateEvidenceRepository,
) (*types.AdmissionEvaluation, error) {
	if err := validateAdmitDeploymentPlanRequest(request, gateEvidenceRepository); err != nil {
		return nil, err
	}
	var result *types.AdmissionEvaluation
	err := RunTx(ctx, func(txCtx context.Context) error {
		snapshot, err := getAdmissionPlanSnapshot(txCtx, request.DeploymentPlanID, request.OrganizationID)
		if err != nil {
			return err
		}
		decisionAt, err := admissionDatabaseTime(txCtx)
		if err != nil {
			return err
		}
		if err := authorizeAdmission(
			txCtx,
			request.Authorize,
			types.AdmissionAuthorizationContext{
				OrganizationID:     request.OrganizationID,
				ActorUserAccountID: request.ActorUserAccountID,
				DeploymentPlanID:   request.DeploymentPlanID,
				EnvironmentID:      snapshot.EnvironmentID,
				DeploymentUnitID:   snapshot.DeploymentUnitID,
				Action:             "plan.execute",
				DecisionAt:         decisionAt,
			},
		); err != nil {
			return err
		}
		admissionRequest, err := buildAdmissionRequest(
			txCtx,
			snapshot,
			request,
			decisionAt,
			gateEvidenceRepository,
		)
		if err != nil {
			return err
		}
		evaluation, err := scheduling.EvaluateAdmission(txCtx, admissionRequest)
		if err != nil {
			return apierrors.NewConflict(err.Error())
		}
		result, err = persistAdmissionEvaluation(txCtx, sealedAdmissionEvaluation{
			AdmissionRequest:        admissionRequest,
			Evaluation:              evaluation,
			ExpectedPlanChecksum:    snapshot.Plan.CanonicalChecksum,
			ExpectedPolicyChecksum:  snapshot.Plan.EffectivePolicyChecksum,
			ActorUserAccountID:      request.ActorUserAccountID,
			SchedulerIdempotencyKey: strings.TrimSpace(request.SchedulerIdempotencyKey),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func CreateEmergencyOverride(
	ctx context.Context,
	request types.CreateEmergencyOverrideRequest,
) (*types.EmergencyOverride, error) {
	if err := validateCreateEmergencyOverrideRequest(request); err != nil {
		return nil, err
	}
	var result *types.EmergencyOverride
	err := RunTx(ctx, func(txCtx context.Context) error {
		snapshot, err := getAdmissionPlanSnapshot(txCtx, request.DeploymentPlanID, request.OrganizationID)
		if err != nil {
			return err
		}
		decisionAt, err := admissionDatabaseTime(txCtx)
		if err != nil {
			return err
		}
		if err := authorizeAdmission(
			txCtx,
			request.Authorize,
			types.AdmissionAuthorizationContext{
				OrganizationID:     request.OrganizationID,
				ActorUserAccountID: request.ActorUserAccountID,
				DeploymentPlanID:   request.DeploymentPlanID,
				EnvironmentID:      snapshot.EnvironmentID,
				DeploymentUnitID:   snapshot.DeploymentUnitID,
				Action:             "emergency.override",
				DecisionAt:         decisionAt,
			},
		); err != nil {
			return err
		}
		approvalEvidence, err := emergencyOverrideApprovalEvidence(
			txCtx,
			request,
		)
		if err != nil {
			return err
		}
		if replay, err := getEmergencyOverrideByIdempotencyKey(
			txCtx,
			request.OrganizationID,
			request.DeploymentPlanID,
			request.IdempotencyKey,
		); err == nil {
			if emergencyOverrideMatchesRequest(
				*replay,
				request,
				snapshot,
				approvalEvidence,
			) {
				result = replay
				return nil
			}
			return apierrors.NewConflict(
				"emergency override idempotency key was already used for different material",
			)
		} else if !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		override := types.EmergencyOverride{
			ID:                      uuid.New(),
			CreatedAt:               decisionAt,
			OrganizationID:          request.OrganizationID,
			DeploymentPlanID:        request.DeploymentPlanID,
			PlanRevision:            snapshot.PlanRevision,
			PlanChecksum:            snapshot.Plan.CanonicalChecksum,
			EffectivePolicyChecksum: snapshot.Plan.EffectivePolicyChecksum,
			Accelerations:           slices.Clone(request.Accelerations),
			Reason:                  strings.TrimSpace(request.Reason),
			ActorUserAccountID:      request.ActorUserAccountID,
			ApprovalEvidence:        approvalEvidence,
			ExpiresAt:               request.ExpiresAt.UTC(),
			IdempotencyKey:          strings.TrimSpace(request.IdempotencyKey),
		}
		override.Checksum = scheduling.EmergencyOverrideChecksum(override)
		if snapshot.Plan.EffectivePolicy == nil {
			return apierrors.NewConflict("deployment plan has no frozen effective policy")
		}
		if err := scheduling.ValidateEmergencyOverride(
			request.OrganizationID,
			admissionPlanEvidence(snapshot),
			*snapshot.Plan.EffectivePolicy,
			override,
			decisionAt,
		); err != nil {
			return apierrors.NewBadRequest(err.Error())
		}
		result, err = insertEmergencyOverride(txCtx, override)
		return err
	})
	return result, err
}

func validateAdmitDeploymentPlanRequest(
	request types.AdmitDeploymentPlanRequest,
	gateEvidenceRepository admissionGateEvidenceRepository,
) error {
	if request.OrganizationID == uuid.Nil ||
		request.DeploymentPlanID == uuid.Nil ||
		request.ActorUserAccountID == uuid.Nil {
		return apierrors.NewBadRequest(
			"organizationId, deploymentPlanId, and actorUserAccountId are required",
		)
	}
	if !admissionIdempotencyKeyValid(request.SchedulerIdempotencyKey) {
		return apierrors.NewBadRequest("schedulerIdempotencyKey is invalid")
	}
	if request.Authorize == nil {
		return apierrors.NewForbidden("scoped admission authorization is required")
	}
	if gateEvidenceRepository == nil {
		return apierrors.NewConflict("trusted gate evidence repository is required")
	}
	return nil
}

func validateCreateEmergencyOverrideRequest(
	request types.CreateEmergencyOverrideRequest,
) error {
	if request.OrganizationID == uuid.Nil ||
		request.DeploymentPlanID == uuid.Nil ||
		request.ActorUserAccountID == uuid.Nil {
		return apierrors.NewBadRequest(
			"organizationId, deploymentPlanId, and actorUserAccountId are required",
		)
	}
	if len(request.Accelerations) == 0 || len(request.ApprovalRequestIDs) == 0 {
		return apierrors.NewBadRequest("accelerations and approvalRequestIds are required")
	}
	if len(request.Accelerations) > 256 || len(request.ApprovalRequestIDs) > 256 {
		return apierrors.NewBadRequest("override evidence contains too many items")
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" || len(reason) > 4096 ||
		strings.ContainsAny(reason, "\r\n") ||
		request.ExpiresAt.IsZero() {
		return apierrors.NewBadRequest("reason and expiresAt are required")
	}
	seenGates := make(map[types.AdmissionGateKey]struct{}, len(request.Accelerations))
	for _, acceleration := range request.Accelerations {
		if _, exists := seenGates[acceleration.GateKey]; exists {
			return apierrors.NewBadRequest("duplicate acceleration gate")
		}
		seenGates[acceleration.GateKey] = struct{}{}
	}
	seenApprovals := make(map[uuid.UUID]struct{}, len(request.ApprovalRequestIDs))
	for _, requestID := range request.ApprovalRequestIDs {
		if requestID == uuid.Nil {
			return apierrors.NewBadRequest("approval request ID is required")
		}
		if _, exists := seenApprovals[requestID]; exists {
			return apierrors.NewBadRequest("duplicate approval request ID")
		}
		seenApprovals[requestID] = struct{}{}
	}
	if !admissionIdempotencyKeyValid(request.IdempotencyKey) {
		return apierrors.NewBadRequest("idempotencyKey is invalid")
	}
	if request.Authorize == nil {
		return apierrors.NewForbidden("scoped emergency override authorization is required")
	}
	return nil
}

func authorizeAdmission(
	ctx context.Context,
	authorize types.AdmissionAuthorizer,
	evidence types.AdmissionAuthorizationContext,
) error {
	if authorize == nil {
		return apierrors.NewForbidden("scoped admission authorization is required")
	}
	if err := authorize(ctx, evidence); err != nil {
		if errors.Is(err, apierrors.ErrForbidden) {
			return err
		}
		return apierrors.NewForbidden(err.Error())
	}
	return nil
}

func getAdmissionPlanSnapshot(
	ctx context.Context,
	planID, organizationID uuid.UUID,
) (types.AdmissionPlanSnapshot, error) {
	var snapshot types.AdmissionPlanSnapshot
	var effectivePolicyJSON []byte
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
		  dp.id,
		  dp.created_at,
		  dp.organization_id,
		  dp.application_id,
		  dp.release_bundle_id,
		  dp.channel_id,
		  dp.environment_id,
		  dp.deployment_unit_id,
		  dp.effective_policy,
		  dp.effective_policy_checksum,
		  dp.subscriber_set_checksum,
		  dp.status,
		  dp.canonical_checksum,
		  dp.canonical_payload,
		  1::bigint,
		  COALESCE(to_jsonb(dp)->>'plan_schema', ''),
		  COALESCE(to_jsonb(dp)->>'protocol_version', '')
		FROM DeploymentPlan dp
		WHERE dp.id = @planID
		  AND dp.organization_id = @organizationID
		FOR UPDATE
	`, pgx.NamedArgs{
		"planID":         planID,
		"organizationID": organizationID,
	}).Scan(
		&snapshot.Plan.ID,
		&snapshot.Plan.CreatedAt,
		&snapshot.Plan.OrganizationID,
		&snapshot.Plan.ApplicationID,
		&snapshot.Plan.ReleaseBundleID,
		&snapshot.Plan.ChannelID,
		&snapshot.Plan.EnvironmentID,
		&snapshot.Plan.DeploymentUnitID,
		&effectivePolicyJSON,
		&snapshot.Plan.EffectivePolicyChecksum,
		&snapshot.Plan.SubscriberSetChecksum,
		&snapshot.Plan.Status,
		&snapshot.Plan.CanonicalChecksum,
		&snapshot.Plan.CanonicalPayload,
		&snapshot.PlanRevision,
		&snapshot.PlanSchema,
		&snapshot.ProtocolVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot, apierrors.ErrNotFound
	}
	if err != nil {
		return snapshot, fmt.Errorf("get admission plan snapshot: %w", err)
	}
	if len(effectivePolicyJSON) > 0 && string(effectivePolicyJSON) != "null" {
		var policy types.EffectivePolicy
		if err := json.Unmarshal(effectivePolicyJSON, &policy); err != nil {
			return snapshot, fmt.Errorf("decode frozen effective policy: %w", err)
		}
		snapshot.Plan.EffectivePolicy = &policy
	}
	snapshot.EnvironmentID = snapshot.Plan.EnvironmentID
	snapshot.DeploymentUnitID = snapshot.Plan.DeploymentUnitID
	return snapshot, nil
}

func admissionPlanEvidence(snapshot types.AdmissionPlanSnapshot) types.AdmissionPlanEvidence {
	return types.AdmissionPlanEvidence{
		ID:              snapshot.Plan.ID,
		Revision:        snapshot.PlanRevision,
		Checksum:        snapshot.Plan.CanonicalChecksum,
		Schema:          snapshot.PlanSchema,
		ProtocolVersion: snapshot.ProtocolVersion,
	}
}

func buildAdmissionRequest(
	ctx context.Context,
	snapshot types.AdmissionPlanSnapshot,
	request types.AdmitDeploymentPlanRequest,
	evaluatedAt time.Time,
	gateEvidenceRepository admissionGateEvidenceRepository,
) (types.AdmissionRequest, error) {
	if snapshot.PlanSchema != types.AdmissionRequiredPlanSchemaV2 ||
		snapshot.ProtocolVersion != types.AdmissionRequiredProtocolV2 {
		return types.AdmissionRequest{}, apierrors.NewConflict(
			"admission requires a frozen plan_schema v2 and protocol_version v2 plan",
		)
	}
	if snapshot.Plan.Status != types.DeploymentPlanStatusReady {
		return types.AdmissionRequest{}, apierrors.NewConflict(
			"deployment plan must be READY before admission",
		)
	}
	if snapshot.Plan.EffectivePolicy == nil ||
		snapshot.Plan.EffectivePolicy.Checksum != snapshot.Plan.EffectivePolicyChecksum ||
		snapshot.Plan.EffectivePolicy.SubscriberSetChecksum != snapshot.Plan.SubscriberSetChecksum {
		return types.AdmissionRequest{}, apierrors.NewConflict(
			"deployment plan frozen policy evidence is missing or stale",
		)
	}
	calendars, err := admissionCalendarEvidence(
		ctx,
		snapshot.Plan.OrganizationID,
		snapshot.Plan.EffectivePolicy.AdmissionRules.MaintenanceWindowVersionIDs,
		evaluatedAt,
	)
	if err != nil {
		return types.AdmissionRequest{}, err
	}
	freezes, err := admissionFreezeEvidence(
		ctx,
		snapshot.Plan.OrganizationID,
		snapshot.Plan.EffectivePolicy.AdmissionRules.FreezeRuleVersionIDs,
		evaluatedAt,
	)
	if err != nil {
		return types.AdmissionRequest{}, err
	}
	approval, err := admissionApprovalEvidence(
		ctx,
		snapshot.Plan.OrganizationID,
		snapshot.Plan.ID,
	)
	if err != nil {
		return types.AdmissionRequest{}, err
	}
	override, err := getActiveEmergencyOverride(
		ctx,
		snapshot.Plan.OrganizationID,
		snapshot.Plan.ID,
		snapshot.Plan.CanonicalChecksum,
		snapshot.Plan.EffectivePolicyChecksum,
		evaluatedAt,
	)
	if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
		return types.AdmissionRequest{}, err
	}
	if errors.Is(err, apierrors.ErrNotFound) {
		override = nil
	}
	gateEvidence, err := gateEvidenceRepository.ResolveAdmissionGateEvidence(
		ctx,
		admissionGateEvidenceContext{
			OrganizationID:          snapshot.Plan.OrganizationID,
			DeploymentPlanID:        snapshot.Plan.ID,
			PlanRevision:            snapshot.PlanRevision,
			PlanChecksum:            snapshot.Plan.CanonicalChecksum,
			EffectivePolicyChecksum: snapshot.Plan.EffectivePolicyChecksum,
			EvaluatedAt:             evaluatedAt.UTC(),
		},
	)
	if err != nil {
		return types.AdmissionRequest{}, err
	}
	return types.AdmissionRequest{
		OrganizationID:    snapshot.Plan.OrganizationID,
		Plan:              admissionPlanEvidence(snapshot),
		Campaign:          request.Campaign,
		EffectivePolicy:   *snapshot.Plan.EffectivePolicy,
		CalendarEvidence:  calendars,
		FreezeEvidence:    freezes,
		Approval:          approval,
		GateEvidence:      slices.Clone(gateEvidence),
		EmergencyOverride: override,
		EvaluatedAt:       evaluatedAt.UTC(),
	}, nil
}

func admissionApprovalEvidence(
	ctx context.Context,
	organizationID, planID uuid.UUID,
) (types.AdmissionApprovalEvidence, error) {
	request, err := getActiveApprovalRequestForSubject(
		ctx,
		organizationID,
		types.ApprovalSubjectDeploymentPlan,
		planID,
		false,
	)
	if errors.Is(err, apierrors.ErrNotFound) {
		return types.AdmissionApprovalEvidence{
			Evaluation: types.ApprovalEvaluation{
				State:                 types.ApprovalRequestStatePending,
				MissingRequirementIDs: []uuid.UUID{},
				Requirements:          []types.ApprovalRequirementEvaluation{},
			},
		}, nil
	}
	if err != nil {
		return types.AdmissionApprovalEvidence{}, err
	}
	evaluation, err := EvaluateApprovalEligibility(ctx, request.ID)
	if err != nil {
		return types.AdmissionApprovalEvidence{}, err
	}
	current, err := GetApprovalRequest(ctx, request.ID, organizationID)
	if err != nil {
		return types.AdmissionApprovalEvidence{}, err
	}
	return types.AdmissionApprovalEvidence{
		RequestID:               current.ID,
		RequestRevision:         current.Revision,
		SubjectChecksum:         current.SubjectChecksum,
		EffectivePolicyChecksum: current.EffectivePolicyChecksum,
		SubscriberSetChecksum:   current.SubscriberSetChecksum,
		Evaluation:              evaluation,
	}, nil
}

func admissionCalendarEvidence(
	ctx context.Context,
	organizationID uuid.UUID,
	versionIDs []uuid.UUID,
	evaluatedAt time.Time,
) ([]types.AdmissionCalendarEvidence, error) {
	result := make([]types.AdmissionCalendarEvidence, 0, len(versionIDs))
	for _, versionID := range versionIDs {
		version, err := getMaintenanceCalendarVersionByID(ctx, organizationID, versionID)
		if err != nil {
			return nil, err
		}
		evaluation, err := scheduling.EvaluateCalendar(
			*version,
			types.CalendarEvaluationInput{
				UTCInstant:  evaluatedAt.UTC(),
				IANAZone:    version.IANAZone,
				RuleVersion: version.RuleVersion,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("evaluate maintenance calendar %s: %w", versionID, err)
		}
		remainingWaitSeconds, err := scheduling.RemainingCalendarWaitSeconds(
			*version,
			evaluatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"calculate maintenance calendar wait %s: %w",
				versionID,
				err,
			)
		}
		result = append(result, types.AdmissionCalendarEvidence{
			VersionID:            version.ID,
			Checksum:             version.Checksum,
			Evaluation:           evaluation,
			RemainingWaitSeconds: remainingWaitSeconds,
		})
	}
	return result, nil
}

func admissionFreezeEvidence(
	ctx context.Context,
	organizationID uuid.UUID,
	revisionIDs []uuid.UUID,
	evaluatedAt time.Time,
) ([]types.AdmissionFreezeEvidence, error) {
	result := make([]types.AdmissionFreezeEvidence, 0, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		revision, err := getDeploymentFreezeRevisionByID(ctx, organizationID, revisionID)
		if err != nil {
			return nil, err
		}
		evaluation := scheduling.EvaluateFreeze(
			[]types.DeploymentFreezeRevision{*revision},
			types.CalendarEvaluationInput{
				UTCInstant:  evaluatedAt.UTC(),
				IANAZone:    revision.IANAZone,
				RuleVersion: revision.RuleVersion,
			},
		)
		remainingWaitSeconds := int64(0)
		if evaluation.ReasonCode == types.CalendarReasonFreezeActive &&
			revision.EndAt.After(evaluatedAt.UTC()) {
			remaining := revision.EndAt.UTC().Sub(evaluatedAt.UTC())
			remainingWaitSeconds = int64((remaining + time.Second - 1) / time.Second)
		}
		result = append(result, types.AdmissionFreezeEvidence{
			RevisionID:           revision.ID,
			Checksum:             revision.Checksum,
			Evaluation:           evaluation,
			RemainingWaitSeconds: remainingWaitSeconds,
		})
	}
	return result, nil
}

func getMaintenanceCalendarVersionByID(
	ctx context.Context,
	organizationID, versionID uuid.UUID,
) (*types.MaintenanceCalendarVersion, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+maintenanceCalendarVersionOutputExpr+`
		FROM MaintenanceCalendarVersion v
		WHERE v.organization_id = @organizationID
		  AND v.id = @versionID
	`, pgx.NamedArgs{"organizationID": organizationID, "versionID": versionID})
	if err != nil {
		return nil, fmt.Errorf("get admission maintenance calendar version: %w", err)
	}
	version, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.MaintenanceCalendarVersion],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect admission maintenance calendar version: %w", err)
	}
	version.WindowRules, err = listMaintenanceWindowRules(ctx, organizationID, version.ID)
	if err != nil {
		return nil, err
	}
	return &version, nil
}

func getDeploymentFreezeRevisionByID(
	ctx context.Context,
	organizationID, revisionID uuid.UUID,
) (*types.DeploymentFreezeRevision, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentFreezeRevisionOutputExpr+`
		FROM DeploymentFreezeRevision r
		WHERE r.organization_id = @organizationID
		  AND r.id = @revisionID
	`, pgx.NamedArgs{"organizationID": organizationID, "revisionID": revisionID})
	if err != nil {
		return nil, fmt.Errorf("get admission deployment freeze revision: %w", err)
	}
	revision, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentFreezeRevision],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect admission deployment freeze revision: %w", err)
	}
	return &revision, nil
}

func persistAdmissionEvaluation(
	ctx context.Context,
	request sealedAdmissionEvaluation,
) (*types.AdmissionEvaluation, error) {
	evaluation, err := verifySealedAdmissionEvaluation(ctx, request)
	if err != nil {
		return nil, err
	}
	if evaluation.OrganizationID == uuid.Nil ||
		evaluation.DeploymentPlanID == uuid.Nil ||
		request.ActorUserAccountID == uuid.Nil ||
		!admissionIdempotencyKeyValid(request.SchedulerIdempotencyKey) {
		return nil, apierrors.NewBadRequest("admission evaluation persistence input is invalid")
	}
	evaluation.ActorUserAccountID = request.ActorUserAccountID
	evaluation.SchedulerIdempotencyKey = strings.TrimSpace(request.SchedulerIdempotencyKey)
	if err := assertAdmissionMaterialCurrent(ctx, request); err != nil {
		return nil, err
	}
	temporalEvidence, err := json.Marshal(evaluation.TemporalEvidence)
	if err != nil {
		return nil, fmt.Errorf("marshal admission temporal evidence: %w", err)
	}
	gateEvidence, err := json.Marshal(evaluation.GateEvidence)
	if err != nil {
		return nil, fmt.Errorf("marshal admission gate evidence: %w", err)
	}
	reasonCodes := make([]string, len(evaluation.ReasonCodes))
	for index, reason := range evaluation.ReasonCodes {
		reasonCodes[index] = string(reason)
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO AdmissionEvaluation AS e (
		  organization_id,
		  deployment_plan_id,
		  plan_revision,
		  plan_checksum,
		  plan_schema,
		  protocol_version,
		  campaign_id,
		  campaign_revision,
		  campaign_checksum,
		  effective_policy_checksum,
		  policy_version_ids,
		  calendar_version_ids,
		  freeze_revision_ids,
		  approval_request_id,
		  approval_request_revision,
		  emergency_override_id,
		  emergency_override_checksum,
		  decision,
		  reason_codes,
		  evaluated_at,
		  temporal_evidence,
		  gate_evidence,
		  material_checksum,
		  decision_checksum,
		  scheduler_idempotency_key,
		  actor_useraccount_id
		) VALUES (
		  @organizationID,
		  @deploymentPlanID,
		  @planRevision,
		  @planChecksum,
		  @planSchema,
		  @protocolVersion,
		  @campaignID,
		  @campaignRevision,
		  @campaignChecksum,
		  @effectivePolicyChecksum,
		  @policyVersionIDs,
		  @calendarVersionIDs,
		  @freezeRevisionIDs,
		  @approvalRequestID,
		  @approvalRequestRevision,
		  @emergencyOverrideID,
		  @emergencyOverrideChecksum,
		  @decision,
		  @reasonCodes,
		  @evaluatedAt,
		  @temporalEvidence,
		  @gateEvidence,
		  @materialChecksum,
		  @decisionChecksum,
		  @schedulerIdempotencyKey,
		  @actorUserAccountID
		)
		ON CONFLICT (
		  organization_id,
		  deployment_plan_id,
		  scheduler_idempotency_key
		) DO NOTHING
		RETURNING `+admissionEvaluationOutputExpr,
		pgx.NamedArgs{
			"organizationID":            evaluation.OrganizationID,
			"deploymentPlanID":          evaluation.DeploymentPlanID,
			"planRevision":              evaluation.PlanRevision,
			"planChecksum":              evaluation.PlanChecksum,
			"planSchema":                evaluation.PlanSchema,
			"protocolVersion":           evaluation.ProtocolVersion,
			"campaignID":                evaluation.CampaignID,
			"campaignRevision":          evaluation.CampaignRevision,
			"campaignChecksum":          evaluation.CampaignChecksum,
			"effectivePolicyChecksum":   evaluation.EffectivePolicyChecksum,
			"policyVersionIDs":          evaluation.PolicyVersionIDs,
			"calendarVersionIDs":        evaluation.CalendarVersionIDs,
			"freezeRevisionIDs":         evaluation.FreezeRevisionIDs,
			"approvalRequestID":         evaluation.ApprovalRequestID,
			"approvalRequestRevision":   evaluation.ApprovalRequestRevision,
			"emergencyOverrideID":       evaluation.EmergencyOverrideID,
			"emergencyOverrideChecksum": evaluation.EmergencyOverrideChecksum,
			"decision":                  evaluation.Decision,
			"reasonCodes":               reasonCodes,
			"evaluatedAt":               evaluation.EvaluatedAt,
			"temporalEvidence":          temporalEvidence,
			"gateEvidence":              gateEvidence,
			"materialChecksum":          evaluation.MaterialChecksum,
			"decisionChecksum":          evaluation.DecisionChecksum,
			"schedulerIdempotencyKey":   evaluation.SchedulerIdempotencyKey,
			"actorUserAccountID":        evaluation.ActorUserAccountID,
		},
	)
	if err != nil {
		return nil, mapAdmissionWriteError("insert admission evaluation", err)
	}
	inserted, err := collectAdmissionEvaluation(rows)
	if err == nil {
		return inserted, nil
	}
	if !errors.Is(err, apierrors.ErrNotFound) {
		return nil, err
	}
	existing, err := getAdmissionEvaluationByIdempotencyKey(
		ctx,
		evaluation.OrganizationID,
		evaluation.DeploymentPlanID,
		evaluation.SchedulerIdempotencyKey,
	)
	if err != nil {
		return nil, err
	}
	replayed, err := resolveAdmissionPersistenceReplay(*existing, evaluation.DecisionChecksum)
	return &replayed, err
}

func assertAdmissionMaterialCurrent(
	ctx context.Context,
	request sealedAdmissionEvaluation,
) error {
	var planChecksum, policyChecksum string
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT canonical_checksum, effective_policy_checksum
		FROM DeploymentPlan
		WHERE id = @planID
		  AND organization_id = @organizationID
		FOR UPDATE
	`, pgx.NamedArgs{
		"planID":         request.Evaluation.DeploymentPlanID,
		"organizationID": request.Evaluation.OrganizationID,
	}).Scan(&planChecksum, &policyChecksum)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierrors.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock admission plan material: %w", err)
	}
	if planChecksum != request.ExpectedPlanChecksum ||
		policyChecksum != request.ExpectedPolicyChecksum ||
		planChecksum != request.Evaluation.PlanChecksum ||
		policyChecksum != request.Evaluation.EffectivePolicyChecksum {
		return apierrors.NewConflict(
			"deployment plan or policy changed; create a new immutable revision",
		)
	}
	return nil
}

func verifySealedAdmissionEvaluation(
	ctx context.Context,
	request sealedAdmissionEvaluation,
) (types.AdmissionEvaluation, error) {
	recomputed, err := scheduling.EvaluateAdmission(ctx, request.AdmissionRequest)
	if err != nil {
		return types.AdmissionEvaluation{}, apierrors.NewConflict(
			"sealed admission material cannot be reevaluated: " + err.Error(),
		)
	}
	if request.Evaluation.MaterialChecksum != recomputed.MaterialChecksum ||
		request.Evaluation.DecisionChecksum != recomputed.DecisionChecksum ||
		request.Evaluation.Decision != recomputed.Decision {
		return types.AdmissionEvaluation{}, apierrors.NewConflict(
			"sealed admission evaluation does not match recomputed material and decision checksums",
		)
	}
	return recomputed, nil
}

func getAdmissionEvaluationByIdempotencyKey(
	ctx context.Context,
	organizationID, planID uuid.UUID,
	key string,
) (*types.AdmissionEvaluation, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+admissionEvaluationOutputExpr+`
		FROM AdmissionEvaluation e
		WHERE e.organization_id = @organizationID
		  AND e.deployment_plan_id = @planID
		  AND e.scheduler_idempotency_key = @key
	`, pgx.NamedArgs{
		"organizationID": organizationID,
		"planID":         planID,
		"key":            strings.TrimSpace(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get admission evaluation replay: %w", err)
	}
	return collectAdmissionEvaluation(rows)
}

func collectAdmissionEvaluation(rows pgx.Rows) (*types.AdmissionEvaluation, error) {
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate admission evaluation: %w", err)
		}
		return nil, apierrors.ErrNotFound
	}
	var evaluation types.AdmissionEvaluation
	var reasonCodes []string
	var temporalEvidence, gateEvidence []byte
	if err := rows.Scan(
		&evaluation.ID,
		&evaluation.CreatedAt,
		&evaluation.OrganizationID,
		&evaluation.DeploymentPlanID,
		&evaluation.PlanRevision,
		&evaluation.PlanChecksum,
		&evaluation.PlanSchema,
		&evaluation.ProtocolVersion,
		&evaluation.CampaignID,
		&evaluation.CampaignRevision,
		&evaluation.CampaignChecksum,
		&evaluation.EffectivePolicyChecksum,
		&evaluation.PolicyVersionIDs,
		&evaluation.CalendarVersionIDs,
		&evaluation.FreezeRevisionIDs,
		&evaluation.ApprovalRequestID,
		&evaluation.ApprovalRequestRevision,
		&evaluation.EmergencyOverrideID,
		&evaluation.EmergencyOverrideChecksum,
		&evaluation.Decision,
		&reasonCodes,
		&evaluation.EvaluatedAt,
		&temporalEvidence,
		&gateEvidence,
		&evaluation.MaterialChecksum,
		&evaluation.DecisionChecksum,
		&evaluation.SchedulerIdempotencyKey,
		&evaluation.ActorUserAccountID,
	); err != nil {
		return nil, fmt.Errorf("scan admission evaluation: %w", err)
	}
	evaluation.ReasonCodes = make([]types.AdmissionReasonCode, len(reasonCodes))
	for index, reason := range reasonCodes {
		evaluation.ReasonCodes[index] = types.AdmissionReasonCode(reason)
	}
	if err := json.Unmarshal(temporalEvidence, &evaluation.TemporalEvidence); err != nil {
		return nil, fmt.Errorf("decode admission temporal evidence: %w", err)
	}
	if err := json.Unmarshal(gateEvidence, &evaluation.GateEvidence); err != nil {
		return nil, fmt.Errorf("decode admission gate evidence: %w", err)
	}
	return &evaluation, nil
}

func resolveAdmissionPersistenceReplay(
	existing types.AdmissionEvaluation,
	decisionChecksum string,
) (types.AdmissionEvaluation, error) {
	if existing.DecisionChecksum != decisionChecksum {
		return types.AdmissionEvaluation{}, apierrors.NewConflict(
			"scheduler idempotency key was already used for a different admission decision",
		)
	}
	return existing, nil
}

func emergencyOverrideApprovalEvidence(
	ctx context.Context,
	request types.CreateEmergencyOverrideRequest,
) ([]types.EmergencyOverrideApprovalEvidence, error) {
	result := make([]types.EmergencyOverrideApprovalEvidence, 0, len(request.ApprovalRequestIDs))
	for _, requestID := range request.ApprovalRequestIDs {
		approval, err := GetApprovalRequest(ctx, requestID, request.OrganizationID)
		if err != nil {
			return nil, err
		}
		if approval.SubjectType != types.ApprovalSubjectDeploymentPlan ||
			approval.SubjectID != request.DeploymentPlanID {
			return nil, apierrors.NewForbidden(
				"override approval is outside the deployment plan scope",
			)
		}
		evaluation, err := EvaluateApprovalEligibility(ctx, requestID)
		if err != nil {
			return nil, err
		}
		if !evaluation.Eligible ||
			evaluation.State != types.ApprovalRequestStateApproved {
			return nil, apierrors.NewConflict(
				"every emergency override approval must be current, eligible, and approved",
			)
		}
		result = append(result, types.EmergencyOverrideApprovalEvidence{
			RequestID:       approval.ID,
			RequestRevision: approval.Revision,
			RequestChecksum: approvalEvidenceChecksum(*approval),
			Eligible:        true,
			State:           evaluation.State,
		})
	}
	slices.SortFunc(result, func(left, right types.EmergencyOverrideApprovalEvidence) int {
		return strings.Compare(left.RequestID.String(), right.RequestID.String())
	})
	return result, nil
}

func approvalEvidenceChecksum(request types.ApprovalRequest) string {
	payload, _ := json.Marshal(struct {
		ID                      uuid.UUID
		SubjectID               uuid.UUID
		SubjectRevision         int64
		SubjectChecksum         string
		EffectivePolicyChecksum string
		SubscriberSetChecksum   string
		Revision                int64
		State                   types.ApprovalRequestState
	}{
		ID:                      request.ID,
		SubjectID:               request.SubjectID,
		SubjectRevision:         request.SubjectRevision,
		SubjectChecksum:         request.SubjectChecksum,
		EffectivePolicyChecksum: request.EffectivePolicyChecksum,
		SubscriberSetChecksum:   request.SubscriberSetChecksum,
		Revision:                request.Revision,
		State:                   request.State,
	})
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func insertEmergencyOverride(
	ctx context.Context,
	override types.EmergencyOverride,
) (*types.EmergencyOverride, error) {
	accelerations, err := json.Marshal(override.Accelerations)
	if err != nil {
		return nil, fmt.Errorf("marshal emergency accelerations: %w", err)
	}
	approvalEvidence, err := json.Marshal(override.ApprovalEvidence)
	if err != nil {
		return nil, fmt.Errorf("marshal emergency approval evidence: %w", err)
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO EmergencyOverride AS o (
		  id,
		  created_at,
		  organization_id,
		  deployment_plan_id,
		  plan_revision,
		  plan_checksum,
		  effective_policy_checksum,
		  accelerations,
		  reason,
		  actor_useraccount_id,
		  approval_evidence,
		  expires_at,
		  checksum,
		  idempotency_key
		) VALUES (
		  @id,
		  @createdAt,
		  @organizationID,
		  @deploymentPlanID,
		  @planRevision,
		  @planChecksum,
		  @effectivePolicyChecksum,
		  @accelerations,
		  @reason,
		  @actorUserAccountID,
		  @approvalEvidence,
		  @expiresAt,
		  @checksum,
		  @idempotencyKey
		)
		RETURNING `+emergencyOverrideOutputExpr,
		pgx.NamedArgs{
			"id":                      override.ID,
			"createdAt":               override.CreatedAt,
			"organizationID":          override.OrganizationID,
			"deploymentPlanID":        override.DeploymentPlanID,
			"planRevision":            override.PlanRevision,
			"planChecksum":            override.PlanChecksum,
			"effectivePolicyChecksum": override.EffectivePolicyChecksum,
			"accelerations":           accelerations,
			"reason":                  override.Reason,
			"actorUserAccountID":      override.ActorUserAccountID,
			"approvalEvidence":        approvalEvidence,
			"expiresAt":               override.ExpiresAt,
			"checksum":                override.Checksum,
			"idempotencyKey":          override.IdempotencyKey,
		},
	)
	if err != nil {
		return nil, mapAdmissionWriteError("insert emergency override", err)
	}
	return collectEmergencyOverride(rows)
}

func getActiveEmergencyOverride(
	ctx context.Context,
	organizationID, planID uuid.UUID,
	planChecksum, policyChecksum string,
	evaluatedAt time.Time,
) (*types.EmergencyOverride, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+emergencyOverrideOutputExpr+`
		FROM EmergencyOverride o
		WHERE o.organization_id = @organizationID
		  AND o.deployment_plan_id = @planID
		  AND o.plan_checksum = @planChecksum
		  AND o.effective_policy_checksum = @policyChecksum
		  AND o.created_at <= @evaluatedAt
		  AND o.expires_at > @evaluatedAt
		ORDER BY o.created_at DESC, o.id DESC
		LIMIT 1
	`, pgx.NamedArgs{
		"organizationID": organizationID,
		"planID":         planID,
		"planChecksum":   planChecksum,
		"policyChecksum": policyChecksum,
		"evaluatedAt":    evaluatedAt.UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("get active emergency override: %w", err)
	}
	return collectEmergencyOverride(rows)
}

func getEmergencyOverrideByIdempotencyKey(
	ctx context.Context,
	organizationID, planID uuid.UUID,
	key string,
) (*types.EmergencyOverride, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+emergencyOverrideOutputExpr+`
		FROM EmergencyOverride o
		WHERE o.organization_id = @organizationID
		  AND o.deployment_plan_id = @planID
		  AND o.idempotency_key = @key
	`, pgx.NamedArgs{
		"organizationID": organizationID,
		"planID":         planID,
		"key":            strings.TrimSpace(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get emergency override replay: %w", err)
	}
	return collectEmergencyOverride(rows)
}

func collectEmergencyOverride(rows pgx.Rows) (*types.EmergencyOverride, error) {
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate emergency override: %w", err)
		}
		return nil, apierrors.ErrNotFound
	}
	var override types.EmergencyOverride
	var accelerations, approvalEvidence []byte
	if err := rows.Scan(
		&override.ID,
		&override.CreatedAt,
		&override.OrganizationID,
		&override.DeploymentPlanID,
		&override.PlanRevision,
		&override.PlanChecksum,
		&override.EffectivePolicyChecksum,
		&accelerations,
		&override.Reason,
		&override.ActorUserAccountID,
		&approvalEvidence,
		&override.ExpiresAt,
		&override.Checksum,
		&override.IdempotencyKey,
	); err != nil {
		return nil, fmt.Errorf("scan emergency override: %w", err)
	}
	if err := json.Unmarshal(accelerations, &override.Accelerations); err != nil {
		return nil, fmt.Errorf("decode emergency accelerations: %w", err)
	}
	if err := json.Unmarshal(approvalEvidence, &override.ApprovalEvidence); err != nil {
		return nil, fmt.Errorf("decode emergency approval evidence: %w", err)
	}
	return &override, nil
}

func emergencyOverrideMatchesRequest(
	override types.EmergencyOverride,
	request types.CreateEmergencyOverrideRequest,
	snapshot types.AdmissionPlanSnapshot,
	approvalEvidence []types.EmergencyOverrideApprovalEvidence,
) bool {
	if len(request.ApprovalRequestIDs) != len(approvalEvidence) {
		return false
	}
	requestedApprovals := make(map[uuid.UUID]struct{}, len(request.ApprovalRequestIDs))
	for _, requestID := range request.ApprovalRequestIDs {
		requestedApprovals[requestID] = struct{}{}
	}
	for _, evidence := range approvalEvidence {
		if _, exists := requestedApprovals[evidence.RequestID]; !exists {
			return false
		}
	}
	candidate := override
	candidate.OrganizationID = request.OrganizationID
	candidate.DeploymentPlanID = request.DeploymentPlanID
	candidate.PlanRevision = snapshot.PlanRevision
	candidate.PlanChecksum = snapshot.Plan.CanonicalChecksum
	candidate.EffectivePolicyChecksum = snapshot.Plan.EffectivePolicyChecksum
	candidate.Accelerations = slices.Clone(request.Accelerations)
	candidate.Reason = strings.TrimSpace(request.Reason)
	candidate.ActorUserAccountID = request.ActorUserAccountID
	candidate.ApprovalEvidence = slices.Clone(approvalEvidence)
	candidate.ExpiresAt = request.ExpiresAt.UTC()
	candidate.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	return override.Checksum == scheduling.EmergencyOverrideChecksum(override) &&
		override.Checksum == scheduling.EmergencyOverrideChecksum(candidate)
}

func admissionDatabaseTime(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := internalctx.GetDb(ctx).QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("get admission database time: %w", err)
	}
	return now.UTC(), nil
}

func admissionIdempotencyKeyValid(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' ||
			index > 0 && strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}

func mapAdmissionWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("%s: %w", action, apierrors.ErrNotFound)
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("%s: %w", action, apierrors.ErrConflict)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("%s: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}
