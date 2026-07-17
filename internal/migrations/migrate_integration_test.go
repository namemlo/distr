package migrations

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	migratepkg "github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

type runnerTestDatabase struct {
	pool   *pgxpool.Pool
	runner *Runner
	schema string
	url    string
}

func newRunnerTestDatabase(t *testing.T) *runnerTestDatabase {
	t.Helper()
	g := NewWithT(t)
	databaseURL := os.Getenv("DISTR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DISTR_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	registerMigrationTestCleanup(t, "runner admin pool", func() error {
		if admin != nil {
			admin.Close()
		}
		return nil
	})
	g.Expect(err).NotTo(HaveOccurred())
	schema := "migration_runner_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	registerMigrationTestCleanup(t, "runner schema", func() error {
		if admin == nil {
			return nil
		}
		_, err := admin.Exec(
			context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE",
		)
		return err
	})
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema)
	g.Expect(err).NotTo(HaveOccurred())

	parsedURL, err := url.Parse(databaseURL)
	g.Expect(err).NotTo(HaveOccurred())
	query := parsedURL.Query()
	query.Set("search_path", schema)
	parsedURL.RawQuery = query.Encode()
	scopedURL := parsedURL.String()
	_, err = pgx.ParseConfig(scopedURL)
	g.Expect(err).NotTo(HaveOccurred())
	runner, err := Open(scopedURL, zap.NewNop())
	registerMigrationTestCleanup(t, "migration runner", func() error {
		if runner == nil {
			return nil
		}
		return runner.Close()
	})
	g.Expect(err).NotTo(HaveOccurred())
	poolConfig, err := pgxpool.ParseConfig(scopedURL)
	g.Expect(err).NotTo(HaveOccurred())
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	registerMigrationTestCleanup(t, "runner pool", func() error {
		if pool != nil {
			pool.Close()
		}
		return nil
	})
	g.Expect(err).NotTo(HaveOccurred())
	return &runnerTestDatabase{
		pool: pool, runner: runner, schema: schema, url: scopedURL,
	}
}

func (database *runnerTestDatabase) migrateTo(t *testing.T, version uint) {
	t.Helper()
	g := NewWithT(t)
	databaseHandle, err := sql.Open("pgx", database.url)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() {
		if err := databaseHandle.Close(); err != nil {
			t.Errorf("close fixture database: %v", err)
		}
	}()
	databaseDriver, err := postgres.WithInstance(databaseHandle, &postgres.Config{
		SchemaName: database.schema,
	})
	g.Expect(err).NotTo(HaveOccurred())
	databaseDriverOwned := true
	defer func() {
		if databaseDriverOwned {
			if err := databaseDriver.Close(); err != nil {
				t.Errorf("close fixture database driver: %v", err)
			}
		}
	}()
	sourceDriver, err := iofs.New(fs, "sql")
	g.Expect(err).NotTo(HaveOccurred())
	sourceDriverOwned := true
	defer func() {
		if sourceDriverOwned {
			if err := sourceDriver.Close(); err != nil {
				t.Errorf("close fixture source driver: %v", err)
			}
		}
	}()
	runner, err := migratepkg.NewWithInstance(
		"", sourceDriver, "distr-runner-test", databaseDriver,
	)
	g.Expect(err).NotTo(HaveOccurred())
	databaseDriverOwned = false
	sourceDriverOwned = false
	defer func() {
		sourceErr, databaseErr := runner.Close()
		if err := errors.Join(sourceErr, databaseErr); err != nil {
			t.Errorf("close fixture migration runner: %v", err)
		}
	}()
	err = runner.Migrate(version)
	if !errors.Is(err, migratepkg.ErrNoChange) {
		g.Expect(err).NotTo(HaveOccurred())
	}
}

func (database *runnerTestDatabase) schemaMigrationsExists(
	t *testing.T,
) bool {
	t.Helper()
	var exists bool
	err := database.pool.QueryRow(context.Background(), `
SELECT to_regclass(format('%I.schema_migrations', current_schema()))
       IS NOT NULL`).Scan(&exists)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return exists
}

