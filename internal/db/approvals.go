package db

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/governance"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	approvalDefaultPageLimit = 50
	approvalMaximumPageLimit = 100
	approvalCursorVersion    = 1
)

var approvalChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

const approvalRequestOutputExpr = `
	request.id,
	request.created_at,
	request.updated_at,
	request.organization_id,
	request.subject_type,
	request.subject_id,
	request.subject_revision,
	request.subject_checksum,
	request.effective_policy_checksum,
	request.subscriber_set_checksum,
	request.requester_useraccount_id,
	request.expires_at,
	request.state,
	request.revision,
	COALESCE(request.invalidation_reason, '') AS invalidation_reason,
	request.invalidated_at,
	request.resolved_at
`

const approvalRequirementOutputExpr = `
	requirement.id,
	requirement.created_at,
	requirement.organization_id,
	requirement.approval_request_id,
	requirement.rule_key,
	requirement.policy_version_id,
	requirement.authority_kind,
	requirement.authority_id,
	requirement.principal_group_id,
	requirement.quorum,
	requirement.separation_constraints,
	requirement.sort_order
`

const approvalDecisionOutputExpr = `
	decision.id,
	decision.created_at,
	decision.organization_id,
	decision.approval_request_id,
	decision.approval_requirement_id,
	decision.actor_useraccount_id,
	decision.decision,
	decision.comment,
	decision.request_revision,
	decision.idempotency_key,
	COALESCE(decision.governance_exception_key, '') AS governance_exception_key,
	COALESCE(decision.governance_exception_reference, '') AS governance_exception_reference
`

type approvalCursor struct {
	Version   int       `json:"v"`
	CreatedAt time.Time `json:"createdAt"`
	ID        uuid.UUID `json:"id"`
}

func RequestApproval(
	ctx context.Context,
	input types.ApprovalRequestInput,
) (*types.ApprovalRequest, error) {
	if err := validateApprovalRequestInput(input); err != nil {
		return nil, err
	}
	var result *types.ApprovalRequest
	err := RunTx(ctx, func(ctx context.Context) error {
		decisionAt, err := approvalDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if !input.ExpiresAt.After(decisionAt) ||
			input.ExpiresAt.After(decisionAt.Add(366*24*time.Hour)) {
			return apierrors.NewBadRequest(
				"expiresAt must be in the future and within 366 days",
			)
		}
		if err := ensureApprovalActor(
			ctx,
			input.OrganizationID,
			input.RequestedByUserAccountID,
		); err != nil {
			return err
		}
		plan, err := getApprovalPlanForUpdate(
			ctx,
			input.DeploymentPlanID,
			input.OrganizationID,
		)
		if err != nil {
			return err
		}
		if err := validateApprovalPlan(*plan); err != nil {
			return err
		}
		if err := input.Authorize(ctx, types.ApprovalAuthorizationContext{
			OrganizationID:     input.OrganizationID,
			ActorUserAccountID: input.RequestedByUserAccountID,
			DecisionAt:         decisionAt,
			DeploymentPlanID:   plan.ID,
			EnvironmentID:      plan.EnvironmentID,
			DeploymentUnitID:   plan.DeploymentUnitID,
		}); err != nil {
			return err
		}

		existing, err := getActiveApprovalRequestForSubject(
			ctx,
			input.OrganizationID,
			types.ApprovalSubjectDeploymentPlan,
			plan.ID,
			true,
		)
		if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if existing != nil {
			current := approvalSnapshotForPlan(*plan)
			reason := detectApprovalInvalidation(
				*existing,
				current,
				decisionAt,
			)
			if reason == "" {
				if err := hydrateApprovalRequest(ctx, existing); err != nil {
					return err
				}
				result = existing
				return nil
			}
			if err := updateApprovalRequestState(
				ctx,
				existing,
				stateForApprovalInvalidation(reason),
				reason,
				decisionAt,
			); err != nil {
				return err
			}
			if err := recordGovernanceAuditMutation(
				ctx,
				approvalInvalidatedAuditEvent(*existing, reason),
			); err != nil {
				return err
			}
		}

		request := &types.ApprovalRequest{
			ID:                      uuid.New(),
			OrganizationID:          input.OrganizationID,
			SubjectType:             types.ApprovalSubjectDeploymentPlan,
			SubjectID:               plan.ID,
			SubjectRevision:         1,
			SubjectChecksum:         plan.CanonicalChecksum,
			EffectivePolicyChecksum: plan.EffectivePolicyChecksum,
			SubscriberSetChecksum:   plan.SubscriberSetChecksum,
			RequesterUserAccountID:  input.RequestedByUserAccountID,
			ExpiresAt:               input.ExpiresAt.UTC(),
			State:                   types.ApprovalRequestStatePending,
			Revision:                1,
		}
		if err := insertApprovalRequest(ctx, request); err != nil {
			return err
		}
		requirements := approvalRequirementsFromPlan(*request, *plan)
		if err := insertApprovalRequirements(ctx, requirements); err != nil {
			return err
		}
		if err := hydrateApprovalRequest(ctx, request); err != nil {
			return err
		}
		result = request
		return recordGovernanceAuditMutation(
			ctx,
			approvalRequestedAuditEvent(*request),
		)
	})
	return result, err
}

func RequestSampleRetirementApproval(
	ctx context.Context,
	input types.SampleRetirementApprovalRequestInput,
) (*types.ApprovalRequest, error) {
	if err := validateSampleRetirementApprovalRequestInput(input); err != nil {
		return nil, err
	}
	var result *types.ApprovalRequest
	err := RunTx(ctx, func(ctx context.Context) error {
		decisionAt, err := approvalDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if !input.ExpiresAt.After(decisionAt) ||
			input.ExpiresAt.After(decisionAt.Add(366*24*time.Hour)) {
			return apierrors.NewBadRequest(
				"expiresAt must be in the future and within 366 days",
			)
		}
		if err := ensureApprovalActor(
			ctx,
			input.OrganizationID,
			input.RequestedByUserAccountID,
		); err != nil {
			return err
		}
		job, err := getSampleRetirementJobForApproval(
			ctx,
			input.SampleRetirementJobID,
			input.OrganizationID,
		)
		if err != nil {
			return err
		}
		if job.State != types.SampleRetirementJobPreviewed {
			return apierrors.NewConflict(
				"sample retirement job must be PREVIEWED before approval can be requested",
			)
		}
		if !approvalChecksumPattern.MatchString(job.PreviewChecksum) {
			return apierrors.NewConflict(
				"sample retirement preview checksum is invalid",
			)
		}
		effectivePolicy, issues, err := resolveSampleRetirementApprovalPolicy(
			ctx,
			input.OrganizationID,
		)
		if err != nil {
			return err
		}
		if len(issues) > 0 {
			return apierrors.NewConflict(
				"sample retirement approval policy is invalid",
			)
		}
		if err := validateSampleRetirementFourEyesPolicy(effectivePolicy); err != nil {
			return err
		}
		if err := input.Authorize(ctx, types.ApprovalAuthorizationContext{
			OrganizationID:        input.OrganizationID,
			ActorUserAccountID:    input.RequestedByUserAccountID,
			DecisionAt:            decisionAt,
			SampleRetirementJobID: job.ID,
		}); err != nil {
			return err
		}

		existing, err := getActiveApprovalRequestForSubject(
			ctx,
			input.OrganizationID,
			types.ApprovalSubjectSampleRetirement,
			job.ID,
			true,
		)
		if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		current := approvalSnapshotForSampleRetirement(*job, effectivePolicy)
		if existing != nil {
			reason := detectApprovalInvalidation(
				*existing,
				current,
				decisionAt,
			)
			if reason == "" {
				if err := hydrateApprovalRequest(ctx, existing); err != nil {
					return err
				}
				result = existing
				return nil
			}
			if err := updateApprovalRequestState(
				ctx,
				existing,
				stateForApprovalInvalidation(reason),
				reason,
				decisionAt,
			); err != nil {
				return err
			}
			if err := recordGovernanceAuditMutation(
				ctx,
				approvalAuditEventForSubject(
					approvalInvalidatedAuditEvent(*existing, reason),
					*existing,
				),
			); err != nil {
				return err
			}
		}

		request := &types.ApprovalRequest{
			ID:                      uuid.New(),
			OrganizationID:          input.OrganizationID,
			SubjectType:             types.ApprovalSubjectSampleRetirement,
			SubjectID:               job.ID,
			SubjectRevision:         job.Version,
			SubjectChecksum:         job.PreviewChecksum,
			EffectivePolicyChecksum: effectivePolicy.Checksum,
			SubscriberSetChecksum:   effectivePolicy.SubscriberSetChecksum,
			RequesterUserAccountID:  input.RequestedByUserAccountID,
			ExpiresAt:               input.ExpiresAt.UTC(),
			State:                   types.ApprovalRequestStatePending,
			Revision:                1,
		}
		if err := insertApprovalRequest(ctx, request); err != nil {
			return err
		}
		requirements := approvalRequirementsFromEffectivePolicy(
			*request,
			effectivePolicy,
		)
		if err := insertApprovalRequirements(ctx, requirements); err != nil {
			return err
		}
		if err := hydrateApprovalRequest(ctx, request); err != nil {
			return err
		}
		result = request
		return recordGovernanceAuditMutation(
			ctx,
			approvalAuditEventForSubject(
				approvalRequestedAuditEvent(*request),
				*request,
			),
		)
	})
	return result, err
}

