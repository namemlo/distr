package handlers

import (
	"errors"
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

func DeploymentProcessesRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Deployment Processes"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyDeploymentProcesses),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getDeploymentProcessesHandler()).
			With(option.Description("List deployment processes")).
			With(option.Response(http.StatusOK, []api.DeploymentProcess{}))

		r.Route("/{deploymentProcessId}", func(r chiopenapi.Router) {
			type DeploymentProcessIDRequest struct {
				DeploymentProcessID uuid.UUID `path:"deploymentProcessId"`
			}
			type DeploymentProcessRevisionIDRequest struct {
				DeploymentProcessIDRequest
				RevisionID uuid.UUID `path:"revisionId"`
			}

			r.Get("/", getDeploymentProcessHandler()).
				With(option.Description("Get a deployment process")).
				With(option.Request(DeploymentProcessIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentProcess{}))

			r.Get("/revisions", getDeploymentProcessRevisionsHandler()).
				With(option.Description("List deployment process revisions")).
				With(option.Request(DeploymentProcessIDRequest{})).
				With(option.Response(http.StatusOK, []api.DeploymentProcessRevision{}))

			r.Get("/revisions/{revisionId}", getDeploymentProcessRevisionHandler()).
				With(option.Description("Get a deployment process revision")).
				With(option.Request(DeploymentProcessRevisionIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentProcessRevision{}))

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).Group(func(r chiopenapi.Router) {
				r.Put("/", updateDeploymentProcessHandler()).
					With(option.Description("Update a deployment process")).
					With(option.Request(struct {
						DeploymentProcessIDRequest
						api.CreateUpdateDeploymentProcessRequest
					}{})).
					With(option.Response(http.StatusOK, api.DeploymentProcess{}))

				r.Delete("/", deleteDeploymentProcessHandler()).
					With(option.Description("Delete a deployment process")).
					With(option.Request(DeploymentProcessIDRequest{}))

				r.Post("/revisions", createDeploymentProcessRevisionHandler()).
					With(option.Description("Create a deployment process revision")).
					With(option.Request(struct {
						DeploymentProcessIDRequest
						api.CreateDeploymentProcessRevisionRequest
					}{})).
					With(option.Response(http.StatusOK, api.DeploymentProcessRevision{}))
			})
		})

		r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
			Post("/", createDeploymentProcessHandler()).
			With(option.Description("Create a deployment process")).
			With(option.Request(api.CreateUpdateDeploymentProcessRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentProcess{}))
	})
}

func getDeploymentProcessesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		processes, err := db.GetDeploymentProcessesByOrganizationID(ctx, *auth.CurrentOrgID())
		if err != nil {
			log.Error("failed to get deployment processes", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, deploymentProcessResponses(processes))
	}
}

//nolint:dupl
func getDeploymentProcessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("deploymentProcessId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		process, err := db.GetDeploymentProcess(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get deployment process", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.DeploymentProcessToAPI(*process))
		}
	}
}

func createDeploymentProcessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateDeploymentProcessRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		process := deploymentProcessFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		if err := db.CreateDeploymentProcess(ctx, &process); err != nil {
			handleDeploymentProcessWriteError(w, r, log, "create", err)
			return
		}
		RespondJSON(w, mapping.DeploymentProcessToAPI(process))
	}
}

//nolint:dupl
func updateDeploymentProcessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("deploymentProcessId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateDeploymentProcessRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		process := deploymentProcessFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		process.ID = id
		if err := db.UpdateDeploymentProcess(ctx, &process); err != nil {
			handleDeploymentProcessWriteError(w, r, log, "update", err)
			return
		}
		RespondJSON(w, mapping.DeploymentProcessToAPI(process))
	}
}

//nolint:dupl
func deleteDeploymentProcessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("deploymentProcessId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		if err := db.DeleteDeploymentProcessWithID(ctx, id, *auth.CurrentOrgID()); errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if errors.Is(err, apierrors.ErrConflict) {
			http.Error(w, "deployment process is in use", http.StatusConflict)
		} else if err != nil {
			log.Error("failed to delete deployment process", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

//nolint:dupl
func getDeploymentProcessRevisionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("deploymentProcessId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		revisions, err := db.GetDeploymentProcessRevisions(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get deployment process revisions", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, deploymentProcessRevisionResponses(revisions))
		}
	}
}

