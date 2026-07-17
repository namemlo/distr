package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
)

func TestDeleteDeploymentTargetWithIDReturnsConflictForProtectedReference(t *testing.T) {
	ctx := deploymentTargetDBTestContext(t)
	g := NewWithT(t)
	orgID := createDeploymentTargetTestOrganization(t, ctx)
	customerOrgID := createDeploymentTargetTestCustomerOrganization(t, ctx, orgID)
	environmentID := createDeploymentTargetTestEnvironment(t, ctx, orgID)
	targetID := createDeploymentTargetTestTarget(t, ctx, orgID, &customerOrgID)
	variableSetID, variableID := createDeploymentTargetTestVariable(t, ctx, orgID)

	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO VariableScopedValue (
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
		)`,
		pgx.NamedArgs{
			"organizationID":         orgID,
			"variableSetID":          variableSetID,
			"variableID":             variableID,
			"customerOrganizationID": customerOrgID,
			"environmentID":          environmentID,
			"deploymentTargetID":     targetID,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())

	err = DeleteDeploymentTargetWithID(ctx, targetID)

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	var remaining int
	g.Expect(internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT count(*) FROM DeploymentTarget WHERE id = @id`,
		pgx.NamedArgs{"id": targetID},
	).Scan(&remaining)).To(Succeed())
	g.Expect(remaining).To(Equal(1))
}

//nolint:dupl
func deploymentTargetDBTestContext(t *testing.T) context.Context {
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

	schema := "deployment_target_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

	runDeploymentTargetTestMigrations(t, ctx, pool)
	return internalctx.WithDb(ctx, pool)
}

func runDeploymentTargetTestMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "migrations", "sql", "*.up.sql"))
	if err != nil {
		t.Fatalf("list migration files: %v", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return deploymentTargetMigrationVersion(t, files[i]) < deploymentTargetMigrationVersion(t, files[j])
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

func deploymentTargetMigrationVersion(t *testing.T, file string) int {
	t.Helper()
	version, err := strconv.Atoi(strings.SplitN(filepath.Base(file), "_", 2)[0])
	if err != nil {
		t.Fatalf("parse migration version %s: %v", file, err)
	}
	return version
}

func createDeploymentTargetTestOrganization(t *testing.T, ctx context.Context) uuid.UUID {
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

func createDeploymentTargetTestCustomerOrganization(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
) uuid.UUID {
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

func createDeploymentTargetTestEnvironment(t *testing.T, ctx context.Context, orgID uuid.UUID) uuid.UUID {
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

func createDeploymentTargetTestTarget(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	customerOrgID *uuid.UUID,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO DeploymentTarget (
			name, type, organization_id, customer_organization_id, agent_version_id
		) VALUES (
			@name, 'docker', @organizationID, @customerOrganizationID, (SELECT id FROM AgentVersion LIMIT 1)
		)
		RETURNING id`,
		pgx.NamedArgs{
			"name":                   "Target " + uuid.NewString(),
			"organizationID":         orgID,
			"customerOrganizationID": customerOrgID,
		},
	).Scan(&id); err != nil {
		t.Fatalf("create deployment target: %v", err)
	}
	return id
}

func createDeploymentTargetTestVariable(t *testing.T, ctx context.Context, orgID uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	var variableSetID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO VariableSet (organization_id, name)
		VALUES (@organizationID, @name)
		RETURNING id`,
		pgx.NamedArgs{"organizationID": orgID, "name": "Variables " + uuid.NewString()},
	).Scan(&variableSetID); err != nil {
		t.Fatalf("create variable set: %v", err)
	}

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
