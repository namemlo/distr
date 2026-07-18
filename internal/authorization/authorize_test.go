package authorization

import (
	"context"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestAuthorizationExactAndAncestorScopeMatches(t *testing.T) {
	now := time.Date(2026, time.July, 18, 3, 0, 0, 0, time.UTC)
	organizationID := uuid.New()
	principalID := uuid.New()
	customerID := uuid.New()
	environmentID := uuid.New()
	unitID := uuid.New()
	exactBindingID := uuid.New()
	ancestorBindingID := uuid.New()
	repository := &fakeRepository{
		grants: []types.AccessGrant{
			{
				BindingID:     exactBindingID,
				Scope:         types.ScopeRef{Kind: types.PermissionScopeEnvironment, ID: environmentID},
				Actions:       []types.Action{types.ActionPlanExecute},
				EffectiveFrom: now.Add(-time.Hour),
			},
			{
				BindingID:     ancestorBindingID,
				Scope:         types.ScopeRef{Kind: types.PermissionScopeOrganization, ID: organizationID},
				Actions:       []types.Action{types.ActionPlanExecute},
				EffectiveFrom: now.Add(-time.Hour),
			},
		},
	}
	service := NewService(repository, WithClock(func() time.Time { return now }))

	decision, err := service.Authorize(context.Background(), types.AccessRequest{
		OrganizationID: organizationID,
		PrincipalID:    principalID,
		Action:         types.ActionPlanExecute,
		ResourceScopes: []types.ScopeRef{
			{Kind: types.PermissionScopeOrganization, ID: organizationID},
			{Kind: types.PermissionScopeCustomer, ID: customerID},
			{Kind: types.PermissionScopeEnvironment, ID: environmentID},
			{Kind: types.PermissionScopeDeploymentUnit, ID: unitID},
		},
	})

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision.Allowed).To(BeTrue())
	g.Expect(decision.MatchedBindings).To(ConsistOf(exactBindingID, ancestorBindingID))
	g.Expect(decision.ReasonCode).To(Equal(types.AccessReasonBindingMatch))
}

func TestAuthorizationDeniesWrongScopedAndExpiredBindingsWithoutLeaking(t *testing.T) {
	now := time.Date(2026, time.July, 18, 3, 0, 0, 0, time.UTC)
	organizationID := uuid.New()
	principalID := uuid.New()
	expectedCustomerID := uuid.New()
	foreignCustomerID := uuid.New()
	expiredAt := now.Add(-time.Minute)
	repository := &fakeRepository{
		grants: []types.AccessGrant{
			{
				BindingID:     uuid.New(),
				Scope:         types.ScopeRef{Kind: types.PermissionScopeCustomer, ID: foreignCustomerID},
				Actions:       []types.Action{types.ActionPlanExecute},
				EffectiveFrom: now.Add(-time.Hour),
			},
			{
				BindingID:      uuid.New(),
				Scope:          types.ScopeRef{Kind: types.PermissionScopeCustomer, ID: expectedCustomerID},
				Actions:        []types.Action{types.ActionPlanExecute},
				EffectiveFrom:  now.Add(-time.Hour),
				EffectiveUntil: &expiredAt,
			},
		},
	}
	service := NewService(repository, WithClock(func() time.Time { return now }))

	request := types.AccessRequest{
		OrganizationID: organizationID,
		PrincipalID:    principalID,
		Action:         types.ActionPlanExecute,
		ResourceScopes: []types.ScopeRef{
			{Kind: types.PermissionScopeOrganization, ID: organizationID},
			{Kind: types.PermissionScopeCustomer, ID: expectedCustomerID},
		},
	}
	decision, err := service.Authorize(context.Background(), request)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision).To(Equal(types.AccessDecision{
		Allowed:         false,
		MatchedBindings: []uuid.UUID{},
		ReasonCode:      types.AccessReasonDenied,
	}))

	request.ResourceScopes[1].ID = uuid.New()
	randomDecision, err := service.Authorize(context.Background(), request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(randomDecision).To(Equal(decision))
}

