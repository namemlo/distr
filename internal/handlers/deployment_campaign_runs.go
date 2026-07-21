package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
)

type CampaignRunService interface {
	StartCampaignRun(context.Context, types.CampaignRunStartInput) (*types.CampaignRun, error)
	GetCampaignRun(context.Context, uuid.UUID, uuid.UUID) (*types.CampaignRun, error)
	TransitionCampaignRun(context.Context, types.CampaignTransition) (*types.CampaignRun, error)
}

func DeploymentCampaignRunsRouter(r chiopenapi.Router) {
	deploymentCampaignRunsRouterWithFlags(r, db.CampaignRepository{}, env.ExperimentalFeatureFlags())
}

func deploymentCampaignRunsRouterWithFlags(
	r chiopenapi.Router,
	service CampaignRunService,
	enabledFlags []featureflags.Key,
) {
	featureGate := middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2)
	runtimeAuthorizer := newCampaignRuntimeAuthorizer()
	_ = enabledFlags // production middleware reads the configured registry; tests exercise handlers directly.
	r.With(middleware.RequireVendor, middleware.RequireOrgAndRole, featureGate).Group(func(r chiopenapi.Router) {
		r.With(
			middleware.RequireReadWriteOrAdmin,
			middleware.BlockSuperAdmin,
		).Post("/", startDeploymentCampaignRunHandler(service, runtimeAuthorizer)).
			With(option.Request(api.StartDeploymentCampaignRunRequest{})).
			With(option.Response(http.StatusCreated, api.DeploymentCampaignRun{}))
		r.Get("/{campaignRunId}", getDeploymentCampaignRunHandler(service)).
			With(option.Request(struct {
				CampaignRunID uuid.UUID `path:"campaignRunId"`
			}{})).
			With(option.Response(http.StatusOK, api.DeploymentCampaignRun{}))
		r.With(
			middleware.RequireReadWriteOrAdmin,
			middleware.BlockSuperAdmin,
		).Post("/{campaignRunId}/transitions", transitionDeploymentCampaignRunHandler(service, runtimeAuthorizer)).
			With(option.Request(api.TransitionDeploymentCampaignRunRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentCampaignRun{}))
	})
}

func startDeploymentCampaignRunHandler(
	service CampaignRunService,
	authorizer campaignRuntimeAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.StartDeploymentCampaignRunRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		organizationID := *authInfo.CurrentOrgID()
		if err := authorizer.AuthorizeCampaignRevision(
			r.Context(),
			organizationID,
			request.CampaignRevisionID,
		); writeCampaignControlError(w, r, err) {
			return
		}
		run, err := service.StartCampaignRun(r.Context(), types.CampaignRunStartInput{
			OrganizationID: organizationID, CampaignRevisionID: request.CampaignRevisionID,
			ActorID: authInfo.CurrentUserID(), StartedAt: time.Now().UTC(),
		})
		if writeCampaignControlError(w, r, err) {
			return
		}
		RespondJSONWithStatus(w, http.StatusCreated, mapping.DeploymentCampaignRunToAPI(*run))
	}
}

func getDeploymentCampaignRunHandler(service CampaignRunService) http.HandlerFunc {
	return GetDeploymentCampaignRunHandler(func(r *http.Request, runID uuid.UUID) (*types.CampaignRun, error) {
		authInfo := auth.Authentication.Require(r.Context())
		return service.GetCampaignRun(r.Context(), runID, *authInfo.CurrentOrgID())
	})
}

func transitionDeploymentCampaignRunHandler(
	service CampaignRunService,
	authorizer campaignRuntimeAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID, err := uuid.Parse(r.PathValue("campaignRunId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		request, err := JsonBody[api.TransitionDeploymentCampaignRunRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		organizationID := *authInfo.CurrentOrgID()
		if err := authorizer.AuthorizeCampaignRun(
			r.Context(),
			organizationID,
			runID,
		); writeCampaignControlError(w, r, err) {
			return
		}
		actorID := authInfo.CurrentUserID()
		run, err := service.TransitionCampaignRun(r.Context(), types.CampaignTransition{
			RunID: runID, OrganizationID: organizationID,
			ExpectedVersion: request.ExpectedVersion, To: request.To,
			Reason: request.Reason, ActorID: &actorID, At: time.Now().UTC(),
		})
		if writeCampaignControlError(w, r, err) {
			return
		}
		RespondJSON(w, mapping.DeploymentCampaignRunToAPI(*run))
	}
}

type CampaignRunLoader func(*http.Request, uuid.UUID) (*types.CampaignRun, error)

func GetDeploymentCampaignRunHandler(load CampaignRunLoader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID, err := uuid.Parse(r.PathValue("campaignRunId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		run, err := load(r, runID)
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, mapping.DeploymentCampaignRunToAPI(*run))
	}
}
