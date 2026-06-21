package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

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

func TasksRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Tasks"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		taskQueueFeatureFlagMiddleware,
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getTasksHandler()).
			With(option.Description("List durable tasks in queue order")).
			With(option.Response(http.StatusOK, []api.Task{}))

		r.Route("/{taskId}", func(r chiopenapi.Router) {
			type TaskIDRequest struct {
				TaskID uuid.UUID `path:"taskId"`
			}

			r.Get("/", getTaskHandler()).
				With(option.Description("Get a durable task")).
				With(option.Request(TaskIDRequest{})).
				With(option.Response(http.StatusOK, api.Task{}))

			type TransitionTaskStateRouteRequest struct {
				TaskIDRequest
				api.TransitionTaskStateRequest
			}

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
				Post("/state", transitionTaskStateHandler()).
				With(option.Description("Transition durable task state without executing deployment actions")).
				With(option.Request(TransitionTaskStateRouteRequest{})).
				With(option.Response(http.StatusOK, api.Task{}))
		})
	})
}

func taskQueueFeatureFlagMiddleware(handler http.Handler) http.Handler {
	for _, feature := range []featureflags.Key{
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

func createTasksForDeploymentPlanHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deploymentPlanID, err := uuid.Parse(r.PathValue("deploymentPlanId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := optionalCreateTasksRequest(w, r)
		if err != nil {
			return
		}
		tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
			OrganizationID:      *auth.CurrentOrgID(),
			DeploymentPlanID:    deploymentPlanID,
			ActorUserAccountID:  auth.CurrentUserID(),
			ConcurrencyPolicy:   request.ConcurrencyPolicy,
			AdditionalResources: taskLockResourcesFromAPI(request.LockResources),
		})
		respondTaskQueueResult(w, r, log, err, func() {
			RespondJSON(w, mapping.List(tasks, mapping.TaskToAPI))
		})
	}
}

func optionalCreateTasksRequest(
	w http.ResponseWriter,
	r *http.Request,
) (api.CreateTasksForDeploymentPlanRequest, error) {
	var request api.CreateTasksForDeploymentPlanRequest
	if r.Body == nil || r.Body == http.NoBody {
		return request, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return request, err
	}
	if strings.TrimSpace(string(body)) == "" {
		return request, nil
	}
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return request, err
	}
	if err := request.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return request, err
	}
	return request, nil
}

func taskLockResourcesFromAPI(resources []api.TaskLockResourceRequest) []types.TaskLockResourceRequest {
	result := make([]types.TaskLockResourceRequest, 0, len(resources))
	for _, resource := range resources {
		result = append(result, types.TaskLockResourceRequest{
			ResourceType:      resource.ResourceType,
			ResourceKey:       resource.ResourceKey,
			ConcurrencyPolicy: resource.ConcurrencyPolicy,
		})
	}
	return result
}

func getTasksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		tasks, err := db.GetTasksByOrganizationID(ctx, *auth.CurrentOrgID())
		respondTaskQueueResult(w, r, log, err, func() {
			RespondJSON(w, mapping.List(tasks, mapping.TaskToAPI))
		})
	}
}

func getTaskHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID, err := uuid.Parse(r.PathValue("taskId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		task, err := db.GetTask(ctx, taskID, *auth.CurrentOrgID())
		respondTaskQueueResult(w, r, log, err, func() {
			RespondJSON(w, mapping.TaskToAPI(*task))
		})
	}
}

func transitionTaskStateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID, err := uuid.Parse(r.PathValue("taskId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.TransitionTaskStateRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		task, err := db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
			OrganizationID: *auth.CurrentOrgID(),
			TaskID:         taskID,
			Status:         request.Status,
		})
		respondTaskQueueResult(w, r, log, err, func() {
			RespondJSON(w, mapping.TaskToAPI(*task))
		})
	}
}

func respondTaskQueueResult(
	w http.ResponseWriter,
	r *http.Request,
	log *zap.Logger,
	err error,
	success func(),
) {
	if errors.Is(err, apierrors.ErrBadRequest) {
		http.Error(w, err.Error(), http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrConflict) {
		http.Error(w, err.Error(), http.StatusConflict)
	} else if errors.Is(err, apierrors.ErrNotFound) {
		http.NotFound(w, r)
	} else if err != nil {
		log.Error("failed to handle task queue request", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	} else {
		success()
	}
}
