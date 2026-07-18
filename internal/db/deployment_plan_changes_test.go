package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestMigration146CreatesTenantFencedAppendOnlyPlanEvidence(t *testing.T) {
	g := NewWithT(t)
	root := filepath.Join("..", "migrations", "sql")
	up, err := os.ReadFile(filepath.Join(root, "146_deployment_plan_baseline_changes.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	down, err := os.ReadFile(filepath.Join(root, "146_deployment_plan_baseline_changes.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upText := string(up)
	downText := string(down)

	for _, table := range []string{
		"CREATE TABLE DeploymentPlanBaseline",
		"CREATE TABLE DeploymentPlanChangeEntry",
		"CREATE TABLE DeploymentPlanRiskEntry",
	} {
		g.Expect(upText).To(ContainSubstring(table))
	}
	g.Expect(strings.Count(upText, "organization_id UUID NOT NULL")).To(BeNumerically(">=", 3))
	g.Expect(strings.Count(upText, "actor_user_account_id UUID NOT NULL")).To(BeNumerically(">=", 3))
	g.Expect(upText).To(ContainSubstring("deployment_plan_change_evidence_append_only_guard"))
	g.Expect(upText).To(ContainSubstring("legacy_projection"))
	g.Expect(upText).To(ContainSubstring("authorizes_v2_execution"))
	g.Expect(downText).To(ContainSubstring("refusing migration 146 rollback"))
}

func TestDeploymentPlanChangeRepositoryUsesTenantScopeAndSerializableCAS(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("deployment_plan_changes.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)

	g.Expect(text).To(ContainSubstring("RunTxIso(ctx, pgx.Serializable"))
	g.Expect(text).To(ContainSubstring("FOR UPDATE"))
	g.Expect(text).To(ContainSubstring("expectedDesiredRevision"))
	g.Expect(text).To(ContainSubstring("expectedDesiredChecksum"))
	g.Expect(strings.Count(text, "organization_id = @organizationID")).To(BeNumerically(">=", 8))
	g.Expect(text).To(ContainSubstring("supersedes_deployment_plan_id"))
	g.Expect(text).To(ContainSubstring("successfulPlanID"))
}
