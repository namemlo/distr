package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/channelrules"
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

func ChannelsRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Channels"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyChannels),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getChannelsHandler()).
			With(option.Description("List channels")).
			With(option.Response(http.StatusOK, []api.Channel{}))

		r.Route("/{channelId}", func(r chiopenapi.Router) {
			type ChannelIDRequest struct {
				ChannelID uuid.UUID `path:"channelId"`
			}

			r.Get("/", getChannelHandler()).
				With(option.Description("Get a channel")).
				With(option.Request(ChannelIDRequest{})).
				With(option.Response(http.StatusOK, api.Channel{}))

			r.Post("/validate-version", validateChannelVersionHandler()).
				With(option.Description("Validate a version and optional source against channel rules")).
				With(option.Request(struct {
					ChannelIDRequest
					api.ValidateChannelVersionRequest
				}{})).
				With(option.Response(http.StatusOK, api.ChannelVersionValidationResponse{}))

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).Group(func(r chiopenapi.Router) {
				r.Put("/", updateChannelHandler()).
					With(option.Description("Update a channel")).
					With(option.Request(struct {
						ChannelIDRequest
						api.CreateUpdateChannelRequest
					}{})).
					With(option.Response(http.StatusOK, api.Channel{}))

				r.Delete("/", deleteChannelHandler()).
					With(option.Description("Delete a channel")).
					With(option.Request(ChannelIDRequest{}))
			})
		})

		r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
			Post("/", createChannelHandler()).
			With(option.Description("Create a channel")).
			With(option.Request(api.CreateUpdateChannelRequest{})).
			With(option.Response(http.StatusOK, api.Channel{}))
	})
}

func getChannelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		channels, err := db.GetChannelsByOrganizationID(ctx, *auth.CurrentOrgID())
		if err != nil {
			log.Error("failed to get channels", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, channelResponses(channels))
	}
}

//nolint:dupl
func getChannelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("channelId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		channel, err := db.GetChannel(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get channel", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.ChannelToAPI(*channel))
		}
	}
}

//nolint:dupl
func validateChannelVersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("channelId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.ValidateChannelVersionRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		channel, err := db.GetChannel(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			log.Error("failed to get channel for validation", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		result, err := channelrules.Evaluate(channelRulesFromChannel(*channel), channelrules.Input{
			Version:      request.Version,
			SourceBranch: request.SourceBranch,
			SourceTag:    request.SourceTag,
		})
		if err != nil {
			log.Error("failed to evaluate channel rules", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, channelVersionValidationResponse(result))
	}
}

func createChannelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateChannelRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		channel := channelFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		if err := db.CreateChannel(ctx, &channel); err != nil {
			handleChannelWriteError(w, r, log, "create", err)
			return
		}
		RespondJSON(w, mapping.ChannelToAPI(channel))
	}
}

//nolint:dupl
func updateChannelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("channelId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateChannelRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		channel := channelFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		channel.ID = id
		if err := db.UpdateChannel(ctx, &channel); err != nil {
			handleChannelWriteError(w, r, log, "update", err)
			return
		}
		RespondJSON(w, mapping.ChannelToAPI(channel))
	}
}

//nolint:dupl
func deleteChannelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("channelId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		if err := db.DeleteChannelWithID(ctx, id, *auth.CurrentOrgID()); errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if errors.Is(err, apierrors.ErrConflict) {
			http.Error(w, "channel is in use", http.StatusConflict)
		} else if err != nil {
			log.Error("failed to delete channel", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func channelFromCreateUpdateRequest(orgID uuid.UUID, request api.CreateUpdateChannelRequest) types.Channel {
	rules, _ := channelrules.NormalizeRules(channelrules.Rules{
		AllowedVersionRanges:        request.AllowedVersionRanges,
		AllowedPrereleasePatterns:   request.AllowedPrereleasePatterns,
		AllowedSourceBranchPatterns: request.AllowedSourceBranchPatterns,
		AllowedSourceTagPatterns:    request.AllowedSourceTagPatterns,
	})
	return types.Channel{
		OrganizationID:              orgID,
		ApplicationID:               request.ApplicationID,
		LifecycleID:                 request.LifecycleID,
		Name:                        strings.TrimSpace(request.Name),
		Description:                 request.Description,
		SortOrder:                   request.SortOrder,
		IsDefault:                   request.IsDefault,
		AllowedVersionRanges:        rules.AllowedVersionRanges,
		AllowedPrereleasePatterns:   rules.AllowedPrereleasePatterns,
		AllowedSourceBranchPatterns: rules.AllowedSourceBranchPatterns,
		AllowedSourceTagPatterns:    rules.AllowedSourceTagPatterns,
	}
}

func channelResponses(channels []types.Channel) []api.Channel {
	return mapping.List(channels, mapping.ChannelToAPI)
}

func channelRulesFromChannel(channel types.Channel) channelrules.Rules {
	return channelrules.Rules{
		AllowedVersionRanges:        channel.AllowedVersionRanges,
		AllowedPrereleasePatterns:   channel.AllowedPrereleasePatterns,
		AllowedSourceBranchPatterns: channel.AllowedSourceBranchPatterns,
		AllowedSourceTagPatterns:    channel.AllowedSourceTagPatterns,
	}
}

func channelVersionValidationResponse(result channelrules.Result) api.ChannelVersionValidationResponse {
	errors := make([]api.ChannelValidationError, 0, len(result.Issues))
	for _, issue := range result.Issues {
		errors = append(errors, api.ChannelValidationError{
			Field:   issue.Field,
			Rule:    issue.Rule,
			Message: issue.Message,
		})
	}
	return api.ChannelVersionValidationResponse{
		Valid:  result.Valid,
		Errors: errors,
	}
}

func handleChannelWriteError(w http.ResponseWriter, r *http.Request, log *zap.Logger, action string, err error) {
	if errors.Is(err, apierrors.ErrAlreadyExists) {
		http.Error(w, "a channel with this name already exists for this application", http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrNotFound) {
		http.NotFound(w, r)
	} else if errors.Is(err, apierrors.ErrConflict) {
		http.Error(w, "channel is in use", http.StatusConflict)
	} else {
		log.Error("failed to "+action+" channel", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
