package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
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
	"github.com/distr-sh/distr/internal/types"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

type deploymentScopeIDRequest struct {
	ScopeID uuid.UUID `path:"scopeId"`
}

type targetEnvironmentAssignmentIDRequest struct {
	AssignmentID uuid.UUID `path:"assignmentId"`
}

type deploymentUnitIDRequest struct {
	UnitID uuid.UUID `path:"unitId"`
}

type deploymentUnitSubscriberIDRequest struct {
	SubscriberID uuid.UUID `path:"subscriberId"`
}

type componentDefinitionIDRequest struct {
	DefinitionID uuid.UUID `path:"definitionId"`
}

type componentAliasIDRequest struct {
	AliasID uuid.UUID `path:"aliasId"`
}

type componentInstanceIDRequest struct {
	InstanceID uuid.UUID `path:"instanceId"`
}

func DeploymentRegistryRouter(r chiopenapi.Router) {
	deploymentRegistryRouterWithFlags(r, env.ExperimentalFeatureFlags())
}

func deploymentRegistryRouterWithFlags(
	r chiopenapi.Router,
	enabledFlags []featureflags.Key,
) {
	mutationAccess := deploymentRegistryMutationAccessMiddlewareWithFlags(enabledFlags)
	r.WithOptions(option.GroupTags("Deployment Registry"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
	).Group(func(r chiopenapi.Router) {
		deploymentScopeRoutes(r, mutationAccess)
		targetEnvironmentAssignmentRoutes(r, mutationAccess)
		deploymentUnitRoutes(r, mutationAccess)
		deploymentUnitSubscriberRoutes(r, mutationAccess)
		componentDefinitionRoutes(r, mutationAccess)
		componentAliasRoutes(r, mutationAccess)
		componentInstanceRoutes(r, mutationAccess)
		deploymentRegistryPlacementRoutes(r)
	})
}

func deploymentRegistryPlainTextResponses(statuses ...int) []option.OperationOption {
	responses := make([]option.OperationOption, 0, len(statuses))
	for _, status := range statuses {
		responses = append(
			responses,
			option.Response(status, "", option.ContentType("text/plain")),
		)
	}
	return responses
}

func deploymentRegistryListErrorResponses() []option.OperationOption {
	return deploymentRegistryPlainTextResponses(
		http.StatusBadRequest,
		http.StatusForbidden,
	)
}

func deploymentRegistryItemGetErrorResponses() []option.OperationOption {
	return deploymentRegistryPlainTextResponses(
		http.StatusForbidden,
		http.StatusNotFound,
	)
}

func deploymentRegistryMutationErrorResponses() []option.OperationOption {
	return deploymentRegistryPlainTextResponses(
		http.StatusBadRequest,
		http.StatusForbidden,
		http.StatusNotFound,
	)
}

func deploymentRegistryConflictMutationErrorResponses() []option.OperationOption {
	return deploymentRegistryPlainTextResponses(
		http.StatusBadRequest,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
	)
}

func deploymentRegistryConflictDeleteErrorResponses() []option.OperationOption {
	return deploymentRegistryPlainTextResponses(
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
	)
}

//nolint:dupl // Explicit request/response types are required for accurate generated OpenAPI schemas.
func deploymentScopeRoutes(
	r chiopenapi.Router,
	mutationAccess func(http.Handler) http.Handler,
) {
	r.Route("/scopes", func(r chiopenapi.Router) {
		r.Get("/", getDeploymentScopesHandler()).
			With(option.Description("List canonical deployment scopes")).
			With(option.Request(api.DeploymentRegistryListRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentScopePage{})).
			With(deploymentRegistryListErrorResponses()...)
		r.With(mutationAccess).
			Post("/", createDeploymentScopeHandler()).
			With(option.Description("Create a canonical deployment scope")).
			With(option.Request(api.CreateDeploymentScopeRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentScope{})).
			With(deploymentRegistryConflictMutationErrorResponses()...)
		r.Route("/{scopeId}", func(r chiopenapi.Router) {
			r.Get("/", getDeploymentScopeHandler()).
				With(option.Description("Get a canonical deployment scope")).
				With(option.Request(deploymentScopeIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentScope{})).
				With(deploymentRegistryItemGetErrorResponses()...)
			r.With(mutationAccess).
				Put("/", updateDeploymentScopeHandler()).
				With(option.Description("Update mutable deployment scope fields")).
				With(option.Request(struct {
					deploymentScopeIDRequest
					api.UpdateDeploymentScopeRequest
				}{})).
				With(option.Response(http.StatusOK, api.DeploymentScope{})).
				With(deploymentRegistryMutationErrorResponses()...)
			r.With(mutationAccess).
				Delete("/", deleteDeploymentScopeHandler()).
				With(option.Description("Delete an unreferenced deployment scope")).
				With(option.Request(deploymentScopeIDRequest{})).
				With(option.Response(http.StatusNoContent, nil)).
				With(deploymentRegistryConflictDeleteErrorResponses()...)
		})
	})
}

//nolint:dupl // Explicit request/response types are required for accurate generated OpenAPI schemas.
func targetEnvironmentAssignmentRoutes(
	r chiopenapi.Router,
	mutationAccess func(http.Handler) http.Handler,
) {
	r.Route("/assignments", func(r chiopenapi.Router) {
		r.Get("/", getTargetEnvironmentAssignmentsHandler()).
			With(option.Description("List target environment assignments")).
			With(option.Request(api.DeploymentRegistryListRequest{})).
			With(option.Response(http.StatusOK, api.TargetEnvironmentAssignmentPage{})).
			With(deploymentRegistryListErrorResponses()...)
		r.With(mutationAccess).
			Post("/", createTargetEnvironmentAssignmentHandler()).
			With(option.Description("Create a target environment assignment")).
			With(option.Request(api.CreateTargetEnvironmentAssignmentRequest{})).
			With(option.Response(http.StatusOK, api.TargetEnvironmentAssignment{})).
			With(deploymentRegistryConflictMutationErrorResponses()...)
		r.Route("/{assignmentId}", func(r chiopenapi.Router) {
			r.Get("/", getTargetEnvironmentAssignmentHandler()).
				With(option.Description("Get a target environment assignment")).
				With(option.Request(targetEnvironmentAssignmentIDRequest{})).
				With(option.Response(http.StatusOK, api.TargetEnvironmentAssignment{})).
				With(deploymentRegistryItemGetErrorResponses()...)
			r.With(mutationAccess).
				Put("/", updateTargetEnvironmentAssignmentHandler()).
				With(option.Description("Update mutable target environment assignment fields")).
				With(option.Request(struct {
					targetEnvironmentAssignmentIDRequest
					api.UpdateTargetEnvironmentAssignmentRequest
				}{})).
				With(option.Response(http.StatusOK, api.TargetEnvironmentAssignment{})).
				With(deploymentRegistryConflictMutationErrorResponses()...)
			r.With(mutationAccess).
				Delete("/", deleteTargetEnvironmentAssignmentHandler()).
				With(option.Description("Delete an unreferenced target environment assignment")).
				With(option.Request(targetEnvironmentAssignmentIDRequest{})).
				With(option.Response(http.StatusNoContent, nil)).
				With(deploymentRegistryConflictDeleteErrorResponses()...)
		})
	})
}

//nolint:dupl // Explicit request/response types are required for accurate generated OpenAPI schemas.
func deploymentUnitRoutes(
	r chiopenapi.Router,
	mutationAccess func(http.Handler) http.Handler,
) {
	r.Route("/units", func(r chiopenapi.Router) {
		r.Get("/", getDeploymentUnitsHandler()).
			With(option.Description("List canonical deployment units")).
			With(option.Request(api.DeploymentRegistryListRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentUnitPage{})).
			With(deploymentRegistryListErrorResponses()...)
		r.With(mutationAccess).
			Post("/", createDeploymentUnitHandler()).
			With(option.Description("Create a canonical deployment unit")).
			With(option.Request(api.CreateDeploymentUnitRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentUnit{})).
			With(deploymentRegistryConflictMutationErrorResponses()...)
		r.Route("/{unitId}", func(r chiopenapi.Router) {
			r.Get("/", getDeploymentUnitHandler()).
				With(option.Description("Get a canonical deployment unit")).
				With(option.Request(deploymentUnitIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentUnit{})).
				With(deploymentRegistryItemGetErrorResponses()...)
			r.With(mutationAccess).
				Put("/", updateDeploymentUnitHandler()).
				With(option.Description("Update mutable deployment unit fields")).
				With(option.Request(struct {
					deploymentUnitIDRequest
					api.UpdateDeploymentUnitRequest
				}{})).
				With(option.Response(http.StatusOK, api.DeploymentUnit{})).
				With(deploymentRegistryMutationErrorResponses()...)
			r.With(mutationAccess).
				Delete("/", deleteDeploymentUnitHandler()).
				With(option.Description("Delete an unreferenced deployment unit")).
				With(option.Request(deploymentUnitIDRequest{})).
				With(option.Response(http.StatusNoContent, nil)).
				With(deploymentRegistryConflictDeleteErrorResponses()...)
		})
	})
}

//nolint:dupl // Explicit request/response types are required for accurate generated OpenAPI schemas.
func deploymentUnitSubscriberRoutes(
	r chiopenapi.Router,
	mutationAccess func(http.Handler) http.Handler,
) {
	r.Route("/subscribers", func(r chiopenapi.Router) {
		r.Get("/", getDeploymentUnitSubscribersHandler()).
			With(option.Description("List shared deployment unit subscribers")).
			With(option.Request(api.DeploymentRegistryListRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentUnitSubscriberPage{})).
			With(deploymentRegistryListErrorResponses()...)
		r.With(mutationAccess).
			Post("/", createDeploymentUnitSubscriberHandler()).
			With(option.Description(
				"Compatibility endpoint: membership is created atomically with the unit; " +
					"standalone additions return 409 Conflict after sealing",
			)).
			With(option.Request(api.CreateDeploymentUnitSubscriberRequest{})).
			With(deploymentRegistryConflictMutationErrorResponses()...)
		r.Route("/{subscriberId}", func(r chiopenapi.Router) {
			r.Get("/", getDeploymentUnitSubscriberHandler()).
				With(option.Description("Get a shared deployment unit subscriber")).
				With(option.Request(deploymentUnitSubscriberIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentUnitSubscriber{})).
				With(deploymentRegistryItemGetErrorResponses()...)
			r.With(mutationAccess).
				Put("/", updateDeploymentUnitSubscriberHandler()).
				With(option.Description(
					"Compatibility endpoint: membership is created atomically with the unit; " +
						"only an exact no-op returns the existing row with 200 OK after sealing; " +
						"membership changes return 409 Conflict",
				)).
				With(option.Request(struct {
					deploymentUnitSubscriberIDRequest
					api.UpdateDeploymentUnitSubscriberRequest
				}{})).
				With(option.Response(http.StatusOK, api.DeploymentUnitSubscriber{})).
				With(deploymentRegistryConflictMutationErrorResponses()...)
			r.With(mutationAccess).
				Delete("/", deleteDeploymentUnitSubscriberHandler()).
				With(option.Description(
					"Compatibility endpoint: membership is created atomically with the unit; " +
						"standalone deletion returns 409 Conflict after sealing",
				)).
				With(option.Request(deploymentUnitSubscriberIDRequest{})).
				With(deploymentRegistryConflictDeleteErrorResponses()...)
		})
	})
}

//nolint:dupl // Explicit request/response types are required for accurate generated OpenAPI schemas.
func componentDefinitionRoutes(
	r chiopenapi.Router,
	mutationAccess func(http.Handler) http.Handler,
) {
	r.Route("/definitions", func(r chiopenapi.Router) {
		r.Get("/", getComponentDefinitionsHandler()).
			With(option.Description("List canonical component definitions")).
			With(option.Request(api.DeploymentRegistryListRequest{})).
			With(option.Response(http.StatusOK, api.ComponentDefinitionPage{})).
			With(deploymentRegistryListErrorResponses()...)
		r.With(mutationAccess).
			Post("/", createComponentDefinitionHandler()).
			With(option.Description("Create a canonical component definition")).
			With(option.Request(api.CreateComponentDefinitionRequest{})).
			With(option.Response(http.StatusOK, api.ComponentDefinition{})).
			With(deploymentRegistryConflictMutationErrorResponses()...)
		r.Route("/{definitionId}", func(r chiopenapi.Router) {
			r.Get("/", getComponentDefinitionHandler()).
				With(option.Description("Get a canonical component definition")).
				With(option.Request(componentDefinitionIDRequest{})).
				With(option.Response(http.StatusOK, api.ComponentDefinition{})).
				With(deploymentRegistryItemGetErrorResponses()...)
			r.With(mutationAccess).
				Put("/", updateComponentDefinitionHandler()).
				With(option.Description("Update mutable component definition fields")).
				With(option.Request(struct {
					componentDefinitionIDRequest
					api.UpdateComponentDefinitionRequest
				}{})).
				With(option.Response(http.StatusOK, api.ComponentDefinition{})).
				With(deploymentRegistryMutationErrorResponses()...)
			r.With(mutationAccess).
				Delete("/", deleteComponentDefinitionHandler()).
				With(option.Description("Delete an unreferenced component definition")).
				With(option.Request(componentDefinitionIDRequest{})).
				With(option.Response(http.StatusNoContent, nil)).
				With(deploymentRegistryConflictDeleteErrorResponses()...)
		})
	})
}

//nolint:dupl // Explicit request/response types are required for accurate generated OpenAPI schemas.
func componentAliasRoutes(
	r chiopenapi.Router,
	mutationAccess func(http.Handler) http.Handler,
) {
	r.Route("/aliases", func(r chiopenapi.Router) {
		r.Get("/", getComponentAliasesHandler()).
			With(option.Description("List canonical component aliases")).
			With(option.Request(api.DeploymentRegistryListRequest{})).
			With(option.Response(http.StatusOK, api.ComponentAliasPage{})).
			With(deploymentRegistryListErrorResponses()...)
		r.With(mutationAccess).
			Post("/", createComponentAliasHandler()).
			With(option.Description("Create a canonical component alias")).
			With(option.Request(api.CreateComponentAliasRequest{})).
			With(option.Response(http.StatusOK, api.ComponentAlias{})).
			With(deploymentRegistryConflictMutationErrorResponses()...)
		r.Route("/{aliasId}", func(r chiopenapi.Router) {
			r.Get("/", getComponentAliasHandler()).
				With(option.Description("Get a canonical component alias")).
				With(option.Request(componentAliasIDRequest{})).
				With(option.Response(http.StatusOK, api.ComponentAlias{})).
				With(deploymentRegistryItemGetErrorResponses()...)
			r.With(mutationAccess).
				Put("/", updateComponentAliasHandler()).
				With(option.Description("Update component alias retirement state")).
				With(option.Request(struct {
					componentAliasIDRequest
					api.UpdateComponentAliasRequest
				}{})).
				With(option.Response(http.StatusOK, api.ComponentAlias{})).
				With(deploymentRegistryConflictMutationErrorResponses()...)
			r.With(mutationAccess).
				Delete("/", deleteComponentAliasHandler()).
				With(option.Description("Delete an unreferenced component alias")).
				With(option.Request(componentAliasIDRequest{})).
				With(option.Response(http.StatusNoContent, nil)).
				With(deploymentRegistryConflictDeleteErrorResponses()...)
		})
	})
}

//nolint:dupl // Explicit request/response types are required for accurate generated OpenAPI schemas.
func componentInstanceRoutes(
	r chiopenapi.Router,
	mutationAccess func(http.Handler) http.Handler,
) {
	r.Route("/instances", func(r chiopenapi.Router) {
		r.Get("/", getComponentInstancesHandler()).
			With(option.Description("List canonical component instances")).
			With(option.Request(api.DeploymentRegistryListRequest{})).
			With(option.Response(http.StatusOK, api.ComponentInstancePage{})).
			With(deploymentRegistryListErrorResponses()...)
		r.With(mutationAccess).
			Post("/", createComponentInstanceHandler()).
			With(option.Description("Create a canonical component instance")).
			With(option.Request(api.CreateComponentInstanceRequest{})).
			With(option.Response(http.StatusOK, api.ComponentInstance{})).
			With(deploymentRegistryConflictMutationErrorResponses()...)
		r.Route("/{instanceId}", func(r chiopenapi.Router) {
			r.Get("/", getComponentInstanceHandler()).
				With(option.Description("Get a canonical component instance")).
				With(option.Request(componentInstanceIDRequest{})).
				With(option.Response(http.StatusOK, api.ComponentInstance{})).
				With(deploymentRegistryItemGetErrorResponses()...)
			r.With(mutationAccess).
				Put("/", updateComponentInstanceHandler()).
				With(option.Description("Update mutable component instance fields")).
				With(option.Request(struct {
					componentInstanceIDRequest
					api.UpdateComponentInstanceRequest
				}{})).
				With(option.Response(http.StatusOK, api.ComponentInstance{})).
				With(deploymentRegistryConflictMutationErrorResponses()...)
			r.With(mutationAccess).
				Delete("/", deleteComponentInstanceHandler()).
				With(option.Description("Delete an unreferenced component instance")).
				With(option.Request(componentInstanceIDRequest{})).
				With(option.Response(http.StatusNoContent, nil)).
				With(deploymentRegistryConflictDeleteErrorResponses()...)
		})
	})
}

func deploymentRegistryPlacementRoutes(r chiopenapi.Router) {
	r.Route("/placements", func(r chiopenapi.Router) {
		r.Get("/", getDeploymentRegistryPlacementsHandler()).
			With(option.Description("List canonical deployment registry placements")).
			With(option.Request(api.DeploymentRegistryListRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentRegistryPlacementPage{})).
			With(deploymentRegistryListErrorResponses()...)
		r.Get("/{unitId}", getDeploymentRegistryPlacementHandler()).
			With(option.Description("Get a canonical deployment registry placement")).
			With(option.Request(deploymentUnitIDRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentRegistryPlacement{})).
			With(deploymentRegistryItemGetErrorResponses()...)
	})
}

func deploymentRegistryMutationAccessMiddlewareWithFlags(
	enabledFlags []featureflags.Key,
) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		flaggedMutation := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !featureflags.NewRegistry(enabledFlags).IsEnabled(featureflags.KeyOperatorControlPlaneV2) {
				http.NotFound(w, r)
				return
			}
			handler.ServeHTTP(w, r)
		})
		flaggedMutationHandler := middleware.RequireReadWriteOrAdmin(
			middleware.BlockSuperAdmin(flaggedMutation),
		)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				handler.ServeHTTP(w, r)
			default:
				flaggedMutationHandler.ServeHTTP(w, r)
			}
		})
	}
}

