package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/auth"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestVariableSetHandlersCRUDAndOrganizationIsolation(t *testing.T) {
	ctx := variableSetHandlerDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, secretID := createVariableSetHandlerDependencies(t, ctx)
	otherOrgID, _, _ := createVariableSetHandlerDependencies(t, ctx)

	body := `{
		"name":" Shared Defaults ",
		"description":"Reusable defaults",
		"sortOrder":10,
		"applicationIds":["` + applicationID.String() + `"],
		"variables":[
			{
				"key":"api_url",
				"type":"string",
				"defaultValue":"https://example.test",
				"scopedValues":[
					{
						"scope":{"applicationId":"` + applicationID.String() + `"},
						"value":"https://application.example"
					}
				]
			},
			{"key":"api_token","type":"secret_reference","referenceId":"` + secretID.String() + `"}
		]
	}`
	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/variable-sets", strings.NewReader(body))
	createRequest = createRequest.WithContext(authenticatedVariableSetHandlerContext(ctx, orgID))

	createVariableSetHandler().ServeHTTP(createRecorder, createRequest)

	g.Expect(createRecorder.Code).To(Equal(http.StatusOK))
	var created api.VariableSet
	g.Expect(json.Unmarshal(createRecorder.Body.Bytes(), &created)).To(Succeed())
	g.Expect(created.Name).To(Equal("Shared Defaults"))
	g.Expect(created.ApplicationIDs).To(Equal([]uuid.UUID{applicationID}))
	g.Expect(created.Variables).To(HaveLen(2))
	g.Expect(apiVariableByKey(created, "api_url").ScopedValues).To(HaveLen(1))
	secretVariable := apiVariableByKey(created, "api_token")
	g.Expect(secretVariable.ReferenceName).To(Equal("api_token"))
	g.Expect(secretVariable.DefaultValue).To(BeNil())

	previewRecorder := httptest.NewRecorder()
	previewBody := `{
		"variableSetIds":["` + created.ID.String() + `"],
		"scope":{"applicationId":"` + applicationID.String() + `"}
	}`
	previewRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/variables/resolve-preview",
		strings.NewReader(previewBody),
	)
	previewRequest = previewRequest.WithContext(authenticatedVariableSetHandlerContext(ctx, orgID))
	resolveVariablesPreviewHandler().ServeHTTP(previewRecorder, previewRequest)
	g.Expect(previewRecorder.Code).To(Equal(http.StatusOK))
	var preview []api.ResolvedVariable
	g.Expect(json.Unmarshal(previewRecorder.Body.Bytes(), &preview)).To(Succeed())
	resolvedURL := resolvedAPIVariableByKey(preview, "api_url")
	g.Expect(resolvedURL.Source).To(Equal("application"))
	g.Expect(resolvedURL.Value).To(MatchJSON(`"https://application.example"`))

	crossOrgPreviewRecorder := httptest.NewRecorder()
	crossOrgPreviewRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/variables/resolve-preview",
		strings.NewReader(previewBody),
	)
	crossOrgPreviewRequest = crossOrgPreviewRequest.WithContext(authenticatedVariableSetHandlerContext(ctx, otherOrgID))
	resolveVariablesPreviewHandler().ServeHTTP(crossOrgPreviewRecorder, crossOrgPreviewRequest)
	g.Expect(crossOrgPreviewRecorder.Code).To(Equal(http.StatusNotFound))

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/variable-sets", nil)
	listRequest = listRequest.WithContext(authenticatedVariableSetHandlerContext(ctx, orgID))
	getVariableSetsHandler().ServeHTTP(listRecorder, listRequest)
	g.Expect(listRecorder.Code).To(Equal(http.StatusOK))
	var listed []api.VariableSet
	g.Expect(json.Unmarshal(listRecorder.Body.Bytes(), &listed)).To(Succeed())
	g.Expect(listed).To(HaveLen(1))

	crossOrgRecorder := httptest.NewRecorder()
	crossOrgRequest := httptest.NewRequest(http.MethodGet, "/api/v1/variable-sets/"+created.ID.String(), nil)
	crossOrgRequest.SetPathValue("variableSetId", created.ID.String())
	crossOrgRequest = crossOrgRequest.WithContext(authenticatedVariableSetHandlerContext(ctx, otherOrgID))
	getVariableSetHandler().ServeHTTP(crossOrgRecorder, crossOrgRequest)
	g.Expect(crossOrgRecorder.Code).To(Equal(http.StatusNotFound))

	updateRecorder := httptest.NewRecorder()
	updateBody := `{"name":"Runtime Defaults","variables":[{"key":"required_url","type":"string","isRequired":true}]}`
	updateRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/variable-sets/"+created.ID.String(),
		strings.NewReader(updateBody),
	)
	updateRequest.SetPathValue("variableSetId", created.ID.String())
	updateRequest = updateRequest.WithContext(authenticatedVariableSetHandlerContext(ctx, orgID))
	updateVariableSetHandler().ServeHTTP(updateRecorder, updateRequest)
	g.Expect(updateRecorder.Code).To(Equal(http.StatusOK))
	var updated api.VariableSet
	g.Expect(json.Unmarshal(updateRecorder.Body.Bytes(), &updated)).To(Succeed())
	g.Expect(updated.Name).To(Equal("Runtime Defaults"))
	g.Expect(updated.ApplicationIDs).To(BeEmpty())
	g.Expect(updated.Variables).To(HaveLen(1))
	g.Expect(updated.Variables[0].IsRequired).To(BeTrue())

	deleteRecorder := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/variable-sets/"+created.ID.String(), nil)
	deleteRequest.SetPathValue("variableSetId", created.ID.String())
	deleteRequest = deleteRequest.WithContext(authenticatedVariableSetHandlerContext(ctx, orgID))
	deleteVariableSetHandler().ServeHTTP(deleteRecorder, deleteRequest)
	g.Expect(deleteRecorder.Code).To(Equal(http.StatusNoContent))
}

