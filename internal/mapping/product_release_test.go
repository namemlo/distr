package mapping

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestProductReleaseToAPIExposesCompleteFrozenManifestAndGraph(t *testing.T) {
	g := NewWithT(t)
	componentReleaseID := uuid.New()
	migrationContract := types.MigrationContract{
		ID: "ledger.042", Checksum: "sha256:" + strings.Repeat("c", 64),
		ComponentKey: "ledger-api", DatabaseResourceKey: "ledger-db",
		ExpectedSourceVersion: "41", ResultingVersion: "42",
		DependsOn: []string{}, PreconditionProbes: []types.MigrationProbe{},
		PostconditionProbes: []types.MigrationProbe{},
	}
	componentContract := types.ComponentReleaseContractV2{
		Schema: types.ReleaseContractSchemaV2, ComponentKey: "ledger-api", Version: "1.2.3",
		Artifacts: []types.ComponentReleaseArtifact{{
			Key: "ledger-api", Type: "oci-image",
			MediaType: "application/vnd.oci.image.manifest.v1+json",
			Digest:    "sha256:" + strings.Repeat("a", 64),
			Platforms: []types.ComponentReleasePlatform{{
				Platform: "linux/amd64", Digest: "sha256:" + strings.Repeat("b", 64),
			}},
		}},
		Provides: []types.CapabilityDeclaration{{Name: "ledger.api", Version: "1.2.3"}},
		Requires: []types.CapabilityRequirement{{
			Name: "identity.api", Range: "=4.5.6", ResolutionStage: "target",
			AllowedModes: []string{"included", "pinned_existing"},
		}},
		Migrations:         []types.MigrationDeclaration{{Key: "ledger.042", Type: "database", Order: 1}},
		MigrationContracts: []types.MigrationContract{migrationContract},
	}
	graph := types.ProductReleaseGraph{
		Nodes: []types.GraphNode{{
			Key: "component:ledger-api", Kind: "component",
			AllowedModes: []types.RequirementResolutionMode{},
		}},
		Edges: []types.GraphEdge{{
			Key: "identity-edge", From: "requirement:identity.api", To: "component:ledger-api",
			Capability: "identity.api", VersionRange: "=4.5.6",
			ResolutionStage: types.CapabilityResolutionStageTarget,
			AllowedModes: []types.RequirementResolutionMode{
				types.RequirementResolutionModeIncluded,
				types.RequirementResolutionModePinnedExisting,
			},
		}},
		TopologicalOrder: []string{"component:ledger-api", "requirement:identity.api"},
		Checksum:         "sha256:" + strings.Repeat("d", 64),
	}
	result, err := ProductReleaseToAPI(types.ReleaseBundle{}, types.ProductReleaseManifest{
		DependencyPolicyChecksum: "sha256:" + strings.Repeat("e", 64),
		GraphChecksum:            graph.Checksum,
		Components: []types.ProductReleaseComponent{{
			ComponentReleaseID: componentReleaseID, ComponentReleaseChecksum: "sha256:" + strings.Repeat("f", 64),
			ComponentKey: "ledger-api", Version: "1.2.3", Platforms: []string{"linux/amd64"},
			Provides: componentContract.Provides, Requires: componentContract.Requires,
			Migrations: componentContract.Migrations, MigrationContracts: []types.MigrationContract{migrationContract},
			Contract: &componentContract,
		}},
	}, graph)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Manifest.Components).To(HaveLen(1))
	g.Expect(result.Manifest.Components[0].MigrationContracts).To(Equal(
		[]types.MigrationContract{migrationContract},
	))
	g.Expect(result.Manifest.Components[0].Artifacts).To(Equal(componentContract.Artifacts))
	g.Expect(result.Manifest.Components[0].Provides).To(Equal(componentContract.Provides))
	g.Expect(result.Manifest.Components[0].Requires).To(Equal(componentContract.Requires))
	g.Expect(result.Manifest.Components[0].Migrations).To(Equal(componentContract.Migrations))
	g.Expect(result.Manifest.Components[0].Platforms).To(Equal([]string{"linux/amd64"}))
	g.Expect(result.Graph).To(Equal(graph))
	g.Expect(result.GraphChecksum).To(Equal(graph.Checksum))

	componentContract.MigrationContracts = nil
	result, err = ProductReleaseToAPI(types.ReleaseBundle{}, types.ProductReleaseManifest{
		DependencyPolicyChecksum: "sha256:" + strings.Repeat("e", 64),
		GraphChecksum:            graph.Checksum,
		Components: []types.ProductReleaseComponent{{
			ComponentReleaseID: componentReleaseID, ComponentReleaseChecksum: "sha256:" + strings.Repeat("f", 64),
			ComponentKey: "ledger-api", Version: "1.2.3", Platforms: []string{"linux/amd64"},
			Contract: &componentContract,
		}},
	}, graph)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Manifest.Components[0].MigrationContracts).To(BeEmpty())
	g.Expect(result.Manifest.Components[0].MigrationContracts).NotTo(BeNil())
}

func TestProductReleaseToAPIFailsClosedForUnavailableNativeFacts(t *testing.T) {
	graphChecksum := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name     string
		manifest types.ProductReleaseManifest
		graph    types.ProductReleaseGraph
		want     string
	}{
		{
			name: "missing dependency policy checksum",
			manifest: types.ProductReleaseManifest{
				GraphChecksum: graphChecksum,
			},
			graph: types.ProductReleaseGraph{Checksum: graphChecksum},
			want:  "dependency-policy checksum",
		},
		{
			name: "missing frozen graph checksum",
			manifest: types.ProductReleaseManifest{
				DependencyPolicyChecksum: "sha256:" + strings.Repeat("b", 64),
			},
			graph: types.ProductReleaseGraph{Checksum: graphChecksum},
			want:  "frozen graph checksum",
		},
		{
			name: "missing component contract",
			manifest: types.ProductReleaseManifest{
				DependencyPolicyChecksum: "sha256:" + strings.Repeat("b", 64), GraphChecksum: graphChecksum,
				Components: []types.ProductReleaseComponent{{ComponentReleaseID: uuid.New()}},
			},
			graph: types.ProductReleaseGraph{Checksum: graphChecksum},
			want:  "component contract snapshot is unavailable",
		},
		{
			name: "graph checksum mismatch",
			manifest: types.ProductReleaseManifest{
				DependencyPolicyChecksum: "sha256:" + strings.Repeat("b", 64), GraphChecksum: graphChecksum,
			},
			graph: types.ProductReleaseGraph{Checksum: "sha256:" + strings.Repeat("c", 64)},
			want:  "graph checksum",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			_, err := ProductReleaseToAPI(types.ReleaseBundle{}, tt.manifest, tt.graph)
			g.Expect(err).To(MatchError(ContainSubstring(tt.want)))
		})
	}
}
