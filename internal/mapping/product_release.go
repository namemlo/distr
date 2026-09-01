package mapping

import (
	"strings"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
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
		Schema:                   request.Schema,
		OrganizationID:           organizationID,
		ApplicationID:            request.ApplicationID,
		ChannelID:                request.ChannelID,
		Product:                  request.Product,
		Version:                  request.Version,
		DependencyPolicyVersion:  request.DependencyPolicyVersion,
		DependencyPolicyChecksum: request.DependencyPolicyChecksum,
		ReleaseNotes:             request.ReleaseNotes,
		RequiredPlatforms:        cloneNonNilProductReleaseSlice(request.RequiredPlatforms),
		Components:               components,
		Requirements:             cloneProductReleaseRequirements(request.Requirements),
	}
}

func ProductReleaseToAPI(
	bundle types.ReleaseBundle,
	manifest types.ProductReleaseManifest,
	graph types.ProductReleaseGraph,
) (api.ProductRelease, error) {
	if strings.TrimSpace(manifest.DependencyPolicyChecksum) == "" {
		return api.ProductRelease{}, apierrors.NewConflict(
			"Product Release dependency-policy checksum is unavailable",
		)
	}
	if strings.TrimSpace(graph.Checksum) == "" {
		return api.ProductRelease{}, apierrors.NewConflict("Product Release graph checksum is unavailable")
	}
	if strings.TrimSpace(manifest.GraphChecksum) == "" {
		return api.ProductRelease{}, apierrors.NewConflict(
			"Product Release frozen graph checksum is unavailable",
		)
	}
	if manifest.GraphChecksum != graph.Checksum {
		return api.ProductRelease{}, apierrors.NewConflict(
			"Product Release graph checksum does not match the frozen graph",
		)
	}
	if len(manifest.Components) == 0 {
		return api.ProductRelease{}, apierrors.NewConflict("Product Release components are unavailable")
	}
	components := make([]api.ProductReleaseComponent, 0, len(manifest.Components))
	for _, component := range manifest.Components {
		if component.Contract == nil {
			return api.ProductRelease{}, apierrors.NewConflict(
				"Product Release component contract snapshot is unavailable",
			)
		}
		if component.ComponentKey != component.Contract.ComponentKey ||
			component.Version != component.Contract.Version {
			return api.ProductRelease{}, apierrors.NewConflict(
				"Product Release component identity does not match the frozen contract",
			)
		}
		if component.ComponentReleaseID == uuid.Nil ||
			strings.TrimSpace(component.ComponentReleaseChecksum) == "" ||
			len(component.Platforms) == 0 ||
			len(component.Contract.Artifacts) == 0 {
			return api.ProductRelease{}, apierrors.NewConflict(
				"Product Release component manifest is incomplete",
			)
		}
		for _, artifact := range component.Contract.Artifacts {
			if strings.TrimSpace(artifact.Key) == "" ||
				strings.TrimSpace(artifact.MediaType) == "" ||
				strings.TrimSpace(artifact.Digest) == "" ||
				len(artifact.Platforms) == 0 {
				return api.ProductRelease{}, apierrors.NewConflict(
					"Product Release component artifact identity is incomplete",
				)
			}
			for _, platform := range artifact.Platforms {
				if strings.TrimSpace(platform.Platform) == "" || strings.TrimSpace(platform.Digest) == "" {
					return api.ProductRelease{}, apierrors.NewConflict(
						"Product Release component platform identity is incomplete",
					)
				}
			}
		}
		components = append(components, api.ProductReleaseComponent{
			ComponentReleaseID:       component.ComponentReleaseID,
			ComponentReleaseChecksum: component.ComponentReleaseChecksum,
			ComponentKey:             component.ComponentKey,
			Version:                  component.Version,
			Platforms:                cloneNonNilProductReleaseSlice(component.Platforms),
			Artifacts:                cloneProductReleaseArtifacts(component.Contract.Artifacts),
			Provides:                 cloneNonNilProductReleaseSlice(component.Provides),
			Requires:                 cloneProductReleaseRequirements(component.Requires),
			Migrations:               cloneNonNilProductReleaseSlice(component.Migrations),
			MigrationContracts:       cloneProductReleaseMigrationContracts(component.MigrationContracts),
		})
	}
	if len(graph.TopologicalOrder) == 0 {
		return api.ProductRelease{}, apierrors.NewConflict("Product Release graph order is unavailable")
	}
	return api.ProductRelease{
		ID:                       bundle.ID,
		CreatedAt:                bundle.CreatedAt,
		UpdatedAt:                bundle.UpdatedAt,
		ApplicationID:            bundle.ApplicationID,
		ChannelID:                bundle.ChannelID,
		Status:                   bundle.Status,
		CanonicalChecksum:        bundle.CanonicalChecksum,
		GraphChecksum:            graph.Checksum,
		PublishedByUserAccountID: bundle.PublishedByUserAccountID,
		PublishedAt:              bundle.PublishedAt,
		Graph:                    cloneProductReleaseGraph(graph),
		Manifest: api.ProductReleaseManifest{
			Schema:                   manifest.Schema,
			Product:                  manifest.Product,
			Version:                  manifest.Version,
			DependencyPolicyVersion:  manifest.DependencyPolicyVersion,
			DependencyPolicyChecksum: manifest.DependencyPolicyChecksum,
			ReleaseNotes:             manifest.ReleaseNotes,
			RequiredPlatforms:        cloneNonNilProductReleaseSlice(manifest.RequiredPlatforms),
			Components:               components,
			Requirements:             cloneProductReleaseRequirements(manifest.Requirements),
		},
	}, nil
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
			Field: issue.Field, Rule: issue.Rule, Message: issue.Message,
			Path: cloneNonNilProductReleaseSlice(issue.Path),
		})
	}
	return response
}

