package handlers

import (
	"context"
	"sort"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/authorization"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type campaignRuntimeAuthorizer interface {
	AuthorizeCampaignRevision(context.Context, uuid.UUID, uuid.UUID) error
	AuthorizeCampaignRun(context.Context, uuid.UUID, uuid.UUID) error
}

type campaignRuntimeAuthorizationTargetResolver interface {
	ResolveCampaignRevisionRuntimeAuthorizationTarget(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (types.CampaignRuntimeAuthorizationTarget, error)
	ResolveCampaignRunRuntimeAuthorizationTarget(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (types.CampaignRuntimeAuthorizationTarget, error)
}

type resolvedCampaignRuntimeAuthorizer struct {
	resolver     campaignRuntimeAuthorizationTargetResolver
	clock        func() time.Time
	dependencies campaignRuntimeAuthorizationDependencies
}

type campaignRuntimeAuthorizationRequest struct {
	OrganizationID uuid.UUID
	PrincipalID    uuid.UUID
	CredentialRole *types.UserRole
	IsSuperAdmin   bool
	DecisionAt     time.Time
}

type campaignRuntimeAuthorizationDependencies struct {
	authorize   func(context.Context, types.AccessRequest) (types.AccessDecision, error)
	isEffective func(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error)
}

func newCampaignRuntimeAuthorizer() campaignRuntimeAuthorizer {
	return resolvedCampaignRuntimeAuthorizer{
		resolver: db.CampaignRepository{},
		clock:    func() time.Time { return time.Now().UTC() },
		dependencies: campaignRuntimeAuthorizationDependencies{
			authorize:   authorization.Authorize,
			isEffective: authorization.IsControlPlaneV2EffectiveAt,
		},
	}
}

func (authorizer resolvedCampaignRuntimeAuthorizer) AuthorizeCampaignRevision(
	ctx context.Context,
	organizationID uuid.UUID,
	revisionID uuid.UUID,
) error {
	if authorizer.resolver == nil || revisionID == uuid.Nil {
		return apierrors.ErrNotFound
	}
	request, err := authorizer.authorizationRequest(ctx, organizationID)
	if err != nil {
		return err
	}
	target, err := authorizer.resolver.ResolveCampaignRevisionRuntimeAuthorizationTarget(
		ctx,
		organizationID,
		revisionID,
	)
	if err != nil {
		return err
	}
	return authorizeCampaignRuntimeTarget(ctx, request, target, authorizer.dependencies)
}

func (authorizer resolvedCampaignRuntimeAuthorizer) AuthorizeCampaignRun(
	ctx context.Context,
	organizationID uuid.UUID,
	runID uuid.UUID,
) error {
	if authorizer.resolver == nil || runID == uuid.Nil {
		return apierrors.ErrNotFound
	}
	request, err := authorizer.authorizationRequest(ctx, organizationID)
	if err != nil {
		return err
	}
	target, err := authorizer.resolver.ResolveCampaignRunRuntimeAuthorizationTarget(
		ctx,
		organizationID,
		runID,
	)
	if err != nil {
		return err
	}
	return authorizeCampaignRuntimeTarget(ctx, request, target, authorizer.dependencies)
}

func (authorizer resolvedCampaignRuntimeAuthorizer) authorizationRequest(
	ctx context.Context,
	organizationID uuid.UUID,
) (campaignRuntimeAuthorizationRequest, error) {
	authInfo, err := auth.Authentication.Get(ctx)
	if err != nil ||
		organizationID == uuid.Nil ||
		authInfo.CurrentOrgID() == nil ||
		*authInfo.CurrentOrgID() != organizationID ||
		authInfo.CurrentUserID() == uuid.Nil ||
		authInfo.CurrentUserRole() == nil ||
		authInfo.IsSuperAdmin() ||
		authorizer.clock == nil {
		return campaignRuntimeAuthorizationRequest{}, apierrors.ErrForbidden
	}
	return campaignRuntimeAuthorizationRequest{
		OrganizationID: organizationID,
		PrincipalID:    authInfo.CurrentUserID(),
		CredentialRole: authInfo.CurrentUserRole(),
		IsSuperAdmin:   authInfo.IsSuperAdmin(),
		DecisionAt:     authorizer.clock().UTC(),
	}, nil
}

func authorizeCampaignRuntimeTarget(
	ctx context.Context,
	request campaignRuntimeAuthorizationRequest,
	target types.CampaignRuntimeAuthorizationTarget,
	dependencies campaignRuntimeAuthorizationDependencies,
) error {
	if request.OrganizationID == uuid.Nil ||
		request.PrincipalID == uuid.Nil ||
		request.CredentialRole == nil ||
		request.IsSuperAdmin ||
		request.DecisionAt.IsZero() ||
		dependencies.authorize == nil ||
		dependencies.isEffective == nil {
		return apierrors.ErrForbidden
	}
	if target.CampaignDraftID == uuid.Nil || len(target.EnvironmentIDs) == 0 {
		return apierrors.ErrNotFound
	}

	environmentIDs := canonicalCampaignEnvironmentIDs(target.EnvironmentIDs)
	if len(environmentIDs) == 0 {
		return apierrors.ErrNotFound
	}

	for _, environmentID := range environmentIDs {
		decision, err := dependencies.authorize(ctx, types.AccessRequest{
			OrganizationID: request.OrganizationID,
			PrincipalID:    request.PrincipalID,
			CredentialRole: request.CredentialRole,
			IsSuperAdmin:   request.IsSuperAdmin,
			DecisionAt:     request.DecisionAt,
			Action:         types.ActionCampaignControl,
			ResourceScopes: authorization.CanonicalizeResourceScopes([]types.ScopeRef{
				{Kind: types.PermissionScopeOrganization, ID: request.OrganizationID},
				{Kind: types.PermissionScopeCampaign, ID: target.CampaignDraftID},
				{Kind: types.PermissionScopeEnvironment, ID: environmentID},
			}),
		})
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return apierrors.ErrForbidden
		}
	}

	for _, environmentID := range environmentIDs {
		effective, err := dependencies.isEffective(
			ctx,
			request.OrganizationID,
			environmentID,
			request.DecisionAt,
		)
		if err != nil {
			return err
		}
		if !effective {
			return apierrors.ErrNotFound
		}
	}
	return nil
}

func canonicalCampaignEnvironmentIDs(environmentIDs []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(environmentIDs))
	result := make([]uuid.UUID, 0, len(environmentIDs))
	for _, environmentID := range environmentIDs {
		if environmentID == uuid.Nil {
			continue
		}
		if _, exists := seen[environmentID]; exists {
			continue
		}
		seen[environmentID] = struct{}{}
		result = append(result, environmentID)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})
	return result
}
