package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestRequireControlPlaneActionUsesResolvedScopedAuthorization(t *testing.T) {
	organizationID := uuid.New()
	principalID := uuid.New()
	environmentID := uuid.New()
	decisionAt := time.Date(2026, time.July, 18, 6, 0, 0, 0, time.UTC)
	var enrollmentDecisionAt time.Time
	var received types.AccessRequest
	dependencies := controlPlaneActionDependencies{
		clock:          func() time.Time { return decisionAt },
		processEnabled: func() bool { return true },
		resolveScopes: func(
			context.Context,
			types.ResourceRef,
		) ([]types.ScopeRef, error) {
			return []types.ScopeRef{
				{Kind: types.PermissionScopeOrganization, ID: organizationID},
				{Kind: types.PermissionScopeEnvironment, ID: environmentID},
			}, nil
		},
		authorize: func(
			_ context.Context,
			request types.AccessRequest,
		) (types.AccessDecision, error) {
			received = request
			return types.AccessDecision{Allowed: true}, nil
		},
		isEffective: func(
			_ context.Context,
			_ uuid.UUID,
			_ uuid.UUID,
			at time.Time,
		) (bool, error) {
			enrollmentDecisionAt = at
			return true, nil
		},
	}
	request := authorizationMiddlewareRequest(organizationID, principalID)
	recorder := httptest.NewRecorder()

	handler := requireControlPlaneActionWith(
		types.ActionPlanExecute,
		func(*http.Request, uuid.UUID) (types.ResourceRef, error) {
			return types.ResourceRef{
				OrganizationID: organizationID,
				Kind:           types.PermissionScopeEnvironment,
				ID:             environmentID,
			}, nil
		},
		true,
		dependencies,
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(recorder, request)

	g := NewWithT(t)
	g.Expect(recorder.Code).To(Equal(http.StatusNoContent))
	g.Expect(received.OrganizationID).To(Equal(organizationID))
	g.Expect(received.PrincipalID).To(Equal(principalID))
	g.Expect(received.Action).To(Equal(types.ActionPlanExecute))
	g.Expect(received.ResourceScopes).To(HaveLen(2))
	g.Expect(received.CredentialRole).NotTo(BeNil())
	g.Expect(*received.CredentialRole).To(Equal(types.UserRoleAdmin))
	g.Expect(received.DecisionAt).To(Equal(decisionAt))
	g.Expect(enrollmentDecisionAt).To(Equal(decisionAt))
}

func TestRequireControlPlaneActionHidesDisabledAndForeignResources(t *testing.T) {
	organizationID := uuid.New()
	principalID := uuid.New()

	cases := []struct {
		name         string
		dependencies controlPlaneActionDependencies
	}{
		{
			name: "process disabled",
			dependencies: controlPlaneActionDependencies{
				processEnabled: func() bool { return false },
			},
		},
		{
			name: "scope not found",
			dependencies: controlPlaneActionDependencies{
				processEnabled: func() bool { return true },
				resolveScopes: func(
					context.Context,
					types.ResourceRef,
				) ([]types.ScopeRef, error) {
					return nil, apierrors.ErrNotFound
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := authorizationMiddlewareRequest(organizationID, principalID)
			handler := requireControlPlaneActionWith(
				types.ActionPlanExecute,
				OrganizationResourceRef,
				false,
				tc.dependencies,
			)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))

			handler.ServeHTTP(recorder, request)

			g := NewWithT(t)
			g.Expect(recorder.Code).To(Equal(http.StatusNotFound))
		})
	}
}

