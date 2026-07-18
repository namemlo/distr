package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestAuthorizationResponseMappingsOmitTenantInternals(t *testing.T) {
	organizationID := uuid.New()
	roleID := uuid.New()
	groupID := uuid.New()
	actorID := uuid.New()
	now := time.Now().UTC()

	g := NewWithT(t)
	g.Expect(authorizationRoleResponse(types.RoleDefinition{
		ID:              roleID,
		CreatedAt:       now,
		OrganizationID:  organizationID,
		Key:             "operators",
		DisplayName:     "Operators",
		Revision:        1,
		CreatedByUserID: &actorID,
		Permissions:     []types.Action{types.ActionPlanExecute},
	})).To(Equal(api.AuthorizationRole{
		ID:                     roleID,
		CreatedAt:              now,
		Key:                    "operators",
		DisplayName:            "Operators",
		Revision:               1,
		CreatedByUserAccountID: &actorID,
		Permissions:            []types.Action{types.ActionPlanExecute},
	}))

	g.Expect(authorizationGroupResponse(types.PrincipalGroup{
		ID:              groupID,
		CreatedAt:       now,
		OrganizationID:  organizationID,
		Key:             "approvers",
		DisplayName:     "Approvers",
		CreatedByUserID: &actorID,
	})).To(Equal(api.AuthorizationPrincipalGroup{
		ID:                     groupID,
		CreatedAt:              now,
		Key:                    "approvers",
		DisplayName:            "Approvers",
		CreatedByUserAccountID: &actorID,
	}))
}

func TestAuthorizationCreateHandlersRejectInvalidPayloadBeforeDatabaseAccess(t *testing.T) {
	cases := []struct {
		name       string
		handler    http.Handler
		body       string
		pathValues map[string]string
	}{
		{
			name:    "role",
			handler: createAuthorizationRoleHandler(),
			body:    `{"key":"operators","displayName":"Operators","permissions":["unknown"]}`,
		},
		{
			name:    "binding",
			handler: createAuthorizationRoleBindingHandler(),
			body:    `{"principalKind":"user","reason":"grant"}`,
		},
		{
			name:    "group",
			handler: createAuthorizationGroupHandler(),
			body:    `{"key":" ","displayName":" "}`,
		},
		{
			name:    "enrollment",
			handler: createControlPlaneEnrollmentHandler(),
			body:    `{"scope":{"kind":"campaign","id":"` + uuid.NewString() + `"},"enabled":true}`,
		},
		{
			name:    "binding revocation",
			handler: revokeAuthorizationRoleBindingHandler(),
			body:    `{"reason":" "}`,
			pathValues: map[string]string{
				"bindingId": uuid.NewString(),
			},
		},
		{
			name:    "membership revocation",
			handler: revokeAuthorizationGroupMemberHandler(),
			body:    `{"reason":" "}`,
			pathValues: map[string]string{
				"groupId":  uuid.NewString(),
				"memberId": uuid.NewString(),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			for key, value := range tc.pathValues {
				request.SetPathValue(key, value)
			}
			ctx := internalctx.WithLogger(request.Context(), zap.NewNop())
			request = request.WithContext(auth.Authentication.NewContext(ctx, testChannelAuth()))

			tc.handler.ServeHTTP(recorder, request)

			NewWithT(t).Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})
	}
}

func TestAuthorizationWriteErrorsAreTenantSafe(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "foreign resource", err: apierrors.ErrNotFound, want: http.StatusNotFound},
		{name: "duplicate immutable record", err: apierrors.ErrAlreadyExists, want: http.StatusConflict},
		{name: "invalid request", err: apierrors.ErrBadRequest, want: http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			handleAuthorizationWriteError(recorder, request, tc.err)

			g := NewWithT(t)
			g.Expect(recorder.Code).To(Equal(tc.want))
			g.Expect(recorder.Body.String()).NotTo(ContainSubstring(uuid.NewString()))
		})
	}
}

func TestAuthorizationListRequestRejectsExplicitZeroAndMalformedCursor(t *testing.T) {
	for _, rawQuery := range []string{"?limit=0", "?limit=101", "?cursor=bad+cursor"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/"+rawQuery, nil)

		_, ok := authorizationListRequestFromHTTP(recorder, request)

		g := NewWithT(t)
		g.Expect(ok).To(BeFalse(), rawQuery)
		g.Expect(recorder.Code).To(Equal(http.StatusBadRequest), rawQuery)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	page, ok := authorizationListRequestFromHTTP(recorder, request)
	g := NewWithT(t)
	g.Expect(ok).To(BeTrue())
	g.Expect(page.Limit).To(BeNil())
}

func TestAuthorizationMemberListResolvesTenantParentBeforeReturningEmptyPage(t *testing.T) {
	organizationID := uuid.New()
	groupID := uuid.New()
	for _, tc := range []struct {
		name           string
		rawQuery       string
		parentError    error
		listError      error
		wantStatus     int
		wantListCalled bool
	}{
		{
			name:           "missing or foreign parent is hidden before cursor validation",
			rawQuery:       "?cursor=bad+cursor",
			parentError:    apierrors.ErrNotFound,
			wantStatus:     http.StatusNotFound,
			wantListCalled: false,
		},
		{
			name:           "existing empty parent returns empty page",
			wantStatus:     http.StatusOK,
			wantListCalled: true,
		},
		{
			name:           "invalid tenant-bound cursor is bad request",
			listError:      apierrors.ErrBadRequest,
			wantStatus:     http.StatusBadRequest,
			wantListCalled: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listCalled := false
			handler := getAuthorizationGroupMembersHandlerWith(
				authorizationGroupMemberListDependencies{
					ensureParent: func(
						context.Context,
						uuid.UUID,
						uuid.UUID,
					) error {
						return tc.parentError
					},
					list: func(
						context.Context,
						types.AuthorizationListFilter,
					) (types.Page[types.PrincipalGroupMember], error) {
						listCalled = true
						return types.Page[types.PrincipalGroupMember]{
							Items: []types.PrincipalGroupMember{},
						}, tc.listError
					},
				},
			)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/"+tc.rawQuery, nil)
			request.SetPathValue("groupId", groupID.String())
			role := types.UserRoleAdmin
			request = request.WithContext(auth.Authentication.NewContext(
				request.Context(),
				channelTestAuth{
					orgID:  organizationID,
					userID: uuid.New(),
					role:   role,
				},
			))

			handler.ServeHTTP(recorder, request)

			g := NewWithT(t)
			g.Expect(recorder.Code).To(Equal(tc.wantStatus))
			g.Expect(listCalled).To(Equal(tc.wantListCalled))
		})
	}
}
