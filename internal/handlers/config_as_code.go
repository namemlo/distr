package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/configascode"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/middleware"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/oaswrap/spec/adapter/chiopenapi"
	"github.com/oaswrap/spec/option"
)

func ConfigAsCodeRouter(r chiopenapi.Router) {
	r.WithOptions(option.GroupTags("Config as Code"))
	r.With(
		middleware.RequireVendor,
		middleware.RequireOrgAndRole,
		configAsCodeFeatureFlagMiddleware,
	).Group(func(r chiopenapi.Router) {
		r.Post("/validate", validateConfigAsCodeHandler()).
			With(option.Description("Validate Config as Code documents without mutating resources")).
			With(option.Request(api.ConfigAsCodeValidateRequest{})).
			With(option.Response(http.StatusOK, api.ConfigAsCodeValidateResponse{}))

		r.Get("/authorities", getConfigAsCodeAuthoritiesHandler()).
			With(option.Description("List Config as Code authority records for the current organization")).
			With(option.Response(http.StatusOK, api.ConfigAsCodeAuthorityListResponse{}))

		r.Route("/authorities/{kind}/{resourceId}", func(r chiopenapi.Router) {
			type AuthorityPathRequest struct {
				Kind       string    `path:"kind"`
				ResourceID uuid.UUID `path:"resourceId"`
			}

			r.Get("/", getConfigAsCodeAuthorityHandler()).
				With(option.Description("Get Config as Code authority for one resource")).
				With(option.Request(AuthorityPathRequest{})).
				With(option.Response(http.StatusOK, api.ConfigAsCodeAuthority{}))

			r.With(middleware.RequireReadWriteOrAdmin, middleware.BlockSuperAdmin).
				Put("/", putConfigAsCodeAuthorityHandler()).
				With(option.Description("Set Config as Code authority for one resource")).
				With(option.Request(struct {
					AuthorityPathRequest
					api.ConfigAsCodeAuthorityUpdateRequest
				}{})).
				With(option.Response(http.StatusOK, api.ConfigAsCodeAuthority{}))
		})
	})
}

func validateConfigAsCodeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := JsonBody[api.ConfigAsCodeValidateRequest](w, r)
		if err != nil {
			return
		}
		RespondJSON(w, validateConfigAsCodeRequest(request))
	}
}

func getConfigAsCodeAuthoritiesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authInfo := auth.Authentication.Require(r.Context())
		authorities, err := db.GetConfigAsCodeAuthorities(r.Context(), *authInfo.CurrentOrgID())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, api.ConfigAsCodeAuthorityListResponse{
			Authorities: configAsCodeAuthorityResponses(authorities),
		})
	}
}

func getConfigAsCodeAuthorityHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authInfo := auth.Authentication.Require(r.Context())
		resourceKind, resourceID, ok := configAsCodeAuthorityPath(w, r)
		if !ok {
			return
		}
		authority, err := db.GetConfigAsCodeAuthority(r.Context(), *authInfo.CurrentOrgID(), resourceKind, resourceID)
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, configAsCodeAuthorityResponse(*authority))
	}
}

func putConfigAsCodeAuthorityHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authInfo := auth.Authentication.Require(r.Context())
		resourceKind, resourceID, ok := configAsCodeAuthorityPath(w, r)
		if !ok {
			return
		}
		request, err := JsonBody[api.ConfigAsCodeAuthorityUpdateRequest](w, r)
		if err != nil {
			return
		}
		actorID := authInfo.CurrentUserID()
		authority := types.ConfigAsCodeAuthority{
			OrganizationID:   *authInfo.CurrentOrgID(),
			ResourceKind:     resourceKind,
			ResourceID:       resourceID,
			Authority:        types.ConfigAsCodeAuthorityValue(request.Authority),
			RepositoryPath:   request.RepositoryPath,
			SourceRevision:   request.SourceRevision,
			DocumentChecksum: request.DocumentChecksum,
			UpdatedByUserID:  &actorID,
		}
		if err := db.UpsertConfigAsCodeAuthority(r.Context(), &authority); errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
			return
		} else if errors.Is(err, apierrors.ErrBadRequest) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, configAsCodeAuthorityResponse(authority))
	}
}