func TestRequireControlPlaneActionUsesGenericDeniedResponse(t *testing.T) {
	organizationID := uuid.New()
	principalID := uuid.New()
	dependencies := controlPlaneActionDependencies{
		processEnabled: func() bool { return true },
		resolveScopes: func(
			context.Context,
			types.ResourceRef,
		) ([]types.ScopeRef, error) {
			return []types.ScopeRef{{
				Kind: types.PermissionScopeOrganization,
				ID:   organizationID,
			}}, nil
		},
		authorize: func(
			context.Context,
			types.AccessRequest,
		) (types.AccessDecision, error) {
			return types.AccessDecision{
				Allowed:    false,
				ReasonCode: types.AccessReasonDenied,
			}, nil
		},
	}
	recorder := httptest.NewRecorder()
	handler := requireControlPlaneActionWith(
		types.ActionAuthorizationManage,
		OrganizationResourceRef,
		false,
		dependencies,
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(
		recorder,
		authorizationMiddlewareRequest(organizationID, principalID),
	)

	g := NewWithT(t)
	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(recorder.Body.String()).To(Equal("insufficient permissions\n"))
	g.Expect(recorder.Body.String()).NotTo(ContainSubstring(organizationID.String()))
}

func TestRequireControlPlaneActionDoesNotRevealEnrollmentBeforeAuthorization(t *testing.T) {
	organizationID := uuid.New()
	principalID := uuid.New()
	environmentID := uuid.New()
	enrollmentChecked := false
	dependencies := controlPlaneActionDependencies{
		processEnabled: func() bool { return true },
		resolveScopes: func(
			context.Context,
			types.ResourceRef,
		) ([]types.ScopeRef, error) {
			return []types.ScopeRef{
				{Kind: types.PermissionScopeOrganization, ID: organizationID},
				{Kind: types.PermissionScopeEnvironment, ID: environmentID},
			}, nil
		},
		authorize: func(
			context.Context,
			types.AccessRequest,
		) (types.AccessDecision, error) {
			return types.AccessDecision{Allowed: false}, nil
		},
		isEffective: func(
			context.Context,
			uuid.UUID,
			uuid.UUID,
			time.Time,
		) (bool, error) {
			enrollmentChecked = true
			return true, nil
		},
	}
	recorder := httptest.NewRecorder()
	handler := requireControlPlaneActionWith(
		types.ActionPlanExecute,
		OrganizationResourceRef,
		true,
		dependencies,
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(
		recorder,
		authorizationMiddlewareRequest(organizationID, principalID),
	)

	g := NewWithT(t)
	g.Expect(recorder.Code).To(Equal(http.StatusForbidden))
	g.Expect(enrollmentChecked).To(BeFalse())
}

func TestControlPlaneReadAllowsSuperAdminButMutationRemainsDenied(t *testing.T) {
	organizationID := uuid.New()
	principalID := uuid.New()
	dependencies := controlPlaneActionDependencies{
		clock:          func() time.Time { return time.Now().UTC() },
		processEnabled: func() bool { return true },
		resolveScopes: func(
			context.Context,
			types.ResourceRef,
		) ([]types.ScopeRef, error) {
			return []types.ScopeRef{{
				Kind: types.PermissionScopeOrganization,
				ID:   organizationID,
			}}, nil
		},
		authorize: func(
			context.Context,
			types.AccessRequest,
		) (types.AccessDecision, error) {
			t.Fatal("superadmin read must not depend on an organization role binding")
			return types.AccessDecision{}, nil
		},
	}
	role := (*types.UserRole)(nil)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(auth.Authentication.NewContext(
		request.Context(),
		authorizationMiddlewareAuth{
			testPermissionAuth: testPermissionAuth{
				role:       role,
				superAdmin: true,
			},
			organizationID: organizationID,
			principalID:    principalID,
		},
	))

	readRecorder := httptest.NewRecorder()
	requireControlPlaneReadActionWith(
		types.ActionAuthorizationManage,
		OrganizationResourceRef,
		dependencies,
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(readRecorder, request)

	mutationRecorder := httptest.NewRecorder()
	requireControlPlaneActionWith(
		types.ActionAuthorizationManage,
		OrganizationResourceRef,
		false,
		dependencies,
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(mutationRecorder, request)

	g := NewWithT(t)
	g.Expect(readRecorder.Code).To(Equal(http.StatusNoContent))
	g.Expect(mutationRecorder.Code).To(Equal(http.StatusForbidden))
}

func TestAuthorizationVendorBoundaryMatrix(t *testing.T) {
	organizationID := uuid.New()
	principalID := uuid.New()
	role := types.UserRoleAdmin
	customerID := uuid.New()
	partnerID := uuid.New()
	for _, tc := range []struct {
		name       string
		customerID *uuid.UUID
		partnerID  *uuid.UUID
		superAdmin bool
		want       int
	}{
		{name: "vendor", want: http.StatusNoContent},
		{
			name:       "customer denied",
			customerID: &customerID,
			want:       http.StatusForbidden,
		},
		{
			name:      "partner denied",
			partnerID: &partnerID,
			want:      http.StatusForbidden,
		},
		{
			name:       "superadmin read boundary",
			partnerID:  &partnerID,
			superAdmin: true,
			want:       http.StatusNoContent,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request = request.WithContext(auth.Authentication.NewContext(
				request.Context(),
				authorizationMiddlewareAuth{
					testPermissionAuth: testPermissionAuth{
						role:       &role,
						superAdmin: tc.superAdmin,
					},
					organizationID: organizationID,
					principalID:    principalID,
					customerID:     tc.customerID,
					partnerID:      tc.partnerID,
				},
			))
			recorder := httptest.NewRecorder()

			RequireVendor(http.HandlerFunc(func(
				w http.ResponseWriter,
				_ *http.Request,
			) {
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(recorder, request)

			NewWithT(t).Expect(recorder.Code).To(Equal(tc.want))
		})
	}
}

func authorizationMiddlewareRequest(
	organizationID uuid.UUID,
	principalID uuid.UUID,
) *http.Request {
	role := types.UserRoleAdmin
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	return request.WithContext(auth.Authentication.NewContext(
		request.Context(),
		authorizationMiddlewareAuth{
			testPermissionAuth: testPermissionAuth{role: &role},
			organizationID:     organizationID,
			principalID:        principalID,
		},
	))
}

type authorizationMiddlewareAuth struct {
	testPermissionAuth
	organizationID uuid.UUID
	principalID    uuid.UUID
	customerID     *uuid.UUID
	partnerID      *uuid.UUID
}

func (value authorizationMiddlewareAuth) CurrentUserID() uuid.UUID {
	return value.principalID
}

func (value authorizationMiddlewareAuth) CurrentOrgID() *uuid.UUID {
	return &value.organizationID
}

func (value authorizationMiddlewareAuth) CurrentCustomerOrgID() *uuid.UUID {
	return value.customerID
}

func (value authorizationMiddlewareAuth) CurrentPartnerOrgID() *uuid.UUID {
	return value.partnerID
}
