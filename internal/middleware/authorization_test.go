package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
	var received types.AccessRequest
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
			_ context.Context,
			request types.AccessRequest,
		) (types.AccessDecision, error) {
			received = request
			return types.AccessDecision{Allowed: true}, nil
		},
		isEffective: func(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
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
		isEffective: func(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
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
}

func (value authorizationMiddlewareAuth) CurrentUserID() uuid.UUID {
	return value.principalID
}

func (value authorizationMiddlewareAuth) CurrentOrgID() *uuid.UUID {
	return &value.organizationID
}
