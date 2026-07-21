package campaignruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func TestBackgroundAdmissionAuthorizerUsesPersistedActorScopeAndDecisionTime(t *testing.T) {
	organizationID := uuid.New()
	actorID := uuid.New()
	planID := uuid.New()
	environmentID := uuid.New()
	deploymentUnitID := uuid.New()
	decisionAt := time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC)
	role := types.UserRoleReadWrite
	wantScopes := []types.ScopeRef{
		{Kind: types.PermissionScopeOrganization, ID: organizationID},
		{Kind: types.PermissionScopeEnvironment, ID: environmentID},
		{Kind: types.PermissionScopeDeploymentUnit, ID: deploymentUnitID},
	}

	authorizer := NewBackgroundAdmissionAuthorizer(BackgroundAuthorizationDependencies{
		LoadActorRole: func(_ context.Context, gotOrganizationID, gotActorID uuid.UUID) (*types.UserRole, error) {
			if gotOrganizationID != organizationID || gotActorID != actorID {
				t.Fatalf("unexpected actor lookup: %s %s", gotOrganizationID, gotActorID)
			}
			return &role, nil
		},
		ResolveResourceScopes: func(_ context.Context, ref types.ResourceRef) ([]types.ScopeRef, error) {
			want := types.ResourceRef{
				OrganizationID: organizationID,
				Kind:           types.PermissionScopeDeploymentUnit,
				ID:             deploymentUnitID,
			}
			if ref != want {
				t.Fatalf("unexpected resource: %#v", ref)
			}
			return wantScopes, nil
		},
		Authorize: func(_ context.Context, request types.AccessRequest) (types.AccessDecision, error) {
			if request.OrganizationID != organizationID || request.PrincipalID != actorID ||
				request.CredentialRole == nil || *request.CredentialRole != role ||
				request.IsSuperAdmin || request.DecisionAt != decisionAt ||
				request.Action != types.ActionPlanExecute {
				t.Fatalf("unexpected access request: %#v", request)
			}
			if len(request.ResourceScopes) != len(wantScopes) {
				t.Fatalf("unexpected scopes: %#v", request.ResourceScopes)
			}
			return types.AccessDecision{Allowed: true}, nil
		},
		IsControlPlaneV2EffectiveAt: func(
			_ context.Context,
			gotOrganizationID, gotEnvironmentID uuid.UUID,
			gotDecisionAt time.Time,
		) (bool, error) {
			if gotOrganizationID != organizationID || gotEnvironmentID != environmentID ||
				gotDecisionAt != decisionAt {
				t.Fatalf("unexpected enrollment lookup: %s %s %s", gotOrganizationID, gotEnvironmentID, gotDecisionAt)
			}
			return true, nil
		},
	})

	err := authorizer(context.Background(), types.AdmissionAuthorizationContext{
		OrganizationID:     organizationID,
		ActorUserAccountID: actorID,
		DeploymentPlanID:   planID,
		EnvironmentID:      environmentID,
		DeploymentUnitID:   &deploymentUnitID,
		Action:             string(types.ActionPlanExecute),
		DecisionAt:         decisionAt,
	})
	if err != nil {
		t.Fatalf("authorize campaign admission: %v", err)
	}
}

func TestBackgroundAdmissionAuthorizerFailsClosed(t *testing.T) {
	organizationID := uuid.New()
	actorID := uuid.New()
	environmentID := uuid.New()
	decisionAt := time.Now().UTC()
	role := types.UserRoleReadWrite
	allowedDependencies := BackgroundAuthorizationDependencies{
		LoadActorRole: func(context.Context, uuid.UUID, uuid.UUID) (*types.UserRole, error) {
			return &role, nil
		},
		ResolveResourceScopes: func(context.Context, types.ResourceRef) ([]types.ScopeRef, error) {
			return []types.ScopeRef{
				{Kind: types.PermissionScopeOrganization, ID: organizationID},
				{Kind: types.PermissionScopeEnvironment, ID: environmentID},
			}, nil
		},
		Authorize: func(context.Context, types.AccessRequest) (types.AccessDecision, error) {
			return types.AccessDecision{Allowed: true}, nil
		},
		IsControlPlaneV2EffectiveAt: func(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error) {
			return true, nil
		},
	}
	evidence := types.AdmissionAuthorizationContext{
		OrganizationID:     organizationID,
		ActorUserAccountID: actorID,
		DeploymentPlanID:   uuid.New(),
		EnvironmentID:      environmentID,
		Action:             string(types.ActionPlanExecute),
		DecisionAt:         decisionAt,
	}

	tests := []struct {
		name   string
		mutate func(*BackgroundAuthorizationDependencies, *types.AdmissionAuthorizationContext)
	}{
		{name: "wrong action", mutate: func(_ *BackgroundAuthorizationDependencies, input *types.AdmissionAuthorizationContext) {
			input.Action = string(types.ActionPlanPublish)
		}},
		{name: "missing actor role", mutate: func(dependencies *BackgroundAuthorizationDependencies, _ *types.AdmissionAuthorizationContext) {
			dependencies.LoadActorRole = func(context.Context, uuid.UUID, uuid.UUID) (*types.UserRole, error) { return nil, nil }
		}},
		{name: "access denied", mutate: func(dependencies *BackgroundAuthorizationDependencies, _ *types.AdmissionAuthorizationContext) {
			dependencies.Authorize = func(context.Context, types.AccessRequest) (types.AccessDecision, error) {
				return types.AccessDecision{}, nil
			}
		}},
		{name: "environment scope absent", mutate: func(dependencies *BackgroundAuthorizationDependencies, _ *types.AdmissionAuthorizationContext) {
			dependencies.ResolveResourceScopes = func(context.Context, types.ResourceRef) ([]types.ScopeRef, error) {
				return []types.ScopeRef{{Kind: types.PermissionScopeOrganization, ID: organizationID}}, nil
			}
		}},
		{name: "environment not enrolled", mutate: func(dependencies *BackgroundAuthorizationDependencies, _ *types.AdmissionAuthorizationContext) {
			dependencies.IsControlPlaneV2EffectiveAt = func(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error) {
				return false, nil
			}
		}},
		{name: "enrollment lookup error", mutate: func(dependencies *BackgroundAuthorizationDependencies, _ *types.AdmissionAuthorizationContext) {
			dependencies.IsControlPlaneV2EffectiveAt = func(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error) {
				return false, errors.New("database unavailable")
			}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := allowedDependencies
			input := evidence
			test.mutate(&dependencies, &input)
			err := NewBackgroundAdmissionAuthorizer(dependencies)(context.Background(), input)
			if !errors.Is(err, apierrors.ErrForbidden) {
				t.Fatalf("expected forbidden, got %v", err)
			}
		})
	}
}
