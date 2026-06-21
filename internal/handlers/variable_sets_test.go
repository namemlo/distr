package handlers

import (
	"encoding/json"
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

func TestVariableSetFromCreateUpdateRequest(t *testing.T) {
	g := NewWithT(t)
	orgID := uuid.New()
	applicationID := uuid.New()
	secretID := uuid.New()

	variableSet := variableSetFromCreateUpdateRequest(orgID, api.CreateUpdateVariableSetRequest{
		Name:           " Shared Defaults ",
		Description:    "Reusable defaults",
		SortOrder:      10,
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []api.VariableRequest{
			{Key: " api_url ", Type: api.VariableTypeString, DefaultValue: json.RawMessage(`"https://example.test"`)},
			{Key: "api_token", Type: api.VariableTypeSecretReference, ReferenceID: secretID.String()},
		},
	})

	g.Expect(variableSet).To(Equal(types.VariableSet{
		OrganizationID: orgID,
		Name:           "Shared Defaults",
		Description:    "Reusable defaults",
		SortOrder:      10,
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{Key: "api_url", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://example.test"`)},
			{Key: "api_token", Type: types.VariableTypeSecretReference, ReferenceID: secretID.String()},
		},
	}))
}

func TestCreateVariableSetHandlerRejectsInvalidPayloadsBeforeDatabaseAccess(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty name", body: `{"name":" "}`},
		{name: "invalid type", body: `{"name":"Shared","variables":[{"key":"api_url","type":"bad"}]}`},
		{
			name: "inline secret",
			body: `{"name":"Shared","variables":[{"key":"api_token","type":"secret_reference",` +
				`"defaultValue":"plaintext","referenceId":"` + uuid.NewString() + `"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/variable-sets", strings.NewReader(tt.body))
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testVariableSetAuth()))

			createVariableSetHandler().ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})
	}
}

func TestVariableSetHandlersRejectMalformedUUIDPathValues(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		body    string
	}{
		{name: "get", handler: getVariableSetHandler(), method: http.MethodGet},
		{name: "update", handler: updateVariableSetHandler(), method: http.MethodPut, body: `{"name":"Shared"}`},
		{name: "delete", handler: deleteVariableSetHandler(), method: http.MethodDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, "/api/v1/variable-sets/not-a-uuid", strings.NewReader(tt.body))
			request.SetPathValue("variableSetId", "not-a-uuid")
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testVariableSetAuth()))

			tt.handler.ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
		})
	}
}

//nolint:dupl
func TestHandleVariableSetWriteError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "duplicate name", err: apierrors.ErrAlreadyExists, want: http.StatusBadRequest},
		{name: "invalid payload", err: apierrors.ErrBadRequest, want: http.StatusBadRequest},
		{name: "missing scoped reference", err: apierrors.ErrNotFound, want: http.StatusNotFound},
		{name: "unsafe delete", err: apierrors.ErrConflict, want: http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/variable-sets", nil)
			request = request.WithContext(internalctx.WithLogger(request.Context(), zap.NewNop()))

			handleVariableSetWriteError(recorder, request, zap.NewNop(), "test", tt.err)

			g.Expect(recorder.Code).To(Equal(tt.want))
		})
	}
}

func TestVariableSetsFeatureFlagMiddlewareRejectsDisabledAPI(t *testing.T) {
	g := NewWithT(t)
	called := false
	handler := middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyScopedVariablesV2)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/variable-sets", nil)

	handler.ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(called).To(BeFalse())
}

type variableSetTestAuth struct {
	orgID  uuid.UUID
	userID uuid.UUID
	role   types.UserRole
}

func testVariableSetAuth() variableSetTestAuth {
	return variableSetTestAuth{
		orgID:  uuid.New(),
		userID: uuid.New(),
		role:   types.UserRoleAdmin,
	}
}

func (a variableSetTestAuth) CurrentUserID() uuid.UUID {
	return a.userID
}

func (a variableSetTestAuth) CurrentUserEmail() string {
	return "admin@example.com"
}

func (a variableSetTestAuth) CurrentUserRole() *types.UserRole {
	return &a.role
}

func (a variableSetTestAuth) CurrentOrgID() *uuid.UUID {
	return &a.orgID
}

func (a variableSetTestAuth) CurrentCustomerOrgID() *uuid.UUID {
	return nil
}

func (a variableSetTestAuth) CurrentPartnerOrgID() *uuid.UUID {
	return nil
}

func (a variableSetTestAuth) CurrentUserEmailVerified() bool {
	return true
}

func (a variableSetTestAuth) TokenScope() authjwt.TokenScope {
	return ""
}

func (a variableSetTestAuth) IsSuperAdmin() bool {
	return false
}

func (a variableSetTestAuth) Token() any {
	return nil
}

func (a variableSetTestAuth) CurrentOrg() *types.Organization {
	return &types.Organization{ID: a.orgID}
}

func (a variableSetTestAuth) CurrentUser() *types.UserAccount {
	return &types.UserAccount{ID: a.userID, Email: a.CurrentUserEmail()}
}