func RecordApprovalDecision(
	ctx context.Context,
	input types.ApprovalDecisionInput,
) (*types.ApprovalDecision, error) {
	if err := validateApprovalDecisionInput(input); err != nil {
		return nil, err
	}
	var result *types.ApprovalDecision
	var invalidationReason types.ApprovalInvalidationReason
	err := RunTx(ctx, func(ctx context.Context) error {
		decisionAt, err := approvalDatabaseTime(ctx)
		if err != nil {
			return err
		}
		observedRequest, err := getApprovalRequest(
			ctx,
			input.ApprovalRequestID,
			input.OrganizationID,
			false,
		)
		if err != nil {
			return err
		}
		current, observedReason, err := currentApprovalSubjectSnapshot(
			ctx,
			*observedRequest,
		)
		if err != nil {
			return err
		}
		request, err := getApprovalRequestForUpdate(
			ctx,
			input.ApprovalRequestID,
			input.OrganizationID,
		)
		if err != nil {
			return err
		}
		authorizationContext, err := approvalAuthorizationContextForRequest(
			ctx,
			*request,
		)
		if err != nil {
			return err
		}
		authorizationContext.ActorUserAccountID = input.ActorUserAccountID
		authorizationContext.DecisionAt = decisionAt
		authorizationContext.ApprovalRequestID = request.ID
		authorizationContext.ApprovalRequirementID = input.ApprovalRequirementID
		if err := input.Authorize(ctx, authorizationContext); err != nil {
			return err
		}
		if request.SubjectType == types.ApprovalSubjectDeploymentPlan {
			input.GovernanceException = input.SingleReviewerPilot.ApprovalEvidence(
				request.OrganizationID,
				authorizationContext.EnvironmentID,
				authorizationContext.DeploymentTargetIDs,
				request.RequesterUserAccountID,
				input.ActorUserAccountID,
				input.Decision == types.ApprovalDecisionApprove,
			)
		}
		if err := validateSampleRetirementDecisionActor(*request, input); err != nil {
			return err
		}
		requirement, err := getApprovalRequirement(
			ctx,
			input.ApprovalRequirementID,
			request.ID,
			request.OrganizationID,
		)
		if err != nil {
			return err
		}
		actorInGroup, err := approvalActorInRequiredGroup(
			ctx,
			request.OrganizationID,
			requirement.PrincipalGroupID,
			input.ActorUserAccountID,
			decisionAt,
		)
		if err != nil {
			return err
		}
		if !actorInGroup {
			return apierrors.ErrForbidden
		}
		invalidationReason = observedReason
		if invalidationReason == "" {
			invalidationReason = detectApprovalInvalidation(
				*request,
				current,
				decisionAt,
			)
		}
		if invalidationReason != "" {
			if request.State.IsActive() {
				if err := updateApprovalRequestState(
					ctx,
					request,
					stateForApprovalInvalidation(invalidationReason),
					invalidationReason,
					decisionAt,
				); err != nil {
					return err
				}
				if err := recordGovernanceAuditMutation(
					ctx,
					approvalAuditEventForSubject(
						approvalInvalidatedAuditEvent(*request, invalidationReason),
						*request,
					),
				); err != nil {
					return err
				}
			}
			return nil
		}
		existing, err := getIdempotentApprovalDecision(ctx, input)
		if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if existing != nil {
			if approvalDecisionMatchesInput(*existing, input) {
				if request.Revision != existing.RequestRevision+1 {
					return apierrors.NewConflict("approval request revision changed")
				}
				result = existing
				return nil
			}
			return apierrors.NewConflict(
				"idempotency key is already bound to a different approval decision",
			)
		}

		decisions, err := listApprovalDecisions(
			ctx,
			request.ID,
			request.OrganizationID,
		)
		if err != nil {
			return err
		}
		if err := governance.ValidateApprovalDecision(
			*request,
			*requirement,
			decisions,
			input,
			actorInGroup,
			decisionAt,
		); err != nil {
			return err
		}
		decision := &types.ApprovalDecision{
			ID:                    uuid.New(),
			OrganizationID:        request.OrganizationID,
			ApprovalRequestID:     request.ID,
			ApprovalRequirementID: requirement.ID,
			ActorUserAccountID:    input.ActorUserAccountID,
			Decision:              input.Decision,
			Comment:               strings.TrimSpace(input.Comment),
			RequestRevision:       request.Revision,
			IdempotencyKey:        strings.TrimSpace(input.IdempotencyKey),
		}
		if input.GovernanceException != nil {
			decision.GovernanceExceptionKey = input.GovernanceException.Key
			decision.GovernanceExceptionReference = input.GovernanceException.ApprovalReference
		}
		if err := insertApprovalDecision(ctx, decision); err != nil {
			return err
		}
		request.Requirements, err = listApprovalRequirements(
			ctx,
			request.ID,
			request.OrganizationID,
		)
		if err != nil {
			return err
		}
		request.Decisions = append(decisions, *decision)
		evaluation := governance.EvaluateApproval(*request, request.Decisions, decisionAt)
		if err := updateApprovalRequestResolution(
			ctx,
			request,
			evaluation.State,
			decisionAt,
		); err != nil {
			return err
		}
		result = decision
		return recordGovernanceAuditMutation(
			ctx,
			approvalAuditEventForSubject(
				approvalDecisionRecordedAuditEvent(*request, *decision),
				*request,
			),
		)
	})
	if err != nil {
		return nil, err
	}
	if invalidationReason != "" {
		return nil, apierrors.NewConflict(
			"approval request is invalid: " + string(invalidationReason),
		)
	}
	return result, nil
}

func EvaluateApprovalEligibility(
	ctx context.Context,
	approvalRequestID uuid.UUID,
) (types.ApprovalEvaluation, error) {
	return evaluateApprovalEligibility(ctx, approvalRequestID, uuid.Nil)
}

func evaluateApprovalEligibilityForAdmission(
	ctx context.Context,
	approvalRequestID uuid.UUID,
	executorUserAccountID uuid.UUID,
) (types.ApprovalEvaluation, error) {
	if executorUserAccountID == uuid.Nil {
		return types.ApprovalEvaluation{}, apierrors.NewBadRequest(
			"executorUserAccountId is required",
		)
	}
	return evaluateApprovalEligibility(
		ctx,
		approvalRequestID,
		executorUserAccountID,
	)
}

func evaluateApprovalEligibility(
	ctx context.Context,
	approvalRequestID uuid.UUID,
	executorUserAccountID uuid.UUID,
) (types.ApprovalEvaluation, error) {
	if approvalRequestID == uuid.Nil {
		return types.ApprovalEvaluation{}, apierrors.NewBadRequest(
			"approvalRequestId is required",
		)
	}
	var result types.ApprovalEvaluation
	err := RunTx(ctx, func(ctx context.Context) error {
		decisionAt, err := approvalDatabaseTime(ctx)
		if err != nil {
			return err
		}
		observedRequest, err := getApprovalRequestByID(ctx, approvalRequestID)
		if err != nil {
			return err
		}
		current, reason, err := currentApprovalSubjectSnapshot(ctx, *observedRequest)
		if err != nil {
			return err
		}
		request, err := getApprovalRequestForUpdateByID(ctx, approvalRequestID)
		if err != nil {
			return err
		}
		if err := hydrateApprovalRequest(ctx, request); err != nil {
			return err
		}
		if reason == "" {
			reason = detectApprovalInvalidation(
				*request,
				current,
				decisionAt,
			)
		}
		if reason != "" && request.State.IsActive() {
			if err := updateApprovalRequestState(
				ctx,
				request,
				stateForApprovalInvalidation(reason),
				reason,
				decisionAt,
			); err != nil {
				return err
			}
			if err := recordGovernanceAuditMutation(
				ctx,
				approvalAuditEventForSubject(
					approvalInvalidatedAuditEvent(*request, reason),
					*request,
				),
			); err != nil {
				return err
			}
		}
		if executorUserAccountID == uuid.Nil {
			result = governance.EvaluateApproval(*request, request.Decisions, decisionAt)
			return nil
		}
		currentlyAuthorized, err := currentAuthorizedApprovalDecisions(
			ctx,
			*request,
			decisionAt,
		)
		if err != nil {
			return err
		}
		result = governance.EvaluateApprovalForAdmission(
			*request,
			currentlyAuthorized,
			executorUserAccountID,
			decisionAt,
		)
		return nil
	})
	return result, err
}