func TestAuthorizationDeniesWrongCustomerEnvironmentAndUnit(t *testing.T) {
	now := time.Date(2026, time.July, 18, 3, 0, 0, 0, time.UTC)
	organizationID := uuid.New()
	principalID := uuid.New()

	for _, scopeKind := range []types.PermissionScope{
		types.PermissionScopeCustomer,
		types.PermissionScopeEnvironment,
		types.PermissionScopeDeploymentUnit,
	} {
		t.Run(string(scopeKind), func(t *testing.T) {
			expectedID := uuid.New()
			service := NewService(
				&fakeRepository{grants: []types.AccessGrant{{
					BindingID:     uuid.New(),
					Scope:         types.ScopeRef{Kind: scopeKind, ID: uuid.New()},
					Actions:       []types.Action{types.ActionPlanExecute},
					EffectiveFrom: now.Add(-time.Hour),
				}}},
				WithClock(func() time.Time { return now }),
			)

			decision, err := service.Authorize(context.Background(), types.AccessRequest{
				OrganizationID: organizationID,
				PrincipalID:    principalID,
				Action:         types.ActionPlanExecute,
				ResourceScopes: []types.ScopeRef{
					{Kind: types.PermissionScopeOrganization, ID: organizationID},
					{Kind: scopeKind, ID: expectedID},
				},
			})

			g := NewWithT(t)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(decision.Allowed).To(BeFalse())
			g.Expect(decision.ReasonCode).To(Equal(types.AccessReasonDenied))
		})
	}
}

func TestAuthorizationUsesEffectiveGroupGrant(t *testing.T) {
	now := time.Date(2026, time.July, 18, 3, 0, 0, 0, time.UTC)
	organizationID := uuid.New()
	principalID := uuid.New()
	bindingID := uuid.New()
	repository := &fakeRepository{
		grants: []types.AccessGrant{{
			BindingID:     bindingID,
			PrincipalKind: types.AuthorizationPrincipalGroup,
			Scope:         types.ScopeRef{Kind: types.PermissionScopeOrganization, ID: organizationID},
			Actions:       []types.Action{types.ActionAuditView},
			EffectiveFrom: now.Add(-time.Minute),
		}},
	}
	service := NewService(repository, WithClock(func() time.Time { return now }))

	decision, err := service.Authorize(context.Background(), types.AccessRequest{
		OrganizationID: organizationID,
		PrincipalID:    principalID,
		Action:         types.ActionAuditView,
		ResourceScopes: []types.ScopeRef{{
			Kind: types.PermissionScopeOrganization,
			ID:   organizationID,
		}},
	})

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision.Allowed).To(BeTrue())
	g.Expect(decision.MatchedBindings).To(Equal([]uuid.UUID{bindingID}))
}

func TestAuthorizationSeparatesViewFromMutationAndFallsBackToLegacyRole(t *testing.T) {
	now := time.Date(2026, time.July, 18, 3, 0, 0, 0, time.UTC)
	organizationID := uuid.New()
	principalID := uuid.New()
	legacyRole := types.UserRoleReadOnly
	repository := &fakeRepository{legacyRole: &legacyRole}
	service := NewService(repository, WithClock(func() time.Time { return now }))
	base := types.AccessRequest{
		OrganizationID: organizationID,
		PrincipalID:    principalID,
		ResourceScopes: []types.ScopeRef{{
			Kind: types.PermissionScopeOrganization,
			ID:   organizationID,
		}},
	}

	view := base
	view.Action = types.ActionAuditView
	viewDecision, err := service.Authorize(context.Background(), view)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(viewDecision.Allowed).To(BeTrue())
	g.Expect(viewDecision.ReasonCode).To(Equal(types.AccessReasonLegacyFallback))

	mutation := base
	mutation.Action = types.ActionPlanExecute
	mutationDecision, err := service.Authorize(context.Background(), mutation)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(mutationDecision.Allowed).To(BeFalse())
	g.Expect(mutationDecision.ReasonCode).To(Equal(types.AccessReasonDenied))
}

type fakeRepository struct {
	grants      []types.AccessGrant
	legacyRole  *types.UserRole
	scopes      []types.ScopeRef
	enrollments []types.ControlPlaneEnrollment
	err         error
}

func (r *fakeRepository) ListAccessGrants(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) ([]types.AccessGrant, error) {
	return append([]types.AccessGrant{}, r.grants...), r.err
}

func (r *fakeRepository) GetLegacyUserRole(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*types.UserRole, error) {
	return r.legacyRole, r.err
}

func (r *fakeRepository) ResolveResourceScopes(
	context.Context,
	types.ResourceRef,
) ([]types.ScopeRef, error) {
	return append([]types.ScopeRef{}, r.scopes...), r.err
}

func (r *fakeRepository) ListControlPlaneEnrollments(
	context.Context,
	uuid.UUID,
	types.PermissionScope,
	uuid.UUID,
) ([]types.ControlPlaneEnrollment, error) {
	return append([]types.ControlPlaneEnrollment{}, r.enrollments...), r.err
}
