package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestDeleteApplicationHandlerReturnsConflictForProtectedReference(t *testing.T) {
	ctx := variableSetHandlerDBTestContext(t)
	orgID := createProtectedDeleteOrganization(t, ctx)
	application := createProtectedDeleteApplication(t, ctx, orgID)
	variableSetID, variableID := createProtectedDeleteStringVariable(t, ctx, orgID)
	execProtectedDeleteTestSQL(t, ctx, `
		INSERT INTO VariableScopedValue (
			organization_id, variable_set_id, variable_id, application_id, value
		) VALUES (
			@organizationID, @variableSetID, @variableID, @applicationID, '"protected"'::jsonb
		)`, pgx.NamedArgs{
		"organizationID": orgID,
		"variableSetID":  variableSetID,
		"variableID":     variableID,
		"applicationID":  application.ID,
	})
	requestContext, observedLogs, sentryTransport := protectedDeleteRequestContext(t, ctx, orgID)
	requestContext = internalctx.WithApplication(requestContext, &application)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/applications/"+application.ID.String(), nil)
	request = request.WithContext(requestContext)

	deleteApplication(recorder, request)

	assertProtectedDeleteConflict(
		t,
		ctx,
		recorder,
		"Application",
		application.ID,
		"application is in use\n",
		observedLogs,
		sentryTransport,
	)
}

func TestDeleteApplicationHandlerDoesNotExposeUnexpectedDatabaseErrors(t *testing.T) {
	ctx := variableSetHandlerDBTestContext(t)
	orgID := createProtectedDeleteOrganization(t, ctx)
	application := createProtectedDeleteApplication(t, ctx, orgID)
	execProtectedDeleteTestSQL(t, ctx, `DROP TABLE Application CASCADE`, nil)
	requestContext, _, _ := protectedDeleteRequestContext(t, ctx, orgID)
	requestContext = internalctx.WithApplication(requestContext, &application)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/applications/"+application.ID.String(), nil)
	request = request.WithContext(requestContext)

	deleteApplication(recorder, request)

	g := NewWithT(t)
	g.Expect(recorder.Code).To(Equal(http.StatusInternalServerError))
	g.Expect(recorder.Header().Get("Content-Type")).To(Equal("text/plain; charset=utf-8"))
	g.Expect(recorder.Body.String()).To(Equal("Internal Server Error\n"))
	assertNoProtectedDeleteDBDetails(t, recorder.Body.String())
}

func TestDeleteDeploymentTargetHandlerReturnsConflictForProtectedReference(t *testing.T) {
	ctx := variableSetHandlerDBTestContext(t)
	orgID := createProtectedDeleteOrganization(t, ctx)
	customerOrgID := createProtectedDeleteCustomerOrganization(t, ctx, orgID)
	environmentID := createProtectedDeleteEnvironment(t, ctx, orgID)
	target := createProtectedDeleteTarget(t, ctx, orgID, &customerOrgID)
	variableSetID, variableID := createProtectedDeleteStringVariable(t, ctx, orgID)
	execProtectedDeleteTestSQL(t, ctx, `
		INSERT INTO VariableScopedValue (
			organization_id,
			variable_set_id,
			variable_id,
			customer_organization_id,
			environment_id,
			deployment_target_id,
			value
		) VALUES (
			@organizationID,
			@variableSetID,
			@variableID,
			@customerOrganizationID,
			@environmentID,
			@deploymentTargetID,
			'"protected"'::jsonb
		)`, pgx.NamedArgs{
		"organizationID":         orgID,
		"variableSetID":          variableSetID,
		"variableID":             variableID,
		"customerOrganizationID": customerOrgID,
		"environmentID":          environmentID,
		"deploymentTargetID":     target.ID,
	})
	requestContext, observedLogs, sentryTransport := protectedDeleteRequestContext(t, ctx, orgID)
	requestContext = internalctx.WithDeploymentTarget(requestContext, &target)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/deployment-targets/"+target.ID.String(), nil)
	request = request.WithContext(requestContext)

	deleteDeploymentTarget(recorder, request)

	assertProtectedDeleteConflict(
		t,
		ctx,
		recorder,
		"DeploymentTarget",
		target.ID,
		"deployment target is in use\n",
		observedLogs,
		sentryTransport,
	)
}