func EvaluateDeploymentPlanApproval(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentPlanID uuid.UUID,
) (types.ApprovalEvaluation, error) {
	if organizationID == uuid.Nil || deploymentPlanID == uuid.Nil {
		return types.ApprovalEvaluation{}, apierrors.NewBadRequest(
			"organizationId and deploymentPlanId are required",
		)
	}
	request, err := getActiveApprovalRequestForSubject(
		ctx,
		organizationID,
		types.ApprovalSubjectDeploymentPlan,
		deploymentPlanID,
		false,
	)
	if errors.Is(err, apierrors.ErrNotFound) {
		return types.ApprovalEvaluation{
			State:                 types.ApprovalRequestStatePending,
			Requirements:          []types.ApprovalRequirementEvaluation{},
			MissingRequirementIDs: []uuid.UUID{},
		}, nil
	}
	if err != nil {
		return types.ApprovalEvaluation{}, err
	}
	return EvaluateApprovalEligibility(ctx, request.ID)
}

type currentDeploymentPlanApproval struct {
	ID       uuid.UUID
	Revision int64
}

func requireCurrentDeploymentPlanApprovalForExecution(
	ctx context.Context,
	organizationID, deploymentPlanID, executorUserAccountID uuid.UUID,
) (currentDeploymentPlanApproval, error) {
	request, err := getActiveApprovalRequestForSubject(
		ctx,
		organizationID,
		types.ApprovalSubjectDeploymentPlan,
		deploymentPlanID,
		false,
	)
	if errors.Is(err, apierrors.ErrNotFound) {
		return currentDeploymentPlanApproval{}, apierrors.NewConflict(
			"deployment plan has no current approval request",
		)
	}
	if err != nil {
		return currentDeploymentPlanApproval{}, err
	}
	evaluation, err := evaluateApprovalEligibilityForAdmission(
		ctx,
		request.ID,
		executorUserAccountID,
	)
	if err != nil {
		return currentDeploymentPlanApproval{}, err
	}
	if !evaluation.Eligible ||
		evaluation.State != types.ApprovalRequestStateApproved {
		return currentDeploymentPlanApproval{}, apierrors.NewConflict(
			"deployment plan approval is not currently eligible for this executor",
		)
	}
	return currentDeploymentPlanApproval{ID: request.ID, Revision: request.Revision}, nil
}

func currentDeploymentPlanApprovalEligibleForAdmission(
	ctx context.Context,
	organizationID, deploymentPlanID, executorUserAccountID uuid.UUID,
	decisionAt time.Time,
) (bool, error) {
	request, err := getActiveApprovalRequestForSubject(
		ctx,
		organizationID,
		types.ApprovalSubjectDeploymentPlan,
		deploymentPlanID,
		false,
	)
	if errors.Is(err, apierrors.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := hydrateApprovalRequest(ctx, request); err != nil {
		return false, err
	}
	current, reason, err := currentApprovalSubjectSnapshot(ctx, *request)
	if err != nil {
		return false, err
	}
	if reason == "" {
		reason = detectApprovalInvalidation(*request, current, decisionAt)
	}
	if reason != "" {
		return false, nil
	}
	currentlyAuthorized, err := currentAuthorizedApprovalDecisions(
		ctx,
		*request,
		decisionAt,
	)
	if err != nil {
		return false, err
	}
	evaluation := governance.EvaluateApprovalForAdmission(
		*request,
		currentlyAuthorized,
		executorUserAccountID,
		decisionAt,
	)
	return evaluation.Eligible &&
		evaluation.State == types.ApprovalRequestStateApproved, nil
}

func InvalidateApproval(
	ctx context.Context,
	approvalRequestID uuid.UUID,
	reason types.ApprovalInvalidationReason,
) error {
	if approvalRequestID == uuid.Nil {
		return apierrors.NewBadRequest("approvalRequestId is required")
	}
	if !reason.IsValid() {
		return apierrors.NewBadRequest("invalidation reason is invalid")
	}
	return RunTx(ctx, func(ctx context.Context) error {
		request, err := getApprovalRequestForUpdateByID(ctx, approvalRequestID)
		if err != nil {
			return err
		}
		if !request.State.IsActive() {
			if request.InvalidationReason == reason {
				return nil
			}
			return apierrors.NewConflict("approval request is already terminal")
		}
		decisionAt, err := approvalDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if err := updateApprovalRequestState(
			ctx,
			request,
			stateForApprovalInvalidation(reason),
			reason,
			decisionAt,
		); err != nil {
			return err
		}
		return recordGovernanceAuditMutation(
			ctx,
			approvalAuditEventForSubject(
				approvalInvalidatedAuditEvent(*request, reason),
				*request,
			),
		)
	})
}

func GetApprovalRequest(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.ApprovalRequest, error) {
	request, err := getApprovalRequest(ctx, id, organizationID, false)
	if err != nil {
		return nil, err
	}
	if err := hydrateApprovalRequest(ctx, request); err != nil {
		return nil, err
	}
	return request, nil
}

func ListApprovalRequests(
	ctx context.Context,
	filter types.ApprovalRequestListFilter,
) (types.Page[types.ApprovalRequest], error) {
	page := types.Page[types.ApprovalRequest]{Items: []types.ApprovalRequest{}}
	limit, cursor, err := normalizeApprovalListFilter(filter)
	if err != nil {
		return page, err
	}
	state := filter.State
	if state == "" {
		state = types.ApprovalRequestStatePending
	}
	var cursorCreatedAt any
	var cursorID any
	if cursor != nil {
		cursorCreatedAt = cursor.CreatedAt
		cursorID = cursor.ID
	}
	limitPlusOne := limit + 1
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+approvalRequestOutputExpr+`
		FROM ApprovalRequest request
		WHERE request.organization_id = @organizationID
		  AND request.state = @state
		  AND (@state <> 'PENDING' OR request.expires_at > now())
		  AND (
		    @cursorCreatedAt::timestamptz IS NULL
		    OR (request.created_at, request.id) <
		      (@cursorCreatedAt::timestamptz, @cursorID::uuid)
		  )
		ORDER BY request.created_at DESC, request.id DESC
		LIMIT @limitPlusOne
	`,
		pgx.NamedArgs{
			"organizationID":  filter.OrganizationID,
			"state":           state,
			"cursorCreatedAt": cursorCreatedAt,
			"cursorID":        cursorID,
			"limitPlusOne":    limitPlusOne,
		},
	)
	if err != nil {
		return page, fmt.Errorf("list approval requests: %w", err)
	}
	items, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.ApprovalRequest],
	)
	if err != nil {
		return page, fmt.Errorf("collect approval requests: %w", err)
	}
	if len(items) > limit {
		last := items[limit-1]
		page.NextCursor, err = encodeApprovalCursor(approvalCursor{
			Version:   approvalCursorVersion,
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		})
		if err != nil {
			return page, err
		}
		items = items[:limit]
	}
	if err := hydrateApprovalRequests(ctx, items); err != nil {
		return page, err
	}
	page.Items = items
	return page, nil
}

func validateApprovalRequestInput(input types.ApprovalRequestInput) error {
	if input.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if input.DeploymentPlanID == uuid.Nil {
		return apierrors.NewBadRequest("deploymentPlanId is required")
	}
	if input.RequestedByUserAccountID == uuid.Nil {
		return apierrors.NewBadRequest("requestedByUserAccountId is required")
	}
	if input.ExpiresAt.IsZero() {
		return apierrors.NewBadRequest("expiresAt is required")
	}
	if input.Authorize == nil {
		return apierrors.ErrForbidden
	}
	return nil
}

