package operatorqueries

import (
	"context"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func ListOperatorReconciliation(
	ctx context.Context,
	filter types.ReconciliationFilter,
	request types.PageRequest,
) (types.OperatorPage[types.OperatorReconciliationRow], error) {
	filter.Status = strings.ToUpper(strings.TrimSpace(filter.Status))
	filter.Drift = strings.ToUpper(strings.TrimSpace(filter.Drift))
	if !validOperatorReconciliationFilter(filter) {
		return types.OperatorPage[types.OperatorReconciliationRow]{}, apierrors.ErrBadRequest
	}

	scopes, err := AuditViewScopesFromOperatorScopeFilter(filter.OperatorScopeFilter)
	if err != nil {
		return types.OperatorPage[types.OperatorReconciliationRow]{}, err
	}
	if scopes.Empty() {
		return types.OperatorPage[types.OperatorReconciliationRow]{}, apierrors.ErrForbidden
	}
	limit, err := NormalizePageRequest(request)
	if err != nil {
		return types.OperatorPage[types.OperatorReconciliationRow]{}, err
	}
	cursorScope, err := reconciliationCursorScope(filter, scopes)
	if err != nil {
		return types.OperatorPage[types.OperatorReconciliationRow]{}, err
	}
	cursor, err := DecodeCursor(request.Cursor, cursorScope)
	if err != nil {
		return types.OperatorPage[types.OperatorReconciliationRow]{}, err
	}

	var afterCreatedAt *time.Time
	var afterID *uuid.UUID
	if cursor != nil {
		afterCreatedAt = new(cursor.CreatedAt)
		afterID = new(cursor.ID)
	}
	items, total, err := db.ListOperatorReconciliationRows(
		ctx,
		filter,
		afterCreatedAt,
		afterID,
		limit+1,
	)
	if err != nil {
		return types.OperatorPage[types.OperatorReconciliationRow]{}, err
	}
	return CompletePage(items, limit, cursorScope, total, func(item types.OperatorReconciliationRow) CursorTuple {
		return CursorTuple{CreatedAt: item.CreatedAt, ID: item.ID}
	})
}

func GetOperatorReconciliation(
	ctx context.Context,
	filter types.OperatorScopeFilter,
	reconciliationID uuid.UUID,
) (*types.OperatorReconciliationDetail, error) {
	scopes, err := AuditViewScopesFromOperatorScopeFilter(filter)
	if err != nil {
		return nil, err
	}
	if scopes.Empty() {
		return nil, apierrors.ErrForbidden
	}
	return db.GetOperatorReconciliationDetail(ctx, filter, reconciliationID)
}

func ListOperatorReconciliationEvidence(
	ctx context.Context,
	filter types.OperatorScopeFilter,
	reconciliationID uuid.UUID,
) ([]types.OperatorEvidenceRef, error) {
	detail, err := GetOperatorReconciliation(ctx, filter, reconciliationID)
	if err != nil {
		return nil, err
	}
	return detail.Evidence, nil
}

func reconciliationCursorScope(
	filter types.ReconciliationFilter,
	scopes AuditViewScopes,
) (CursorScope, error) {
	filterChecksum, err := CanonicalFilterChecksum(struct {
		Status             string     `json:"status"`
		Drift              string     `json:"drift"`
		EnvironmentID      *uuid.UUID `json:"environmentId"`
		DeploymentTargetID *uuid.UUID `json:"deploymentTargetId"`
	}{
		Status: filter.Status, Drift: filter.Drift,
		EnvironmentID: filter.EnvironmentID, DeploymentTargetID: filter.DeploymentTargetID,
	})
	if err != nil {
		return CursorScope{}, err
	}
	return CursorScope{
		OrganizationID: filter.OrganizationID,
		Collection:     types.OperatorCollectionReconciliation,
		DecisionAt:     scopes.DecisionAt,
		ScopeChecksum:  scopes.Checksum(),
		FilterChecksum: filterChecksum,
	}, nil
}

func validOperatorReconciliationFilter(filter types.ReconciliationFilter) bool {
	if invalidOptionalOperatorUUID(filter.EnvironmentID) ||
		invalidOptionalOperatorUUID(filter.DeploymentTargetID) {
		return false
	}
	switch types.DriftCaseStatus(filter.Status) {
	case "", types.DriftCaseStatusOpen, types.DriftCaseStatusAssigned,
		types.DriftCaseStatusException, types.DriftCaseStatusResolved:
	default:
		return false
	}
	switch types.DriftClass(filter.Drift) {
	case "", types.DriftClassArtifact, types.DriftClassConfiguration,
		types.DriftClassSchema, types.DriftClassCapability, types.DriftClassHealth,
		types.DriftClassPlatform, types.DriftClassTopology, types.DriftClassMissing,
		types.DriftClassStale, types.DriftClassUnverified,
		types.DriftClassExecutorMismatch, types.DriftClassConflict:
		return true
	default:
		return false
	}
}

func invalidOptionalOperatorUUID(value *uuid.UUID) bool {
	return value != nil && *value == uuid.Nil
}
