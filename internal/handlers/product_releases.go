package handlers

import (
	"encoding/json"
	"errors"
	"io"
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

func ProductReleasesRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Product Releases"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyReleaseBundles),
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
	).Group(func(r chiopenapi.Router) {
		type productReleaseIDRequest struct {
			ProductReleaseID uuid.UUID `path:"productReleaseId"`
		}

		r.Route("/{productReleaseId}", func(r chiopenapi.Router) {
			r.Get("/", getProductReleaseHandler()).
				With(option.Description("Get an immutable Product Release manifest")).
				With(option.Request(productReleaseIDRequest{})).
				With(option.Response(http.StatusOK, api.ProductRelease{})).
				With(option.Response(http.StatusNotFound, api.ErrorResponse{}))

			r.Post("/validate", validateProductReleaseHandler()).
				With(option.Description("Validate a Product Release capability graph")).
				With(option.Request(productReleaseIDRequest{})).
				With(option.Response(http.StatusOK, api.ProductReleaseValidationResponse{})).
				With(option.Response(http.StatusNotFound, api.ErrorResponse{}))

			r.Get("/graph", getProductReleaseGraphHandler()).
				With(option.Description("Get the frozen Product Release capability graph")).
				With(option.Request(productReleaseIDRequest{})).
				With(option.Response(http.StatusOK, api.ProductReleaseGraphResponse{})).
				With(option.Response(http.StatusConflict, api.ErrorResponse{})).
				With(option.Response(http.StatusNotFound, api.ErrorResponse{}))

			r.With(
				middleware.RequireReadWriteOrAdmin,
				middleware.BlockSuperAdmin,
			).Post("/publish", publishProductReleaseHandler()).
				With(option.Description("Publish and freeze a valid Product Release")).
				With(option.Request(productReleaseIDRequest{})).
				With(option.Response(http.StatusOK, api.ProductRelease{})).
				With(option.Response(http.StatusBadRequest, api.ProductReleaseValidationResponse{})).
				With(option.Response(http.StatusConflict, api.ErrorResponse{})).
				With(option.Response(http.StatusNotFound, api.ErrorResponse{}))
		})

		r.With(
			middleware.RequireReadWriteOrAdmin,
			middleware.BlockSuperAdmin,
		).Post("/", createProductReleaseHandler()).
			With(option.Description("Create a target-neutral Product Release draft")).
			With(option.Request(api.CreateProductReleaseRequest{})).
			With(option.Response(http.StatusOK, api.ProductRelease{})).
			With(option.Response(http.StatusBadRequest, api.ErrorResponse{})).
			With(option.Response(http.StatusConflict, api.ErrorResponse{})).
			With(option.Response(http.StatusNotFound, api.ErrorResponse{}))
	})
}

func createProductReleaseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		authentication := auth.Authentication.Require(ctx)
		request, err := strictProductReleaseBody(w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		manifest := mapping.ProductReleaseManifestFromCreateRequest(*authentication.CurrentOrgID(), request)
		bundle, err := db.CreateProductReleaseDraft(ctx, &manifest)
		if err != nil {
			var validationErr *db.ProductReleaseValidationError
			if errors.As(err, &validationErr) {
				RespondJSONWithStatus(
					w,
					http.StatusBadRequest,
					mapping.ProductReleaseValidationToAPI(validationErr.Issues),
				)
				return
			}
			handleProductReleaseError(w, r, log, "create", err)
			return
		}
		_, storedManifest, err := db.GetProductRelease(ctx, bundle.ID, *authentication.CurrentOrgID())
		if err != nil {
			handleProductReleaseError(w, r, log, "load created", err)
			return
		}
		RespondJSON(w, mapping.ProductReleaseToAPI(*bundle, *storedManifest))
	}
}

func strictProductReleaseBody(
	w http.ResponseWriter,
	r *http.Request,
) (api.CreateProductReleaseRequest, error) {
	var request api.CreateProductReleaseRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return request, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("request must contain exactly one JSON value")
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return request, err
	}
	return request, nil
}

func getProductReleaseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := productReleaseID(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		authentication := auth.Authentication.Require(ctx)
		bundle, manifest, err := db.GetProductRelease(ctx, id, *authentication.CurrentOrgID())
		if err != nil {
			handleProductReleaseError(w, r, log, "get", err)
			return
		}
		RespondJSON(w, mapping.ProductReleaseToAPI(*bundle, *manifest))
	}
}

func validateProductReleaseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := productReleaseID(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		authentication := auth.Authentication.Require(ctx)
		issues, err := db.ValidateProductRelease(ctx, id, *authentication.CurrentOrgID())
		if err != nil {
			handleProductReleaseError(w, r, log, "validate", err)
			return
		}
		RespondJSON(w, mapping.ProductReleaseValidationToAPI(issues))
	}
}

func publishProductReleaseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := productReleaseID(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		authentication := auth.Authentication.Require(ctx)
		ctx = db.WithProductReleaseOrganizationID(ctx, *authentication.CurrentOrgID())
		bundle, err := db.PublishProductRelease(ctx, id, authentication.CurrentUserID())
		if err != nil {
			var validationErr *db.ProductReleaseValidationError
			if errors.As(err, &validationErr) {
				RespondJSONWithStatus(
					w,
					http.StatusBadRequest,
					mapping.ProductReleaseValidationToAPI(validationErr.Issues),
				)
				return
			}
			handleProductReleaseError(w, r, log, "publish", err)
			return
		}
		_, manifest, err := db.GetProductRelease(ctx, id, *authentication.CurrentOrgID())
		if err != nil {
			handleProductReleaseError(w, r, log, "load published", err)
			return
		}
		RespondJSON(w, mapping.ProductReleaseToAPI(*bundle, *manifest))
	}
}

func getProductReleaseGraphHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := productReleaseID(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		authentication := auth.Authentication.Require(ctx)
		graph, err := db.GetProductReleaseGraph(ctx, id, *authentication.CurrentOrgID())
		if err != nil {
			handleProductReleaseError(w, r, log, "get graph", err)
			return
		}
		RespondJSON(w, api.ProductReleaseGraphResponse{ReleaseBundleID: id, Graph: *graph})
	}
}

func productReleaseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("productReleaseId"))
	if err != nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func handleProductReleaseError(
	w http.ResponseWriter,
	r *http.Request,
	log *zap.Logger,
	action string,
	err error,
) {
	status, message := productReleasePublicError(err)
	if status == http.StatusInternalServerError {
		log.Error("failed to "+action+" Product Release", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
	}
	http.Error(w, message, status)
}

func productReleasePublicError(err error) (int, string) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		return http.StatusNotFound, "product release not found"
	case errors.Is(err, apierrors.ErrBadRequest):
		return http.StatusBadRequest, "product release request is invalid"
	case errors.Is(err, apierrors.ErrConflict), errors.Is(err, apierrors.ErrAlreadyExists):
		return http.StatusConflict, "product release conflicts with immutable state"
	case errors.Is(err, apierrors.ErrForbidden):
		return http.StatusForbidden, "product release operation is forbidden"
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}
