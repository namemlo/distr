package handlers

import (
	"errors"
	"net/http"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
)

func AuthorizationRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Scoped Authorization"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.BlockSuperAdmin,
		middleware.RequireControlPlaneAction(
			types.ActionAuthorizationManage,
			middleware.OrganizationResourceRef,
		),
	).Group(func(r chiopenapi.Router) {
		r.Get("/roles", getAuthorizationRolesHandler()).
			With(option.Description("List immutable scoped authorization roles")).
			With(option.Response(http.StatusOK, api.AuthorizationRoleListResponse{})).
			With(option.Response(http.StatusForbidden, "")).
			With(option.Response(http.StatusNotFound, ""))
		r.Post("/roles", createAuthorizationRoleHandler()).
			With(option.Description("Create an immutable scoped authorization role")).
			With(option.Request(api.CreateAuthorizationRoleRequest{})).
			With(option.Response(http.StatusCreated, api.AuthorizationRole{})).
			With(option.Response(http.StatusBadRequest, "")).
			With(option.Response(http.StatusForbidden, "")).
			With(option.Response(http.StatusNotFound, "")).
			With(option.Response(http.StatusConflict, ""))

		r.Get("/bindings", getAuthorizationRoleBindingsHandler()).
			With(option.Description("List immutable scoped role bindings")).
			With(option.Response(http.StatusOK, api.AuthorizationRoleBindingListResponse{})).
			With(option.Response(http.StatusForbidden, "")).
			With(option.Response(http.StatusNotFound, ""))
		r.Post("/bindings", createAuthorizationRoleBindingHandler()).
			With(option.Description("Create an immutable scoped role binding")).
			With(option.Request(api.CreateAuthorizationRoleBindingRequest{})).
			With(option.Response(http.StatusCreated, api.AuthorizationRoleBinding{})).
			With(option.Response(http.StatusBadRequest, "")).
			With(option.Response(http.StatusForbidden, "")).
			With(option.Response(http.StatusNotFound, "")).
			With(option.Response(http.StatusConflict, ""))

		r.Get("/groups", getAuthorizationGroupsHandler()).
			With(option.Description("List authorization principal groups")).
			With(option.Response(http.StatusOK, api.AuthorizationPrincipalGroupListResponse{})).
			With(option.Response(http.StatusForbidden, "")).
			With(option.Response(http.StatusNotFound, ""))
		r.Post("/groups", createAuthorizationGroupHandler()).
			With(option.Description("Create an authorization principal group")).
			With(option.Request(api.CreateAuthorizationPrincipalGroupRequest{})).
			With(option.Response(http.StatusCreated, api.AuthorizationPrincipalGroup{})).
			With(option.Response(http.StatusBadRequest, "")).
			With(option.Response(http.StatusForbidden, "")).
			With(option.Response(http.StatusNotFound, "")).
			With(option.Response(http.StatusConflict, ""))

		r.Route("/groups/{groupId}/members", func(r chiopenapi.Router) {
			type groupIDRequest struct {
				GroupID uuid.UUID `path:"groupId"`
			}
			r.Get("/", getAuthorizationGroupMembersHandler()).
				With(option.Description("List effective-interval group memberships")).
				With(option.Request(groupIDRequest{})).
				With(option.Response(
					http.StatusOK,
					api.AuthorizationPrincipalGroupMemberListResponse{},
				)).
				With(option.Response(http.StatusForbidden, "")).
				With(option.Response(http.StatusNotFound, ""))
			r.Post("/", addAuthorizationGroupMemberHandler()).
				With(option.Description("Add an immutable effective-interval group membership")).
				With(option.Request(struct {
					groupIDRequest
					api.AddAuthorizationPrincipalGroupMemberRequest
				}{})).
				With(option.Response(
					http.StatusCreated,
					api.AuthorizationPrincipalGroupMember{},
				)).
				With(option.Response(http.StatusBadRequest, "")).
				With(option.Response(http.StatusForbidden, "")).
				With(option.Response(http.StatusNotFound, "")).
				With(option.Response(http.StatusConflict, ""))
		})

		r.Get("/control-plane-enrollments", getControlPlaneEnrollmentsHandler()).
			With(option.Description("List organization and environment control-plane enrollments")).
			With(option.Response(http.StatusOK, api.ControlPlaneEnrollmentListResponse{})).
			With(option.Response(http.StatusForbidden, "")).
			With(option.Response(http.StatusNotFound, ""))
		r.Post("/control-plane-enrollments", createControlPlaneEnrollmentHandler()).
			With(option.Description("Append a control-plane enrollment revision")).
			With(option.Request(api.CreateControlPlaneEnrollmentRequest{})).
			With(option.Response(http.StatusCreated, api.ControlPlaneEnrollment{})).
			With(option.Response(http.StatusBadRequest, "")).
			With(option.Response(http.StatusForbidden, "")).
			With(option.Response(http.StatusNotFound, "")).
			With(option.Response(http.StatusConflict, ""))
	})
}

func getAuthorizationRolesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authInfo := auth.Authentication.Require(r.Context())
		organizationID := *authInfo.CurrentOrgID()
		if err := db.BackfillBuiltInAuthorization(r.Context(), organizationID); err != nil {
			handleAuthorizationWriteError(w, r, err)
			return
		}
		roles, err := db.ListAuthorizationRoleDefinitions(r.Context(), organizationID)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response := make([]api.AuthorizationRole, 0, len(roles))
		for _, role := range roles {
			response = append(response, authorizationRoleResponse(role))
		}
		RespondJSON(w, api.AuthorizationRoleListResponse{Roles: response})
	}
}

func createAuthorizationRoleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.CreateAuthorizationRoleRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		actorID := authInfo.CurrentUserID()
		role := types.RoleDefinition{
			OrganizationID:  *authInfo.CurrentOrgID(),
			Key:             request.Key,
			DisplayName:     request.DisplayName,
			Description:     request.Description,
			CreatedByUserID: &actorID,
			Permissions:     append([]types.Action{}, request.Permissions...),
		}
		if err := db.CreateAuthorizationRoleDefinition(r.Context(), &role); err != nil {
			handleAuthorizationWriteError(w, r, err)
			return
		}
		RespondJSONWithStatus(w, http.StatusCreated, authorizationRoleResponse(role))
	}
}

func getAuthorizationRoleBindingsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authInfo := auth.Authentication.Require(r.Context())
		bindings, err := db.ListAuthorizationRoleBindings(
			r.Context(),
			*authInfo.CurrentOrgID(),
		)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response := make([]api.AuthorizationRoleBinding, 0, len(bindings))
		for _, binding := range bindings {
			response = append(response, authorizationBindingResponse(binding))
		}
		RespondJSON(w, api.AuthorizationRoleBindingListResponse{Bindings: response})
	}
}

func createAuthorizationRoleBindingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.CreateAuthorizationRoleBindingRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		actorID := authInfo.CurrentUserID()
		binding := types.RoleBinding{
			OrganizationID:   *authInfo.CurrentOrgID(),
			RoleDefinitionID: request.RoleDefinitionID,
			PrincipalKind:    request.PrincipalKind,
			PrincipalID:      request.PrincipalID,
			Scope:            request.Scope,
			EffectiveFrom:    request.EffectiveFrom,
			EffectiveUntil:   request.EffectiveUntil,
			Reason:           request.Reason,
			CreatedByUserID:  &actorID,
		}
		if err := db.CreateAuthorizationRoleBinding(r.Context(), &binding); err != nil {
			handleAuthorizationWriteError(w, r, err)
			return
		}
		RespondJSONWithStatus(
			w,
			http.StatusCreated,
			authorizationBindingResponse(binding),
		)
	}
}

func getAuthorizationGroupsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authInfo := auth.Authentication.Require(r.Context())
		groups, err := db.ListAuthorizationPrincipalGroups(
			r.Context(),
			*authInfo.CurrentOrgID(),
		)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response := make([]api.AuthorizationPrincipalGroup, 0, len(groups))
		for _, group := range groups {
			response = append(response, authorizationGroupResponse(group))
		}
		RespondJSON(w, api.AuthorizationPrincipalGroupListResponse{Groups: response})
	}
}

func createAuthorizationGroupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.CreateAuthorizationPrincipalGroupRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		actorID := authInfo.CurrentUserID()
		group := types.PrincipalGroup{
			OrganizationID:  *authInfo.CurrentOrgID(),
			Key:             request.Key,
			DisplayName:     request.DisplayName,
			Description:     request.Description,
			CreatedByUserID: &actorID,
		}
		if err := db.CreateAuthorizationPrincipalGroup(r.Context(), &group); err != nil {
			handleAuthorizationWriteError(w, r, err)
			return
		}
		RespondJSONWithStatus(w, http.StatusCreated, authorizationGroupResponse(group))
	}
}

func getAuthorizationGroupMembersHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID, err := uuid.Parse(r.PathValue("groupId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		members, err := db.ListAuthorizationPrincipalGroupMembers(
			r.Context(),
			*authInfo.CurrentOrgID(),
			groupID,
		)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response := make([]api.AuthorizationPrincipalGroupMember, 0, len(members))
		for _, member := range members {
			response = append(response, authorizationGroupMemberResponse(member))
		}
		RespondJSON(w, api.AuthorizationPrincipalGroupMemberListResponse{Members: response})
	}
}

func addAuthorizationGroupMemberHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID, err := uuid.Parse(r.PathValue("groupId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		request, err := JsonBody[api.AddAuthorizationPrincipalGroupMemberRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		actorID := authInfo.CurrentUserID()
		member := types.PrincipalGroupMember{
			OrganizationID: *authInfo.CurrentOrgID(),
			GroupID:        groupID,
			UserAccountID:  request.UserAccountID,
			EffectiveFrom:  request.EffectiveFrom,
			EffectiveUntil: request.EffectiveUntil,
			AddedByUserID:  &actorID,
			Reason:         request.Reason,
		}
		if err := db.AddAuthorizationPrincipalGroupMember(r.Context(), &member); err != nil {
			handleAuthorizationWriteError(w, r, err)
			return
		}
		RespondJSONWithStatus(
			w,
			http.StatusCreated,
			authorizationGroupMemberResponse(member),
		)
	}
}

func getControlPlaneEnrollmentsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authInfo := auth.Authentication.Require(r.Context())
		enrollments, err := db.ListControlPlaneEnrollments(
			r.Context(),
			*authInfo.CurrentOrgID(),
		)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response := make([]api.ControlPlaneEnrollment, 0, len(enrollments))
		for _, enrollment := range enrollments {
			response = append(response, controlPlaneEnrollmentResponse(enrollment))
		}
		RespondJSON(w, api.ControlPlaneEnrollmentListResponse{Enrollments: response})
	}
}

func createControlPlaneEnrollmentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.CreateControlPlaneEnrollmentRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		enrollment := types.ControlPlaneEnrollment{
			OrganizationID: *authInfo.CurrentOrgID(),
			Scope:          request.Scope,
			Enabled:        request.Enabled,
			EffectiveFrom:  request.EffectiveFrom,
			EffectiveUntil: request.EffectiveUntil,
			ActorUserID:    authInfo.CurrentUserID(),
			Reason:         request.Reason,
		}
		if err := db.CreateControlPlaneEnrollment(r.Context(), &enrollment); err != nil {
			handleAuthorizationWriteError(w, r, err)
			return
		}
		RespondJSONWithStatus(
			w,
			http.StatusCreated,
			controlPlaneEnrollmentResponse(enrollment),
		)
	}
}

func authorizationRoleResponse(role types.RoleDefinition) api.AuthorizationRole {
	return api.AuthorizationRole{
		ID:                     role.ID,
		CreatedAt:              role.CreatedAt,
		Key:                    role.Key,
		DisplayName:            role.DisplayName,
		Description:            role.Description,
		BuiltIn:                role.BuiltIn,
		SourceLegacyRole:       role.SourceLegacyRole,
		Revision:               role.Revision,
		CreatedByUserAccountID: role.CreatedByUserID,
		Permissions:            append([]types.Action{}, role.Permissions...),
	}
}

func authorizationBindingResponse(
	binding types.RoleBinding,
) api.AuthorizationRoleBinding {
	return api.AuthorizationRoleBinding{
		ID:                     binding.ID,
		CreatedAt:              binding.CreatedAt,
		RoleDefinitionID:       binding.RoleDefinitionID,
		PrincipalKind:          binding.PrincipalKind,
		PrincipalID:            binding.PrincipalID,
		Scope:                  binding.Scope,
		EffectiveFrom:          binding.EffectiveFrom,
		EffectiveUntil:         binding.EffectiveUntil,
		Reason:                 binding.Reason,
		Revision:               binding.Revision,
		CreatedByUserAccountID: binding.CreatedByUserID,
		Source:                 binding.Source,
	}
}

func authorizationGroupResponse(
	group types.PrincipalGroup,
) api.AuthorizationPrincipalGroup {
	return api.AuthorizationPrincipalGroup{
		ID:                     group.ID,
		CreatedAt:              group.CreatedAt,
		Key:                    group.Key,
		DisplayName:            group.DisplayName,
		Description:            group.Description,
		CreatedByUserAccountID: group.CreatedByUserID,
	}
}

func authorizationGroupMemberResponse(
	member types.PrincipalGroupMember,
) api.AuthorizationPrincipalGroupMember {
	return api.AuthorizationPrincipalGroupMember{
		ID:                   member.ID,
		CreatedAt:            member.CreatedAt,
		GroupID:              member.GroupID,
		UserAccountID:        member.UserAccountID,
		EffectiveFrom:        member.EffectiveFrom,
		EffectiveUntil:       member.EffectiveUntil,
		AddedByUserAccountID: member.AddedByUserID,
		Reason:               member.Reason,
	}
}

func controlPlaneEnrollmentResponse(
	enrollment types.ControlPlaneEnrollment,
) api.ControlPlaneEnrollment {
	return api.ControlPlaneEnrollment{
		ID:                 enrollment.ID,
		CreatedAt:          enrollment.CreatedAt,
		Scope:              enrollment.Scope,
		Enabled:            enrollment.Enabled,
		EffectiveFrom:      enrollment.EffectiveFrom,
		EffectiveUntil:     enrollment.EffectiveUntil,
		ActorUserAccountID: enrollment.ActorUserID,
		Reason:             enrollment.Reason,
		Revision:           enrollment.Revision,
	}
}

func handleAuthorizationWriteError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, apierrors.ErrAlreadyExists),
		errors.Is(err, apierrors.ErrConflict):
		http.Error(w, http.StatusText(http.StatusConflict), http.StatusConflict)
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
	default:
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
	}
}