func validateSampleRetirementApprovalRequestInput(
	input types.SampleRetirementApprovalRequestInput,
) error {
	if input.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if input.SampleRetirementJobID == uuid.Nil {
		return apierrors.NewBadRequest("sampleRetirementJobId is required")
	}
	if input.RequestedByUserAccountID == uuid.Nil {
		return apierrors.NewBadRequest("requestedByUserAccountId is required")
	}
	if input.ExpiresAt.IsZero() {
		return apierrors.NewBadRequest("expiresAt is required")
	}
	if input.Authorize == nil {
		return apierrors.ErrForbidden
	}
	return nil
}

func validateSampleRetirementFourEyesPolicy(
	policy types.EffectivePolicy,
) error {
	if len(policy.ApprovalRules) == 0 {
		return apierrors.NewConflict(
			"sample retirement approval policy has no approval requirements",
		)
	}
	for _, rule := range policy.ApprovalRules {
		if !slices.Contains(
			rule.SeparationConstraints,
			types.SeparationConstraintRequesterCannotApprove,
		) {
			return apierrors.NewConflict(
				"every sample retirement approval rule must prevent requester approval",
			)
		}
	}
	return nil
}

func validateSampleRetirementDecisionActor(
	request types.ApprovalRequest,
	input types.ApprovalDecisionInput,
) error {
	if request.SubjectType == types.ApprovalSubjectSampleRetirement &&
		input.Decision == types.ApprovalDecisionApprove &&
		input.ActorUserAccountID == request.RequesterUserAccountID {
		return apierrors.NewForbidden(
			"sample retirement requester cannot approve their own request",
		)
	}
	return nil
}

func validateApprovalDecisionInput(input types.ApprovalDecisionInput) error {
	if input.OrganizationID == uuid.Nil ||
		input.ApprovalRequestID == uuid.Nil ||
		input.ApprovalRequirementID == uuid.Nil ||
		input.ActorUserAccountID == uuid.Nil {
		return apierrors.NewBadRequest("approval decision identity is required")
	}
	if !input.Decision.IsValid() {
		return apierrors.NewBadRequest("decision is invalid")
	}
	if input.ExpectedRequestRevision < 1 {
		return apierrors.NewBadRequest("expectedRequestRevision must be greater than zero")
	}
	if strings.TrimSpace(input.Comment) == "" || len(strings.TrimSpace(input.Comment)) > 4096 {
		return apierrors.NewBadRequest("comment is required and must contain at most 4096 characters")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`).
		MatchString(strings.TrimSpace(input.IdempotencyKey)) {
		return apierrors.NewBadRequest("idempotencyKey is invalid")
	}
	if input.Authorize == nil {
		return apierrors.ErrForbidden
	}
	return nil
}

func validateApprovalPlan(plan types.DeploymentPlan) error {
	if plan.Status != types.DeploymentPlanStatusReady {
		return apierrors.NewConflict(
			"deployment plan must be READY before approval can be requested",
		)
	}
	if plan.EffectivePolicy == nil ||
		!approvalChecksumPattern.MatchString(plan.CanonicalChecksum) ||
		!approvalChecksumPattern.MatchString(plan.EffectivePolicyChecksum) ||
		!approvalChecksumPattern.MatchString(plan.SubscriberSetChecksum) ||
		plan.EffectivePolicy.Checksum != plan.EffectivePolicyChecksum ||
		plan.EffectivePolicy.SubscriberSetChecksum != plan.SubscriberSetChecksum {
		return apierrors.NewConflict(
			"deployment plan does not contain valid frozen policy evidence",
		)
	}
	if len(plan.EffectivePolicy.ApprovalRules) == 0 {
		return apierrors.NewConflict(
			"deployment plan policy does not require an approval workflow",
		)
	}
	for _, rule := range plan.EffectivePolicy.ApprovalRules {
		if rule.PolicyVersionID == uuid.Nil ||
			!rule.AuthorityKind.IsValid() ||
			rule.AuthorityID == uuid.Nil ||
			rule.PrincipalGroupID == uuid.Nil ||
			rule.Quorum < 1 {
			return apierrors.NewConflict(
				"deployment plan contains an invalid frozen approval requirement",
			)
		}
	}
	return nil
}

func getApprovalPlanForUpdate(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.DeploymentPlan, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentPlanOutputExpr+`
		FROM DeploymentPlan dp
		WHERE dp.id = @id
		  AND dp.organization_id = @organizationID
		FOR UPDATE
	`,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("lock approval deployment plan: %w", err)
	}
	plan, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentPlan],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect approval deployment plan: %w", err)
	}
	return &plan, nil
}

func getApprovalRequest(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
	forUpdate bool,
) (*types.ApprovalRequest, error) {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+approvalRequestOutputExpr+`
		FROM ApprovalRequest request
		WHERE request.id = @id
		  AND request.organization_id = @organizationID
	`+lockClause,
		pgx.NamedArgs{"id": id, "organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("get approval request: %w", err)
	}
	request, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ApprovalRequest],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect approval request: %w", err)
	}
	return &request, nil
}

func getApprovalRequestForUpdate(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.ApprovalRequest, error) {
	return getApprovalRequest(ctx, id, organizationID, true)
}

func getApprovalRequestByID(
	ctx context.Context,
	id uuid.UUID,
) (*types.ApprovalRequest, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+approvalRequestOutputExpr+`
		FROM ApprovalRequest request
		WHERE request.id = @id
	`, pgx.NamedArgs{"id": id})
	if err != nil {
		return nil, fmt.Errorf("get approval request identity: %w", err)
	}
	request, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ApprovalRequest],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect approval request identity: %w", err)
	}
	return &request, nil
}

func getApprovalRequestForUpdateByID(
	ctx context.Context,
	id uuid.UUID,
) (*types.ApprovalRequest, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+approvalRequestOutputExpr+`
		FROM ApprovalRequest request
		WHERE request.id = @id
		FOR UPDATE
	`, pgx.NamedArgs{"id": id})
	if err != nil {
		return nil, fmt.Errorf("lock approval request: %w", err)
	}
	request, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ApprovalRequest],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect locked approval request: %w", err)
	}
	return &request, nil
}

func getActiveApprovalRequestForSubject(
	ctx context.Context,
	organizationID uuid.UUID,
	subjectType types.ApprovalSubjectType,
	subjectID uuid.UUID,
	forUpdate bool,
) (*types.ApprovalRequest, error) {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+approvalRequestOutputExpr+`
		FROM ApprovalRequest request
		WHERE request.organization_id = @organizationID
		  AND request.subject_type = @subjectType
		  AND request.subject_id = @subjectID
		  AND request.state IN ('PENDING', 'APPROVED')
	`+lockClause,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"subjectType":    subjectType,
			"subjectID":      subjectID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("get active approval request: %w", err)
	}
	request, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ApprovalRequest],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect active approval request: %w", err)
	}
	return &request, nil
}

func insertApprovalRequest(
	ctx context.Context,
	request *types.ApprovalRequest,
) error {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO ApprovalRequest AS request (
			id,
			organization_id,
			subject_type,
			subject_id,
			subject_revision,
			subject_checksum,
			effective_policy_checksum,
			subscriber_set_checksum,
			requester_useraccount_id,
			expires_at,
			state,
			revision
		) VALUES (
			@id,
			@organizationID,
			@subjectType,
			@subjectID,
			@subjectRevision,
			@subjectChecksum,
			@effectivePolicyChecksum,
			@subscriberSetChecksum,
			@requesterUserAccountID,
			@expiresAt,
			@state,
			@revision
		)
		RETURNING `+approvalRequestOutputExpr,
		pgx.NamedArgs{
			"id":                      request.ID,
			"organizationID":          request.OrganizationID,
			"subjectType":             request.SubjectType,
			"subjectID":               request.SubjectID,
			"subjectRevision":         request.SubjectRevision,
			"subjectChecksum":         request.SubjectChecksum,
			"effectivePolicyChecksum": request.EffectivePolicyChecksum,
			"subscriberSetChecksum":   request.SubscriberSetChecksum,
			"requesterUserAccountID":  request.RequesterUserAccountID,
			"expiresAt":               request.ExpiresAt,
			"state":                   request.State,
			"revision":                request.Revision,
		},
	)
	if err != nil {
		return mapApprovalWriteError("insert approval request", err)
	}
	inserted, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ApprovalRequest],
	)
	if err != nil {
		return mapApprovalWriteError("collect approval request", err)
	}
	*request = inserted
	return nil
}

func approvalRequirementsFromPlan(
	request types.ApprovalRequest,
	plan types.DeploymentPlan,
) []types.ApprovalRequirement {
	if plan.EffectivePolicy == nil {
		return nil
	}
	return approvalRequirementsFromEffectivePolicy(request, *plan.EffectivePolicy)
}