func TestDeleteArtifactHandlerReturnsConflictForProtectedReference(t *testing.T) {
	ctx := variableSetHandlerDBTestContext(t)
	orgID := createProtectedDeleteOrganization(t, ctx)
	artifact := createProtectedDeleteArtifact(t, ctx, orgID)
	execProtectedDeleteTestSQL(t, ctx, `
		CREATE TABLE ProtectedDeleteArtifactGuard (
			artifact_id UUID PRIMARY KEY REFERENCES Artifact(id) ON DELETE RESTRICT
		)`, nil)
	execProtectedDeleteTestSQL(t, ctx, `
		INSERT INTO ProtectedDeleteArtifactGuard (artifact_id) VALUES (@artifactID)
	`, pgx.NamedArgs{"artifactID": artifact.ID})
	requestContext, observedLogs, sentryTransport := protectedDeleteRequestContext(t, ctx, orgID)
	requestContext = internalctx.WithArtifact(requestContext, &artifact)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/"+artifact.ID.String(), nil)
	request = request.WithContext(requestContext)

	deleteArtifactHandler(recorder, request)

	assertProtectedDeleteConflict(
		t,
		ctx,
		recorder,
		"Artifact",
		artifact.ID,
		"artifact is in use\n",
		observedLogs,
		sentryTransport,
	)
}

