package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/releasebundles"
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
			type ReleaseBundleEligibilityRequest struct {
				ReleaseBundleIDRequest
				EnvironmentID uuid.UUID `query:"environmentId"`
			}

			r.Get("/", getReleaseBundleHandler()).
				With(option.Description("Get a release bundle")).
				With(option.Request(ReleaseBundleIDRequest{})).
				With(option.Response(http.StatusOK, api.ReleaseBundle{}))

			r.Post("/validate", validateReleaseBundleHandler()).
				With(option.Description("Validate a release bundle")).
				With(option.Request(ReleaseBundleIDRequest{})).
				With(option.Response(http.StatusOK, api.ReleaseBundleValidationResponse{}))

			r.With(releaseBundleEligibilityFeatureFlagMiddleware).
				Get("/eligibility", getReleaseBundleEligibilityHandler()).
				With(option.Description("Explain lifecycle eligibility for a release bundle and environment")).
				With(option.Request(ReleaseBundleEligibilityRequest{})).
				With(option.Response(http.StatusOK, api.ReleaseBundleEligibilityResponse{}))

			r.With(releaseBundleProcessSnapshotFeatureFlagMiddleware).
				Get("/process-snapshot", getReleaseBundleProcessSnapshotHandler()).
				With(option.Description("Get the immutable process snapshot linked to a release bundle")).
				With(option.Request(ReleaseBundleIDRequest{})).
				With(option.Response(http.StatusOK, api.ProcessSnapshot{}))

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

				r.Post("/publish", publishReleaseBundleHandler()).
					With(option.Description("Publish a valid draft release bundle")).
					With(option.Request(ReleaseBundleIDRequest{})).
					With(option.Response(http.StatusOK, api.ReleaseBundle{})).
					With(option.Response(http.StatusBadRequest, api.ReleaseBundleValidationResponse{}))

				r.Post("/block", blockReleaseBundleHandler()).
					With(option.Description("Block a published release bundle")).
					With(option.Request(ReleaseBundleIDRequest{})).
					With(option.Response(http.StatusOK, api.ReleaseBundle{}))

				r.Post("/archive", archiveReleaseBundleHandler()).
					With(option.Description("Archive a published or blocked release bundle")).
					With(option.Request(ReleaseBundleIDRequest{})).
					With(option.Response(http.StatusOK, api.ReleaseBundle{}))
			})
		})

		r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
			Post("/", createReleaseBundleHandler()).
			With(option.Description("Create a draft release bundle")).
			With(option.Request(struct {
				IdempotencyKey string `header:"Idempotency-Key"`
				api.CreateUpdateReleaseBundleRequest
			}{})).
			With(option.Response(http.StatusOK, api.ReleaseBundle{})).
			With(option.Response(http.StatusConflict, api.ErrorResponse{}))
	})
}

func releaseBundleEligibilityFeatureFlagMiddleware(handler http.Handler) http.Handler {
	for _, feature := range []featureflags.Key{
		featureflags.KeyReleaseBundles,
		featureflags.KeyChannels,
		featureflags.KeyLifecycles,
		featureflags.KeyEnvironments,
	} {
		handler = middleware.ExperimentalFeatureFlagMiddleware(feature)(handler)
	}
	return handler
}

func releaseBundleProcessSnapshotFeatureFlagMiddleware(handler http.Handler) http.Handler {
	for _, feature := range []featureflags.Key{
		featureflags.KeyReleaseBundles,
		featureflags.KeyDeploymentProcesses,
	} {
		handler = middleware.ExperimentalFeatureFlagMiddleware(feature)(handler)
	}
	return handler
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

//nolint:dupl
func getReleaseBundleEligibilityHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("releaseBundleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		environmentIDValue := strings.TrimSpace(r.URL.Query().Get("environmentId"))
		if environmentIDValue == "" {
			http.Error(w, "environmentId query parameter is required", http.StatusBadRequest)
			return
		}
		environmentID, err := uuid.Parse(environmentIDValue)
		if err != nil {
			http.Error(w, "environmentId query parameter is invalid", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		result, err := db.GetReleaseBundleEligibility(ctx, id, environmentID, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get release bundle eligibility", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.ReleaseBundleEligibilityToAPI(result))
		}
	}
}

//nolint:dupl
func validateReleaseBundleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("releaseBundleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		result, err := db.ValidateReleaseBundle(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to validate release bundle", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, releaseBundleValidationResponse(result))
		}
	}
}

