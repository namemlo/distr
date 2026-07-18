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
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

func DeploymentPlansRouter(r chiopenapi.Router) {
	DeploymentPlansRouterWithVerifier(db.NewUnavailableTargetConfigObjectVerifier())(r)
}

func DeploymentPlansRouterWithVerifier(
	verifier db.TargetConfigObjectVerifier,
) func(chiopenapi.Router) {
	if verifier == nil {
		verifier = db.NewUnavailableTargetConfigObjectVerifier()
	}
	return func(r chiopenapi.Router) {
		deploymentPlansRouterWithVerifier(r, verifier)
	}
}

func deploymentPlansRouterWithVerifier(
	r chiopenapi.Router,
	verifier db.TargetConfigObjectVerifier,
) {
	r.WithOptions(option.GroupTags("Deployment Plans"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		deploymentPlansFeatureFlagMiddleware,
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getDeploymentPlansHandler()).
			With(option.Description("List deployment plans")).
			With(option.Response(http.StatusOK, []api.DeploymentPlan{}))

		r.Route("/{deploymentPlanId}", func(r chiopenapi.Router) {
			type DeploymentPlanIDRequest struct {
				DeploymentPlanID uuid.UUID `path:"deploymentPlanId"`
			}

			r.Get("/", getDeploymentPlanHandler()).
				With(option.Description("Get a deployment plan")).
				With(option.Request(DeploymentPlanIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentPlan{}))

			type CreateTasksForDeploymentPlanRouteRequest struct {
				DeploymentPlanIDRequest
				api.CreateTasksForDeploymentPlanRequest
			}

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin, taskQueueFeatureFlagMiddleware).
				Post("/tasks", createTasksForDeploymentPlanHandler()).
				With(option.Description("Create durable tasks for a ready deployment plan")).
				With(option.Request(CreateTasksForDeploymentPlanRouteRequest{})).
				With(option.Response(http.StatusOK, []api.Task{}))

			type CreatePreviousStateDeploymentPlanRouteRequest struct {
				DeploymentPlanIDRequest
				api.CreatePreviousStateDeploymentPlanRequest
			}

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
				Post("/previous-state", createPreviousStateDeploymentPlanHandler(verifier)).
				With(option.Description("Create a new immutable plan for a previously successful state")).
				With(option.Request(CreatePreviousStateDeploymentPlanRouteRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentPlan{}))
		})

		r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
			Post("/", createDeploymentPlanHandler()).
			With(option.Description("Create a resolved deployment plan preview")).
			With(option.Request(api.CreateDeploymentPlanRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentPlan{}))
	})
}

func deploymentPlansFeatureFlagMiddleware(handler http.Handler) http.Handler {
	for _, feature := range []featureflags.Key{
		featureflags.KeyDeploymentPlans,
		featureflags.KeyReleaseBundles,
		featureflags.KeyDeploymentProcesses,
		featureflags.KeyScopedVariablesV2,
		featureflags.KeyChannels,
		featureflags.KeyLifecycles,
		featureflags.KeyEnvironments,
	} {
		handler = middleware.ExperimentalFeatureFlagMiddleware(feature)(handler)
	}
	return handler
}

func getDeploymentPlansHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		plans, err := db.GetDeploymentPlansByOrganizationID(ctx, *auth.CurrentOrgID())
		if err != nil {
			log.Error("failed to get deployment plans", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, mapping.List(plans, mapping.DeploymentPlanToAPI))
	}
}

//nolint:dupl
func getDeploymentPlanHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("deploymentPlanId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		plan, err := db.GetDeploymentPlan(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get deployment plan", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.DeploymentPlanToAPI(*plan))
		}
	}
}

func createDeploymentPlanHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateDeploymentPlanRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if request.DeploymentUnitID != nil {
			scopedAuthorizationStackPresent, probeErr := pr066ScopedAuthorizationSchemaPresent(ctx)
			if probeErr != nil || scopedAuthorizationStackPresent {
				// Creating a v2 plan freezes policy evidence and is itself a
				// policy-managed operation. A PR-066 stack must authorize it
				// through policy.manage plus effective enrollment.
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
		}

		plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
			OrganizationID:   *auth.CurrentOrgID(),
			ReleaseBundleID:  request.ReleaseBundleID,
			EnvironmentID:    request.EnvironmentID,
			TargetIDs:        request.TargetIDs,
			DeploymentUnitID: request.DeploymentUnitID,
		})
		if errors.Is(err, apierrors.ErrBadRequest) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to create deployment plan", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.DeploymentPlanToAPI(*plan))
		}
	}
}

func createPreviousStateDeploymentPlanHandler(
	verifier db.TargetConfigObjectVerifier,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentPlanID, err := uuid.Parse(r.PathValue("deploymentPlanId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		request, err := JsonBody[api.CreatePreviousStateDeploymentPlanRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		authentication := auth.Authentication.Require(ctx)
		plan, err := db.CreatePreviousStatePlanForOrganization(
			ctx,
			*authentication.CurrentOrgID(),
			authentication.CurrentUserID(),
			currentPlanID,
			request.SuccessfulDeploymentPlanID,
			request.Reason,
			verifier,
		)
		switch {
		case errors.Is(err, apierrors.ErrNotFound):
			http.NotFound(w, r)
		case errors.Is(err, apierrors.ErrBadRequest):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, apierrors.ErrConflict), errors.Is(err, apierrors.ErrAlreadyExists):
			http.Error(w, err.Error(), http.StatusConflict)
		case err != nil:
			log.Error("failed to create previous-state deployment plan", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		default:
			RespondJSON(w, mapping.DeploymentPlanToAPI(*plan))
		}
	}
}
