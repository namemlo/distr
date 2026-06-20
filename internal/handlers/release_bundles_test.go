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
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestReleaseBundleFromCreateUpdateRequest(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	applicationID := uuid.New()
	channelID := uuid.New()
	versionID := uuid.New()

	bundle := releaseBundleFromCreateUpdateRequest(orgID, api.CreateUpdateReleaseBundleRequest{
		ApplicationID:  applicationID,
		ChannelID:      channelID,
		ReleaseNumber:  " 2026.06.20 ",
		ReleaseNotes:   "notes",
		SourceRevision: " abc123 ",
		Components: []api.ReleaseBundleComponentRequest{
			{
				Key:                  " api ",
				Name:                 " API ",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              " 1.2.3 ",
				ApplicationVersionID: &versionID,
			},
		},
	})

	g.Expect(bundle.OrganizationID).To(Equal(orgID))
	g.Expect(bundle.ApplicationID).To(Equal(applicationID))
	g.Expect(bundle.ChannelID).To(Equal(channelID))
	g.Expect(bundle.ReleaseNumber).To(Equal("2026.06.20"))
	g.Expect(bundle.SourceRevision).To(Equal("abc123"))
	g.Expect(bundle.Components).To(Equal([]types.ReleaseBundleComponent{
		{
			Key:                  "api",
			Name:                 "API",
			Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
			Version:              "1.2.3",
			ApplicationVersionID: &versionID,
		},
	}))
}

func TestCreateReleaseBundleHandlerRejectsInvalidPayloadBeforeDatabaseAccess(t *testing.T) {
	g := NewWithT(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/release-bundles", strings.NewReader(`{"releaseNumber":" "}`))
	ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
	request = request.WithContext(auth.Authentication.NewContext(ctx, testReleaseBundleAuth()))

	createReleaseBundleHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
}

func TestReleaseBundleHandlersRejectMalformedUUIDPathValues(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		body    string
	}{
		{name: "get", handler: getReleaseBundleHandler(), method: http.MethodGet},
		{name: "update", handler: updateReleaseBundleHandler(), method: http.MethodPut, body: `{"releaseNumber":"1.2.3"}`},
		{name: "delete", handler: deleteReleaseBundleHandler(), method: http.MethodDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, "/api/v1/release-bundles/not-a-uuid", strings.NewReader(tt.body))
			request.SetPathValue("releaseBundleId", "not-a-uuid")
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testReleaseBundleAuth()))

			tt.handler.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
		})
	}
}

//nolint:dupl
func TestHandleReleaseBundleWriteError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "duplicate release number", err: apierrors.ErrAlreadyExists, want: http.StatusBadRequest},
		{name: "missing scoped reference", err: apierrors.ErrNotFound, want: http.StatusNotFound},
		{name: "non-draft mutation", err: apierrors.ErrConflict, want: http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/release-bundles", nil)
			request = request.WithContext(internalctx.WithLogger(request.Context(), zap.NewNop()))

			handleReleaseBundleWriteError(recorder, request, zap.NewNop(), "test", tt.err)

			g.Expect(recorder.Code).To(Equal(tt.want))
		})
	}
}

func TestReleaseBundlesFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyReleaseBundles)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/release-bundles", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}

type releaseBundleTestAuth struct {
	orgID uuid.UUID
	role  types.UserRole
}

func testReleaseBundleAuth() releaseBundleTestAuth {
	return releaseBundleTestAuth{
		orgID: uuid.New(),
		role:  types.UserRoleAdmin,
	}
}

func (a releaseBundleTestAuth) CurrentUserID() uuid.UUID {
	return uuid.New()
}

func (a releaseBundleTestAuth) CurrentUserEmail() string {
	return "admin@example.com"
}

func (a releaseBundleTestAuth) CurrentUserRole() *types.UserRole {
	return &a.role
}

func (a releaseBundleTestAuth) CurrentOrgID() *uuid.UUID {
	return &a.orgID
}

func (a releaseBundleTestAuth) CurrentCustomerOrgID() *uuid.UUID {
	return nil
}

func (a releaseBundleTestAuth) CurrentPartnerOrgID() *uuid.UUID {
	return nil
}

func (a releaseBundleTestAuth) CurrentUserEmailVerified() bool {
	return true
}

func (a releaseBundleTestAuth) TokenScope() authjwt.TokenScope {
	return ""
}

func (a releaseBundleTestAuth) IsSuperAdmin() bool {
	return false
}

func (a releaseBundleTestAuth) Token() any {
	return nil
}

func (a releaseBundleTestAuth) CurrentOrg() *types.Organization {
	return &types.Organization{ID: a.orgID}
}

func (a releaseBundleTestAuth) CurrentUser() *types.UserAccount {
	return &types.UserAccount{ID: uuid.New(), Email: a.CurrentUserEmail()}
}
