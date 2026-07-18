package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
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

type approvalRequestIDRoute struct {
	ApprovalRequestID uuid.UUID `path:"approvalRequestId"`
}

type approvalAuthorizationRequest struct {
	OrganizationID        uuid.UUID
	ActorUserAccountID    uuid.UUID
	CredentialRole        *types.UserRole
	IsSuperAdmin          bool
	Action                string
	DecisionAt            time.Time
	DeploymentPlanID      uuid.UUID
	ApprovalRequestID     uuid.UUID
	ApprovalRequirementID uuid.UUID
}

type approvalHandlerDependencies struct {
	requestApproval func(
		context.Context,
		types.ApprovalRequestInput,
	) (*types.ApprovalRequest, error)
	listRequests func(
		context.Context,
		types.ApprovalRequestListFilter,
	) (types.Page[types.ApprovalRequest], error)
	getRequest func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (*types.ApprovalRequest, error)
	recordDecision func(
		context.Context,
		types.ApprovalDecisionInput,
	) (*types.ApprovalDecision, error)
	authorizeRequest  func(context.Context, approvalAuthorizationRequest) error
	authorizeDecision func(context.Context, approvalAuthorizationRequest) error
	clock             func() time.Time
}

func defaultApprovalHandlerDependencies() approvalHandlerDependencies {
	return approvalHandlerDependencies{
		requestApproval:   db.RequestApproval,
		listRequests:      db.ListApprovalRequests,
		getRequest:        db.GetApprovalRequest,
		recordDecision:    db.RecordApprovalDecision,
		authorizeRequest:  approvalScopedAuthorizationUnavailable,
		authorizeDecision: approvalScopedAuthorizationUnavailable,
		clock: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func approvalScopedAuthorizationUnavailable(
	context.Context,
	approvalAuthorizationRequest,
) error {
	// PR-066 supplies the action/scope evaluator. Keeping this adapter closed
	// prevents legacy role middleware from becoming approval authority while the
	// speculative PR stack is waiting to be rebased onto that slice.
	return apierrors.NewForbidden(
		"scoped approval authorization is unavailable until PR-066 is integrated",
	)
}

func ApprovalRequestsRouter(r chiopenapi.Router) {
	dependencies := defaultApprovalHandlerDependencies()
	mutationAccess := approvalMutationAccessMiddlewareWithFlags(
		env.ExperimentalFeatureFlags(),
	)
	r.WithOptions(option.GroupTags("Approval Requests"))
	r.With(middleware.RequireVendor, middleware.RequireOrgAndRole).Group(
		func(r chiopenapi.Router) {
			r.Get("/", listApprovalRequestsHandlerWithDependencies(dependencies)).
				With(option.Description("List checksum-bound approval work using keyset pagination")).
				With(option.Request(api.ApprovalRequestListRequest{})).
				With(option.Response(http.StatusOK, api.ApprovalRequestPage{}))
			r.Route("/{approvalRequestId}", func(r chiopenapi.Router) {
				r.Get("/", getApprovalRequestHandlerWithDependencies(dependencies)).
					With(option.Description("Get immutable approval evidence and decisions")).
					With(option.Request(approvalRequestIDRoute{})).
					With(option.Response(http.StatusOK, api.ApprovalRequest{}))
				r.With(mutationAccess).Post(
					"/decisions",
					recordApprovalDecisionHandlerWithDependencies(dependencies),
				).With(option.Description("Append a scoped checksum-bound approval decision")).
					With(option.Request(struct {
						approvalRequestIDRoute
						api.RecordApprovalDecisionRequest
					}{})).
					With(option.Response(http.StatusOK, api.ApprovalDecision{}))
			})
		},
	)
}

func approvalMutationAccessMiddlewareWithFlags(
	enabledFlags []featureflags.Key,
) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				handler.ServeHTTP(w, r)
			default:
				if !featureflags.NewRegistry(enabledFlags).
					IsEnabled(featureflags.KeyOperatorControlPlaneV2) {
					http.NotFound(w, r)
					return
				}
				handler.ServeHTTP(w, r)
			}
		})
	}
}

func approvalMutationAccessMiddleware() func(http.Handler) http.Handler {
	return approvalMutationAccessMiddlewareWithFlags(env.ExperimentalFeatureFlags())
}

func createApprovalRequestHandler() http.HandlerFunc {
	return createApprovalRequestHandlerWithDependencies(
		defaultApprovalHandlerDependencies(),
	)
}

