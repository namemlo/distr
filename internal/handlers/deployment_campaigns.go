package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
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

type deploymentCampaignDraftIDRequest struct {
	CampaignDraftID uuid.UUID `path:"campaignDraftId"`
}

type campaignActionAuthorizer interface {
	AuthorizeCampaignAction(
		context.Context,
		types.CampaignAuthorizationContext,
	) error
}

type campaignActionAuthorizerFunc func(
	context.Context,
	types.CampaignAuthorizationContext,
) error

func (fn campaignActionAuthorizerFunc) AuthorizeCampaignAction(
	ctx context.Context,
	request types.CampaignAuthorizationContext,
) error {
	return fn(ctx, request)
}

type scopedCampaignActionAuthorizer struct {
	dependencies controlPlaneResourceAuthorizationDependencies
}

func (authorizer scopedCampaignActionAuthorizer) AuthorizeCampaignAction(
	ctx context.Context,
	request types.CampaignAuthorizationContext,
) error {
	authInfo, err := auth.Authentication.Get(ctx)
	if err != nil ||
		authInfo.CurrentOrgID() == nil ||
		*authInfo.CurrentOrgID() != request.OrganizationID ||
		authInfo.CurrentUserID() != request.ActorUserID {
		return apierrors.ErrForbidden
	}
	return authorizeControlPlaneResourceWithDependencies(
		ctx,
		controlPlaneResourceAuthorizationRequest{
			OrganizationID: request.OrganizationID,
			PrincipalID:    request.ActorUserID,
			CredentialRole: authInfo.CurrentUserRole(),
			IsSuperAdmin:   authInfo.IsSuperAdmin(),
			Action:         types.ActionCampaignControl,
			Resource: types.ResourceRef{
				OrganizationID: request.OrganizationID,
				Kind:           types.PermissionScopeCampaign,
				ID:             request.CampaignDraftID,
			},
			DecisionAt: time.Now().UTC(),
		},
		authorizer.dependencies,
	)
}

func newCampaignActionAuthorizer() campaignActionAuthorizer {
	return scopedCampaignActionAuthorizer{
		dependencies: defaultControlPlaneResourceAuthorizationDependencies(),
	}
}

func DeploymentCampaignDraftsRouter(r chiopenapi.Router) {
	deploymentCampaignDraftsRouterWithDependencies(
		r,
		env.ExperimentalFeatureFlags(),
		newCampaignActionAuthorizer(),
	)
}

func deploymentCampaignDraftsRouterWithDependencies(
	r chiopenapi.Router,
	enabledFlags []featureflags.Key,
	authorizer campaignActionAuthorizer,
) {
	mutationAccess := operatorControlPlaneMutationAccessMiddlewareWithFlags(enabledFlags)
	r.WithOptions(option.GroupTags("Deployment Campaigns"))
	r.With(middleware.RequireVendor, middleware.RequireOrgAndRole).Group(
		func(r chiopenapi.Router) {
			r.With(mutationAccess).Post("/", createDeploymentCampaignDraftHandler(authorizer)).
				With(option.Description("Create an editable deployment campaign draft")).
				With(option.Request(api.CreateDeploymentCampaignDraftRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentCampaignDraft{}))
			r.Route("/{campaignDraftId}", func(r chiopenapi.Router) {
				r.Get("/", getDeploymentCampaignDraftHandler()).
					With(option.Description("Get a deployment campaign draft")).
					With(option.Request(deploymentCampaignDraftIDRequest{})).
					With(option.Response(http.StatusOK, api.DeploymentCampaignDraft{}))
				r.With(mutationAccess).Patch(
					"/",
					updateDeploymentCampaignDraftHandler(authorizer),
				).With(option.Description("Edit a deployment campaign draft")).
					With(option.Request(struct {
						deploymentCampaignDraftIDRequest
						api.UpdateDeploymentCampaignDraftRequest
					}{})).
					With(option.Response(http.StatusOK, api.DeploymentCampaignDraft{}))
				r.With(mutationAccess).Post(
					"/validate",
					validateDeploymentCampaignDraftHandler(authorizer),
				).With(option.Description("Validate frozen campaign inputs")).
					With(option.Request(deploymentCampaignDraftIDRequest{})).
					With(option.Response(
						http.StatusOK,
						api.DeploymentCampaignValidationResponse{},
					))
				r.With(mutationAccess).Post(
					"/publish",
					publishDeploymentCampaignRevisionHandler(authorizer),
				).With(option.Description("Publish an immutable campaign revision")).
					With(option.Request(struct {
						deploymentCampaignDraftIDRequest
						api.PublishDeploymentCampaignRevisionRequest
					}{})).
					With(option.Response(
						http.StatusOK,
						api.DeploymentCampaignRevision{},
					))
			})
		},
	)
}

