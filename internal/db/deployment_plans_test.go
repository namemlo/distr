package db_test

import (
	"context"
	"encoding/json"
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

func TestDeploymentPlanRepositoryCreatesReadyPlanFromPublishedRelease(t *testing.T) {
	ctx := deploymentPlanDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, deps.orgID, deps.applicationID, "Standard deploy")
	createDeploymentPlanVariableSet(t, ctx, deps.orgID, deps.applicationID)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	bundle.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, publishResult, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publishResult.Valid).To(BeTrue())
	g.Expect(published.ProcessSnapshotID).NotTo(BeNil())
	g.Expect(published.VariableSnapshotID).NotTo(BeNil())

	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusReady))
	g.Expect(plan.ApplicationID).To(Equal(deps.applicationID))
	g.Expect(plan.ChannelID).To(Equal(deps.channelID))
	g.Expect(plan.ReleaseBundleID).To(Equal(published.ID))
	g.Expect(plan.ProcessSnapshotID).To(Equal(published.ProcessSnapshotID))
	g.Expect(plan.VariableSnapshotID).To(Equal(published.VariableSnapshotID))
	g.Expect(plan.CanonicalChecksum).To(HavePrefix("sha256:"))
	g.Expect(plan.Targets).To(HaveLen(1))
	g.Expect(plan.Targets[0].DeploymentTargetID).To(Equal(targetID))
	g.Expect(plan.Steps).To(HaveLen(1))
	g.Expect(plan.Steps[0].StepKey).To(Equal("deploy"))
	g.Expect(plan.Steps[0].ActionType).To(Equal("distr.http.check"))
	g.Expect(plan.Steps[0].ActionName).To(Equal("HTTP check"))
	g.Expect(plan.Steps[0].Included).To(BeTrue())
	g.Expect(plan.Variables).To(HaveLen(2))
	g.Expect(deploymentPlanVariableByKey(plan.Variables, "api_url").Status).
		To(Equal(types.VariableResolutionStatusResolved))
	g.Expect(deploymentPlanVariableByKey(plan.Variables, "api_token").Redacted).To(BeTrue())
	g.Expect(deploymentPlanIssueCodes(plan.Issues, types.DeploymentPlanIssueSeverityBlocker)).To(BeEmpty())
	g.Expect(deploymentPlanIssueCodes(plan.Issues, types.DeploymentPlanIssueSeverityWarning)).
		To(ContainElement("dry_run_not_performed"))

	fetched, err := db.GetDeploymentPlan(ctx, plan.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.CanonicalChecksum).To(Equal(plan.CanonicalChecksum))
	g.Expect(fetched.Targets).To(HaveLen(1))
	g.Expect(fetched.Steps).To(HaveLen(1))
	g.Expect(fetched.Variables).To(HaveLen(2))
	g.Expect(fetched.Issues).To(HaveLen(len(plan.Issues)))

	listed, err := db.GetDeploymentPlansByOrganizationID(ctx, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(HaveLen(1))
	g.Expect(listed[0].ID).To(Equal(plan.ID))
}

func TestDeploymentPlanRepositoryPlansFromVariableSnapshotAfterVariableSetUpdate(t *testing.T) {
	ctx := deploymentPlanDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, deps.orgID, deps.applicationID, "Standard deploy")
	variableSet := createDeploymentPlanEditableVariableSet(t, ctx, deps.orgID, deps.applicationID)
	originalVariableID := deploymentPlanVariableSetVariableByKey(variableSet, "API_URL").ID
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	bundle.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, publishResult, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publishResult.Valid).To(BeTrue())

	variableSet.Variables = []types.Variable{
		{Key: "API_URL", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://new.example"`)},
		{Key: "DEBUG", Type: types.VariableTypeBoolean, DefaultValue: json.RawMessage(`true`)},
	}
	g.Expect(db.UpdateVariableSet(ctx, &variableSet)).To(Succeed())
	g.Expect(deploymentPlanVariableSetVariableByKey(variableSet, "API_URL").ID).NotTo(Equal(originalVariableID))

	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusReady))
	g.Expect(plan.Variables).To(HaveLen(1))
	apiURL := deploymentPlanVariableByKey(plan.Variables, "API_URL")
	g.Expect(apiURL.VariableID).To(Equal(originalVariableID))
	g.Expect(apiURL.Value).To(MatchJSON(`"https://old.example"`))
}

func TestDeploymentPlanRepositoryVariableSetUpdateAfterPlanDoesNotMutatePlanVariables(t *testing.T) {
	ctx := deploymentPlanDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, deps.orgID, deps.applicationID, "Standard deploy")
	variableSet := createDeploymentPlanEditableVariableSet(t, ctx, deps.orgID, deps.applicationID)
	originalVariableID := deploymentPlanVariableSetVariableByKey(variableSet, "API_URL").ID
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
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

	variableSet.Variables = []types.Variable{
		{Key: "API_URL", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://new.example"`)},
		{Key: "DEBUG", Type: types.VariableTypeBoolean, DefaultValue: json.RawMessage(`true`)},
	}
	g.Expect(db.UpdateVariableSet(ctx, &variableSet)).To(Succeed())

	fetched, err := db.GetDeploymentPlan(ctx, plan.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Variables).To(HaveLen(1))
	apiURL := deploymentPlanVariableByKey(fetched.Variables, "API_URL")
	g.Expect(apiURL.VariableID).To(Equal(originalVariableID))
	g.Expect(apiURL.Value).To(MatchJSON(`"https://old.example"`))
}

