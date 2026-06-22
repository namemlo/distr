package handlers

import (
	"net/http"
	"strconv"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
)

func DeploymentTimelineRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Deployment Timeline"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		deploymentTimelineFeatureFlagMiddleware,
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getDeploymentTimelineHandler()).
			With(option.Description("List deployment timeline entries")).
			With(option.Request(api.DeploymentTimelineQueryRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentTimeline{}))

		r.Get("/compare", compareDeploymentTimelineHandler()).
			With(option.Description("Compare two deployment timeline entries")).
			With(option.Request(api.DeploymentTimelineCompareQueryRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentTimelineComparison{}))

		r.Route("/{taskId}", func(r chiopenapi.Router) {
			type DeploymentTimelineTaskIDRequest struct {
				TaskID uuid.UUID `path:"taskId"`
			}

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
				Post("/redeploy", redeployDeploymentTimelineTaskHandler()).
				With(option.Description("Create a deployment plan for the same release and target as a timeline entry")).
				With(option.Request(DeploymentTimelineTaskIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentTimelineRedeploy{}))
		})
	})
}

func deploymentTimelineFeatureFlagMiddleware(handler http.Handler) http.Handler {
	for _, feature := range []featureflags.Key{
		featureflags.KeyDeploymentTimeline,
		featureflags.KeyTaskQueue,
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

func getDeploymentTimelineHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		query, ok := deploymentTimelineQueryFromRequest(w, r, *auth.CurrentOrgID())
		if !ok {
			return
		}
		timeline, err := db.GetDeploymentTimeline(ctx, query)
		respondTaskQueueResult(w, r, log, err, func() {
			RespondJSON(w, mapping.DeploymentTimelineToAPI(*timeline))
		})
	}
}

func compareDeploymentTimelineHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		baseTaskID, ok := requiredUUIDQueryParam(w, r, "baseTaskId")
		if !ok {
			return
		}
		compareTaskID, ok := requiredUUIDQueryParam(w, r, "compareTaskId")
		if !ok {
			return
		}
		comparison, err := db.CompareDeploymentTimelineTasks(ctx, types.DeploymentTimelineCompareRequest{
			OrganizationID: *auth.CurrentOrgID(),
			BaseTaskID:     baseTaskID,
			CompareTaskID:  compareTaskID,
		})
		respondTaskQueueResult(w, r, log, err, func() {
			RespondJSON(w, mapping.DeploymentTimelineComparisonToAPI(*comparison))
		})
	}
}

func redeployDeploymentTimelineTaskHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID, err := uuid.Parse(r.PathValue("taskId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		redeploy, err := db.CreateDeploymentPlanFromTimelineTask(ctx, types.CreateDeploymentTimelineRedeployRequest{
			OrganizationID:     *auth.CurrentOrgID(),
			TaskID:             taskID,
			ActorUserAccountID: auth.CurrentUserID(),
		})
		respondTaskQueueResult(w, r, log, err, func() {
			RespondJSON(w, mapping.DeploymentTimelineRedeployToAPI(*redeploy))
		})
	}
}

func deploymentTimelineQueryFromRequest(
	w http.ResponseWriter,
	r *http.Request,
	orgID uuid.UUID,
) (types.DeploymentTimelineQuery, bool) {
	query := types.DeploymentTimelineQuery{
		OrganizationID:      orgID,
		IncludeNonTerminal:  true,
		IncludeRedeployInfo: true,
	}
	var ok bool
	if query.ApplicationID, ok = optionalUUIDQueryParam(w, r, "applicationId"); !ok {
		return query, false
	}
	if query.ReleaseBundleID, ok = optionalUUIDQueryParam(w, r, "releaseBundleId"); !ok {
		return query, false
	}
	if query.EnvironmentID, ok = optionalUUIDQueryParam(w, r, "environmentId"); !ok {
		return query, false
	}
	if query.DeploymentTargetID, ok = optionalUUIDQueryParam(w, r, "deploymentTargetId"); !ok {
		return query, false
	}
	if query.CustomerOrganizationID, ok = optionalUUIDQueryParam(w, r, "customerOrganizationId"); !ok {
		return query, false
	}
	if limit, ok := optionalIntQueryParam(w, r, "limit"); ok {
		query.Limit = limit
	} else {
		return query, false
	}
	if include, ok := optionalBoolQueryParam(w, r, "includeNonTerminal"); ok {
		query.IncludeNonTerminal = include
	} else {
		return query, false
	}
	return query, true
}

func requiredUUIDQueryParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	value := r.URL.Query().Get(name)
	if value == "" {
		http.Error(w, name+" is required", http.StatusBadRequest)
		return uuid.Nil, false
	}
	id, err := uuid.Parse(value)
	if err != nil {
		http.Error(w, name+" is invalid", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}

func optionalUUIDQueryParam(w http.ResponseWriter, r *http.Request, name string) (*uuid.UUID, bool) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return nil, true
	}
	id, err := uuid.Parse(value)
	if err != nil {
		http.Error(w, name+" is invalid", http.StatusBadRequest)
		return nil, false
	}
	return &id, true
}

func optionalIntQueryParam(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		http.Error(w, name+" is invalid", http.StatusBadRequest)
		return 0, false
	}
	return parsed, true
}

func optionalBoolQueryParam(w http.ResponseWriter, r *http.Request, name string) (bool, bool) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return true, true
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		http.Error(w, name+" is invalid", http.StatusBadRequest)
		return false, false
	}
	return parsed, true
}