func approvalRequirementsFromEffectivePolicy(
	request types.ApprovalRequest,
	policy types.EffectivePolicy,
) []types.ApprovalRequirement {
	requirements := make(
		[]types.ApprovalRequirement,
		len(policy.ApprovalRules),
	)
	for index, rule := range policy.ApprovalRules {
		requirements[index] = types.ApprovalRequirement{
			ID:                uuid.New(),
			OrganizationID:    request.OrganizationID,
			ApprovalRequestID: request.ID,
			RuleKey:           rule.Key,
			PolicyVersionID:   rule.PolicyVersionID,
			AuthorityKind:     rule.AuthorityKind,
			AuthorityID:       rule.AuthorityID,
			PrincipalGroupID:  rule.PrincipalGroupID,
			Quorum:            rule.Quorum,
			SeparationConstraints: append(
				[]types.SeparationConstraint{},
				rule.SeparationConstraints...,
			),
			SortOrder: index,
		}
	}
	return requirements
}

func insertApprovalRequirements(
	ctx context.Context,
	requirements []types.ApprovalRequirement,
) error {
	if len(requirements) == 0 {
		return apierrors.NewConflict("approval request requires at least one requirement")
	}
	_, err := internalctx.GetDb(ctx).CopyFrom(
		ctx,
		pgx.Identifier{"approvalrequirement"},
		[]string{
			"id",
			"organization_id",
			"approval_request_id",
			"rule_key",
			"policy_version_id",
			"authority_kind",
			"authority_id",
			"principal_group_id",
			"quorum",
			"separation_constraints",
			"sort_order",
		},
		pgx.CopyFromSlice(len(requirements), func(index int) ([]any, error) {
			requirement := requirements[index]
			return []any{
				requirement.ID,
				requirement.OrganizationID,
				requirement.ApprovalRequestID,
				requirement.RuleKey,
				requirement.PolicyVersionID,
				requirement.AuthorityKind,
				requirement.AuthorityID,
				requirement.PrincipalGroupID,
				requirement.Quorum,
				requirement.SeparationConstraints,
				requirement.SortOrder,
			}, nil
		}),
	)
	if err != nil {
		return mapApprovalWriteError("insert approval requirements", err)
	}
	return nil
}

func getApprovalRequirement(
	ctx context.Context,
	id uuid.UUID,
	requestID uuid.UUID,
	organizationID uuid.UUID,
) (*types.ApprovalRequirement, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+approvalRequirementOutputExpr+`
		FROM ApprovalRequirement requirement
		WHERE requirement.id = @id
		  AND requirement.approval_request_id = @requestID
		  AND requirement.organization_id = @organizationID
	`,
		pgx.NamedArgs{
			"id":             id,
			"requestID":      requestID,
			"organizationID": organizationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("get approval requirement: %w", err)
	}
	requirement, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ApprovalRequirement],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect approval requirement: %w", err)
	}
	return &requirement, nil
}

func listApprovalRequirements(
	ctx context.Context,
	requestID uuid.UUID,
	organizationID uuid.UUID,
) ([]types.ApprovalRequirement, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+approvalRequirementOutputExpr+`
		FROM ApprovalRequirement requirement
		WHERE requirement.approval_request_id = @requestID
		  AND requirement.organization_id = @organizationID
		ORDER BY requirement.sort_order, requirement.id
	`,
		pgx.NamedArgs{
			"requestID":      requestID,
			"organizationID": organizationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list approval requirements: %w", err)
	}
	result, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.ApprovalRequirement],
	)
	if err != nil {
		return nil, fmt.Errorf("collect approval requirements: %w", err)
	}
	return result, nil
}

func insertApprovalDecision(
	ctx context.Context,
	decision *types.ApprovalDecision,
) error {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO ApprovalDecision AS decision (
			id,
			organization_id,
			approval_request_id,
			approval_requirement_id,
			actor_useraccount_id,
			decision,
			comment,
			request_revision,
			idempotency_key,
			governance_exception_key,
			governance_exception_reference
		) VALUES (
			@id,
			@organizationID,
			@approvalRequestID,
			@approvalRequirementID,
			@actorUserAccountID,
			@decision,
			@comment,
			@requestRevision,
			@idempotencyKey,
			@governanceExceptionKey,
			@governanceExceptionReference
		)
		RETURNING `+approvalDecisionOutputExpr,
		pgx.NamedArgs{
			"id":                           decision.ID,
			"organizationID":               decision.OrganizationID,
			"approvalRequestID":            decision.ApprovalRequestID,
			"approvalRequirementID":        decision.ApprovalRequirementID,
			"actorUserAccountID":           decision.ActorUserAccountID,
			"decision":                     decision.Decision,
			"comment":                      decision.Comment,
			"requestRevision":              decision.RequestRevision,
			"idempotencyKey":               decision.IdempotencyKey,
			"governanceExceptionKey":       protectedHistoryNullableString(decision.GovernanceExceptionKey),
			"governanceExceptionReference": protectedHistoryNullableString(decision.GovernanceExceptionReference),
		},
	)
	if err != nil {
		return mapApprovalWriteError("insert approval decision", err)
	}
	inserted, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ApprovalDecision],
	)
	if err != nil {
		return mapApprovalWriteError("collect approval decision", err)
	}
	*decision = inserted
	return nil
}

func getIdempotentApprovalDecision(
	ctx context.Context,
	input types.ApprovalDecisionInput,
) (*types.ApprovalDecision, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+approvalDecisionOutputExpr+`
		FROM ApprovalDecision decision
		WHERE decision.organization_id = @organizationID
		  AND decision.approval_request_id = @approvalRequestID
		  AND decision.actor_useraccount_id = @actorUserAccountID
		  AND decision.idempotency_key = @idempotencyKey
	`,
		pgx.NamedArgs{
			"organizationID":     input.OrganizationID,
			"approvalRequestID":  input.ApprovalRequestID,
			"actorUserAccountID": input.ActorUserAccountID,
			"idempotencyKey":     strings.TrimSpace(input.IdempotencyKey),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("get idempotent approval decision: %w", err)
	}
	decision, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ApprovalDecision],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect idempotent approval decision: %w", err)
	}
	return &decision, nil
}

func listApprovalDecisions(
	ctx context.Context,
	requestID uuid.UUID,
	organizationID uuid.UUID,
) ([]types.ApprovalDecision, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+approvalDecisionOutputExpr+`
		FROM ApprovalDecision decision
		WHERE decision.approval_request_id = @requestID
		  AND decision.organization_id = @organizationID
		ORDER BY decision.created_at, decision.id
	`,
		pgx.NamedArgs{
			"requestID":      requestID,
			"organizationID": organizationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list approval decisions: %w", err)
	}
	result, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.ApprovalDecision],
	)
	if err != nil {
		return nil, fmt.Errorf("collect approval decisions: %w", err)
	}
	return result, nil
}

func approvalDecisionMatchesInput(
	decision types.ApprovalDecision,
	input types.ApprovalDecisionInput,
) bool {
	return decision.OrganizationID == input.OrganizationID &&
		decision.ApprovalRequestID == input.ApprovalRequestID &&
		decision.ApprovalRequirementID == input.ApprovalRequirementID &&
		decision.ActorUserAccountID == input.ActorUserAccountID &&
		decision.Decision == input.Decision &&
		decision.Comment == strings.TrimSpace(input.Comment) &&
		decision.RequestRevision == input.ExpectedRequestRevision &&
		decision.IdempotencyKey == strings.TrimSpace(input.IdempotencyKey)
}

func updateApprovalRequestResolution(
	ctx context.Context,
	request *types.ApprovalRequest,
	state types.ApprovalRequestState,
	decisionAt time.Time,
) error {
	if state != types.ApprovalRequestStatePending &&
		state != types.ApprovalRequestStateApproved &&
		state != types.ApprovalRequestStateRejected {
		return apierrors.NewConflict("approval evaluation produced an invalid decision state")
	}
	var resolvedAt any
	if state == types.ApprovalRequestStateApproved ||
		state == types.ApprovalRequestStateRejected {
		resolvedAt = decisionAt
	}
	command, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ApprovalRequest
		SET
			updated_at = @decisionAt,
			state = @state,
			revision = revision + 1,
			resolved_at = @resolvedAt
		WHERE id = @id
		  AND organization_id = @organizationID
		  AND revision = @expectedRequestRevision
	`,
		pgx.NamedArgs{
			"id":                      request.ID,
			"organizationID":          request.OrganizationID,
			"decisionAt":              decisionAt,
			"state":                   state,
			"resolvedAt":              resolvedAt,
			"expectedRequestRevision": request.Revision,
		},
	)
	if err != nil {
		return mapApprovalWriteError("resolve approval request", err)
	}
	if command.RowsAffected() != 1 {
		return apierrors.NewConflict("approval request revision changed")
	}
	request.UpdatedAt = decisionAt
	request.State = state
	request.Revision++
	if resolvedAt != nil {
		value := decisionAt
		request.ResolvedAt = &value
	}
	return nil
}