func TestRunnerStatusDoesNotConstructMigrator(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	var constructed uint64
	database.runner.engineFactory = func(
		*sql.DB,
		*zap.Logger,
		string,
		time.Duration,
	) (migrationEngine, error) {
		constructed++
		return &fakeMigrationEngine{}, nil
	}
	g.Expect(database.schemaMigrationsExists(t)).To(BeFalse())
	status, err := database.runner.Status(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(SchemaStatus{Version: -1, Dirty: false}))
	g.Expect(constructed).To(Equal(uint64(0)))
	g.Expect(database.schemaMigrationsExists(t)).To(BeFalse())
}

func TestRunnerCheckOnlyDoesNotConstructMigrator(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	var constructed uint64
	database.runner.engineFactory = func(
		*sql.DB,
		*zap.Logger,
		string,
		time.Duration,
	) (migrationEngine, error) {
		constructed++
		return &fakeMigrationEngine{}, nil
	}
	g.Expect(database.schemaMigrationsExists(t)).To(BeFalse())
	g.Expect(database.runner.Run(context.Background(), RunOptions{
		CheckOnly: true,
	})).To(Succeed())
	g.Expect(constructed).To(Equal(uint64(0)))
	g.Expect(database.schemaMigrationsExists(t)).To(BeFalse())
}

func TestRunnerRejectsNewerSchemaForEveryRunMode(t *testing.T) {
	tests := []struct {
		name    string
		options RunOptions
	}{
		{name: "default up"},
		{name: "down", options: RunOptions{Down: true}},
		{name: "target 138", options: RunOptions{Target: uintPointer(138)}},
		{name: "target 137", options: RunOptions{Target: uintPointer(137)}},
		{name: "target 0", options: RunOptions{Target: uintPointer(0)}},
		{name: "check default", options: RunOptions{CheckOnly: true}},
		{name: "check down", options: RunOptions{Down: true, CheckOnly: true}},
		{
			name: "check target 138",
			options: RunOptions{
				Target: uintPointer(138), CheckOnly: true,
			},
		},
		{
			name: "check target 137",
			options: RunOptions{
				Target: uintPointer(137), CheckOnly: true,
			},
		},
		{
			name: "check target 0",
			options: RunOptions{
				Target: uintPointer(0), CheckOnly: true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			_, err := database.pool.Exec(context.Background(), `
CREATE TABLE schema_migrations (
  version BIGINT PRIMARY KEY,
  dirty BOOLEAN NOT NULL
);
INSERT INTO schema_migrations VALUES (139, FALSE);
CREATE TABLE newer_schema_marker (id BIGINT PRIMARY KEY)`)
			g.Expect(err).NotTo(HaveOccurred())

			var beforeTables string
			err = database.pool.QueryRow(context.Background(), `
SELECT COALESCE(string_agg(relation.relname, ',' ORDER BY relation.relname), '')
FROM pg_catalog.pg_class relation
JOIN pg_catalog.pg_namespace namespace ON namespace.oid=relation.relnamespace
WHERE namespace.nspname=current_schema()
  AND relation.relkind='r'`).Scan(&beforeTables)
			g.Expect(err).NotTo(HaveOccurred())

			var constructed uint64
			database.runner.engineFactory = func(
				*sql.DB,
				*zap.Logger,
				string,
				time.Duration,
			) (migrationEngine, error) {
				constructed++
				return &fakeMigrationEngine{}, nil
			}
			err = database.runner.Run(context.Background(), test.options)
			g.Expect(err).To(MatchError(
				"current schema 139 is newer than latest embedded schema 138",
			))
			g.Expect(constructed).To(Equal(uint64(0)))

			status, statusErr := database.runner.Status(context.Background())
			g.Expect(statusErr).NotTo(HaveOccurred())
			g.Expect(status).To(Equal(SchemaStatus{Version: 139, Dirty: false}))
			var afterTables string
			statusErr = database.pool.QueryRow(context.Background(), `
SELECT COALESCE(string_agg(relation.relname, ',' ORDER BY relation.relname), '')
FROM pg_catalog.pg_class relation
JOIN pg_catalog.pg_namespace namespace ON namespace.oid=relation.relnamespace
WHERE namespace.nspname=current_schema()
  AND relation.relkind='r'`).Scan(&afterTables)
			g.Expect(statusErr).NotTo(HaveOccurred())
			g.Expect(afterTables).To(Equal(beforeTables))
		})
	}
}

