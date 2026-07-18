package migrations

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	migrate "github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	migratesource "github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	. "github.com/onsi/gomega"
)

type migrationTestDatabase struct {
	pool   *pgxpool.Pool
	runner *migrate.Migrate
}

func registerMigrationTestCleanup(
	t *testing.T,
	resource string,
	cleanup func() error,
) {
	t.Helper()
	t.Cleanup(func() {
		if cleanup == nil {
			return
		}
		if err := cleanup(); err != nil {
			t.Logf("cleanup %s: %v", resource, err)
		}
	})
}

func TestMigrationTestCleanupRunsInReverseAcquisitionOrder(t *testing.T) {
	var actual []string
	t.Run("partial setup", func(t *testing.T) {
		for _, resource := range []string{
			"admin",
			"schema",
			"sql database",
			"database driver",
			"source driver",
			"runner",
			"pool",
		} {
			registerMigrationTestCleanup(t, resource, func() error {
				actual = append(actual, resource)
				return nil
			})
		}
		registerMigrationTestCleanup(t, "unacquired resource", nil)
	})

	NewWithT(t).Expect(actual).To(Equal([]string{
		"pool",
		"runner",
		"source driver",
		"database driver",
		"sql database",
		"schema",
		"admin",
	}))
}

func newMigrationTestDatabase(t *testing.T) *migrationTestDatabase {
	t.Helper()
	g := NewWithT(t)
	databaseURL := os.Getenv("DISTR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DISTR_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	registerMigrationTestCleanup(t, "admin pool", func() error {
		if admin == nil {
			return nil
		}
		admin.Close()
		return nil
	})
	g.Expect(err).NotTo(HaveOccurred())
	schema := "migration_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	registerMigrationTestCleanup(t, "schema", func() error {
		if admin == nil || schema == "" {
			return nil
		}
		_, err := admin.Exec(
			context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE",
		)
		return err
	})
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema)
	g.Expect(err).NotTo(HaveOccurred())

	sqlConfig, err := pgx.ParseConfig(databaseURL)
	g.Expect(err).NotTo(HaveOccurred())
	sqlConfig.RuntimeParams["search_path"] = schema
	sqlDB := stdlib.OpenDB(*sqlConfig)
	registerMigrationTestCleanup(t, "SQL database", func() error {
		if sqlDB == nil {
			return nil
		}
		return sqlDB.Close()
	})
	g.Expect(sqlDB.PingContext(ctx)).To(Succeed())
	var databaseDriver migratedatabase.Driver
	databaseDriver, err = postgres.WithInstance(sqlDB, &postgres.Config{
		SchemaName: schema,
	})
	registerMigrationTestCleanup(t, "database driver", func() error {
		if databaseDriver == nil {
			return nil
		}
		return databaseDriver.Close()
	})
	g.Expect(err).NotTo(HaveOccurred())
	var sourceDriver migratesource.Driver
	sourceDriver, err = iofs.New(fs, "sql")
	registerMigrationTestCleanup(t, "source driver", func() error {
		if sourceDriver == nil {
			return nil
		}
		return sourceDriver.Close()
	})
	g.Expect(err).NotTo(HaveOccurred())
	runner, err := migrate.NewWithInstance(
		"", sourceDriver, "distr-test", databaseDriver,
	)
	registerMigrationTestCleanup(t, "runner", func() error {
		if runner == nil {
			return nil
		}
		sourceErr, databaseErr := runner.Close()
		return errors.Join(sourceErr, databaseErr)
	})
	if err == nil {
		sourceDriver = nil
		databaseDriver = nil
	}
	g.Expect(err).NotTo(HaveOccurred())

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	g.Expect(err).NotTo(HaveOccurred())
	poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+quotedSchema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	registerMigrationTestCleanup(t, "pool", func() error {
		if pool == nil {
			return nil
		}
		pool.Close()
		return nil
	})
	g.Expect(err).NotTo(HaveOccurred())
	return &migrationTestDatabase{pool: pool, runner: runner}
}

func (database *migrationTestDatabase) migrateTo(t *testing.T, version uint) {
	t.Helper()
	err := database.runner.Migrate(version)
	if !errors.Is(err, migrate.ErrNoChange) {
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	}
	actual, dirty, err := database.runner.Version()
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	NewWithT(t).Expect(actual).To(Equal(version))
	NewWithT(t).Expect(dirty).To(BeFalse())
}

func dropExternalExecutionFixtureForeignKeys(
	t *testing.T,
	database *migrationTestDatabase,
) {
	t.Helper()
	_, err := database.pool.Exec(context.Background(), `
DO $$
DECLARE item RECORD;
BEGIN
  FOR item IN
    SELECT relation.relname, constraint_row.conname
    FROM pg_constraint constraint_row
    JOIN pg_class relation ON relation.oid = constraint_row.conrelid
    WHERE relation.relnamespace = to_regnamespace(current_schema())
      AND relation.relname IN ('externalexecution', 'externalexecutionevent')
      AND constraint_row.contype = 'f'
  LOOP
    EXECUTE format(
      'ALTER TABLE %I DROP CONSTRAINT %I',
      item.relname,
      item.conname
    );
  END LOOP;
END
$$`)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
}

func insertHistoricalExecutionFixture(
	t *testing.T,
	database *migrationTestDatabase,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	g := NewWithT(t)
	executionID := uuid.New()
	eventID := uuid.New()
	organizationID := uuid.New()
	dropExternalExecutionFixtureForeignKeys(t, database)
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecution (
  id, created_at, updated_at, started_at, completed_at, callback_deadline_at,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum
) VALUES (
  $1::uuid,
  TIMESTAMP '2026-07-15 10:00:00.000001',
  TIMESTAMP '2026-07-15 10:01:00.000002',
  TIMESTAMP '2026-07-15 10:02:00.000003',
  TIMESTAMP '2026-07-15 10:03:00.000004',
  TIMESTAMP '2026-07-15 10:04:00.000005',
  $2, $3, $4, $5, $6, $7, $8, $9,
  'api-image', 'sha256:' || repeat('a', 64),
  'fixture-' || $1::uuid::text,
  0, '1.0.0', 'repo/image@sha256:' || repeat('b', 64), 'linux/amd64',
  'config:fixture', 'sha256:' || repeat('c', 64)
)`,
		executionID, organizationID, uuid.New(), uuid.New(), uuid.New(),
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionEvent (
  id, created_at, organization_id, external_execution_id,
  sequence, status, payload_hash
) VALUES (
  $1, TIMESTAMP '2026-07-15 10:05:00.000006',
  $2, $3, 1, 'SUCCEEDED', 'sha256:' || repeat('d', 64)
)`, eventID, organizationID, executionID)
	g.Expect(err).NotTo(HaveOccurred())
	return executionID, eventID
}