func updateApprovalRequestState(
	ctx context.Context,
	request *types.ApprovalRequest,
	state types.ApprovalRequestState,
	reason types.ApprovalInvalidationReason,
	decisionAt time.Time,
) error {
	command, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE ApprovalRequest
		SET
			updated_at = @decisionAt,
			state = @state,
			revision = revision + 1,
			invalidation_reason = @reason,
			invalidated_at = @decisionAt,
			resolved_at = NULL
		WHERE id = @id
		  AND organization_id = @organizationID
		  AND revision = @expectedRequestRevision
	`,
		pgx.NamedArgs{
			"id":                      request.ID,
			"organizationID":          request.OrganizationID,
			"decisionAt":              decisionAt,
			"state":                   state,
			"reason":                  reason,
			"expectedRequestRevision": request.Revision,
		},
	)
	if err != nil {
		return mapApprovalWriteError("invalidate approval request", err)
	}
	if command.RowsAffected() != 1 {
		return apierrors.NewConflict("approval request revision changed")
	}
	request.State = state
	request.Revision++
	request.UpdatedAt = decisionAt
	request.InvalidationReason = reason
	value := decisionAt
	request.InvalidatedAt = &value
	request.ResolvedAt = nil
	return nil
}

func stateForApprovalInvalidation(
	reason types.ApprovalInvalidationReason,
) types.ApprovalRequestState {
	switch reason {
	case types.ApprovalInvalidationExpired:
		return types.ApprovalRequestStateExpired
	case types.ApprovalInvalidationSuperseded:
		return types.ApprovalRequestStateSuperseded
	default:
		return types.ApprovalRequestStateInvalidated
	}
}

func approvalSnapshotForPlan(
	plan types.DeploymentPlan,
) types.ApprovalSubjectSnapshot {
	return types.ApprovalSubjectSnapshot{
		SubjectType:             types.ApprovalSubjectDeploymentPlan,
		SubjectID:               plan.ID,
		SubjectRevision:         1,
		SubjectChecksum:         plan.CanonicalChecksum,
		EffectivePolicyChecksum: plan.EffectivePolicyChecksum,
		SubscriberSetChecksum:   plan.SubscriberSetChecksum,
	}
}

func approvalSnapshotForSampleRetirement(
	job types.SampleRetirementJob,
	policy types.EffectivePolicy,
) types.ApprovalSubjectSnapshot {
	return types.ApprovalSubjectSnapshot{
		SubjectType:             types.ApprovalSubjectSampleRetirement,
		SubjectID:               job.ID,
		SubjectRevision:         job.Version,
		SubjectChecksum:         job.PreviewChecksum,
		EffectivePolicyChecksum: policy.Checksum,
		SubscriberSetChecksum:   policy.SubscriberSetChecksum,
	}
}

func currentApprovalSubjectSnapshot(
	ctx context.Context,
	request types.ApprovalRequest,
) (
	types.ApprovalSubjectSnapshot,
	types.ApprovalInvalidationReason,
	error,
) {
	if request.SubjectType == types.ApprovalSubjectSampleRetirement {
		job, err := getSampleRetirementJobForApproval(
			ctx,
			request.SubjectID,
			request.OrganizationID,
		)
		if errors.Is(err, apierrors.ErrNotFound) {
			return types.ApprovalSubjectSnapshot{},
				types.ApprovalInvalidationSampleRetirementChanged,
				nil
		}
		if err != nil {
			return types.ApprovalSubjectSnapshot{}, "", err
		}
		effectivePolicy, issues, err := resolveSampleRetirementApprovalPolicy(
			ctx,
			request.OrganizationID,
		)
		if err != nil {
			return types.ApprovalSubjectSnapshot{}, "", err
		}
		current := approvalSnapshotForSampleRetirement(*job, effectivePolicy)
		switch job.State {
		case types.SampleRetirementJobPreviewed:
		case types.SampleRetirementJobApplying,
			types.SampleRetirementJobApplied,
			types.SampleRetirementJobVerified:
			if job.ApprovalID == nil ||
				*job.ApprovalID != request.ID.String() ||
				job.ApprovalChecksum == nil ||
				*job.ApprovalChecksum != approvalEvidenceChecksum(request) {
				return types.ApprovalSubjectSnapshot{},
					types.ApprovalInvalidationSampleRetirementChanged,
					nil
			}
			current.SubjectRevision = request.SubjectRevision
		default:
			return types.ApprovalSubjectSnapshot{},
				types.ApprovalInvalidationSampleRetirementChanged,
				nil
		}
		if len(issues) > 0 || len(effectivePolicy.ApprovalRules) == 0 {
			return current, types.ApprovalInvalidationPolicyChanged, nil
		}
		return current, "", nil
	}
	if request.SubjectType != types.ApprovalSubjectDeploymentPlan {
		return types.ApprovalSubjectSnapshot{},
			types.ApprovalInvalidationCampaignMemberUnapproved,
			nil
	}
	plan, err := getApprovalPlanForUpdate(
		ctx,
		request.SubjectID,
		request.OrganizationID,
	)
	if errors.Is(err, apierrors.ErrNotFound) {
		return types.ApprovalSubjectSnapshot{},
			types.ApprovalInvalidationPlanChanged,
			nil
	}
	if err != nil {
		return types.ApprovalSubjectSnapshot{}, "", err
	}
	if plan.DeploymentUnitID == nil {
		return types.ApprovalSubjectSnapshot{},
			types.ApprovalInvalidationPlanChanged,
			nil
	}
	effectivePolicy, issues, err := ResolveEffectivePolicyForDeploymentUnit(
		ctx,
		request.OrganizationID,
		*plan.DeploymentUnitID,
		plan.EnvironmentID,
	)
	if errors.Is(err, apierrors.ErrNotFound) {
		return types.ApprovalSubjectSnapshot{},
			types.ApprovalInvalidationPlanChanged,
			nil
	}
	if err != nil {
		return types.ApprovalSubjectSnapshot{}, "", err
	}
	current := approvalSnapshotForPlan(*plan)
	current.EffectivePolicyChecksum = effectivePolicy.Checksum
	current.SubscriberSetChecksum = effectivePolicy.SubscriberSetChecksum
	if len(issues) > 0 {
		return current, types.ApprovalInvalidationPolicyChanged, nil
	}
	return current, "", nil
}

func getSampleRetirementJobForApproval(
	ctx context.Context,
	id uuid.UUID,
	organizationID uuid.UUID,
) (*types.SampleRetirementJob, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+sampleRetirementJobColumns+`
		FROM SampleRetirementJob
		WHERE id = @id
		  AND organization_id = @organizationID
		FOR UPDATE
	`, pgx.NamedArgs{"id": id, "organizationID": organizationID})
	if err != nil {
		return nil, fmt.Errorf("lock sample retirement job for approval: %w", err)
	}
	job, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.SampleRetirementJob],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("collect sample retirement job for approval: %w", err)
	}
	return &job, nil
}

func resolveSampleRetirementApprovalPolicy(
	ctx context.Context,
	organizationID uuid.UUID,
) (types.EffectivePolicy, []types.ValidationIssue, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentPolicyVersionOutputExpr+`
		FROM DeploymentPolicyBinding binding
		JOIN DeploymentPolicyVersion version
		  ON version.id = binding.deployment_policy_version_id
		 AND version.organization_id = binding.organization_id
		WHERE binding.organization_id = @organizationID
		  AND binding.scope_kind = 'organization'
		  AND binding.scope_id = @organizationID
		  AND binding.binding_role = 'owner'
		  AND binding.retired_at IS NULL
		  AND version.state = 'PUBLISHED'
		ORDER BY version.id
	`, pgx.NamedArgs{"organizationID": organizationID})
	if err != nil {
		return types.EffectivePolicy{}, nil, fmt.Errorf(
			"resolve sample retirement approval policy: %w",
			err,
		)
	}
	versions, err := collectDeploymentPolicyVersions(rows)
	if err != nil {
		return types.EffectivePolicy{}, nil, fmt.Errorf(
			"collect sample retirement approval policy: %w",
			err,
		)
	}
	effective, issues := governance.ComposeEffectivePolicy(
		types.PolicySet{
			AuthorityKind: types.PolicyAuthorityOwner,
			AuthorityID:   organizationID,
			Versions:      versions,
		},
		nil,
	)
	return effective, issues, nil
}

func approvalAuthorizationContextForRequest(
	ctx context.Context,
	request types.ApprovalRequest,
) (types.ApprovalAuthorizationContext, error) {
	result := types.ApprovalAuthorizationContext{
		OrganizationID: request.OrganizationID,
	}
	switch request.SubjectType {
	case types.ApprovalSubjectDeploymentPlan:
		plan, err := getApprovalPlanForUpdate(
			ctx,
			request.SubjectID,
			request.OrganizationID,
		)
		if err != nil {
			return result, err
		}
		result.DeploymentPlanID = request.SubjectID
		result.EnvironmentID = plan.EnvironmentID
		result.DeploymentUnitID = plan.DeploymentUnitID
		targets, err := getDeploymentPlanTargets(ctx, plan.ID, plan.OrganizationID)
		if err != nil {
			return result, err
		}
		for _, target := range targets {
			result.DeploymentTargetIDs = append(
				result.DeploymentTargetIDs,
				target.DeploymentTargetID,
			)
		}
	case types.ApprovalSubjectSampleRetirement:
		if _, err := getSampleRetirementJobForApproval(
			ctx,
			request.SubjectID,
			request.OrganizationID,
		); err != nil {
			return result, err
		}
		result.SampleRetirementJobID = request.SubjectID
	default:
		return result, apierrors.NewConflict("approval subject type is unsupported")
	}
	return result, nil
}

func approvalAuditEventForSubject(
	event types.ControlPlaneAuditEventInput,
	request types.ApprovalRequest,
) types.ControlPlaneAuditEventInput {
	if request.SubjectType == types.ApprovalSubjectSampleRetirement {
		event.DeploymentPlanID = nil
		event.DeploymentPlanChecksum = ""
	}
	return event
}

func validateSampleRetirementApprovalBinding(
	request types.ApprovalRequest,
	job types.SampleRetirementJob,
	decisionAt time.Time,
) (types.SampleRetirementApprovalBinding, error) {
	if job.State != types.SampleRetirementJobPreviewed {
		return types.SampleRetirementApprovalBinding{}, apierrors.NewConflict(
			"sample retirement job is not frozen in PREVIEWED state",
		)
	}
	if request.OrganizationID != job.OrganizationID ||
		request.SubjectType != types.ApprovalSubjectSampleRetirement ||
		request.SubjectID != job.ID ||
		request.SubjectRevision != job.Version ||
		request.SubjectChecksum != job.PreviewChecksum {
		return types.SampleRetirementApprovalBinding{}, apierrors.NewConflict(
			"approval request does not match the frozen sample retirement preview",
		)
	}
	if request.State != types.ApprovalRequestStateApproved {
		return types.SampleRetirementApprovalBinding{}, apierrors.NewConflict(
			"sample retirement approval request is not APPROVED",
		)
	}
	if !request.ExpiresAt.After(decisionAt) {
		return types.SampleRetirementApprovalBinding{}, apierrors.NewConflict(
			"sample retirement approval request has expired",
		)
	}
	return types.SampleRetirementApprovalBinding{
		ApprovalRequestID: request.ID,
		ApprovalChecksum:  approvalEvidenceChecksum(request),
		RequestRevision:   request.Revision,
		ExpiresAt:         request.ExpiresAt,
	}, nil
}

func ResolveSampleRetirementApproval(
	ctx context.Context,
	organizationID uuid.UUID,
	jobID uuid.UUID,
	approvalRequestID uuid.UUID,
) (types.SampleRetirementApprovalBinding, error) {
	if organizationID == uuid.Nil || jobID == uuid.Nil || approvalRequestID == uuid.Nil {
		return types.SampleRetirementApprovalBinding{}, apierrors.NewBadRequest(
			"sample retirement approval identity is required",
		)
	}
	var result types.SampleRetirementApprovalBinding
	err := RunTx(ctx, func(ctx context.Context) error {
		job, err := getSampleRetirementJobForApproval(ctx, jobID, organizationID)
		if err != nil {
			return err
		}
		result, err = resolveSampleRetirementApprovalForJob(
			ctx,
			*job,
			approvalRequestID,
		)
		return err
	})
	return result, err
}

func resolveSampleRetirementApprovalForJob(
	ctx context.Context,
	job types.SampleRetirementJob,
	approvalRequestID uuid.UUID,
) (types.SampleRetirementApprovalBinding, error) {
	request, err := getApprovalRequestForUpdate(
		ctx,
		approvalRequestID,
		job.OrganizationID,
	)
	if err != nil {
		return types.SampleRetirementApprovalBinding{}, err
	}
	effectivePolicy, issues, err := resolveSampleRetirementApprovalPolicy(
		ctx,
		job.OrganizationID,
	)
	if err != nil {
		return types.SampleRetirementApprovalBinding{}, err
	}
	if len(issues) > 0 || len(effectivePolicy.ApprovalRules) == 0 {
		return types.SampleRetirementApprovalBinding{}, apierrors.NewConflict(
			"sample retirement approval policy is invalid",
		)
	}
	current := approvalSnapshotForSampleRetirement(job, effectivePolicy)
	decisionAt, err := approvalDatabaseTime(ctx)
	if err != nil {
		return types.SampleRetirementApprovalBinding{}, err
	}
	if job.State != types.SampleRetirementJobPreviewed {
		if job.State != types.SampleRetirementJobApplying ||
			request.OrganizationID != job.OrganizationID ||
			request.SubjectType != types.ApprovalSubjectSampleRetirement ||
			request.SubjectID != job.ID ||
			request.SubjectRevision != 1 ||
			request.SubjectChecksum != job.PreviewChecksum ||
			request.EffectivePolicyChecksum != effectivePolicy.Checksum ||
			request.SubscriberSetChecksum != effectivePolicy.SubscriberSetChecksum ||
			request.State != types.ApprovalRequestStateApproved ||
			!request.ExpiresAt.After(decisionAt) {
			return types.SampleRetirementApprovalBinding{}, apierrors.NewConflict(
				"sample retirement approval binding is no longer current",
			)
		}
		binding := types.SampleRetirementApprovalBinding{
			ApprovalRequestID: request.ID,
			ApprovalChecksum:  approvalEvidenceChecksum(*request),
			RequestRevision:   request.Revision,
			ExpiresAt:         request.ExpiresAt,
		}
		if job.ApprovalID == nil ||
			*job.ApprovalID != binding.ApprovalRequestID.String() ||
			job.ApprovalChecksum == nil ||
			*job.ApprovalChecksum != binding.ApprovalChecksum {
			return types.SampleRetirementApprovalBinding{}, apierrors.NewConflict(
				"sample retirement approval binding does not match the job",
			)
		}
		return binding, nil
	}
	if reason := detectApprovalInvalidation(
		*request,
		current,
		decisionAt,
	); reason != "" {
		return types.SampleRetirementApprovalBinding{}, apierrors.NewConflict(
			"sample retirement approval request is invalid: " + string(reason),
		)
	}
	return validateSampleRetirementApprovalBinding(*request, job, decisionAt)
}

func detectApprovalInvalidation(
	request types.ApprovalRequest,
	current types.ApprovalSubjectSnapshot,
	decisionAt time.Time,
) types.ApprovalInvalidationReason {
	reason := governance.DetectApprovalInvalidation(request, current, decisionAt)
	if reason == types.ApprovalInvalidationPlanChanged &&
		request.SubjectType == types.ApprovalSubjectSampleRetirement {
		return types.ApprovalInvalidationSampleRetirementChanged
	}
	return reason
}

func ensureApprovalActor(
	ctx context.Context,
	organizationID uuid.UUID,
	userAccountID uuid.UUID,
) error {
	var exists bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM Organization_UserAccount membership
		  WHERE membership.organization_id = @organizationID
		    AND membership.user_account_id = @userAccountID
		)
	`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"userAccountID":  userAccountID,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("validate approval actor: %w", err)
	}
	if !exists {
		return apierrors.ErrForbidden
	}
	return nil
}