func TestDeleteArtifactHandlerPreservesEntitlementBadRequest(t *testing.T) {
	ctx := variableSetHandlerDBTestContext(t)
	orgID := createProtectedDeleteOrganization(t, ctx)
	artifact := createProtectedDeleteArtifact(t, ctx, orgID)
	var entitlementID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO ArtifactEntitlement (name, organization_id)
		VALUES (@name, @organizationID)
		RETURNING id`,
		pgx.NamedArgs{"name": "Entitlement " + uuid.NewString(), "organizationID": orgID},
	).Scan(&entitlementID); err != nil {
		t.Fatalf("create artifact entitlement: %v", err)
	}
	execProtectedDeleteTestSQL(t, ctx, `
		INSERT INTO ArtifactEntitlement_Artifact (artifact_entitlement_id, artifact_id)
		VALUES (@entitlementID, @artifactID)
	`, pgx.NamedArgs{"entitlementID": entitlementID, "artifactID": artifact.ID})
	requestContext, observedLogs, sentryTransport := protectedDeleteRequestContext(t, ctx, orgID)
	requestContext = internalctx.WithArtifact(requestContext, &artifact)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/"+artifact.ID.String(), nil)
	request = request.WithContext(requestContext)

	deleteArtifactHandler(recorder, request)

	g := NewWithT(t)
	g.Expect(recorder.Code).To(Equal(http.StatusBadRequest))
	g.Expect(recorder.Header().Get("Content-Type")).To(Equal("text/plain; charset=utf-8"))
	g.Expect(recorder.Body.String()).To(Equal(
		"bad request: Cannot delete artifact: it is referenced in one or more entitlements.\n"))
	assertProtectedDeleteParentExists(t, ctx, "Artifact", artifact.ID)
	g.Expect(observedLogs.Len()).To(Equal(0))
	g.Expect(sentryTransport.eventCount()).To(Equal(0))
}

func TestDeleteSecretHandlerReturnsConflictForProtectedReference(t *testing.T) {
	ctx := variableSetHandlerDBTestContext(t)
	orgID := createProtectedDeleteOrganization(t, ctx)
	secretID := createProtectedDeleteSecret(t, ctx, orgID, "db_reference")
	variableSetID := createProtectedDeleteVariableSet(t, ctx, orgID)
	execProtectedDeleteTestSQL(t, ctx, `
		INSERT INTO Variable (
			organization_id, variable_set_id, key, type, secret_reference_id, reference_name
		) VALUES (
			@organizationID, @variableSetID, @key, 'secret_reference', @secretID, @referenceName
		)`, pgx.NamedArgs{
		"organizationID": orgID,
		"variableSetID":  variableSetID,
		"key":            "secret_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		"secretID":       secretID,
		"referenceName":  "db_reference",
	})
	requestContext, observedLogs, sentryTransport := protectedDeleteRequestContext(t, ctx, orgID)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/secrets/"+secretID.String(), nil)
	request.SetPathValue("secretId", secretID.String())
	request = request.WithContext(requestContext)

	deleteSecretHandler().ServeHTTP(recorder, request)

	assertProtectedDeleteConflict(
		t,
		ctx,
		recorder,
		"Secret",
		secretID,
		"secret is in use\n",
		observedLogs,
		sentryTransport,
	)
}

func TestDeleteSecretHandlerPreservesAffectedDeploymentsConflict(t *testing.T) {
	ctx := variableSetHandlerDBTestContext(t)
	orgID := createProtectedDeleteOrganization(t, ctx)
	secretID := createProtectedDeleteSecret(t, ctx, orgID, "runtime_token")
	application := createProtectedDeleteApplication(t, ctx, orgID)
	applicationVersionID := createProtectedDeleteApplicationVersion(t, ctx, application.ID)
	target := createProtectedDeleteTarget(t, ctx, orgID, nil)
	releaseName := "protected-secret-release"
	dockerType := types.DockerTypeCompose
	request := api.DeploymentRequest{
		DeploymentTargetID:   target.ID,
		ApplicationVersionID: applicationVersionID,
		ReleaseName:          &releaseName,
		DockerType:           &dockerType,
		ValuesYaml:           []byte("apiToken: '{{ .Secrets.runtime_token }}'\n"),
		ValuesHash:           []byte("fixture-hash"),
	}
	if err := db.CreateDeployment(ctx, &request); err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	if _, err := db.CreateDeploymentRevision(ctx, &request); err != nil {
		t.Fatalf("create deployment revision: %v", err)
	}
	requestContext, observedLogs, sentryTransport := protectedDeleteRequestContext(t, ctx, orgID)

	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/secrets/"+secretID.String(), nil)
	httpRequest.SetPathValue("secretId", secretID.String())
	httpRequest = httpRequest.WithContext(requestContext)

	deleteSecretHandler().ServeHTTP(recorder, httpRequest)

	g := NewWithT(t)
	g.Expect(recorder.Code).To(Equal(http.StatusConflict))
	g.Expect(recorder.Header().Get("Content-Type")).To(Equal("application/json"))
	var response api.AffectedDeploymentsConflictResponse
	g.Expect(json.Unmarshal(recorder.Body.Bytes(), &response)).To(Succeed())
	g.Expect(response.AffectedDeployments).To(Equal([]api.AffectedDeployment{{
		DeploymentTargetID:   target.ID,
		DeploymentTargetName: target.Name,
		DeploymentID:         *request.DeploymentID,
		ApplicationName:      application.Name,
	}}))
	assertProtectedDeleteParentExists(t, ctx, "Secret", secretID)
	g.Expect(observedLogs.Len()).To(Equal(0))
	g.Expect(sentryTransport.eventCount()).To(Equal(0))
}

func protectedDeleteRequestContext(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
) (context.Context, *observer.ObservedLogs, *protectedDeleteSentryTransport) {
	t.Helper()
	logCore, observedLogs := observer.New(zap.DebugLevel)
	ctx = internalctx.WithLogger(ctx, zap.New(logCore))
	testAuth := testChannelAuth()
	testAuth.orgID = orgID
	ctx = auth.Authentication.NewContext(ctx, testAuth)

	transport := &protectedDeleteSentryTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "https://public@example.com/1",
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("create test Sentry client: %v", err)
	}
	ctx = sentry.SetHubOnContext(ctx, sentry.NewHub(client, sentry.NewScope()))
	return ctx, observedLogs, transport
}

type protectedDeleteSentryTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (t *protectedDeleteSentryTransport) Configure(sentry.ClientOptions) {}

func (t *protectedDeleteSentryTransport) SendEvent(event *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
}

func (t *protectedDeleteSentryTransport) Flush(time.Duration) bool { return true }

func (t *protectedDeleteSentryTransport) FlushWithContext(context.Context) bool { return true }

func (t *protectedDeleteSentryTransport) Close() {}

func (t *protectedDeleteSentryTransport) eventCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.events)
}

func assertProtectedDeleteConflict(
	t *testing.T,
	ctx context.Context,
	recorder *httptest.ResponseRecorder,
	table string,
	id uuid.UUID,
	expectedBody string,
	observedLogs *observer.ObservedLogs,
	sentryTransport *protectedDeleteSentryTransport,
) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(recorder.Code).To(Equal(http.StatusConflict))
	g.Expect(recorder.Header().Get("Content-Type")).To(Equal("text/plain; charset=utf-8"))
	g.Expect(recorder.Body.String()).To(Equal(expectedBody))
	assertNoProtectedDeleteDBDetails(t, recorder.Body.String())
	assertProtectedDeleteParentExists(t, ctx, table, id)
	g.Expect(observedLogs.Len()).To(Equal(0))
	g.Expect(sentryTransport.eventCount()).To(Equal(0))
}

func assertNoProtectedDeleteDBDetails(t *testing.T, body string) {
	t.Helper()
	g := NewWithT(t)
	lowerBody := strings.ToLower(body)
	for _, leaked := range []string{"sqlstate", "constraint", "table", "pgconn", "foreign key"} {
		g.Expect(lowerBody).NotTo(ContainSubstring(leaked))
	}
}

func assertProtectedDeleteParentExists(t *testing.T, ctx context.Context, table string, id uuid.UUID) {
	t.Helper()
	var query string
	switch table {
	case "Application":
		query = `SELECT count(*) FROM Application WHERE id = @id`
	case "DeploymentTarget":
		query = `SELECT count(*) FROM DeploymentTarget WHERE id = @id`
	case "Artifact":
		query = `SELECT count(*) FROM Artifact WHERE id = @id`
	case "Secret":
		query = `SELECT count(*) FROM Secret WHERE id = @id`
	default:
		t.Fatalf("unsupported parent table %q", table)
	}
	var count int
	if err := internalctx.GetDb(ctx).QueryRow(ctx, query, pgx.NamedArgs{"id": id}).Scan(&count); err != nil {
		t.Fatalf("query protected parent %s: %v", table, err)
	}
	NewWithT(t).Expect(count).To(Equal(1))
}

func createProtectedDeleteOrganization(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Organization (name) VALUES (@name) RETURNING id`,
		pgx.NamedArgs{"name": "Organization " + uuid.NewString()},
	).Scan(&id); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	return id
}