func authenticatedVariableSetHandlerContext(ctx context.Context, orgID uuid.UUID) context.Context {
	ctx = internalctx.WithLogger(ctx, zap.NewNop())
	variableSetAuth := testVariableSetAuth()
	variableSetAuth.orgID = orgID
	return auth.Authentication.NewContext(ctx, variableSetAuth)
}

//nolint:dupl
func variableSetHandlerDBTestContext(t *testing.T) context.Context {
	t.Helper()
	databaseURL := os.Getenv("DISTR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DISTR_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "variable_set_handler_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE"); err != nil {
			t.Logf("drop test schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database url: %v", err)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+quotedSchema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect to isolated test schema: %v", err)
	}
	t.Cleanup(pool.Close)

	runVariableSetHandlerTestMigrations(t, ctx, pool)
	return internalctx.WithDb(ctx, pool)
}

func runVariableSetHandlerTestMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "migrations", "sql", "*.up.sql"))
	if err != nil {
		t.Fatalf("list migration files: %v", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return variableSetHandlerMigrationVersion(t, files[i]) < variableSetHandlerMigrationVersion(t, files[j])
	})
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			t.Fatalf("run migration %s: %v", file, err)
		}
	}
}

func variableSetHandlerMigrationVersion(t *testing.T, file string) int {
	t.Helper()
	base := filepath.Base(file)
	version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
	if err != nil {
		t.Fatalf("parse migration version %s: %v", file, err)
	}
	return version
}

func createVariableSetHandlerDependencies(
	t *testing.T,
	ctx context.Context,
) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	var orgID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Organization (name) VALUES (@name) RETURNING id`,
		pgx.NamedArgs{"name": "Organization " + uuid.NewString()},
	).Scan(&orgID); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	application := types.Application{
		Name: "Application " + uuid.NewString(),
		Type: types.DeploymentTypeDocker,
	}
	if err := db.CreateApplication(ctx, &application, orgID); err != nil {
		t.Fatalf("create application: %v", err)
	}
	var secretID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Secret (organization_id, key, value)
		VALUES (@organizationId, @key, @value)
		RETURNING id`,
		pgx.NamedArgs{
			"organizationId": orgID,
			"key":            "api_token",
			"value":          "secret-value",
		},
	).Scan(&secretID); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	return orgID, application.ID, secretID
}

func apiVariableByKey(variableSet api.VariableSet, key string) api.Variable {
	for _, variable := range variableSet.Variables {
		if variable.Key == key {
			return variable
		}
	}
	return api.Variable{}
}

func resolvedAPIVariableByKey(variables []api.ResolvedVariable, key string) api.ResolvedVariable {
	for _, variable := range variables {
		if variable.Key == key {
			return variable
		}
	}
	return api.ResolvedVariable{}
}
