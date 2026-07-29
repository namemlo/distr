package operatorqueries

import (
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"

	"github.com/distr-sh/distr/internal/types"
)

func TestNormalizeReleaseQueryBindsCursorToScopeAndFilters(t *testing.T) {
	t.Parallel()

	organizationID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	decisionAt := time.Date(2026, time.July, 22, 9, 0, 0, 0, time.UTC)
	customerID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	filter := types.ReleaseFilter{
		OperatorScopeFilter:    organizationWideReleaseScope(organizationID, decisionAt),
		CustomerOrganizationID: &customerID,
		Kind:                   string(types.ReleaseBundleKindComponent),
		Status:                 string(types.ReleaseBundleStatusPublished),
		Search:                 " 100%_ready ",
	}

	query, err := NormalizeReleaseQuery(filter, types.PageRequest{}, testCursorCodec())
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(query.Limit).To(Equal(types.OperatorDefaultPageLimit))
	g.Expect(query.SearchPattern).To(Equal(`%100\%\_ready%`))
	g.Expect(query.CustomerOrganizationID).To(Equal(&customerID))
	g.Expect(query.Scopes.OrganizationWide).To(BeTrue())
	g.Expect(query.Cursor).To(BeNil())

	encoded, err := EncodeCursor(testCursorCodec(), query.CursorScope, CursorTuple{
		CreatedAt: decisionAt.Add(-time.Minute),
		ID:        uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"),
	})
	g.Expect(err).NotTo(HaveOccurred())
	query, err = NormalizeReleaseQuery(filter, types.PageRequest{
		Cursor: encoded,
		Limit:  types.OperatorMaximumPageLimit,
	}, testCursorCodec())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(query.Cursor).NotTo(BeNil())

	filter.Status = string(types.ReleaseBundleStatusBlocked)
	_, err = NormalizeReleaseQuery(filter, types.PageRequest{Cursor: encoded, Limit: 50}, testCursorCodec())
	g.Expect(err).To(MatchError(ContainSubstring("cursor is invalid")))

	filter.Status = string(types.ReleaseBundleStatusPublished)
	otherCustomer := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	filter.CustomerOrganizationID = &otherCustomer
	_, err = NormalizeReleaseQuery(filter, types.PageRequest{Cursor: encoded, Limit: 50}, testCursorCodec())
	g.Expect(err).To(MatchError(ContainSubstring("cursor is invalid")))
}

func TestNormalizeReleaseQueryFailsClosed(t *testing.T) {
	t.Parallel()

	organizationID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	decisionAt := time.Date(2026, time.July, 22, 9, 0, 0, 0, time.UTC)
	base := types.ReleaseFilter{OperatorScopeFilter: organizationWideReleaseScope(organizationID, decisionAt)}
	tests := []struct {
		name   string
		mutate func(*types.ReleaseFilter)
		page   types.PageRequest
		want   string
	}{
		{name: "missing organization", mutate: func(filter *types.ReleaseFilter) { filter.OrganizationID = uuid.Nil }, want: "operator scope filter is invalid"},
		{name: "missing decision instant", mutate: func(filter *types.ReleaseFilter) { filter.DecisionAt = time.Time{} }, want: "operator scope filter is invalid"},
		{name: "empty scopes", mutate: func(filter *types.ReleaseFilter) { filter.OrganizationWide = false }, want: "forbidden"},
		{name: "mixed broad and narrow scopes", mutate: func(filter *types.ReleaseFilter) { filter.ComponentIDs = []uuid.UUID{uuid.New()} }, want: "operator scope filter is invalid"},
		{name: "nil narrow scope", mutate: func(filter *types.ReleaseFilter) {
			filter.OrganizationWide = false
			filter.ComponentIDs = []uuid.UUID{uuid.Nil}
		}, want: "operator scope filter is invalid"},
		{name: "invalid application", mutate: func(filter *types.ReleaseFilter) { filter.ApplicationID = new(uuid.UUID) }, want: "applicationId is invalid"},
		{name: "invalid customer", mutate: func(filter *types.ReleaseFilter) { filter.CustomerOrganizationID = new(uuid.UUID) }, want: "customerOrganizationId is invalid"},
		{name: "invalid kind", mutate: func(filter *types.ReleaseFilter) { filter.Kind = "COMPONENT" }, want: "kind is invalid"},
		{name: "invalid status", mutate: func(filter *types.ReleaseFilter) { filter.Status = "published" }, want: "status is invalid"},
		{name: "oversized page", mutate: func(*types.ReleaseFilter) {}, page: types.PageRequest{Limit: 101}, want: "limit must be between 1 and 100"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filter := base
			test.mutate(&filter)
			_, err := NormalizeReleaseQuery(filter, test.page, testCursorCodec())
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(test.want)))
		})
	}
}