//nolint:dupl
func createDeploymentProcessRevisionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("deploymentProcessId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateDeploymentProcessRevisionRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		revision := deploymentProcessRevisionFromCreateRequest(*auth.CurrentOrgID(), id, request)
		if err := db.CreateDeploymentProcessRevision(ctx, &revision); err != nil {
			handleDeploymentProcessWriteError(w, r, log, "create revision", err)
			return
		}
		RespondJSON(w, mapping.DeploymentProcessRevisionToAPI(revision))
	}
}

//nolint:dupl
func getDeploymentProcessRevisionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		processID, err := uuid.Parse(r.PathValue("deploymentProcessId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		revisionID, err := uuid.Parse(r.PathValue("revisionId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		revision, err := db.GetDeploymentProcessRevision(ctx, processID, revisionID, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get deployment process revision", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.DeploymentProcessRevisionToAPI(*revision))
		}
	}
}

func deploymentProcessFromCreateUpdateRequest(
	orgID uuid.UUID,
	request api.CreateUpdateDeploymentProcessRequest,
) types.DeploymentProcess {
	_ = request.Validate()
	return types.DeploymentProcess{
		OrganizationID: orgID,
		ApplicationID:  request.ApplicationID,
		Name:           strings.TrimSpace(request.Name),
		Description:    request.Description,
		SortOrder:      request.SortOrder,
	}
}

func deploymentProcessRevisionFromCreateRequest(
	orgID uuid.UUID,
	processID uuid.UUID,
	request api.CreateDeploymentProcessRevisionRequest,
) types.DeploymentProcessRevision {
	_ = request.Validate()
	steps := make([]types.DeploymentProcessStep, 0, len(request.Steps))
	for _, step := range request.Steps {
		steps = append(steps, types.DeploymentProcessStep{
			Key:                   step.Key,
			Name:                  step.Name,
			ActionType:            step.ActionType,
			StepTemplateVersionID: step.StepTemplateVersionID,
			ExecutionLocation:     step.ExecutionLocation,
			InputBindings:         step.InputBindings,
			Condition:             step.Condition,
			ChannelIDs:            step.ChannelIDs,
			EnvironmentIDs:        step.EnvironmentIDs,
			TargetTags:            step.TargetTags,
			FailureMode:           step.FailureMode,
			TimeoutSeconds:        step.TimeoutSeconds,
			RetryMaxAttempts:      step.RetryPolicy.MaxAttempts,
			RetryIntervalSeconds:  step.RetryPolicy.IntervalSeconds,
			RequiredPermissions:   step.RequiredPermissions,
			SortOrder:             step.SortOrder,
			Dependencies:          step.Dependencies,
		})
	}
	return types.DeploymentProcessRevision{
		OrganizationID:      orgID,
		DeploymentProcessID: processID,
		Description:         request.Description,
		Steps:               steps,
	}
}

func deploymentProcessResponses(processes []types.DeploymentProcess) []api.DeploymentProcess {
	return mapping.List(processes, mapping.DeploymentProcessToAPI)
}

func deploymentProcessRevisionResponses(revisions []types.DeploymentProcessRevision) []api.DeploymentProcessRevision {
	return mapping.List(revisions, mapping.DeploymentProcessRevisionToAPI)
}

func handleDeploymentProcessWriteError(
	w http.ResponseWriter,
	r *http.Request,
	log *zap.Logger,
	action string,
	err error,
) {
	if errors.Is(err, apierrors.ErrAlreadyExists) {
		http.Error(w, "a deployment process with this name already exists for this application", http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrBadRequest) {
		http.Error(w, err.Error(), http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrNotFound) {
		http.NotFound(w, r)
	} else if errors.Is(err, apierrors.ErrConflict) {
		http.Error(w, "deployment process is in use", http.StatusConflict)
	} else {
		log.Error("failed to "+action+" deployment process", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