func createApprovalRequestHandlerWithDependencies(
	dependencies approvalHandlerDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		planID, ok := approvalPathID(w, r, "deploymentPlanId")
		if !ok {
			return
		}
		request, err := approvalJSONBody[api.CreateApprovalRequestRequest](w, r)
		if err != nil {
			return
		}
		now := dependencies.clock()
		if err := request.Validate(now); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		authorizationRequest := approvalAuthorizationRequest{
			OrganizationID:     *authInfo.CurrentOrgID(),
			ActorUserAccountID: authInfo.CurrentUserID(),
			CredentialRole:     authInfo.CurrentUserRole(),
			IsSuperAdmin:       authInfo.IsSuperAdmin(),
			Action:             "plan.publish",
			DeploymentPlanID:   planID,
		}
		created, err := dependencies.requestApproval(
			r.Context(),
			types.ApprovalRequestInput{
				OrganizationID:           authorizationRequest.OrganizationID,
				DeploymentPlanID:         planID,
				RequestedByUserAccountID: authorizationRequest.ActorUserAccountID,
				ExpiresAt:                request.ExpiresAt,
				Authorize: func(
					ctx context.Context,
					evidence types.ApprovalAuthorizationContext,
				) error {
					authorizationRequest.DecisionAt = evidence.DecisionAt
					authorizationRequest.DeploymentPlanID = evidence.DeploymentPlanID
					return dependencies.authorizeRequest(ctx, authorizationRequest)
				},
			},
		)
		if err != nil {
			handleApprovalError(w, r, "request deployment approval", err)
			return
		}
		RespondJSON(w, mapping.ApprovalRequestToAPI(*created))
	}
}

func listApprovalRequestsHandlerWithDependencies(
	dependencies approvalHandlerDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := approvalListRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		page, err := dependencies.listRequests(
			r.Context(),
			types.ApprovalRequestListFilter{
				OrganizationID: *authInfo.CurrentOrgID(),
				State:          request.State,
				Cursor:         request.Cursor,
				Limit:          request.Limit,
			},
		)
		if err != nil {
			handleApprovalError(w, r, "list approval requests", err)
			return
		}
		RespondJSON(w, mapping.ApprovalRequestPageToAPI(page))
	}
}

func getApprovalRequestHandlerWithDependencies(
	dependencies approvalHandlerDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, ok := approvalPathID(w, r, "approvalRequestId")
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		request, err := dependencies.getRequest(
			r.Context(),
			requestID,
			*authInfo.CurrentOrgID(),
		)
		if err != nil {
			handleApprovalError(w, r, "get approval request", err)
			return
		}
		RespondJSON(w, mapping.ApprovalRequestToAPI(*request))
	}
}

func recordApprovalDecisionHandlerWithDependencies(
	dependencies approvalHandlerDependencies,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, ok := approvalPathID(w, r, "approvalRequestId")
		if !ok {
			return
		}
		request, err := approvalJSONBody[api.RecordApprovalDecisionRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		authorizationRequest := approvalAuthorizationRequest{
			OrganizationID:        *authInfo.CurrentOrgID(),
			ActorUserAccountID:    authInfo.CurrentUserID(),
			CredentialRole:        authInfo.CurrentUserRole(),
			IsSuperAdmin:          authInfo.IsSuperAdmin(),
			Action:                "approval.decide",
			ApprovalRequestID:     requestID,
			ApprovalRequirementID: request.ApprovalRequirementID,
		}
		decision, err := dependencies.recordDecision(
			r.Context(),
			types.ApprovalDecisionInput{
				OrganizationID:          authorizationRequest.OrganizationID,
				ApprovalRequestID:       requestID,
				ApprovalRequirementID:   request.ApprovalRequirementID,
				ActorUserAccountID:      authorizationRequest.ActorUserAccountID,
				Decision:                request.Decision,
				Comment:                 request.Comment,
				ExpectedRequestRevision: request.ExpectedRequestRevision,
				IdempotencyKey:          request.IdempotencyKey,
				Authorize: func(
					ctx context.Context,
					evidence types.ApprovalAuthorizationContext,
				) error {
					authorizationRequest.DecisionAt = evidence.DecisionAt
					authorizationRequest.DeploymentPlanID = evidence.DeploymentPlanID
					authorizationRequest.ApprovalRequestID = evidence.ApprovalRequestID
					authorizationRequest.ApprovalRequirementID =
						evidence.ApprovalRequirementID
					return dependencies.authorizeDecision(ctx, authorizationRequest)
				},
			},
		)
		if err != nil {
			handleApprovalError(w, r, "record approval decision", err)
			return
		}
		RespondJSON(w, mapping.ApprovalDecisionToAPI(*decision))
	}
}

func approvalJSONBody[T any](
	w http.ResponseWriter,
	r *http.Request,
) (T, error) {
	var value T
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return value, err
	}
	var trailing any
	err := decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		return value, err
	}
	return value, nil
}

func approvalListRequest(r *http.Request) (api.ApprovalRequestListRequest, error) {
	request := api.ApprovalRequestListRequest{
		State:  types.ApprovalRequestState(r.URL.Query().Get("state")),
		Cursor: r.URL.Query().Get("cursor"),
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil {
			return request, apierrors.NewBadRequest("limit must be a number")
		}
		request.Limit = limit
	}
	if err := request.Validate(); err != nil {
		return request, err
	}
	return request, nil
}

func approvalPathID(
	w http.ResponseWriter,
	r *http.Request,
	key string,
) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(key))
	if err != nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func handleApprovalError(
	w http.ResponseWriter,
	r *http.Request,
	action string,
	err error,
) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, apierrors.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, apierrors.ErrConflict),
		errors.Is(err, apierrors.ErrAlreadyExists):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		internalctx.GetLogger(r.Context()).Error(action+" failed", zap.Error(err))
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.CaptureException(err)
		}
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
