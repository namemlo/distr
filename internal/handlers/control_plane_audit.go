package handlers

import (
	"errors"
	"net/http"
	"strconv"
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

func ControlPlaneAuditRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Control Plane Audit"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		controlPlaneAuditFeatureFlagMiddleware,
	).Group(func(r chiopenapi.Router) {
		r.With(middleware.RequireOrganizationPermission(types.PermissionAuditView)).
			Get("/events", getControlPlaneAuditEventsHandler()).
			With(option.Description("List correlated control-plane audit events")).
			With(option.Request(api.ControlPlaneAuditListRequest{})).
			With(option.Response(http.StatusOK, api.ControlPlaneAuditEventPage{}))

		r.With(middleware.RequireOrganizationPermission(types.PermissionAuditView)).
			Post("/evidence-bundles", createControlPlaneEvidenceBundleHandler()).
			With(option.Description("Build a deterministic deployment evidence bundle")).
			With(option.Request(api.EvidenceBundleRequest{})).
			With(option.Response(http.StatusOK, api.EvidenceBundle{}))

		r.With(middleware.RequireOrganizationPermission(types.PermissionAuditView)).
			Get("/export-sinks", getAuditExportSinksHandler()).
			With(option.Description("List configured audit export sinks")).
			With(option.Response(http.StatusOK, []api.AuditExportSink{}))

		r.With(
			middleware.RequireOrganizationPermission(types.PermissionAuditExport),
			middleware.BlockSuperAdmin,
		).Post("/export-sinks", createAuditExportSinkHandler()).
			With(option.Description("Create an audit export sink using an endpoint or secret reference")).
			With(option.Request(api.CreateAuditExportSinkRequest{})).
			With(option.Response(http.StatusCreated, api.AuditExportSink{}))

		r.With(middleware.RequireOrganizationPermission(types.PermissionAuditView)).
			Get("/export-status", getAuditExportStatusesHandler()).
			With(option.Description("List audit export checkpoint lag and failure status")).
			With(option.Response(http.StatusOK, []api.AuditExportStatus{}))
	})
}

func controlPlaneAuditFeatureFlagMiddleware(handler http.Handler) http.Handler {
	return middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2)(handler)
}

func getControlPlaneAuditEventsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, ok := controlPlaneAuditListRequestFromHTTP(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		organizationID := *auth.Authentication.Require(ctx).CurrentOrgID()
		events, err := db.GetControlPlaneAuditEvents(
			ctx,
			organizationID,
			request.AfterSequence,
			request.PageLimit(),
		)
		respondControlPlaneAuditResult(w, r, err, func() {
			RespondJSON(w, mapping.ControlPlaneAuditEventPageToAPI(events))
		})
	}
}

func createControlPlaneEvidenceBundleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.EvidenceBundleRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		organizationID := *auth.Authentication.Require(ctx).CurrentOrgID()
		bundle, err := db.BuildDeploymentEvidenceBundle(ctx, types.EvidenceBundleQuery{
			OrganizationID:   organizationID,
			DeploymentPlanID: request.DeploymentPlanID,
		})
		respondControlPlaneAuditResult(w, r, err, func() {
			RespondJSON(w, mapping.EvidenceBundleToAPI(*bundle))
		})
	}
}

func getAuditExportSinksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		organizationID := *auth.Authentication.Require(ctx).CurrentOrgID()
		sinks, err := db.GetAuditExportSinks(ctx, organizationID)
		respondControlPlaneAuditResult(w, r, err, func() {
			RespondJSON(w, mapping.List(sinks, mapping.AuditExportSinkToAPI))
		})
	}
}

func createAuditExportSinkHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.CreateAuditExportSinkRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		organizationID := *auth.Authentication.Require(ctx).CurrentOrgID()
		sink, err := db.CreateAuditExportSink(ctx, auditExportSinkInput(organizationID, request))
		respondControlPlaneAuditResult(w, r, err, func() {
			RespondJSONWithStatus(w, http.StatusCreated, mapping.AuditExportSinkToAPI(*sink))
		})
	}
}

func getAuditExportStatusesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		organizationID := *auth.Authentication.Require(ctx).CurrentOrgID()
		statuses, err := db.GetAuditExportStatuses(ctx, organizationID)
		respondControlPlaneAuditResult(w, r, err, func() {
			RespondJSON(w, mapping.List(statuses, mapping.AuditExportStatusToAPI))
		})
	}
}

func auditExportSinkInput(
	organizationID uuid.UUID,
	request api.CreateAuditExportSinkRequest,
) types.CreateAuditExportSinkInput {
	return types.CreateAuditExportSinkInput{
		OrganizationID:    organizationID,
		Name:              strings.TrimSpace(request.Name),
		Kind:              request.Kind,
		EndpointReference: strings.TrimSpace(request.EndpointReference),
		ConfigChecksum:    request.ConfigChecksum,
		Enabled:           request.IsEnabled(),
	}
}

func controlPlaneAuditListRequestFromHTTP(
	w http.ResponseWriter,
	r *http.Request,
) (api.ControlPlaneAuditListRequest, bool) {
	request := api.ControlPlaneAuditListRequest{}
	if value := r.URL.Query().Get("afterSequence"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			http.Error(w, "afterSequence is invalid", http.StatusBadRequest)
			return request, false
		}
		request.AfterSequence = parsed
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			http.Error(w, "limit is invalid", http.StatusBadRequest)
			return request, false
		}
		request.Limit = parsed
	}
	if err := request.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return request, false
	}
	return request, true
}

func respondControlPlaneAuditResult(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	success func(),
) {
	switch {
	case errors.Is(err, apierrors.ErrBadRequest), errors.Is(err, apierrors.ErrAlreadyExists):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, apierrors.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, apierrors.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, apierrors.ErrConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	case err != nil:
		log := internalctx.GetLogger(r.Context())
		log.Error("failed to handle control-plane audit request", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	default:
		success()
	}
}