func createProtectedDeleteCustomerOrganization(t *testing.T, ctx context.Context, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO CustomerOrganization (organization_id, name)
		VALUES (@organizationID, @name)
		RETURNING id`,
		pgx.NamedArgs{"organizationID": orgID, "name": "Customer " + uuid.NewString()},
	).Scan(&id); err != nil {
		t.Fatalf("create customer organization: %v", err)
	}
	return id
}

func createProtectedDeleteEnvironment(t *testing.T, ctx context.Context, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Environment (organization_id, name)
		VALUES (@organizationID, @name)
		RETURNING id`,
		pgx.NamedArgs{"organizationID": orgID, "name": "Environment " + uuid.NewString()},
	).Scan(&id); err != nil {
		t.Fatalf("create environment: %v", err)
	}
	return id
}

func createProtectedDeleteApplication(t *testing.T, ctx context.Context, orgID uuid.UUID) types.Application {
	t.Helper()
	application := types.Application{
		Name: "Application " + uuid.NewString(),
		Type: types.DeploymentTypeDocker,
	}
	if err := db.CreateApplication(ctx, &application, orgID); err != nil {
		t.Fatalf("create application: %v", err)
	}
	return application
}

func createProtectedDeleteApplicationVersion(t *testing.T, ctx context.Context, applicationID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO ApplicationVersion (name, application_id)
		VALUES (@name, @applicationID)
		RETURNING id`,
		pgx.NamedArgs{"name": "1.0.0-" + uuid.NewString(), "applicationID": applicationID},
	).Scan(&id); err != nil {
		t.Fatalf("create application version: %v", err)
	}
	return id
}

func createProtectedDeleteTarget(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	customerOrgID *uuid.UUID,
) types.DeploymentTargetFull {
	t.Helper()
	var agentVersionID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT id FROM AgentVersion LIMIT 1`,
	).Scan(&agentVersionID); err != nil {
		t.Fatalf("get agent version: %v", err)
	}
	target := types.DeploymentTargetFull{DeploymentTarget: types.DeploymentTarget{
		Name:                   "Target " + uuid.NewString(),
		Type:                   types.DeploymentTypeDocker,
		Platform:               types.DeploymentTargetPlatformLinuxAMD64,
		OrganizationID:         orgID,
		CustomerOrganizationID: customerOrgID,
		AgentVersionID:         &agentVersionID,
	}}
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO DeploymentTarget (
			name, type, platform, organization_id, customer_organization_id, agent_version_id
		) VALUES (
			@name, @type, @platform, @organizationID, @customerOrganizationID, @agentVersionID
		)
		RETURNING id, created_at`,
		pgx.NamedArgs{
			"name":                   target.Name,
			"type":                   target.Type,
			"platform":               target.Platform,
			"organizationID":         target.OrganizationID,
			"customerOrganizationID": target.CustomerOrganizationID,
			"agentVersionID":         agentVersionID,
		},
	).Scan(&target.ID, &target.CreatedAt); err != nil {
		t.Fatalf("create deployment target: %v", err)
	}
	return target
}

func createProtectedDeleteArtifact(t *testing.T, ctx context.Context, orgID uuid.UUID) types.ArtifactWithTaggedVersion {
	t.Helper()
	artifact := types.ArtifactWithTaggedVersion{
		ArtifactWithDownloads: types.ArtifactWithDownloads{Artifact: types.Artifact{
			OrganizationID: orgID,
			Name:           "Artifact " + uuid.NewString(),
		}},
	}
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Artifact (organization_id, name)
		VALUES (@organizationID, @name)
		RETURNING id, created_at`,
		pgx.NamedArgs{"organizationID": orgID, "name": artifact.Name},
	).Scan(&artifact.ID, &artifact.CreatedAt); err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	return artifact
}

