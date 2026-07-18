package planning

import (
	"slices"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestClassifyDeploymentRiskBlocksForwardOnlyAndNonAuthoritativeBaseline(t *testing.T) {
	g := NewWithT(t)
	changes := []types.DeploymentPlanChangeEntry{
		{Kind: types.DeploymentPlanChangeSchema, ComponentKey: "ledger", ForwardOnly: true},
		{
			Kind: types.DeploymentPlanChangeBaselineAuthority, ComponentKey: "ledger",
			Before: string(types.BaselineProjectionLegacy),
		},
	}

	risks := ClassifyDeploymentRisk(changes, types.PlanRiskPolicy{
		AllowForwardOnlyMigration:      false,
		RequireAuthoritativeV2Baseline: true,
	})

	g.Expect(risks).To(HaveLen(2))
	g.Expect(risks[0].Code).To(Equal("baseline_not_v2_authoritative"))
	g.Expect(risks[0].Blocking).To(BeTrue())
	g.Expect(risks[1].Code).To(Equal("forward_only_migration"))
	g.Expect(risks[1].Blocking).To(BeTrue())
}

func TestClassifyDeploymentRiskRequiresBootstrapApproval(t *testing.T) {
	g := NewWithT(t)

	risks := ClassifyDeploymentRisk(
		[]types.DeploymentPlanChangeEntry{{Kind: types.DeploymentPlanChangeBootstrap, ComponentKey: "worker"}},
		types.PlanRiskPolicy{RequireBootstrapApproval: true},
	)

	g.Expect(risks).To(HaveLen(1))
	g.Expect(risks[0].Code).To(Equal("bootstrap_approval_required"))
	g.Expect(risks[0].Level).To(Equal(types.DeploymentPlanRiskHigh))
	g.Expect(risks[0].Blocking).To(BeTrue())
}

func TestClassifyDeploymentRiskHasStableOrderForEquivalentChanges(t *testing.T) {
	g := NewWithT(t)
	changes := []types.DeploymentPlanChangeEntry{
		{
			ComponentInstanceID: uuid.MustParse("00000000-0000-0000-0000-000000000002"),
			ComponentKey:        "worker",
			Kind:                types.DeploymentPlanChangeTopology,
		},
		{
			ComponentInstanceID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			ComponentKey:        "api",
			Kind:                types.DeploymentPlanChangeProvider,
		},
		{
			ComponentInstanceID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			ComponentKey:        "api",
			Kind:                types.DeploymentPlanChangeSchema,
			ForwardOnly:         true,
		},
	}
	policy := types.PlanRiskPolicy{}
	reversed := slices.Clone(changes)
	slices.Reverse(reversed)

	risks := ClassifyDeploymentRisk(changes, policy)
	reversedRisks := ClassifyDeploymentRisk(reversed, policy)

	g.Expect(risks).To(Equal(reversedRisks))
	g.Expect(risks).To(HaveLen(3))
	g.Expect(risks[0].Code).To(Equal("provider_binding_change"))
	g.Expect(risks[1].Code).To(Equal("forward_only_migration"))
	g.Expect(risks[2].Code).To(Equal("topology_change"))
}
