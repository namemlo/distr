package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCampaignRevisionDraftEditWithoutAuthorityIsDenied(t *testing.T) {
	g := NewWithT(t)
	denied := campaignActionAuthorizerFunc(func(
		context.Context,
		types.CampaignAuthorizationContext,
		types.ResourceRef,
	) error {
		return apierrors.ErrForbidden
	})

	err := authorizeCampaignDraftMutation(
		t.Context(),
		denied,
		uuid.New(),
		uuid.New(),
		uuid.New(),
	)

	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
}

func TestCampaignRevisionProductionAuthorizerDeniesMissingAuthentication(t *testing.T) {
	g := NewWithT(t)

	err := newCampaignActionAuthorizer().AuthorizeCampaignAction(
		t.Context(),
		types.CampaignAuthorizationContext{
			OrganizationID:  uuid.New(),
			ActorUserID:     uuid.New(),
			CampaignDraftID: uuid.New(),
		},
		types.ResourceRef{
			OrganizationID: uuid.New(),
			Kind:           types.PermissionScopeCampaign,
			ID:             uuid.New(),
		},
	)

	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
}

func TestIntegratedCampaignProductionAuthorizerAllowsCreateAtOrganizationScope(t *testing.T) {
	g := NewWithT(t)
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleReadWrite
	organizationID := *userAuth.CurrentOrgID()
	actorID := userAuth.CurrentUserID()
	ctx := auth.Authentication.NewContext(t.Context(), userAuth)
	authorizer := scopedCampaignActionAuthorizer{
		dependencies: controlPlaneResourceAuthorizationDependencies{
			resolveScopes: func(
				_ context.Context,
				resource types.ResourceRef,
			) ([]types.ScopeRef, error) {
				g.Expect(resource).To(Equal(types.ResourceRef{
					OrganizationID: organizationID,
					Kind:           types.PermissionScopeOrganization,
					ID:             organizationID,
				}))
				return []types.ScopeRef{{
					Kind: types.PermissionScopeOrganization,
					ID:   organizationID,
				}}, nil
			},
			authorize: func(
				_ context.Context,
				request types.AccessRequest,
			) (types.AccessDecision, error) {
				g.Expect(request.Action).To(Equal(types.ActionCampaignControl))
				g.Expect(request.ResourceScopes).To(Equal([]types.ScopeRef{{
					Kind: types.PermissionScopeOrganization,
					ID:   organizationID,
				}}))
				return types.AccessDecision{Allowed: true}, nil
			},
		},
	}

	err := authorizeCampaignDraftCreation(
		ctx,
		authorizer,
		organizationID,
		actorID,
	)

	g.Expect(err).NotTo(HaveOccurred())
}

func TestIntegratedCampaignProductionAuthorizerDeniesForeignDraft(t *testing.T) {
	g := NewWithT(t)
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleReadWrite
	organizationID := *userAuth.CurrentOrgID()
	actorID := userAuth.CurrentUserID()
	draftID := uuid.New()
	ctx := auth.Authentication.NewContext(t.Context(), userAuth)
	authorizer := scopedCampaignActionAuthorizer{
		dependencies: controlPlaneResourceAuthorizationDependencies{
			resolveScopes: func(
				_ context.Context,
				resource types.ResourceRef,
			) ([]types.ScopeRef, error) {
				g.Expect(resource).To(Equal(types.ResourceRef{
					OrganizationID: organizationID,
					Kind:           types.PermissionScopeCampaign,
					ID:             draftID,
				}))
				return nil, apierrors.ErrNotFound
			},
			authorize: func(
				context.Context,
				types.AccessRequest,
			) (types.AccessDecision, error) {
				t.Fatal("foreign campaign scope must stop before action evaluation")
				return types.AccessDecision{}, nil
			},
		},
	}

	err := authorizeCampaignDraftMutation(
		ctx,
		authorizer,
		organizationID,
		actorID,
		draftID,
	)

	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}