type deploymentRegistryValidatedRequest interface {
	Validate() error
}

func deploymentRegistryListHandler[Model any, Response any](
	list func(context.Context, types.RegistryListFilter) (types.Page[Model], error),
	toResponse func(types.Page[Model]) Response,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		request, ok := deploymentRegistryListRequestFromHTTP(w, r)
		if !ok {
			return
		}
		userAuth := auth.Authentication.Require(ctx)
		page, err := list(ctx, types.RegistryListFilter{
			OrganizationID: *userAuth.CurrentOrgID(),
			Cursor:         request.Cursor,
			Limit:          request.Limit,
		})
		if err != nil {
			handleDeploymentRegistryReadError(w, r, internalctx.GetLogger(ctx), "list", err)
			return
		}
		RespondJSON(w, toResponse(page))
	}
}

func deploymentRegistryGetHandler[Model any, Response any](
	pathKey string,
	get func(context.Context, uuid.UUID, uuid.UUID) (*Model, error),
	toResponse func(Model) Response,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := deploymentRegistryPathID(w, r, pathKey)
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		value, err := get(ctx, *userAuth.CurrentOrgID(), id)
		if err != nil {
			handleDeploymentRegistryReadError(w, r, internalctx.GetLogger(ctx), "get", err)
			return
		}
		RespondJSON(w, toResponse(*value))
	}
}

