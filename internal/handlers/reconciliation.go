package handlers

import (
	"errors"
	"net/http"
	"time"

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
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

func DriftCasesRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Drift Cases"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", listDriftCasesHandler()).
			With(option.Description("List independent observed-state drift cases")).
			With(option.Response(http.StatusOK, []api.DriftCase{}))

		type driftCaseIDRequest struct {
			DriftCaseID uuid.UUID `path:"driftCaseId"`
		}
		type resolveDriftCaseRequest struct {
			driftCaseIDRequest
			api.ReconciliationDecisionRequest
		}
		r.Route("/{driftCaseId}", func(r chiopenapi.Router) {
			r.With(
				middleware.RequireReadWriteOrAdmin,
				middleware.BlockSuperAdmin,
			).Post("/resolve", resolveDriftCaseHandler()).
				With(option.Description("Record an approved reconciliation action")).
				With(option.Request(resolveDriftCaseRequest{})).
				With(option.Response(http.StatusNoContent, nil)).
				With(option.Response(http.StatusConflict, api.ErrorResponse{}))
		})
	})
}

func ReconciliationActionsRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Reconciliation Actions"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
	).Get("/", listReconciliationActionsHandler()).
		With(option.Description("List approved reconciliation actions")).
		With(option.Response(http.StatusOK, []api.ReconciliationAction{}))
}

func listDriftCasesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authentication := auth.Authentication.Require(r.Context())
		items, err := db.ListDriftCases(r.Context(), *authentication.CurrentOrgID())
		if err != nil {
			handleReconciliationError(w, r, "list drift cases", err)
			return
		}
		RespondJSON(w, mapping.List(items, mapping.DriftCaseToAPI))
	}
}

func resolveDriftCaseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		driftCaseID, err := uuid.Parse(r.PathValue("driftCaseId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		request, err := JsonBody[api.ReconciliationDecisionRequest](w, r)
		if err != nil {
			return
		}
		now := time.Now().UTC()
		if err := request.Validate(now); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authentication := auth.Authentication.Require(r.Context())
		err = db.ResolveDriftCase(r.Context(), types.ReconciliationDecision{
			OrganizationID: *authentication.CurrentOrgID(), DriftCaseID: driftCaseID,
			Action: request.Action, Reason: request.Reason,
			ActorID:          authentication.CurrentUserID(),
			DeploymentPlanID: request.DeploymentPlanID,
			AcceptedUntil:    request.AcceptedUntil,
		})
		if err != nil {
			handleReconciliationError(w, r, "resolve drift case", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func listReconciliationActionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authentication := auth.Authentication.Require(r.Context())
		items, err := db.ListReconciliationActions(
			r.Context(), *authentication.CurrentOrgID(),
		)
		if err != nil {
			handleReconciliationError(w, r, "list actions", err)
			return
		}
		RespondJSON(w, mapping.List(items, mapping.ReconciliationActionToAPI))
	}
}

func handleReconciliationError(
	w http.ResponseWriter,
	r *http.Request,
	action string,
	err error,
) {
	status, message := reconciliationPublicError(err)
	if status == http.StatusInternalServerError {
		internalctx.GetLogger(r.Context()).Error(
			"failed to "+action,
			zap.Error(err),
		)
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
	}
	http.Error(w, message, status)
}

func reconciliationPublicError(err error) (int, string) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		return http.StatusNotFound, "reconciliation resource not found"
	case errors.Is(err, apierrors.ErrBadRequest):
		return http.StatusBadRequest, "reconciliation request is invalid"
	case errors.Is(err, apierrors.ErrForbidden):
		return http.StatusForbidden, "reconciliation operation is forbidden"
	case errors.Is(err, apierrors.ErrConflict):
		return http.StatusConflict, "reconciliation conflicts with current case state"
	default:
		return http.StatusInternalServerError, "reconciliation operation failed"
	}
}
