package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestVariableSnapshotHandlersPublishAndReadSnapshot(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, channelID, versionID := createReleaseBundleHandlerDependencies(t, ctx)
	secretID := createVariableSnapshotHandlerSecret(t, ctx, orgID, "api_token", "super-secret-value")
	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Runtime Defaults",
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{Key: "API_URL", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://default.example"`)},
			{Key: "API_TOKEN", Type: types.VariableTypeSecretReference, ReferenceID: secretID.String()},
		},
	}
	g.Expect(db.CreateVariableSet(ctx, &variableSet)).To(Succeed())

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/release-bundles",
		strings.NewReader(releaseBundleRequestBody(applicationID, channelID, versionID, "2026.06.20", "1.2.3")),
	)
	createRequest = createRequest.WithContext(authenticatedChannelHandlerContext(ctx, orgID))
	createReleaseBundleHandler().ServeHTTP(createRecorder, createRequest)
	g.Expect(createRecorder.Code).To(Equal(http.StatusOK))
	var created api.ReleaseBundle
	g.Expect(json.Unmarshal(createRecorder.Body.Bytes(), &created)).To(Succeed())

	publishRecorder := httptest.NewRecorder()
	publishRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/release-bundles/"+created.ID.String()+"/publish",
		nil,
	)
	publishRequest.SetPathValue("releaseBundleId", created.ID.String())
	publishRequest = publishRequest.WithContext(authenticatedReleaseBundleHandlerContext(
		ctx,
		orgID,
		createReleaseBundleHandlerUser(t, ctx, orgID),
	))
	publishReleaseBundleHandler().ServeHTTP(publishRecorder, publishRequest)
	g.Expect(publishRecorder.Code).To(Equal(http.StatusOK))
	var published api.ReleaseBundle
	g.Expect(json.Unmarshal(publishRecorder.Body.Bytes(), &published)).To(Succeed())
	g.Expect(published.VariableSnapshotID).NotTo(BeNil())

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/variable-snapshots/"+published.VariableSnapshotID.String(),
		nil,
	)
	getRequest.SetPathValue("variableSnapshotId", published.VariableSnapshotID.String())
	getRequest = getRequest.WithContext(authenticatedReleaseBundleHandlerContext(
		ctx,
		orgID,
		createReleaseBundleHandlerUser(t, ctx, orgID),
	))
	getVariableSnapshotHandler().ServeHTTP(getRecorder, getRequest)

	g.Expect(getRecorder.Code).To(Equal(http.StatusOK))
	var snapshot api.VariableSnapshot
	g.Expect(json.Unmarshal(getRecorder.Body.Bytes(), &snapshot)).To(Succeed())
	g.Expect(snapshot.ReleaseBundleID).To(Equal(created.ID))
	g.Expect(snapshot.Values).To(HaveLen(2))
	g.Expect(getRecorder.Body.String()).NotTo(ContainSubstring("super-secret-value"))
	g.Expect(variableSnapshotAPIValueByKey(snapshot.Values, "API_TOKEN").Redacted).To(BeTrue())
}

func TestDeploymentConfigurationDriftHandlerReturnsDrift(t *testing.T) {
	ctx := channelHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, _, versionID := createReleaseBundleHandlerDependencies(t, ctx)
	secretID := createVariableSnapshotHandlerSecret(t, ctx, orgID, "api_token", "super-secret-value")
	targetID := createVariableSnapshotHandlerDockerTarget(t, ctx, orgID, "production")
	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Runtime Defaults",
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{Key: "API_URL", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://new.example"`)},
			{Key: "REPLICAS", Type: types.VariableTypeNumber, IsRequired: true},
			{Key: "API_TOKEN", Type: types.VariableTypeSecretReference, ReferenceID: secretID.String()},
		},
	}
	g.Expect(db.CreateVariableSet(ctx, &variableSet)).To(Succeed())
	deploymentRequest := api.DeploymentRequest{
		DeploymentTargetID:   targetID,
		ApplicationVersionID: versionID,
		EnvFileData:          []byte("API_URL=https://old.example\nAPI_TOKEN=runtime-secret\nOLD_SETTING=legacy\n"),
	}
	g.Expect(db.CreateDeployment(ctx, &deploymentRequest)).To(Succeed())
	_, err := db.CreateDeploymentRevision(ctx, &deploymentRequest)
	g.Expect(err).NotTo(HaveOccurred())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/deployments/"+deploymentRequest.DeploymentID.String()+"/configuration-drift",
		nil,
	)
	request.SetPathValue("deploymentId", deploymentRequest.DeploymentID.String())
	request = request.WithContext(authenticatedReleaseBundleHandlerContext(
		ctx,
		orgID,
		createReleaseBundleHandlerUser(t, ctx, orgID),
	))
	getDeploymentConfigurationDriftHandler().ServeHTTP(recorder, request)

	g.Expect(recorder.Code).To(Equal(http.StatusOK))
	var drift api.ConfigurationDrift
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &drift)).To(Succeed())
	g.Expect(drift.DeploymentID).To(Equal(*deploymentRequest.DeploymentID))
	g.Expect(drift.HasDrift).To(BeTrue())
	g.Expect(drift.NewRequiredVariables).To(HaveLen(1))
	g.Expect(drift.DefaultChanges).To(HaveLen(1))
	g.Expect(drift.RemovedVariables).To(ConsistOf(api.ConfigurationDriftRemovedValue{Key: "OLD_SETTING"}))
	g.Expect(drift.SecretReferenceChanges).To(HaveLen(1))
	g.Expect(recorder.Body.String()).NotTo(ContainSubstring("runtime-secret"))
	g.Expect(recorder.Body.String()).NotTo(ContainSubstring("super-secret-value"))
}

func createVariableSnapshotHandlerSecret(
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

func createVariableSnapshotHandlerDockerTarget(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	name string,
) uuid.UUID {
	t.Helper()
	agentVersionID := createVariableSnapshotHandlerAgentVersion(t, ctx)
	target := types.DeploymentTargetFull{
		DeploymentTarget: types.DeploymentTarget{
			Name:           name + " " + uuid.NewString(),
			Type:           types.DeploymentTypeDocker,
			OrganizationID: orgID,
			AgentVersionID: &agentVersionID,
		},
	}
	if err := db.CreateDeploymentTarget(
		ctx,
		&target,
		orgID,
		createReleaseBundleHandlerUser(t, ctx, orgID),
		nil,
	); err != nil {
		t.Fatalf("create deployment target: %v", err)
	}
	return target.ID
}

func createVariableSnapshotHandlerAgentVersion(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	var agentVersionID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO AgentVersion (name, manifest_file_revision, compose_file_revision)
		VALUES (@name, @manifestFileRevision, @composeFileRevision)
		RETURNING id`,
		pgx.NamedArgs{
			"name":                 "variable-snapshot-handler-agent-" + uuid.NewString(),
			"manifestFileRevision": "v1",
			"composeFileRevision":  "v1",
		},
	).Scan(&agentVersionID); err != nil {
		t.Fatalf("create agent version: %v", err)
	}
	return agentVersionID
}

func variableSnapshotAPIValueByKey(values []api.VariableSnapshotValue, key string) api.VariableSnapshotValue {
	for _, value := range values {
		if value.Key == key {
			return value
		}
	}
	return api.VariableSnapshotValue{}
}
