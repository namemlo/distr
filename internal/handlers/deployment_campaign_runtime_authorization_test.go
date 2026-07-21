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

type campaignRuntimeAuthorizationResolverStub struct {
	revisionTarget types.CampaignRuntimeAuthorizationTarget
	runTarget      types.CampaignRuntimeAuthorizationTarget
	revisionErr    error
	runErr         error
	revisionID     uuid.UUID
	runID          uuid.UUID
	organizationID uuid.UUID
}

type campaignRuntimeSuperAdminAuth struct {
	channelTestAuth
}

func (campaignRuntimeSuperAdminAuth) IsSuperAdmin() bool { return true }

func (stub *campaignRuntimeAuthorizationResolverStub) ResolveCampaignRevisionRuntimeAuthorizationTarget(
	_ context.Context,
	organizationID uuid.UUID,
	revisionID uuid.UUID,
) (types.CampaignRuntimeAuthorizationTarget, error) {
	stub.organizationID = organizationID
	stub.revisionID = revisionID
	return stub.revisionTarget, stub.revisionErr
}

func (stub *campaignRuntimeAuthorizationResolverStub) ResolveCampaignRunRuntimeAuthorizationTarget(
	_ context.Context,
	organizationID uuid.UUID,
	runID uuid.UUID,
) (types.CampaignRuntimeAuthorizationTarget, error) {
	stub.organizationID = organizationID
	stub.runID = runID
	return stub.runTarget, stub.runErr
}

func TestCampaignRuntimeAuthorizationRequiresEveryEnvironmentBeforeEnrollment(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	campaignID := uuid.New()
	environments := []uuid.UUID{
		uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		uuid.MustParse("00000000-0000-0000-0000-000000000002"),
	}
	principalID := uuid.New()
	role := types.UserRoleReadWrite
	authorizationCalls := 0
	enrollmentCalls := 0

	err := authorizeCampaignRuntimeTarget(
		t.Context(),
		campaignRuntimeAuthorizationRequest{
			OrganizationID: organizationID, PrincipalID: principalID,
			CredentialRole: &role, DecisionAt: time.Now().UTC(),
		},
		types.CampaignRuntimeAuthorizationTarget{
			CampaignDraftID: campaignID, EnvironmentIDs: environments,
		},
		campaignRuntimeAuthorizationDependencies{
			authorize: func(_ context.Context, request types.AccessRequest) (types.AccessDecision, error) {
				authorizationCalls++
				allowed := request.ResourceScopes[1].ID != environments[1]
				return types.AccessDecision{Allowed: allowed}, nil
			},
			isEffective: func(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error) {
				enrollmentCalls++
				return true, nil
			},
		},
	)

	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
	g.Expect(authorizationCalls).To(Equal(2))
	g.Expect(enrollmentCalls).To(Equal(0))
}