func createProtectedDeleteSecret(t *testing.T, ctx context.Context, orgID uuid.UUID, key string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Secret (organization_id, key, value)
		VALUES (@organizationID, @key, @value)
		RETURNING id`,
		pgx.NamedArgs{"organizationID": orgID, "key": key, "value": "protected-value"},
	).Scan(&id); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	return id
}

func createProtectedDeleteVariableSet(t *testing.T, ctx context.Context, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO VariableSet (organization_id, name)
		VALUES (@organizationID, @name)
		RETURNING id`,
		pgx.NamedArgs{"organizationID": orgID, "name": "Variables " + uuid.NewString()},
	).Scan(&id); err != nil {
		t.Fatalf("create variable set: %v", err)
	}
	return id
}

func createProtectedDeleteStringVariable(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	variableSetID := createProtectedDeleteVariableSet(t, ctx, orgID)
	var variableID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Variable (organization_id, variable_set_id, key, type)
		VALUES (@organizationID, @variableSetID, @key, 'string')
		RETURNING id`,
		pgx.NamedArgs{
			"organizationID": orgID,
			"variableSetID":  variableSetID,
			"key":            "variable_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		},
	).Scan(&variableID); err != nil {
		t.Fatalf("create variable: %v", err)
	}
	return variableSetID, variableID
}

func execProtectedDeleteTestSQL(t *testing.T, ctx context.Context, sql string, args pgx.NamedArgs) {
	t.Helper()
	if _, err := internalctx.GetDb(ctx).Exec(ctx, sql, args); err != nil {
		t.Fatalf("execute protected-delete fixture SQL: %v", err)
	}
}
