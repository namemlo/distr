package authorization

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type Repository interface {
	ListAccessGrants(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) ([]types.AccessGrant, error)
	GetLegacyUserRole(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (*types.UserRole, error)
	ResolveResourceScopes(
		context.Context,
		types.ResourceRef,
	) ([]types.ScopeRef, error)
	ListControlPlaneEnrollments(
		context.Context,
		uuid.UUID,
		types.PermissionScope,
		uuid.UUID,
	) ([]types.ControlPlaneEnrollment, error)
}

type Service struct {
	repository     Repository
	clock          func() time.Time
	processEnabled func() bool
}

type Option func(*Service)

func WithClock(clock func() time.Time) Option {
	return func(service *Service) {
		if clock != nil {
			service.clock = clock
		}
	}
}

func WithProcessFlag(enabled func() bool) Option {
	return func(service *Service) {
		if enabled != nil {
			service.processEnabled = enabled
		}
	}
}

func NewService(repository Repository, options ...Option) *Service {
	service := &Service{
		repository: repository,
		clock:      func() time.Time { return time.Now().UTC() },
		processEnabled: func() bool {
			return featureflags.NewRegistry(env.ExperimentalFeatureFlags()).
				IsEnabled(featureflags.KeyOperatorControlPlaneV2)
		},
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func Authorize(ctx context.Context, request types.AccessRequest) (types.AccessDecision, error) {
	return NewService(databaseRepository{}).Authorize(ctx, request)
}

func (service *Service) Authorize(
	ctx context.Context,
	request types.AccessRequest,
) (types.AccessDecision, error) {
	denied := types.AccessDecision{
		Allowed:         false,
		MatchedBindings: []uuid.UUID{},
		ReasonCode:      types.AccessReasonDenied,
	}
	if request.OrganizationID == uuid.Nil ||
		request.PrincipalID == uuid.Nil ||
		!request.Action.Valid() ||
		!validResourceScopes(request.OrganizationID, request.ResourceScopes) {
		return denied, nil
	}

	grants, err := service.repository.ListAccessGrants(
		ctx,
		request.OrganizationID,
		request.PrincipalID,
	)
	if err != nil {
		return denied, err
	}

	now := service.clock().UTC()
	matches := make([]uuid.UUID, 0, len(grants))
	seen := make(map[uuid.UUID]struct{}, len(grants))
	for _, grant := range grants {
		if !GrantEffectiveAt(grant, now) ||
			!slices.Contains(grant.Actions, request.Action) ||
			!slices.Contains(request.ResourceScopes, grant.Scope) {
			continue
		}
		if _, exists := seen[grant.BindingID]; exists {
			continue
		}
		seen[grant.BindingID] = struct{}{}
		matches = append(matches, grant.BindingID)
	}
	if len(matches) > 0 {
		sort.Slice(matches, func(i, j int) bool {
			return matches[i].String() < matches[j].String()
		})
		return types.AccessDecision{
			Allowed:         true,
			MatchedBindings: matches,
			ReasonCode:      types.AccessReasonBindingMatch,
		}, nil
	}

	legacyRole, err := service.repository.GetLegacyUserRole(
		ctx,
		request.OrganizationID,
		request.PrincipalID,
	)
	if err != nil {
		return denied, err
	}
	if legacyRole != nil &&
		slices.Contains(types.ActionsForLegacyRole(*legacyRole), request.Action) &&
		slices.Contains(request.ResourceScopes, types.ScopeRef{
			Kind: types.PermissionScopeOrganization,
			ID:   request.OrganizationID,
		}) {
		return types.AccessDecision{
			Allowed:         true,
			MatchedBindings: []uuid.UUID{},
			ReasonCode:      types.AccessReasonLegacyFallback,
		}, nil
	}

	return denied, nil
}

func GrantEffectiveAt(grant types.AccessGrant, at time.Time) bool {
	if grant.BindingID == uuid.Nil ||
		grant.Scope.ID == uuid.Nil ||
		!grant.Scope.Kind.Supported() ||
		grant.EffectiveFrom.After(at) {
		return false
	}
	return grant.EffectiveUntil == nil || grant.EffectiveUntil.After(at)
}

func validResourceScopes(organizationID uuid.UUID, scopes []types.ScopeRef) bool {
	if len(scopes) == 0 {
		return false
	}
	for _, scope := range scopes {
		if scope.ID == uuid.Nil || !scope.Kind.Supported() {
			return false
		}
		if scope.Kind == types.PermissionScopeOrganization && scope.ID != organizationID {
			return false
		}
	}
	return slices.Contains(scopes, types.ScopeRef{
		Kind: types.PermissionScopeOrganization,
		ID:   organizationID,
	})
}

type databaseRepository struct{}

func (databaseRepository) ListAccessGrants(
	ctx context.Context,
	organizationID uuid.UUID,
	principalID uuid.UUID,
) ([]types.AccessGrant, error) {
	return db.ListAuthorizationAccessGrants(ctx, organizationID, principalID)
}

func (databaseRepository) GetLegacyUserRole(
	ctx context.Context,
	organizationID uuid.UUID,
	principalID uuid.UUID,
) (*types.UserRole, error) {
	return db.GetAuthorizationLegacyUserRole(ctx, organizationID, principalID)
}

func (databaseRepository) ResolveResourceScopes(
	ctx context.Context,
	ref types.ResourceRef,
) ([]types.ScopeRef, error) {
	return db.ResolveAuthorizationResourceScopes(ctx, ref)
}

func (databaseRepository) ListControlPlaneEnrollments(
	ctx context.Context,
	organizationID uuid.UUID,
	scopeKind types.PermissionScope,
	scopeID uuid.UUID,
) ([]types.ControlPlaneEnrollment, error) {
	return db.ListControlPlaneEnrollmentsForScope(ctx, organizationID, scopeKind, scopeID)
}