func TestDeploymentPlanRepositoryBlocksDraftReleaseMissingSnapshots(t *testing.T) {
	ctx := deploymentPlanDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())

	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: bundle.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusBlocked))
	g.Expect(deploymentPlanIssueCodes(plan.Issues, types.DeploymentPlanIssueSeverityBlocker)).
		To(ContainElements("release_not_published", "missing_process_snapshot", "missing_variable_snapshot"))
	g.Expect(plan.Steps).To(BeEmpty())
	g.Expect(plan.Variables).To(BeEmpty())
}

func TestDeploymentPlanRepositoryBlocksUnresolvedRequiredVariables(t *testing.T) {
	ctx := deploymentPlanDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, deps.orgID, deps.applicationID, "Standard deploy")
	createDeploymentPlanRequiredVariableSet(t, ctx, deps.orgID, deps.applicationID)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
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
	g.Expect(deploymentPlanVariableByKey(plan.Variables, "required_url").Status).
		To(Equal(types.VariableResolutionStatusUnresolved))
	g.Expect(deploymentPlanIssueCodes(plan.Issues, types.DeploymentPlanIssueSeverityBlocker)).
		To(ContainElement("required_variable_unresolved"))
}

func TestDeploymentPlanRepositoryBlocksInvalidStepCondition(t *testing.T) {
	ctx := deploymentPlanDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, deps.orgID, deps.applicationID, "Standard deploy")
	revision.Steps[0].Condition = `channel =~ "Stable"`
	g.Expect(db.CreateDeploymentProcessRevision(ctx, &revision)).To(Succeed())
	createDeploymentPlanVariableSet(t, ctx, deps.orgID, deps.applicationID)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
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
		To(ContainElement("invalid_step_condition"))
}

func TestDeploymentPlanRepositoryPreservesOrganizationIsolation(t *testing.T) {
	ctx := deploymentPlanDBTestContext(t)
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	otherDeps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, deps.orgID, deps.applicationID, "Standard deploy")
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
	otherTargetID := createReleaseBundleDockerTargetForOrganization(t, ctx, otherDeps.orgID, "cluster-b")
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	bundle.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, publishResult, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publishResult.Valid).To(BeTrue())

	_, err = db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{otherTargetID},
	})
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	_, err = db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   otherDeps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	_, err = db.GetDeploymentPlan(ctx, uuid.New(), deps.orgID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestDeploymentPlanMigrationDefinesPlanTables(t *testing.T) {
	g := NewWithT(t)
	sql, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "120_deployment_plans.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := string(sql)
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE DeploymentPlan"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE DeploymentPlanTarget"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE DeploymentPlanStep"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE DeploymentPlanVariable"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE DeploymentPlanIssue"))
	g.Expect(upSQL).To(ContainSubstring("FOREIGN KEY (release_bundle_id, application_id, channel_id, organization_id)"))
	g.Expect(upSQL).To(ContainSubstring("FOREIGN KEY (deployment_target_id, organization_id)"))
	g.Expect(upSQL).NotTo(ContainSubstring("REFERENCES Variable(id, variable_set_id, organization_id)"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "120_deployment_plans.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	downSQL := string(down)
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS DeploymentPlanIssue"))
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS DeploymentPlan"))
}

func deploymentPlanDBTestContext(t testing.TB) context.Context {
	t.Helper()
	return releaseBundleDBTestContext(t)
}

func createDeploymentPlanVariableSet(t *testing.T, ctx context.Context, orgID, applicationID uuid.UUID) {
	t.Helper()
	secretID := createReleaseBundleSecretForOrganization(t, ctx, orgID, "api_token", "secret-value")
	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Plan defaults " + uuid.NewString(),
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{
				Key:          "api_url",
				Type:         types.VariableTypeString,
				DefaultValue: json.RawMessage(`"https://example.test"`),
			},
			{
				Key:         "api_token",
				Type:        types.VariableTypeSecretReference,
				ReferenceID: secretID.String(),
			},
		},
	}
	if err := db.CreateVariableSet(ctx, &variableSet); err != nil {
		t.Fatalf("create variable set: %v", err)
	}
}

func createDeploymentPlanEditableVariableSet(
	t *testing.T,
	ctx context.Context,
	orgID, applicationID uuid.UUID,
) types.VariableSet {
	t.Helper()
	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Editable defaults " + uuid.NewString(),
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{Key: "API_URL", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://old.example"`)},
		},
	}
	if err := db.CreateVariableSet(ctx, &variableSet); err != nil {
		t.Fatalf("create editable variable set: %v", err)
	}
	return variableSet
}

func createDeploymentPlanRequiredVariableSet(t *testing.T, ctx context.Context, orgID, applicationID uuid.UUID) {
	t.Helper()
	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Required defaults " + uuid.NewString(),
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{
				Key:        "required_url",
				Type:       types.VariableTypeString,
				IsRequired: true,
			},
		},
	}
	if err := db.CreateVariableSet(ctx, &variableSet); err != nil {
		t.Fatalf("create required variable set: %v", err)
	}
}

func deploymentPlanVariableByKey(variables []types.DeploymentPlanVariable, key string) types.DeploymentPlanVariable {
	for _, variable := range variables {
		if variable.Key == key {
			return variable
		}
	}
	return types.DeploymentPlanVariable{}
}

func deploymentPlanVariableSetVariableByKey(variableSet types.VariableSet, key string) types.Variable {
	for _, variable := range variableSet.Variables {
		if variable.Key == key {
			return variable
		}
	}
	return types.Variable{}
}

func deploymentPlanIssueCodes(
	issues []types.DeploymentPlanIssue,
	severity types.DeploymentPlanIssueSeverity,
) []string {
	codes := []string{}
	for _, issue := range issues {
		if issue.Severity == severity {
			codes = append(codes, issue.Code)
		}
	}
	return codes
}
