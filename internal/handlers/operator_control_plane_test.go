package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/operatorqueries"
	"github.com/distr-sh/distr/internal/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestOperatorControlPlaneFeatureGateHidesDisabledRoutes(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := operatorControlPlaneFeatureGateWithFlags(nil)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		},
	))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/control-plane/fleet", nil),
	)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
	g.Expect(called).To(BeFalse())
}

func TestOperatorControlPlaneFeatureGateAllowsEnabledRoutes(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := operatorControlPlaneFeatureGateWithFlags([]featureflags.Key{
		featureflags.KeyOperatorControlPlaneV2,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/v1/control-plane/fleet", nil),
	)

	g.Expect(recorder.Code).To(Equal(http.StatusNoContent))
	g.Expect(called).To(BeTrue())
}

func TestResolveOperatorReadScopesUsesEffectiveAuditViewGrantsOnce(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	principalID := uuid.New()
	environmentID := uuid.New()
	decisionAt := time.Date(2026, time.July, 22, 4, 5, 6, 0, time.UTC)
	role := types.UserRoleReadOnly
	listCalls := 0
	legacyCalls := 0

	scopes, err := resolveOperatorReadScopes(context.Background(), operatorReadPrincipal{
		OrganizationID: organizationID,
		PrincipalID:    principalID,
		CredentialRole: &role,
	}, operatorReadScopeDependencies{
		clock: func() time.Time { return decisionAt },
		listAccessGrants: func(
			_ context.Context,
			gotOrganizationID uuid.UUID,
			gotPrincipalID uuid.UUID,
			gotDecisionAt time.Time,
		) ([]types.AccessGrant, error) {
			listCalls++
			g.Expect(gotOrganizationID).To(Equal(organizationID))
			g.Expect(gotPrincipalID).To(Equal(principalID))
			g.Expect(gotDecisionAt).To(Equal(decisionAt))
			return []types.AccessGrant{
				{
					BindingID:     uuid.New(),
					Scope:         types.ScopeRef{Kind: types.PermissionScopeEnvironment, ID: environmentID},
					Actions:       []types.Action{types.ActionAuditView},
					EffectiveFrom: decisionAt.Add(-time.Minute),
				},
				{
					BindingID:     uuid.New(),
					Scope:         types.ScopeRef{Kind: types.PermissionScopeEnvironment, ID: environmentID},
					Actions:       []types.Action{types.ActionAuditView},
					EffectiveFrom: decisionAt.Add(-time.Minute),
				},
				{
					BindingID:     uuid.New(),
					Scope:         types.ScopeRef{Kind: types.PermissionScopeOrganization, ID: organizationID},
					Actions:       []types.Action{types.ActionPlanExecute},
					EffectiveFrom: decisionAt.Add(-time.Minute),
				},
			}, nil
		},
		getLegacyRole: func(context.Context, uuid.UUID, uuid.UUID) (*types.UserRole, error) {
			legacyCalls++
			return nil, nil
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(scopes).To(Equal(operatorqueries.AuditViewScopes{
		OrganizationID:    organizationID,
		DecisionAt:        decisionAt,
		CustomerIDs:       []uuid.UUID{},
		EnvironmentIDs:    []uuid.UUID{environmentID},
		DeploymentUnitIDs: []uuid.UUID{},
		ComponentIDs:      []uuid.UUID{},
		CampaignIDs:       []uuid.UUID{},
	}))
	g.Expect(listCalls).To(Equal(1))
	g.Expect(legacyCalls).To(Equal(1))
}

func TestResolveOperatorReadScopesUsesOrganizationLegacyFallback(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	principalID := uuid.New()
	role := types.UserRoleReadWrite

	scopes, err := resolveOperatorReadScopes(context.Background(), operatorReadPrincipal{
		OrganizationID: organizationID,
		PrincipalID:    principalID,
		CredentialRole: &role,
	}, operatorReadScopeDependencies{
		clock:            time.Now,
		listAccessGrants: func(context.Context, uuid.UUID, uuid.UUID, time.Time) ([]types.AccessGrant, error) { return nil, nil },
		getLegacyRole: func(context.Context, uuid.UUID, uuid.UUID) (*types.UserRole, error) {
			return &role, nil
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(scopes.OrganizationID).To(Equal(organizationID))
	g.Expect(scopes.OrganizationWide).To(BeTrue())
	g.Expect(scopes.DecisionAt).NotTo(BeZero())
}

func TestResolveOperatorReadScopesAllowsOrganizationContextSuperAdminWithoutDatabaseReads(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	listCalls := 0
	legacyCalls := 0

	scopes, err := resolveOperatorReadScopes(context.Background(), operatorReadPrincipal{
		OrganizationID: organizationID,
		PrincipalID:    uuid.New(),
		SuperAdmin:     true,
	}, operatorReadScopeDependencies{
		clock: time.Now,
		listAccessGrants: func(context.Context, uuid.UUID, uuid.UUID, time.Time) ([]types.AccessGrant, error) {
			listCalls++
			return nil, nil
		},
		getLegacyRole: func(context.Context, uuid.UUID, uuid.UUID) (*types.UserRole, error) {
			legacyCalls++
			return nil, nil
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(scopes.OrganizationID).To(Equal(organizationID))
	g.Expect(scopes.OrganizationWide).To(BeTrue())
	g.Expect(scopes.DecisionAt).NotTo(BeZero())
	g.Expect(listCalls).To(BeZero())
	g.Expect(legacyCalls).To(BeZero())
}

func TestResolveOperatorReadScopesRejectsCredentialWithoutAuditView(t *testing.T) {
	g := NewWithT(t)
	unsupported := types.UserRole("unsupported")

	_, err := resolveOperatorReadScopes(context.Background(), operatorReadPrincipal{
		OrganizationID: uuid.New(),
		PrincipalID:    uuid.New(),
		CredentialRole: &unsupported,
	}, operatorReadScopeDependencies{
		clock:            time.Now,
		listAccessGrants: func(context.Context, uuid.UUID, uuid.UUID, time.Time) ([]types.AccessGrant, error) { return nil, nil },
		getLegacyRole:    func(context.Context, uuid.UUID, uuid.UUID) (*types.UserRole, error) { return nil, nil },
	})

	g.Expect(err).To(MatchError(errOperatorReadForbidden))
}

func TestOperatorScopeFilterCarriesDecisionInstantAndSQLScopes(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	decisionAt := time.Date(2026, time.July, 22, 6, 7, 8, 0, time.UTC)
	customerID := uuid.New()
	environmentID := uuid.New()
	unitID := uuid.New()
	componentID := uuid.New()
	campaignID := uuid.New()

	filter := operatorScopeFilter(operatorqueries.AuditViewScopes{
		OrganizationID:    organizationID,
		DecisionAt:        decisionAt,
		CustomerIDs:       []uuid.UUID{customerID},
		EnvironmentIDs:    []uuid.UUID{environmentID},
		DeploymentUnitIDs: []uuid.UUID{unitID},
		ComponentIDs:      []uuid.UUID{componentID},
		CampaignIDs:       []uuid.UUID{campaignID},
	})

	g.Expect(filter.OrganizationID).To(Equal(organizationID))
	g.Expect(filter.DecisionAt).To(Equal(decisionAt))
	g.Expect(filter.OrganizationWide).To(BeFalse())
	g.Expect(filter.CustomerIDs).To(Equal([]uuid.UUID{customerID}))
	g.Expect(filter.EnvironmentIDs).To(Equal([]uuid.UUID{environmentID}))
	g.Expect(filter.DeploymentUnitIDs).To(Equal([]uuid.UUID{unitID}))
	g.Expect(filter.ComponentIDs).To(Equal([]uuid.UUID{componentID}))
	g.Expect(filter.CampaignIDs).To(Equal([]uuid.UUID{campaignID}))
}

func TestOperatorScopeFilterRepresentsOrganizationWideAccessExplicitly(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()

	filter := operatorScopeFilter(operatorqueries.AuditViewScopes{
		OrganizationID:   organizationID,
		DecisionAt:       time.Now().UTC(),
		OrganizationWide: true,
	})

	g.Expect(filter.OrganizationWide).To(BeTrue())
	g.Expect(filter.CustomerIDs).To(BeEmpty())
	g.Expect(filter.EnvironmentIDs).To(BeEmpty())
	g.Expect(filter.DeploymentUnitIDs).To(BeEmpty())
	g.Expect(filter.ComponentIDs).To(BeEmpty())
	g.Expect(filter.CampaignIDs).To(BeEmpty())
}

func TestOperatorFleetListRequestFromHTTPParsesFiltersAndDefaultsPage(t *testing.T) {
	g := NewWithT(t)
	customerID := uuid.New()
	environmentID := uuid.New()
	targetID := uuid.New()
	unitID := uuid.New()
	request := httptest.NewRequest(http.MethodGet,
		"/fleet?customerOrganizationId="+customerID.String()+
			"&environmentId="+environmentID.String()+
			"&deploymentTargetId="+targetID.String()+
			"&deploymentUnitId="+unitID.String()+
			"&component=api&observedState=stale&drift=drifted"+
			"&enrollment=enabled&search=client",
		nil,
	)

	parsed, err := operatorFleetListRequestFromHTTP(request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(parsed.CustomerOrganizationID).To(Equal(&customerID))
	g.Expect(parsed.EnvironmentID).To(Equal(&environmentID))
	g.Expect(parsed.DeploymentTargetID).To(Equal(&targetID))
	g.Expect(parsed.DeploymentUnitID).To(Equal(&unitID))
	g.Expect(parsed.Component).To(Equal("api"))
	g.Expect(parsed.ObservedState).To(Equal("stale"))
	g.Expect(parsed.Drift).To(Equal("drifted"))
	g.Expect(parsed.Enrollment).To(Equal("enabled"))
	g.Expect(parsed.Search).To(Equal("client"))
	g.Expect(parsed.ToPageRequest()).To(Equal(types.PageRequest{Limit: 50}))
}

func TestOperatorListRequestFromHTTPRejectsExplicitZeroAndInvalidValues(t *testing.T) {
	for name, target := range map[string]string{
		"zero limit":   "/fleet?limit=0",
		"invalid uuid": "/fleet?environmentId=not-a-uuid",
		"invalid time": "/executions?from=tomorrow",
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, target, nil)
			var err error
			if strings.HasPrefix(target, "/executions") {
				_, err = operatorExecutionListRequestFromHTTP(request)
			} else {
				_, err = operatorFleetListRequestFromHTTP(request)
			}
			NewWithT(t).Expect(err).To(HaveOccurred())
		})
	}
}

func TestOperatorCollectionRequestParsersApplyEndpointFilters(t *testing.T) {
	g := NewWithT(t)
	id := uuid.New()

	release, err := operatorReleaseListRequestFromHTTP(httptest.NewRequest(
		http.MethodGet,
		"/releases?limit=100&applicationId="+id.String()+"&kind=component&status=published&search=v1",
		nil,
	))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(release.ApplicationID).To(Equal(&id))
	g.Expect(release.Kind).To(Equal("component"))
	g.Expect(release.ToPageRequest().Limit).To(Equal(100))

	plan, err := operatorPlanListRequestFromHTTP(httptest.NewRequest(
		http.MethodGet,
		"/plans?environmentId="+id.String()+"&deploymentUnitId="+id.String()+"&productReleaseId="+id.String()+"&status=ready",
		nil,
	))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.EnvironmentID).To(Equal(&id))
	g.Expect(plan.DeploymentUnitID).To(Equal(&id))
	g.Expect(plan.ProductReleaseID).To(Equal(&id))

	campaign, err := operatorCampaignListRequestFromHTTP(httptest.NewRequest(
		http.MethodGet,
		"/campaigns?environmentId="+id.String()+"&deploymentPlanId="+id.String()+"&status=running",
		nil,
	))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(campaign.EnvironmentID).To(Equal(&id))
	g.Expect(campaign.DeploymentPlanID).To(Equal(&id))

	reconciliation, err := operatorReconciliationListRequestFromHTTP(httptest.NewRequest(
		http.MethodGet,
		"/reconciliation?environmentId="+id.String()+"&deploymentTargetId="+id.String()+"&status=open&drift=stale",
		nil,
	))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reconciliation.EnvironmentID).To(Equal(&id))
	g.Expect(reconciliation.DeploymentTargetID).To(Equal(&id))

	audit, err := operatorAuditListRequestFromHTTP(httptest.NewRequest(
		http.MethodGet,
		"/audit?subjectId="+id.String()+"&actorUserAccountId="+id.String()+"&subjectType=execution&action=view&search=success",
		nil,
	))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(audit.SubjectID).To(Equal(&id))
	g.Expect(audit.ActorUserAccountID).To(Equal(&id))
}

func TestOperatorFleetHandlerResolvesScopesOnceBeforeQueryPagination(t *testing.T) {
	g := NewWithT(t)
	userAuth := testChannelAuth()
	organizationID := *userAuth.CurrentOrgID()
	decisionAt := time.Date(2026, time.July, 22, 8, 9, 10, 0, time.UTC)
	environmentID := uuid.New()
	scopeCalls := 0
	queryCalls := 0
	row := types.FleetRow{ID: uuid.New(), Component: "api"}
	total := int64(1)
	dependencies := operatorControlPlaneDependencies{
		resolveScopes: func(
			_ context.Context,
			principal operatorReadPrincipal,
		) (operatorqueries.AuditViewScopes, error) {
			scopeCalls++
			g.Expect(principal.OrganizationID).To(Equal(organizationID))
			g.Expect(principal.PrincipalID).To(Equal(userAuth.CurrentUserID()))
			return operatorqueries.AuditViewScopes{
				OrganizationID: organizationID,
				DecisionAt:     decisionAt,
				EnvironmentIDs: []uuid.UUID{environmentID},
			}, nil
		},
		listFleet: func(
			_ context.Context,
			filter types.FleetFilter,
			page types.PageRequest,
		) (types.OperatorPage[types.FleetRow], error) {
			queryCalls++
			g.Expect(filter.OrganizationID).To(Equal(organizationID))
			g.Expect(filter.DecisionAt).To(Equal(decisionAt))
			g.Expect(filter.EnvironmentIDs).To(Equal([]uuid.UUID{environmentID}))
			g.Expect(page).To(Equal(types.PageRequest{Limit: 50}))
			return types.OperatorPage[types.FleetRow]{
				Items: []types.FleetRow{row}, Total: &total,
			}, nil
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/fleet", nil)
	request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
	recorder := httptest.NewRecorder()

	operatorFleetHandler(dependencies).ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	g.Expect(scopeCalls).To(Equal(1))
	g.Expect(queryCalls).To(Equal(1))
	var response api.OperatorFleetPage
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.Items).To(Equal([]types.FleetRow{row}))
	g.Expect(response.Total).To(Equal(&total))
}

func TestOperatorFleetHandlerAcceptsNextCursorAfterReauthorizingAtAdvancedTime(t *testing.T) {
	g := NewWithT(t)
	cursorCodec := operatorqueries.NewCursorCodec([]byte("handler-pagination-test-key"))
	userAuth := testChannelAuth()
	organizationID := *userAuth.CurrentOrgID()
	environmentID := uuid.New()
	firstDecisionAt := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)
	secondDecisionAt := firstDecisionAt.Add(time.Minute)
	scopeChecksum, err := operatorqueries.CanonicalFilterChecksum([]string{"environment:" + environmentID.String()})
	g.Expect(err).NotTo(HaveOccurred())
	filterChecksum, err := operatorqueries.CanonicalFilterChecksum(struct{}{})
	g.Expect(err).NotTo(HaveOccurred())
	cursorScope := operatorqueries.CursorScope{
		OrganizationID: organizationID, Collection: types.OperatorCollectionFleet,
		DecisionAt: firstDecisionAt, ScopeChecksum: scopeChecksum, FilterChecksum: filterChecksum,
	}
	nextCursor, err := operatorqueries.EncodeCursor(cursorCodec, cursorScope, operatorqueries.CursorTuple{
		CreatedAt: firstDecisionAt.Add(-time.Minute), ID: uuid.New(),
	})
	g.Expect(err).NotTo(HaveOccurred())

	scopeCalls := 0
	queryCalls := 0
	dependencies := operatorControlPlaneDependencies{
		cursorCodec: cursorCodec,
		resolveScopes: func(context.Context, operatorReadPrincipal) (operatorqueries.AuditViewScopes, error) {
			scopeCalls++
			decisionAt := firstDecisionAt
			if scopeCalls == 2 {
				decisionAt = secondDecisionAt
			}
			return operatorqueries.AuditViewScopes{OrganizationID: organizationID, DecisionAt: decisionAt,
				EnvironmentIDs: []uuid.UUID{environmentID}, CustomerIDs: []uuid.UUID{},
				DeploymentUnitIDs: []uuid.UUID{}, ComponentIDs: []uuid.UUID{}, CampaignIDs: []uuid.UUID{}}, nil
		},
		listFleet: func(_ context.Context, filter types.FleetFilter, page types.PageRequest) (types.OperatorPage[types.FleetRow], error) {
			queryCalls++
			if queryCalls == 1 {
				return types.OperatorPage[types.FleetRow]{NextCursor: nextCursor}, nil
			}
			g.Expect(filter.DecisionAt).To(Equal(firstDecisionAt))
			_, err := operatorqueries.DecodeCursor(cursorCodec, page.Cursor, cursorScope)
			g.Expect(err).NotTo(HaveOccurred())
			return types.OperatorPage[types.FleetRow]{}, nil
		},
	}
	handler := operatorFleetHandler(dependencies)
	for _, path := range []string{"/fleet", "/fleet?cursor=" + nextCursor} {
		request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(auth.Authentication.NewContext(context.Background(), userAuth))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		g.Expect(recorder.Code).To(Equal(http.StatusOK))
	}
	g.Expect(scopeCalls).To(Equal(2))
	g.Expect(queryCalls).To(Equal(2))

	tamperedCursor, err := tamperOperatorCursorDecisionAt(nextCursor, firstDecisionAt.Add(time.Second))
	g.Expect(err).NotTo(HaveOccurred())
	request := httptest.NewRequest(http.MethodGet, "/fleet?cursor="+tamperedCursor, nil).WithContext(auth.Authentication.NewContext(context.Background(), userAuth))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	g.Expect(queryCalls).To(Equal(2))
}

func tamperOperatorCursorDecisionAt(value string, decisionAt time.Time) (string, error) {
	payload, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return "", err
	}
	type cursorPayload struct {
		Version        int       `json:"v"`
		Scope          string    `json:"s"`
		DecisionAt     time.Time `json:"d"`
		ScopeChecksum  string    `json:"a"`
		FilterChecksum string    `json:"f"`
		CreatedAt      time.Time `json:"t"`
		ID             uuid.UUID `json:"i"`
	}
	var envelope struct {
		Payload cursorPayload `json:"p"`
		MAC     string        `json:"m"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return "", err
	}
	if envelope.Payload.ID == uuid.Nil || envelope.MAC == "" {
		return "", errors.New("cursor payload is missing")
	}
	envelope.Payload.DecisionAt = decisionAt.UTC()
	payload, err = json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func TestOperatorReleaseDetailHandlerScopesOnceAndMapsErrors(t *testing.T) {
	userAuth := testChannelAuth()
	organizationID := *userAuth.CurrentOrgID()
	releaseID := uuid.New()
	scopes := operatorqueries.AuditViewScopes{
		OrganizationID:   organizationID,
		DecisionAt:       time.Now().UTC(),
		OrganizationWide: true,
	}

	t.Run("success", func(t *testing.T) {
		g := NewWithT(t)
		scopeCalls := 0
		queryCalls := 0
		dependencies := operatorControlPlaneDependencies{
			resolveScopes: func(context.Context, operatorReadPrincipal) (operatorqueries.AuditViewScopes, error) {
				scopeCalls++
				return scopes, nil
			},
			getRelease: func(
				_ context.Context,
				scope types.OperatorScopeFilter,
				gotReleaseID uuid.UUID,
			) (*types.OperatorReleaseDetail, error) {
				queryCalls++
				g.Expect(scope.OrganizationID).To(Equal(organizationID))
				g.Expect(gotReleaseID).To(Equal(releaseID))
				return &types.OperatorReleaseDetail{
					Release: types.OperatorReleaseRow{ID: releaseID},
				}, nil
			},
		}
		recorder := serveOperatorReleaseDetail(t, userAuth, releaseID.String(), dependencies)
		g.Expect(recorder.Code).To(Equal(http.StatusOK))
		g.Expect(scopeCalls).To(Equal(1))
		g.Expect(queryCalls).To(Equal(1))
	})

	for name, test := range map[string]struct {
		id           string
		dependencies operatorControlPlaneDependencies
		status       int
	}{
		"malformed path": {
			id: "not-a-uuid", dependencies: operatorControlPlaneDependencies{},
			status: http.StatusBadRequest,
		},
		"forbidden": {
			id: releaseID.String(),
			dependencies: operatorControlPlaneDependencies{
				resolveScopes: func(context.Context, operatorReadPrincipal) (operatorqueries.AuditViewScopes, error) {
					return operatorqueries.AuditViewScopes{}, apierrors.ErrForbidden
				},
			},
			status: http.StatusForbidden,
		},
		"not found": {
			id: releaseID.String(),
			dependencies: operatorControlPlaneDependencies{
				resolveScopes: func(context.Context, operatorReadPrincipal) (operatorqueries.AuditViewScopes, error) {
					return scopes, nil
				},
				getRelease: func(context.Context, types.OperatorScopeFilter, uuid.UUID) (*types.OperatorReleaseDetail, error) {
					return nil, apierrors.ErrNotFound
				},
			},
			status: http.StatusNotFound,
		},
		"query failure": {
			id: releaseID.String(),
			dependencies: operatorControlPlaneDependencies{
				resolveScopes: func(context.Context, operatorReadPrincipal) (operatorqueries.AuditViewScopes, error) {
					return scopes, nil
				},
				getRelease: func(context.Context, types.OperatorScopeFilter, uuid.UUID) (*types.OperatorReleaseDetail, error) {
					return nil, errors.New("database unavailable")
				},
			},
			status: http.StatusInternalServerError,
		},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := serveOperatorReleaseDetail(t, userAuth, test.id, test.dependencies)
			NewWithT(t).Expect(recorder.Code).To(Equal(test.status))
		})
	}
}

func serveOperatorReleaseDetail(
	t *testing.T,
	userAuth channelTestAuth,
	releaseID string,
	dependencies operatorControlPlaneDependencies,
) *httptest.ResponseRecorder {
	t.Helper()
	router := chi.NewRouter()
	router.Get("/releases/{releaseId}", operatorReleaseDetailHandler(dependencies))
	request := httptest.NewRequest(http.MethodGet, "/releases/"+releaseID, nil)
	request = request.WithContext(auth.Authentication.NewContext(request.Context(), userAuth))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
