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
	decision.idempotency_key
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
			reason := governance.DetectApprovalInvalidation(
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
		return nil
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
		if err := input.Authorize(ctx, types.ApprovalAuthorizationContext{
			OrganizationID:        request.OrganizationID,
			ActorUserAccountID:    input.ActorUserAccountID,
			DecisionAt:            decisionAt,
			DeploymentPlanID:      request.SubjectID,
			ApprovalRequestID:     request.ID,
			ApprovalRequirementID: input.ApprovalRequirementID,
		}); err != nil {
			return err
		}
		existing, err := getIdempotentApprovalDecision(ctx, input)
		if err != nil && !errors.Is(err, apierrors.ErrNotFound) {
			return err
		}
		if existing != nil {
			if approvalDecisionMatchesInput(*existing, input) {
				result = existing
				return nil
			}
			return apierrors.NewConflict(
				"idempotency key is already bound to a different approval decision",
			)
		}

		invalidationReason = observedReason
		if invalidationReason == "" {
			invalidationReason = governance.DetectApprovalInvalidation(
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
			}
			return nil
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
		decisions, err := listApprovalDecisions(
			ctx,
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
		return nil
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
			reason = governance.DetectApprovalInvalidation(
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
		}
		result = governance.EvaluateApproval(*request, request.Decisions, decisionAt)
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
		return updateApprovalRequestState(
			ctx,
			request,
			stateForApprovalInvalidation(reason),
			reason,
			decisionAt,
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
	requirements := make(
		[]types.ApprovalRequirement,
		len(plan.EffectivePolicy.ApprovalRules),
	)
	for index, rule := range plan.EffectivePolicy.ApprovalRules {
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
			idempotency_key
		) VALUES (
			@id,
			@organizationID,
			@approvalRequestID,
			@approvalRequirementID,
			@actorUserAccountID,
			@decision,
			@comment,
			@requestRevision,
			@idempotencyKey
		)
		RETURNING `+approvalDecisionOutputExpr,
		pgx.NamedArgs{
			"id":                    decision.ID,
			"organizationID":        decision.OrganizationID,
			"approvalRequestID":     decision.ApprovalRequestID,
			"approvalRequirementID": decision.ApprovalRequirementID,
			"actorUserAccountID":    decision.ActorUserAccountID,
			"decision":              decision.Decision,
			"comment":               decision.Comment,
			"requestRevision":       decision.RequestRevision,
			"idempotencyKey":        decision.IdempotencyKey,
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

func currentApprovalSubjectSnapshot(
	ctx context.Context,
	request types.ApprovalRequest,
) (
	types.ApprovalSubjectSnapshot,
	types.ApprovalInvalidationReason,
	error,
) {
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
