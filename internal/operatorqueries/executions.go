package operatorqueries

import (
	"context"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var operatorExecutionStatuses = map[string]struct{}{
	"PENDING": {}, "CLAIMED": {}, "RUNNING": {}, "SUCCEEDED": {},
	"FAILED": {}, "CANCELED": {}, "TIMED_OUT": {}, "FENCED": {},
	"UNKNOWN": {},
}

type NormalizedExecutionQuery struct {
	Filter      types.ExecutionFilter
	Scopes      AuditViewScopes
	Limit       int
	Cursor      *CursorTuple
	CursorScope CursorScope
}

type ExecutionRepository interface {
	ListOperatorExecutions(
		context.Context,
		types.ExecutionFilter,
		*time.Time,
		*uuid.UUID,
		int,
	) ([]types.OperatorExecutionRow, error)
	GetOperatorExecution(
		context.Context,
		types.OperatorScopeFilter,
		uuid.UUID,
	) (*types.OperatorExecutionDetail, error)
}

func ListOperatorExecutions(
	ctx context.Context,
	repository ExecutionRepository,
	filter types.ExecutionFilter,
	scopes AuditViewScopes,
	pageRequest types.PageRequest,
) (types.OperatorPage[types.OperatorExecutionRow], error) {
	empty := types.OperatorPage[types.OperatorExecutionRow]{Items: []types.OperatorExecutionRow{}}
	if repository == nil {
		return empty, apierrors.NewForbidden("operator execution repository is unavailable")
	}
	normalized, err := NormalizeExecutionQuery(filter, scopes, pageRequest)
	if err != nil {
		return empty, err
	}
	var after *time.Time
	var afterID *uuid.UUID
	if normalized.Cursor != nil {
		createdAt, id := normalized.Cursor.CreatedAt, normalized.Cursor.ID
		after, afterID = &createdAt, &id
	}
	items, err := repository.ListOperatorExecutions(
		ctx,
		normalized.Filter,
		after,
		afterID,
		normalized.Limit+1,
	)
	if err != nil {
		return empty, err
	}
	return CompleteExecutionPage(items, normalized.Limit, normalized.CursorScope)
}

func GetOperatorExecution(
	ctx context.Context,
	repository ExecutionRepository,
	organizationID uuid.UUID,
	scopes AuditViewScopes,
	executionID uuid.UUID,
) (*types.OperatorExecutionDetail, error) {
	if repository == nil || organizationID == uuid.Nil || executionID == uuid.Nil ||
		scopes.OrganizationID != organizationID || scopes.DecisionAt.IsZero() || scopes.Empty() {
		return nil, apierrors.NewForbidden("operator execution scope is invalid")
	}
	return repository.GetOperatorExecution(
		ctx,
		scopes.ToOperatorScopeFilter(),
		executionID,
	)
}

func NormalizeExecutionQuery(
	filter types.ExecutionFilter,
	scopes AuditViewScopes,
	page types.PageRequest,
) (NormalizedExecutionQuery, error) {
	var normalized NormalizedExecutionQuery
	if filter.OrganizationID == uuid.Nil ||
		scopes.OrganizationID == uuid.Nil ||
		filter.OrganizationID != scopes.OrganizationID ||
		scopes.DecisionAt.IsZero() || scopes.Empty() {
		return normalized, apierrors.NewForbidden("operator execution scope is invalid")
	}

	filter.Status = strings.ToUpper(strings.TrimSpace(filter.Status))
	if filter.Status != "" {
		if _, valid := operatorExecutionStatuses[filter.Status]; !valid {
			return normalized, apierrors.NewBadRequest("execution status is invalid")
		}
	}
	if filter.From != nil {
		value := filter.From.UTC()
		filter.From = &value
	}
	if filter.To != nil {
		value := filter.To.UTC()
		filter.To = &value
	}
	if filter.From != nil && filter.To != nil && !filter.From.Before(*filter.To) {
		return normalized, apierrors.NewBadRequest("from must be before to")
	}

	limit, err := NormalizePageRequest(page)
	if err != nil {
		return normalized, err
	}
	filterChecksum, err := CanonicalFilterChecksum(struct {
		Status             string     `json:"status,omitempty"`
		CampaignID         *uuid.UUID `json:"campaignId,omitempty"`
		DeploymentPlanID   *uuid.UUID `json:"deploymentPlanId,omitempty"`
		DeploymentTargetID *uuid.UUID `json:"deploymentTargetId,omitempty"`
		From               *time.Time `json:"from,omitempty"`
		To                 *time.Time `json:"to,omitempty"`
	}{
		Status: filter.Status, CampaignID: filter.CampaignID,
		DeploymentPlanID:   filter.DeploymentPlanID,
		DeploymentTargetID: filter.DeploymentTargetID,
		From:               filter.From, To: filter.To,
	})
	if err != nil {
		return normalized, err
	}
	cursorScope := CursorScope{
		OrganizationID: filter.OrganizationID,
		Collection:     types.OperatorCollectionExecutions,
		DecisionAt:     scopes.DecisionAt.UTC(),
		ScopeChecksum:  scopes.Checksum(),
		FilterChecksum: filterChecksum,
	}
	cursor, err := DecodeCursor(page.Cursor, cursorScope)
	if err != nil {
		return normalized, err
	}
	filter.OperatorScopeFilter = scopes.ToOperatorScopeFilter()
	return NormalizedExecutionQuery{
		Filter: filter, Scopes: scopes, Limit: limit, Cursor: cursor,
		CursorScope: cursorScope,
	}, nil
}

func CompleteExecutionPage(
	items []types.OperatorExecutionRow,
	limit int,
	scope CursorScope,
) (types.OperatorPage[types.OperatorExecutionRow], error) {
	page := types.OperatorPage[types.OperatorExecutionRow]{
		Items: []types.OperatorExecutionRow{},
	}
	if limit < 1 || limit > types.OperatorMaximumPageLimit {
		return page, apierrors.NewBadRequest("limit must be between 1 and 100")
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page.Items = items
	if !hasMore || len(items) == 0 {
		return page, nil
	}
	last := items[len(items)-1]
	nextCursor, err := EncodeCursor(scope, CursorTuple{
		CreatedAt: last.CreatedAt,
		ID:        last.ID,
	})
	if err != nil {
		return page, err
	}
	page.NextCursor = nextCursor
	return page, nil
}

func ExecutionObservationLabel(
	outcome string,
	freshUntil *time.Time,
	now time.Time,
) string {
	outcome = strings.ToUpper(strings.TrimSpace(outcome))
	if outcome == "" || outcome == "UNKNOWN" {
		return "UNKNOWN"
	}
	if freshUntil != nil && freshUntil.Before(now) {
		return "STALE"
	}
	return outcome
}