func cloneProductReleaseRequirements(
	input []types.CapabilityRequirement,
) []types.CapabilityRequirement {
	result := cloneNonNilProductReleaseSlice(input)
	for index := range result {
		result[index].AllowedModes = cloneNonNilProductReleaseSlice(input[index].AllowedModes)
	}
	return result
}

func cloneProductReleaseArtifacts(
	input []types.ComponentReleaseArtifact,
) []types.ComponentReleaseArtifact {
	result := cloneNonNilProductReleaseSlice(input)
	for index := range result {
		result[index].Platforms = cloneNonNilProductReleaseSlice(input[index].Platforms)
	}
	return result
}

func cloneProductReleaseMigrationContracts(
	input []types.MigrationContract,
) []types.MigrationContract {
	result := cloneNonNilProductReleaseSlice(input)
	for index := range result {
		result[index].DependsOn = cloneNonNilProductReleaseSlice(input[index].DependsOn)
		result[index].PreconditionProbes = cloneNonNilProductReleaseSlice(input[index].PreconditionProbes)
		result[index].PostconditionProbes = cloneNonNilProductReleaseSlice(input[index].PostconditionProbes)
	}
	return result
}

func cloneProductReleaseGraph(input types.ProductReleaseGraph) types.ProductReleaseGraph {
	result := input
	result.Nodes = cloneNonNilProductReleaseSlice(input.Nodes)
	for index := range result.Nodes {
		result.Nodes[index].AllowedModes = cloneNonNilProductReleaseSlice(input.Nodes[index].AllowedModes)
	}
	result.Edges = cloneNonNilProductReleaseSlice(input.Edges)
	for index := range result.Edges {
		result.Edges[index].AllowedModes = cloneNonNilProductReleaseSlice(input.Edges[index].AllowedModes)
	}
	result.TopologicalOrder = cloneNonNilProductReleaseSlice(input.TopologicalOrder)
	return result
}

func cloneNonNilProductReleaseSlice[T any](input []T) []T {
	result := make([]T, len(input))
	copy(result, input)
	return result
}
