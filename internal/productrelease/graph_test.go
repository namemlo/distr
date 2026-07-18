package productrelease

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestProductReleaseGraphProviderBeforeConsumer(t *testing.T) {
	g := NewWithT(t)
	manifest := neutralProviderConsumerManifest()

	graph := BuildProductReleaseGraph(manifest)
	g.Expect(graph.TopologicalOrder).To(Equal([]string{"component:provider", "component:consumer"}))
	g.Expect(graph.Edges).To(ConsistOf(types.GraphEdge{
		Key:             "component:provider->component:consumer:transactions",
		From:            "component:provider",
		To:              "component:consumer",
		Capability:      "transactions",
		VersionRange:    ">=1.0.0 <2.0.0",
		ProviderVersion: "1.4.0",
		ResolutionStage: types.CapabilityResolutionStageProduct,
		Ordering:        "provider_deploy_and_health_before_consumer",
	}))
}

func TestProductReleaseGraphRetainsTargetDeferredRequirement(t *testing.T) {
	g := NewWithT(t)
	manifest := neutralProviderConsumerManifest()
	manifest.Components[1].Requires[0].ResolutionStage = "target"
	manifest.Components[1].Requires[0].AllowedModes = []string{"shared_provider", "included"}

	issues := ValidateProductReleaseGraph(manifest)
	g.Expect(issues).To(BeEmpty())
	graph := BuildProductReleaseGraph(manifest)
	g.Expect(graph.Nodes).To(ContainElement(types.GraphNode{
		Key:             "target:consumer:transactions",
		Kind:            "target_requirement",
		Capability:      "transactions",
		VersionRange:    ">=1.0.0 <2.0.0",
		ResolutionStage: types.CapabilityResolutionStageTarget,
		AllowedModes: []types.RequirementResolutionMode{
			types.RequirementResolutionModeIncluded,
			types.RequirementResolutionModeSharedProvider,
		},
		Unresolved: true,
	}))
	g.Expect(graph.Edges[0].From).To(Equal("target:consumer:transactions"))
	g.Expect(graph.Edges[0].To).To(Equal("component:consumer"))
}

func TestProductReleaseValidationFailures(t *testing.T) {
	t.Run("exact cycle path", func(t *testing.T) {
		g := NewWithT(t)
		manifest := neutralProviderConsumerManifest()
		manifest.Components[0].Requires = []types.CapabilityRequirement{{
			Name: "consumer-api", Range: "^2.0.0", ResolutionStage: "product",
		}}
		manifest.Components[1].Provides = []types.CapabilityDeclaration{{
			Name: "consumer-api", Version: "2.1.0",
		}}
		issues := ValidateProductReleaseGraph(manifest)
		g.Expect(issues).To(ContainElement(And(
			HaveField("Rule", "cycle"),
			HaveField("Path", []string{"component:consumer", "component:provider", "component:consumer"}),
		)))
	})

	tests := []struct {
		name   string
		mutate func(*types.ProductReleaseManifest)
		rule   string
	}{
		{
			name: "missing provider",
			mutate: func(m *types.ProductReleaseManifest) {
				m.Components[0].Provides = nil
			},
			rule: "missingProvider",
		},
		{
			name: "ambiguous provider",
			mutate: func(m *types.ProductReleaseManifest) {
				other := m.Components[0]
				other.ComponentReleaseID = uuid.New()
				other.ComponentKey = "provider-two"
				m.Components = append(m.Components, other)
			},
			rule: "ambiguousProvider",
		},
		{
			name: "incompatible range",
			mutate: func(m *types.ProductReleaseManifest) {
				m.Components[1].Requires[0].Range = ">=3.0.0"
			},
			rule: "incompatibleRange",
		},
		{
			name: "unpublished child",
			mutate: func(m *types.ProductReleaseManifest) {
				m.Components[0].Published = false
			},
			rule: "published",
		},
		{
			name: "foreign child",
			mutate: func(m *types.ProductReleaseManifest) {
				m.Components[0].OrganizationID = uuid.New()
			},
			rule: "organization",
		},
		{
			name: "duplicate component",
			mutate: func(m *types.ProductReleaseManifest) {
				duplicate := m.Components[0]
				duplicate.ComponentReleaseID = uuid.New()
				m.Components = append(m.Components, duplicate)
			},
			rule: "uniqueComponent",
		},
		{
			name: "duplicate product requirement",
			mutate: func(m *types.ProductReleaseManifest) {
				requirement := types.CapabilityRequirement{
					Name: "transactions", Range: "^1.0.0", ResolutionStage: "product",
				}
				m.Requirements = []types.CapabilityRequirement{requirement, requirement}
			},
			rule: "uniqueRequirement",
		},
		{
			name: "product stage gap",
			mutate: func(m *types.ProductReleaseManifest) {
				m.Components[0].Provides = nil
			},
			rule: "productStageGap",
		},
		{
			name: "target mode required",
			mutate: func(m *types.ProductReleaseManifest) {
				m.Components[1].Requires[0].ResolutionStage = "target"
				m.Components[1].Requires[0].AllowedModes = nil
			},
			rule: "allowedModes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			manifest := neutralProviderConsumerManifest()
			tt.mutate(&manifest)
			g.Expect(ValidateProductReleaseGraph(manifest)).To(
				ContainElement(HaveField("Rule", tt.rule)),
			)
		})
	}
}

