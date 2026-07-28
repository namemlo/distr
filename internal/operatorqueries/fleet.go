package operatorqueries

import (
	"context"
	"errors"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type fleetRepository func(
	context.Context,
	db.OperatorFleetQuery,
) (db.OperatorFleetResult, error)

func ListFleet(
	ctx context.Context,
	filter types.FleetFilter,
	pageRequest types.PageRequest,
) (types.OperatorPage[types.FleetRow], error) {
	return listFleetWithRepository(ctx, filter, pageRequest, db.ListOperatorFleetRows)
}

func listFleetWithRepository(
	ctx context.Context,
	filter types.FleetFilter,
	pageRequest types.PageRequest,
	repository fleetRepository,
) (types.OperatorPage[types.FleetRow], error) {
	page := types.OperatorPage[types.FleetRow]{Items: []types.FleetRow{}}
	if repository == nil {
		return page, errors.New("fleet repository is required")
	}
	normalized, scopes, err := normalizeFleetFilter(filter)
	if err != nil {
		return page, err
	}
	if scopes.Empty() {
		return page, apierrors.ErrForbidden
	}
	limit, err := NormalizePageRequest(pageRequest)
	if err != nil {
		return page, err
	}
	cursorScope, err := fleetCursorScopeFromNormalized(normalized, scopes)
	if err != nil {
		return page, err
	}
	cursor, err := DecodeCursor(pageRequest.Cursor, cursorScope)
	if err != nil {
		return page, err
	}

	query := db.OperatorFleetQuery{
		OrganizationID:         scopes.OrganizationID,
		DecisionAt:             scopes.DecisionAt,
		OrganizationWide:       scopes.OrganizationWide,
		CustomerScopeIDs:       scopes.CustomerIDs,
		EnvironmentScopeIDs:    scopes.EnvironmentIDs,
		DeploymentUnitScopeIDs: scopes.DeploymentUnitIDs,
		ComponentScopeIDs:      scopes.ComponentIDs,
		Filter:                 normalized,
		Limit:                  limit + 1,
	}
	if cursor != nil {
		query.Cursor = &db.OperatorFleetCursor{
			CreatedAt: cursor.CreatedAt,
			ID:        cursor.ID,
		}
	}
	result, err := repository(ctx, query)
	if err != nil {
		return page, err
	}
	total := result.Total
	return CompletePage(
		result.Items,
		limit,
		cursorScope,
		&total,
		func(row types.FleetRow) CursorTuple {
			return CursorTuple{CreatedAt: row.CreatedAt, ID: row.ID}
		},
	)
}

func fleetCursorScope(filter types.FleetFilter) (CursorScope, error) {
	normalized, scopes, err := normalizeFleetFilter(filter)
	if err != nil {
		return CursorScope{}, err
	}
	if scopes.Empty() {
		return CursorScope{}, apierrors.ErrForbidden
	}
	return fleetCursorScopeFromNormalized(normalized, scopes)
}

func fleetCursorScopeFromNormalized(
	filter types.FleetFilter,
	scopes AuditViewScopes,
) (CursorScope, error) {
	filterChecksum, err := CanonicalFilterChecksum(struct {
		CustomerOrganizationID *uuid.UUID `json:"customerOrganizationId,omitempty"`
		EnvironmentID          *uuid.UUID `json:"environmentId,omitempty"`
		DeploymentTargetID     *uuid.UUID `json:"deploymentTargetId,omitempty"`
		DeploymentUnitID       *uuid.UUID `json:"deploymentUnitId,omitempty"`
		Component              string     `json:"component,omitempty"`
		ObservedState          string     `json:"observedState,omitempty"`
		Drift                  string     `json:"drift,omitempty"`
		Enrollment             string     `json:"enrollment,omitempty"`
		Search                 string     `json:"search,omitempty"`
	}{
		CustomerOrganizationID: filter.CustomerOrganizationID,
		EnvironmentID:          filter.EnvironmentID,
		DeploymentTargetID:     filter.DeploymentTargetID,
		DeploymentUnitID:       filter.DeploymentUnitID,
		Component:              filter.Component,
		ObservedState:          filter.ObservedState,
		Drift:                  filter.Drift,
		Enrollment:             filter.Enrollment,
		Search:                 filter.Search,
	})
	if err != nil {
		return CursorScope{}, err
	}
	return CursorScope{
		OrganizationID: filter.OrganizationID,
		Collection:     types.OperatorCollectionFleet,
		DecisionAt:     filter.DecisionAt,
		ScopeChecksum:  scopes.Checksum(),
		FilterChecksum: filterChecksum,
	}, nil
}

func normalizeFleetFilter(
	filter types.FleetFilter,
) (types.FleetFilter, AuditViewScopes, error) {
	scopes, err := AuditViewScopesFromOperatorScopeFilter(filter.OperatorScopeFilter)
	if err != nil {
		return types.FleetFilter{}, AuditViewScopes{}, err
	}
	filter.OperatorScopeFilter = scopes.ToOperatorScopeFilter()
	filter.Component = strings.ToLower(strings.TrimSpace(filter.Component))
	filter.ObservedState = strings.ToLower(strings.TrimSpace(filter.ObservedState))
	filter.Drift = strings.ToLower(strings.TrimSpace(filter.Drift))
	filter.Enrollment = strings.ToLower(strings.TrimSpace(filter.Enrollment))
	filter.Search = strings.ToLower(strings.TrimSpace(filter.Search))

	if filter.OrganizationID == uuid.Nil || filter.DecisionAt.IsZero() ||
		invalidFleetFilterID(filter.CustomerOrganizationID) ||
		invalidFleetFilterID(filter.EnvironmentID) ||
		invalidFleetFilterID(filter.DeploymentTargetID) ||
		invalidFleetFilterID(filter.DeploymentUnitID) ||
		!validFleetStateFilter(
			filter.ObservedState,
			"unknown", "conflict", "stale", "partial", "healthy", "unhealthy",
		) ||
		!validFleetStateFilter(
			filter.Drift,
			"unknown", "conflict", "stale", "drifted", "in_sync",
		) ||
		!validFleetStateFilter(filter.Enrollment, "unknown", "enabled", "disabled") {
		return types.FleetFilter{}, AuditViewScopes{}, apierrors.ErrBadRequest
	}

	return filter, scopes, nil
}

func invalidFleetFilterID(value *uuid.UUID) bool {
	return value != nil && *value == uuid.Nil
}

func validFleetStateFilter(value string, allowed ...string) bool {
	if value == "" {
		return true
	}
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
