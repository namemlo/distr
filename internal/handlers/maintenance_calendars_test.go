package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/authn/authinfo"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestMaintenanceCalendarJSONBodyIsStrictAndBounded(t *testing.T) {
	g := NewWithT(t)
	for _, body := range []string{
		`{"name":"calendar","unknown":true}`,
		`{"name":"calendar"} {}`,
		strings.Repeat(" ", int(maintenanceCalendarMaximumBodyBytes)+1),
	} {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		recorder := httptest.NewRecorder()

		_, err := maintenanceCalendarJSONBody[api.CreateMaintenanceCalendarRequest](
			recorder,
			request,
		)

		g.Expect(err).To(HaveOccurred())
		g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	}
}

func TestMaintenanceCalendarListRequestRejectsAmbiguousQueryValues(t *testing.T) {
	g := NewWithT(t)
	for _, rawURL := range []string{
		"/?limit=10&limit=20",
		"/?cursor=first&cursor=second",
		"/?limit=0",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, rawURL, nil)

		_, ok := maintenanceCalendarListRequestFromHTTP(recorder, request)

		g.Expect(ok).To(BeFalse(), rawURL)
		g.Expect(recorder.Code).To(Equal(http.StatusBadRequest), rawURL)
	}
}

func TestMaintenanceCalendarRoutesAreHiddenAndAdminOnly(t *testing.T) {
	const path = "/api/v1/maintenance-calendars/"
	tests := []struct {
		name       string
		flags      []featureflags.Key
		role       types.UserRole
		superAdmin bool
		want       int
	}{
		{
			name: "disabled route is hidden",
			role: types.UserRoleAdmin,
			want: http.StatusNotFound,
		},
		{
			name:  "read write role cannot manage calendars",
			flags: []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			role:  types.UserRoleReadWrite,
			want:  http.StatusForbidden,
		},
		{
			name:       "super admin cannot manage calendars",
			flags:      []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			role:       types.UserRoleAdmin,
			superAdmin: true,
			want:       http.StatusForbidden,
		},
		{
			name:  "admin reaches request validation",
			flags: []featureflags.Key{featureflags.KeyOperatorControlPlaneV2},
			role:  types.UserRoleAdmin,
			want:  http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			router := maintenanceCalendarRoutedTestHandler(
				tt.flags,
				allowCalendarActionsForTest(),
			)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			testAuth := testChannelAuth()
			testAuth.role = tt.role
			var userAuth authinfo.AuthInfoWithUserAndOrganization = testAuth
			if tt.superAdmin {
				userAuth = deploymentRegistrySuperAdminAuth{channelTestAuth: testAuth}
			}
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, userAuth))

			router.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(tt.want), recorder.Body.String())
		})
	}
}

