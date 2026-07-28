package operatorqueries

import (
	"context"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func ListOperatorCampaigns(
	ctx context.Context,
	filter types.CampaignFilter,
	request types.PageRequest,
) (types.OperatorPage[types.OperatorCampaignRow], error) {
	page := types.OperatorPage[types.OperatorCampaignRow]{Items: []types.OperatorCampaignRow{}}
	limit, cursor, err := NormalizeCampaignPage(filter, request)
	if err != nil {
		return page, err
	}
	var cursorCreatedAt *time.Time
	var cursorID *uuid.UUID
	if cursor != nil {
		cursorCreatedAt = &cursor.CreatedAt
		cursorID = &cursor.ID
	}
	items, err := db.ListOperatorCampaigns(ctx, filter, limit, cursorCreatedAt, cursorID)
	if err != nil {
		return page, err
	}
	scope, err := campaignCursorScope(filter)
	if err != nil {
		return page, err
	}
	return CompletePage(items, limit, scope, nil, func(item types.OperatorCampaignRow) CursorTuple {
		return CursorTuple{CreatedAt: item.CreatedAt, ID: item.ID}
	})
}

func GetOperatorCampaign(
	ctx context.Context,
	campaignID uuid.UUID,
	filter types.CampaignFilter,
) (*types.OperatorCampaignDetail, error) {
	return db.GetOperatorCampaign(ctx, campaignID, filter)
}

func NormalizeCampaignPage(
	filter types.CampaignFilter,
	request types.PageRequest,
) (int, *CursorTuple, error) {
	limit, err := NormalizePageRequest(request)
	if err != nil {
		return 0, nil, err
	}
	scope, err := campaignCursorScope(filter)
	if err != nil {
		return 0, nil, err
	}
	cursor, err := DecodeCursor(request.Cursor, scope)
	if err != nil {
		return 0, nil, err
	}
	return limit, cursor, nil
}

func EncodeCampaignCursor(
	filter types.CampaignFilter,
	tuple CursorTuple,
) (string, error) {
	scope, err := campaignCursorScope(filter)
	if err != nil {
		return "", err
	}
	return EncodeCursor(scope, tuple)
}

func campaignCursorScope(filter types.CampaignFilter) (CursorScope, error) {
	if filter.OrganizationID == uuid.Nil || filter.DecisionAt.IsZero() {
		return CursorScope{}, apierrors.ErrBadRequest
	}
	scopes, err := AuditViewScopesFromOperatorScopeFilter(filter.OperatorScopeFilter)
	if err != nil {
		return CursorScope{}, err
	}
	filterChecksum, err := CanonicalFilterChecksum(struct {
		Status           string     `json:"status"`
		EnvironmentID    *uuid.UUID `json:"environmentId"`
		DeploymentPlanID *uuid.UUID `json:"deploymentPlanId"`
	}{
		Status:           filter.Status,
		EnvironmentID:    filter.EnvironmentID,
		DeploymentPlanID: filter.DeploymentPlanID,
	})
	if err != nil {
		return CursorScope{}, err
	}
	return CursorScope{
		OrganizationID: filter.OrganizationID,
		Collection:     types.OperatorCollectionCampaigns,
		DecisionAt:     filter.DecisionAt.UTC(),
		ScopeChecksum:  scopes.Checksum(),
		FilterChecksum: filterChecksum,
	}, nil
}
