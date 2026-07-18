package handlers

import (
	"errors"
	"net/http"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/getsentry/sentry-go"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

func AdapterImplementationsRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Adapter Implementations"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", listAdapterImplementationsHandler()).
			With(option.Description("List versioned adapter implementations")).
			With(option.Request(api.DeploymentRegistryListRequest{})).
			With(option.Response(http.StatusOK, api.AdapterImplementationPage{}))

		r.With(
			middleware.RequireReadWriteOrAdmin,
			middleware.BlockSuperAdmin,
		).Post("/", createAdapterImplementationHandler()).
			With(option.Description("Register a versioned adapter implementation and capabilities")).
			With(option.Request(api.CreateAdapterImplementationRequest{})).
			With(option.Response(http.StatusOK, api.AdapterImplementation{})).
			With(option.Response(http.StatusBadRequest, api.ErrorResponse{})).
			With(option.Response(http.StatusConflict, api.ErrorResponse{}))
	})
}

func AdapterAssignmentsRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Adapter Assignments"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", listAdapterAssignmentsHandler()).
			With(option.Description("List target-scoped adapter assignments")).
			With(option.Request(api.DeploymentRegistryListRequest{})).
			With(option.Response(http.StatusOK, api.AdapterAssignmentPage{}))

		r.With(
			middleware.RequireReadWriteOrAdmin,
			middleware.BlockSuperAdmin,
		).Post("/", createAdapterAssignmentHandler()).
			With(option.Description("Assign an adapter version and immutable config snapshot")).
			With(option.Request(api.CreateAdapterAssignmentRequest{})).
			With(option.Response(http.StatusOK, api.AdapterAssignment{})).
			With(option.Response(http.StatusBadRequest, api.ErrorResponse{})).
			With(option.Response(http.StatusConflict, api.ErrorResponse{})).
			With(option.Response(http.StatusNotFound, api.ErrorResponse{}))
	})
}

func listAdapterImplementationsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		authentication := auth.Authentication.Require(ctx)
		request, ok := deploymentRegistryListRequestFromHTTP(w, r)
		if !ok {
			return
		}
		page, err := db.ListAdapterImplementationsPage(ctx, types.RegistryListFilter{
			OrganizationID: *authentication.CurrentOrgID(),
			Cursor:         request.Cursor, Limit: request.Limit,
		})
		if err != nil {
			handleAdapterError(w, r, internalctx.GetLogger(ctx), "list implementations", err)
			return
		}
		RespondJSON(w, api.AdapterImplementationPage{
			Items:      mapping.List(page.Items, mapping.AdapterImplementationToAPI),
			NextCursor: page.NextCursor,
		})
	}
}

func createAdapterImplementationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		authentication := auth.Authentication.Require(ctx)
		request, err := JsonBody[api.CreateAdapterImplementationRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		value, err := db.CreateAdapterImplementation(
			ctx,
			mapping.AdapterImplementationFromCreateRequest(*authentication.CurrentOrgID(), request),
		)
		if err != nil {
			handleAdapterError(w, r, internalctx.GetLogger(ctx), "create implementation", err)
			return
		}
		RespondJSON(w, mapping.AdapterImplementationToAPI(*value))
	}
}

func listAdapterAssignmentsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		authentication := auth.Authentication.Require(ctx)
		request, ok := deploymentRegistryListRequestFromHTTP(w, r)
		if !ok {
			return
		}
		page, err := db.ListAdapterAssignmentsPage(ctx, types.RegistryListFilter{
			OrganizationID: *authentication.CurrentOrgID(),
			Cursor:         request.Cursor, Limit: request.Limit,
		})
		if err != nil {
			handleAdapterError(w, r, internalctx.GetLogger(ctx), "list assignments", err)
			return
		}
		RespondJSON(w, api.AdapterAssignmentPage{
			Items:      mapping.List(page.Items, mapping.AdapterAssignmentToAPI),
			NextCursor: page.NextCursor,
		})
	}
}

func createAdapterAssignmentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		authentication := auth.Authentication.Require(ctx)
		request, err := JsonBody[api.CreateAdapterAssignmentRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		value, err := db.CreateAdapterAssignment(
			ctx,
			mapping.AdapterAssignmentFromCreateRequest(*authentication.CurrentOrgID(), request),
		)
		if err != nil {
			handleAdapterError(w, r, internalctx.GetLogger(ctx), "create assignment", err)
			return
		}
		RespondJSON(w, mapping.AdapterAssignmentToAPI(*value))
	}
}

func handleAdapterError(
	w http.ResponseWriter,
	r *http.Request,
	log *zap.Logger,
	action string,
	err error,
) {
	status, message := adapterPublicError(err)
	if status == http.StatusInternalServerError {
		log.Error("failed to "+action, zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
	}
	http.Error(w, message, status)
}

func adapterPublicError(err error) (int, string) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		return http.StatusNotFound, "adapter resource not found"
	case errors.Is(err, apierrors.ErrBadRequest):
		return http.StatusBadRequest, "adapter request is invalid"
	case errors.Is(err, apierrors.ErrConflict), errors.Is(err, apierrors.ErrAlreadyExists):
		return http.StatusConflict, "adapter resource conflicts with current state"
	case errors.Is(err, apierrors.ErrForbidden):
		return http.StatusForbidden, "adapter operation is forbidden"
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}
