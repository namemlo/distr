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

func RunbooksRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Runbooks"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyRunbooks),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getRunbooksHandler()).
			With(option.Description("List runbooks")).
			With(option.Response(http.StatusOK, []api.Runbook{}))

		r.Route("/{runbookId}", func(r chiopenapi.Router) {
			type RunbookIDRequest struct {
				RunbookID uuid.UUID `path:"runbookId"`
			}
			type RunbookRevisionIDRequest struct {
				RunbookIDRequest
				RevisionID uuid.UUID `path:"revisionId"`
			}

			r.Get("/", getRunbookHandler()).
				With(option.Description("Get a runbook")).
				With(option.Request(RunbookIDRequest{})).
				With(option.Response(http.StatusOK, api.Runbook{}))

			r.Get("/revisions", getRunbookRevisionsHandler()).
				With(option.Description("List runbook revisions")).
				With(option.Request(RunbookIDRequest{})).
				With(option.Response(http.StatusOK, []api.RunbookRevision{}))

			r.Get("/revisions/{revisionId}", getRunbookRevisionHandler()).
				With(option.Description("Get a runbook revision")).
				With(option.Request(RunbookRevisionIDRequest{})).
				With(option.Response(http.StatusOK, api.RunbookRevision{}))

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).Group(func(r chiopenapi.Router) {
				r.Put("/", updateRunbookHandler()).
					With(option.Description("Update a runbook")).
					With(option.Request(struct {
						RunbookIDRequest
						api.CreateUpdateRunbookRequest
					}{})).
					With(option.Response(http.StatusOK, api.Runbook{}))

				r.Delete("/", deleteRunbookHandler()).
					With(option.Description("Delete a runbook")).
					With(option.Request(RunbookIDRequest{}))

				r.Post("/revisions", createRunbookRevisionHandler()).
					With(option.Description("Create a runbook revision")).
					With(option.Request(struct {
						RunbookIDRequest
						api.CreateRunbookRevisionRequest
					}{})).
					With(option.Response(http.StatusOK, api.RunbookRevision{}))

				r.Post("/revisions/{revisionId}/publish", publishRunbookRevisionHandler()).
					With(option.Description("Publish an immutable runbook revision snapshot")).
					With(option.Request(RunbookRevisionIDRequest{})).
					With(option.Response(http.StatusOK, api.RunbookSnapshot{}))
			})
		})

		r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
			Post("/", createRunbookHandler()).
			With(option.Description("Create a runbook")).
			With(option.Request(api.CreateUpdateRunbookRequest{})).
			With(option.Response(http.StatusOK, api.Runbook{}))
	})
}

func getRunbooksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		runbooks, err := db.GetRunbooksByOrganizationID(ctx, *auth.CurrentOrgID())
		if err != nil {
			log.Error("failed to get runbooks", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, runbookResponses(runbooks))
	}
}

//nolint:dupl
func getRunbookHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("runbookId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		runbook, err := db.GetRunbook(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get runbook", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.RunbookToAPI(*runbook))
		}
	}
}

func createRunbookHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateRunbookRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		runbook := runbookFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		if err := db.CreateRunbook(ctx, &runbook); err != nil {
			handleRunbookWriteError(w, r, log, "create", err)
			return
		}
		RespondJSON(w, mapping.RunbookToAPI(runbook))
	}
}

//nolint:dupl
func updateRunbookHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("runbookId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateRunbookRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		runbook := runbookFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		runbook.ID = id
		if err := db.UpdateRunbook(ctx, &runbook); err != nil {
			handleRunbookWriteError(w, r, log, "update", err)
			return
		}
		RespondJSON(w, mapping.RunbookToAPI(runbook))
	}
}

//nolint:dupl
func deleteRunbookHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("runbookId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		if err := db.DeleteRunbookWithID(ctx, id, *auth.CurrentOrgID()); errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if errors.Is(err, apierrors.ErrConflict) {
			http.Error(w, "runbook is in use", http.StatusConflict)
		} else if err != nil {
			log.Error("failed to delete runbook", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

//nolint:dupl
func getRunbookRevisionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("runbookId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		revisions, err := db.GetRunbookRevisions(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get runbook revisions", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, runbookRevisionResponses(revisions))
		}
	}
}

