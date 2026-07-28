package operatorqueries

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestPlanCursorScopeBindsTenantDecisionFiltersAndCanonicalScopes(t *testing.T) {
	t.Parallel()

	organizationID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	environmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	unitID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	decisionAt := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	first := types.OperatorPlanFilter{
		OperatorScopeFilter: types.OperatorScopeFilter{
			OrganizationID: organizationID, DecisionAt: decisionAt,
			CustomerIDs:       []uuid.UUID{},
			EnvironmentIDs:    []uuid.UUID{environmentID},
			DeploymentUnitIDs: []uuid.UUID{unitID},
			ComponentIDs:      []uuid.UUID{},
			CampaignIDs:       []uuid.UUID{},
		},
		Status: "READY", EnvironmentID: &environmentID,
	}
	firstScope, err := planCursorScope(first)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	NewWithT(t).Expect(firstScope.OrganizationID).To(Equal(organizationID))
	NewWithT(t).Expect(firstScope.Collection).To(Equal(types.OperatorCollectionPlans))
	NewWithT(t).Expect(firstScope.DecisionAt).To(Equal(decisionAt))

	changed := first
	changed.Status = "BLOCKED"
	changedScope, err := planCursorScope(changed)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	NewWithT(t).Expect(changedScope.FilterChecksum).NotTo(Equal(firstScope.FilterChecksum))

	invalid := first
	invalid.EnvironmentIDs = []uuid.UUID{environmentID, environmentID}
	_, err = planCursorScope(invalid)
	NewWithT(t).Expect(err).To(MatchError(ContainSubstring("operator scope filter is invalid")))
}

func TestCompleteOperatorPlanPageUsesLastReturnedImmutableTuple(t *testing.T) {
	t.Parallel()

	filter := types.OperatorPlanFilter{OperatorScopeFilter: types.OperatorScopeFilter{
		OrganizationID:   uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		DecisionAt:       time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC),
		OrganizationWide: true,
		CustomerIDs:      []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
	}}
	scope, err := planCursorScope(filter)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	createdAt := time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC)
	rows := []types.OperatorPlanRow{
		{ID: uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"), CreatedAt: createdAt},
		{ID: uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"), CreatedAt: createdAt},
		{ID: uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), CreatedAt: createdAt},
	}
	page, err := completeOperatorPlanPage(rows, 2, scope, testCursorCodec())
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	NewWithT(t).Expect(page.Items).To(Equal(rows[:2]))
	NewWithT(t).Expect(page.NextCursor).NotTo(BeEmpty())

	cursor, err := DecodeCursor(testCursorCodec(), page.NextCursor, scope)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	NewWithT(t).Expect(cursor).To(Equal(&CursorTuple{
		CreatedAt: rows[1].CreatedAt,
		ID:        rows[1].ID,
	}))
}

func TestPlanCursorScopeRejectsEmptyVisibility(t *testing.T) {
	t.Parallel()

	_, err := planCursorScope(types.OperatorPlanFilter{OperatorScopeFilter: types.OperatorScopeFilter{
		OrganizationID: uuid.New(),
		DecisionAt:     time.Now().UTC(),
		CustomerIDs:    []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
	}})
	NewWithT(t).Expect(err).To(MatchError(ContainSubstring("operator scope is empty")))
}
