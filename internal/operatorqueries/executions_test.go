package operatorqueries

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestNormalizeExecutionQueryDefaultsPageAndBindsCursorToTenantScopeAndFilter(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	scopes := AuditViewScopes{
		OrganizationID: organizationID,
		DecisionAt:     now,
		EnvironmentIDs: []uuid.UUID{uuid.New()},
		CustomerIDs:    []uuid.UUID{}, DeploymentUnitIDs: []uuid.UUID{},
		ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
	}
	filter := types.ExecutionFilter{
		OperatorScopeFilter: types.OperatorScopeFilter{OrganizationID: organizationID},
		Status:              "RUNNING",
	}

	normalized, err := NormalizeExecutionQuery(filter, scopes, types.PageRequest{}, testCursorCodec())
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(normalized.Limit).To(Equal(types.OperatorDefaultPageLimit))
	g.Expect(normalized.Cursor).To(BeNil())
	g.Expect(normalized.CursorScope.OrganizationID).To(Equal(organizationID))
	g.Expect(normalized.CursorScope.Collection).To(Equal(types.OperatorCollectionExecutions))
	g.Expect(normalized.CursorScope.DecisionAt).To(Equal(now))
	g.Expect(normalized.CursorScope.ScopeChecksum).To(Equal(scopes.Checksum()))
	g.Expect(normalized.CursorScope.FilterChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))

	encoded, err := EncodeCursor(testCursorCodec(), normalized.CursorScope, CursorTuple{
		CreatedAt: now.Add(-time.Minute),
		ID:        uuid.New(),
	})
	g.Expect(err).NotTo(HaveOccurred())
	normalized, err = NormalizeExecutionQuery(
		filter,
		scopes,
		types.PageRequest{Cursor: encoded, Limit: 100},
		testCursorCodec(),
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(normalized.Cursor).NotTo(BeNil())
	g.Expect(normalized.Limit).To(Equal(types.OperatorMaximumPageLimit))

	foreign := scopes
	foreign.OrganizationID = uuid.New()
	_, err = NormalizeExecutionQuery(filter, foreign, types.PageRequest{Cursor: encoded}, testCursorCodec())
	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
}

func TestNormalizeExecutionQueryRejectsInvalidStatusTimeRangeAndEmptyScope(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	now := time.Now().UTC()
	scopes := AuditViewScopes{
		OrganizationID: organizationID, DecisionAt: now, OrganizationWide: true,
		CustomerIDs: []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{},
		CampaignIDs: []uuid.UUID{},
	}
	base := types.ExecutionFilter{
		OperatorScopeFilter: types.OperatorScopeFilter{OrganizationID: organizationID},
	}

	for name, mutate := range map[string]func(*types.ExecutionFilter){
		"invalid status": func(filter *types.ExecutionFilter) { filter.Status = "MAYBE" },
		"equal bound": func(filter *types.ExecutionFilter) {
			filter.From, filter.To = new(now), new(now)
		},
		"reversed bound": func(filter *types.ExecutionFilter) {
			from, to := now, now.Add(-time.Second)
			filter.From, filter.To = &from, &to
		},
	} {
		t.Run(name, func(t *testing.T) {
			filter := base
			mutate(&filter)
			_, err := NormalizeExecutionQuery(filter, scopes, types.PageRequest{}, testCursorCodec())
			NewWithT(t).Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
		})
	}

	empty := scopes
	empty.OrganizationWide = false
	_, err := NormalizeExecutionQuery(base, empty, types.PageRequest{}, testCursorCodec())
	NewWithT(t).Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
}

func TestCompleteExecutionPageKeepsRetriesDistinctAndBuildsStableCursor(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	executionID := uuid.New()
	rows := []types.OperatorExecutionRow{
		{ID: uuid.New(), CreatedAt: createdAt, AttemptNumber: 3, StepKey: "deploy"},
		{ID: uuid.New(), CreatedAt: createdAt, AttemptNumber: 2, StepKey: "deploy"},
		{ID: executionID, CreatedAt: createdAt.Add(-time.Second), AttemptNumber: 1, StepKey: "deploy"},
	}
	scope := CursorScope{
		OrganizationID: uuid.New(), Collection: types.OperatorCollectionExecutions,
		DecisionAt: createdAt, ScopeChecksum: "sha256:" + strings.Repeat("0", 64),
		FilterChecksum: "sha256:" + strings.Repeat("1", 64),
	}

	page, err := CompleteExecutionPage(rows, 2, scope, testCursorCodec())
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(page.Items).To(HaveLen(2))
	g.Expect(page.Items[0].AttemptNumber).To(Equal(3))
	g.Expect(page.Items[1].AttemptNumber).To(Equal(2))
	g.Expect(page.NextCursor).NotTo(BeEmpty())

	cursor, err := DecodeCursor(testCursorCodec(), page.NextCursor, scope)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cursor.CreatedAt).To(Equal(rows[1].CreatedAt))
	g.Expect(cursor.ID).To(Equal(rows[1].ID))
}

