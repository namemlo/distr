package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestPublishReleaseBundleCreatesVariableSnapshotWithoutSecretPlaintext(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	secretID := createReleaseBundleSecretForOrganization(t, ctx, orgID, "api_token", "super-secret-value")

	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Runtime Defaults",
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{
				Key:          "API_URL",
				Type:         types.VariableTypeString,
				DefaultValue: json.RawMessage(`"https://default.example"`),
			},
			{
				Key:         "API_TOKEN",
				Type:        types.VariableTypeSecretReference,
				ReferenceID: secretID.String(),
			},
		},
	}
	g.Expect(db.CreateVariableSet(ctx, &variableSet)).To(Succeed())

	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	actorID := createReleaseBundleTestUser(t, ctx, orgID)

	published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, orgID, actorID)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Valid).To(BeTrue())
	g.Expect(published.VariableSnapshotID).NotTo(BeNil())
	g.Expect(published.CanonicalPayload).To(ContainSubstring(`variableSnapshotId`))

	snapshot, err := db.GetVariableSnapshot(ctx, *published.VariableSnapshotID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(snapshot.ReleaseBundleID).To(Equal(bundle.ID))
	g.Expect(snapshot.ApplicationID).To(Equal(applicationID))
	g.Expect(snapshot.ChannelID).To(Equal(channelID))
	g.Expect(snapshot.Values).To(HaveLen(2))
	g.Expect(snapshot.CanonicalChecksum).To(HavePrefix("sha256:"))
	g.Expect(string(snapshot.CanonicalPayload)).NotTo(ContainSubstring("super-secret-value"))

	apiURL := variableSnapshotValueByKey(snapshot.Values, "API_URL")
	g.Expect(apiURL.Value).To(MatchJSON(`"https://default.example"`))
	g.Expect(apiURL.Redacted).To(BeFalse())

	apiToken := variableSnapshotValueByKey(snapshot.Values, "API_TOKEN")
	g.Expect(apiToken.Value).To(BeNil())
	g.Expect(apiToken.ReferenceID).To(Equal(secretID.String()))
	g.Expect(apiToken.ReferenceName).To(Equal("api_token"))
	g.Expect(apiToken.Redacted).To(BeTrue())
}

func TestVariableSnapshotRepositoryIsOrganizationScoped(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	otherOrgID := createReleaseBundleTestOrganization(t, ctx)
	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Runtime Defaults",
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{Key: "API_URL", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://default.example"`)},
		},
	}
	g.Expect(db.CreateVariableSet(ctx, &variableSet)).To(Succeed())
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, _, err := db.PublishReleaseBundle(ctx, bundle.ID, orgID, createReleaseBundleTestUser(t, ctx, orgID))
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.GetVariableSnapshot(ctx, *published.VariableSnapshotID, otherOrgID)

	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestPublishedVariableSnapshotDoesNotBlockVariableSetUpdates(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleDependencies(t, ctx)
	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Runtime Defaults",
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{Key: "API_URL", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://old.example"`)},
		},
	}
	g.Expect(db.CreateVariableSet(ctx, &variableSet)).To(Succeed())
	originalVariableID := variableSet.Variables[0].ID
	bundle := releaseBundleFixture(orgID, applicationID, channelID, versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, _, err := db.PublishReleaseBundle(ctx, bundle.ID, orgID, createReleaseBundleTestUser(t, ctx, orgID))
	g.Expect(err).NotTo(HaveOccurred())

	variableSet.Variables = []types.Variable{
		{Key: "API_URL", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://new.example"`)},
		{Key: "DEBUG", Type: types.VariableTypeBoolean, DefaultValue: json.RawMessage(`true`)},
	}
	g.Expect(db.UpdateVariableSet(ctx, &variableSet)).To(Succeed())
	g.Expect(variableSet.Variables).To(HaveLen(2))
	g.Expect(variableByKeyForSnapshotTest(variableSet, "API_URL").ID).NotTo(Equal(originalVariableID))

	snapshot, err := db.GetVariableSnapshot(ctx, *published.VariableSnapshotID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(snapshot.Values).To(HaveLen(1))
	apiURL := variableSnapshotValueByKey(snapshot.Values, "API_URL")
	g.Expect(apiURL.VariableID).To(Equal(originalVariableID))
	g.Expect(apiURL.Value).To(MatchJSON(`"https://old.example"`))
}

func TestGetDeploymentConfigurationDriftComparesLatestDeploymentAgainstCurrentVariableSchema(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, _, versionID := createReleaseBundleDependencies(t, ctx)
	secretID := createReleaseBundleSecretForOrganization(t, ctx, orgID, "api_token", "super-secret-value")
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, orgID, "production")

	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Runtime Defaults",
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{Key: "API_URL", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://new.example"`)},
			{Key: "REPLICAS", Type: types.VariableTypeNumber, IsRequired: true},
			{Key: "DEBUG", Type: types.VariableTypeBoolean, DefaultValue: json.RawMessage(`true`)},
			{Key: "API_TOKEN", Type: types.VariableTypeSecretReference, ReferenceID: secretID.String()},
		},
	}
	g.Expect(db.CreateVariableSet(ctx, &variableSet)).To(Succeed())
	request := api.DeploymentRequest{
		DeploymentTargetID:   targetID,
		ApplicationVersionID: versionID,
		EnvFileData:          []byte("API_URL=https://old.example\nAPI_TOKEN=runtime-secret\nOLD_SETTING=legacy\n"),
	}
	g.Expect(db.CreateDeployment(ctx, &request)).To(Succeed())
	_, err := db.CreateDeploymentRevision(ctx, &request)
	g.Expect(err).NotTo(HaveOccurred())

	drift, err := db.GetDeploymentConfigurationDrift(ctx, *request.DeploymentID, orgID)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(drift.DeploymentID).To(Equal(*request.DeploymentID))
	g.Expect(drift.ApplicationID).To(Equal(applicationID))
	g.Expect(drift.HasDrift).To(BeTrue())
	g.Expect(drift.NewRequiredVariables).To(ConsistOf(types.ConfigurationDriftVariable{
		Key:        "REPLICAS",
		Type:       types.VariableTypeNumber,
		IsRequired: true,
		Source:     types.VariableResolutionSourceUnresolved,
	}))
	g.Expect(drift.MissingVariables).To(ConsistOf(types.ConfigurationDriftVariable{
		Key:    "DEBUG",
		Type:   types.VariableTypeBoolean,
		Source: types.VariableResolutionSourceDefault,
		Value:  json.RawMessage(`true`),
	}))
	g.Expect(drift.RemovedVariables).To(ConsistOf(types.ConfigurationDriftRemovedValue{Key: "OLD_SETTING"}))
	g.Expect(drift.DefaultChanges).To(ConsistOf(types.ConfigurationDriftDefaultChange{
		Key:           "API_URL",
		Type:          types.VariableTypeString,
		CurrentValue:  json.RawMessage(`"https://new.example"`),
		DeployedValue: json.RawMessage(`"https://old.example"`),
	}))
	g.Expect(drift.SecretReferenceChanges).To(ConsistOf(types.ConfigurationDriftReferenceChange{
		Key:           "API_TOKEN",
		Type:          types.VariableTypeSecretReference,
		ReferenceID:   secretID.String(),
		ReferenceName: "api_token",
		Redacted:      true,
	}))
}

