package planning

import (
	"context"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCanonicalTargetPlanFreezesAdaptersDeterministically(t *testing.T) {
	g := NewWithT(t)
	firstID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	secondID := uuid.MustParse("10000000-0000-0000-0000-000000000002")
	first := types.ResolvedPlanStepAdapter{
		StepKey: "component:web:health",
		ResolvedStepAdapter: types.ResolvedStepAdapter{
			AdapterAssignmentID: firstID, AdapterImplementationID: firstID,
			ImplementationVersion: "2.0.0", Capability: "health.http",
			CapabilityVersion: "1.0.0",
		},
	}
	second := types.ResolvedPlanStepAdapter{
		StepKey: "component:web:deploy",
		ResolvedStepAdapter: types.ResolvedStepAdapter{
			AdapterAssignmentID: secondID, AdapterImplementationID: secondID,
			ImplementationVersion: "3.0.0", Capability: "deployment.compose",
			CapabilityVersion: "1.0.0",
		},
	}
	left := types.TargetDeploymentPlanCanonical{
		StepAdapters: []types.ResolvedPlanStepAdapter{first, second},
	}
	right := types.TargetDeploymentPlanCanonical{
		StepAdapters: []types.ResolvedPlanStepAdapter{second, first},
	}

	leftPayload, leftChecksum, err := CanonicalizeTargetDeploymentPlan(left)
	g.Expect(err).NotTo(HaveOccurred())
	rightPayload, rightChecksum, err := CanonicalizeTargetDeploymentPlan(right)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rightPayload).To(Equal(leftPayload))
	g.Expect(rightChecksum).To(Equal(leftChecksum))

	right.StepAdapters[0].ImplementationVersion = "3.1.0"
	_, driftedChecksum, err := CanonicalizeTargetDeploymentPlan(right)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(driftedChecksum).NotTo(Equal(leftChecksum))
}

func TestBuildTargetPlanGraphIsStableAndAcyclic(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())

	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(graph.Checksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(graph.TopologicalOrder).To(HaveLen(len(graph.Steps)))
	g.Expect(graph.Steps[0].StepKey).To(Equal("config:verify"))
	for _, edge := range graph.Edges {
		g.Expect(indexOf(graph.TopologicalOrder, edge.FromStepKey)).To(
			BeNumerically("<", indexOf(graph.TopologicalOrder, edge.ToStepKey)),
		)
	}

	draft.ResolutionInput.ReleasePins = append(
		[]types.ComponentReleasePin(nil),
		draft.ResolutionInput.ReleasePins...,
	)
	second, err := BuildTargetPlanGraph(context.Background(), draft, reverseResolutions(resolutions))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).To(Equal(graph))
}

func TestBuildTargetPlanGraphOrdersProviderHealthBeforeConsumerDeploy(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ResolutionInput.ReleasePins = append(
		draft.ResolutionInput.ReleasePins,
		types.ComponentReleasePin{
			ComponentKey:       "provider",
			ComponentReleaseID: *draft.ResolutionInput.Candidates[0].ProviderReleaseID,
			ReleaseChecksum:    checksum("f"),
			Platforms:          []string{"linux/amd64"},
			ProvenanceVerified: true,
		},
	)
	draft.ResolutionInput.ProductEdges = []types.GraphEdge{{
		Key:             "component:provider->component:consumer:cache",
		From:            "component:provider",
		To:              "component:consumer",
		Capability:      "cache",
		VersionRange:    "^1.0.0",
		ProviderVersion: "1.4.0",
		ResolutionStage: types.CapabilityResolutionStageProduct,
		Ordering:        "provider_deploy_and_health_before_consumer",
	}}

	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())
	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(graph.Edges).To(ContainElement(types.DeploymentPlanStepEdge{
		Key:         "component:provider:health->component:consumer:deploy",
		FromStepKey: "component:provider:health",
		ToStepKey:   "component:consumer:deploy",
	}))
}

