package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestTargetRequirementsFromGraphPreservesEverySymbolicTargetRequirement(t *testing.T) {
	g := NewWithT(t)
	graph := types.ProductReleaseGraph{Edges: []types.GraphEdge{
		{
			Key: "product", From: "component:provider", To: "component:consumer",
			Capability: "cache", ResolutionStage: types.CapabilityResolutionStageProduct,
		},
		{
			Key: "target-b", From: "target:consumer:email", To: "component:consumer",
			Capability: "email", VersionRange: "^2.0.0",
			ResolutionStage: types.CapabilityResolutionStageTarget,
			AllowedModes: []types.RequirementResolutionMode{
				types.RequirementResolutionModeApprovedExternal,
			},
		},
		{
			Key: "target-a", From: "target:consumer:cache", To: "component:consumer",
			Capability: "cache", VersionRange: "^1.0.0",
			ResolutionStage: types.CapabilityResolutionStageTarget,
			AllowedModes: []types.RequirementResolutionMode{
				types.RequirementResolutionModeIncluded,
			},
		},
	}}

	requirements := targetRequirementsFromGraph(graph)

	g.Expect(requirements).To(HaveLen(2))
	g.Expect(requirements[0].Key).To(Equal("target:consumer:cache"))
	g.Expect(requirements[1].Key).To(Equal("target:consumer:email"))
}

func TestProjectTargetPlanStepsFreezesDependencies(t *testing.T) {
	g := NewWithT(t)
	graph := types.TargetPlanGraph{
		Steps: []types.TargetPlanStep{
			{StepKey: "a", Name: "A", InputBindings: []byte(`{}`), V1Compatible: true},
			{StepKey: "b", Name: "B", InputBindings: []byte(`{}`), V1Compatible: true},
		},
		Edges: []types.DeploymentPlanStepEdge{
			{Key: "a->b", FromStepKey: "a", ToStepKey: "b"},
		},
	}

	steps := projectTargetPlanSteps(graph)

	g.Expect(steps).To(HaveLen(2))
	g.Expect(steps[1].Dependencies).To(Equal([]string{"a"}))
	g.Expect(steps[0].ID).NotTo(Equal(uuid.Nil))
}

func TestMigration145HasTenantFencesImmutabilityAndRollbackRefusal(t *testing.T) {
	g := NewWithT(t)
	root := filepath.Join("..", "migrations", "sql")
	up, err := os.ReadFile(filepath.Join(root, "145_target_deployment_plan_v2.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join(root, "145_target_deployment_plan_v2.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upText := string(up)
	downText := string(down)

	for _, table := range []string{
		"CREATE TABLE DeploymentPlanDraft",
		"CREATE TABLE DeploymentPlanResolvedRequirement",
		"CREATE TABLE DeploymentPlanStepEdge",
	} {
		g.Expect(upText).To(ContainSubstring(table))
	}
	for _, column := range []string{
		"plan_schema", "draft_id", "deployment_unit_id",
		"target_config_snapshot_id", "protocol_version",
		"supersedes_deployment_plan_id", "supersede_reason",
	} {
		g.Expect(upText).To(ContainSubstring(column))
	}
	g.Expect(upText).To(ContainSubstring("FOREIGN KEY (draft_id, organization_id)"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanDraft_publication_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlan_v2_immutable_guard"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanResolvedRequirement_append_only"))
	g.Expect(upText).To(ContainSubstring("DeploymentPlanStepEdge_append_only"))
	g.Expect(downText).To(ContainSubstring("refusing migration 145 rollback"))
}

func TestDraftPublicationUsesRowLockAndExactOptimisticChecksum(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plan_drafts.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	g.Expect(text).To(ContainSubstring("RunTxIso(ctx, pgx.Serializable"))
	g.Expect(text).To(ContainSubstring("FOR UPDATE"))
	g.Expect(text).To(ContainSubstring("draft.Revision != expectedRevision"))
	g.Expect(text).To(ContainSubstring("validation.PreviewChecksum != expectedPreviewChecksum"))
	g.Expect(strings.Count(text, "organization_id = @organizationID")).To(
		BeNumerically(">=", 8),
	)
}
