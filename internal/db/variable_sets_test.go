package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
)

func TestVariableSetRepositoryCRUDAndReferenceMetadata(t *testing.T) {
	ctx := variableSetDBTestContext(t)
	g := NewWithT(t)
	orgID := createVariableSetTestOrganization(t, ctx)
	applicationID := createVariableSetApplicationForOrganization(t, ctx, orgID)
	secretID := createVariableSetSecretForOrganization(t, ctx, orgID, "api_token")

	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           " Shared Defaults ",
		Description:    "Reusable defaults",
		SortOrder:      10,
		ApplicationIDs: []uuid.UUID{applicationID},
		Variables: []types.Variable{
			{Key: " api_url ", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://example.test"`)},
			{Key: "replicas", Type: types.VariableTypeNumber, DefaultValue: json.RawMessage(`3`)},
			{Key: "enabled", Type: types.VariableTypeBoolean, DefaultValue: json.RawMessage(`true`)},
			{Key: "payload", Type: types.VariableTypeJSON, DefaultValue: json.RawMessage(`{"mode":"safe"}`)},
			{Key: "api_token", Type: types.VariableTypeSecretReference, ReferenceID: secretID.String()},
			{
				Key:           "cloud_account",
				Type:          types.VariableTypeAccountReference,
				ReferenceID:   uuid.NewString(),
				ReferenceName: "Build account",
			},
		},
	}

	g.Expect(db.CreateVariableSet(ctx, &variableSet)).To(Succeed())
	g.Expect(variableSet.Name).To(Equal("Shared Defaults"))
	g.Expect(variableSet.ApplicationIDs).To(Equal([]uuid.UUID{applicationID}))
	g.Expect(variableSet.Variables).To(HaveLen(6))
	g.Expect(variableByKey(variableSet, "api_url").DefaultValue).To(MatchJSON(`"https://example.test"`))
	g.Expect(variableByKey(variableSet, "replicas").DefaultValue).To(MatchJSON(`3`))
	g.Expect(variableByKey(variableSet, "enabled").DefaultValue).To(MatchJSON(`true`))
	g.Expect(variableByKey(variableSet, "payload").DefaultValue).To(MatchJSON(`{"mode":"safe"}`))
	secretVariable := variableByKey(variableSet, "api_token")
	g.Expect(secretVariable.ReferenceID).To(Equal(secretID.String()))
	g.Expect(secretVariable.ReferenceName).To(Equal("api_token"))
	g.Expect(secretVariable.DefaultValue).To(BeNil())

	loaded, err := db.GetVariableSet(ctx, variableSet.ID, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(loaded.Name).To(Equal("Shared Defaults"))
	g.Expect(loaded.ApplicationIDs).To(Equal([]uuid.UUID{applicationID}))
	g.Expect(variableByKey(*loaded, "api_token").ReferenceName).To(Equal("api_token"))

	variableSet.Name = "Runtime Defaults"
	variableSet.ApplicationIDs = nil
	variableSet.Variables = []types.Variable{
		{Key: "required_url", Type: types.VariableTypeString, IsRequired: true},
	}
	g.Expect(db.UpdateVariableSet(ctx, &variableSet)).To(Succeed())
	g.Expect(variableSet.Name).To(Equal("Runtime Defaults"))
	g.Expect(variableSet.ApplicationIDs).To(BeEmpty())
	g.Expect(variableSet.Variables).To(HaveLen(1))
	g.Expect(variableSet.Variables[0].IsRequired).To(BeTrue())

	sets, err := db.GetVariableSetsByOrganizationID(ctx, orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(sets).To(HaveLen(1))
	g.Expect(sets[0].Name).To(Equal("Runtime Defaults"))

	g.Expect(db.DeleteVariableSetWithID(ctx, variableSet.ID, orgID)).To(Succeed())
	_, err = db.GetVariableSet(ctx, variableSet.ID, orgID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestVariableSetRepositoryRejectsDuplicateNamesAndKeys(t *testing.T) {
	ctx := variableSetDBTestContext(t)
	g := NewWithT(t)
	orgID := createVariableSetTestOrganization(t, ctx)

	first := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Shared Defaults",
		Variables: []types.Variable{
			{Key: "api_url", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://example.test"`)},
		},
	}
	g.Expect(db.CreateVariableSet(ctx, &first)).To(Succeed())

	duplicateName := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Shared Defaults",
	}
	err := db.CreateVariableSet(ctx, &duplicateName)
	g.Expect(errors.Is(err, apierrors.ErrAlreadyExists)).To(BeTrue())

	duplicateKey := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Other Defaults",
		Variables: []types.Variable{
			{Key: "api_url", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://example.test"`)},
			{Key: " api_url ", Type: types.VariableTypeString, DefaultValue: json.RawMessage(`"https://other.test"`)},
		},
	}
	err = db.CreateVariableSet(ctx, &duplicateKey)
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}

func TestVariableSetRepositoryRejectsCrossOrganizationReferences(t *testing.T) {
	ctx := variableSetDBTestContext(t)
	g := NewWithT(t)
	orgID := createVariableSetTestOrganization(t, ctx)
	otherOrgID := createVariableSetTestOrganization(t, ctx)
	otherApplicationID := createVariableSetApplicationForOrganization(t, ctx, otherOrgID)
	otherSecretID := createVariableSetSecretForOrganization(t, ctx, otherOrgID, "api_token")

	crossApp := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Shared Defaults",
		ApplicationIDs: []uuid.UUID{otherApplicationID},
	}
	err := db.CreateVariableSet(ctx, &crossApp)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	crossSecret := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Secret Defaults",
		Variables: []types.Variable{
			{Key: "api_token", Type: types.VariableTypeSecretReference, ReferenceID: otherSecretID.String()},
		},
	}
	err = db.CreateVariableSet(ctx, &crossSecret)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestVariableSetSecretReferencePreventsUnsafeSecretDelete(t *testing.T) {
	ctx := variableSetDBTestContext(t)
	g := NewWithT(t)
	orgID := createVariableSetTestOrganization(t, ctx)
	secretID := createVariableSetSecretForOrganization(t, ctx, orgID, "api_token")

	variableSet := types.VariableSet{
		OrganizationID: orgID,
		Name:           "Shared Defaults",
		Variables: []types.Variable{
			{Key: "api_token", Type: types.VariableTypeSecretReference, ReferenceID: secretID.String()},
		},
	}
	g.Expect(db.CreateVariableSet(ctx, &variableSet)).To(Succeed())

	err := db.DeleteSecret(ctx, secretID, orgID, nil, nil)

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestVariableSetMigrationDefinesScopedTables(t *testing.T) {
	g := NewWithT(t)

	up, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "117_variable_sets.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	sql := string(up)

	g.Expect(sql).To(ContainSubstring("CREATE TABLE VariableSet"))
	g.Expect(sql).To(ContainSubstring("variableset_organization_name_unique"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE VariableSetApplication"))
	g.Expect(sql).To(ContainSubstring("CREATE TABLE Variable"))
	g.Expect(sql).To(ContainSubstring("variable_secret_reference_org_fk"))
	g.Expect(sql).To(ContainSubstring("REFERENCES Secret(id, organization_id) ON DELETE RESTRICT"))
	g.Expect(sql).To(ContainSubstring("variable_variable_set_key_unique"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "117_variable_sets.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS Variable"))
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS VariableSetApplication"))
	g.Expect(string(down)).To(ContainSubstring("DROP TABLE IF EXISTS VariableSet"))
}

//nolint:dupl
func variableSetDBTestContext(t *testing.T) context.Context {
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

	schema := "variable_set_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

	runVariableSetTestMigrations(t, ctx, pool)
	return internalctx.WithDb(ctx, pool)
}

func runVariableSetTestMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "migrations", "sql", "*.up.sql"))
	if err != nil {
		t.Fatalf("list migration files: %v", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return variableSetMigrationVersion(t, files[i]) < variableSetMigrationVersion(t, files[j])
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

func variableSetMigrationVersion(t *testing.T, file string) int {
	t.Helper()
	base := filepath.Base(file)
	version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
	if err != nil {
		t.Fatalf("parse migration version %s: %v", file, err)
	}
	return version
}

func createVariableSetTestOrganization(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	var orgID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Organization (name) VALUES (@name) RETURNING id`,
		pgx.NamedArgs{"name": "Organization " + uuid.NewString()},
	).Scan(&orgID); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	return orgID
}

func createVariableSetApplicationForOrganization(t *testing.T, ctx context.Context, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	application := types.Application{
		Name: "Application " + uuid.NewString(),
		Type: types.DeploymentTypeDocker,
	}
	if err := db.CreateApplication(ctx, &application, orgID); err != nil {
		t.Fatalf("create application: %v", err)
	}
	return application.ID
}

func createVariableSetSecretForOrganization(t *testing.T, ctx context.Context, orgID uuid.UUID, key string) uuid.UUID {
	t.Helper()
	var secretID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Secret (organization_id, key, value)
		VALUES (@organizationId, @key, @value)
		RETURNING id`,
		pgx.NamedArgs{
			"organizationId": orgID,
			"key":            key,
			"value":          "secret-value",
		},
	).Scan(&secretID); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	return secretID
}

func variableByKey(variableSet types.VariableSet, key string) types.Variable {
	for _, variable := range variableSet.Variables {
		if variable.Key == key {
			return variable
		}
	}
	return types.Variable{}
}
