package authorization

import (
	"context"
	"sort"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func ResolveResourceScopes(
	ctx context.Context,
	ref types.ResourceRef,
) ([]types.ScopeRef, error) {
	return NewService(databaseRepository{}).ResolveResourceScopes(ctx, ref)
}

func (service *Service) ResolveResourceScopes(
	ctx context.Context,
	ref types.ResourceRef,
) ([]types.ScopeRef, error) {
	if ref.OrganizationID == uuid.Nil ||
		ref.ID == uuid.Nil ||
		!ref.Kind.Supported() {
		return nil, ErrInvalidResourceRef
	}
	scopes, err := service.repository.ResolveResourceScopes(ctx, ref)
	if err != nil {
		return nil, err
	}
	scopes = append(scopes, types.ScopeRef{
		Kind: types.PermissionScopeOrganization,
		ID:   ref.OrganizationID,
	})
	return CanonicalizeResourceScopes(scopes), nil
}

func CanonicalizeResourceScopes(scopes []types.ScopeRef) []types.ScopeRef {
	result := make([]types.ScopeRef, 0, len(scopes))
	seen := make(map[types.ScopeRef]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope.ID == uuid.Nil || !scope.Kind.Supported() {
			continue
		}
		if _, exists := seen[scope]; exists {
			continue
		}
		seen[scope] = struct{}{}
		result = append(result, scope)
	}
	sort.Slice(result, func(i, j int) bool {
		leftRank := scopeRank(result[i].Kind)
		rightRank := scopeRank(result[j].Kind)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return result[i].ID.String() < result[j].ID.String()
	})
	return result
}

func IsControlPlaneV2Effective(
	ctx context.Context,
	organizationID uuid.UUID,
	environmentID uuid.UUID,
) (bool, error) {
	return NewService(databaseRepository{}).
		IsControlPlaneV2Effective(ctx, organizationID, environmentID)
}

func (service *Service) IsControlPlaneV2Effective(
	ctx context.Context,
	organizationID uuid.UUID,
	environmentID uuid.UUID,
) (bool, error) {
	if !service.processEnabled() ||
		organizationID == uuid.Nil ||
		environmentID == uuid.Nil {
		return false, nil
	}

	organizationEnrollments, err := service.repository.ListControlPlaneEnrollments(
		ctx,
		organizationID,
		types.PermissionScopeOrganization,
		organizationID,
	)
	if err != nil {
		return false, err
	}
	now := service.clock().UTC()
	if !EnrollmentEffectiveAt(organizationEnrollments, now) {
		return false, nil
	}

	environmentEnrollments, err := service.repository.ListControlPlaneEnrollments(
		ctx,
		organizationID,
		types.PermissionScopeEnvironment,
		environmentID,
	)
	if err != nil {
		return false, err
	}
	return EnrollmentEffectiveAt(environmentEnrollments, now), nil
}

func EnrollmentEffectiveAt(enrollments []types.ControlPlaneEnrollment, at time.Time) bool {
	var selected *types.ControlPlaneEnrollment
	for index := range enrollments {
		enrollment := &enrollments[index]
		if enrollment.EffectiveFrom.After(at) ||
			(enrollment.EffectiveUntil != nil && !enrollment.EffectiveUntil.After(at)) {
			continue
		}
		if selected == nil ||
			enrollment.Revision > selected.Revision ||
			(enrollment.Revision == selected.Revision &&
				enrollment.CreatedAt.After(selected.CreatedAt)) {
			selected = enrollment
		}
	}
	return selected != nil && selected.Enabled
}

func scopeRank(scope types.PermissionScope) int {
	switch scope {
	case types.PermissionScopeOrganization:
		return 0
	case types.PermissionScopeCustomer:
		return 1
	case types.PermissionScopeEnvironment:
		return 2
	case types.PermissionScopeDeploymentUnit:
		return 3
	case types.PermissionScopeComponent:
		return 4
	case types.PermissionScopeCampaign:
		return 5
	default:
		return 6
	}
}
