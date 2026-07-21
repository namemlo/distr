package campaignruntime

import (
	"context"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/authorization"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type BackgroundAuthorizationDependencies struct {
	LoadActorRole               func(context.Context, uuid.UUID, uuid.UUID) (*types.UserRole, error)
	ResolveResourceScopes       func(context.Context, types.ResourceRef) ([]types.ScopeRef, error)
	Authorize                   func(context.Context, types.AccessRequest) (types.AccessDecision, error)
	IsControlPlaneV2EffectiveAt func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		time.Time,
	) (bool, error)
}

func NewDatabaseBackgroundAdmissionAuthorizer() types.AdmissionAuthorizer {
	return NewBackgroundAdmissionAuthorizer(BackgroundAuthorizationDependencies{
		LoadActorRole:               db.GetAuthorizationLegacyUserRole,
		ResolveResourceScopes:       authorization.ResolveResourceScopes,
		Authorize:                   authorization.Authorize,
		IsControlPlaneV2EffectiveAt: authorization.IsControlPlaneV2EffectiveAt,
	})
}

func NewBackgroundAdmissionAuthorizer(
	dependencies BackgroundAuthorizationDependencies,
) types.AdmissionAuthorizer {
	return func(ctx context.Context, evidence types.AdmissionAuthorizationContext) error {
		if evidence.OrganizationID == uuid.Nil || evidence.ActorUserAccountID == uuid.Nil ||
			evidence.DeploymentPlanID == uuid.Nil || evidence.EnvironmentID == uuid.Nil ||
			evidence.DecisionAt.IsZero() ||
			types.Action(evidence.Action) != types.ActionPlanExecute ||
			dependencies.LoadActorRole == nil || dependencies.ResolveResourceScopes == nil ||
			dependencies.Authorize == nil || dependencies.IsControlPlaneV2EffectiveAt == nil {
			return apierrors.NewForbidden("campaign admission authorization is unavailable")
		}

		role, err := dependencies.LoadActorRole(
			ctx,
			evidence.OrganizationID,
			evidence.ActorUserAccountID,
		)
		if err != nil || role == nil {
			return apierrors.NewForbidden("campaign admission actor is not authorized")
		}

		resource := types.ResourceRef{
			OrganizationID: evidence.OrganizationID,
			Kind:           types.PermissionScopeEnvironment,
			ID:             evidence.EnvironmentID,
		}
		if evidence.DeploymentUnitID != nil {
			if *evidence.DeploymentUnitID == uuid.Nil {
				return apierrors.NewForbidden("campaign admission resource is invalid")
			}
			resource.Kind = types.PermissionScopeDeploymentUnit
			resource.ID = *evidence.DeploymentUnitID
		}
		scopes, err := dependencies.ResolveResourceScopes(ctx, resource)
		if err != nil {
			return apierrors.NewForbidden("campaign admission scope cannot be resolved")
		}
		if !containsEnvironmentScope(scopes, evidence.EnvironmentID) {
			return apierrors.NewForbidden("campaign admission environment scope is unavailable")
		}

		decision, err := dependencies.Authorize(ctx, types.AccessRequest{
			OrganizationID: evidence.OrganizationID,
			PrincipalID:    evidence.ActorUserAccountID,
			CredentialRole: role,
			IsSuperAdmin:   false,
			DecisionAt:     evidence.DecisionAt.UTC(),
			Action:         types.ActionPlanExecute,
			ResourceScopes: scopes,
		})
		if err != nil || !decision.Allowed {
			return apierrors.NewForbidden("campaign admission plan execution is denied")
		}

		effective, err := dependencies.IsControlPlaneV2EffectiveAt(
			ctx,
			evidence.OrganizationID,
			evidence.EnvironmentID,
			evidence.DecisionAt.UTC(),
		)
		if err != nil || !effective {
			return apierrors.NewForbidden("campaign admission environment is not enrolled")
		}
		return nil
	}
}

func containsEnvironmentScope(scopes []types.ScopeRef, environmentID uuid.UUID) bool {
	for _, scope := range scopes {
		if scope.Kind == types.PermissionScopeEnvironment && scope.ID == environmentID {
			return true
		}
	}
	return false
}