func TestExternalExecutionTimestampPreflightRejectsMissingExecutionTablesAtOrAfter136(
	t *testing.T,
) {
	tests := []struct {
		name      string
		version   uint
		checkOnly bool
	}{
		{name: "schema 136 mutating", version: 136},
		{name: "schema 136 check", version: 136, checkOnly: true},
		{name: "schema 137 mutating", version: 137},
		{name: "schema 137 check", version: 137, checkOnly: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			database.migrateTo(t, test.version)
			_, err := database.pool.Exec(context.Background(), `
DROP TABLE ExternalExecutionEvent, ExternalExecution CASCADE`)
			g.Expect(err).NotTo(HaveOccurred())

			before, err := database.runner.Status(context.Background())
			g.Expect(err).NotTo(HaveOccurred())
			var constructed uint64
			database.runner.engineFactory = func(
				*sql.DB,
				*zap.Logger,
				string,
				time.Duration,
			) (migrationEngine, error) {
				constructed++
				return &fakeMigrationEngine{}, nil
			}
			target := uint(138)
			err = database.runner.Run(context.Background(), RunOptions{
				Target:    &target,
				CheckOnly: test.checkOnly,
			})
			g.Expect(err).To(MatchError(
				"schema 136 or later requires ExternalExecution and ExternalExecutionEvent",
			))
			g.Expect(constructed).To(Equal(uint64(0)))

			after, statusErr := database.runner.Status(context.Background())
			g.Expect(statusErr).NotTo(HaveOccurred())
			g.Expect(after).To(Equal(before))
			var executionExists, eventExists bool
			statusErr = database.pool.QueryRow(context.Background(), `
SELECT
  to_regclass(format('%I.externalexecution', current_schema())) IS NOT NULL,
  to_regclass(format('%I.externalexecutionevent', current_schema())) IS NOT NULL`).Scan(
				&executionExists, &eventExists,
			)
			g.Expect(statusErr).NotTo(HaveOccurred())
			g.Expect(executionExists).To(BeFalse())
			g.Expect(eventExists).To(BeFalse())
		})
	}
}

func TestExternalExecutionTimestampPreflightRejectsMissingExecutionTablesOutside138Crossing(
	t *testing.T,
) {
	tests := []struct {
		name    string
		version uint
		options RunOptions
	}{
		{name: "schema 136 target 136", version: 136, options: RunOptions{Target: uintPointer(136)}},
		{
			name: "schema 136 check target 137", version: 136,
			options: RunOptions{Target: uintPointer(137), CheckOnly: true},
		},
		{name: "schema 136 down", version: 136, options: RunOptions{Down: true}},
		{name: "schema 137 target 137", version: 137, options: RunOptions{Target: uintPointer(137)}},
		{
			name: "schema 137 check target 137", version: 137,
			options: RunOptions{Target: uintPointer(137), CheckOnly: true},
		},
		{name: "schema 137 down", version: 137, options: RunOptions{Down: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			_, err := database.pool.Exec(context.Background(), `
CREATE TABLE schema_migrations (
  version BIGINT PRIMARY KEY,
  dirty BOOLEAN NOT NULL
);
CREATE TABLE application_marker (id BIGINT PRIMARY KEY)`)
			g.Expect(err).NotTo(HaveOccurred())
			_, err = database.pool.Exec(
				context.Background(),
				`INSERT INTO schema_migrations VALUES ($1, FALSE)`,
				test.version,
			)
			g.Expect(err).NotTo(HaveOccurred())
			var constructed uint64
			database.runner.engineFactory = func(
				*sql.DB,
				*zap.Logger,
				string,
				time.Duration,
			) (migrationEngine, error) {
				constructed++
				return &fakeMigrationEngine{}, nil
			}

			err = database.runner.Run(context.Background(), test.options)
			g.Expect(err).To(MatchError(
				"schema 136 or later requires ExternalExecution and ExternalExecutionEvent",
			))
			g.Expect(constructed).To(Equal(uint64(0)))
			status, statusErr := database.runner.Status(context.Background())
			g.Expect(statusErr).NotTo(HaveOccurred())
			g.Expect(status).To(Equal(SchemaStatus{
				Version: int(test.version), Dirty: false,
			}))
		})
	}
}

