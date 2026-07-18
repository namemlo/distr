package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
)

type CampaignControlService interface {
	ApplyCampaignControl(
		context.Context,
		types.CampaignControlInput,
	) (*types.CampaignControlResult, error)
	ExcludeCampaignMember(
		context.Context,
		types.CampaignMemberControlInput,
	) (*types.CampaignExclusion, error)
	RetryCampaignMember(
		context.Context,
		types.CampaignMemberControlInput,
	) (*types.DeploymentPlan, error)
}

func DeploymentCampaignControlRoutePaths() []string {
	return []string{
		"POST /api/v1/deployment-campaigns/{id}/pause",
		"POST /api/v1/deployment-campaigns/{id}/resume",
		"POST /api/v1/deployment-campaigns/{id}/retry",
		"POST /api/v1/deployment-campaigns/{id}/exclude",
		"POST /api/v1/deployment-campaigns/{id}/cancel",
	}
}

func DeploymentCampaignControlsRouter(
	r chiopenapi.Router,
	service CampaignControlService,
) {
	type CampaignControlRouteRequest struct {
		ID uuid.UUID `path:"id"`
		api.CampaignControlRequest
	}
	type CampaignMemberControlRouteRequest struct {
		ID uuid.UUID `path:"id"`
		api.CampaignMemberControlRequest
	}
	r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
		Post(
			"/deployment-campaigns/{id}/pause",
			campaignControlHandler(service, types.CampaignControlKindPause),
		).
		With(option.Request(CampaignControlRouteRequest{})).
		With(option.Response(http.StatusOK, api.DeploymentCampaignControlResult{}))
	r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
		Post(
			"/deployment-campaigns/{id}/resume",
			campaignControlHandler(service, types.CampaignControlKindResume),
		).
		With(option.Request(CampaignControlRouteRequest{})).
		With(option.Response(http.StatusOK, api.DeploymentCampaignControlResult{}))
	r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
		Post(
			"/deployment-campaigns/{id}/cancel",
			campaignControlHandler(service, types.CampaignControlKindCancel),
		).
		With(option.Request(CampaignControlRouteRequest{})).
		With(option.Response(http.StatusOK, api.DeploymentCampaignControlResult{}))
	r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
		Post(
			"/deployment-campaigns/{id}/exclude",
			campaignMemberControlHandler(service, false),
		).
		With(option.Request(CampaignMemberControlRouteRequest{})).
		With(option.Response(http.StatusOK, api.DeploymentCampaignExclusion{}))
	r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
		Post(
			"/deployment-campaigns/{id}/retry",
			campaignMemberControlHandler(service, true),
		).
		With(option.Request(CampaignMemberControlRouteRequest{})).
		With(option.Response(http.StatusOK, api.DeploymentPlan{}))
}

func campaignControlHandler(
	service CampaignControlService,
	kind types.CampaignControlKind,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		request, err := JsonBody[api.CampaignControlRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		result, err := service.ApplyCampaignControl(r.Context(), types.CampaignControlInput{
			RequestID:       request.RequestID,
			OrganizationID:  *authInfo.CurrentOrgID(),
			RunID:           runID,
			ActorID:         authInfo.CurrentUserID(),
			ExpectedVersion: request.ExpectedVersion,
			Kind:            kind,
			Reason:          request.Reason,
			RequestedAt:     time.Now().UTC(),
		})
		if writeCampaignControlError(w, r, err) {
			return
		}
		RespondJSON(w, mapping.DeploymentCampaignControlResultToAPI(*result))
	}
}

func campaignMemberControlHandler(
	service CampaignControlService,
	retry bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		request, err := JsonBody[api.CampaignMemberControlRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(retry); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		input := types.CampaignMemberControlInput{
			CampaignControlInput: types.CampaignControlInput{
				RequestID:       request.RequestID,
				OrganizationID:  *authInfo.CurrentOrgID(),
				RunID:           runID,
				ActorID:         authInfo.CurrentUserID(),
				ExpectedVersion: request.ExpectedVersion,
				Reason:          request.Reason,
				RequestedAt:     time.Now().UTC(),
			},
			MemberRunID:     request.MemberRunID,
			ProtocolVersion: request.ProtocolVersion,
		}
		if retry {
			input.Kind = types.CampaignControlKindRetry
			plan, err := service.RetryCampaignMember(r.Context(), input)
			if writeCampaignControlError(w, r, err) {
				return
			}
			RespondJSON(w, mapping.DeploymentPlanToAPI(*plan))
			return
		}
		input.Kind = types.CampaignControlKindExclude
		exclusion, err := service.ExcludeCampaignMember(r.Context(), input)
		if writeCampaignControlError(w, r, err) {
			return
		}
		RespondJSON(w, mapping.DeploymentCampaignExclusionToAPI(*exclusion))
	}
}

func writeCampaignControlError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, apierrors.ErrConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, apierrors.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return true
}
