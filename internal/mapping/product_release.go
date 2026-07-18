package mapping

import (
	"slices"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func ProductReleaseManifestFromCreateRequest(
	organizationID uuid.UUID,
	request api.CreateProductReleaseRequest,
) types.ProductReleaseManifest {
	components := make([]types.ProductReleaseComponent, 0, len(request.Components))
	for _, component := range request.Components {
		components = append(components, types.ProductReleaseComponent{
			ComponentReleaseID:       component.ComponentReleaseID,
			ComponentReleaseChecksum: component.ComponentReleaseChecksum,
		})
	}
	return types.ProductReleaseManifest{
		Schema:                  request.Schema,
		OrganizationID:          organizationID,
		ApplicationID:           request.ApplicationID,
		ChannelID:               request.ChannelID,
		Product:                 request.Product,
		Version:                 request.Version,
		DependencyPolicyVersion: request.DependencyPolicyVersion,
		ReleaseNotes:            request.ReleaseNotes,
		RequiredPlatforms:       slices.Clone(request.RequiredPlatforms),
		Components:              components,
		Requirements:            cloneProductReleaseRequirements(request.Requirements),
	}
}

func ProductReleaseToAPI(
	bundle types.ReleaseBundle,
	manifest types.ProductReleaseManifest,
) api.ProductRelease {
	components := make([]api.ProductReleaseComponent, 0, len(manifest.Components))
	for _, component := range manifest.Components {
		components = append(components, api.ProductReleaseComponent{
			ComponentReleaseID:       component.ComponentReleaseID,
			ComponentReleaseChecksum: component.ComponentReleaseChecksum,
			ComponentKey:             component.ComponentKey,
			Version:                  component.Version,
		})
	}
	return api.ProductRelease{
		ID:                       bundle.ID,
		CreatedAt:                bundle.CreatedAt,
		UpdatedAt:                bundle.UpdatedAt,
		ApplicationID:            bundle.ApplicationID,
		ChannelID:                bundle.ChannelID,
		Status:                   bundle.Status,
		CanonicalChecksum:        bundle.CanonicalChecksum,
		GraphChecksum:            manifest.GraphChecksum,
		PublishedByUserAccountID: bundle.PublishedByUserAccountID,
		PublishedAt:              bundle.PublishedAt,
		Manifest: api.ProductReleaseManifest{
			Schema:                  manifest.Schema,
			Product:                 manifest.Product,
			Version:                 manifest.Version,
			DependencyPolicyVersion: manifest.DependencyPolicyVersion,
			ReleaseNotes:            manifest.ReleaseNotes,
			RequiredPlatforms:       slices.Clone(manifest.RequiredPlatforms),
			Components:              components,
			Requirements:            cloneProductReleaseRequirements(manifest.Requirements),
		},
	}
}

func ProductReleaseValidationToAPI(
	issues []types.ProductReleaseValidationIssue,
) api.ProductReleaseValidationResponse {
	response := api.ProductReleaseValidationResponse{
		Valid:  len(issues) == 0,
		Issues: make([]api.ProductReleaseValidationIssue, 0, len(issues)),
	}
	for _, issue := range issues {
		response.Issues = append(response.Issues, api.ProductReleaseValidationIssue{
			Field: issue.Field, Rule: issue.Rule, Message: issue.Message, Path: slices.Clone(issue.Path),
		})
	}
	return response
}

func cloneProductReleaseRequirements(
	input []types.CapabilityRequirement,
) []types.CapabilityRequirement {
	result := slices.Clone(input)
	for index := range result {
		result[index].AllowedModes = slices.Clone(input[index].AllowedModes)
	}
	return result
}