//nolint:dupl
func getReleaseBundleProcessSnapshotHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("releaseBundleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		snapshot, err := db.GetProcessSnapshotForReleaseBundle(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get release bundle process snapshot", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.ProcessSnapshotToAPI(*snapshot))
		}
	}
}

//nolint:dupl
func publishReleaseBundleHandler() http.HandlerFunc {
	return publishReleaseBundleHandlerWithFlags(env.ExperimentalFeatureFlags())
}

func publishReleaseBundleHandlerWithFlags(enabledFlags []featureflags.Key) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("releaseBundleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)
		if !featureflags.NewRegistry(enabledFlags).IsEnabled(featureflags.KeyOperatorControlPlaneV2) {
			existing, err := db.GetReleaseBundle(ctx, id, *auth.CurrentOrgID())
			if errors.Is(err, apierrors.ErrNotFound) {
				http.NotFound(w, r)
				return
			} else if err != nil {
				log.Error("failed to inspect release bundle before publish", zap.Error(err))
				sentry.GetHubFromContext(ctx).CaptureException(err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if existing.Kind == types.ReleaseBundleKindComponent {
				http.Error(w, "operator control plane v2 is not enabled", http.StatusForbidden)
				return
			}
		}

		bundle, result, err := db.PublishReleaseBundle(ctx, id, *auth.CurrentOrgID(), auth.CurrentUserID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if errors.Is(err, apierrors.ErrBadRequest) {
			RespondJSONWithStatus(w, http.StatusBadRequest, releaseBundleValidationResponse(result))
		} else if errors.Is(err, apierrors.ErrConflict) {
			http.Error(w, "release bundle state transition is invalid", http.StatusConflict)
		} else if err != nil {
			log.Error("failed to publish release bundle", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.ReleaseBundleToAPI(*bundle))
		}
	}
}

//nolint:dupl
func blockReleaseBundleHandler() http.HandlerFunc {
	return releaseBundleTransitionHandler("block", db.BlockReleaseBundle)
}

//nolint:dupl
func archiveReleaseBundleHandler() http.HandlerFunc {
	return releaseBundleTransitionHandler("archive", db.ArchiveReleaseBundle)
}

func releaseBundleTransitionHandler(
	action string,
	transition func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*types.ReleaseBundle, error),
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("releaseBundleId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		bundle, err := transition(ctx, id, *auth.CurrentOrgID(), auth.CurrentUserID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if errors.Is(err, apierrors.ErrConflict) {
			http.Error(w, "release bundle state transition is invalid", http.StatusConflict)
		} else if err != nil {
			log.Error("failed to "+action+" release bundle", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.ReleaseBundleToAPI(*bundle))
		}
	}
}

func createReleaseBundleHandler() http.HandlerFunc {
	return createReleaseBundleHandlerWithFlags(env.ExperimentalFeatureFlags())
}

func createReleaseBundleHandlerWithFlags(enabledFlags []featureflags.Key) http.HandlerFunc {
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
		if !componentReleaseWriteEnabled(request.ReleaseContract, enabledFlags) {
			http.Error(w, "operator control plane v2 is not enabled", http.StatusForbidden)
			return
		}

		bundle := releaseBundleFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		if err := db.CreateReleaseBundleWithIdempotency(ctx, &bundle, r.Header.Get("Idempotency-Key")); err != nil {
			handleReleaseBundleWriteError(w, r, log, "create", err)
			return
		}
		RespondJSON(w, mapping.ReleaseBundleToAPI(bundle))
	}
}