func validateConfigAsCodeRequest(request api.ConfigAsCodeValidateRequest) api.ConfigAsCodeValidateResponse {
	response := api.ConfigAsCodeValidateResponse{Valid: true}
	documentOffset := 0
	for _, document := range request.Documents {
		result := configascode.ValidateDocuments([]byte(document.Content))
		response.Documents = append(response.Documents, configAsCodeDocumentResults(result.Documents)...)
		response.Errors = append(response.Errors, configAsCodeIssues(result.Errors, documentOffset)...)
		response.Warnings = append(response.Warnings, configAsCodeIssues(result.Warnings, documentOffset)...)
		if !result.Valid {
			response.Valid = false
		}
		documentOffset += configAsCodeDocumentCount(result)
	}
	if len(request.Documents) == 0 {
		response.Valid = false
		response.Errors = append(response.Errors, api.ConfigAsCodeIssue{
			DocumentIndex: -1,
			Path:          "$.documents",
			Message:       "at least one document is required",
		})
	}
	return response
}

func configAsCodeDocumentResults(documents []configascode.DocumentResult) []api.ConfigAsCodeDocumentResult {
	results := make([]api.ConfigAsCodeDocumentResult, 0, len(documents))
	for _, document := range documents {
		results = append(results, api.ConfigAsCodeDocumentResult{
			Kind:              document.Kind,
			APIVersion:        document.APIVersion,
			MetadataName:      document.MetadataName,
			MetadataPath:      document.MetadataPath,
			CanonicalChecksum: document.CanonicalChecksum,
		})
	}
	return results
}

func configAsCodeAuthorityPath(
	w http.ResponseWriter,
	r *http.Request,
) (types.ConfigAsCodeResourceKind, uuid.UUID, bool) {
	resourceKind := types.ConfigAsCodeResourceKind(r.PathValue("kind"))
	resourceID, err := uuid.Parse(r.PathValue("resourceId"))
	if err != nil {
		http.NotFound(w, r)
		return "", uuid.Nil, false
	}
	return resourceKind, resourceID, true
}

func configAsCodeAuthorityResponses(authorities []types.ConfigAsCodeAuthority) []api.ConfigAsCodeAuthority {
	responses := make([]api.ConfigAsCodeAuthority, 0, len(authorities))
	for _, authority := range authorities {
		responses = append(responses, configAsCodeAuthorityResponse(authority))
	}
	return responses
}

func configAsCodeAuthorityResponse(authority types.ConfigAsCodeAuthority) api.ConfigAsCodeAuthority {
	var updatedBy *string
	if authority.UpdatedByUserID != nil {
		value := authority.UpdatedByUserID.String()
		updatedBy = &value
	}
	updatedAt := ""
	if !authority.UpdatedAt.IsZero() {
		updatedAt = authority.UpdatedAt.Format(time.RFC3339)
	}
	return api.ConfigAsCodeAuthority{
		ResourceKind:     string(authority.ResourceKind),
		ResourceID:       authority.ResourceID.String(),
		Authority:        string(authority.Authority),
		RepositoryPath:   authority.RepositoryPath,
		SourceRevision:   authority.SourceRevision,
		DocumentChecksum: authority.DocumentChecksum,
		UpdatedByUserID:  updatedBy,
		UpdatedAt:        updatedAt,
	}
}

func configAsCodeIssues(issues []configascode.Issue, documentOffset int) []api.ConfigAsCodeIssue {
	results := make([]api.ConfigAsCodeIssue, 0, len(issues))
	for _, issue := range issues {
		documentIndex := issue.DocumentIndex
		if documentIndex >= 0 {
			documentIndex += documentOffset
		}
		results = append(results, api.ConfigAsCodeIssue{
			DocumentIndex: documentIndex,
			Path:          issue.Path,
			Message:       issue.Message,
		})
	}
	return results
}

func configAsCodeDocumentCount(result configascode.ValidationResult) int {
	count := len(result.Documents)
	for _, issue := range result.Errors {
		if issue.DocumentIndex >= count {
			count = issue.DocumentIndex + 1
		}
	}
	for _, issue := range result.Warnings {
		if issue.DocumentIndex >= count {
			count = issue.DocumentIndex + 1
		}
	}
	if count == 0 {
		return 1
	}
	return count
}

func configAsCodeFeatureFlagMiddleware(handler http.Handler) http.Handler {
	return configAsCodeFeatureFlagMiddlewareWithFlags(env.ExperimentalFeatureFlags())(handler)
}

func configAsCodeFeatureFlagMiddlewareWithFlags(enabledFlags []featureflags.Key) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			registry := featureflags.NewRegistry(enabledFlags)
			if !registry.IsEnabled(featureflags.KeyConfigAsCode) {
				http.NotFound(w, r)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}
}