//nolint:dupl
func createRunbookRevisionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("runbookId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateRunbookRevisionRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		revision := runbookRevisionFromCreateRequest(*auth.CurrentOrgID(), id, request)
		if err := db.CreateRunbookRevision(ctx, &revision); err != nil {
			handleRunbookWriteError(w, r, log, "create revision", err)
			return
		}
		RespondJSON(w, mapping.RunbookRevisionToAPI(revision))
	}
}

//nolint:dupl
func getRunbookRevisionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runbookID, err := uuid.Parse(r.PathValue("runbookId"))
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

		revision, err := db.GetRunbookRevision(ctx, runbookID, revisionID, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get runbook revision", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.RunbookRevisionToAPI(*revision))
		}
	}
}

//nolint:dupl
func publishRunbookRevisionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runbookID, err := uuid.Parse(r.PathValue("runbookId"))
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

		snapshot, err := db.PublishRunbookRevision(ctx, runbookID, revisionID, *auth.CurrentOrgID(), auth.CurrentUserID())
		if err != nil {
			handleRunbookWriteError(w, r, log, "publish revision", err)
			return
		}
		RespondJSON(w, mapping.RunbookSnapshotToAPI(*snapshot))
	}
}

func runbookFromCreateUpdateRequest(
	orgID uuid.UUID,
	request api.CreateUpdateRunbookRequest,
) types.Runbook {
	_ = request.Validate()
	return types.Runbook{
		OrganizationID: orgID,
		ApplicationID:  request.ApplicationID,
		Name:           strings.TrimSpace(request.Name),
		Description:    request.Description,
		SortOrder:      request.SortOrder,
	}
}

func runbookRevisionFromCreateRequest(
	orgID uuid.UUID,
	runbookID uuid.UUID,
	request api.CreateRunbookRevisionRequest,
) types.RunbookRevision {
	_ = request.Validate()
	steps := make([]types.RunbookStep, 0, len(request.Steps))
	for _, step := range request.Steps {
		steps = append(steps, types.RunbookStep{
			Key:                   step.Key,
			Name:                  step.Name,
			ActionType:            step.ActionType,
			StepTemplateVersionID: step.StepTemplateVersionID,
			ExecutionLocation:     step.ExecutionLocation,
			InputBindings:         step.InputBindings,
			Condition:             step.Condition,
			FailureMode:           step.FailureMode,
			TimeoutSeconds:        step.TimeoutSeconds,
			RetryMaxAttempts:      step.RetryPolicy.MaxAttempts,
			RetryIntervalSeconds:  step.RetryPolicy.IntervalSeconds,
			RequiredPermissions:   step.RequiredPermissions,
			SortOrder:             step.SortOrder,
			Dependencies:          step.Dependencies,
		})
	}
	return types.RunbookRevision{
		OrganizationID: orgID,
		RunbookID:      runbookID,
		Description:    request.Description,
		Steps:          steps,
	}
}

func runbookResponses(runbooks []types.Runbook) []api.Runbook {
	return mapping.List(runbooks, mapping.RunbookToAPI)
}

func runbookRevisionResponses(revisions []types.RunbookRevision) []api.RunbookRevision {
	return mapping.List(revisions, mapping.RunbookRevisionToAPI)
}

func handleRunbookWriteError(
	w http.ResponseWriter,
	r *http.Request,
	log *zap.Logger,
	action string,
	err error,
) {
	if errors.Is(err, apierrors.ErrAlreadyExists) {
		http.Error(w, "a runbook with this name already exists for this application", http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrBadRequest) {
		http.Error(w, err.Error(), http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrNotFound) {
		http.NotFound(w, r)
	} else if errors.Is(err, apierrors.ErrConflict) {
		http.Error(w, "runbook is in use", http.StatusConflict)
	} else {
		log.Error("failed to "+action+" runbook", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
