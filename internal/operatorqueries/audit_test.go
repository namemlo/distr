package operatorqueries

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestAuditCursorIsBoundToTypedCorrelationTenantAndSearchFilters(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	subjectID := uuid.New()
	filter := types.AuditFilter{
		OperatorScopeFilter: types.OperatorScopeFilter{
			OrganizationID:   organizationID,
			DecisionAt:       time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC),
			OrganizationWide: true,
			CustomerIDs:      []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
			DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
		},
		Action: "execution.completed", SubjectType: "execution", SubjectID: &subjectID,
		Search: "SUCCEEDED",
	}
	scopes, err := AuditViewScopesFromOperatorScopeFilter(filter.OperatorScopeFilter)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	cursorScope, err := auditCursorScope(filter, scopes)
	g.Expect(err).NotTo(HaveOccurred())
	encoded, err := EncodeCursor(cursorScope, CursorTuple{
		CreatedAt: filter.DecisionAt.Add(-time.Minute),
		ID:        uuid.New(),
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(encoded).NotTo(ContainSubstring(organizationID.String()))
	g.Expect(encoded).NotTo(ContainSubstring("execution.completed"))

	changed := filter
	changed.SubjectType = "deployment_plan"
	changedScope, err := auditCursorScope(changed, scopes)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = DecodeCursor(encoded, changedScope)
	g.Expect(err).To(MatchError(ContainSubstring("cursor is invalid")))
}

func TestOperatorAuditFilterRequiresTypedSubjectsAndValidTimeRange(t *testing.T) {
	t.Parallel()

	subjectID := uuid.New()
	from := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	g := NewWithT(t)
	g.Expect(validOperatorAuditFilter(types.AuditFilter{
		SubjectType: "deployment_plan", SubjectID: &subjectID, From: &from, To: &to,
	})).To(BeTrue())
	g.Expect(validOperatorAuditFilter(types.AuditFilter{SubjectID: &subjectID})).To(BeFalse())
	g.Expect(validOperatorAuditFilter(types.AuditFilter{
		SubjectType: "not a typed subject", SubjectID: &subjectID,
	})).To(BeFalse())
	g.Expect(validOperatorAuditFilter(types.AuditFilter{From: &to, To: &from})).To(BeFalse())
}
