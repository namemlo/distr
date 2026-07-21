package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestControlPlaneResourceAuthorizationUsesResolvedScopesAndEnrollment(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	principalID := uuid.New()
	environmentID := uuid.New()
	unitID := uuid.New()
	decisionAt := time.Date(2026, time.July, 22, 4, 5, 6, 0, time.UTC)
	role := types.UserRoleReadWrite
	wantResource := types.ResourceRef{
		OrganizationID: organizationID,
		Kind:           types.PermissionScopeDeploymentUnit,
		ID:             unitID,
	}
	wantScopes := []types.ScopeRef{
		{Kind: types.PermissionScopeOrganization, ID: organizationID},
		{Kind: types.PermissionScopeEnvironment, ID: environmentID},
		{Kind: types.PermissionScopeDeploymentUnit, ID: unitID},
	}
	var gotAccess types.AccessRequest
	var gotEnrollmentAt time.Time

	err := authorizeControlPlaneResourceWithDependencies(
		t.Context(),
		controlPlaneResourceAuthorizationRequest{
			OrganizationID:    organizationID,
			PrincipalID:       principalID,
			CredentialRole:    &role,
			Action:            types.ActionPlanExecute,
			Resource:          wantResource,
			DecisionAt:        decisionAt,
			RequireEnrollment: true,
			EnvironmentID:     environmentID,
		},
		controlPlaneResourceAuthorizationDependencies{
			resolveScopes: func(_ context.Context, resource types.ResourceRef) ([]types.ScopeRef, error) {
				g.Expect(resource).To(Equal(wantResource))
				return wantScopes, nil
			},
			authorize: func(_ context.Context, request types.AccessRequest) (types.AccessDecision, error) {
				gotAccess = request
				return types.AccessDecision{Allowed: true}, nil
			},
			isEffective: func(
				_ context.Context,
				gotOrganizationID uuid.UUID,
				gotEnvironmentID uuid.UUID,
				at time.Time,
			) (bool, error) {
				g.Expect(gotOrganizationID).To(Equal(organizationID))
				g.Expect(gotEnvironmentID).To(Equal(environmentID))
				gotEnrollmentAt = at
				return true, nil
			},
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gotAccess).To(Equal(types.AccessRequest{
		OrganizationID: organizationID,
		PrincipalID:    principalID,
		CredentialRole: &role,
		DecisionAt:     decisionAt,
		Action:         types.ActionPlanExecute,
		ResourceScopes: wantScopes,
	}))
	g.Expect(gotEnrollmentAt).To(Equal(decisionAt))
}

func TestControlPlaneResourceAuthorizationDeniesRejectedAndUnenrolledMutations(t *testing.T) {
	organizationID := uuid.New()
	principalID := uuid.New()
	environmentID := uuid.New()
	role := types.UserRoleAdmin
	request := controlPlaneResourceAuthorizationRequest{
		OrganizationID: organizationID,
		PrincipalID:    principalID,
		CredentialRole: &role,
		Action:         types.ActionPlanExecute,
		Resource: types.ResourceRef{
			OrganizationID: organizationID,
			Kind:           types.PermissionScopeEnvironment,
			ID:             environmentID,
		},
		DecisionAt:        time.Now().UTC(),
		RequireEnrollment: true,
		EnvironmentID:     environmentID,
	}
	baseDependencies := controlPlaneResourceAuthorizationDependencies{
		resolveScopes: func(context.Context, types.ResourceRef) ([]types.ScopeRef, error) {
			return []types.ScopeRef{
				{Kind: types.PermissionScopeOrganization, ID: organizationID},
				{Kind: types.PermissionScopeEnvironment, ID: environmentID},
			}, nil
		},
	}

	t.Run("scoped action denied", func(t *testing.T) {
		g := NewWithT(t)
		enrollmentCalled := false
		dependencies := baseDependencies
		dependencies.authorize = func(context.Context, types.AccessRequest) (types.AccessDecision, error) {
			return types.AccessDecision{Allowed: false}, nil
		}
		dependencies.isEffective = func(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error) {
			enrollmentCalled = true
			return true, nil
		}

		err := authorizeControlPlaneResourceWithDependencies(t.Context(), request, dependencies)

		g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
		g.Expect(enrollmentCalled).To(BeFalse())
	})

	t.Run("effective enrollment denied", func(t *testing.T) {
		g := NewWithT(t)
		dependencies := baseDependencies
		dependencies.authorize = func(context.Context, types.AccessRequest) (types.AccessDecision, error) {
			return types.AccessDecision{Allowed: true}, nil
		}
		dependencies.isEffective = func(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error) {
			return false, nil
		}

		err := authorizeControlPlaneResourceWithDependencies(t.Context(), request, dependencies)

		g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
	})

	t.Run("super administrator mutation denied before scope resolution", func(t *testing.T) {
		g := NewWithT(t)
		dependencies := baseDependencies
		resolved := false
		dependencies.resolveScopes = func(context.Context, types.ResourceRef) ([]types.ScopeRef, error) {
			resolved = true
			return nil, nil
		}
		superAdminRequest := request
		superAdminRequest.IsSuperAdmin = true

		err := authorizeControlPlaneResourceWithDependencies(t.Context(), superAdminRequest, dependencies)

		g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
		g.Expect(resolved).To(BeFalse())
	})
}

func TestPR066DownstreamAdaptersAuthorizeExactActionsAndScopes(t *testing.T) {
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleAdmin
	organizationID := *userAuth.CurrentOrgID()
	actorID := userAuth.CurrentUserID()
	environmentID := uuid.New()
	unitID := uuid.New()
	campaignID := uuid.New()
	decisionAt := time.Date(2026, time.July, 22, 7, 8, 9, 0, time.UTC)
	authenticatedContext := auth.Authentication.NewContext(t.Context(), userAuth)

	newDependencies := func(
		t *testing.T,
		wantAction types.Action,
		wantResource types.ResourceRef,
		wantDecisionAt *time.Time,
		requireEnrollment bool,
	) controlPlaneResourceAuthorizationDependencies {
		g := NewWithT(t)
		return controlPlaneResourceAuthorizationDependencies{
			resolveScopes: func(_ context.Context, resource types.ResourceRef) ([]types.ScopeRef, error) {
				g.Expect(resource).To(Equal(wantResource))
				scopes := []types.ScopeRef{{
					Kind: types.PermissionScopeOrganization,
					ID:   organizationID,
				}}
				if resource.Kind == types.PermissionScopeDeploymentUnit {
					scopes = append(scopes,
						types.ScopeRef{Kind: types.PermissionScopeEnvironment, ID: environmentID},
						types.ScopeRef{Kind: types.PermissionScopeDeploymentUnit, ID: resource.ID},
					)
				} else if resource.Kind != types.PermissionScopeOrganization {
					scopes = append(scopes, types.ScopeRef{Kind: resource.Kind, ID: resource.ID})
				}
				return scopes, nil
			},
			authorize: func(_ context.Context, request types.AccessRequest) (types.AccessDecision, error) {
				g.Expect(request.OrganizationID).To(Equal(organizationID))
				g.Expect(request.PrincipalID).To(Equal(actorID))
				g.Expect(request.Action).To(Equal(wantAction))
				if wantDecisionAt != nil {
					g.Expect(request.DecisionAt).To(Equal(*wantDecisionAt))
				} else {
					g.Expect(request.DecisionAt).To(BeTemporally("~", time.Now(), time.Second))
				}
				return types.AccessDecision{Allowed: true}, nil
			},
			isEffective: func(
				_ context.Context,
				gotOrganizationID uuid.UUID,
				gotEnvironmentID uuid.UUID,
				at time.Time,
			) (bool, error) {
				g.Expect(requireEnrollment).To(BeTrue())
				g.Expect(gotOrganizationID).To(Equal(organizationID))
				g.Expect(gotEnvironmentID).To(Equal(environmentID))
				g.Expect(at).To(Equal(decisionAt))
				return true, nil
			},
		}
	}

	t.Run("admission uses plan execute and effective environment enrollment", func(t *testing.T) {
		resource := types.ResourceRef{
			OrganizationID: organizationID,
			Kind:           types.PermissionScopeDeploymentUnit,
			ID:             unitID,
		}
		err := admissionScopedAuthorizationWithDependencies(
			authenticatedContext,
			types.AdmissionAuthorizationContext{
				OrganizationID:     organizationID,
				ActorUserAccountID: actorID,
				DeploymentPlanID:   uuid.New(),
				EnvironmentID:      environmentID,
				DeploymentUnitID:   &unitID,
				Action:             string(types.ActionPlanExecute),
				DecisionAt:         decisionAt,
			},
			newDependencies(t, types.ActionPlanExecute, resource, &decisionAt, true),
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	})

	t.Run("approval uses plan publish without enrollment lookup", func(t *testing.T) {
		resource := types.ResourceRef{
			OrganizationID: organizationID,
			Kind:           types.PermissionScopeDeploymentUnit,
			ID:             unitID,
		}
		err := approvalScopedAuthorizationWithDependencies(
			t.Context(),
			approvalAuthorizationRequest{
				OrganizationID:     organizationID,
				ActorUserAccountID: actorID,
				CredentialRole:     userAuth.CurrentUserRole(),
				Action:             string(types.ActionPlanPublish),
				DecisionAt:         decisionAt,
				EnvironmentID:      environmentID,
				DeploymentUnitID:   &unitID,
			},
			newDependencies(t, types.ActionPlanPublish, resource, &decisionAt, false),
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	})

	t.Run("calendar uses calendar manage at organization scope", func(t *testing.T) {
		resource := types.ResourceRef{
			OrganizationID: organizationID,
			Kind:           types.PermissionScopeOrganization,
			ID:             organizationID,
		}
		err := (scopedCalendarActionAuthorizer{
			dependencies: newDependencies(t, types.ActionCalendarManage, resource, nil, false),
		}).AuthorizeCalendarAction(
			authenticatedContext,
			organizationID,
			actorID,
			calendarActionManage,
			types.CalendarScopeRef{
				Kind: types.CalendarScopeOrganization,
				ID:   organizationID,
			},
			time.Now().UTC(),
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	})

	t.Run("campaign uses campaign control at exact draft scope", func(t *testing.T) {
		resource := types.ResourceRef{
			OrganizationID: organizationID,
			Kind:           types.PermissionScopeCampaign,
			ID:             campaignID,
		}
		err := (scopedCampaignActionAuthorizer{
			dependencies: newDependencies(t, types.ActionCampaignControl, resource, nil, false),
		}).AuthorizeCampaignAction(
			authenticatedContext,
			types.CampaignAuthorizationContext{
				OrganizationID:  organizationID,
				ActorUserID:     actorID,
				CampaignDraftID: campaignID,
			},
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	})
}