func TestProductReleaseDuplicateMigrationRequiresEveryFunctionalFieldToMatch(t *testing.T) {
	baseline := types.MigrationDeclaration{
		Key:           "ledger-schema-42",
		Type:          "sql",
		Order:         42,
		Compatibility: "backward",
		FailurePolicy: "stop",
		Description:   "add immutable ledger index",
	}
	mutations := []struct {
		name   string
		mutate func(*types.MigrationDeclaration)
	}{
		{name: "type", mutate: func(value *types.MigrationDeclaration) { value.Type = "job" }},
		{name: "order", mutate: func(value *types.MigrationDeclaration) { value.Order++ }},
		{
			name: "compatibility",
			mutate: func(value *types.MigrationDeclaration) {
				value.Compatibility = "breaking"
			},
		},
		{
			name: "failure policy",
			mutate: func(value *types.MigrationDeclaration) {
				value.FailurePolicy = "continue"
			},
		},
		{
			name: "description",
			mutate: func(value *types.MigrationDeclaration) {
				value.Description = "different operation"
			},
		},
	}

	t.Run("identical duplicate declaration", func(t *testing.T) {
		g := NewWithT(t)
		manifest := neutralProviderConsumerManifest()
		manifest.Components[0].Migrations = []types.MigrationDeclaration{baseline}
		manifest.Components[1].Migrations = []types.MigrationDeclaration{baseline}
		g.Expect(ValidateProductReleaseGraph(manifest)).NotTo(
			ContainElement(HaveField("Rule", "migrationConflict")),
		)
	})
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			manifest := neutralProviderConsumerManifest()
			other := baseline
			tt.mutate(&other)
			manifest.Components[0].Migrations = []types.MigrationDeclaration{baseline}
			manifest.Components[1].Migrations = []types.MigrationDeclaration{other}
			g.Expect(ValidateProductReleaseGraph(manifest)).To(
				ContainElement(HaveField("Rule", "migrationConflict")),
			)
		})
	}
}

func TestProductReleaseGraphRejectsUnboundedAggregateRequirements(t *testing.T) {
	g := NewWithT(t)
	manifest := neutralProviderConsumerManifest()
	manifest.Requirements = make(
		[]types.CapabilityRequirement,
		types.ProductReleaseMaxRequirements+1,
	)
	issues := ValidateProductReleaseGraph(manifest)
	g.Expect(issues).To(ContainElement(And(
		HaveField("Field", "requirements"),
		HaveField("Rule", "maxItems"),
	)))
}

func neutralProviderConsumerManifest() types.ProductReleaseManifest {
	orgID := uuid.New()
	return types.ProductReleaseManifest{
		Schema:                  types.ProductReleaseSchemaV1,
		ReleaseBundleID:         uuid.New(),
		OrganizationID:          orgID,
		Product:                 "neutral-suite",
		Version:                 "2026.7.0",
		DependencyPolicyVersion: uuid.New(),
		Components: []types.ProductReleaseComponent{
			{
				ComponentReleaseID:       uuid.New(),
				ComponentReleaseChecksum: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ComponentKey:             "provider",
				Version:                  "1.4.0",
				OrganizationID:           orgID,
				Published:                true,
				Provides: []types.CapabilityDeclaration{{
					Name: "transactions", Version: "1.4.0",
				}},
				Platforms: []string{"linux/amd64"},
			},
			{
				ComponentReleaseID:       uuid.New(),
				ComponentReleaseChecksum: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				ComponentKey:             "consumer",
				Version:                  "2.0.0",
				OrganizationID:           orgID,
				Published:                true,
				Requires: []types.CapabilityRequirement{{
					Name:            "transactions",
					Range:           ">=1.0.0 <2.0.0",
					ResolutionStage: "product",
				}},
				Platforms: []string{"linux/amd64"},
			},
		},
		RequiredPlatforms: []string{"linux/amd64"},
	}
}
