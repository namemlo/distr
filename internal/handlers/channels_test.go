package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/authjwt"
	"github.com/distr-sh/distr/internal/channelrules"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

const channelVersionRangeMessage = "version does not match an allowed range"

func TestChannelFromCreateUpdateRequest(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	applicationID := uuid.New()
	lifecycleID := uuid.New()

	channel := channelFromCreateUpdateRequest(orgID, api.CreateUpdateChannelRequest{
		ApplicationID:               applicationID,
		LifecycleID:                 lifecycleID,
		Name:                        " Stable ",
		Description:                 "Default production-ready channel",
		SortOrder:                   10,
		IsDefault:                   true,
		AllowedVersionRanges:        []string{" >=1.0.0 <2.0.0 "},
		AllowedPrereleasePatterns:   []string{" rc.* "},
		AllowedSourceBranchPatterns: []string{" release/* "},
		AllowedSourceTagPatterns:    []string{" v* "},
	})

	g.Expect(channel).To(Equal(types.Channel{
		OrganizationID:              orgID,
		ApplicationID:               applicationID,
		LifecycleID:                 lifecycleID,
		Name:                        "Stable",
		Description:                 "Default production-ready channel",
		SortOrder:                   10,
		IsDefault:                   true,
		AllowedVersionRanges:        []string{">=1.0.0 <2.0.0"},
		AllowedPrereleasePatterns:   []string{"rc.*"},
		AllowedSourceBranchPatterns: []string{"release/*"},
		AllowedSourceTagPatterns:    []string{"v*"},
	}))
}

func TestChannelResponses(t *testing.T) {
	g := NewWithT(t)
	id := uuid.New()

	responses := channelResponses([]types.Channel{{ID: id, Name: "Stable"}})

	g.Expect(responses).To(Equal([]api.Channel{{ID: id, Name: "Stable"}}))
}

func TestCreateChannelHandlerRejectsInvalidPayloadsBeforeDatabaseAccess(t *testing.T) {
	applicationID := uuid.New()
	lifecycleID := uuid.New()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "empty name",
			body: `{"applicationId":"` + applicationID.String() + `","lifecycleId":"` + lifecycleID.String() + `","name":" "}`,
		},
		{
			name: "missing application id",
			body: `{"lifecycleId":"` + lifecycleID.String() + `","name":"Stable"}`,
		},
		{
			name: "missing lifecycle id",
			body: `{"applicationId":"` + applicationID.String() + `","name":"Stable"}`,
		},
		{
			name: "negative sort order",
			body: `{"applicationId":"` + applicationID.String() +
				`","lifecycleId":"` + lifecycleID.String() + `","name":"Stable","sortOrder":-1}`,
		},
		{
			name: "invalid version range",
			body: `{"applicationId":"` + applicationID.String() +
				`","lifecycleId":"` + lifecycleID.String() + `","name":"Stable","allowedVersionRanges":[">=>1.0.0"]}`,
		},
		{
			name: "empty source branch pattern",
			body: `{"applicationId":"` + applicationID.String() +
				`","lifecycleId":"` + lifecycleID.String() + `","name":"Stable","allowedSourceBranches":[" "]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/channels", strings.NewReader(tt.body))
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testChannelAuth()))

			createChannelHandler().ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})
	}
}

func TestChannelHandlersRejectMalformedUUIDPathValues(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		body    string
	}{
		{
			name:    "get",
			handler: getChannelHandler(),
			method:  http.MethodGet,
		},
		{
			name:    "update",
			handler: updateChannelHandler(),
			method:  http.MethodPut,
			body:    `{"name":"Stable"}`,
		},
		{
			name:    "delete",
			handler: deleteChannelHandler(),
			method:  http.MethodDelete,
		},
		{
			name:    "validate version",
			handler: validateChannelVersionHandler(),
			method:  http.MethodPost,
			body:    `{"version":"1.2.3"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, "/api/v1/channels/not-a-uuid", strings.NewReader(tt.body))
			request.SetPathValue("channelId", "not-a-uuid")
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testChannelAuth()))

			tt.handler.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
		})
	}
}

func TestValidateChannelVersionHandlerRejectsInvalidPayloadBeforeDatabaseAccess(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	id := uuid.New()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/channels/"+id.String()+"/validate-version",
		strings.NewReader(`{"version":" "}`),
	)
	request.SetPathValue("channelId", id.String())
	ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
	request = request.WithContext(auth.Authentication.NewContext(ctx, testChannelAuth()))

	validateChannelVersionHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
}

func TestChannelVersionValidationResponse(t *testing.T) {
	g := NewWithT(t)

	response := channelVersionValidationResponse(channelrules.Result{
		Valid: false,
		Issues: []channelrules.Issue{
			{
				Field:   "version",
				Rule:    ">=1.0.0 <2.0.0",
				Message: channelVersionRangeMessage,
			},
		},
	})

	g.Expect(response).To(Equal(api.ChannelVersionValidationResponse{
		Valid: false,
		Errors: []api.ChannelValidationError{
			{
				Field:   "version",
				Rule:    ">=1.0.0 <2.0.0",
				Message: channelVersionRangeMessage,
			},
		},
	}))
}

func TestHandleChannelWriteError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "duplicate name", err: apierrors.ErrAlreadyExists, want: http.StatusBadRequest},
		{name: "missing scoped reference", err: apierrors.ErrNotFound, want: http.StatusNotFound},
		{name: "unsafe delete", err: apierrors.ErrConflict, want: http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/channels", nil)
			request = request.WithContext(internalctx.WithLogger(request.Context(), zap.NewNop()))

			handleChannelWriteError(recorder, request, zap.NewNop(), "test", tt.err)

			g.Expect(recorder.Code).To(Equal(tt.want))
		})
	}
}

func TestChannelsFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyChannels)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/channels", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}

type channelTestAuth struct {
	orgID uuid.UUID
	role  types.UserRole
}

func testChannelAuth() channelTestAuth {
	return channelTestAuth{
		orgID: uuid.New(),
		role:  types.UserRoleAdmin,
	}
}

func (a channelTestAuth) CurrentUserID() uuid.UUID {
	return uuid.New()
}

func (a channelTestAuth) CurrentUserEmail() string {
	return "admin@example.com"
}

func (a channelTestAuth) CurrentUserRole() *types.UserRole {
	return &a.role
}

func (a channelTestAuth) CurrentOrgID() *uuid.UUID {
	return &a.orgID
}

func (a channelTestAuth) CurrentCustomerOrgID() *uuid.UUID {
	return nil
}

func (a channelTestAuth) CurrentPartnerOrgID() *uuid.UUID {
	return nil
}

func (a channelTestAuth) CurrentUserEmailVerified() bool {
	return true
}

func (a channelTestAuth) TokenScope() authjwt.TokenScope {
	return ""
}

func (a channelTestAuth) IsSuperAdmin() bool {
	return false
}

func (a channelTestAuth) Token() any {
	return nil
}

func (a channelTestAuth) CurrentOrg() *types.Organization {
	return &types.Organization{ID: a.orgID}
}

func (a channelTestAuth) CurrentUser() *types.UserAccount {
	return &types.UserAccount{ID: uuid.New(), Email: a.CurrentUserEmail()}
}
