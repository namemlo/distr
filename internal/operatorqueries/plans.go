package operatorqueries

import (
	"context"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type operatorPlanCursorFilter struct {
	Status           string     `json:"status,omitempty"`
	EnvironmentID    *uuid.UUID `json:"environmentId,omitempty"`
	DeploymentUnitID *uuid.UUID `json:"deploymentUnitId,omitempty"`
	ProductReleaseID *uuid.UUID `json:"productReleaseId,omitempty"`
}

func ListOperatorPlans(
	ctx context.Context,
	filter types.OperatorPlanFilter,
	pageRequest types.PageRequest,
) (types.OperatorPage[types.OperatorPlanRow], error) {
	empty := types.OperatorPage[types.OperatorPlanRow]{Items: []types.OperatorPlanRow{}}
	limit, err := NormalizePageRequest(pageRequest)
	if err != nil {
		return empty, err
	}
	scope, err := planCursorScope(filter)
	if err != nil {
		return empty, err
	}
	cursor, err := DecodeCursor(pageRequest.Cursor, scope)
	if err != nil {
		return empty, err
	}
	var cursorCreatedAt *time.Time
	var cursorID *uuid.UUID
	if cursor != nil {
		cursorCreatedAt = &cursor.CreatedAt
		cursorID = &cursor.ID
	}
	items, err := db.ListOperatorPlans(
		ctx,
		filter,
		limit,
		cursorCreatedAt,
		cursorID,
	)
	if err != nil {
		return empty, err
	}
	return completeOperatorPlanPage(items, limit, scope)
}

func planCursorScope(filter types.OperatorPlanFilter) (CursorScope, error) {
	scopes, err := AuditViewScopesFromOperatorScopeFilter(filter.OperatorScopeFilter)
	if err != nil {
		return CursorScope{}, err
	}
	if scopes.Empty() {
		return CursorScope{}, apierrors.NewForbidden("operator scope is empty")
	}
	filterChecksum, err := CanonicalFilterChecksum(operatorPlanCursorFilter{
		Status: filter.Status, EnvironmentID: filter.EnvironmentID,
		DeploymentUnitID: filter.DeploymentUnitID, ProductReleaseID: filter.ProductReleaseID,
	})
	if err != nil {
		return CursorScope{}, err
	}
	return CursorScope{
		OrganizationID: filter.OrganizationID,
		Collection:     types.OperatorCollectionPlans,
		DecisionAt:     scopes.DecisionAt,
		ScopeChecksum:  scopes.Checksum(),
		FilterChecksum: filterChecksum,
	}, nil
}

func completeOperatorPlanPage(
	items []types.OperatorPlanRow,
	limit int,
	scope CursorScope,
) (types.OperatorPage[types.OperatorPlanRow], error) {
	return CompletePage(items, limit, scope, nil, func(item types.OperatorPlanRow) CursorTuple {
		return CursorTuple{CreatedAt: item.CreatedAt, ID: item.ID}
	})
}

func GetOperatorPlan(
	ctx context.Context,
	scopeFilter types.OperatorScopeFilter,
	planID uuid.UUID,
) (*types.OperatorPlanDetail, error) {
	scopes, err := AuditViewScopesFromOperatorScopeFilter(scopeFilter)
	if err != nil {
		return nil, err
	}
	if scopes.Empty() {
		return nil, apierrors.ErrNotFound
	}
	return db.GetOperatorPlan(ctx, scopeFilter, planID)
}