func deploymentRegistryCreateHandler[
	Request deploymentRegistryValidatedRequest,
	Model any,
	Response any,
](
	resource string,
	fromRequest func(uuid.UUID, Request) Model,
	create func(context.Context, *Model) error,
	toResponse func(Model) Response,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		request, err := JsonBody[Request](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		userAuth := auth.Authentication.Require(ctx)
		value := fromRequest(*userAuth.CurrentOrgID(), request)
		if err := create(ctx, &value); err != nil {
			handleDeploymentRegistryWriteError(
				w,
				r,
				internalctx.GetLogger(ctx),
				"create",
				resource,
				err,
			)
			return
		}
		RespondJSON(w, toResponse(value))
	}
}

func deploymentRegistryUpdateHandler[
	Request deploymentRegistryValidatedRequest,
	Model any,
	Response any,
](
	pathKey string,
	resource string,
	get func(context.Context, uuid.UUID, uuid.UUID) (*Model, error),
	apply func(*Model, Request),
	update func(context.Context, *Model) error,
	toResponse func(Model) Response,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := deploymentRegistryPathID(w, r, pathKey)
		if !ok {
			return
		}
		ctx := r.Context()
		request, err := JsonBody[Request](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		userAuth := auth.Authentication.Require(ctx)
		value, err := get(ctx, *userAuth.CurrentOrgID(), id)
		if err != nil {
			handleDeploymentRegistryWriteError(
				w,
				r,
				internalctx.GetLogger(ctx),
				"update",
				resource,
				err,
			)
			return
		}
		apply(value, request)
		if err := update(ctx, value); err != nil {
			handleDeploymentRegistryWriteError(
				w,
				r,
				internalctx.GetLogger(ctx),
				"update",
				resource,
				err,
			)
			return
		}
		RespondJSON(w, toResponse(*value))
	}
}

