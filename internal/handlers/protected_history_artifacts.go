package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/protectedhistory"
	"github.com/distr-sh/distr/internal/types"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

type protectedHistoryArtifactService interface {
	Retain(
		context.Context,
		protectedhistory.CreateRetentionRequest,
	) (*protectedhistory.RetainedArtifact, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (*protectedhistory.RetainedArtifact, error)
	Verify(context.Context, uuid.UUID, uuid.UUID) (*protectedhistory.ObjectIdentity, error)
}

type databaseProtectedHistoryArtifactService struct {
	store protectedhistory.ObjectStore
	clock func() time.Time
}

func (service databaseProtectedHistoryArtifactService) Retain(
	ctx context.Context,
	request protectedhistory.CreateRetentionRequest,
) (*protectedhistory.RetainedArtifact, error) {
	return db.RetainProtectedHistoryArtifact(ctx, request, service.store, service.clock())
}

func (service databaseProtectedHistoryArtifactService) Get(
	ctx context.Context,
	organizationID,
	id uuid.UUID,
) (*protectedhistory.RetainedArtifact, error) {
	return db.GetProtectedHistoryArtifact(ctx, organizationID, id)
}

func (service databaseProtectedHistoryArtifactService) Verify(
	ctx context.Context,
	organizationID,
	id uuid.UUID,
) (*protectedhistory.ObjectIdentity, error) {
	return db.VerifyProtectedHistoryArtifact(ctx, organizationID, id, service.store)
}

func ProtectedHistoryArtifactsRouter(r chiopenapi.Router) {
	ProtectedHistoryArtifactsRouterWithStore(protectedhistory.NewUnavailableObjectStore())(r)
}

func ProtectedHistoryArtifactsRouterWithStore(
	store protectedhistory.ObjectStore,
) func(chiopenapi.Router) {
	if store == nil {
		store = protectedhistory.NewUnavailableObjectStore()
	}
	return ProtectedHistoryArtifactsRouterWithService(databaseProtectedHistoryArtifactService{
		store: store,
		clock: time.Now,
	})
}

func ProtectedHistoryArtifactsRouterWithService(
	service protectedHistoryArtifactService,
) func(chiopenapi.Router) {
	return func(r chiopenapi.Router) {
		r.WithOptions(option.GroupTags("Protected History Artifacts"))
		r.With(
			middleware.RequireVendor,
			middleware.RequireOrgAndRole,
			middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyOperatorControlPlaneV2),
		).Group(func(r chiopenapi.Router) {
			r.With(
				requireControlPlaneOrganizationAction(types.ActionAuditExport),
				middleware.BlockSuperAdmin,
			).Post("/", createProtectedHistoryArtifactHandler(service)).
				With(option.Description(
					"Export current scoped protected history to immutable object storage and retain audit-bound metadata",
				)).
				With(option.Request(api.CreateProtectedHistoryArtifactRequest{})).
				With(option.Response(http.StatusCreated, api.ProtectedHistoryArtifact{}))

			type protectedHistoryArtifactIDRequest struct {
				ProtectedHistoryArtifactID uuid.UUID `path:"protectedHistoryArtifactId"`
			}
			r.With(requireControlPlaneOrganizationAction(types.ActionAuditView)).
				Get("/{protectedHistoryArtifactId}", getProtectedHistoryArtifactHandler(service)).
				With(option.Description("Get immutable protected-history artifact metadata")).
				With(option.Request(protectedHistoryArtifactIDRequest{})).
				With(option.Response(http.StatusOK, api.ProtectedHistoryArtifact{}))
			r.With(requireControlPlaneOrganizationAction(types.ActionAuditView)).
				Get("/{protectedHistoryArtifactId}/verification", verifyProtectedHistoryArtifactHandler(service)).
				With(option.Description("Read back and verify the retained immutable object without changing metadata")).
				With(option.Request(protectedHistoryArtifactIDRequest{})).
				With(option.Response(http.StatusOK, api.ProtectedHistoryArtifactVerification{}))
		})
	}
}

func createProtectedHistoryArtifactHandler(
	service protectedHistoryArtifactService,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := strictProtectedHistoryArtifactBody(w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authentication := auth.Authentication.Require(r.Context())
		organizationID := *authentication.CurrentOrgID()
		scope := request.Scope(organizationID)
		retained, err := service.Retain(r.Context(), protectedhistory.CreateRetentionRequest{
			OrganizationID:        organizationID,
			Scope:                 scope,
			IssuerUserAccountID:   authentication.CurrentUserID(),
			ReviewerUserAccountID: request.ReviewerUserAccountID,
			SingleReviewerPilot:   env.ScopedSingleReviewerPilotConfig(),
			IdempotencyKey:        request.IdempotencyKey,
		})
		respondProtectedHistoryArtifactResult(w, r, err, func() {
			RespondJSONWithStatus(w, http.StatusCreated, api.ProtectedHistoryArtifactFromDomain(*retained))
		})
	}
}

func strictProtectedHistoryArtifactBody(
	w http.ResponseWriter,
	r *http.Request,
) (api.CreateProtectedHistoryArtifactRequest, error) {
	var request api.CreateProtectedHistoryArtifactRequest
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
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

func getProtectedHistoryArtifactHandler(
	service protectedHistoryArtifactService,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := protectedHistoryArtifactID(w, r)
		if !ok {
			return
		}
		organizationID := *auth.Authentication.Require(r.Context()).CurrentOrgID()
		retained, err := service.Get(r.Context(), organizationID, id)
		respondProtectedHistoryArtifactResult(w, r, err, func() {
			RespondJSON(w, api.ProtectedHistoryArtifactFromDomain(*retained))
		})
	}
}

func verifyProtectedHistoryArtifactHandler(
	service protectedHistoryArtifactService,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := protectedHistoryArtifactID(w, r)
		if !ok {
			return
		}
		organizationID := *auth.Authentication.Require(r.Context()).CurrentOrgID()
		identity, err := service.Verify(r.Context(), organizationID, id)
		respondProtectedHistoryArtifactResult(w, r, err, func() {
			RespondJSON(w, api.ProtectedHistoryArtifactVerification{
				ProtectedHistoryArtifactID: id,
				ObjectReference:            identity.Reference,
				MediaType:                  identity.MediaType,
				ByteLength:                 identity.ByteLength,
				ContentChecksum:            identity.Checksum,
				VerifiedAt:                 time.Now().UTC(),
			})
		})
	}
}

func protectedHistoryArtifactID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("protectedHistoryArtifactId"))
	if err != nil || id == uuid.Nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func respondProtectedHistoryArtifactResult(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	success func(),
) {
	switch {
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, apierrors.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, apierrors.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, apierrors.ErrConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	case err != nil:
		log := internalctx.GetLogger(r.Context())
		log.Error("failed to handle protected-history artifact request", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	default:
		success()
	}
}
