package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/targetconfig"
	"github.com/distr-sh/distr/internal/types"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

const maxTargetConfigSnapshotRequestBytes = 512 * 1024

type targetConfigSnapshotIDRequest struct {
	SnapshotID uuid.UUID `path:"snapshotId"`
}

type targetConfigVerifierFactory func(context.Context) targetconfig.ObjectVerifier

func TargetConfigSnapshotsRouter(r chiopenapi.Router) {
	TargetConfigSnapshotsRouterWithVerifier(targetconfig.NewUnavailableObjectVerifier())(r)
}

func TargetConfigSnapshotsRouterWithVerifier(
	verifier targetconfig.ObjectVerifier,
) func(chiopenapi.Router) {
	if verifier == nil {
		verifier = targetconfig.NewUnavailableObjectVerifier()
	}
	return func(r chiopenapi.Router) {
		targetConfigSnapshotsRouterWithDependencies(
			r,
			env.ExperimentalFeatureFlags(),
			func(context.Context) targetconfig.ObjectVerifier {
				return verifier
			},
		)
	}
}

func targetConfigSnapshotsRouterWithDependencies(
	r chiopenapi.Router,
	enabledFlags []featureflags.Key,
	verifierFactory targetConfigVerifierFactory,
) {
	mutationAccess := targetConfigMutationAccessMiddlewareWithFlags(enabledFlags)
	r.WithOptions(option.GroupTags("Target Config Snapshots"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
	).Group(func(r chiopenapi.Router) {
		r.Get("/", listTargetConfigSnapshotsHandler()).
			With(option.Description("List immutable target config snapshots")).
			With(option.Request(api.TargetConfigSnapshotListRequest{})).
			With(option.Response(http.StatusOK, api.TargetConfigSnapshotPage{})).
			With(targetConfigPlainTextResponses(http.StatusBadRequest, http.StatusForbidden)...)
		r.With(mutationAccess).
			Post("/", createTargetConfigSnapshotHandler()).
			With(option.Description("Create an immutable target config snapshot")).
			With(option.Request(api.CreateTargetConfigSnapshotRequest{})).
			With(option.Response(http.StatusOK, api.TargetConfigSnapshot{})).
			With(targetConfigPlainTextResponses(
				http.StatusBadRequest,
				http.StatusForbidden,
				http.StatusNotFound,
				http.StatusConflict,
			)...)
		r.Route("/{snapshotId}", func(r chiopenapi.Router) {
			r.Get("/", getTargetConfigSnapshotHandler()).
				With(option.Description("Get an immutable target config snapshot")).
				With(option.Request(targetConfigSnapshotIDRequest{})).
				With(option.Response(http.StatusOK, api.TargetConfigSnapshot{})).
				With(targetConfigPlainTextResponses(
					http.StatusForbidden,
					http.StatusNotFound,
				)...)
			r.With(mutationAccess).
				Post("/verify", verifyTargetConfigSnapshotHandler(verifierFactory)).
				With(option.Description("Verify immutable target config snapshot objects")).
				With(option.Request(targetConfigSnapshotIDRequest{})).
				With(option.Response(http.StatusOK, api.TargetConfigObjectVerificationResult{})).
				With(targetConfigPlainTextResponses(
					http.StatusForbidden,
					http.StatusNotFound,
				)...)
		})
	})
}

func targetConfigPlainTextResponses(statuses ...int) []option.OperationOption {
	responses := make([]option.OperationOption, 0, len(statuses))
	for _, status := range statuses {
		responses = append(
			responses,
			option.Response(status, "", option.ContentType("text/plain")),
		)
	}
	return responses
}

func targetConfigMutationAccessMiddlewareWithFlags(
	enabledFlags []featureflags.Key,
) func(http.Handler) http.Handler {
	return deploymentRegistryMutationAccessMiddlewareWithFlags(enabledFlags)
}

func createTargetConfigSnapshotHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request api.CreateTargetConfigSnapshotRequest
		if !decodeStrictTargetConfigJSON(w, r, &request) {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		draft := request.ToDraft(*userAuth.CurrentOrgID())
		draft.CreatedByUserAccountID = userAuth.CurrentUserID()
		snapshot, err := db.CreateTargetConfigSnapshot(ctx, &draft)
		if err != nil {
			handleTargetConfigSnapshotError(w, r, "create", err)
			return
		}
		RespondJSON(w, mapping.TargetConfigSnapshotToAPI(*snapshot))
	}
}

func listTargetConfigSnapshotsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		request, err := api.TargetConfigSnapshotListRequestFromQuery(
			query.Get("deploymentUnitId"),
			query.Get("targetEnvironmentAssignmentId"),
			query.Get("limit"),
			query.Get("cursor"),
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		page, err := db.ListTargetConfigSnapshots(ctx, types.TargetConfigListFilter{
			OrganizationID:                *userAuth.CurrentOrgID(),
			DeploymentUnitID:              request.DeploymentUnitID,
			TargetEnvironmentAssignmentID: request.TargetEnvironmentAssignmentID,
			Cursor:                        request.Cursor,
			Limit:                         request.Limit,
		})
		if err != nil {
			handleTargetConfigSnapshotError(w, r, "list", err)
			return
		}
		RespondJSON(w, mapping.TargetConfigSnapshotPageToAPI(page))
	}
}

func getTargetConfigSnapshotHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snapshotID, ok := targetConfigSnapshotPathID(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		snapshot, err := db.GetTargetConfigSnapshot(ctx, *userAuth.CurrentOrgID(), snapshotID)
		if err != nil {
			handleTargetConfigSnapshotError(w, r, "get", err)
			return
		}
		RespondJSON(w, mapping.TargetConfigSnapshotToAPI(*snapshot))
	}
}

func verifyTargetConfigSnapshotHandler(
	verifierFactory targetConfigVerifierFactory,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snapshotID, ok := targetConfigSnapshotPathID(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		snapshot, err := db.GetTargetConfigSnapshot(ctx, *userAuth.CurrentOrgID(), snapshotID)
		if err != nil {
			handleTargetConfigSnapshotError(w, r, "get for verification", err)
			return
		}
		result, err := targetconfig.VerifyObjects(ctx, *snapshot, verifierFactory(ctx))
		if err != nil {
			handleTargetConfigSnapshotError(w, r, "verify", err)
			return
		}
		RespondJSON(w, mapping.TargetConfigVerificationResultToAPI(*result))
	}
}

func targetConfigSnapshotPathID(
	w http.ResponseWriter,
	r *http.Request,
) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("snapshotId"))
	if err != nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func decodeStrictTargetConfigJSON(
	w http.ResponseWriter,
	r *http.Request,
	target any,
) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxTargetConfigSnapshotRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

func handleTargetConfigSnapshotError(
	w http.ResponseWriter,
	r *http.Request,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, apierrors.ErrAlreadyExists):
		http.Error(w, "target config snapshot already exists", http.StatusConflict)
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		logger := internalctx.GetLogger(r.Context())
		logger.Error("failed to "+action+" target config snapshot", zap.Error(err))
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.CaptureException(err)
		}
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