func deploymentRegistryDeleteHandler(
	pathKey string,
	resource string,
	deleteResource func(context.Context, uuid.UUID, uuid.UUID) error,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := deploymentRegistryPathID(w, r, pathKey)
		if !ok {
			return
		}
		ctx := r.Context()
		userAuth := auth.Authentication.Require(ctx)
		if err := deleteResource(ctx, *userAuth.CurrentOrgID(), id); err != nil {
			handleDeploymentRegistryWriteError(
				w,
				r,
				internalctx.GetLogger(ctx),
				"delete",
				resource,
				err,
			)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func deploymentRegistryListRequestFromHTTP(
	w http.ResponseWriter,
	r *http.Request,
) (api.DeploymentRegistryListRequest, bool) {
	query := r.URL.Query()
	request := api.DeploymentRegistryListRequest{
		Cursor: query.Get("cursor"),
	}
	if _, provided := query["limit"]; provided {
		rawLimit := query.Get("limit")
		limit, err := strconv.Atoi(rawLimit)
		if err != nil {
			http.Error(w, "limit is invalid", http.StatusBadRequest)
			return request, false
		}
		if limit < 1 {
			http.Error(
				w,
				"limit must be between 1 and 100 when provided",
				http.StatusBadRequest,
			)
			return request, false
		}
		request.Limit = limit
	}
	if err := request.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return request, false
	}
	return request, true
}

func deploymentRegistryPathID(
	w http.ResponseWriter,
	r *http.Request,
	pathKey string,
) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(pathKey))
	if err != nil {
		http.NotFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func handleDeploymentRegistryReadError(
	w http.ResponseWriter,
	r *http.Request,
	log *zap.Logger,
	action string,
	err error,
) {
	if errors.Is(err, apierrors.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, apierrors.ErrBadRequest) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Error("failed to "+action+" deployment registry resource", zap.Error(err))
	if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
		hub.CaptureException(err)
	}
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func handleDeploymentRegistryWriteError(
	w http.ResponseWriter,
	r *http.Request,
	log *zap.Logger,
	action string,
	resource string,
	err error,
) {
	switch {
	case errors.Is(err, apierrors.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, apierrors.ErrAlreadyExists):
		http.Error(w, resource+" already exists", http.StatusConflict)
	case errors.Is(err, apierrors.ErrConflict):
		http.Error(w, resource+" is in use", http.StatusConflict)
	case errors.Is(err, apierrors.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		log.Error("failed to "+action+" "+resource, zap.Error(err))
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.CaptureException(err)
		}
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func getDeploymentScopesHandler() http.HandlerFunc {
	return deploymentRegistryListHandler(db.ListDeploymentScopes, mapping.DeploymentScopePageToAPI)
}

func getDeploymentScopeHandler() http.HandlerFunc {
	return deploymentRegistryGetHandler("scopeId", db.GetDeploymentScope, mapping.DeploymentScopeToAPI)
}

func createDeploymentScopeHandler() http.HandlerFunc {
	return deploymentRegistryCreateHandler(
		"deployment scope",
		deploymentScopeFromCreateRequest,
		db.CreateDeploymentScope,
		mapping.DeploymentScopeToAPI,
	)
}

func updateDeploymentScopeHandler() http.HandlerFunc {
	return deploymentRegistryUpdateHandler(
		"scopeId",
		"deployment scope",
		db.GetDeploymentScope,
		applyDeploymentScopeUpdate,
		db.UpdateDeploymentScope,
		mapping.DeploymentScopeToAPI,
	)
}

func deleteDeploymentScopeHandler() http.HandlerFunc {
	return deploymentRegistryDeleteHandler("scopeId", "deployment scope", db.DeleteDeploymentScope)
}

func getTargetEnvironmentAssignmentsHandler() http.HandlerFunc {
	return deploymentRegistryListHandler(
		db.ListTargetEnvironmentAssignments,
		mapping.TargetEnvironmentAssignmentPageToAPI,
	)
}

func getTargetEnvironmentAssignmentHandler() http.HandlerFunc {
	return deploymentRegistryGetHandler(
		"assignmentId",
		db.GetTargetEnvironmentAssignment,
		mapping.TargetEnvironmentAssignmentToAPI,
	)
}

func createTargetEnvironmentAssignmentHandler() http.HandlerFunc {
	return deploymentRegistryCreateHandler(
		"target environment assignment",
		targetEnvironmentAssignmentFromCreateRequest,
		db.CreateTargetEnvironmentAssignment,
		mapping.TargetEnvironmentAssignmentToAPI,
	)
}

func updateTargetEnvironmentAssignmentHandler() http.HandlerFunc {
	return deploymentRegistryUpdateHandler(
		"assignmentId",
		"target environment assignment",
		db.GetTargetEnvironmentAssignment,
		applyTargetEnvironmentAssignmentUpdate,
		db.UpdateTargetEnvironmentAssignment,
		mapping.TargetEnvironmentAssignmentToAPI,
	)
}

func deleteTargetEnvironmentAssignmentHandler() http.HandlerFunc {
	return deploymentRegistryDeleteHandler(
		"assignmentId",
		"target environment assignment",
		db.DeleteTargetEnvironmentAssignment,
	)
}

func getDeploymentUnitsHandler() http.HandlerFunc {
	return deploymentRegistryListHandler(db.ListDeploymentUnits, mapping.DeploymentUnitPageToAPI)
}

func getDeploymentUnitHandler() http.HandlerFunc {
	return deploymentRegistryGetHandler("unitId", db.GetDeploymentUnit, mapping.DeploymentUnitToAPI)
}

func createDeploymentUnitHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		request, err := JsonBody[api.CreateDeploymentUnitRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		userAuth := auth.Authentication.Require(ctx)
		organizationID := *userAuth.CurrentOrgID()
		unit := deploymentUnitFromCreateRequest(organizationID, request)
		subscribers := deploymentUnitSubscribersFromCreateRequest(organizationID, request)
		if err := db.CreateDeploymentUnitWithSubscribers(ctx, &unit, subscribers); err != nil {
			handleDeploymentRegistryWriteError(
				w,
				r,
				internalctx.GetLogger(ctx),
				"create",
				"deployment unit",
				err,
			)
			return
		}
		RespondJSON(w, mapping.DeploymentUnitToAPI(unit))
	}
}

func updateDeploymentUnitHandler() http.HandlerFunc {
	return deploymentRegistryUpdateHandler(
		"unitId",
		"deployment unit",
		db.GetDeploymentUnit,
		applyDeploymentUnitUpdate,
		db.UpdateDeploymentUnit,
		mapping.DeploymentUnitToAPI,
	)
}

func deleteDeploymentUnitHandler() http.HandlerFunc {
	return deploymentRegistryDeleteHandler("unitId", "deployment unit", db.DeleteDeploymentUnit)
}

func getDeploymentUnitSubscribersHandler() http.HandlerFunc {
	return deploymentRegistryListHandler(
		db.ListDeploymentUnitSubscribers,
		mapping.DeploymentUnitSubscriberPageToAPI,
	)
}

func getDeploymentUnitSubscriberHandler() http.HandlerFunc {
	return deploymentRegistryGetHandler(
		"subscriberId",
		db.GetDeploymentUnitSubscriber,
		mapping.DeploymentUnitSubscriberToAPI,
	)
}

func createDeploymentUnitSubscriberHandler() http.HandlerFunc {
	return deploymentRegistryCreateHandler(
		"deployment unit subscriber",
		deploymentUnitSubscriberFromCreateRequest,
		db.CreateDeploymentUnitSubscriber,
		mapping.DeploymentUnitSubscriberToAPI,
	)
}

func updateDeploymentUnitSubscriberHandler() http.HandlerFunc {
	return deploymentRegistryUpdateHandler(
		"subscriberId",
		"deployment unit subscriber",
		db.GetDeploymentUnitSubscriber,
		applyDeploymentUnitSubscriberUpdate,
		db.UpdateDeploymentUnitSubscriber,
		mapping.DeploymentUnitSubscriberToAPI,
	)
}

func deleteDeploymentUnitSubscriberHandler() http.HandlerFunc {
	return deploymentRegistryDeleteHandler(
		"subscriberId",
		"deployment unit subscriber",
		db.DeleteDeploymentUnitSubscriber,
	)
}

func getComponentDefinitionsHandler() http.HandlerFunc {
	return deploymentRegistryListHandler(
		db.ListComponentDefinitions,
		mapping.ComponentDefinitionPageToAPI,
	)
}

func getComponentDefinitionHandler() http.HandlerFunc {
	return deploymentRegistryGetHandler(
		"definitionId",
		db.GetComponentDefinition,
		mapping.ComponentDefinitionToAPI,
	)
}

func createComponentDefinitionHandler() http.HandlerFunc {
	return deploymentRegistryCreateHandler(
		"component definition",
		componentDefinitionFromCreateRequest,
		db.CreateComponentDefinition,
		mapping.ComponentDefinitionToAPI,
	)
}

func updateComponentDefinitionHandler() http.HandlerFunc {
	return deploymentRegistryUpdateHandler(
		"definitionId",
		"component definition",
		db.GetComponentDefinition,
		applyComponentDefinitionUpdate,
		db.UpdateComponentDefinition,
		mapping.ComponentDefinitionToAPI,
	)
}

func deleteComponentDefinitionHandler() http.HandlerFunc {
	return deploymentRegistryDeleteHandler(
		"definitionId",
		"component definition",
		db.DeleteComponentDefinition,
	)
}

func getComponentAliasesHandler() http.HandlerFunc {
	return deploymentRegistryListHandler(db.ListComponentAliases, mapping.ComponentAliasPageToAPI)
}

func getComponentAliasHandler() http.HandlerFunc {
	return deploymentRegistryGetHandler("aliasId", db.GetComponentAlias, mapping.ComponentAliasToAPI)
}

func createComponentAliasHandler() http.HandlerFunc {
	return deploymentRegistryCreateHandler(
		"component alias",
		componentAliasFromCreateRequest,
		db.CreateComponentAlias,
		mapping.ComponentAliasToAPI,
	)
}

func updateComponentAliasHandler() http.HandlerFunc {
	return deploymentRegistryUpdateHandler(
		"aliasId",
		"component alias",
		db.GetComponentAlias,
		applyComponentAliasUpdate,
		db.UpdateComponentAlias,
		mapping.ComponentAliasToAPI,
	)
}

func deleteComponentAliasHandler() http.HandlerFunc {
	return deploymentRegistryDeleteHandler("aliasId", "component alias", db.DeleteComponentAlias)
}

func getComponentInstancesHandler() http.HandlerFunc {
	return deploymentRegistryListHandler(
		db.ListComponentInstances,
		mapping.ComponentInstancePageToAPI,
	)
}

func getComponentInstanceHandler() http.HandlerFunc {
	return deploymentRegistryGetHandler(
		"instanceId",
		db.GetComponentInstance,
		mapping.ComponentInstanceToAPI,
	)
}

func createComponentInstanceHandler() http.HandlerFunc {
	return deploymentRegistryCreateHandler(
		"component instance",
		componentInstanceFromCreateRequest,
		db.CreateComponentInstance,
		mapping.ComponentInstanceToAPI,
	)
}

func updateComponentInstanceHandler() http.HandlerFunc {
	return deploymentRegistryUpdateHandler(
		"instanceId",
		"component instance",
		db.GetComponentInstance,
		applyComponentInstanceUpdate,
		db.UpdateComponentInstance,
		mapping.ComponentInstanceToAPI,
	)
}

func deleteComponentInstanceHandler() http.HandlerFunc {
	return deploymentRegistryDeleteHandler(
		"instanceId",
		"component instance",
		db.DeleteComponentInstance,
	)
}

func getDeploymentRegistryPlacementsHandler() http.HandlerFunc {
	return deploymentRegistryListHandler(
		db.ListDeploymentRegistryPlacements,
		mapping.DeploymentRegistryPlacementPageToAPI,
	)
}

func getDeploymentRegistryPlacementHandler() http.HandlerFunc {
	return deploymentRegistryGetHandler(
		"unitId",
		db.GetDeploymentRegistryPlacement,
		mapping.DeploymentRegistryPlacementToAPI,
	)
}

func deploymentScopeFromCreateRequest(
	organizationID uuid.UUID,
	request api.CreateDeploymentScopeRequest,
) types.DeploymentScope {
	return types.DeploymentScope{
		OrganizationID:         organizationID,
		CustomerOrganizationID: request.CustomerOrganizationID,
		Key:                    strings.TrimSpace(request.Key),
		Name:                   strings.TrimSpace(request.Name),
		Description:            request.Description,
		DeliveryModel:          request.DeliveryModel,
		ManagementState:        request.ManagementState,
		RetiredAt:              request.RetiredAt,
	}
}

func targetEnvironmentAssignmentFromCreateRequest(
	organizationID uuid.UUID,
	request api.CreateTargetEnvironmentAssignmentRequest,
) types.TargetEnvironmentAssignment {
	return types.TargetEnvironmentAssignment{
		OrganizationID:     organizationID,
		DeploymentTargetID: request.DeploymentTargetID,
		EnvironmentID:      request.EnvironmentID,
		ActiveFrom:         request.ActiveFrom,
		ActiveUntil:        request.ActiveUntil,
		PolicyConstraints:  request.PolicyConstraints,
	}
}

func deploymentUnitFromCreateRequest(
	organizationID uuid.UUID,
	request api.CreateDeploymentUnitRequest,
) types.DeploymentUnit {
	return types.DeploymentUnit{
		OrganizationID:                organizationID,
		DeploymentScopeID:             request.DeploymentScopeID,
		TargetEnvironmentAssignmentID: request.TargetEnvironmentAssignmentID,
		DeploymentTargetID:            request.DeploymentTargetID,
		Key:                           strings.TrimSpace(request.Key),
		Name:                          strings.TrimSpace(request.Name),
		PhysicalIdentity:              strings.TrimSpace(request.PhysicalIdentity),
		ManagementState:               request.ManagementState,
		SubscriberSetChecksum:         request.SubscriberSetChecksum,
		RetiredAt:                     request.RetiredAt,
	}
}

func deploymentUnitSubscriberFromCreateRequest(
	organizationID uuid.UUID,
	request api.CreateDeploymentUnitSubscriberRequest,
) types.DeploymentUnitSubscriber {
	return types.DeploymentUnitSubscriber{
		OrganizationID:         organizationID,
		DeploymentUnitID:       request.DeploymentUnitID,
		CustomerOrganizationID: request.CustomerOrganizationID,
		RetiredAt:              request.RetiredAt,
	}
}

func deploymentUnitSubscribersFromCreateRequest(
	organizationID uuid.UUID,
	request api.CreateDeploymentUnitRequest,
) []types.DeploymentUnitSubscriber {
	subscribers := make(
		[]types.DeploymentUnitSubscriber,
		len(request.SubscriberCustomerOrganizationIDs),
	)
	for index, customerOrganizationID := range request.SubscriberCustomerOrganizationIDs {
		subscribers[index] = types.DeploymentUnitSubscriber{
			OrganizationID:         organizationID,
			CustomerOrganizationID: customerOrganizationID,
		}
	}
	return subscribers
}

func componentDefinitionFromCreateRequest(
	organizationID uuid.UUID,
	request api.CreateComponentDefinitionRequest,
) types.ComponentDefinition {
	return types.ComponentDefinition{
		OrganizationID:  organizationID,
		Key:             strings.TrimSpace(request.Key),
		Name:            strings.TrimSpace(request.Name),
		Description:     request.Description,
		CapabilityScope: request.CapabilityScope,
		ManagementState: request.ManagementState,
		RetiredAt:       request.RetiredAt,
	}
}

func componentAliasFromCreateRequest(
	organizationID uuid.UUID,
	request api.CreateComponentAliasRequest,
) types.ComponentAlias {
	return types.ComponentAlias{
		OrganizationID:        organizationID,
		ComponentDefinitionID: request.ComponentDefinitionID,
		Alias:                 strings.TrimSpace(request.Alias),
		RetiredAt:             request.RetiredAt,
	}
}

func componentInstanceFromCreateRequest(
	organizationID uuid.UUID,
	request api.CreateComponentInstanceRequest,
) types.ComponentInstance {
	return types.ComponentInstance{
		OrganizationID:        organizationID,
		DeploymentUnitID:      request.DeploymentUnitID,
		ComponentDefinitionID: request.ComponentDefinitionID,
		PhysicalName:          strings.TrimSpace(request.PhysicalName),
		ConfigNamespace:       request.ConfigNamespace,
		DatabaseBoundary:      request.DatabaseBoundary,
		HealthAdapter:         request.HealthAdapter,
		ManagementState:       request.ManagementState,
		RenamedFrom:           strings.TrimSpace(request.RenamedFrom),
		RetiredAt:             request.RetiredAt,
	}
}

func applyDeploymentScopeUpdate(
	scope *types.DeploymentScope,
	request api.UpdateDeploymentScopeRequest,
) {
	scope.Name = strings.TrimSpace(request.Name)
	scope.Description = request.Description
	scope.ManagementState = request.ManagementState
	scope.RetiredAt = request.RetiredAt
}

func applyTargetEnvironmentAssignmentUpdate(
	assignment *types.TargetEnvironmentAssignment,
	request api.UpdateTargetEnvironmentAssignmentRequest,
) {
	assignment.ActiveUntil = request.ActiveUntil
	assignment.PolicyConstraints = request.PolicyConstraints
}

func applyDeploymentUnitUpdate(
	unit *types.DeploymentUnit,
	request api.UpdateDeploymentUnitRequest,
) {
	unit.Name = strings.TrimSpace(request.Name)
	unit.ManagementState = request.ManagementState
	unit.RetiredAt = request.RetiredAt
}

func applyDeploymentUnitSubscriberUpdate(
	subscriber *types.DeploymentUnitSubscriber,
	request api.UpdateDeploymentUnitSubscriberRequest,
) {
	subscriber.RetiredAt = request.RetiredAt
}

func applyComponentDefinitionUpdate(
	definition *types.ComponentDefinition,
	request api.UpdateComponentDefinitionRequest,
) {
	definition.Name = strings.TrimSpace(request.Name)
	definition.Description = request.Description
	definition.CapabilityScope = request.CapabilityScope
	definition.ManagementState = request.ManagementState
	definition.RetiredAt = request.RetiredAt
}

func applyComponentAliasUpdate(
	alias *types.ComponentAlias,
	request api.UpdateComponentAliasRequest,
) {
	alias.RetiredAt = request.RetiredAt
}

func applyComponentInstanceUpdate(
	instance *types.ComponentInstance,
	request api.UpdateComponentInstanceRequest,
) {
	instance.PhysicalName = strings.TrimSpace(request.PhysicalName)
	instance.ConfigNamespace = request.ConfigNamespace
	instance.DatabaseBoundary = request.DatabaseBoundary
	instance.HealthAdapter = request.HealthAdapter
	instance.ManagementState = request.ManagementState
	instance.RenamedFrom = strings.TrimSpace(request.RenamedFrom)
	instance.RetiredAt = request.RetiredAt
}
