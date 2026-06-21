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

//nolint:dupl
func VariableSetsRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Variable Sets"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyScopedVariablesV2),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getVariableSetsHandler()).
			With(option.Description("List variable sets")).
			With(option.Response(http.StatusOK, []api.VariableSet{}))

		r.Route("/{variableSetId}", func(r chiopenapi.Router) {
			type VariableSetIDRequest struct {
				VariableSetID uuid.UUID `path:"variableSetId"`
			}

			r.Get("/", getVariableSetHandler()).
				With(option.Description("Get a variable set")).
				With(option.Request(VariableSetIDRequest{})).
				With(option.Response(http.StatusOK, api.VariableSet{}))

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).Group(func(r chiopenapi.Router) {
				r.Put("/", updateVariableSetHandler()).
					With(option.Description("Update a variable set")).
					With(option.Request(struct {
						VariableSetIDRequest
						api.CreateUpdateVariableSetRequest
					}{})).
					With(option.Response(http.StatusOK, api.VariableSet{}))

				r.Delete("/", deleteVariableSetHandler()).
					With(option.Description("Delete a variable set")).
					With(option.Request(VariableSetIDRequest{}))
			})
		})

		r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
			Post("/", createVariableSetHandler()).
			With(option.Description("Create a variable set")).
			With(option.Request(api.CreateUpdateVariableSetRequest{})).
			With(option.Response(http.StatusOK, api.VariableSet{}))
	})
}

func getVariableSetsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		variableSets, err := db.GetVariableSetsByOrganizationID(ctx, *auth.CurrentOrgID())
		if err != nil {
			log.Error("failed to get variable sets", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, mapping.List(variableSets, mapping.VariableSetToAPI))
	}
}

//nolint:dupl
func getVariableSetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("variableSetId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		variableSet, err := db.GetVariableSet(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get variable set", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.VariableSetToAPI(*variableSet))
		}
	}
}

func createVariableSetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateVariableSetRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		variableSet := variableSetFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		if err := db.CreateVariableSet(ctx, &variableSet); err != nil {
			handleVariableSetWriteError(w, r, log, "create", err)
			return
		}
		RespondJSON(w, mapping.VariableSetToAPI(variableSet))
	}
}

//nolint:dupl
func updateVariableSetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("variableSetId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.CreateUpdateVariableSetRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		variableSet := variableSetFromCreateUpdateRequest(*auth.CurrentOrgID(), request)
		variableSet.ID = id
		if err := db.UpdateVariableSet(ctx, &variableSet); err != nil {
			handleVariableSetWriteError(w, r, log, "update", err)
			return
		}
		RespondJSON(w, mapping.VariableSetToAPI(variableSet))
	}
}

//nolint:dupl
func deleteVariableSetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("variableSetId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		if err := db.DeleteVariableSetWithID(ctx, id, *auth.CurrentOrgID()); errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if errors.Is(err, apierrors.ErrConflict) {
			http.Error(w, "variable set is in use", http.StatusConflict)
		} else if err != nil {
			log.Error("failed to delete variable set", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func variableSetFromCreateUpdateRequest(orgID uuid.UUID, request api.CreateUpdateVariableSetRequest) types.VariableSet {
	return types.VariableSet{
		OrganizationID: orgID,
		Name:           strings.TrimSpace(request.Name),
		Description:    request.Description,
		SortOrder:      request.SortOrder,
		ApplicationIDs: append([]uuid.UUID(nil), request.ApplicationIDs...),
		Variables:      variablesFromRequests(request.Variables),
	}
}

func variablesFromRequests(requests []api.VariableRequest) []types.Variable {
	variables := make([]types.Variable, 0, len(requests))
	for _, request := range requests {
		variables = append(variables, types.Variable{
			Key:           strings.TrimSpace(request.Key),
			Description:   request.Description,
			Type:          types.VariableType(request.Type),
			IsRequired:    request.IsRequired,
			DefaultValue:  request.DefaultValue,
			ReferenceID:   strings.TrimSpace(request.ReferenceID),
			ReferenceName: strings.TrimSpace(request.ReferenceName),
		})
	}
	return variables
}

func handleVariableSetWriteError(w http.ResponseWriter, r *http.Request, log *zap.Logger, action string, err error) {
	if errors.Is(err, apierrors.ErrAlreadyExists) {
		http.Error(w, "a variable set with this name already exists", http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrBadRequest) {
		http.Error(w, "invalid variable set", http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrNotFound) {
		http.NotFound(w, r)
	} else if errors.Is(err, apierrors.ErrConflict) {
		http.Error(w, "variable set is in use", http.StatusConflict)
	} else {
		log.Error("failed to "+action+" variable set", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