func TestNormalizeReleaseQueryRejectsNonCanonicalNarrowScopeOrder(t *testing.T) {
	t.Parallel()

	first := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	second := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	query, err := NormalizeReleaseQuery(types.ReleaseFilter{
		OperatorScopeFilter: types.OperatorScopeFilter{
			OrganizationID:    uuid.New(),
			DecisionAt:        time.Now().UTC(),
			CustomerIDs:       []uuid.UUID{},
			EnvironmentIDs:    []uuid.UUID{},
			DeploymentUnitIDs: []uuid.UUID{},
			ComponentIDs:      []uuid.UUID{second, first, second},
			CampaignIDs:       []uuid.UUID{},
		},
	}, types.PageRequest{Limit: 50}, testCursorCodec())

	g := NewWithT(t)
	g.Expect(err).To(MatchError(ContainSubstring("operator scope filter is invalid")))
	g.Expect(query.Scopes.ComponentIDs).To(BeEmpty())
}

func organizationWideReleaseScope(organizationID uuid.UUID, decisionAt time.Time) types.OperatorScopeFilter {
	return AuditViewScopes{
		OrganizationID:    organizationID,
		DecisionAt:        decisionAt,
		OrganizationWide:  true,
		CustomerIDs:       []uuid.UUID{},
		EnvironmentIDs:    []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{},
		ComponentIDs:      []uuid.UUID{},
		CampaignIDs:       []uuid.UUID{},
	}.ToOperatorScopeFilter()
}

func TestCompareOperatorReleaseDetailsReportsAddedRemovedAndModifiedFacts(t *testing.T) {
	t.Parallel()

	left := types.OperatorReleaseDetail{
		Release: types.OperatorReleaseRow{ID: uuid.New(), Checksum: "sha256:left"},
		Artifacts: []types.OperatorReleaseArtifact{
			{Name: "api", ManifestDigest: "sha256:api-left"},
			{Name: "removed", ManifestDigest: "sha256:removed"},
		},
	}
	right := types.OperatorReleaseDetail{
		Release: types.OperatorReleaseRow{ID: uuid.New(), Checksum: "sha256:right"},
		Artifacts: []types.OperatorReleaseArtifact{
			{Name: "api", ManifestDigest: "sha256:api-right"},
			{Name: "added", ManifestDigest: "sha256:added"},
		},
	}

	comparison := compareOperatorReleaseDetails(left, right)

	g := NewWithT(t)
	g.Expect(comparison.Left).To(Equal(left.Release))
	g.Expect(comparison.Right).To(Equal(right.Release))
	g.Expect(comparison.Changes).To(Equal([]types.OperatorReleaseCompareFact{
		{Component: "added", Change: "added", RightDigest: "sha256:added"},
		{Component: "api", Change: "modified", LeftDigest: "sha256:api-left", RightDigest: "sha256:api-right"},
		{Component: "removed", Change: "removed", LeftDigest: "sha256:removed"},
	}))
}