func TestExternalExecutionTimestampPreflightRejectsVersionlessApplicationTables(
	t *testing.T,
) {
	for _, test := range []struct {
		name      string
		checkOnly bool
	}{
		{name: "mutating"},
		{name: "check", checkOnly: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			_, err := database.pool.Exec(context.Background(), `
CREATE TABLE application_marker (id BIGINT PRIMARY KEY)`)
			g.Expect(err).NotTo(HaveOccurred())
			var constructed uint64
			database.runner.engineFactory = func(
				*sql.DB,
				*zap.Logger,
				string,
				time.Duration,
			) (migrationEngine, error) {
				constructed++
				return &fakeMigrationEngine{}, nil
			}
			target := uint(138)
			err = database.runner.Run(context.Background(), RunOptions{
				Target:    &target,
				CheckOnly: test.checkOnly,
			})
			g.Expect(err).To(MatchError(
				"schema_migrations is absent for an existing application schema",
			))
			g.Expect(constructed).To(Equal(uint64(0)))

			status, statusErr := database.runner.Status(context.Background())
			g.Expect(statusErr).NotTo(HaveOccurred())
			g.Expect(status).To(Equal(SchemaStatus{Version: -1, Dirty: false}))
			var markerExists bool
			statusErr = database.pool.QueryRow(context.Background(), `
SELECT to_regclass(format('%I.application_marker', current_schema())) IS NOT NULL`).Scan(
				&markerExists,
			)
			g.Expect(statusErr).NotTo(HaveOccurred())
			g.Expect(markerExists).To(BeTrue())
		})
	}
}

func TestExternalExecutionTimestampPreflightRejectsVersionlessApplicationTablesOutside138Crossing(
	t *testing.T,
) {
	for _, test := range []struct {
		name    string
		options RunOptions
	}{
		{name: "target 137", options: RunOptions{Target: uintPointer(137)}},
		{
			name:    "check target 137",
			options: RunOptions{Target: uintPointer(137), CheckOnly: true},
		},
		{name: "target 0", options: RunOptions{Target: uintPointer(0)}},
		{name: "down", options: RunOptions{Down: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			_, err := database.pool.Exec(context.Background(), `
CREATE TABLE application_marker (id BIGINT PRIMARY KEY)`)
			g.Expect(err).NotTo(HaveOccurred())
			var constructed uint64
			database.runner.engineFactory = func(
				*sql.DB,
				*zap.Logger,
				string,
				time.Duration,
			) (migrationEngine, error) {
				constructed++
				return &fakeMigrationEngine{}, nil
			}

			err = database.runner.Run(context.Background(), test.options)
			g.Expect(err).To(MatchError(
				"schema_migrations is absent for an existing application schema",
			))
			g.Expect(constructed).To(Equal(uint64(0)))
			status, statusErr := database.runner.Status(context.Background())
			g.Expect(statusErr).NotTo(HaveOccurred())
			g.Expect(status).To(Equal(SchemaStatus{Version: -1, Dirty: false}))
		})
	}
}

func TestExternalExecutionTimestampPreflightIgnoresExtensionOwnedOrdinaryTables(
	t *testing.T,
) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	quotedSchema := pgx.Identifier{database.schema}.Sanitize()
	_, err := database.pool.Exec(
		context.Background(), "CREATE EXTENSION hstore WITH SCHEMA "+quotedSchema,
	)
	g.Expect(err).NotTo(HaveOccurred())
	registerMigrationTestCleanup(t, "hstore extension", func() error {
		_, err := database.pool.Exec(
			context.Background(), "DROP EXTENSION IF EXISTS hstore CASCADE",
		)
		return err
	})
	_, err = database.pool.Exec(context.Background(), `
CREATE TABLE extension_owned_marker (id BIGINT PRIMARY KEY);
ALTER EXTENSION hstore ADD TABLE extension_owned_marker`)
	g.Expect(err).NotTo(HaveOccurred())

	var constructed uint64
	database.runner.engineFactory = func(
		*sql.DB,
		*zap.Logger,
		string,
		time.Duration,
	) (migrationEngine, error) {
		constructed++
		return &fakeMigrationEngine{}, nil
	}
	target := uint(138)
	g.Expect(database.runner.Run(context.Background(), RunOptions{
		Target:    &target,
		CheckOnly: true,
	})).To(Succeed())
	g.Expect(constructed).To(Equal(uint64(0)))
	status, err := database.runner.Status(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(SchemaStatus{Version: -1, Dirty: false}))
}

