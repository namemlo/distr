package operatorqueries

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestListFleetDefaultsLimitTrimsLookaheadAndReturnsStableCursor(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	decisionAt := time.Date(2026, time.July, 22, 5, 0, 0, 0, time.UTC)
	rows := make([]types.FleetRow, types.OperatorDefaultPageLimit+1)
	for index := range rows {
		rows[index] = types.FleetRow{
			ID:        uuid.New(),
			CreatedAt: decisionAt.Add(-time.Duration(index) * time.Minute),
			Unit:      "unit",
			Component: "component",
		}
	}
	total := int64(81)
	var captured db.OperatorFleetQuery

	page, err := listFleetWithRepository(
		context.Background(),
		types.FleetFilter{
			OperatorScopeFilter: AuditViewScopes{
				OrganizationID:   organizationID,
				DecisionAt:       decisionAt,
				OrganizationWide: true,
				CustomerIDs:      []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
				DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{},
				CampaignIDs: []uuid.UUID{},
			}.ToOperatorScopeFilter(),
			Component: "api",
		},
		types.PageRequest{},
		func(_ context.Context, query db.OperatorFleetQuery) (db.OperatorFleetResult, error) {
			captured = query
			return db.OperatorFleetResult{Items: rows, Total: total}, nil
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(page.Items).To(HaveLen(types.OperatorDefaultPageLimit))
	g.Expect(page.NextCursor).NotTo(BeEmpty())
	g.Expect(page.Total).NotTo(BeNil())
	g.Expect(*page.Total).To(Equal(total))
	g.Expect(captured.Limit).To(Equal(types.OperatorDefaultPageLimit + 1))
	g.Expect(captured.OrganizationID).To(Equal(organizationID))
	g.Expect(captured.DecisionAt).To(Equal(decisionAt))
	g.Expect(captured.OrganizationWide).To(BeTrue())
	g.Expect(captured.Cursor).To(BeNil())

	scope, err := fleetCursorScope(types.FleetFilter{
		OperatorScopeFilter: AuditViewScopes{
			OrganizationID:   organizationID,
			DecisionAt:       decisionAt,
			OrganizationWide: true,
			CustomerIDs:      []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
			DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{},
			CampaignIDs: []uuid.UUID{},
		}.ToOperatorScopeFilter(),
		Component: "api",
	})
	g.Expect(err).NotTo(HaveOccurred())
	tuple, err := DecodeCursor(page.NextCursor, scope)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tuple).To(Equal(&CursorTuple{
		CreatedAt: rows[types.OperatorDefaultPageLimit-1].CreatedAt,
		ID:        rows[types.OperatorDefaultPageLimit-1].ID,
	}))
}

func TestListFleetDecodesCursorAndBindsItToVisibilityAndFilters(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	environmentID := uuid.New()
	decisionAt := time.Date(2026, time.July, 22, 5, 10, 0, 0, time.UTC)
	filter := types.FleetFilter{
		OperatorScopeFilter: AuditViewScopes{
			OrganizationID: organizationID,
			DecisionAt:     decisionAt,
			EnvironmentIDs: []uuid.UUID{environmentID},
			CustomerIDs:    []uuid.UUID{}, DeploymentUnitIDs: []uuid.UUID{},
			ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
		}.ToOperatorScopeFilter(),
		EnvironmentID: &environmentID,
		ObservedState: "stale",
	}
	scope, err := fleetCursorScope(filter)
	g.Expect(err).NotTo(HaveOccurred())
	expected := CursorTuple{CreatedAt: decisionAt.Add(-time.Hour), ID: uuid.New()}
	cursor, err := EncodeCursor(scope, expected)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = listFleetWithRepository(
		context.Background(),
		filter,
		types.PageRequest{Cursor: cursor, Limit: 100},
		func(_ context.Context, query db.OperatorFleetQuery) (db.OperatorFleetResult, error) {
			g.Expect(query.Limit).To(Equal(101))
			g.Expect(query.Cursor).To(Equal(&db.OperatorFleetCursor{
				CreatedAt: expected.CreatedAt,
				ID:        expected.ID,
			}))
			g.Expect(query.EnvironmentScopeIDs).To(Equal([]uuid.UUID{environmentID}))
			return db.OperatorFleetResult{Items: []types.FleetRow{}}, nil
		},
	)
	g.Expect(err).NotTo(HaveOccurred())

	filter.ObservedState = "healthy"
	_, err = listFleetWithRepository(
		context.Background(),
		filter,
		types.PageRequest{Cursor: cursor, Limit: 100},
		func(context.Context, db.OperatorFleetQuery) (db.OperatorFleetResult, error) {
			t.Fatal("repository must not run for a cursor from a different filter")
			return db.OperatorFleetResult{}, nil
		},
	)
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}

func TestListFleetFailsClosedBeforeQueryAndEnforcesMaximumPage(t *testing.T) {
	organizationID := uuid.New()
	decisionAt := time.Now().UTC()
	repositoryCalls := 0
	repository := func(context.Context, db.OperatorFleetQuery) (db.OperatorFleetResult, error) {
		repositoryCalls++
		return db.OperatorFleetResult{}, nil
	}

	_, err := listFleetWithRepository(
		context.Background(),
		types.FleetFilter{OperatorScopeFilter: AuditViewScopes{
			OrganizationID: organizationID,
			DecisionAt:     decisionAt,
			CustomerIDs:    []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
			DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{},
			CampaignIDs: []uuid.UUID{},
		}.ToOperatorScopeFilter()},
		types.PageRequest{},
		repository,
	)
	g := NewWithT(t)
	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
	g.Expect(repositoryCalls).To(BeZero())

	_, err = listFleetWithRepository(
		context.Background(),
		types.FleetFilter{OperatorScopeFilter: AuditViewScopes{
			OrganizationID:   organizationID,
			DecisionAt:       decisionAt,
			OrganizationWide: true,
			CustomerIDs:      []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
			DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{},
			CampaignIDs: []uuid.UUID{},
		}.ToOperatorScopeFilter()},
		types.PageRequest{Limit: 101},
		repository,
	)
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
	g.Expect(repositoryCalls).To(BeZero())
}
