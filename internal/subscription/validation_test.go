package subscription

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/onsi/gomega"
)

func TestIsDeploymentTargetLimitReachedAllowsCommunityCustomerHundredthTarget(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := subscriptionValidationDBTestContext(t)

	orgID := createSubscriptionValidationOrganization(t, ctx)
	customerOrgID := createSubscriptionValidationCustomerOrganization(t, ctx, orgID)
	agentVersionID := createSubscriptionValidationAgentVersion(t, ctx)
	org := types.Organization{
		ID:               orgID,
		SubscriptionType: types.SubscriptionTypeCommunity,
	}

	insertSubscriptionValidationDeploymentTargets(t, ctx, orgID, customerOrgID, agentVersionID, 99)

	reached, err := IsDeploymentTargetLimitReached(ctx, org, &customerOrgID)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(reached).To(gomega.BeFalse(), "99 existing targets should still allow creating the 100th")

	insertSubscriptionValidationDeploymentTargets(t, ctx, orgID, customerOrgID, agentVersionID, 1)

	reached, err = IsDeploymentTargetLimitReached(ctx, org, &customerOrgID)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(reached).To(gomega.BeTrue(), "100 existing targets should reject creating the 101st")
}

func subscriptionValidationDBTestContext(t *testing.T) context.Context {
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

	schema := "subscription_validation_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

	runSubscriptionValidationTestMigrations(t, ctx, pool)
	return internalctx.WithDb(ctx, pool)
}

func runSubscriptionValidationTestMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "migrations", "sql", "*.up.sql"))
	if err != nil {
		t.Fatalf("list migration files: %v", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return subscriptionValidationMigrationVersion(t, files[i]) < subscriptionValidationMigrationVersion(t, files[j])
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

func subscriptionValidationMigrationVersion(t *testing.T, file string) int {
	t.Helper()
	base := filepath.Base(file)
	version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
	if err != nil {
		t.Fatalf("parse migration version %s: %v", file, err)
	}
	return version
}

func createSubscriptionValidationOrganization(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	var orgID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Organization (name, subscription_type)
		VALUES (@name, @subscriptionType)
		RETURNING id`,
		pgx.NamedArgs{
			"name":             "Organization " + uuid.NewString(),
			"subscriptionType": types.SubscriptionTypeCommunity,
		},
	).Scan(&orgID); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	return orgID
}

func createSubscriptionValidationCustomerOrganization(t *testing.T, ctx context.Context, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	var customerOrgID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO CustomerOrganization (organization_id, name)
		VALUES (@organizationId, @name)
		RETURNING id`,
		pgx.NamedArgs{
			"organizationId": orgID,
			"name":           "Customer " + uuid.NewString(),
		},
	).Scan(&customerOrgID); err != nil {
		t.Fatalf("create customer organization: %v", err)
	}
	return customerOrgID
}

func createSubscriptionValidationAgentVersion(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	var agentVersionID uuid.UUID
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO AgentVersion (name, manifest_file_revision, compose_file_revision)
		VALUES (@name, @manifestFileRevision, @composeFileRevision)
		RETURNING id`,
		pgx.NamedArgs{
			"name":                 "subscription-validation-agent-" + uuid.NewString(),
			"manifestFileRevision": "v1",
			"composeFileRevision":  "v1",
		},
	).Scan(&agentVersionID); err != nil {
		t.Fatalf("create agent version: %v", err)
	}
	return agentVersionID
}

func insertSubscriptionValidationDeploymentTargets(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	customerOrgID uuid.UUID,
	agentVersionID uuid.UUID,
	count int,
) {
	t.Helper()
	if _, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO DeploymentTarget (name, type, organization_id, customer_organization_id, agent_version_id)
		SELECT @namePrefix || gs::text, 'docker', @organizationId, @customerOrganizationId, @agentVersionId
		FROM generate_series(1, @count) AS gs`,
		pgx.NamedArgs{
			"namePrefix":             "target-" + uuid.NewString() + "-",
			"organizationId":         orgID,
			"customerOrganizationId": customerOrgID,
			"agentVersionId":         agentVersionID,
			"count":                  count,
		},
	); err != nil {
		t.Fatalf("insert deployment targets: %v", err)
	}
}
