package operatorqueries

import (
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestResolveAuditViewScopesDeniesEmptyInactiveAndIrrelevantGrants(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	expired := now
	scopes := ResolveAuditViewScopes(organizationID, []types.AccessGrant{
		auditViewGrant(types.PermissionScopeCustomer, uuid.New(), future, nil),
		auditViewGrant(types.PermissionScopeEnvironment, uuid.New(), now.Add(-time.Hour), &expired),
		{
			BindingID:     uuid.New(),
			Scope:         types.ScopeRef{Kind: types.PermissionScopeComponent, ID: uuid.New()},
			Actions:       []types.Action{types.ActionPlanExecute},
			EffectiveFrom: now.Add(-time.Hour),
		},
		auditViewGrant(types.PermissionScopeApplication, uuid.New(), now.Add(-time.Hour), nil),
		auditViewGrant(types.PermissionScopeOrganization, uuid.New(), now.Add(-time.Hour), nil),
	}, now)

	g := NewWithT(t)
	g.Expect(scopes.Empty()).To(BeTrue())
	g.Expect(scopes.OrganizationID).To(Equal(organizationID))
	g.Expect(scopes.DecisionAt).To(Equal(now))
	g.Expect(scopes.OrganizationWide).To(BeFalse())
	g.Expect(scopes.CustomerIDs).To(BeEmpty())
	g.Expect(scopes.EnvironmentIDs).To(BeEmpty())
	g.Expect(scopes.DeploymentUnitIDs).To(BeEmpty())
	g.Expect(scopes.ComponentIDs).To(BeEmpty())
	g.Expect(scopes.CampaignIDs).To(BeEmpty())
	g.Expect(scopes.Matches(types.ScopeRef{
		Kind: types.PermissionScopeCustomer,
		ID:   uuid.New(),
	})).To(BeFalse())
	g.Expect(scopes.MatchesAny()).To(BeFalse())
}

func TestResolveAuditViewScopesGroupsSortsAndDeduplicatesSQLValues(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	ids := map[types.PermissionScope][]uuid.UUID{
		types.PermissionScopeCustomer: {
			uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"),
			uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		},
		types.PermissionScopeEnvironment: {
			uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd"),
			uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
		},
		types.PermissionScopeDeploymentUnit: {
			uuid.MustParse("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"),
		},
		types.PermissionScopeComponent: {
			uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff"),
		},
		types.PermissionScopeCampaign: {
			uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		},
	}
	grants := make([]types.AccessGrant, 0, 12)
	for kind, scopeIDs := range ids {
		for _, id := range scopeIDs {
			grants = append(grants, auditViewGrant(kind, id, now.Add(-time.Hour), nil))
		}
	}
	grants = append(grants,
		auditViewGrant(
			types.PermissionScopeCustomer,
			ids[types.PermissionScopeCustomer][0],
			now.Add(-time.Hour),
			nil,
		),
	)

	scopes := ResolveAuditViewScopes(organizationID, grants, now)

	g := NewWithT(t)
	g.Expect(scopes.Empty()).To(BeFalse())
	g.Expect(scopes.OrganizationWide).To(BeFalse())
	g.Expect(scopes.CustomerIDs).To(Equal([]uuid.UUID{
		ids[types.PermissionScopeCustomer][1],
		ids[types.PermissionScopeCustomer][0],
	}))
	g.Expect(scopes.EnvironmentIDs).To(Equal([]uuid.UUID{
		ids[types.PermissionScopeEnvironment][1],
		ids[types.PermissionScopeEnvironment][0],
	}))
	g.Expect(scopes.DeploymentUnitIDs).To(Equal(ids[types.PermissionScopeDeploymentUnit]))
	g.Expect(scopes.ComponentIDs).To(Equal(ids[types.PermissionScopeComponent]))
	g.Expect(scopes.CampaignIDs).To(Equal(ids[types.PermissionScopeCampaign]))

	for kind, scopeIDs := range ids {
		for _, id := range scopeIDs {
			g.Expect(scopes.Matches(types.ScopeRef{Kind: kind, ID: id})).To(BeTrue())
		}
	}
	g.Expect(scopes.Matches(types.ScopeRef{
		Kind: types.PermissionScopeCustomer,
		ID:   uuid.New(),
	})).To(BeFalse())
	g.Expect(scopes.MatchesAny(
		types.ScopeRef{Kind: types.PermissionScopeCustomer, ID: uuid.New()},
		types.ScopeRef{
			Kind: types.PermissionScopeEnvironment,
			ID:   ids[types.PermissionScopeEnvironment][0],
		},
	)).To(BeTrue())
}

func TestResolveAuditViewScopesOrganizationGrantSubsumesNarrowGrants(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	organizationOnly := ResolveAuditViewScopes(organizationID, []types.AccessGrant{
		auditViewGrant(
			types.PermissionScopeOrganization,
			organizationID,
			now.Add(-time.Hour),
			nil,
		),
	}, now)
	withRedundantGrant := ResolveAuditViewScopes(organizationID, []types.AccessGrant{
		auditViewGrant(
			types.PermissionScopeOrganization,
			organizationID,
			now.Add(-time.Hour),
			nil,
		),
		auditViewGrant(types.PermissionScopeCustomer, uuid.New(), now.Add(-time.Hour), nil),
	}, now)

	g := NewWithT(t)
	g.Expect(withRedundantGrant.OrganizationWide).To(BeTrue())
	g.Expect(withRedundantGrant.CustomerIDs).To(BeEmpty())
	g.Expect(withRedundantGrant.EnvironmentIDs).To(BeEmpty())
	g.Expect(withRedundantGrant.DeploymentUnitIDs).To(BeEmpty())
	g.Expect(withRedundantGrant.ComponentIDs).To(BeEmpty())
	g.Expect(withRedundantGrant.CampaignIDs).To(BeEmpty())
	g.Expect(withRedundantGrant.Checksum()).To(Equal(organizationOnly.Checksum()))
	g.Expect(withRedundantGrant.Matches(types.ScopeRef{
		Kind: types.PermissionScopeDeploymentUnit,
		ID:   uuid.New(),
	})).To(BeTrue())
	g.Expect(withRedundantGrant.Matches(types.ScopeRef{
		Kind: types.PermissionScopeApplication,
		ID:   uuid.New(),
	})).To(BeFalse())
	g.Expect(withRedundantGrant.Matches(types.ScopeRef{
		Kind: types.PermissionScopeOrganization,
		ID:   uuid.New(),
	})).To(BeFalse())
}

func TestAuditViewScopeChecksumIsCanonicalAndTenantBound(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	otherOrganizationID := uuid.New()
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	firstID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	secondID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	first := ResolveAuditViewScopes(organizationID, []types.AccessGrant{
		auditViewGrant(types.PermissionScopeEnvironment, secondID, now.Add(-time.Hour), nil),
		auditViewGrant(types.PermissionScopeEnvironment, firstID, now.Add(-time.Hour), nil),
	}, now)
	reordered := ResolveAuditViewScopes(organizationID, []types.AccessGrant{
		auditViewGrant(types.PermissionScopeEnvironment, firstID, now.Add(-time.Hour), nil),
		auditViewGrant(types.PermissionScopeEnvironment, secondID, now.Add(-time.Hour), nil),
	}, now)
	otherTenant := ResolveAuditViewScopes(otherOrganizationID, []types.AccessGrant{
		auditViewGrant(types.PermissionScopeEnvironment, firstID, now.Add(-time.Hour), nil),
		auditViewGrant(types.PermissionScopeEnvironment, secondID, now.Add(-time.Hour), nil),
	}, now)

	g := NewWithT(t)
	g.Expect(first.Checksum()).To(Equal(reordered.Checksum()))
	g.Expect(first.Checksum()).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(otherTenant.Checksum()).NotTo(Equal(first.Checksum()))
}

func TestAuditViewScopesConvertToIndependentOperatorSQLFilter(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	decisionAt := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	customerID := uuid.New()
	scopes := ResolveAuditViewScopes(organizationID, []types.AccessGrant{
		auditViewGrant(
			types.PermissionScopeCustomer,
			customerID,
			decisionAt.Add(-time.Hour),
			nil,
		),
	}, decisionAt)

	filter := scopes.ToOperatorScopeFilter()

	g := NewWithT(t)
	g.Expect(filter.OrganizationID).To(Equal(organizationID))
	g.Expect(filter.DecisionAt).To(Equal(decisionAt))
	g.Expect(filter.OrganizationWide).To(BeFalse())
	g.Expect(filter.CustomerIDs).To(Equal([]uuid.UUID{customerID}))
	g.Expect(filter.EnvironmentIDs).NotTo(BeNil())
	g.Expect(filter.DeploymentUnitIDs).NotTo(BeNil())
	g.Expect(filter.ComponentIDs).NotTo(BeNil())
	g.Expect(filter.CampaignIDs).NotTo(BeNil())

	filter.CustomerIDs[0] = uuid.New()
	g.Expect(scopes.CustomerIDs).To(Equal([]uuid.UUID{customerID}))
}

func TestAuditViewScopesRoundTripCanonicalOperatorSQLFilter(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	decisionAt := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	scopes := ResolveAuditViewScopes(organizationID, []types.AccessGrant{
		auditViewGrant(
			types.PermissionScopeEnvironment,
			uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
			decisionAt.Add(-time.Hour),
			nil,
		),
		auditViewGrant(
			types.PermissionScopeEnvironment,
			uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"),
			decisionAt.Add(-time.Hour),
			nil,
		),
	}, decisionAt)

	roundTrip, err := AuditViewScopesFromOperatorScopeFilter(scopes.ToOperatorScopeFilter())

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(roundTrip).To(Equal(scopes))
	g.Expect(roundTrip.Checksum()).To(Equal(scopes.Checksum()))
}

func TestAuditViewScopesRejectNonCanonicalOperatorSQLFilter(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	decisionAt := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	firstID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	secondID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	valid := types.OperatorScopeFilter{
		OrganizationID:    organizationID,
		DecisionAt:        decisionAt,
		CustomerIDs:       []uuid.UUID{firstID, secondID},
		EnvironmentIDs:    []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{},
		ComponentIDs:      []uuid.UUID{},
		CampaignIDs:       []uuid.UUID{},
	}

	for name, mutate := range map[string]func(*types.OperatorScopeFilter){
		"missing organization": func(filter *types.OperatorScopeFilter) {
			filter.OrganizationID = uuid.Nil
		},
		"missing decision instant": func(filter *types.OperatorScopeFilter) {
			filter.DecisionAt = time.Time{}
		},
		"organization wide with narrow ids": func(filter *types.OperatorScopeFilter) {
			filter.OrganizationWide = true
		},
		"zero scope id": func(filter *types.OperatorScopeFilter) {
			filter.CustomerIDs[0] = uuid.Nil
		},
		"unsorted ids": func(filter *types.OperatorScopeFilter) {
			filter.CustomerIDs[0], filter.CustomerIDs[1] =
				filter.CustomerIDs[1], filter.CustomerIDs[0]
		},
		"duplicate ids": func(filter *types.OperatorScopeFilter) {
			filter.CustomerIDs[1] = filter.CustomerIDs[0]
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			candidate.CustomerIDs = slicesClone(valid.CustomerIDs)
			mutate(&candidate)

			_, err := AuditViewScopesFromOperatorScopeFilter(candidate)

			g := NewWithT(t)
			g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
		})
	}
}

func slicesClone[T any](values []T) []T {
	return append([]T(nil), values...)
}

func auditViewGrant(
	kind types.PermissionScope,
	id uuid.UUID,
	effectiveFrom time.Time,
	effectiveUntil *time.Time,
) types.AccessGrant {
	return types.AccessGrant{
		BindingID:      uuid.New(),
		Scope:          types.ScopeRef{Kind: kind, ID: id},
		Actions:        []types.Action{types.ActionAuditView},
		EffectiveFrom:  effectiveFrom,
		EffectiveUntil: effectiveUntil,
	}
}
