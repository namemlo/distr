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

func LifecyclesRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Lifecycles"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyLifecycles),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getLifecyclesHandler()).
			With(option.Description("List lifecycles")).
			With(option.Response(http.StatusOK, []api.Lifecycle{}))

		r.Route("/{lifecycleId}", func(r chiopenapi.Router) {
			type LifecycleIDRequest struct {
				LifecycleID uuid.UUID `path:"lifecycleId"`
			}

			r.Get("/", getLifecycleHandler()).
				With(option.Description("Get a lifecycle")).
				With(option.Request(LifecycleIDRequest{})).
				With(option.Response(http.StatusOK, api.Lifecycle{}))

			r.Get("/phases", getLifecyclePhasesHandler()).
				With(option.Description("Get lifecycle phases")).
				With(option.Request(LifecycleIDRequest{})).
				With(option.Response(http.StatusOK, []api.LifecyclePhase{}))

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).Group(func(r chiopenapi.Router) {
				r.Put("/", updateLifecycleHandler()).
					With(option.Description("Update a lifecycle")).
					With(option.Request(struct {
						LifecycleIDRequest
						api.CreateUpdateLifecycleRequest
					}{})).
					With(option.Response(http.StatusOK, api.Lifecycle{}))

				r.Put("/phases", replaceLifecyclePhasesHandler()).
					With(option.Description("Replace lifecycle phases")).
					With(option.Request(struct {
						LifecycleIDRequest
						api.UpdateLifecyclePhasesRequest
					}{})).
					With(option.Response(http.StatusOK, api.Lifecycle{}))

				r.Delete("/", deleteLifecycleHandler()).
					With(option.Description("Delete a lifecycle")).
					With(option.Request(LifecycleIDRequest{}))
			})
		})

		r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
			Post("/", createLifecycleHandler()).
			With(option.Description("Create a lifecycle")).
			With(option.Request(api.CreateUpdateLifecycleRequest{})).
			With(option.Response(http.StatusOK, api.Lifecycle{}))
	})
}

func getLifecyclesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		lifecycles, err := db.GetLifecyclesByOrganizationID(ctx, *auth.CurrentOrgID())
		if err != nil {
			log.Error("failed to get lifecycles", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, lifecycleResponses(lifecycles))
	}
}

//nolint:dupl
func getLifecycleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("lifecycleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		lifecycle, err := db.GetLifecycle(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get lifecycle", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.LifecycleToAPI(*lifecycle))
		}
	}
}

func getLifecyclePhasesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("lifecycleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		lifecycle, err := db.GetLifecycle(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get lifecycle phases", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.List(lifecycle.Phases, mapping.LifecyclePhaseToAPI))
		}
	}
}

func createLifecycleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateLifecycleRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		lifecycle := lifecycleFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		if err := db.CreateLifecycle(ctx, &lifecycle); err != nil {
			handleLifecycleWriteError(w, r, log, "create", err)
			return
		}
		RespondJSON(w, mapping.LifecycleToAPI(lifecycle))
	}
}

//nolint:dupl
func updateLifecycleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("lifecycleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateLifecycleRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		lifecycle := lifecycleFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		lifecycle.ID = id
		if err := db.UpdateLifecycle(ctx, &lifecycle); err != nil {
			handleLifecycleWriteError(w, r, log, "update", err)
			return
		}
		RespondJSON(w, mapping.LifecycleToAPI(lifecycle))
	}
}

func replaceLifecyclePhasesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("lifecycleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.UpdateLifecyclePhasesRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		lifecycle, err := db.ReplaceLifecyclePhases(
			ctx,
			id,
			*auth.CurrentOrgID(),
			lifecyclePhasesFromRequests(request.Phases),
		)
		if err != nil {
			handleLifecycleWriteError(w, r, log, "replace phases", err)
			return
		}
		RespondJSON(w, mapping.LifecycleToAPI(*lifecycle))
	}
}

//nolint:dupl
func deleteLifecycleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("lifecycleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		if err := db.DeleteLifecycleWithID(ctx, id, *auth.CurrentOrgID()); errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if errors.Is(err, apierrors.ErrConflict) {
			http.Error(w, "lifecycle is in use", http.StatusConflict)
		} else if err != nil {
			log.Error("failed to delete lifecycle", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func lifecycleFromCreateUpdateRequest(orgID uuid.UUID, request api.CreateUpdateLifecycleRequest) types.Lifecycle {
	return types.Lifecycle{
		OrganizationID: orgID,
		Name:           strings.TrimSpace(request.Name),
		Description:    request.Description,
		SortOrder:      request.SortOrder,
		Phases:         lifecyclePhasesFromRequests(request.Phases),
	}
}

func lifecyclePhasesFromRequests(requests []api.CreateUpdateLifecyclePhaseRequest) []types.LifecyclePhase {
	phases := make([]types.LifecyclePhase, 0, len(requests))
	for _, request := range requests {
		phases = append(phases, types.LifecyclePhase{
			Name:                         strings.TrimSpace(request.Name),
			Description:                  request.Description,
			SortOrder:                    request.SortOrder,
			EnvironmentIDs:               request.EnvironmentIDs,
			Optional:                     request.Optional,
			AutomaticPromotion:           request.AutomaticPromotion,
			MinimumSuccessfulDeployments: request.MinimumSuccessfulDeployments,
			ApprovalPolicyID:             request.ApprovalPolicyID,
			RetentionPolicyID:            request.RetentionPolicyID,
		})
	}
	return phases
}

func lifecycleResponses(lifecycles []types.Lifecycle) []api.Lifecycle {
	return mapping.List(lifecycles, mapping.LifecycleToAPI)
}

func handleLifecycleWriteError(w http.ResponseWriter, r *http.Request, log *zap.Logger, action string, err error) {
	if errors.Is(err, apierrors.ErrAlreadyExists) {
		http.Error(w, "a lifecycle or phase with this name or order already exists", http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrNotFound) {
		http.NotFound(w, r)
	} else {
		log.Error("failed to "+action+" lifecycle", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