func TestCampaignRuntimeAuthorizationChecksAllEnrollmentsAtOneDecisionTime(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	campaignID := uuid.New()
	environments := []uuid.UUID{uuid.New(), uuid.New()}
	principalID := uuid.New()
	role := types.UserRoleAdmin
	decisionAt := time.Date(2026, time.July, 22, 8, 9, 10, 0, time.UTC)
	checked := make(map[uuid.UUID]time.Time)

	err := authorizeCampaignRuntimeTarget(
		t.Context(),
		campaignRuntimeAuthorizationRequest{
			OrganizationID: organizationID, PrincipalID: principalID,
			CredentialRole: &role, DecisionAt: decisionAt,
		},
		types.CampaignRuntimeAuthorizationTarget{
			CampaignDraftID: campaignID, EnvironmentIDs: environments,
		},
		campaignRuntimeAuthorizationDependencies{
			authorize: func(_ context.Context, request types.AccessRequest) (types.AccessDecision, error) {
				g.Expect(request.Action).To(Equal(types.ActionCampaignControl))
				g.Expect(request.ResourceScopes).To(ConsistOf(
					types.ScopeRef{Kind: types.PermissionScopeOrganization, ID: organizationID},
					types.ScopeRef{Kind: types.PermissionScopeCampaign, ID: campaignID},
					HaveField("Kind", types.PermissionScopeEnvironment),
				))
				return types.AccessDecision{Allowed: true}, nil
			},
			isEffective: func(_ context.Context, gotOrg, environmentID uuid.UUID, at time.Time) (bool, error) {
				g.Expect(gotOrg).To(Equal(organizationID))
				checked[environmentID] = at
				return true, nil
			},
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(checked).To(HaveLen(2))
	for _, environmentID := range environments {
		g.Expect(checked[environmentID]).To(Equal(decisionAt))
	}
}

func TestCampaignRuntimeAuthorizationFailsClosedForEmptyTargetAndSuperAdmin(t *testing.T) {
	organizationID := uuid.New()
	principalID := uuid.New()
	role := types.UserRoleAdmin
	dependencies := campaignRuntimeAuthorizationDependencies{
		authorize: func(context.Context, types.AccessRequest) (types.AccessDecision, error) {
			t.Fatal("invalid requests must stop before authorization")
			return types.AccessDecision{}, nil
		},
	}

	for _, request := range []campaignRuntimeAuthorizationRequest{
		{OrganizationID: organizationID, PrincipalID: principalID, CredentialRole: &role, DecisionAt: time.Now().UTC()},
		{OrganizationID: organizationID, PrincipalID: principalID, CredentialRole: &role, IsSuperAdmin: true, DecisionAt: time.Now().UTC()},
	} {
		err := authorizeCampaignRuntimeTarget(
			t.Context(), request, types.CampaignRuntimeAuthorizationTarget{}, dependencies,
		)
		NewWithT(t).Expect(err).To(HaveOccurred())
	}
}

func TestResolvedCampaignRuntimeAuthorizerUsesAuthenticatedTenantAndOneDecisionTime(t *testing.T) {
	g := NewWithT(t)
	userAuth := testChannelAuth()
	userAuth.role = types.UserRoleReadWrite
	organizationID := *userAuth.CurrentOrgID()
	revisionID := uuid.New()
	environmentID := uuid.New()
	decisionAt := time.Date(2026, time.July, 22, 10, 11, 12, 0, time.UTC)
	resolver := &campaignRuntimeAuthorizationResolverStub{
		revisionTarget: types.CampaignRuntimeAuthorizationTarget{
			CampaignDraftID: uuid.New(),
			EnvironmentIDs:  []uuid.UUID{environmentID},
		},
	}
	ctx := auth.Authentication.NewContext(t.Context(), userAuth)
	authorizer := resolvedCampaignRuntimeAuthorizer{
		resolver: resolver,
		clock:    func() time.Time { return decisionAt },
		dependencies: campaignRuntimeAuthorizationDependencies{
			authorize: func(_ context.Context, request types.AccessRequest) (types.AccessDecision, error) {
				g.Expect(request.OrganizationID).To(Equal(organizationID))
				g.Expect(request.PrincipalID).To(Equal(userAuth.CurrentUserID()))
				g.Expect(request.DecisionAt).To(Equal(decisionAt))
				return types.AccessDecision{Allowed: true}, nil
			},
			isEffective: func(_ context.Context, gotOrg, gotEnvironment uuid.UUID, gotAt time.Time) (bool, error) {
				g.Expect(gotOrg).To(Equal(organizationID))
				g.Expect(gotEnvironment).To(Equal(environmentID))
				g.Expect(gotAt).To(Equal(decisionAt))
				return true, nil
			},
		},
	}

	err := authorizer.AuthorizeCampaignRevision(ctx, organizationID, revisionID)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resolver.organizationID).To(Equal(organizationID))
	g.Expect(resolver.revisionID).To(Equal(revisionID))
}

func TestResolvedCampaignRuntimeAuthorizerPreservesNotFoundAndRejectsMissingAuthentication(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	runID := uuid.New()
	resolver := &campaignRuntimeAuthorizationResolverStub{runErr: apierrors.ErrNotFound}
	authorizer := resolvedCampaignRuntimeAuthorizer{
		resolver: resolver,
		clock:    func() time.Time { return time.Now().UTC() },
		dependencies: campaignRuntimeAuthorizationDependencies{
			authorize: func(context.Context, types.AccessRequest) (types.AccessDecision, error) {
				t.Fatal("missing authentication must stop before authorization")
				return types.AccessDecision{}, nil
			},
		},
	}

	err := authorizer.AuthorizeCampaignRun(t.Context(), organizationID, runID)
	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
	g.Expect(resolver.runID).To(Equal(uuid.Nil))

	superAdmin := campaignRuntimeSuperAdminAuth{channelTestAuth: testChannelAuth()}
	superAdmin.orgID = organizationID
	ctx := auth.Authentication.NewContext(t.Context(), superAdmin)
	err = authorizer.AuthorizeCampaignRun(ctx, organizationID, runID)
	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
	g.Expect(resolver.runID).To(Equal(uuid.Nil))

	userAuth := testChannelAuth()
	userAuth.orgID = organizationID
	ctx = auth.Authentication.NewContext(t.Context(), userAuth)
	err = authorizer.AuthorizeCampaignRun(ctx, organizationID, runID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
	g.Expect(resolver.runID).To(Equal(runID))
}
