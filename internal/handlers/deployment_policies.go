package handlers

import (
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
	"github.com/distr-sh/distr/internal/types"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
	"go.uber.org/zap"
)

type deploymentPolicyIDRequest struct {
	PolicyID uuid.UUID `path:"policyId"`
}

type deploymentPolicyVersionIDRequest struct {
	deploymentPolicyIDRequest
	VersionID uuid.UUID `path:"versionId"`
}

type deploymentPolicyBindingIDRequest struct {
	BindingID uuid.UUID `path:"bindingId"`
}

func DeploymentPoliciesRouter(r chiopenapi.Router) {
	deploymentPoliciesRouterWithFlags(r, env.ExperimentalFeatureFlags())
}

func deploymentPoliciesRouterWithFlags(
	r chiopenapi.Router,
	enabledFlags []featureflags.Key,
) {
	mutationAccess := deploymentPolicyMutationAccessMiddlewareWithFlags(enabledFlags)
	r.WithOptions(option.GroupTags("Deployment Policies"))
	r.With(middleware.RequireVendor, middleware.RequireOrgAndRole).Group(func(r chiopenapi.Router) {
		r.Get("/", getDeploymentPoliciesHandler()).
			With(option.Description("List versioned deployment policies")).
			With(option.Response(http.StatusOK, []api.DeploymentPolicy{}))
		r.With(mutationAccess).Post("/", createDeploymentPolicyHandler()).
			With(option.Description("Create a deployment policy resource")).
			With(option.Request(api.CreateDeploymentPolicyRequest{})).
			With(option.Response(http.StatusOK, api.DeploymentPolicy{}))

		r.Get("/bindings", getDeploymentPolicyBindingsHandler()).
			With(option.Description("List deployment policy bindings")).
			With(option.Response(http.StatusOK, []api.DeploymentPolicyBinding{}))
		r.With(mutationAccess).Post("/bindings", createDeploymentPolicyBindingHandler()).
			With(option.Description("Bind a published policy version to a governance scope")).
			With(option.Request(api.CreateDeploymentPolicyBindingRequest{}))
		r.With(mutationAccess).Delete(
			"/bindings/{bindingId}",
			retireDeploymentPolicyBindingHandler(),
		).With(option.Description("Retire an active deployment policy binding")).
			With(option.Request(deploymentPolicyBindingIDRequest{}))

		r.Route("/{policyId}", func(r chiopenapi.Router) {
			r.Get("/", getDeploymentPolicyHandler()).
				With(option.Description("Get a deployment policy resource")).
				With(option.Request(deploymentPolicyIDRequest{})).
				With(option.Response(http.StatusOK, api.DeploymentPolicy{}))
			r.With(mutationAccess).Put("/", updateDeploymentPolicyHandler()).
				With(option.Description("Update deployment policy metadata")).
				With(option.Request(struct {
					deploymentPolicyIDRequest
					api.UpdateDeploymentPolicyRequest
				}{})).
				With(option.Response(http.StatusOK, api.DeploymentPolicy{}))
			r.With(mutationAccess).Delete("/", deleteDeploymentPolicyHandler()).
				With(option.Description("Delete a policy without published versions")).
				With(option.Request(deploymentPolicyIDRequest{}))

			r.Get("/versions", getDeploymentPolicyVersionsHandler()).
				With(option.Description("List immutable policy versions")).
				With(option.Request(deploymentPolicyIDRequest{})).
				With(option.Response(http.StatusOK, []api.DeploymentPolicyVersion{}))
			r.With(mutationAccess).Post("/versions", createDeploymentPolicyVersionHandler()).
				With(option.Description("Create a validated draft policy version")).
				With(option.Request(struct {
					deploymentPolicyIDRequest
					api.CreateDeploymentPolicyVersionRequest
				}{})).
				With(option.Response(http.StatusOK, api.DeploymentPolicyVersion{}))

			r.Route("/versions/{versionId}", func(r chiopenapi.Router) {
				r.Get("/", getDeploymentPolicyVersionHandler()).
					With(option.Description("Get a deployment policy version")).
					With(option.Request(deploymentPolicyVersionIDRequest{})).
					With(option.Response(http.StatusOK, api.DeploymentPolicyVersion{}))
				r.With(mutationAccess).Put("/", updateDeploymentPolicyVersionHandler()).
					With(option.Description("Replace an unpublished draft policy document")).
					With(option.Request(struct {
						deploymentPolicyVersionIDRequest
						api.CreateDeploymentPolicyVersionRequest
					}{})).
					With(option.Response(http.StatusOK, api.DeploymentPolicyVersion{}))
				r.With(mutationAccess).Post("/validate", validateDeploymentPolicyVersionHandler()).
					With(option.Description("Validate a stored policy version")).
					With(option.Request(deploymentPolicyVersionIDRequest{})).
					With(option.Response(http.StatusOK, api.DeploymentPolicyValidationResponse{}))
				r.With(mutationAccess).Post("/publish", publishDeploymentPolicyVersionHandler()).
					With(option.Description("Publish an immutable valid policy version")).
					With(option.Request(deploymentPolicyVersionIDRequest{})).
					With(option.Response(http.StatusOK, api.DeploymentPolicyVersion{})).
					With(option.Response(http.StatusBadRequest, api.DeploymentPolicyValidationResponse{}))
			})
		})
	})
}

