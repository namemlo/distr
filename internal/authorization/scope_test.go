package authorization

import (
	"context"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestResolveResourceScopesCanonicalizesAndAddsOrganizationAncestor(t *testing.T) {
	organizationID := uuid.New()
	environmentID := uuid.New()
	unitID := uuid.New()
	repository := &fakeRepository{scopes: []types.ScopeRef{
		{Kind: types.PermissionScopeDeploymentUnit, ID: unitID},
		{Kind: types.PermissionScopeEnvironment, ID: environmentID},
		{Kind: types.PermissionScopeEnvironment, ID: environmentID},
	}}
	service := NewService(repository)

	scopes, err := service.ResolveResourceScopes(context.Background(), types.ResourceRef{
		OrganizationID: organizationID,
		Kind:           types.PermissionScopeDeploymentUnit,
		ID:             unitID,
	})

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(scopes).To(Equal([]types.ScopeRef{
		{Kind: types.PermissionScopeOrganization, ID: organizationID},
		{Kind: types.PermissionScopeEnvironment, ID: environmentID},
		{Kind: types.PermissionScopeDeploymentUnit, ID: unitID},
	}))
}

func TestControlPlaneEnrollmentRequiresProcessOrganizationAndEnvironmentGates(t *testing.T) {
	now := time.Date(2026, time.July, 18, 3, 0, 0, 0, time.UTC)
	organizationID := uuid.New()
	environmentID := uuid.New()
	activeFrom := now.Add(-time.Hour)
	activeUntil := now.Add(time.Hour)
	repository := &fakeRepository{}

	t.Run("process flag off short circuits", func(t *testing.T) {
		service := NewService(
			repository,
			WithClock(func() time.Time { return now }),
			WithProcessFlag(func() bool { return false }),
		)
		effective, err := service.IsControlPlaneV2Effective(
			context.Background(),
			organizationID,
			environmentID,
		)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(effective).To(BeFalse())
	})

	t.Run("organization disabled", func(t *testing.T) {
		repository.enrollments = []types.ControlPlaneEnrollment{{
			OrganizationID: organizationID,
			Scope:          types.ScopeRef{Kind: types.PermissionScopeOrganization, ID: organizationID},
			Enabled:        false,
			EffectiveFrom:  activeFrom,
			EffectiveUntil: &activeUntil,
			Revision:       1,
		}}
		service := NewService(
			repository,
			WithClock(func() time.Time { return now }),
			WithProcessFlag(func() bool { return true }),
		)
		effective, err := service.IsControlPlaneV2Effective(context.Background(), organizationID, environmentID)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(effective).To(BeFalse())
	})

	t.Run("both active", func(t *testing.T) {
		repository.enrollments = nil
		repositoryByScope := &enrollmentRepository{
			fakeRepository: *repository,
			byScope: map[types.PermissionScope][]types.ControlPlaneEnrollment{
				types.PermissionScopeOrganization: {{
					OrganizationID: organizationID,
					Scope:          types.ScopeRef{Kind: types.PermissionScopeOrganization, ID: organizationID},
					Enabled:        true,
					EffectiveFrom:  activeFrom,
					EffectiveUntil: &activeUntil,
					Revision:       1,
				}},
				types.PermissionScopeEnvironment: {{
					OrganizationID: organizationID,
					Scope:          types.ScopeRef{Kind: types.PermissionScopeEnvironment, ID: environmentID},
					Enabled:        true,
					EffectiveFrom:  activeFrom,
					EffectiveUntil: &activeUntil,
					Revision:       1,
				}},
			},
		}
		service := NewService(
			repositoryByScope,
			WithClock(func() time.Time { return now }),
			WithProcessFlag(func() bool { return true }),
		)
		effective, err := service.IsControlPlaneV2Effective(context.Background(), organizationID, environmentID)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(effective).To(BeTrue())
	})

	t.Run("environment disabled", func(t *testing.T) {
		repositoryByScope := &enrollmentRepository{
			byScope: map[types.PermissionScope][]types.ControlPlaneEnrollment{
				types.PermissionScopeOrganization: {{
					OrganizationID: organizationID,
					Scope:          types.ScopeRef{Kind: types.PermissionScopeOrganization, ID: organizationID},
					Enabled:        true,
					EffectiveFrom:  activeFrom,
					EffectiveUntil: &activeUntil,
					Revision:       1,
				}},
				types.PermissionScopeEnvironment: {{
					OrganizationID: organizationID,
					Scope:          types.ScopeRef{Kind: types.PermissionScopeEnvironment, ID: environmentID},
					Enabled:        false,
					EffectiveFrom:  activeFrom,
					EffectiveUntil: &activeUntil,
					Revision:       1,
				}},
			},
		}
		service := NewService(
			repositoryByScope,
			WithClock(func() time.Time { return now }),
			WithProcessFlag(func() bool { return true }),
		)
		effective, err := service.IsControlPlaneV2Effective(context.Background(), organizationID, environmentID)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(effective).To(BeFalse())
	})
}

func TestEnrollmentEffectiveAtUsesLatestActiveRevisionForRollback(t *testing.T) {
	now := time.Date(2026, time.July, 18, 3, 0, 0, 0, time.UTC)
	organizationID := uuid.New()
	scope := types.ScopeRef{Kind: types.PermissionScopeOrganization, ID: organizationID}

	g := NewWithT(t)
	g.Expect(EnrollmentEffectiveAt([]types.ControlPlaneEnrollment{
		{
			OrganizationID: organizationID,
			Scope:          scope,
			Enabled:        true,
			EffectiveFrom:  now.Add(-time.Hour),
			Revision:       1,
		},
		{
			OrganizationID: organizationID,
			Scope:          scope,
			Enabled:        false,
			EffectiveFrom:  now.Add(-time.Minute),
			Revision:       2,
		},
	}, now)).To(BeFalse())

	future := now.Add(time.Hour)
	g.Expect(EnrollmentEffectiveAt([]types.ControlPlaneEnrollment{{
		OrganizationID: organizationID,
		Scope:          scope,
		Enabled:        true,
		EffectiveFrom:  future,
		Revision:       3,
	}}, now)).To(BeFalse())
}

func TestControlPlaneEnrollmentUsesCallerDecisionAtEndToEnd(t *testing.T) {
	serviceClock := time.Date(2026, time.July, 18, 5, 0, 0, 0, time.UTC)
	decisionAt := serviceClock.Add(30 * time.Minute)
	organizationID := uuid.New()
	environmentID := uuid.New()
	repository := &enrollmentRepository{
		byScope: map[types.PermissionScope][]types.ControlPlaneEnrollment{
			types.PermissionScopeOrganization: {{
				OrganizationID: organizationID,
				Scope: types.ScopeRef{
					Kind: types.PermissionScopeOrganization,
					ID:   organizationID,
				},
				Enabled:       true,
				EffectiveFrom: serviceClock.Add(15 * time.Minute),
				Revision:      1,
			}},
			types.PermissionScopeEnvironment: {{
				OrganizationID: organizationID,
				Scope: types.ScopeRef{
					Kind: types.PermissionScopeEnvironment,
					ID:   environmentID,
				},
				Enabled:       true,
				EffectiveFrom: serviceClock.Add(15 * time.Minute),
				Revision:      1,
			}},
		},
	}
	service := NewService(
		repository,
		WithClock(func() time.Time { return serviceClock }),
		WithProcessFlag(func() bool { return true }),
	)

	effective, err := service.IsControlPlaneV2EffectiveAt(
		context.Background(),
		organizationID,
		environmentID,
		decisionAt,
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(effective).To(BeTrue())
	g.Expect(repository.enrollmentDecisionAt).To(Equal(decisionAt))
}

type enrollmentRepository struct {
	fakeRepository
	byScope              map[types.PermissionScope][]types.ControlPlaneEnrollment
	enrollmentDecisionAt time.Time
}

func (r *enrollmentRepository) ListControlPlaneEnrollments(
	_ context.Context,
	_ uuid.UUID,
	scope types.PermissionScope,
	_ uuid.UUID,
	decisionAt time.Time,
) ([]types.ControlPlaneEnrollment, error) {
	r.enrollmentDecisionAt = decisionAt
	return append([]types.ControlPlaneEnrollment{}, r.byScope[scope]...), r.err
}