func TestRunnerMigratesLegitimatePre136SchemaWithoutExecutionTables(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 135)
	var executionExists, eventExists bool
	err := database.pool.QueryRow(context.Background(), `
SELECT
  to_regclass(format('%I.externalexecution', current_schema())) IS NOT NULL,
  to_regclass(format('%I.externalexecutionevent', current_schema())) IS NOT NULL`).Scan(
		&executionExists, &eventExists,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(executionExists).To(BeFalse())
	g.Expect(eventExists).To(BeFalse())

	target := uint(138)
	g.Expect(database.runner.Run(context.Background(), RunOptions{
		Target: &target,
	})).To(Succeed())
	status, err := database.runner.Status(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(SchemaStatus{Version: 138, Dirty: false}))
	err = database.pool.QueryRow(context.Background(), `
SELECT
  to_regclass(format('%I.externalexecution', current_schema())) IS NOT NULL,
  to_regclass(format('%I.externalexecutionevent', current_schema())) IS NOT NULL`).Scan(
		&executionExists, &eventExists,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(executionExists).To(BeTrue())
	g.Expect(eventExists).To(BeTrue())
}

func TestRunnerStatusEmptyAndInvalidCatalogs(t *testing.T) {
	t.Run("empty exact table", func(t *testing.T) {
		g := NewWithT(t)
		database := newRunnerTestDatabase(t)
		_, err := database.pool.Exec(context.Background(), `
CREATE TABLE schema_migrations (
	  version BIGINT PRIMARY KEY,
  dirty BOOLEAN NOT NULL
)`)
		g.Expect(err).NotTo(HaveOccurred())
		status, err := database.runner.Status(context.Background())
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(status).To(Equal(SchemaStatus{Version: -1, Dirty: false}))
		g.Expect(database.runner.Run(context.Background(), RunOptions{
			CheckOnly: true,
		})).To(Succeed())
		var rows int
		err = database.pool.QueryRow(
			context.Background(), `SELECT count(*) FROM schema_migrations`,
		).Scan(&rows)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(rows).To(Equal(0))
	})

	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "same-name view",
			sql: `CREATE VIEW schema_migrations AS
SELECT 137::BIGINT AS version, FALSE::BOOLEAN AS dirty`,
			want: "schema_migrations must be an ordinary table",
		},
		{
			name: "missing primary key",
			sql: `CREATE TABLE schema_migrations (
  version BIGINT NOT NULL,
  dirty BOOLEAN NOT NULL
)`,
			want: "primary key on version",
		},
		{
			name: "wrong version type",
			sql: `CREATE TABLE schema_migrations (
	  version TEXT PRIMARY KEY,
  dirty BOOLEAN NOT NULL
)`,
			want: "catalog must contain exactly",
		},
		{
			name: "nullable dirty",
			sql: `CREATE TABLE schema_migrations (
	  version BIGINT PRIMARY KEY,
  dirty BOOLEAN
)`,
			want: "catalog must contain exactly",
		},
		{
			name: "extra column",
			sql: `CREATE TABLE schema_migrations (
	  version BIGINT PRIMARY KEY,
  dirty BOOLEAN NOT NULL,
  extra TEXT
)`,
			want: "catalog must contain exactly",
		},
		{
			name: "multiple rows",
			sql: `CREATE TABLE schema_migrations (
	  version BIGINT PRIMARY KEY,
  dirty BOOLEAN NOT NULL
);
INSERT INTO schema_migrations VALUES (136, FALSE), (137, FALSE)`,
			want: "at most one row; found 2",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			database := newRunnerTestDatabase(t)
			_, err := database.pool.Exec(context.Background(), test.sql)
			g.Expect(err).NotTo(HaveOccurred())
			_, err = database.runner.Status(context.Background())
			g.Expect(err).To(MatchError(ContainSubstring(test.want)))
		})
	}
}

