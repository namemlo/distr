package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db/queryable"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestCalendarCampaignScopeRequiresTenantOwnedDraft(t *testing.T) {
	organizationID := uuid.New()
	campaignID := uuid.New()
	database := &campaignScopeQueryable{exists: true}
	ctx := internalctx.WithDb(context.Background(), database)

	err := ensureCalendarScopeBelongsToOrganization(
		ctx,
		organizationID,
		types.CalendarScopeCampaign,
		campaignID,
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	assertTenantCampaignLookup(g, database, organizationID, campaignID)
}

func TestPolicyCampaignScopeRequiresTenantOwnedDraft(t *testing.T) {
	organizationID := uuid.New()
	campaignID := uuid.New()
	database := &campaignScopeQueryable{exists: true}
	ctx := internalctx.WithDb(context.Background(), database)

	err := ensureDeploymentPolicyBindingScope(
		ctx,
		organizationID,
		types.DeploymentPolicyBindingScopeCampaign,
		campaignID,
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	assertTenantCampaignLookup(g, database, organizationID, campaignID)
}

func TestAuthorizationCampaignResourceRequiresTenantOwnedDraft(t *testing.T) {
	organizationID := uuid.New()
	campaignID := uuid.New()
	database := &campaignScopeQueryable{exists: true}
	ctx := internalctx.WithDb(context.Background(), database)

	scopes, err := ResolveAuthorizationResourceScopes(ctx, types.ResourceRef{
		Kind:           types.PermissionScopeCampaign,
		ID:             campaignID,
		OrganizationID: organizationID,
	})

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(scopes).To(Equal([]types.ScopeRef{
		{Kind: types.PermissionScopeOrganization, ID: organizationID},
		{Kind: types.PermissionScopeCampaign, ID: campaignID},
	}))
	assertTenantCampaignLookup(g, database, organizationID, campaignID)
}

func TestAuthorizationCampaignBindingRejectsUnknownOrForeignDraft(t *testing.T) {
	database := &campaignScopeQueryable{exists: false}
	ctx := internalctx.WithDb(context.Background(), database)
	organizationID := uuid.New()
	campaignID := uuid.New()

	err := ensureAuthorizationScopeExists(ctx, organizationID, types.ScopeRef{
		Kind: types.PermissionScopeCampaign,
		ID:   campaignID,
	})

	g := NewWithT(t)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
	assertTenantCampaignLookup(g, database, organizationID, campaignID)
}

func assertTenantCampaignLookup(
	g Gomega,
	database *campaignScopeQueryable,
	organizationID uuid.UUID,
	campaignID uuid.UUID,
) {
	g.Expect(database.query).To(ContainSubstring("FROM DeploymentCampaignDraft"))
	g.Expect(strings.ToLower(database.query)).To(ContainSubstring("organization_id"))
	g.Expect(database.args).To(HaveLen(1))
	arguments := database.args[0].(pgx.NamedArgs)
	g.Expect(arguments["organizationID"]).To(Equal(organizationID))
	identity := arguments["scopeID"]
	if identity == nil {
		identity = arguments["id"]
	}
	g.Expect(identity).To(Equal(campaignID))
}

type campaignScopeQueryable struct {
	queryable.Queryable
	exists bool
	query  string
	args   []any
}

func (database *campaignScopeQueryable) QueryRow(
	_ context.Context,
	query string,
	args ...any,
) pgx.Row {
	database.query = query
	database.args = args
	return campaignScopeRow{exists: database.exists}
}

type campaignScopeRow struct {
	exists bool
}

func (row campaignScopeRow) Scan(destinations ...any) error {
	*destinations[0].(*bool) = row.exists
	return nil
}