func createDeploymentCampaignDraftHandler(
	authorizer campaignActionAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := deploymentPolicyJSONBody[api.CreateDeploymentCampaignDraftRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		draftID := uuid.New()
		if err := authorizeCampaignDraftMutation(
			r.Context(),
			authorizer,
			*authInfo.CurrentOrgID(),
			authInfo.CurrentUserID(),
			draftID,
		); err != nil {
			handleDeploymentCampaignError(w, r, "authorize campaign draft", err)
			return
		}
		draft := request.ToDomain(
			*authInfo.CurrentOrgID(),
			authInfo.CurrentUserID(),
		)
		draft.ID = draftID
		if err := db.CreateDeploymentCampaignDraft(r.Context(), &draft); err != nil {
			handleDeploymentCampaignError(w, r, "create campaign draft", err)
			return
		}
		RespondJSON(w, mapping.CampaignDraftToAPI(draft))
	}
}

func getDeploymentCampaignDraftHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		draftID, ok := deploymentCampaignDraftPathID(w, r)
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		draft, err := db.GetDeploymentCampaignDraft(
			r.Context(),
			draftID,
			*authInfo.CurrentOrgID(),
		)
		if err != nil {
			handleDeploymentCampaignError(w, r, "get campaign draft", err)
			return
		}
		RespondJSON(w, mapping.CampaignDraftToAPI(*draft))
	}
}

func updateDeploymentCampaignDraftHandler(
	authorizer campaignActionAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		draftID, ok := deploymentCampaignDraftPathID(w, r)
		if !ok {
			return
		}
		request, err := deploymentPolicyJSONBody[api.UpdateDeploymentCampaignDraftRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		if err := authorizeCampaignDraftMutation(
			r.Context(),
			authorizer,
			*authInfo.CurrentOrgID(),
			authInfo.CurrentUserID(),
			draftID,
		); err != nil {
			handleDeploymentCampaignError(w, r, "authorize campaign draft edit", err)
			return
		}
		draft := request.CreateDeploymentCampaignDraftRequest.ToDomain(
			*authInfo.CurrentOrgID(),
			authInfo.CurrentUserID(),
		)
		draft.ID = draftID
		if err := db.UpdateDeploymentCampaignDraft(
			r.Context(),
			&draft,
			request.ExpectedRevision,
		); err != nil {
			handleDeploymentCampaignError(w, r, "update campaign draft", err)
			return
		}
		RespondJSON(w, mapping.CampaignDraftToAPI(draft))
	}
}

func validateDeploymentCampaignDraftHandler(
	authorizer campaignActionAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		draftID, ok := deploymentCampaignDraftPathID(w, r)
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		if err := authorizeCampaignDraftMutation(
			r.Context(),
			authorizer,
			*authInfo.CurrentOrgID(),
			authInfo.CurrentUserID(),
			draftID,
		); err != nil {
			handleDeploymentCampaignError(w, r, "authorize campaign validation", err)
			return
		}
		issues, err := db.ValidateStoredDeploymentCampaignDraft(
			r.Context(),
			draftID,
			*authInfo.CurrentOrgID(),
		)
		if err != nil {
			handleDeploymentCampaignError(w, r, "validate campaign draft", err)
			return
		}
		RespondJSON(w, api.DeploymentCampaignValidationResponse{
			Valid:  len(issues) == 0,
			Issues: issues,
		})
	}
}

func publishDeploymentCampaignRevisionHandler(
	authorizer campaignActionAuthorizer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		draftID, ok := deploymentCampaignDraftPathID(w, r)
		if !ok {
			return
		}
		request, err := deploymentPolicyJSONBody[api.PublishDeploymentCampaignRevisionRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		if err := authorizeCampaignDraftMutation(
			r.Context(),
			authorizer,
			*authInfo.CurrentOrgID(),
			authInfo.CurrentUserID(),
			draftID,
		); err != nil {
			handleDeploymentCampaignError(w, r, "authorize campaign publication", err)
			return
		}
		ctx := types.WithCampaignPublicationContext(
			r.Context(),
			types.CampaignPublicationContext{
				OrganizationID: *authInfo.CurrentOrgID(),
				ActorUserID:    authInfo.CurrentUserID(),
			},
		)
		revision, err := db.PublishCampaignRevision(
			ctx,
			draftID,
			strings.TrimSpace(request.IdempotencyKey),
		)
		if err != nil {
			handleDeploymentCampaignError(w, r, "publish campaign revision", err)
			return
		}
		RespondJSON(w, mapping.CampaignRevisionToAPI(*revision))
	}
}

func authorizeCampaignDraftMutation(
	ctx context.Context,
	authorizer campaignActionAuthorizer,
	organizationID uuid.UUID,
	actorUserID uuid.UUID,
	campaignDraftID uuid.UUID,
) error {
	return authorizer.AuthorizeCampaignAction(
		ctx,
		types.CampaignAuthorizationContext{
			OrganizationID:  organizationID,
			ActorUserID:     actorUserID,
			CampaignDraftID: campaignDraftID,
		},
	)
}

func deploymentCampaignDraftPathID(
	w http.ResponseWriter,
	r *http.Request,
) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("campaignDraftId"))
	if err != nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func handleDeploymentCampaignError(
	w http.ResponseWriter,
	r *http.Request,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, apierrors.ErrForbidden):
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
	case errors.Is(err, apierrors.ErrConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		internalctx.GetLogger(r.Context()).Error(action+" failed", zap.Error(err))
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.CaptureException(err)
		}
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
	}
}
