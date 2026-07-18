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

func DeploymentPlanDraftsRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Deployment Plan Drafts"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyDeploymentPlans),
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
	).Group(func(r chiopenapi.Router) {
		type deploymentPlanDraftIDRequest struct {
			DeploymentPlanDraftID uuid.UUID `path:"deploymentPlanDraftId"`
		}
		r.Route("/{deploymentPlanDraftId}", func(r chiopenapi.Router) {
			r.Get("/", getDeploymentPlanDraftHandler()).
				With(option.Description("Get a mutable target deployment plan draft")).
				With(option.Request(deploymentPlanDraftIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentPlanDraft{})).
				With(option.Response(http.StatusNotFound, api.ErrorResponse{}))

			type updateDeploymentPlanDraftRouteRequest struct {
				deploymentPlanDraftIDRequest
				api.UpdateDeploymentPlanDraftRequest
			}
			r.With(
				middleware.RequireReadWriteOrAdmin,
				middleware.BlockSuperAdmin,
			).Patch("/", updateDeploymentPlanDraftHandler()).
				With(option.Description("Update a target deployment plan draft with optimistic revision")).
				With(option.Request(updateDeploymentPlanDraftRouteRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentPlanDraft{})).
				With(option.Response(http.StatusConflict, api.ErrorResponse{}))

			r.With(
				middleware.RequireReadWriteOrAdmin,
				middleware.BlockSuperAdmin,
			).Post("/validate", validateDeploymentPlanDraftHandler()).
				With(option.Description("Resolve and validate an exact target deployment plan preview")).
				With(option.Request(deploymentPlanDraftIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentPlanDraftValidation{}))

			type publishDeploymentPlanDraftRouteRequest struct {
				deploymentPlanDraftIDRequest
				api.PublishDeploymentPlanDraftRequest
			}
			r.With(
				middleware.RequireReadWriteOrAdmin,
				middleware.BlockSuperAdmin,
			).Post("/publish", publishDeploymentPlanDraftHandler()).
				With(option.Description("Publish an immutable target deployment plan")).
				With(option.Request(publishDeploymentPlanDraftRouteRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentPlan{})).
				With(option.Response(http.StatusBadRequest, api.DeploymentPlanDraftValidation{})).
				With(option.Response(http.StatusConflict, api.ErrorResponse{}))
		})

		r.With(
			middleware.RequireReadWriteOrAdmin,
			middleware.BlockSuperAdmin,
		).Post("/", createDeploymentPlanDraftHandler()).
			With(option.Description("Create a mutable target deployment plan draft")).
			With(option.Request(api.CreateDeploymentPlanDraftRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentPlanDraft{})).
			With(option.Response(http.StatusBadRequest, api.ErrorResponse{})).
			With(option.Response(http.StatusConflict, api.ErrorResponse{}))
	})
}

func createDeploymentPlanDraftHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.CreateDeploymentPlanDraftRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authentication := auth.Authentication.Require(r.Context())
		draft, err := db.CreateDeploymentPlanDraft(r.Context(), &types.PlanDraft{
			OrganizationID:             *authentication.CurrentOrgID(),
			ProductReleaseID:           request.ProductReleaseID,
			DeploymentUnitID:           request.DeploymentUnitID,
			EnvironmentAssignmentID:    request.EnvironmentAssignmentID,
			TargetConfigSnapshotID:     request.TargetConfigSnapshotID,
			ProtocolVersion:            request.ProtocolVersion,
			SupersedesDeploymentPlanID: request.SupersedesDeploymentPlanID,
			SupersedeReason:            request.SupersedeReason,
		})
		if err != nil {
			handleDeploymentPlanDraftError(w, r, "create", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPlanDraftToAPI(*draft))
	}
}

func getDeploymentPlanDraftHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := deploymentPlanDraftID(w, r)
		if !ok {
			return
		}
		authentication := auth.Authentication.Require(r.Context())
		draft, err := db.GetDeploymentPlanDraft(
			r.Context(),
			id,
			*authentication.CurrentOrgID(),
		)
		if err != nil {
			handleDeploymentPlanDraftError(w, r, "get", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPlanDraftToAPI(*draft))
	}
}

func updateDeploymentPlanDraftHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := deploymentPlanDraftID(w, r)
		if !ok {
			return
		}
		request, err := JsonBody[api.UpdateDeploymentPlanDraftRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authentication := auth.Authentication.Require(r.Context())
		draft, err := db.UpdateDeploymentPlanDraft(r.Context(), &types.PlanDraft{
			ID:                         id,
			OrganizationID:             *authentication.CurrentOrgID(),
			ProductReleaseID:           request.ProductReleaseID,
			DeploymentUnitID:           request.DeploymentUnitID,
			EnvironmentAssignmentID:    request.EnvironmentAssignmentID,
			TargetConfigSnapshotID:     request.TargetConfigSnapshotID,
			ProtocolVersion:            request.ProtocolVersion,
			SupersedesDeploymentPlanID: request.SupersedesDeploymentPlanID,
			SupersedeReason:            request.SupersedeReason,
		}, request.ExpectedRevision)
		if err != nil {
			handleDeploymentPlanDraftError(w, r, "update", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPlanDraftToAPI(*draft))
	}
}

func validateDeploymentPlanDraftHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := deploymentPlanDraftID(w, r)
		if !ok {
			return
		}
		authentication := auth.Authentication.Require(r.Context())
		result, err := db.ValidateDeploymentPlanDraft(
			r.Context(),
			id,
			*authentication.CurrentOrgID(),
		)
		if err != nil {
			handleDeploymentPlanDraftError(w, r, "validate", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPlanDraftValidationToAPI(*result))
	}
}

func publishDeploymentPlanDraftHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := deploymentPlanDraftID(w, r)
		if !ok {
			return
		}
		request, err := JsonBody[api.PublishDeploymentPlanDraftRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authentication := auth.Authentication.Require(r.Context())
		plan, err := db.PublishTargetDeploymentPlan(
			r.Context(),
			id,
			*authentication.CurrentOrgID(),
			request.ExpectedRevision,
			request.ExpectedPreviewChecksum,
		)
		if err != nil {
			var validationErr *db.DeploymentPlanDraftValidationError
			if errors.As(err, &validationErr) {
				RespondJSONWithStatus(
					w,
					http.StatusBadRequest,
					api.DeploymentPlanDraftValidation{Issues: validationErr.Issues},
				)
				return
			}
			handleDeploymentPlanDraftError(w, r, "publish", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPlanToAPI(*plan))
	}
}

func deploymentPlanDraftID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("deploymentPlanDraftId"))
	if err != nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func handleDeploymentPlanDraftError(
	w http.ResponseWriter,
	r *http.Request,
	action string,
	err error,
) {
	status, message := deploymentPlanDraftPublicError(err)
	if status == http.StatusInternalServerError {
		log := internalctx.GetLogger(r.Context())
		log.Error("failed to "+action+" Deployment Plan Draft", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
	}
	http.Error(w, message, status)
}

func deploymentPlanDraftPublicError(err error) (int, string) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		return http.StatusNotFound, "deployment plan draft not found"
	case errors.Is(err, apierrors.ErrBadRequest):
		return http.StatusBadRequest, "deployment plan draft request is invalid"
	case errors.Is(err, apierrors.ErrConflict), errors.Is(err, apierrors.ErrAlreadyExists):
		return http.StatusConflict, "deployment plan draft conflicts with current immutable state"
	case errors.Is(err, apierrors.ErrForbidden):
		return http.StatusForbidden, "deployment plan draft operation is forbidden"
	default:
		return http.StatusInternalServerError, "deployment plan draft operation failed"
	}
}