func TestExecutionObservationLabelPreservesUncertainPartialUnknownAndStaleStates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	tests := map[string]struct {
		outcome    string
		freshUntil *time.Time
		want       string
	}{
		"missing is unknown": {want: "UNKNOWN"},
		"partial is visible": {outcome: "PARTIAL", freshUntil: new(now.Add(time.Minute)), want: "PARTIAL"},
		"unknown is visible": {outcome: "UNKNOWN", freshUntil: new(now.Add(time.Minute)), want: "UNKNOWN"},
		"complete but stale": {outcome: "COMPLETE", freshUntil: new(now.Add(-time.Nanosecond)), want: "STALE"},
		"fresh complete":     {outcome: "COMPLETE", freshUntil: new(now), want: "COMPLETE"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			NewWithT(t).Expect(ExecutionObservationLabel(
				test.outcome,
				test.freshUntil,
				now,
			)).To(Equal(test.want))
		})
	}
}

type executionRepositoryStub struct {
	listCalls int
	getCalls  int
	filter    types.ExecutionFilter
	after     *time.Time
	afterID   *uuid.UUID
	limit     int
	rows      []types.OperatorExecutionRow
	detail    *types.OperatorExecutionDetail
	err       error
}

func (stub *executionRepositoryStub) ListOperatorExecutions(
	_ context.Context,
	filter types.ExecutionFilter,
	after *time.Time,
	afterID *uuid.UUID,
	limit int,
) ([]types.OperatorExecutionRow, error) {
	stub.listCalls++
	stub.filter, stub.after, stub.afterID, stub.limit = filter, after, afterID, limit
	return stub.rows, stub.err
}

func (stub *executionRepositoryStub) GetOperatorExecution(
	_ context.Context,
	scope types.OperatorScopeFilter,
	id uuid.UUID,
) (*types.OperatorExecutionDetail, error) {
	stub.getCalls++
	stub.filter.OperatorScopeFilter = scope
	stub.filter.DeploymentPlanID = &id
	return stub.detail, stub.err
}

func TestListOperatorExecutionsQueriesOnceAndCompletesPage(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	now := time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)
	scopes := AuditViewScopes{
		OrganizationID: organizationID, DecisionAt: now, OrganizationWide: true,
		CustomerIDs: []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
	}
	rows := []types.OperatorExecutionRow{
		{ID: uuid.New(), CreatedAt: now, AttemptNumber: 2},
		{ID: uuid.New(), CreatedAt: now.Add(-time.Second), AttemptNumber: 1},
	}
	repository := &executionRepositoryStub{rows: rows}

	page, err := ListOperatorExecutions(
		context.Background(), repository,
		types.ExecutionFilter{OperatorScopeFilter: types.OperatorScopeFilter{OrganizationID: organizationID}},
		scopes, types.PageRequest{Limit: 1}, testCursorCodec(),
	)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(repository.listCalls).To(Equal(1))
	g.Expect(repository.limit).To(Equal(2))
	g.Expect(repository.filter.OrganizationWide).To(BeTrue())
	g.Expect(page.Items).To(Equal(rows[:1]))
	g.Expect(page.NextCursor).NotTo(BeEmpty())
}

func TestGetOperatorExecutionFailsClosedBeforeRepositoryForForeignScope(t *testing.T) {
	t.Parallel()

	repository := &executionRepositoryStub{}
	scopes := AuditViewScopes{
		OrganizationID: uuid.New(), DecisionAt: time.Now().UTC(), OrganizationWide: true,
		CustomerIDs: []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
	}
	_, err := GetOperatorExecution(
		context.Background(), repository, uuid.New(), scopes, uuid.New(),
	)

	g := NewWithT(t)
	g.Expect(errors.Is(err, apierrors.ErrForbidden)).To(BeTrue())
	g.Expect(repository.getCalls).To(Equal(0))
}