func acquireTimestampMigrationAdvisoryLock(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) *pgxpool.Conn {
	t.Helper()
	connection, err := pool.Acquire(ctx)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	_, err = connection.Exec(
		ctx,
		`SELECT pg_advisory_lock($1)`,
		externalexecutiontimestamp.MigrationAdvisoryLockKey,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return connection
}

func releaseTimestampMigrationAdvisoryLock(
	t *testing.T,
	ctx context.Context,
	connection *pgxpool.Conn,
) {
	t.Helper()
	_, err := connection.Exec(
		ctx,
		`SELECT pg_advisory_unlock($1)`,
		externalexecutiontimestamp.MigrationAdvisoryLockKey,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	connection.Release()
}

func TestRunnerAdvisoryLockTimeout(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	ctx := context.Background()
	lock := acquireTimestampMigrationAdvisoryLock(t, ctx, database.pool)
	defer releaseTimestampMigrationAdvisoryLock(t, context.Background(), lock)
	before, err := database.runner.Status(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	var engineFactoryCalls uint64
	database.runner.engineFactory = func(
		*sql.DB,
		*zap.Logger,
		string,
		time.Duration,
	) (migrationEngine, error) {
		engineFactoryCalls++
		return &fakeMigrationEngine{}, nil
	}
	err = database.runner.Run(ctx, RunOptions{
		Target:      uintPointer(138),
		LockTimeout: 100 * time.Millisecond,
	})
	g.Expect(err).To(MatchError(ContainSubstring(
		"timestamp migration advisory lock timeout after 100ms",
	)))
	g.Expect(engineFactoryCalls).To(Equal(uint64(0)))
	after, err := database.runner.Status(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(after).To(Equal(before))
}

func TestRunnerAdvisoryLockCancellation(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	lock := acquireTimestampMigrationAdvisoryLock(
		t, context.Background(), database.pool,
	)
	defer releaseTimestampMigrationAdvisoryLock(
		t, context.Background(), lock,
	)
	var engineFactoryCalls uint64
	database.runner.engineFactory = func(
		*sql.DB,
		*zap.Logger,
		string,
		time.Duration,
	) (migrationEngine, error) {
		engineFactoryCalls++
		return &fakeMigrationEngine{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := database.runner.Run(ctx, RunOptions{
		Target:      uintPointer(138),
		LockTimeout: 5 * time.Second,
	})
	g.Expect(err).To(MatchError(context.DeadlineExceeded))
	g.Expect(time.Since(started)).To(BeNumerically("<", 2*time.Second))
	g.Expect(engineFactoryCalls).To(Equal(uint64(0)))
	status, statusErr := database.runner.Status(context.Background())
	g.Expect(statusErr).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(SchemaStatus{Version: 137, Dirty: false}))
}

type blockingMigrationEngine struct {
	started   chan struct{}
	stopped   chan struct{}
	stopOnce  sync.Once
	stopCalls atomic.Uint64
	upCalls   atomic.Uint64
}

func newBlockingMigrationEngine() *blockingMigrationEngine {
	return &blockingMigrationEngine{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (engine *blockingMigrationEngine) Up() error {
	engine.upCalls.Add(1)
	close(engine.started)
	<-engine.stopped
	return context.Canceled
}

func (engine *blockingMigrationEngine) Down() error {
	return errors.New("unexpected Down call")
}

func (engine *blockingMigrationEngine) Migrate(uint) error {
	return errors.New("unexpected Migrate call")
}

func (engine *blockingMigrationEngine) Stop() {
	engine.stopCalls.Add(1)
	engine.stopOnce.Do(func() { close(engine.stopped) })
}

func (*blockingMigrationEngine) Close() error { return nil }

type joiningStopMigrationEngine struct {
	started     chan struct{}
	allowUp     chan struct{}
	stopStarted chan struct{}
	allowStop   chan struct{}
	stopCalls   atomic.Uint64
	stopOnce    sync.Once
}

func newJoiningStopMigrationEngine() *joiningStopMigrationEngine {
	return &joiningStopMigrationEngine{
		started:     make(chan struct{}),
		allowUp:     make(chan struct{}),
		stopStarted: make(chan struct{}),
		allowStop:   make(chan struct{}),
	}
}

func (engine *joiningStopMigrationEngine) Up() error {
	close(engine.started)
	<-engine.allowUp
	return nil
}

func (*joiningStopMigrationEngine) Down() error {
	return errors.New("unexpected Down call")
}

func (*joiningStopMigrationEngine) Migrate(uint) error {
	return errors.New("unexpected Migrate call")
}

func (engine *joiningStopMigrationEngine) Stop() {
	engine.stopCalls.Add(1)
	engine.stopOnce.Do(func() {
		close(engine.stopStarted)
		<-engine.allowStop
	})
}

func (*joiningStopMigrationEngine) Close() error { return nil }

func TestRunnerCancellationStopsBeforeNextMigration(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	engine := newBlockingMigrationEngine()
	database.runner.engineFactory = func(
		*sql.DB,
		*zap.Logger,
		string,
		time.Duration,
	) (migrationEngine, error) {
		return engine, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- database.runner.Run(ctx, RunOptions{})
	}()
	select {
	case <-engine.started:
	case <-time.After(5 * time.Second):
		t.Fatal("migration engine did not start")
	}
	cancel()
	select {
	case err := <-result:
		g.Expect(err).To(MatchError(context.Canceled))
	case <-time.After(5 * time.Second):
		t.Fatal("migration runner did not stop after cancellation")
	}
	g.Expect(engine.stopCalls.Load()).To(Equal(uint64(1)))
	g.Expect(engine.upCalls.Load()).To(Equal(uint64(1)))

	connection, err := database.pool.Acquire(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer connection.Release()
	var locked bool
	err = connection.QueryRow(
		context.Background(),
		`SELECT pg_try_advisory_lock($1)`,
		externalexecutiontimestamp.MigrationAdvisoryLockKey,
	).Scan(&locked)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(locked).To(BeTrue())
	var unlocked bool
	err = connection.QueryRow(
		context.Background(),
		`SELECT pg_advisory_unlock($1)`,
		externalexecutiontimestamp.MigrationAdvisoryLockKey,
	).Scan(&unlocked)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(unlocked).To(BeTrue())
}

func TestRunnerCancellationJoinsStopWatcher(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	engine := newJoiningStopMigrationEngine()
	database.runner.engineFactory = func(
		*sql.DB,
		*zap.Logger,
		string,
		time.Duration,
	) (migrationEngine, error) {
		return engine, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- database.runner.Run(ctx, RunOptions{}) }()
	select {
	case <-engine.started:
	case <-time.After(5 * time.Second):
		t.Fatal("migration engine did not start")
	}
	cancel()
	select {
	case <-engine.stopStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation watcher did not enter Stop")
	}
	close(engine.allowUp)
	var earlyErr error
	returnedEarly := false
	select {
	case earlyErr = <-result:
		returnedEarly = true
	case <-time.After(150 * time.Millisecond):
	}
	close(engine.allowStop)
	if !returnedEarly {
		select {
		case earlyErr = <-result:
		case <-time.After(5 * time.Second):
			t.Fatal("migration runner did not return after Stop completed")
		}
	}
	g.Expect(returnedEarly).To(BeFalse(),
		"Runner.Run returned while its cancellation Stop call was still active")
	g.Expect(earlyErr).To(MatchError(context.Canceled))
	g.Expect(engine.stopCalls.Load()).To(Equal(uint64(1)))
}

func TestRunnerConfiguresDriverTimeouts(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	var capturedSchema string
	var capturedLockTimeout time.Duration
	database.runner.engineFactory = func(
		_ *sql.DB,
		_ *zap.Logger,
		schema string,
		lockTimeout time.Duration,
	) (migrationEngine, error) {
		capturedSchema = schema
		capturedLockTimeout = lockTimeout
		return &fakeMigrationEngine{}, nil
	}
	g.Expect(database.runner.Run(context.Background(), RunOptions{
		Target:      uintPointer(138),
		LockTimeout: 275 * time.Millisecond,
	})).To(Succeed())
	g.Expect(capturedSchema).To(Equal(database.schema))
	g.Expect(capturedLockTimeout).To(Equal(275 * time.Millisecond))
	g.Expect(migrationStatementTimeout).To(Equal(5 * time.Minute))
}

func TestRunnerMigratesCleanInstallInConfiguredSchema(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	target := uint(138)
	g.Expect(database.runner.Run(context.Background(), RunOptions{
		Target: &target,
	})).To(Succeed())
	status, err := database.runner.Status(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(SchemaStatus{Version: 138, Dirty: false}))
	var kind string
	err = database.pool.QueryRow(context.Background(), `
SELECT transition_kind
FROM ExternalExecutionTimestampExpandState
WHERE singleton`).Scan(&kind)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(kind).To(Equal("ZERO_HISTORY"))
	var publicVersionTable bool
	err = database.pool.QueryRow(context.Background(), `
SELECT to_regclass('public.schema_migrations') IS NOT NULL`).Scan(
		&publicVersionTable,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(publicVersionTable).To(BeFalse())
}

func TestRunnerMigratesHistoricalDataWithMatchingManifest(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	insertRunnerHistoricalFixture(t, database)
	manifest := approvedRunnerManifest(t, database)
	target := uint(138)
	g.Expect(database.runner.Run(context.Background(), RunOptions{
		Target:         &target,
		ExpandManifest: &manifest,
	})).To(Succeed())
	status, err := database.runner.Status(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(SchemaStatus{Version: 138, Dirty: false}))
	var kind string
	var executionCount, eventCount int64
	err = database.pool.QueryRow(context.Background(), `
SELECT transition_kind, transition_execution_count, transition_event_count
FROM ExternalExecutionTimestampExpandState
WHERE singleton`).Scan(&kind, &executionCount, &eventCount)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(kind).To(Equal("MANIFEST_REQUIRED"))
	g.Expect([]int64{executionCount, eventCount}).To(Equal([]int64{1, 1}))
}

func TestRunnerRefusesHistoricalMigrationBeforeEngineConstruction(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 137)
	insertRunnerHistoricalFixture(t, database)
	target := uint(138)
	err := database.runner.Run(context.Background(), RunOptions{Target: &target})
	g.Expect(err).To(MatchError(ContainSubstring("approved manifest is required")))
	status, statusErr := database.runner.Status(context.Background())
	g.Expect(statusErr).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(SchemaStatus{Version: 137, Dirty: false}))
	g.Expect(database.schemaMigrationsExists(t)).To(BeTrue())
	var expandTableExists bool
	statusErr = database.pool.QueryRow(context.Background(), `
SELECT to_regclass(format(
  '%I.externalexecutiontimestampmanifest', current_schema()
)) IS NOT NULL`).Scan(&expandTableExists)
	g.Expect(statusErr).NotTo(HaveOccurred())
	g.Expect(expandTableExists).To(BeFalse())
}

func TestRunnerAllowsPreapplyDownCrossing(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 138)
	target := uint(137)
	g.Expect(database.runner.Run(context.Background(), RunOptions{
		Target: &target,
	})).To(Succeed())
	status, err := database.runner.Status(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(SchemaStatus{Version: 137, Dirty: false}))
	var expandTableExists bool
	err = database.pool.QueryRow(context.Background(), `
SELECT to_regclass(format(
  '%I.externalexecutiontimestampmanifest', current_schema()
)) IS NOT NULL`).Scan(&expandTableExists)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(expandTableExists).To(BeFalse())
}

func TestRunnerExplicitTargetZeroDoesNotRunUp(t *testing.T) {
	g := NewWithT(t)
	database := newRunnerTestDatabase(t)
	database.migrateTo(t, 1)
	target := uint(0)
	g.Expect(database.runner.Run(context.Background(), RunOptions{
		Target: &target,
	})).To(Succeed())
	status, err := database.runner.Status(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(SchemaStatus{Version: 0, Dirty: false}))
	var helmColumnCount int
	err = database.pool.QueryRow(context.Background(), `
SELECT count(*)
FROM information_schema.columns
WHERE table_schema=current_schema()
  AND table_name='applicationversion'
  AND column_name='chart_type'`).Scan(&helmColumnCount)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(helmColumnCount).To(Equal(0))
}