func approvalActorInRequiredGroup(
	ctx context.Context,
	organizationID uuid.UUID,
	groupID uuid.UUID,
	userAccountID uuid.UUID,
	decisionAt time.Time,
) (bool, error) {
	var allowed bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM PrincipalGroupMember member
		  JOIN LATERAL (
		    SELECT revision.state
		    FROM PrincipalGroupMemberRevision revision
		    WHERE revision.organization_id = member.organization_id
		      AND revision.principal_group_member_id = member.id
		      AND revision.effective_from <= @decisionAt
		    ORDER BY revision.effective_from DESC, revision.revision DESC
		    LIMIT 1
		  ) current_revision ON current_revision.state = 'active'
		  JOIN Organization_UserAccount membership
		    ON membership.organization_id = member.organization_id
		   AND membership.user_account_id = member.user_account_id
		   AND membership.created_at = member.user_membership_created_at
		  WHERE member.organization_id = @organizationID
		    AND member.group_id = @groupID
		    AND member.user_account_id = @userAccountID
		    AND member.effective_from <= @decisionAt
		    AND (
		      member.effective_until IS NULL
		      OR member.effective_until > @decisionAt
		    )
		)
	`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"groupID":        groupID,
			"userAccountID":  userAccountID,
			"decisionAt":     decisionAt,
		},
	).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("authorize approval requirement group: %w", err)
	}
	return allowed, nil
}

func currentAuthorizedApprovalDecisions(
	ctx context.Context,
	request types.ApprovalRequest,
	decisionAt time.Time,
) ([]types.ApprovalDecision, error) {
	if len(request.Decisions) == 0 {
		return []types.ApprovalDecision{}, nil
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT decision.id
		FROM ApprovalDecision decision
		JOIN ApprovalRequirement requirement
		  ON requirement.id = decision.approval_requirement_id
		 AND requirement.approval_request_id = decision.approval_request_id
		 AND requirement.organization_id = decision.organization_id
		JOIN PrincipalGroupMember member
		  ON member.organization_id = requirement.organization_id
		 AND member.group_id = requirement.principal_group_id
		 AND member.user_account_id = decision.actor_useraccount_id
		 AND member.effective_from <= @decisionAt
		 AND (
		   member.effective_until IS NULL
		   OR member.effective_until > @decisionAt
		 )
		JOIN LATERAL (
		  SELECT revision.state
		  FROM PrincipalGroupMemberRevision revision
		  WHERE revision.organization_id = member.organization_id
		    AND revision.principal_group_member_id = member.id
		    AND revision.effective_from <= @decisionAt
		  ORDER BY revision.effective_from DESC, revision.revision DESC
		  LIMIT 1
		) current_revision ON current_revision.state = 'active'
		JOIN Organization_UserAccount membership
		  ON membership.organization_id = member.organization_id
		 AND membership.user_account_id = member.user_account_id
		 AND membership.created_at = member.user_membership_created_at
		WHERE decision.organization_id = @organizationID
		  AND decision.approval_request_id = @approvalRequestID
		  AND decision.decision = 'APPROVE'
	`, pgx.NamedArgs{
		"organizationID":    request.OrganizationID,
		"approvalRequestID": request.ID,
		"decisionAt":        decisionAt.UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("revalidate current approval decision authority: %w", err)
	}
	authorizedIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("collect current approval decision authority: %w", err)
	}
	authorized := make(map[uuid.UUID]struct{}, len(authorizedIDs))
	for _, decisionID := range authorizedIDs {
		authorized[decisionID] = struct{}{}
	}
	result := make([]types.ApprovalDecision, 0, len(request.Decisions))
	for _, decision := range request.Decisions {
		if decision.Decision == types.ApprovalDecisionReject {
			result = append(result, decision)
			continue
		}
		if _, ok := authorized[decision.ID]; ok {
			result = append(result, decision)
		}
	}
	return result, nil
}

