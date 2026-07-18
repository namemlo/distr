package productrelease

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/distr-sh/distr/internal/types"
)

type canonicalProductRelease struct {
	Schema                  string                        `json:"schema"`
	Product                 string                        `json:"product"`
	Version                 string                        `json:"version"`
	DependencyPolicyVersion string                        `json:"dependencyPolicyVersion"`
	ReleaseNotes            string                        `json:"releaseNotes"`
	RequiredPlatforms       []string                      `json:"requiredPlatforms"`
	Components              []canonicalProductComponent   `json:"components"`
	Requirements            []types.CapabilityRequirement `json:"requirements"`
	Graph                   canonicalProductReleaseGraph  `json:"graph"`
}

type canonicalProductComponent struct {
	ComponentReleaseID       string `json:"componentReleaseId"`
	ComponentReleaseChecksum string `json:"componentReleaseChecksum"`
	ComponentKey             string `json:"componentKey"`
	Version                  string `json:"version"`
}

type canonicalProductReleaseGraph struct {
	Nodes            []types.GraphNode `json:"nodes"`
	Edges            []types.GraphEdge `json:"edges"`
	TopologicalOrder []string          `json:"topologicalOrder"`
	Checksum         string            `json:"checksum"`
}

func CanonicalizeProductRelease(manifest types.ProductReleaseManifest) ([]byte, string, error) {
	normalized := normalizedProductReleaseManifest(manifest)
	graph := BuildProductReleaseGraph(normalized)
	graphPayload, err := json.Marshal(struct {
		Nodes            []types.GraphNode `json:"nodes"`
		Edges            []types.GraphEdge `json:"edges"`
		TopologicalOrder []string          `json:"topologicalOrder"`
	}{
		Nodes: graph.Nodes, Edges: graph.Edges, TopologicalOrder: graph.TopologicalOrder,
	})
	if err != nil {
		return nil, "", err
	}
	graphSum := sha256.Sum256(graphPayload)
	graph.Checksum = "sha256:" + hex.EncodeToString(graphSum[:])

	canonical := canonicalProductRelease{
		Schema:                  types.ProductReleaseSchemaV1,
		Product:                 normalized.Product,
		Version:                 normalized.Version,
		DependencyPolicyVersion: normalized.DependencyPolicyVersion.String(),
		ReleaseNotes:            normalized.ReleaseNotes,
		RequiredPlatforms:       slices.Clone(normalized.RequiredPlatforms),
		Components:              make([]canonicalProductComponent, 0, len(normalized.Components)),
		Requirements:            slices.Clone(normalized.Requirements),
		Graph: canonicalProductReleaseGraph{
			Nodes:            graph.Nodes,
			Edges:            graph.Edges,
			TopologicalOrder: graph.TopologicalOrder,
			Checksum:         graph.Checksum,
		},
	}
	for _, component := range normalized.Components {
		canonical.Components = append(canonical.Components, canonicalProductComponent{
			ComponentReleaseID:       component.ComponentReleaseID.String(),
			ComponentReleaseChecksum: component.ComponentReleaseChecksum,
			ComponentKey:             component.ComponentKey,
			Version:                  component.Version,
		})
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(payload)
	return payload, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ProductReleaseGraphChecksum(manifest types.ProductReleaseManifest) (string, error) {
	normalized := normalizedProductReleaseManifest(manifest)
	graph := BuildProductReleaseGraph(normalized)
	payload, err := json.Marshal(struct {
		Nodes            []types.GraphNode `json:"nodes"`
		Edges            []types.GraphEdge `json:"edges"`
		TopologicalOrder []string          `json:"topologicalOrder"`
	}{
		Nodes: graph.Nodes, Edges: graph.Edges, TopologicalOrder: graph.TopologicalOrder,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func NormalizeProductReleaseManifest(manifest types.ProductReleaseManifest) types.ProductReleaseManifest {
	return normalizedProductReleaseManifest(manifest)
}

func normalizedProductReleaseManifest(manifest types.ProductReleaseManifest) types.ProductReleaseManifest {
	normalized := manifest
	normalized.Schema = types.ProductReleaseSchemaV1
	normalized.Product = strings.TrimSpace(normalized.Product)
	normalized.Version = strings.TrimSpace(normalized.Version)
	normalized.ReleaseNotes = strings.TrimSpace(normalized.ReleaseNotes)
	normalized.RequiredPlatforms = normalizedStrings(normalized.RequiredPlatforms)
	normalized.Components = normalizedComponents(normalized.Components)
	normalized.Requirements = slices.Clone(normalized.Requirements)
	for index := range normalized.Requirements {
		normalized.Requirements[index] = normalizedRequirement(normalized.Requirements[index])
	}
	slices.SortFunc(normalized.Requirements, func(a, b types.CapabilityRequirement) int {
		if cmp := strings.Compare(a.Name, b.Name); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.Range, b.Range); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ResolutionStage, b.ResolutionStage)
	})
	checksum, err := ProductReleaseGraphChecksumWithoutNormalization(normalized)
	if err == nil {
		normalized.GraphChecksum = checksum
	}
	return normalized
}

func ProductReleaseGraphChecksumWithoutNormalization(manifest types.ProductReleaseManifest) (string, error) {
	graph := BuildProductReleaseGraph(manifest)
	payload, err := json.Marshal(struct {
		Nodes            []types.GraphNode `json:"nodes"`
		Edges            []types.GraphEdge `json:"edges"`
		TopologicalOrder []string          `json:"topologicalOrder"`
	}{
		Nodes: graph.Nodes, Edges: graph.Edges, TopologicalOrder: graph.TopologicalOrder,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
