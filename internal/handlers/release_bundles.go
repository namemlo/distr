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

func ReleaseBundlesRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Release Bundles"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyReleaseBundles),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getReleaseBundlesHandler()).
			With(option.Description("List release bundles")).
			With(option.Response(http.StatusOK, []api.ReleaseBundle{}))

		r.Route("/{releaseBundleId}", func(r chiopenapi.Router) {
			type ReleaseBundleIDRequest struct {
				ReleaseBundleID uuid.UUID `path:"releaseBundleId"`
			}

			r.Get("/", getReleaseBundleHandler()).
				With(option.Description("Get a release bundle")).
				With(option.Request(ReleaseBundleIDRequest{})).
				With(option.Response(http.StatusOK, api.ReleaseBundle{}))

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).Group(func(r chiopenapi.Router) {
				r.Put("/", updateReleaseBundleHandler()).
					With(option.Description("Update a draft release bundle")).
					With(option.Request(struct {
						ReleaseBundleIDRequest
						api.CreateUpdateReleaseBundleRequest
					}{})).
					With(option.Response(http.StatusOK, api.ReleaseBundle{}))

				r.Delete("/", deleteReleaseBundleHandler()).
					With(option.Description("Delete a draft release bundle")).
					With(option.Request(ReleaseBundleIDRequest{}))
			})
		})

		r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
			Post("/", createReleaseBundleHandler()).
			With(option.Description("Create a draft release bundle")).
			With(option.Request(api.CreateUpdateReleaseBundleRequest{})).
			With(option.Response(http.StatusOK, api.ReleaseBundle{}))
	})
}

func getReleaseBundlesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		bundles, err := db.GetReleaseBundlesByOrganizationID(ctx, *auth.CurrentOrgID())
		if err != nil {
			log.Error("failed to get release bundles", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, releaseBundleResponses(bundles))
	}
}

//nolint:dupl
func getReleaseBundleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("releaseBundleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		bundle, err := db.GetReleaseBundle(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get release bundle", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.ReleaseBundleToAPI(*bundle))
		}
	}
}

func createReleaseBundleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateReleaseBundleRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		bundle := releaseBundleFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		if err := db.CreateReleaseBundle(ctx, &bundle); err != nil {
			handleReleaseBundleWriteError(w, r, log, "create", err)
			return
		}
		RespondJSON(w, mapping.ReleaseBundleToAPI(bundle))
	}
}

//nolint:dupl
func updateReleaseBundleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("releaseBundleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateReleaseBundleRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		bundle := releaseBundleFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		bundle.ID = id
		if err := db.UpdateReleaseBundle(ctx, &bundle); err != nil {
			handleReleaseBundleWriteError(w, r, log, "update", err)
			return
		}
		RespondJSON(w, mapping.ReleaseBundleToAPI(bundle))
	}
}

//nolint:dupl
func deleteReleaseBundleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("releaseBundleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		if err := db.DeleteReleaseBundleWithID(ctx, id, *auth.CurrentOrgID()); errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if errors.Is(err, apierrors.ErrConflict) {
			http.Error(w, "release bundle is not editable", http.StatusConflict)
		} else if err != nil {
			log.Error("failed to delete release bundle", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func releaseBundleFromCreateUpdateRequest(
	orgID uuid.UUID,
	request api.CreateUpdateReleaseBundleRequest,
) types.ReleaseBundle {
	_ = request.Validate()
	components := make([]types.ReleaseBundleComponent, 0, len(request.Components))
	for _, component := range request.Components {
		components = append(components, types.ReleaseBundleComponent{
			Key:                  component.Key,
			Name:                 component.Name,
			Type:                 component.Type,
			Version:              component.Version,
			ApplicationVersionID: component.ApplicationVersionID,
			PackageRef:           component.PackageRef,
			Digest:               component.Digest,
			Checksum:             component.Checksum,
			ChildReleaseBundleID: component.ChildReleaseBundleID,
		})
	}
	return types.ReleaseBundle{
		OrganizationID: orgID,
		ApplicationID:  request.ApplicationID,
		ChannelID:      request.ChannelID,
		ReleaseNumber:  strings.TrimSpace(request.ReleaseNumber),
		ReleaseNotes:   request.ReleaseNotes,
		SourceRevision: strings.TrimSpace(request.SourceRevision),
		Status:         types.ReleaseBundleStatusDraft,
		Components:     components,
	}
}

func releaseBundleResponses(bundles []types.ReleaseBundle) []api.ReleaseBundle {
	return mapping.List(bundles, mapping.ReleaseBundleToAPI)
}

func handleReleaseBundleWriteError(w http.ResponseWriter, r *http.Request, log *zap.Logger, action string, err error) {
	if errors.Is(err, apierrors.ErrAlreadyExists) {
		http.Error(w, "a release bundle with this release number already exists for this application", http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrNotFound) {
		http.NotFound(w, r)
	} else if errors.Is(err, apierrors.ErrConflict) {
		http.Error(w, "release bundle is not editable", http.StatusConflict)
	} else {
		log.Error("failed to "+action+" release bundle", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