func approvalDatabaseTime(ctx context.Context) (time.Time, error) {
	var result time.Time
	if err := internalctx.GetDb(ctx).QueryRow(ctx, "SELECT now()").Scan(&result); err != nil {
		return time.Time{}, fmt.Errorf("read approval decision time: %w", err)
	}
	return result.UTC(), nil
}

func hydrateApprovalRequest(
	ctx context.Context,
	request *types.ApprovalRequest,
) error {
	var err error
	request.Requirements, err = listApprovalRequirements(
		ctx,
		request.ID,
		request.OrganizationID,
	)
	if err != nil {
		return err
	}
	request.Decisions, err = listApprovalDecisions(
		ctx,
		request.ID,
		request.OrganizationID,
	)
	return err
}

func hydrateApprovalRequests(
	ctx context.Context,
	requests []types.ApprovalRequest,
) error {
	if len(requests) == 0 {
		return nil
	}
	organizationID := requests[0].OrganizationID
	requestIDs := make([]uuid.UUID, len(requests))
	indexByID := make(map[uuid.UUID]int, len(requests))
	for index := range requests {
		if requests[index].OrganizationID != organizationID {
			return errors.New("cannot hydrate approval requests across organizations")
		}
		requestIDs[index] = requests[index].ID
		indexByID[requests[index].ID] = index
		requests[index].Requirements = []types.ApprovalRequirement{}
		requests[index].Decisions = []types.ApprovalDecision{}
	}
	requirementRows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+approvalRequirementOutputExpr+`
		FROM ApprovalRequirement requirement
		WHERE requirement.organization_id = @organizationID
		  AND requirement.approval_request_id = ANY(@requestIDs)
		ORDER BY requirement.approval_request_id, requirement.sort_order, requirement.id
	`, pgx.NamedArgs{
		"organizationID": organizationID,
		"requestIDs":     requestIDs,
	})
	if err != nil {
		return fmt.Errorf("batch list approval requirements: %w", err)
	}
	requirements, err := pgx.CollectRows(
		requirementRows,
		pgx.RowToStructByName[types.ApprovalRequirement],
	)
	if err != nil {
		return fmt.Errorf("batch collect approval requirements: %w", err)
	}
	for _, requirement := range requirements {
		if index, ok := indexByID[requirement.ApprovalRequestID]; ok {
			requests[index].Requirements = append(
				requests[index].Requirements,
				requirement,
			)
		}
	}
	decisionRows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+approvalDecisionOutputExpr+`
		FROM ApprovalDecision decision
		WHERE decision.organization_id = @organizationID
		  AND decision.approval_request_id = ANY(@requestIDs)
		ORDER BY decision.approval_request_id, decision.created_at, decision.id
	`, pgx.NamedArgs{
		"organizationID": organizationID,
		"requestIDs":     requestIDs,
	})
	if err != nil {
		return fmt.Errorf("batch list approval decisions: %w", err)
	}
	decisions, err := pgx.CollectRows(
		decisionRows,
		pgx.RowToStructByName[types.ApprovalDecision],
	)
	if err != nil {
		return fmt.Errorf("batch collect approval decisions: %w", err)
	}
	for _, decision := range decisions {
		if index, ok := indexByID[decision.ApprovalRequestID]; ok {
			requests[index].Decisions = append(requests[index].Decisions, decision)
		}
	}
	return nil
}

func normalizeApprovalListFilter(
	filter types.ApprovalRequestListFilter,
) (int, *approvalCursor, error) {
	if filter.OrganizationID == uuid.Nil {
		return 0, nil, apierrors.NewBadRequest("organizationId is required")
	}
	if filter.State != "" && !filter.State.IsValid() {
		return 0, nil, apierrors.NewBadRequest("state is invalid")
	}
	limit := filter.Limit
	if limit == 0 {
		limit = approvalDefaultPageLimit
	}
	if limit < 1 || limit > approvalMaximumPageLimit {
		return 0, nil, apierrors.NewBadRequest("limit must be between 1 and 100")
	}
	cursor, err := decodeApprovalCursor(filter.Cursor)
	if err != nil {
		return 0, nil, err
	}
	return limit, cursor, nil
}

func encodeApprovalCursor(cursor approvalCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode approval cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeApprovalCursor(value string) (*approvalCursor, error) {
	if value == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor approvalCursor
	if err := decoder.Decode(&cursor); err != nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	if cursor.Version != approvalCursorVersion ||
		cursor.CreatedAt.IsZero() ||
		cursor.ID == uuid.Nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	return &cursor, nil
}

func mapApprovalWriteError(action string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgerrcode.UniqueViolation:
			return apierrors.NewConflict(action + " conflicts with existing approval evidence")
		case pgerrcode.ForeignKeyViolation:
			return apierrors.ErrNotFound
		case pgerrcode.CheckViolation:
			return apierrors.NewBadRequest(action + " violates the approval contract")
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}