func TestGetDeploymentConfigurationDriftReturnsNotFoundForCrossOrganizationDeployment(t *testing.T) {
	ctx := releaseBundleDBTestContext(t)
	g := NewWithT(t)
	orgID, _, _, versionID := createReleaseBundleDependencies(t, ctx)
	otherOrgID := createReleaseBundleTestOrganization(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, orgID, "production")
	request := api.DeploymentRequest{
		DeploymentTargetID:   targetID,
		ApplicationVersionID: versionID,
		EnvFileData:          []byte("API_URL=https://old.example\n"),
	}
	g.Expect(db.CreateDeployment(ctx, &request)).To(Succeed())
	_, err := db.CreateDeploymentRevision(ctx, &request)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.GetDeploymentConfigurationDrift(ctx, *request.DeploymentID, otherOrgID)

	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestVariableSnapshotMigrationsDefineReversibleSchema(t *testing.T) {
	g := NewWithT(t)
	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "119_variable_snapshots.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := string(up)
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE VariableSnapshot"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE VariableSnapshotValue"))
	g.Expect(upSQL).To(ContainSubstring("variable_snapshot_id UUID"))
	g.Expect(upSQL).To(ContainSubstring("variablesnapshotvalue_secret_redaction_check"))
	g.Expect(upSQL).NotTo(ContainSubstring("variablesnapshotvalue_variable_fk"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "119_variable_snapshots.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	downSQL := string(down)
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS VariableSnapshotValue"))
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS VariableSnapshot"))
	g.Expect(downSQL).To(ContainSubstring("DROP COLUMN IF EXISTS variable_snapshot_id"))
}

func createReleaseBundleSecretForOrganization(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	key string,
	value string,
) uuid.UUID {
	t.Helper()
	var secretID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Secret (organization_id, key, value)
		VALUES (@organizationId, @key, @value)
		RETURNING id`,
		pgx.NamedArgs{"organizationId": orgID, "key": key, "value": value},
	).Scan(&secretID); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	return secretID
}

func createReleaseBundleDockerTargetForOrganization(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	name string,
) uuid.UUID {
	t.Helper()
	createdByID := createReleaseBundleTestUser(t, ctx, orgID)
	agentVersionID := createReleaseBundleAgentVersion(t, ctx)
	target := types.DeploymentTargetFull{
		DeploymentTarget: types.DeploymentTarget{
			Name:           name + " " + uuid.NewString(),
			Type:           types.DeploymentTypeDocker,
			OrganizationID: orgID,
			AgentVersionID: &agentVersionID,
		},
	}
	if err := db.CreateDeploymentTarget(ctx, &target, orgID, createdByID, nil); err != nil {
		t.Fatalf("create deployment target: %v", err)
	}
	return target.ID
}

func createReleaseBundleAgentVersion(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	var agentVersionID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO AgentVersion (name, manifest_file_revision, compose_file_revision)
		VALUES (@name, @manifestFileRevision, @composeFileRevision)
		RETURNING id`,
		pgx.NamedArgs{
			"name":                 "variable-snapshot-agent-" + uuid.NewString(),
			"manifestFileRevision": "v1",
			"composeFileRevision":  "v1",
		},
	).Scan(&agentVersionID); err != nil {
		t.Fatalf("create agent version: %v", err)
	}
	return agentVersionID
}

func variableSnapshotValueByKey(values []types.VariableSnapshotValue, key string) types.VariableSnapshotValue {
	for _, value := range values {
		if value.Key == key {
			return value
		}
	}
	return types.VariableSnapshotValue{}
}

func variableByKeyForSnapshotTest(variableSet types.VariableSet, key string) types.Variable {
	for _, variable := range variableSet.Variables {
		if variable.Key == key {
			return variable
		}
	}
	return types.Variable{}
}
