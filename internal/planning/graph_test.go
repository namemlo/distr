package planning

import (
	"context"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

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
