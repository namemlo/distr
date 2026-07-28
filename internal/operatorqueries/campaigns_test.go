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

func TestCampaignPageCursorIsBoundToTenantFiltersAndAuthorizedScopes(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	organizationID := uuid.New()
	environmentID := uuid.New()
	campaignID := uuid.New()
	filter := types.CampaignFilter{
		OperatorScopeFilter: types.OperatorScopeFilter{
			OrganizationID: organizationID,
			DecisionAt:     time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC),
			CampaignIDs:    []uuid.UUID{campaignID},
			CustomerIDs:    []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
			DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{},
		},
		Status:        "RUNNING",
		EnvironmentID: &environmentID,
	}
	tuple := CursorTuple{
		CreatedAt: time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC),
		ID:        uuid.New(),
	}

	value, err := EncodeCampaignCursor(filter, tuple)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(value).NotTo(ContainSubstring(organizationID.String()))
	g.Expect(value).NotTo(ContainSubstring(campaignID.String()))

	limit, decoded, err := NormalizeCampaignPage(filter, types.PageRequest{
		Limit:  100,
		Cursor: value,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(limit).To(Equal(100))
	g.Expect(decoded).To(Equal(&tuple))

	foreignTenant := filter
	foreignTenant.OrganizationID = uuid.New()
	changedStatus := filter
	changedStatus.Status = "PAUSED"
	changedScope := filter
	changedScope.CampaignIDs = []uuid.UUID{uuid.New()}
	for name, changed := range map[string]types.CampaignFilter{
		"tenant": foreignTenant,
		"filter": changedStatus,
		"scope":  changedScope,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := NormalizeCampaignPage(changed, types.PageRequest{Cursor: value})
			NewWithT(t).Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
		})
	}
}

func TestCampaignPageCursorRejectsNonCanonicalScopeCopies(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	organizationID := uuid.New()
	first := uuid.New()
	second := uuid.New()
	base := types.CampaignFilter{OperatorScopeFilter: types.OperatorScopeFilter{
		OrganizationID: organizationID,
		DecisionAt:     time.Now().UTC(),
		EnvironmentIDs: []uuid.UUID{first},
		CampaignIDs:    []uuid.UUID{second},
		CustomerIDs:    []uuid.UUID{}, DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{},
	}}
	nonCanonical := base
	nonCanonical.EnvironmentIDs = []uuid.UUID{first, first}
	nonCanonical.CampaignIDs = []uuid.UUID{second, second}
	tuple := CursorTuple{CreatedAt: time.Now().UTC(), ID: uuid.New()}

	value, err := EncodeCampaignCursor(base, tuple)
	g.Expect(err).NotTo(HaveOccurred())
	_, _, err = NormalizeCampaignPage(nonCanonical, types.PageRequest{Cursor: value})
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}

func TestNormalizeCampaignPageEnforcesMaximumAndRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	filter := types.CampaignFilter{OperatorScopeFilter: types.OperatorScopeFilter{
		OrganizationID:   uuid.New(),
		DecisionAt:       time.Now().UTC(),
		OrganizationWide: true,
		CustomerIDs:      []uuid.UUID{}, EnvironmentIDs: []uuid.UUID{},
		DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{},
	}}

	_, _, err := NormalizeCampaignPage(filter, types.PageRequest{Limit: 101})
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
	filter.OrganizationID = uuid.Nil
	_, _, err = NormalizeCampaignPage(filter, types.PageRequest{})
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}
