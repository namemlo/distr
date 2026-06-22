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

func RetentionPoliciesRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Retention Policies"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		retentionPoliciesFeatureFlagMiddleware,
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getRetentionPoliciesHandler()).
			With(option.Description("List retention policies")).
			With(option.Response(http.StatusOK, []api.RetentionPolicy{}))

		r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
			Post("/", createRetentionPolicyHandler()).
			With(option.Description("Create a retention policy")).
			With(option.Request(api.CreateUpdateRetentionPolicyRequest{})).
			With(option.Response(http.StatusOK, api.RetentionPolicy{}))

		r.Route("/{retentionPolicyId}", func(r chiopenapi.Router) {
			type RetentionPolicyIDRequest struct {
				RetentionPolicyID uuid.UUID `path:"retentionPolicyId"`
			}
			type RetentionCleanupJobRouteRequest struct {
				RetentionPolicyIDRequest
				api.CreateRetentionCleanupJobRequest
			}

			r.Get("/", getRetentionPolicyHandler()).
				With(option.Description("Get a retention policy")).
				With(option.Request(RetentionPolicyIDRequest{})).
				With(option.Response(http.StatusOK, api.RetentionPolicy{}))

			r.Post("/preview", previewRetentionCleanupHandler()).
				With(option.Description("Preview cleanup candidates and safety blocks for a retention policy")).
				With(option.Request(RetentionPolicyIDRequest{})).
				With(option.Response(http.StatusOK, api.RetentionCleanupPreview{}))

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
				Post("/cleanup-jobs", createRetentionCleanupJobHandler()).
				With(option.Description("Record a dry-run cleanup job for a reviewed retention preview")).
				With(option.Request(RetentionCleanupJobRouteRequest{})).
				With(option.Response(http.StatusOK, api.RetentionCleanupJob{}))
		})
	})
}

func retentionPoliciesFeatureFlagMiddleware(handler http.Handler) http.Handler {
	for _, feature := range []featureflags.Key{
		featureflags.KeyRetentionPolicies,
		featureflags.KeyTaskQueue,
		featureflags.KeyReleaseBundles,
		featureflags.KeyEnvironments,
	} {
		handler = middleware.ExperimentalFeatureFlagMiddleware(feature)(handler)
	}
	return handler
}

func getRetentionPoliciesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		policies, err := db.GetRetentionPoliciesByOrganizationID(ctx, *auth.CurrentOrgID())
		respondRetentionPolicyResult(w, r, log, err, func() {
			RespondJSON(w, mapping.List(policies, mapping.RetentionPolicyToAPI))
		})
	}
}

func createRetentionPolicyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateRetentionPolicyRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		policy, err := db.CreateRetentionPolicy(ctx, retentionPolicyFromCreateRequest(*auth.CurrentOrgID(), request))
		respondRetentionPolicyResult(w, r, log, err, func() {
			RespondJSON(w, mapping.RetentionPolicyToAPI(*policy))
		})
	}
}

func getRetentionPolicyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		retentionPolicyID, err := uuid.Parse(r.PathValue("retentionPolicyId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		policy, err := db.GetRetentionPolicy(ctx, retentionPolicyID, *auth.CurrentOrgID())
		respondRetentionPolicyResult(w, r, log, err, func() {
			RespondJSON(w, mapping.RetentionPolicyToAPI(*policy))
		})
	}
}

func previewRetentionCleanupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		retentionPolicyID, err := uuid.Parse(r.PathValue("retentionPolicyId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		preview, err := db.PreviewRetentionCleanup(ctx, types.RetentionCleanupPreviewRequest{
			OrganizationID: *auth.CurrentOrgID(),
			PolicyID:       retentionPolicyID,
		})
		respondRetentionPolicyResult(w, r, log, err, func() {
			RespondJSON(w, mapping.RetentionCleanupPreviewToAPI(*preview))
		})
	}
}

func createRetentionCleanupJobHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		retentionPolicyID, err := uuid.Parse(r.PathValue("retentionPolicyId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateRetentionCleanupJobRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		job, err := db.CreateRetentionCleanupJob(ctx, types.CreateRetentionCleanupJobRequest{
			OrganizationID: *auth.CurrentOrgID(),
			PolicyID:       retentionPolicyID,
			ActorUserID:    auth.CurrentUserID(),
			DryRun:         request.DryRun,
		})
		respondRetentionPolicyResult(w, r, log, err, func() {
			RespondJSON(w, mapping.RetentionCleanupJobToAPI(*job))
		})
	}
}

func retentionPolicyFromCreateRequest(
	orgID uuid.UUID,
	request api.CreateUpdateRetentionPolicyRequest,
) types.CreateRetentionPolicyRequest {
	_ = request.Validate()
	return types.CreateRetentionPolicyRequest{
		OrganizationID:                    orgID,
		Name:                              strings.TrimSpace(request.Name),
		Description:                       strings.TrimSpace(request.Description),
		KeepLastSuccessfulReleases:        request.KeepLastSuccessfulReleases,
		FailedTaskRetentionDays:           request.FailedTaskRetentionDays,
		ProductionFailedTaskRetentionDays: request.ProductionFailedTaskRetentionDays,
		StepLogRetentionDays:              request.StepLogRetentionDays,
		ProtectCurrentlyDeployedReleases:  request.ProtectCurrentlyDeployedReleases,
		ProtectRetentionProtectedReleases: request.ProtectRetentionProtectedReleases,
		MinimumAuditRetentionDays:         request.MinimumAuditRetentionDays,
	}
}

func respondRetentionPolicyResult(
	w http.ResponseWriter,
	r *http.Request,
	log *zap.Logger,
	err error,
	success func(),
) {
	if errors.Is(err, apierrors.ErrBadRequest) || errors.Is(err, apierrors.ErrAlreadyExists) {
		http.Error(w, err.Error(), http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrConflict) {
		http.Error(w, err.Error(), http.StatusConflict)
	} else if errors.Is(err, apierrors.ErrNotFound) {
		http.NotFound(w, r)
	} else if err != nil {
		log.Error("failed to handle retention policy request", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	} else {
		success()
	}
}
