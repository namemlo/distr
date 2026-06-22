package handlers

import (
	"errors"
	"net/http"

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

func StepTemplatesRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Step Templates"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyStepTemplates),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getStepTemplatesHandler()).
			With(option.Description("List installed step templates")).
			With(option.Response(http.StatusOK, []api.StepTemplate{}))

		r.Route("/{stepTemplateId}", func(r chiopenapi.Router) {
			type StepTemplateIDRequest struct {
				StepTemplateID uuid.UUID `path:"stepTemplateId"`
			}

			r.Get("/", getStepTemplateHandler()).
				With(option.Description("Get an installed step template")).
				With(option.Request(StepTemplateIDRequest{})).
				With(option.Response(http.StatusOK, api.StepTemplate{}))
		})

		r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
			Post("/import", importStepTemplateHandler()).
			With(option.Description("Import and install a step template")).
			With(option.Request(api.ImportStepTemplateRequest{})).
			With(option.Response(http.StatusOK, api.StepTemplate{}))
	})
}

func getStepTemplatesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		templates, err := db.GetStepTemplatesByOrganizationID(ctx, *auth.CurrentOrgID())
		if err != nil {
			log.Error("failed to get step templates", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, stepTemplateResponses(templates))
	}
}

func getStepTemplateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.PathValue("stepTemplateId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		template, err := db.GetStepTemplate(ctx, id, *auth.CurrentOrgID())
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Error("failed to get step template", zap.Error(err))
			sentry.GetHubFromContext(ctx).CaptureException(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			RespondJSON(w, mapping.StepTemplateToAPI(*template))
		}
	}
}

func importStepTemplateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := internalctx.GetLogger(ctx)
		auth := auth.Authentication.Require(ctx)

		request, err := JsonBody[api.ImportStepTemplateRequest](w, r)
		if err != nil {
			return
		} else if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		template, err := db.ImportStepTemplate(ctx, stepTemplateImportFromAPI(
			*auth.CurrentOrgID(),
			auth.CurrentUserID(),
			request,
		))
		if err != nil {
			handleStepTemplateWriteError(w, r, log, "import", err)
			return
		}
		RespondJSON(w, mapping.StepTemplateToAPI(*template))
	}
}

func stepTemplateImportFromAPI(
	orgID uuid.UUID,
	userID uuid.UUID,
	request api.ImportStepTemplateRequest,
) types.StepTemplateImport {
	_ = request.Validate()
	return types.StepTemplateImport{
		OrganizationID:            orgID,
		InstalledByUserAccountID:  &userID,
		SourceType:                types.StepTemplateSourceType(request.SourceType),
		SourceRef:                 request.SourceRef,
		Name:                      request.Name,
		Description:               request.Description,
		Category:                  request.Category,
		Version:                   request.Version,
		ActionType:                request.ActionType,
		ExecutionLocation:         request.ExecutionLocation,
		InputSchema:               request.InputSchema,
		OutputSchema:              request.OutputSchema,
		DefaultInputBindings:      request.DefaultInputBindings,
		MinimumAgentVersion:       request.MinimumAgentVersion,
		CompatibleActionVersion:   request.CompatibleActionVersion,
		RuntimeCompatibilityNotes: request.RuntimeCompatibilityNotes,
		Deprecated:                request.Deprecated,
	}
}

func stepTemplateResponses(templates []types.StepTemplate) []api.StepTemplate {
	return mapping.List(templates, mapping.StepTemplateToAPI)
}

func handleStepTemplateWriteError(
	w http.ResponseWriter,
	r *http.Request,
	log *zap.Logger,
	action string,
	err error,
) {
	if errors.Is(err, apierrors.ErrAlreadyExists) {
		http.Error(w, "step template version is already installed", http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrBadRequest) {
		http.Error(w, err.Error(), http.StatusBadRequest)
	} else if errors.Is(err, apierrors.ErrNotFound) {
		http.NotFound(w, r)
	} else {
		log.Error("failed to "+action+" step template", zap.Error(err))
		sentry.GetHubFromContext(r.Context()).CaptureException(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