//nolint:dupl
func updateReleaseBundleHandler() http.HandlerFunc {
	return updateReleaseBundleHandlerWithFlags(env.ExperimentalFeatureFlags())
}

func updateReleaseBundleHandlerWithFlags(enabledFlags []featureflags.Key) http.HandlerFunc {
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
		if !componentReleaseWriteEnabled(request.ReleaseContract, enabledFlags) {
			http.Error(w, "operator control plane v2 is not enabled", http.StatusForbidden)
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
	bundle := types.ReleaseBundle{
		OrganizationID:              orgID,
		ApplicationID:               request.ApplicationID,
		ChannelID:                   request.ChannelID,
		DeploymentProcessRevisionID: request.DeploymentProcessRevisionID,
		ReleaseNumber:               strings.TrimSpace(request.ReleaseNumber),
		ReleaseNotes:                request.ReleaseNotes,
		SourceRevision:              strings.TrimSpace(request.SourceRevision),
		ReleaseContract:             request.ReleaseContract,
		Kind:                        types.ReleaseBundleKindLegacy,
		ReleaseContractSchema:       types.ReleaseContractStorageSchemaV1,
		Status:                      types.ReleaseBundleStatusDraft,
		Components:                  components,
	}
	if request.ReleaseContract != nil && request.ReleaseContract.ComponentV2 != nil {
		bundle.Kind = types.ReleaseBundleKindComponent
		bundle.ReleaseContractSchema = types.ReleaseContractSchemaV2
	}
	if request.SourceMetadata != nil {
		bundle.SourceRepository = request.SourceMetadata.Repository
		bundle.SourceBranch = request.SourceMetadata.Branch
		bundle.SourceTag = request.SourceMetadata.Tag
		bundle.CIProvider = request.SourceMetadata.CIProvider
		bundle.CIRunID = request.SourceMetadata.CIRunID
		bundle.CIRunURL = request.SourceMetadata.CIRunURL
	}
	return bundle
}

func componentReleaseWriteEnabled(contract *types.ReleaseContract, enabledFlags []featureflags.Key) bool {
	if contract == nil || contract.ComponentV2 == nil {
		return true
	}
	return featureflags.NewRegistry(enabledFlags).IsEnabled(featureflags.KeyOperatorControlPlaneV2)
}

func releaseBundleResponses(bundles []types.ReleaseBundle) []api.ReleaseBundle {
	return mapping.List(bundles, mapping.ReleaseBundleToAPI)
}

func releaseBundleValidationResponse(result releasebundles.ValidationResult) api.ReleaseBundleValidationResponse {
	errors := make([]api.ReleaseBundleValidationIssue, 0, len(result.Errors))
	for _, issue := range result.Errors {
		errors = append(errors, api.ReleaseBundleValidationIssue{
			Field:   issue.Field,
			Rule:    issue.Rule,
			Message: issue.Message,
		})
	}
	warnings := make([]api.ReleaseBundleValidationIssue, 0, len(result.Warnings))
	for _, issue := range result.Warnings {
		warnings = append(warnings, api.ReleaseBundleValidationIssue{
			Field:   issue.Field,
			Rule:    issue.Rule,
			Message: issue.Message,
		})
	}
	return api.ReleaseBundleValidationResponse{
		Valid:    result.Valid,
		Errors:   errors,
		Warnings: warnings,
	}
}

func handleReleaseBundleWriteError(w http.ResponseWriter, r *http.Request, log *zap.Logger, action string, err error) {
	if errors.Is(err, db.ErrReleaseBundleIdempotencyConflict) {
		RespondJSONWithStatus(w, http.StatusConflict, api.ErrorResponse{
			Code:    api.ErrorCodeIdempotencyKeyReusedWithDifferentRequest,
			Message: "idempotency key was already used with a different release bundle request",
		})
	} else if errors.Is(err, apierrors.ErrAlreadyExists) {
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
