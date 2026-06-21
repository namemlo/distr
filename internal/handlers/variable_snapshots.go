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
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

func VariableSnapshotsRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Variable Snapshots"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		variableSnapshotsFeatureFlagMiddleware,
	).Group(func(r chiopenapi.Router) {
		r.Route("/{variableSnapshotId}", func(r chiopenapi.Router) {
			type VariableSnapshotIDRequest struct {
				VariableSnapshotID uuid.UUID `path:"variableSnapshotId"`
			}

			r.Get("/", getVariableSnapshotHandler()).
				With(option.Description("Get an immutable variable snapshot")).
				With(option.Request(VariableSnapshotIDRequest{})).
				With(option.Response(http.StatusOK, api.VariableSnapshot{}))
		})
	})
}

func variableSnapshotsFeatureFlagMiddleware(handler http.Handler) http.Handler {
	for _, feature := range []featureflags.Key{
		featureflags.KeyReleaseBundles,
		featureflags.KeyScopedVariablesV2,
	} {
		handler = middleware.ExperimentalFeatureFlagMiddleware(feature)(handler)
	}
	return handler
}

//nolint:dupl
func getVariableSnapshotHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("variableSnapshotId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		snapshot, err := db.GetVariableSnapshot(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get variable snapshot", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.VariableSnapshotToAPI(*snapshot))
		}
	}
}

func getDeploymentConfigurationDriftHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deploymentID, err := uuid.Parse(r.PathValue("deploymentId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		drift, err := db.GetDeploymentConfigurationDrift(ctx, deploymentID, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if errors.Is(err, apierrors.ErrBadRequest) {
			http.Error(w, "invalid configuration drift request", http.StatusBadRequest)
		} else if err != nil {
			log.Error("failed to get deployment configuration drift", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.ConfigurationDriftToAPI(drift))
		}
	}
}