func TestMaintenanceCalendarDisabledRouteIsHiddenWithoutAuthentication(t *testing.T) {
	g := NewWithT(t)
	router := maintenanceCalendarRoutedTestHandler(nil, allowCalendarActionsForTest())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/maintenance-calendars/",
		nil,
	)

	router.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func TestDeploymentFreezeDisabledRouteIsHiddenWithoutAuthentication(t *testing.T) {
	g := NewWithT(t)
	baseRouter := chi.NewRouter()
	openAPIRouter := chiopenapi.NewRouter(baseRouter)
	openAPIRouter.Route("/api/v1/deployment-freezes", func(r chiopenapi.Router) {
		deploymentFreezesRouterWithDependencies(r, nil, allowCalendarActionsForTest())
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployment-freezes/",
		nil,
	)

	baseRouter.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func TestMaintenanceCalendarActionAuthorizationStopsBeforeDatabaseAccess(t *testing.T) {
	g := NewWithT(t)
	authorizer := calendarActionAuthorizerFunc(func(
		_ context.Context,
		organizationID, actorID uuid.UUID,
		action string,
		scope types.CalendarScopeRef,
	) error {
		g.Expect(organizationID).NotTo(Equal(uuid.Nil))
		g.Expect(actorID).NotTo(Equal(uuid.Nil))
		g.Expect(action).To(Equal(calendarActionManage))
		g.Expect(scope).To(Equal(types.CalendarScopeRef{
			Kind: types.CalendarScopeOrganization,
			ID:   organizationID,
		}))
		return apierrors.ErrForbidden
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/maintenance-calendars/",
		strings.NewReader(`{
			"name":"Retail production",
			"description":"production maintenance",
			"ianaZone":"Asia/Bangkok",
			"ruleVersion":"2026a",
			"windowRules":[
				{"name":"evening","weekdays":[1],"startMinute":1200,"endMinute":1320,"sortOrder":1}
			]
		}`),
	)
	testAuth := testChannelAuth()
	ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
	request = request.WithContext(auth.Authentication.NewContext(ctx, testAuth))

	createMaintenanceCalendarHandler(authorizer).ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
}

func TestProductionCalendarAuthorizationSeamFailsClosedUntilPR066Adapter(t *testing.T) {
	g := NewWithT(t)
	organizationID := uuid.New()
	err := authorizeCalendarAction(
		context.Background(),
		newCalendarActionAuthorizer(),
		organizationID,
		uuid.New(),
		calendarActionManage,
		types.CalendarScopeRef{
			Kind: types.CalendarScopeOrganization,
			ID:   organizationID,
		},
	)
	g.Expect(err).To(MatchError(apierrors.ErrForbidden))
}

func TestFreezeScopeTransitionAuthorizesCurrentBeforeDestination(t *testing.T) {
	g := NewWithT(t)
	current := types.CalendarScopeRef{
		Kind: types.CalendarScopeEnvironment,
		ID:   uuid.New(),
	}
	destination := types.CalendarScopeRef{
		Kind: types.CalendarScopeEnvironment,
		ID:   uuid.New(),
	}
	visited := []types.CalendarScopeRef{}
	authorizer := calendarActionAuthorizerFunc(func(
		_ context.Context,
		_, _ uuid.UUID,
		_ string,
		scope types.CalendarScopeRef,
	) error {
		visited = append(visited, scope)
		if scope == current {
			return apierrors.ErrForbidden
		}
		return nil
	})

	err := authorizeDeploymentFreezeScopeTransition(
		context.Background(),
		authorizer,
		uuid.New(),
		uuid.New(),
		current,
		destination,
	)
	g.Expect(err).To(MatchError(apierrors.ErrForbidden))
	g.Expect(visited).To(Equal([]types.CalendarScopeRef{current}))
}

func TestDeploymentFreezeActionAuthorizationReceivesRequestedScope(t *testing.T) {
	g := NewWithT(t)
	scopeID := uuid.New()
	authorizer := calendarActionAuthorizerFunc(func(
		_ context.Context,
		_, _ uuid.UUID,
		action string,
		scope types.CalendarScopeRef,
	) error {
		g.Expect(action).To(Equal(freezeActionManage))
		g.Expect(scope).To(Equal(types.CalendarScopeRef{
			Kind: types.CalendarScopeEnvironment,
			ID:   scopeID,
		}))
		return apierrors.ErrForbidden
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/deployment-freezes/",
		strings.NewReader(`{
			"name":"Retail settlement",
			"startAt":"2026-07-18T12:00:00Z",
			"endAt":"2026-07-18T13:00:00Z",
			"ianaZone":"Asia/Bangkok",
			"ruleVersion":"2026a",
			"scopeKind":"environment",
			"scopeId":"`+scopeID.String()+`",
			"priority":100,
			"reason":"settlement"
		}`),
	)
	testAuth := testChannelAuth()
	ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
	request = request.WithContext(auth.Authentication.NewContext(ctx, testAuth))

	createDeploymentFreezeHandler(authorizer).ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
}

func TestMaintenanceCalendarHandlersRejectMalformedPathIDs(t *testing.T) {
	g := NewWithT(t)
	request := httptest.NewRequest(http.MethodGet, "/not-a-uuid", nil)
	request.SetPathValue("calendarId", "not-a-uuid")
	recorder := httptest.NewRecorder()

	getMaintenanceCalendarHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
}

func TestMaintenanceCalendarErrorMappingIsStableAndTenantSafe(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{err: apierrors.ErrBadRequest, want: http.StatusBadRequest},
		{err: apierrors.ErrForbidden, want: http.StatusForbidden},
		{err: apierrors.ErrNotFound, want: http.StatusNotFound},
		{err: apierrors.ErrAlreadyExists, want: http.StatusConflict},
		{err: apierrors.ErrConflict, want: http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			request = request.WithContext(
				internalctx.WithLogger(request.Context(), zap.NewNop()),
			)

			handleMaintenanceCalendarError(recorder, request, "calendar operation", tt.err)

			g.Expect(recorder.Code).To(Equal(tt.want))
			g.Expect(strings.ToLower(recorder.Body.String())).NotTo(Or(
				ContainSubstring("sqlstate"),
				ContainSubstring("constraint"),
				ContainSubstring("foreign key"),
				ContainSubstring("pgconn"),
			))
		})
	}
}

func maintenanceCalendarRoutedTestHandler(
	enabledFlags []featureflags.Key,
	authorizer calendarActionAuthorizer,
) http.Handler {
	baseRouter := chi.NewRouter()
	openAPIRouter := chiopenapi.NewRouter(baseRouter)
	openAPIRouter.Route("/api/v1/maintenance-calendars", func(r chiopenapi.Router) {
		maintenanceCalendarsRouterWithDependencies(r, enabledFlags, authorizer)
	})
	return baseRouter
}

func allowCalendarActionsForTest() calendarActionAuthorizer {
	return calendarActionAuthorizerFunc(func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		string,
		types.CalendarScopeRef,
	) error {
		return nil
	})
}