func deploymentPolicyMutationAccessMiddlewareWithFlags(
	enabledFlags []featureflags.Key,
) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		scoped := failClosedUntilScopedAuthorizationAdapter(
			pr066ScopedAuthorizationSchemaPresent,
		)(handler)
		mutation := operatorControlPlaneMutationAccessMiddlewareWithFlags(enabledFlags)(
			scoped,
		)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				handler.ServeHTTP(w, r)
			default:
				mutation.ServeHTTP(w, r)
			}
		})
	}
}

func deploymentPolicyJSONBody[T any](
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

func getDeploymentPoliciesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authInfo := auth.Authentication.Require(r.Context())
		policies, err := db.GetDeploymentPoliciesByOrganizationID(
			r.Context(),
			*authInfo.CurrentOrgID(),
		)
		if err != nil {
			handleDeploymentPolicyError(w, r, "list deployment policies", "", err)
			return
		}
		RespondJSON(w, mapping.List(policies, mapping.DeploymentPolicyToAPI))
	}
}

func createDeploymentPolicyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := deploymentPolicyJSONBody[api.CreateDeploymentPolicyRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		policy := types.DeploymentPolicy{
			OrganizationID: *authInfo.CurrentOrgID(),
			Key:            request.Key,
			Name:           request.Name,
			Description:    request.Description,
		}
		if err := db.CreateDeploymentPolicy(r.Context(), &policy); err != nil {
			handleDeploymentPolicyError(w, r, "create deployment policy", "deployment policy", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPolicyToAPI(policy))
	}
}

func getDeploymentPolicyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policyID, ok := deploymentPolicyPathID(w, r, "policyId")
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		policy, err := db.GetDeploymentPolicy(r.Context(), policyID, *authInfo.CurrentOrgID())
		if err != nil {
			handleDeploymentPolicyError(w, r, "get deployment policy", "", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPolicyToAPI(*policy))
	}
}

func updateDeploymentPolicyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policyID, ok := deploymentPolicyPathID(w, r, "policyId")
		if !ok {
			return
		}
		request, err := deploymentPolicyJSONBody[api.UpdateDeploymentPolicyRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		policy, err := db.GetDeploymentPolicy(r.Context(), policyID, *authInfo.CurrentOrgID())
		if err != nil {
			handleDeploymentPolicyError(w, r, "get deployment policy", "", err)
			return
		}
		policy.Name = request.Name
		policy.Description = request.Description
		if err := db.UpdateDeploymentPolicy(r.Context(), policy); err != nil {
			handleDeploymentPolicyError(w, r, "update deployment policy", "deployment policy", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPolicyToAPI(*policy))
	}
}

func deleteDeploymentPolicyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policyID, ok := deploymentPolicyPathID(w, r, "policyId")
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		err := db.DeleteDeploymentPolicy(r.Context(), policyID, *authInfo.CurrentOrgID())
		if err != nil {
			handleDeploymentPolicyError(w, r, "delete deployment policy", "deployment policy", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func getDeploymentPolicyVersionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policyID, ok := deploymentPolicyPathID(w, r, "policyId")
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		versions, err := db.GetDeploymentPolicyVersions(
			r.Context(),
			policyID,
			*authInfo.CurrentOrgID(),
		)
		if err != nil {
			handleDeploymentPolicyError(w, r, "list deployment policy versions", "", err)
			return
		}
		RespondJSON(w, mapping.List(versions, mapping.DeploymentPolicyVersionToAPI))
	}
}

func createDeploymentPolicyVersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policyID, ok := deploymentPolicyPathID(w, r, "policyId")
		if !ok {
			return
		}
		request, err := deploymentPolicyJSONBody[api.CreateDeploymentPolicyVersionRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		version := types.DeploymentPolicyVersion{
			OrganizationID:         *authInfo.CurrentOrgID(),
			PolicyID:               policyID,
			Document:               request.Document,
			CreatedByUserAccountID: authInfo.CurrentUserID(),
		}
		if err := db.CreateDeploymentPolicyVersion(r.Context(), &version); err != nil {
			handleDeploymentPolicyError(w, r, "create deployment policy version", "policy version", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPolicyVersionToAPI(version))
	}
}

func getDeploymentPolicyVersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policyID, versionID, ok := deploymentPolicyVersionPathIDs(w, r)
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		version, err := db.GetDeploymentPolicyVersion(
			r.Context(),
			versionID,
			*authInfo.CurrentOrgID(),
		)
		if err != nil || version.PolicyID != policyID {
			if err == nil {
				err = apierrors.ErrNotFound
			}
			handleDeploymentPolicyError(w, r, "get deployment policy version", "", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPolicyVersionToAPI(*version))
	}
}

func updateDeploymentPolicyVersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policyID, versionID, ok := deploymentPolicyVersionPathIDs(w, r)
		if !ok {
			return
		}
		request, err := deploymentPolicyJSONBody[api.CreateDeploymentPolicyVersionRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		version, err := db.GetDeploymentPolicyVersion(
			r.Context(),
			versionID,
			*authInfo.CurrentOrgID(),
		)
		if err != nil || version.PolicyID != policyID {
			if err == nil {
				err = apierrors.ErrNotFound
			}
			handleDeploymentPolicyError(w, r, "get deployment policy version", "", err)
			return
		}
		version.Document = request.Document
		if err := db.UpdateDeploymentPolicyVersion(r.Context(), version); err != nil {
			handleDeploymentPolicyError(w, r, "update deployment policy version", "policy version", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPolicyVersionToAPI(*version))
	}
}

func validateDeploymentPolicyVersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policyID, versionID, ok := deploymentPolicyVersionPathIDs(w, r)
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		version, err := db.GetDeploymentPolicyVersion(
			r.Context(),
			versionID,
			*authInfo.CurrentOrgID(),
		)
		if err != nil || version.PolicyID != policyID {
			if err == nil {
				err = apierrors.ErrNotFound
			}
			handleDeploymentPolicyError(w, r, "get deployment policy version", "", err)
			return
		}
		issues, err := db.ValidateStoredDeploymentPolicyVersion(
			r.Context(),
			versionID,
			*authInfo.CurrentOrgID(),
		)
		if err != nil {
			handleDeploymentPolicyError(w, r, "validate deployment policy version", "", err)
			return
		}
		RespondJSON(w, api.DeploymentPolicyValidationResponse{
			Valid:  len(issues) == 0,
			Issues: issues,
		})
	}
}

func publishDeploymentPolicyVersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policyID, versionID, ok := deploymentPolicyVersionPathIDs(w, r)
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		version, err := db.GetDeploymentPolicyVersion(
			r.Context(),
			versionID,
			*authInfo.CurrentOrgID(),
		)
		if err != nil || version.PolicyID != policyID {
			if err == nil {
				err = apierrors.ErrNotFound
			}
			handleDeploymentPolicyError(w, r, "get deployment policy version", "", err)
			return
		}
		published, issues, err := db.PublishDeploymentPolicyVersion(
			r.Context(),
			versionID,
			*authInfo.CurrentOrgID(),
			authInfo.CurrentUserID(),
		)
		if len(issues) != 0 {
			RespondJSONWithStatus(w, http.StatusBadRequest, api.DeploymentPolicyValidationResponse{
				Valid:  false,
				Issues: issues,
			})
			return
		}
		if err != nil {
			handleDeploymentPolicyError(w, r, "publish deployment policy version", "policy version", err)
			return
		}
		RespondJSON(w, mapping.DeploymentPolicyVersionToAPI(*published))
	}
}

func getDeploymentPolicyBindingsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authInfo := auth.Authentication.Require(r.Context())
		bindings, err := db.GetDeploymentPolicyBindings(r.Context(), *authInfo.CurrentOrgID())
		if err != nil {
			handleDeploymentPolicyError(w, r, "list deployment policy bindings", "", err)
			return
		}
		RespondJSON(w, mapping.List(bindings, mapping.DeploymentPolicyBindingToAPI))
	}
}

func createDeploymentPolicyBindingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := deploymentPolicyJSONBody[api.CreateDeploymentPolicyBindingRequest](w, r)
		if err != nil {
			return
		}
		if err := request.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		err = db.BindDeploymentPolicy(r.Context(), types.PolicyBindingRequest{
			OrganizationID:         *authInfo.CurrentOrgID(),
			PolicyVersionID:        request.PolicyVersionID,
			ScopeKind:              request.ScopeKind,
			ScopeID:                request.ScopeID,
			Role:                   request.Role,
			CreatedByUserAccountID: authInfo.CurrentUserID(),
		})
		if err != nil {
			handleDeploymentPolicyError(w, r, "bind deployment policy", "policy binding", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func retireDeploymentPolicyBindingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bindingID, ok := deploymentPolicyPathID(w, r, "bindingId")
		if !ok {
			return
		}
		authInfo := auth.Authentication.Require(r.Context())
		err := db.RetireDeploymentPolicyBinding(
			r.Context(),
			bindingID,
			*authInfo.CurrentOrgID(),
		)
		if err != nil {
			handleDeploymentPolicyError(w, r, "retire deployment policy binding", "policy binding", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func deploymentPolicyPathID(
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

func deploymentPolicyVersionPathIDs(
	w http.ResponseWriter,
	r *http.Request,
) (uuid.UUID, uuid.UUID, bool) {
	policyID, ok := deploymentPolicyPathID(w, r, "policyId")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	versionID, ok := deploymentPolicyPathID(w, r, "versionId")
	return policyID, versionID, ok
}

func handleDeploymentPolicyError(
	w http.ResponseWriter,
	r *http.Request,
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
		internalctx.GetLogger(r.Context()).Error(action+" failed", zap.Error(err))
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.CaptureException(err)
		}
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
