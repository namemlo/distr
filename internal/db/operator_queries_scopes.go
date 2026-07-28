package db

import (
	"bytes"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

// validateCanonicalOperatorScopeFilter protects DB query boundaries from
// ambiguous authorization input. Callers must pass the exact canonical scope
// representation used to bind cursor checksums and SQL visibility predicates.
func validateCanonicalOperatorScopeFilter(scope types.OperatorScopeFilter) error {
	if scope.OrganizationID == uuid.Nil || scope.DecisionAt.IsZero() ||
		!canonicalOperatorScopeIDs(scope.CustomerIDs) ||
		!canonicalOperatorScopeIDs(scope.EnvironmentIDs) ||
		!canonicalOperatorScopeIDs(scope.DeploymentUnitIDs) ||
		!canonicalOperatorScopeIDs(scope.ComponentIDs) ||
		!canonicalOperatorScopeIDs(scope.CampaignIDs) {
		return apierrors.ErrForbidden
	}

	narrow := len(scope.CustomerIDs) + len(scope.EnvironmentIDs) +
		len(scope.DeploymentUnitIDs) + len(scope.ComponentIDs) + len(scope.CampaignIDs)
	if (scope.OrganizationWide && narrow != 0) || (!scope.OrganizationWide && narrow == 0) {
		return apierrors.ErrForbidden
	}
	return nil
}

func canonicalOperatorScopeIDs(ids []uuid.UUID) bool {
	if ids == nil {
		return false
	}
	for index, id := range ids {
		if id == uuid.Nil ||
			(index > 0 && bytes.Compare(ids[index-1][:], id[:]) >= 0) {
			return false
		}
	}
	return true
}