func TestBuildTargetPlanGraphGatesConsumerFirstMigration(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ResolutionInput.ReleasePins[0].Migrations = []types.MigrationDeclaration{{
		Key: "schema", Type: "database", Order: 1,
	}}
	draft.ResolutionInput.ReleasePins = append(
		draft.ResolutionInput.ReleasePins,
		types.ComponentReleasePin{
			ComponentKey:       "provider",
			ComponentReleaseID: *draft.ResolutionInput.Candidates[0].ProviderReleaseID,
			ReleaseChecksum:    checksum("f"),
			Platforms:          []string{"linux/amd64"},
			ProvenanceVerified: true,
		},
	)
	draft.ResolutionInput.ProductEdges = []types.GraphEdge{{
		Key:             "component:provider->component:consumer:cache",
		From:            "component:provider",
		To:              "component:consumer",
		Capability:      "cache",
		VersionRange:    "^1.0.0",
		ProviderVersion: "1.4.0",
		ResolutionStage: types.CapabilityResolutionStageProduct,
	}}

	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())
	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	first := "component:consumer:migration:schema"
	g.Expect(graph.Edges).To(ContainElement(types.DeploymentPlanStepEdge{
		Key:         "component:provider:health->" + first,
		FromStepKey: "component:provider:health",
		ToStepKey:   first,
	}))
	requirementKey := "requirement:target.consumer.cache:verify"
	g.Expect(graph.Edges).To(ContainElement(types.DeploymentPlanStepEdge{
		Key:         requirementKey + "->" + first,
		FromStepKey: requirementKey,
		ToStepKey:   first,
	}))
}

func TestBuildTargetPlanGraphRejectsCycles(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ResolutionInput.ReleasePins = append(
		draft.ResolutionInput.ReleasePins,
		types.ComponentReleasePin{
			ComponentKey:       "provider",
			ComponentReleaseID: *draft.ResolutionInput.Candidates[0].ProviderReleaseID,
			ReleaseChecksum:    checksum("f"),
			Platforms:          []string{"linux/amd64"},
			ProvenanceVerified: true,
		},
	)
	draft.ResolutionInput.ProductEdges = []types.GraphEdge{
		{
			Key: "a", From: "component:provider", To: "component:consumer",
			ResolutionStage: types.CapabilityResolutionStageProduct,
		},
		{
			Key: "b", From: "component:consumer", To: "component:provider",
			ResolutionStage: types.CapabilityResolutionStageProduct,
		},
	}
	resolutions, _ := ResolveTargetRequirements(context.Background(), draft)

	_, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).To(MatchError(ContainSubstring("cycle")))
}

func TestValidateProtocolV1RequiresCompatibleSteps(t *testing.T) {
	g := NewWithT(t)
	graph := types.TargetPlanGraph{
		Steps: []types.TargetPlanStep{{
			StepKey: "future", V1Compatible: false,
		}},
	}

	g.Expect(ValidateProtocolGraph(types.DeploymentPlanProtocolV1, graph)).
		To(MatchError(ContainSubstring("not compatible")))
	g.Expect(ValidateProtocolGraph(types.DeploymentPlanProtocolV2, graph)).To(Succeed())
}

func TestBuildTargetPlanGraphHasReachableProtocolV1Projection(t *testing.T) {
	g := NewWithT(t)
	draft := resolverFixture()
	draft.ProtocolVersion = types.DeploymentPlanProtocolV1
	resolutions, issues := ResolveTargetRequirements(context.Background(), draft)
	g.Expect(issues).To(BeEmpty())

	graph, err := BuildTargetPlanGraph(context.Background(), draft, resolutions)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ValidateProtocolGraph(types.DeploymentPlanProtocolV1, graph)).To(Succeed())
	for _, step := range graph.Steps {
		g.Expect(step.V1Compatible).To(BeTrue(), step.StepKey)
	}
}

func TestCanonicalizeTargetDeploymentPlanRejectsOversizedPayload(t *testing.T) {
	g := NewWithT(t)
	canonical := types.TargetDeploymentPlanCanonical{
		Schema:                 types.TargetDeploymentPlanSchemaV2,
		ProductReleaseID:       uuid.New(),
		ProductReleaseChecksum: checksum("a"),
		Graph: types.TargetPlanGraph{Steps: []types.TargetPlanStep{{
			StepKey:       "huge",
			Name:          strings.Repeat("x", MaxTargetPlanPayloadBytes),
			V1Compatible:  true,
			InputBindings: []byte(`{}`),
		}}},
	}

	_, _, err := CanonicalizeTargetDeploymentPlan(canonical)

	g.Expect(err).To(MatchError(ContainSubstring("payload limit")))
}

func reverseResolutions(values []types.RequirementResolution) []types.RequirementResolution {
	result := append([]types.RequirementResolution(nil), values...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}
