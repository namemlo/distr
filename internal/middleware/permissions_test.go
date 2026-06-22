package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/authjwt"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestRequireOrganizationPermission(t *testing.T) {
	cases := []struct {
		name       string
		role       *types.UserRole
		permission types.Permission
		expected   int
	}{
		{
			name:       "read only may view releases",
			role:       ptrTo(types.UserRoleReadOnly),
			permission: types.PermissionReleaseView,
			expected:   http.StatusNoContent,
		},
		{
			name:       "read only may not create releases",
			role:       ptrTo(types.UserRoleReadOnly),
			permission: types.PermissionReleaseCreate,
			expected:   http.StatusForbidden,
		},
		{
			name:       "read write may execute deployments",
			role:       ptrTo(types.UserRoleReadWrite),
			permission: types.PermissionDeploymentExecute,
			expected:   http.StatusNoContent,
		},
		{
			name:       "admin may manage environments",
			role:       ptrTo(types.UserRoleAdmin),
			permission: types.PermissionEnvironmentManage,
			expected:   http.StatusNoContent,
		},
		{
			name:       "missing role is forbidden",
			role:       nil,
			permission: types.PermissionReleaseView,
			expected:   http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			request = request.WithContext(auth.Authentication.NewContext(request.Context(), testPermissionAuth{role: tc.role}))

			handler := RequireOrganizationPermission(tc.permission)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				}),
			)

			handler.ServeHTTP(recorder, request)
			g.Expect(recorder.Code).To(Equal(tc.expected))
		})
	}
}

func TestRequireOrganizationPermissionAllowsSuperAdmin(t *testing.T) {
	g := NewWithT(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request = request.WithContext(auth.Authentication.NewContext(request.Context(), testPermissionAuth{superAdmin: true}))

	handler := RequireOrganizationPermission(types.PermissionDeploymentExecute)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	handler.ServeHTTP(recorder, request)
	g.Expect(recorder.Code).To(Equal(http.StatusNoContent))
}

func TestRequireReadWriteOrAdminUsesMutationPermissions(t *testing.T) {
	cases := []struct {
		name     string
		role     *types.UserRole
		expected int
	}{
		{name: "read only denied", role: ptrTo(types.UserRoleReadOnly), expected: http.StatusForbidden},
		{name: "read write allowed", role: ptrTo(types.UserRoleReadWrite), expected: http.StatusNoContent},
		{name: "admin allowed", role: ptrTo(types.UserRoleAdmin), expected: http.StatusNoContent},
		{name: "missing role denied", role: nil, expected: http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			request = request.WithContext(auth.Authentication.NewContext(request.Context(), testPermissionAuth{role: tc.role}))

			RequireReadWriteOrAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(recorder, request)

			g.Expect(recorder.Code).To(Equal(tc.expected))
		})
	}
}

func TestRequireScopedPermissionRejectsUnsupportedScope(t *testing.T) {
	g := NewWithT(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request = request.WithContext(
		auth.Authentication.NewContext(request.Context(), testPermissionAuth{role: ptrTo(types.UserRoleAdmin)}),
	)

	handler := RequireScopedPermission(types.ScopedPermission{
		Permission: types.PermissionDeploymentExecute,
		Scope:      types.PermissionScopeApplication,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(recorder, request)
	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
}

type testPermissionAuth struct {
	role       *types.UserRole
	superAdmin bool
}

func (a testPermissionAuth) CurrentUserID() uuid.UUID         { return uuid.Nil }
func (a testPermissionAuth) CurrentUserEmail() string         { return "test@example.com" }
func (a testPermissionAuth) CurrentUserRole() *types.UserRole { return a.role }
func (a testPermissionAuth) CurrentOrgID() *uuid.UUID         { return ptrTo(uuid.Nil) }
func (a testPermissionAuth) CurrentCustomerOrgID() *uuid.UUID { return nil }
func (a testPermissionAuth) CurrentPartnerOrgID() *uuid.UUID  { return nil }
func (a testPermissionAuth) CurrentUserEmailVerified() bool   { return true }
func (a testPermissionAuth) TokenScope() authjwt.TokenScope   { return "" }
func (a testPermissionAuth) IsSuperAdmin() bool               { return a.superAdmin }
func (a testPermissionAuth) Token() any                       { return nil }
func (a testPermissionAuth) CurrentOrg() *types.Organization {
	return &types.Organization{ID: uuid.Nil}
}
func (a testPermissionAuth) CurrentUser() *types.UserAccount { return &types.UserAccount{ID: uuid.Nil} }

func ptrTo[T any](value T) *T {
	return &value
}
