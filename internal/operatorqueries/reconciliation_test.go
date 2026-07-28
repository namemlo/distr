package operatorqueries

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestReconciliationCursorIsBoundToTenantScopeAndEveryFilter(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	filter := types.ReconciliationFilter{
		OperatorScopeFilter: types.OperatorScopeFilter{
			OrganizationID:   organizationID,
			DecisionAt:       time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC),
			OrganizationWide: true,
			CustomerIDs:      []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
			DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
		},
		Status: "OPEN",
		Drift:  "STALE",
	}
	scopes, err := AuditViewScopesFromOperatorScopeFilter(filter.OperatorScopeFilter)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	cursorScope, err := reconciliationCursorScope(filter, scopes)
	g.Expect(err).NotTo(HaveOccurred())
	encoded, err := EncodeCursor(testCursorCodec(), cursorScope, CursorTuple{
		CreatedAt: filter.DecisionAt.Add(-time.Minute),
		ID:        uuid.New(),
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(encoded).NotTo(ContainSubstring(organizationID.String()))

	changed := filter
	changed.Drift = "UNKNOWN"
	changedScope, err := reconciliationCursorScope(changed, scopes)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = DecodeCursor(testCursorCodec(), encoded, changedScope)
	g.Expect(err).To(MatchError(ContainSubstring("cursor is invalid")))

	foreign := filter
	foreign.OrganizationID = uuid.New()
	foreignScopes := scopes
	foreignScopes.OrganizationID = foreign.OrganizationID
	foreignScope, err := reconciliationCursorScope(foreign, foreignScopes)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = DecodeCursor(testCursorCodec(), encoded, foreignScope)
	g.Expect(err).To(MatchError(ContainSubstring("cursor is invalid")))
}

func TestOperatorReconciliationFilterAcceptsKnownStatesAndRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	g.Expect(validOperatorReconciliationFilter(types.ReconciliationFilter{
		Status: "OPEN", Drift: "EXECUTOR_OBSERVER_MISMATCH",
	})).To(BeTrue())
	g.Expect(validOperatorReconciliationFilter(types.ReconciliationFilter{
		Status: "NOT_A_STATUS",
	})).To(BeFalse())
	g.Expect(validOperatorReconciliationFilter(types.ReconciliationFilter{
		Drift: "NOT_A_DRIFT_CLASS",
	})).To(BeFalse())
	zero := uuid.Nil
	g.Expect(validOperatorReconciliationFilter(types.ReconciliationFilter{
		EnvironmentID: &zero,
	})).To(BeFalse())
}
