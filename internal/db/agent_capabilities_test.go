package db_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestAgentCapabilityRepositoryUpsertsAndReplacesReport(t *testing.T) {
	ctx := agentCapabilityDBTestContext(t)
	g := NewWithT(t)
	orgID := createReleaseBundleTestOrganization(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, orgID, "cluster-a")
	report := agentCapabilityReportFixture(orgID, targetID)

	saved, err := db.UpsertAgentCapabilityReport(ctx, report)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(saved.ID).NotTo(Equal(uuid.Nil))
	g.Expect(saved.DeploymentTargetID).To(Equal(targetID))
	g.Expect(saved.ProtocolVersion).To(Equal(types.AgentCapabilityProtocolV1))
	g.Expect(saved.AgentVersion).To(Equal("1.2.3"))
	g.Expect(saved.SupportedRuntimes).To(Equal([]string{"docker"}))
	g.Expect(agentActionCapabilityPairs(saved.SupportedActions)).To(Equal([]string{"distr.http.check:1"}))

	report.AgentVersion = "1.2.4"
	report.SupportedRuntimes = []string{"docker", "kubernetes"}
	report.SupportedActions = []types.AgentActionCapability{
		{ActionType: "distr.preflight", Versions: []string{types.AgentActionVersionV1}},
	}
	updated, err := db.UpsertAgentCapabilityReport(ctx, report)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(updated.ID).To(Equal(saved.ID))
	g.Expect(updated.AgentVersion).To(Equal("1.2.4"))
	g.Expect(updated.SupportedRuntimes).To(Equal([]string{"docker", "kubernetes"}))
	g.Expect(agentActionCapabilityPairs(updated.SupportedActions)).To(Equal([]string{"distr.preflight:1"}))

	fetched, err := db.GetAgentCapabilityReportForDeploymentTarget(ctx, targetID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.ID).To(Equal(saved.ID))
	g.Expect(agentActionCapabilityPairs(fetched.SupportedActions)).To(Equal([]string{"distr.preflight:1"}))
}

func TestAgentCapabilityRepositoryPreservesOrganizationIsolation(t *testing.T) {
	ctx := agentCapabilityDBTestContext(t)
	g := NewWithT(t)
	orgID := createReleaseBundleTestOrganization(t, ctx)
	otherOrgID := createReleaseBundleTestOrganization(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, orgID, "cluster-a")

	_, err := db.UpsertAgentCapabilityReport(ctx, agentCapabilityReportFixture(otherOrgID, targetID))
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	_, err = db.GetAgentCapabilityReportForDeploymentTarget(ctx, targetID, otherOrgID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestDeploymentPlanRepositoryBlocksIncludedStepUnsupportedByReportedAgentCapabilities(t *testing.T) {
	ctx := agentCapabilityDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, deps.orgID, deps.applicationID, "Capability deploy")
	createDeploymentPlanVariableSet(t, ctx, deps.orgID, deps.applicationID)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
	report := agentCapabilityReportFixture(deps.orgID, targetID)
	report.SupportedActions = []types.AgentActionCapability{
		{ActionType: "distr.preflight", Versions: []string{types.AgentActionVersionV1}},
	}
	_, err := db.UpsertAgentCapabilityReport(ctx, report)
	g.Expect(err).NotTo(HaveOccurred())
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	bundle.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, publishResult, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publishResult.Valid).To(BeTrue())

	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusBlocked))
	g.Expect(deploymentPlanIssueCodes(plan.Issues, types.DeploymentPlanIssueSeverityBlocker)).
		To(ContainElement("agent_action_unsupported"))
}

func TestDeploymentPlanRepositoryBlocksIncludedStepWhenReportedAgentHasNoActionSupport(t *testing.T) {
	ctx := agentCapabilityDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, deps.orgID, deps.applicationID, "Capability deploy")
	createDeploymentPlanVariableSet(t, ctx, deps.orgID, deps.applicationID)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
	report := agentCapabilityReportFixture(deps.orgID, targetID)
	report.SupportedActions = nil
	_, err := db.UpsertAgentCapabilityReport(ctx, report)
	g.Expect(err).NotTo(HaveOccurred())
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	bundle.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, publishResult, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publishResult.Valid).To(BeTrue())

	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusBlocked))
	g.Expect(deploymentPlanIssueCodes(plan.Issues, types.DeploymentPlanIssueSeverityBlocker)).
		To(ContainElement("agent_action_unsupported"))
}

func TestDeploymentPlanRepositoryAllowsIncludedStepSupportedByReportedAgentCapabilities(t *testing.T) {
	ctx := agentCapabilityDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, deps.orgID, deps.applicationID, "Capability deploy")
	createDeploymentPlanVariableSet(t, ctx, deps.orgID, deps.applicationID)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
	_, err := db.UpsertAgentCapabilityReport(ctx, agentCapabilityReportFixture(deps.orgID, targetID))
	g.Expect(err).NotTo(HaveOccurred())
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	bundle.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, publishResult, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publishResult.Valid).To(BeTrue())

	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusReady))
	g.Expect(deploymentPlanIssueCodes(plan.Issues, types.DeploymentPlanIssueSeverityBlocker)).To(BeEmpty())
}

func TestAgentCapabilityMigrationDefinesReportTables(t *testing.T) {
	g := NewWithT(t)
	sql, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "123_agent_capabilities.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := string(sql)
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE AgentCapabilityReport"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE AgentActionCapability"))
	g.Expect(upSQL).To(ContainSubstring("UNIQUE (deployment_target_id)"))
	g.Expect(upSQL).To(ContainSubstring("FOREIGN KEY (deployment_target_id, organization_id)"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "123_agent_capabilities.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	downSQL := string(down)
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS AgentActionCapability"))
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS AgentCapabilityReport"))
}

func agentCapabilityDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return deploymentPlanDBTestContext(t)
}

func agentCapabilityReportFixture(orgID, targetID uuid.UUID) types.AgentCapabilityReport {
	return types.AgentCapabilityReport{
		OrganizationID:        orgID,
		DeploymentTargetID:    targetID,
		ProtocolVersion:       types.AgentCapabilityProtocolV1,
		AgentVersion:          "1.2.3",
		SupportedRuntimes:     []string{"docker"},
		OperatingSystem:       "linux",
		Architecture:          "amd64",
		AvailableTooling:      []string{"docker"},
		StrategyCapabilities:  []string{"rolling"},
		CompatibilityWarnings: []string{},
		SupportedActions: []types.AgentActionCapability{
			{ActionType: "distr.http.check", Versions: []string{types.AgentActionVersionV1}},
		},
	}
}

func agentActionCapabilityPairs(actions []types.AgentActionCapability) []string {
	pairs := make([]string, 0, len(actions))
	for _, action := range actions {
		for _, version := range action.Versions {
			pairs = append(pairs, action.ActionType+":"+version)
		}
	}
	return pairs
}
