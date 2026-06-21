package handlers

import (
	"net/http"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/actionregistry"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
)

func ActionDefinitionsRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Action Definitions"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		middleware.ExperimentalFeatureFlagMiddleware(featureflags.KeyDeploymentProcesses),
	).Group(func(r chiopenapi.Router) {
		r.Get("/", getActionDefinitionsHandler()).
			With(option.Description("List built-in deployment process action definitions")).
			With(option.Response(http.StatusOK, []api.ActionDefinition{}))
	})
}

func getActionDefinitionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		RespondJSON(w, mapping.List(actionregistry.DefaultRegistry().List(), mapping.ActionDefinitionToAPI))
	}
}
