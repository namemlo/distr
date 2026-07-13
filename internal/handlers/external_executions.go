package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

func ExternalExecutionsRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("External Executions"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		taskQueueFeatureFlagMiddleware,
	).Group(func(r chiopenapi.Router) {
		type ExternalExecutionIDRequest struct {
			ExternalExecutionID uuid.UUID `path:"externalExecutionId"`
		}

		r.Route("/{externalExecutionId}", func(r chiopenapi.Router) {
			r.Get("/", getExternalExecutionHandler()).
				With(option.Description("Get an external execution and its callback history")).
				With(option.Request(ExternalExecutionIDRequest{})).
				With(option.Response(http.StatusOK, api.ExternalExecution{}))

			type ExternalExecutionCallbackRouteRequest struct {
				ExternalExecutionIDRequest
				api.ExternalExecutionCallbackRequest
			}
			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
				Post("/callbacks", recordExternalExecutionCallbackHandler()).
				With(option.Description("Record an authenticated external execution callback")).
				With(option.Request(ExternalExecutionCallbackRouteRequest{})).
				With(option.Response(http.StatusOK, api.ExternalExecution{}))
		})
	})
}

func getExternalExecutionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("externalExecutionId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ctx := r.Context()
		organizationID := *auth.Authentication.Require(ctx).CurrentOrgID()
		execution, err := db.GetExternalExecution(ctx, id, organizationID)
		if err != nil {
			logExternalExecutionError(ctx, "failed to get external execution", err)
			respondExternalExecutionError(w, err)
			return
		}
		events, err := db.GetExternalExecutionEvents(ctx, id, organizationID)
		if err != nil {
			logExternalExecutionError(ctx, "failed to get external execution events", err)
			respondExternalExecutionError(w, err)
			return
		}
		response := mapping.ExternalExecutionToAPI(*execution)
		response.Events = mapping.List(events, mapping.ExternalExecutionEventToAPI)
		RespondJSON(w, response)
	}
}

func recordExternalExecutionCallbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("externalExecutionId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		request, err := JsonBody[api.ExternalExecutionCallbackRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		organizationID := *auth.Authentication.Require(ctx).CurrentOrgID()
		execution, err := db.RecordExternalExecutionCallback(ctx, request.ToTypes(organizationID, id))
		if err != nil {
			logExternalExecutionError(ctx, "failed to record external execution callback", err)
			respondExternalExecutionError(w, err)
			return
		}
		RespondJSON(w, mapping.ExternalExecutionToAPI(*execution))
	}
}

func logExternalExecutionError(ctx context.Context, message string, err error) {
	if errors.Is(err, apierrors.ErrNotFound) || errors.Is(err, apierrors.ErrBadRequest) ||
		errors.Is(err, apierrors.ErrConflict) {
		return
	}
	log := internalctx.GetLogger(ctx)
	log.Error(message, zap.Error(err))
	sentry.GetHubFromContext(ctx).CaptureException(err)
}

func respondExternalExecutionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, apierrors.ErrConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
