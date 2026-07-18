package db_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/gomega"
)

const task8ExecutionTimestampPairGuardError = "execution timestamp pair guard"

var (
	task4ExecutionOne = uuid.MustParse("00000000-0000-4000-8000-000000000001")
	task4ExecutionTwo = uuid.MustParse("00000000-0000-4000-8000-000000000002")
	task4EventOne     = uuid.MustParse("00000000-0000-4000-8000-000000000101")
)

type task4TestDatabase struct {
	ctx  context.Context
	pool *pgxpool.Pool
}

func newTask4TestDatabase(t *testing.T, version int, zone string) *task4TestDatabase {
	t.Helper()
	databaseURL := os.Getenv("DISTR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DISTR_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to task 4 database: %v", err)
	}
	t.Cleanup(admin.Close)

	schema := "timestamp_task4_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create task 4 schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(),
			"DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE"); err != nil {
			t.Logf("drop task 4 schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse task 4 database URL: %v", err)
	}
	config.MaxConns = 8
	config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		if _, err := connection.Exec(ctx, "SET search_path TO "+quotedSchema); err != nil {
			return err
		}
		_, err := connection.Exec(ctx, `SELECT set_config('TimeZone', $1, false)`, zone)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect to task 4 schema: %v", err)
	}
	t.Cleanup(pool.Close)
	task4ApplyMigrations(t, pool, -1, version)
	if _, err := pool.Exec(ctx, `
CREATE TABLE schema_migrations (
	version BIGINT PRIMARY KEY,
  dirty BOOLEAN NOT NULL
);`); err != nil {
		t.Fatalf("create task 4 migration state: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO schema_migrations (version, dirty) VALUES ($1, FALSE)`,
		version,
	); err != nil {
		t.Fatalf("insert task 4 migration state: %v", err)
	}
	return &task4TestDatabase{
		ctx:  internalctx.WithDb(ctx, pool),
		pool: pool,
	}
}

func task4ApplyMigrations(
	t *testing.T,
	pool *pgxpool.Pool,
	fromVersion int,
	toVersion int,
) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "migrations", "sql", "*.up.sql"))
	if err != nil {
		t.Fatalf("list task 4 migrations: %v", err)
	}
	sort.Slice(files, func(left, right int) bool {
		return task4MigrationVersion(t, files[left]) < task4MigrationVersion(t, files[right])
	})
	for _, file := range files {
		version := task4MigrationVersion(t, file)
		if version <= fromVersion || version > toVersion {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read task 4 migration %s: %v", file, err)
		}
		if _, err := pool.Exec(context.Background(), string(data)); err != nil {
			t.Fatalf("apply task 4 migration %s: %v", file, err)
		}
	}
}

func task4MigrationVersion(t *testing.T, file string) int {
	t.Helper()
	version, err := strconv.Atoi(strings.SplitN(filepath.Base(file), "_", 2)[0])
	if err != nil {
		t.Fatalf("parse task 4 migration version %s: %v", file, err)
	}
	return version
}

func (database *task4TestDatabase) migrateTo(t *testing.T) {
	t.Helper()
	const version = 138
	var current int
	if err := database.pool.QueryRow(context.Background(),
		`SELECT version FROM schema_migrations`).Scan(&current); err != nil {
		t.Fatalf("read current task 4 migration: %v", err)
	}
	task4ApplyMigrations(t, database.pool, current, version)
	if _, err := database.pool.Exec(context.Background(),
		`UPDATE schema_migrations SET version = $1, dirty = FALSE`, version); err != nil {
		t.Fatalf("advance task 4 migration state: %v", err)
	}
}

func task4DropFixtureForeignKeys(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
DO $$
DECLARE item RECORD;
BEGIN
  FOR item IN
    SELECT relation.relname AS table_name, constraint_row.conname
    FROM pg_constraint constraint_row
    JOIN pg_class relation ON relation.oid = constraint_row.conrelid
    WHERE relation.relnamespace = to_regnamespace(current_schema())
      AND relation.relname IN ('externalexecution', 'externalexecutionevent')
      AND constraint_row.contype = 'f'
  LOOP
    EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I',
      item.table_name, item.conname);
  END LOOP;
END $$`)
	if err != nil {
		t.Fatalf("drop task 4 fixture foreign keys: %v", err)
	}
}

func task4InsertFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	task4DropFixtureForeignKeys(t, pool)
	task4InsertExecution(t, pool, task4ExecutionOne,
		"2026-07-15T10:00:00.000001", "2026-07-15T10:01:00.000002",
		ptr("2026-07-15T10:02:00.000003"), ptr("2026-07-15T10:03:00.000004"),
		"2026-07-15T10:04:00.000005")
	task4InsertExecution(t, pool, task4ExecutionTwo,
		"2026-07-16T11:00:00.100001", "2026-07-16T11:01:00.100002",
		nil, nil, "2026-07-16T11:04:00.100005")
	_, err := pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionEvent (
  id, created_at, organization_id, external_execution_id,
  sequence, status, payload_hash
) VALUES (
  $1, $2::timestamp, $3, $4, 1, 'RUNNING', 'sha256:' || repeat('d', 64)
)`, task4EventOne, "2026-07-15T10:05:00.000006", uuid.New(), task4ExecutionOne)
	if err != nil {
		t.Fatalf("insert task 4 event fixture: %v", err)
	}
}

func task4InsertExecution(
	t *testing.T,
	pool *pgxpool.Pool,
	id uuid.UUID,
	createdAt string,
	updatedAt string,
	startedAt *string,
	completedAt *string,
	callbackDeadlineAt string,
) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
INSERT INTO ExternalExecution (
  id, created_at, updated_at, started_at, completed_at, callback_deadline_at,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum
) VALUES (
  $1, $2::timestamp, $3::timestamp, $4::timestamp, $5::timestamp, $6::timestamp,
  $7, $8, $9, $10, $11, $12, $13, $14,
  'api-image', 'sha256:' || repeat('a', 64), $15, 0, '1.0.0',
  'repo/image@sha256:' || repeat('b', 64), 'linux/amd64',
  'config:fixture', 'sha256:' || repeat('c', 64)
)`, id, createdAt, updatedAt, startedAt, completedAt, callbackDeadlineAt,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		uuid.New(), uuid.New(), "fixture-"+id.String())
	if err != nil {
		t.Fatalf("insert task 4 execution fixture: %v", err)
	}
}

func ptr[T any](value T) *T {
	return &value
}

func TestTimestampExpandReadinessCoreRunsOnSuppliedDirtyTransaction(t *testing.T) {
	g := NewWithT(t)
	database := newTask4TestDatabase(t, 138, "UTC")
	_, err := database.pool.Exec(
		context.Background(),
		`UPDATE schema_migrations SET dirty=TRUE`,
	)
	g.Expect(err).NotTo(HaveOccurred())
	tx, err := database.pool.BeginTx(
		context.Background(),
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
	)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = tx.Rollback(context.Background()) }()

	readiness, err := db.CheckExternalExecutionTimestampExpandReadinessInTx(
		context.Background(),
		tx,
		138,
		138,
		true,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(readiness.SchemaVersion).To(Equal(uint(138)))
	g.Expect(readiness.TransitionKind).To(Equal("ZERO_HISTORY"))

	mismatched, err := db.CheckExternalExecutionTimestampExpandReadinessInTx(
		context.Background(),
		tx,
		138,
		138,
		false,
	)
	g.Expect(err).To(MatchError(ContainSubstring(
		"expected version 138 dirty=false",
	)))
	g.Expect(mismatched).To(BeNil())
}

func TestInspectExternalExecutionTimestampsCompleteSnapshot(t *testing.T) {
	database := newTask4TestDatabase(t, 137, "UTC")
	task4InsertFixture(t, database.pool)
	g := NewWithT(t)

	manifest, err := db.InspectExternalExecutionTimestamps(database.ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(manifest.State).To(Equal(types.ExternalExecutionTimestampManifestStateDraft))
	g.Expect(manifest.SourceSchemaVersion).To(Equal(uint(137)))
	g.Expect(manifest.ExecutionCount).To(Equal(uint64(2)))
	g.Expect(manifest.EventCount).To(Equal(uint64(1)))
	g.Expect(manifest.RawCellCount).To(Equal(uint64(11)))
	g.Expect(manifest.PopulatedCellCount).To(Equal(uint64(9)))
	g.Expect(manifest.Cells).To(HaveLen(11))
	g.Expect(manifest.SnapshotStartedAt).NotTo(BeEmpty())
	g.Expect(manifest.SnapshotEndedAt).NotTo(BeEmpty())
	for _, cell := range manifest.Cells {
		g.Expect(cell.ConversionExpressionVersion).To(Equal(
			externalexecutiontimestamp.ConversionExpressionVersion,
		))
		g.Expect(cell.RawCellChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
		if cell.RawValue == nil {
			g.Expect(cell.Decision).To(Equal(types.ExternalExecutionTimestampDecisionNull))
		} else {
			g.Expect(*cell.RawValue).To(MatchRegexp(
				`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}$`,
			))
			g.Expect(cell.Decision).To(Equal(types.ExternalExecutionTimestampDecisionUnresolved))
		}
	}
	g.Expect(externalexecutiontimestamp.ValidateManifestDocument(*manifest)).To(BeEmpty())

	var expandObjectCount int
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT count(*) FROM (VALUES
  (to_regclass(format('%I.externalexecutiontimestampmanifest', current_schema()))),
  (to_regclass(format('%I.externalexecutiontimestampcellprovenance', current_schema()))),
  (to_regclass(format('%I.externalexecutiontimestampexpandstate', current_schema()))),
  (to_regclass(format('%I.externalexecutiontimestampcontractgate', current_schema())))
) object_name(name) WHERE name IS NOT NULL`).Scan(&expandObjectCount)).To(Succeed())
	g.Expect(expandObjectCount).To(Equal(0))
}

func TestInspectExternalExecutionTimestampsTimezoneInvariant(t *testing.T) {
	var rawSetChecksum, databaseIdentityChecksum string
	for index, zone := range []string{"UTC", "Asia/Bangkok", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			database := newTask4TestDatabase(t, 137, zone)
			task4InsertFixture(t, database.pool)
			manifest, err := db.InspectExternalExecutionTimestamps(database.ctx)
			g := NewWithT(t)
			g.Expect(err).NotTo(HaveOccurred())
			if index == 0 {
				rawSetChecksum = manifest.RawCellChecksum
				databaseIdentityChecksum = manifest.DatabaseIdentityChecksum
			}
			g.Expect(manifest.RawCellChecksum).To(Equal(rawSetChecksum))
			g.Expect(manifest.DatabaseIdentityChecksum).To(Equal(databaseIdentityChecksum))
		})
	}
}

func TestInspectExternalExecutionTimestampsRejectsInvalidMigrationState(t *testing.T) {
	tests := []struct {
		name    string
		version int
		mutate  func(*testing.T, *task4TestDatabase)
		want    string
	}{
		{name: "missing table", version: 137, mutate: func(t *testing.T, database *task4TestDatabase) {
			_, err := database.pool.Exec(context.Background(), `DROP TABLE schema_migrations`)
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
		}, want: "schema_migrations is absent"},
		{name: "missing row", version: 137, mutate: func(t *testing.T, database *task4TestDatabase) {
			_, err := database.pool.Exec(context.Background(), `DELETE FROM schema_migrations`)
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
		}, want: "exactly one row"},
		{name: "multiple rows", version: 137, mutate: func(t *testing.T, database *task4TestDatabase) {
			_, err := database.pool.Exec(context.Background(),
				`INSERT INTO schema_migrations (version, dirty) VALUES (138, FALSE)`)
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
		}, want: "exactly one row"},
		{name: "dirty", version: 137, mutate: func(t *testing.T, database *task4TestDatabase) {
			_, err := database.pool.Exec(context.Background(), `UPDATE schema_migrations SET dirty = TRUE`)
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
		}, want: "schema version 137 is dirty"},
		{name: "dirty NULL", version: 137, mutate: func(t *testing.T, database *task4TestDatabase) {
			_, err := database.pool.Exec(context.Background(), `
ALTER TABLE schema_migrations ALTER COLUMN dirty DROP NOT NULL;
UPDATE schema_migrations SET dirty = NULL`)
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
		}, want: "schema_migrations catalog must contain exactly"},
		{name: "unsupported 136", version: 136, want: "schema 136 is not supported"},
		{name: "malformed version", version: 137, mutate: func(t *testing.T, database *task4TestDatabase) {
			_, err := database.pool.Exec(context.Background(), `
ALTER TABLE schema_migrations ALTER COLUMN version TYPE TEXT USING version::text;
UPDATE schema_migrations SET version = '137'`)
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
		}, want: "schema_migrations catalog must contain exactly"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTask4TestDatabase(t, test.version, "UTC")
			if test.mutate != nil {
				test.mutate(t, database)
			}
			_, err := db.InspectExternalExecutionTimestamps(database.ctx)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(test.want)))
		})
	}
}

func TestInspectExternalExecutionTimestampsRejectsUnexpected137ExpandObject(t *testing.T) {
	database := newTask4TestDatabase(t, 137, "UTC")
	_, err := database.pool.Exec(context.Background(),
		`CREATE TABLE ExternalExecutionTimestampContractGate (id INTEGER)`)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	_, err = db.InspectExternalExecutionTimestamps(database.ctx)
	NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
		"schema 137 has unexpected expand objects",
	)))
}

func TestInspectExternalExecutionTimestampsRejectsMalformed137ShadowColumns(
	t *testing.T,
) {
	tests := []struct {
		name       string
		definition string
	}{
		{name: "text", definition: "TEXT"},
		{name: "timestamp", definition: "TIMESTAMP WITHOUT TIME ZONE"},
		{
			name:       "not nullable timestamptz",
			definition: "TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTask4TestDatabase(t, 137, "UTC")
			_, err := database.pool.Exec(context.Background(), fmt.Sprintf(
				"ALTER TABLE ExternalExecution ADD COLUMN created_at_instant %s",
				test.definition,
			))
			NewWithT(t).Expect(err).NotTo(HaveOccurred())

			_, err = db.InspectExternalExecutionTimestamps(database.ctx)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
				"schema 137 has unexpected expand objects",
			)))
		})
	}
}

func TestInspectExternalExecutionTimestampsValidatesComplete138Catalog(t *testing.T) {
	database := newTask4TestDatabase(t, 138, "UTC")
	g := NewWithT(t)
	manifest, err := db.InspectExternalExecutionTimestamps(database.ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(manifest.SourceSchemaVersion).To(Equal(uint(137)))

	for _, table := range []string{
		"ExternalExecutionTimestampManifest",
		"ExternalExecutionTimestampCellProvenance",
		"ExternalExecutionTimestampExpandState",
		"ExternalExecutionTimestampContractGate",
	} {
		t.Run("missing "+table, func(t *testing.T) {
			missingName := table + "Missing"
			_, err := database.pool.Exec(context.Background(), fmt.Sprintf(
				"ALTER TABLE %s RENAME TO %s", table, missingName,
			))
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
			t.Cleanup(func() {
				_, _ = database.pool.Exec(context.Background(), fmt.Sprintf(
					"ALTER TABLE %s RENAME TO %s", missingName, table,
				))
			})
			_, err = db.InspectExternalExecutionTimestamps(database.ctx)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
				"schema 138 is not expand-compatible",
			)))
		})
	}

	_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecution ALTER COLUMN created_at_instant SET NOT NULL`)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.InspectExternalExecutionTimestamps(database.ctx)
	g.Expect(err).To(MatchError(ContainSubstring("schema 138 is not expand-compatible")))
}

func TestInspectExternalExecutionTimestampsRequiresOrdinary138Tables(t *testing.T) {
	t.Run("view", func(t *testing.T) {
		database := newTask4TestDatabase(t, 138, "UTC")
		_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampExpandState
  RENAME TO ExternalExecutionTimestampExpandStateStorage;
CREATE VIEW ExternalExecutionTimestampExpandState AS
  SELECT * FROM ExternalExecutionTimestampExpandStateStorage`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())

		_, err = db.InspectExternalExecutionTimestamps(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
			"schema 138 is not expand-compatible",
		)))
	})

	t.Run("index", func(t *testing.T) {
		database := newTask4TestDatabase(t, 138, "UTC")
		_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampContractGate
  RENAME TO ExternalExecutionTimestampContractGateStorage;
CREATE INDEX ExternalExecutionTimestampContractGate
  ON ExternalExecution (id)`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())

		_, err = db.InspectExternalExecutionTimestamps(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
			"schema 138 is not expand-compatible",
		)))
	})
}

func TestInspectExternalExecutionTimestampsValidates138TableColumns(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "manifest nullability",
			sql: "ALTER TABLE ExternalExecutionTimestampManifest " +
				"ALTER COLUMN decision_content_checksum DROP NOT NULL",
		},
		{
			name: "manifest type",
			sql: "ALTER TABLE ExternalExecutionTimestampManifest " +
				"ALTER COLUMN tool_version TYPE VARCHAR(255)",
		},
		{
			name: "provenance nullability",
			sql: "ALTER TABLE ExternalExecutionTimestampCellProvenance " +
				"ALTER COLUMN raw_cell_checksum DROP NOT NULL",
		},
		{
			name: "expand state nullability",
			sql: "ALTER TABLE ExternalExecutionTimestampExpandState " +
				"ALTER COLUMN source_schema_version DROP NOT NULL",
		},
		{
			name: "contract gate nullability",
			sql: "ALTER TABLE ExternalExecutionTimestampContractGate " +
				"ALTER COLUMN backup_reference DROP NOT NULL",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTask4TestDatabase(t, 138, "UTC")
			_, err := database.pool.Exec(context.Background(), test.sql)
			NewWithT(t).Expect(err).NotTo(HaveOccurred())

			_, err = db.InspectExternalExecutionTimestamps(database.ctx)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
				"schema 138 is not expand-compatible",
			)))
		})
	}
}

func TestInspectExternalExecutionTimestampsValidates138TableConstraints(
	t *testing.T,
) {
	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "manifest",
			sql: `ALTER TABLE ExternalExecutionTimestampManifest
DROP CONSTRAINT externalexecutiontimestampmanifest_source_schema_137;
ALTER TABLE ExternalExecutionTimestampManifest
ADD CONSTRAINT externalexecutiontimestampmanifest_source_schema_137
CHECK (source_schema_version >= 137)`,
		},
		{
			name: "provenance",
			sql: `ALTER TABLE ExternalExecutionTimestampCellProvenance
DROP CONSTRAINT externalexecutiontimestampcell_raw_null_match;
ALTER TABLE ExternalExecutionTimestampCellProvenance
ADD CONSTRAINT externalexecutiontimestampcell_raw_null_match CHECK (TRUE)`,
		},
		{
			name: "expand state",
			sql: `ALTER TABLE ExternalExecutionTimestampExpandState
DROP CONSTRAINT externalexecutiontimestampexpandstate_cell_count;
ALTER TABLE ExternalExecutionTimestampExpandState
ADD CONSTRAINT externalexecutiontimestampexpandstate_cell_count
CHECK (transition_raw_cell_count >= 0)`,
		},
		{
			name: "contract gate",
			sql: `ALTER TABLE ExternalExecutionTimestampContractGate
DROP CONSTRAINT externalexecutiontimestampcontractgate_expiry;
ALTER TABLE ExternalExecutionTimestampContractGate
ADD CONSTRAINT externalexecutiontimestampcontractgate_expiry
CHECK (expires_at >= prepared_at)`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTask4TestDatabase(t, 138, "UTC")
			_, err := database.pool.Exec(context.Background(), test.sql)
			NewWithT(t).Expect(err).NotTo(HaveOccurred())

			_, err = db.InspectExternalExecutionTimestamps(database.ctx)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
				"schema 138 is not expand-compatible",
			)))
		})
	}
}

func TestInspectExternalExecutionTimestampsRejectsContractedShape(t *testing.T) {
	database := newTask4TestDatabase(t, 138, "UTC")
	_, err := database.pool.Exec(context.Background(), `
DROP TRIGGER ExternalExecution_timestamp_pair_guard
  ON ExternalExecution;
ALTER TABLE ExternalExecution
  ALTER COLUMN created_at DROP DEFAULT,
  ALTER COLUMN created_at TYPE TIMESTAMPTZ
    USING created_at AT TIME ZONE 'UTC'`)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	_, err = db.InspectExternalExecutionTimestamps(database.ctx)
	NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
		"legacy timestamp catalog has 5 of 6 required columns",
	)))
}

func TestInspectExternalExecutionTimestampsRequiresOneSource137Marker(t *testing.T) {
	database := newTask4TestDatabase(t, 138, "UTC")
	_, err := database.pool.Exec(context.Background(), `
DROP TRIGGER ExternalExecutionTimestampExpandState_append_only
  ON ExternalExecutionTimestampExpandState;
DELETE FROM ExternalExecutionTimestampExpandState`)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	_, err = db.InspectExternalExecutionTimestamps(database.ctx)
	NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
		"expand state must contain exactly one source version 137 row",
	)))
}

func TestInspectExternalExecutionTimestampsPreserves137IdentityAcross138(t *testing.T) {
	database := newTask4TestDatabase(t, 137, "UTC")
	task4InsertFixture(t, database.pool)
	g := NewWithT(t)
	before, err := db.InspectExternalExecutionTimestamps(database.ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(before.SourceSchemaVersion).To(Equal(uint(137)))

	database.migrateTo(t)
	after, err := db.InspectExternalExecutionTimestamps(database.ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(after.SourceSchemaVersion).To(Equal(uint(137)))
	g.Expect(after.RawCellChecksum).To(Equal(before.RawCellChecksum))
	g.Expect(after.DatabaseIdentityChecksum).To(Equal(before.DatabaseIdentityChecksum))

	report, err := db.ValidateExternalExecutionTimestampManifest(database.ctx, *before)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(report.SchemaVersion).To(Equal(uint(138)))
	g.Expect(report.RawCellCount).To(Equal(uint64(11)))
	g.Expect(report.UnresolvedCellCount).To(Equal(uint64(9)))

	var manifestRows, provenanceRows, contractGateRows, expandRows int
	g.Expect(database.pool.QueryRow(
		context.Background(), `
SELECT
  (SELECT count(*) FROM ExternalExecutionTimestampManifest),
  (SELECT count(*) FROM ExternalExecutionTimestampCellProvenance),
  (SELECT count(*) FROM ExternalExecutionTimestampContractGate),
	  (SELECT count(*) FROM ExternalExecutionTimestampExpandState)`,
	).Scan(&manifestRows, &provenanceRows, &contractGateRows, &expandRows)).To(Succeed())
	g.Expect([]int{manifestRows, provenanceRows, contractGateRows, expandRows}).To(
		Equal([]int{0, 0, 0, 1}),
	)
}

func TestValidateExternalExecutionTimestampManifestRejectsChildAtLiveBoundary(t *testing.T) {
	database := newTask4TestDatabase(t, 137, "UTC")
	task4InsertFixture(t, database.pool)
	root, err := db.InspectExternalExecutionTimestamps(database.ctx)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	child := *root
	child.ID = uuid.New()
	child.SupersedesManifestID = &root.ID
	child.DecisionContentChecksum, err = externalexecutiontimestamp.ComputeDecisionContentChecksum(child)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.ValidateExternalExecutionTimestampManifest(database.ctx, child)
	g.Expect(err).To(MatchError(ContainSubstring(
		"superseding manifest live validation requires verified-tip provenance",
	)))
}

func TestValidateExternalExecutionTimestampManifestRejectsChangedLiveRawCell(t *testing.T) {
	database := newTask4TestDatabase(t, 137, "UTC")
	task4InsertFixture(t, database.pool)
	manifest, err := db.InspectExternalExecutionTimestamps(database.ctx)
	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	changed := *manifest
	changed.Cells = append([]types.ExternalExecutionTimestampCellDecision(nil), manifest.Cells...)
	changed.Cells[0].RawValue = ptr("2026-07-15T09:59:59.999999")
	task4RecomputeManifest(t, &changed)
	g.Expect(externalexecutiontimestamp.ValidateManifestDocument(changed)).To(BeEmpty())

	_, err = db.ValidateExternalExecutionTimestampManifest(database.ctx, changed)
	g.Expect(err).To(MatchError(ContainSubstring(
		"manifest does not match the current raw snapshot",
	)))
}

func task4RecomputeManifest(
	t *testing.T,
	manifest *types.ExternalExecutionTimestampManifest,
) {
	t.Helper()
	g := NewWithT(t)
	rawCells := make([]types.ExternalExecutionTimestampRawCell, 0, len(manifest.Cells))
	executionIDs := make(map[uuid.UUID]struct{})
	eventIDs := make(map[uuid.UUID]struct{})
	for index := range manifest.Cells {
		raw := manifest.Cells[index].ExternalExecutionTimestampRawCell
		checksum, err := externalexecutiontimestamp.ComputeRawCellChecksum(raw)
		g.Expect(err).NotTo(HaveOccurred())
		manifest.Cells[index].RawCellChecksum = checksum
		raw = manifest.Cells[index].ExternalExecutionTimestampRawCell
		rawCells = append(rawCells, raw)
		if raw.SourceTable == "externalexecution" {
			executionIDs[raw.SourceRowID] = struct{}{}
		} else {
			eventIDs[raw.SourceRowID] = struct{}{}
		}
	}
	rawSet, err := externalexecutiontimestamp.ComputeRawSetChecksum(rawCells)
	g.Expect(err).NotTo(HaveOccurred())
	manifest.RawCellChecksum = rawSet
	executions := make([]uuid.UUID, 0, len(executionIDs))
	for id := range executionIDs {
		executions = append(executions, id)
	}
	events := make([]uuid.UUID, 0, len(eventIDs))
	for id := range eventIDs {
		events = append(events, id)
	}
	manifest.DatabaseIdentityChecksum, err = externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
		manifest.SourceSchemaVersion, executions, events,
		manifest.RawCellCount, manifest.RawCellChecksum,
	)
	g.Expect(err).NotTo(HaveOccurred())
	manifest.DecisionContentChecksum, err = externalexecutiontimestamp.ComputeDecisionContentChecksum(*manifest)
	g.Expect(err).NotTo(HaveOccurred())
}

type task4SnapshotBarrierPool struct {
	*pgxpool.Pool
	queryStarted chan struct{}
	updateDone   chan struct{}
	once         sync.Once
}

func (pool *task4SnapshotBarrierPool) BeginTx(
	ctx context.Context,
	options pgx.TxOptions,
) (pgx.Tx, error) {
	tx, err := pool.Pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &task4SnapshotBarrierTx{
		Tx: tx, queryStarted: pool.queryStarted,
		updateDone: pool.updateDone, once: &pool.once,
	}, nil
}

type task4SnapshotBarrierTx struct {
	pgx.Tx
	queryStarted chan struct{}
	updateDone   chan struct{}
	once         *sync.Once
}

func (tx *task4SnapshotBarrierTx) Query(
	ctx context.Context,
	sql string,
	args ...any,
) (pgx.Rows, error) {
	rows, err := tx.Tx.Query(ctx, sql, args...)
	if err == nil && strings.Contains(sql, "SELECT source_table, source_row_id") {
		tx.once.Do(func() { close(tx.queryStarted) })
		<-tx.updateDone
	}
	return rows, err
}

func TestInspectExternalExecutionTimestampsUsesOneConcurrentSnapshot(t *testing.T) {
	database := newTask4TestDatabase(t, 137, "UTC")
	task4InsertFixture(t, database.pool)
	barrier := &task4SnapshotBarrierPool{
		Pool: database.pool, queryStarted: make(chan struct{}), updateDone: make(chan struct{}),
	}
	ctx := internalctx.WithDb(context.Background(), barrier)
	type result struct {
		manifest *types.ExternalExecutionTimestampManifest
		err      error
	}
	resultChannel := make(chan result, 1)
	go func() {
		manifest, err := db.InspectExternalExecutionTimestamps(ctx)
		resultChannel <- result{manifest: manifest, err: err}
	}()
	<-barrier.queryStarted
	_, updateErr := database.pool.Exec(context.Background(), `
UPDATE ExternalExecution
SET created_at = TIMESTAMP '2030-01-01 00:00:00.000001'
WHERE id = $1`, task4ExecutionOne)
	close(barrier.updateDone)
	g := NewWithT(t)
	g.Expect(updateErr).NotTo(HaveOccurred())
	inspected := <-resultChannel
	g.Expect(inspected.err).NotTo(HaveOccurred())
	g.Expect(task4RawValue(inspected.manifest,
		"externalexecution", task4ExecutionOne, 1)).To(Equal(
		"2026-07-15T10:00:00.000001",
	))

	current, err := db.InspectExternalExecutionTimestamps(database.ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(task4RawValue(current,
		"externalexecution", task4ExecutionOne, 1)).To(Equal(
		"2030-01-01T00:00:00.000001",
	))
}

func task4RawValue(
	manifest *types.ExternalExecutionTimestampManifest,
	table string,
	rowID uuid.UUID,
	ordinal uint8,
) string {
	for _, cell := range manifest.Cells {
		if cell.SourceTable == table && cell.SourceRowID == rowID &&
			cell.ColumnOrdinal == ordinal && cell.RawValue != nil {
			return *cell.RawValue
		}
	}
	return ""
}

type task5TimestampFixture struct {
	Manifest     types.ExternalExecutionTimestampManifest
	ExecutionIDs []uuid.UUID
	EventIDs     []uuid.UUID
}

func createFiveExecutionTimestampFixture(
	t *testing.T,
	database *task4TestDatabase,
	statuses []types.ExternalExecutionStatus,
) task5TimestampFixture {
	t.Helper()
	g := NewWithT(t)
	g.Expect(statuses).To(HaveLen(5))
	task4DropFixtureForeignKeys(t, database.pool)

	fixture := task5TimestampFixture{
		ExecutionIDs: make([]uuid.UUID, 0, len(statuses)),
		EventIDs:     make([]uuid.UUID, 0, len(statuses)),
	}
	for index, status := range statuses {
		executionID := uuid.New()
		eventID := uuid.New()
		day := index + 10
		createdAt := fmt.Sprintf("2026-07-%02dT10:00:00.000001", day)
		updatedAt := fmt.Sprintf("2026-07-%02dT10:01:00.000002", day)
		startedAt := fmt.Sprintf("2026-07-%02dT10:02:00.000003", day)
		completedAt := fmt.Sprintf("2026-07-%02dT10:03:00.000004", day)
		deadlineAt := fmt.Sprintf("2026-07-%02dT10:04:00.000005", day)
		eventAt := fmt.Sprintf("2026-07-%02dT10:05:00.000006", day)
		task4InsertExecution(
			t, database.pool, executionID, createdAt, updatedAt,
			&startedAt, &completedAt, deadlineAt,
		)
		_, err := database.pool.Exec(context.Background(),
			`UPDATE ExternalExecution SET status=$2 WHERE id=$1`,
			executionID, status,
		)
		g.Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionEvent (
  id, created_at, organization_id, external_execution_id,
  sequence, status, payload_hash
) VALUES (
  $1, $2::timestamp, $3, $4, 1, $5, 'sha256:' || repeat('f', 64)
)`, eventID, eventAt, uuid.New(), executionID, status)
		g.Expect(err).NotTo(HaveOccurred())
		fixture.ExecutionIDs = append(fixture.ExecutionIDs, executionID)
		fixture.EventIDs = append(fixture.EventIDs, eventID)
	}

	draft, err := db.InspectExternalExecutionTimestamps(database.ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(draft.ExecutionCount).To(Equal(uint64(5)))
	g.Expect(draft.EventCount).To(Equal(uint64(5)))
	g.Expect(draft.RawCellCount).To(Equal(uint64(30)))
	g.Expect(draft.PopulatedCellCount).To(Equal(uint64(30)))
	fixture.Manifest = task5ApproveTimestampManifest(t, *draft, 18)
	return fixture
}

func task5ApproveTimestampManifest(
	t *testing.T,
	draft types.ExternalExecutionTimestampManifest,
	resolvedCount int,
) types.ExternalExecutionTimestampManifest {
	t.Helper()
	g := NewWithT(t)
	resolved := 0

	for index := range draft.Cells {
		if draft.Cells[index].RawValue == nil || resolved >= resolvedCount {
			continue
		}
		cell := &draft.Cells[index]
		g.Expect(cell.RawValue).NotTo(BeNil())
		offset := int32(0)
		converted, convertErr := externalexecutiontimestamp.ConvertWallClock(
			*cell.RawValue, offset,
		)
		g.Expect(convertErr).NotTo(HaveOccurred())
		convertedValue := externalexecutiontimestamp.FormatInstant(converted)
		cell.Decision = types.ExternalExecutionTimestampDecisionProven
		if resolved%2 == 1 {
			cell.Decision = types.ExternalExecutionTimestampDecisionAttested
		}
		cell.SourceZone = "UTC"
		cell.SourceOffsetSeconds = &offset
		cell.ConvertedValue = &convertedValue
		cell.EvidenceReference = fmt.Sprintf("evidence:fixture:%d", index)
		cell.EvidenceChecksum = "sha256:" + strings.Repeat("a", 64)
		cell.ApprovingIdentity = "timestamp-reviewer@example.test"
		resolved++
	}
	g.Expect(resolved).To(Equal(resolvedCount))
	approved, err := externalexecutiontimestamp.SealManifest(
		draft,
		types.ExternalExecutionTimestampSealOptions{
			AuthorIdentity:          "timestamp-author@example.test",
			ReviewerIdentity:        "timestamp-reviewer@example.test",
			EvidenceBundleReference: "evidence:fixture-bundle",
			EvidenceBundleChecksum:  "sha256:" + strings.Repeat("b", 64),
			TargetReleaseCommit:     strings.Repeat("c", 40),
			TargetImageDigest:       "sha256:" + strings.Repeat("d", 64),
		},
		time.Now().UTC().Add(-time.Minute),
	)
	g.Expect(err).NotTo(HaveOccurred())
	return approved
}

func task5Statuses() []types.ExternalExecutionStatus {
	return []types.ExternalExecutionStatus{
		types.ExternalExecutionStatusSucceeded,
		types.ExternalExecutionStatusSucceeded,
		types.ExternalExecutionStatusSucceeded,
		types.ExternalExecutionStatusSucceeded,
		types.ExternalExecutionStatusTimedOut,
	}
}

func task5CountLedgerAndShadowWrites(
	t *testing.T,
	database *task4TestDatabase,
) uint64 {
	t.Helper()
	var writes uint64
	err := database.pool.QueryRow(context.Background(), `
SELECT
  (SELECT count(*) FROM ExternalExecutionTimestampManifest) +
  (SELECT count(*) FROM ExternalExecutionTimestampCellProvenance) +
  (SELECT count(*) FROM ExternalExecution
   WHERE created_at_instant IS NOT NULL
      OR updated_at_instant IS NOT NULL
      OR started_at_instant IS NOT NULL
      OR completed_at_instant IS NOT NULL
      OR callback_deadline_at_instant IS NOT NULL) +
  (SELECT count(*) FROM ExternalExecutionEvent
   WHERE created_at_instant IS NOT NULL)`).Scan(&writes)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return writes
}

func task5TimestampExpandTablesExist(
	t *testing.T,
	database *task4TestDatabase,
) bool {
	t.Helper()
	var count int
	err := database.pool.QueryRow(context.Background(), `
SELECT count(*) FROM (VALUES
  (to_regclass(format('%I.externalexecutiontimestampmanifest', current_schema()))),
  (to_regclass(format('%I.externalexecutiontimestampcellprovenance', current_schema()))),
  (to_regclass(format('%I.externalexecutiontimestampexpandstate', current_schema()))),
  (to_regclass(format('%I.externalexecutiontimestampcontractgate', current_schema())))
) object_name(name) WHERE name IS NOT NULL`).Scan(&count)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return count != 0
}

func TestApplyExternalExecutionTimestampManifestDryRunFiveExecutionFixture(
	t *testing.T,
) {
	database := newTask4TestDatabase(t, 137, "UTC")
	fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
	database.migrateTo(t)
	g := NewWithT(t)

	report, err := db.ApplyExternalExecutionTimestampManifest(
		database.ctx,
		types.ExternalExecutionTimestampApplyRequest{
			Manifest: fixture.Manifest,
			Apply:    false,
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(report.DryRun).To(BeTrue())
	g.Expect(report.WouldPopulateCount).To(Equal(uint64(18)))
	g.Expect(report.UnresolvedCount).To(Equal(uint64(12)))
	g.Expect(task5CountLedgerAndShadowWrites(t, database)).To(Equal(uint64(0)))
}

func TestApplyExternalExecutionTimestampManifestDryRunSchema137WithoutExpandTables(
	t *testing.T,
) {
	database := newTask4TestDatabase(t, 137, "UTC")
	fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
	g := NewWithT(t)
	g.Expect(task5TimestampExpandTablesExist(t, database)).To(BeFalse())

	report, err := db.ApplyExternalExecutionTimestampManifest(
		database.ctx,
		types.ExternalExecutionTimestampApplyRequest{
			Manifest: fixture.Manifest,
			Apply:    false,
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(report.DryRun).To(BeTrue())
	g.Expect(report.WouldPopulateCount).To(Equal(uint64(18)))
	g.Expect(task5TimestampExpandTablesExist(t, database)).To(BeFalse())
}

func TestApplyExternalExecutionTimestampManifestRejectsPrepopulatedHistoricalShadow(
	t *testing.T,
) {
	for _, test := range []struct {
		name  string
		value func(types.ExternalExecutionTimestampCellDecision) string
	}{
		{
			name: "matching approved converted value",
			value: func(cell types.ExternalExecutionTimestampCellDecision) string {
				return *cell.ConvertedValue
			},
		},
		{
			name: "different converted value",
			value: func(types.ExternalExecutionTimestampCellDecision) string {
				return "2030-01-01T00:00:00.000000Z"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := newTask4TestDatabase(t, 137, "UTC")
			fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
			database.migrateTo(t)
			var resolved types.ExternalExecutionTimestampCellDecision
			for _, cell := range fixture.Manifest.Cells {
				if cell.Decision == types.ExternalExecutionTimestampDecisionProven ||
					cell.Decision == types.ExternalExecutionTimestampDecisionAttested {
					resolved = cell
					break
				}
			}
			g := NewWithT(t)
			g.Expect(resolved.SourceTable).NotTo(BeEmpty())
			g.Expect(resolved.ConvertedValue).NotTo(BeNil())
			task5CorruptShadowForCell(t, database, resolved, test.value(resolved))
			beforeWrites := task5ReadWriteState(t, database)
			beforeExpandState := task5ReadExpandStateSnapshot(t, database)

			report, err := db.ApplyExternalExecutionTimestampManifest(
				database.ctx, task5ApplyRequest(fixture.Manifest),
			)

			g.Expect(err).To(MatchError(ContainSubstring(
				"first manifest requires every historical timestamp shadow to be null",
			)))
			g.Expect(report).To(BeNil())
			g.Expect(task5ReadWriteState(t, database)).To(Equal(beforeWrites))
			g.Expect(task5ReadExpandStateSnapshot(t, database)).To(Equal(
				beforeExpandState,
			))
		})
	}
}

func TestApplyExternalExecutionTimestampManifestRequiresMigration138RootSnapshot(
	t *testing.T,
) {
	t.Run("pre-138 manifest is accepted after migration 138", func(t *testing.T) {
		database := newTask4TestDatabase(t, 137, "UTC")
		fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
		database.migrateTo(t)

		report, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(fixture.Manifest),
		)

		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(report).NotTo(BeNil())
		g.Expect(report.PopulatedShadowCount).To(Equal(uint64(18)))
	})

	t.Run("post-138 inspection is rejected without writes", func(t *testing.T) {
		database := newTask4TestDatabase(t, 137, "UTC")
		createFiveExecutionTimestampFixture(t, database, task5Statuses())
		database.migrateTo(t)
		draft, err := db.InspectExternalExecutionTimestamps(database.ctx)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		manifest := task5ApproveTimestampManifest(t, *draft, 18)
		beforeWrites := task5ReadWriteState(t, database)
		beforeExpandState := task5ReadExpandStateSnapshot(t, database)

		report, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(manifest),
		)

		g.Expect(err).To(MatchError(ContainSubstring(
			"first manifest snapshot must end at or before migration-138 transition",
		)))
		g.Expect(report).To(BeNil())
		g.Expect(task5ReadWriteState(t, database)).To(Equal(beforeWrites))
		g.Expect(task5ReadExpandStateSnapshot(t, database)).To(Equal(
			beforeExpandState,
		))
	})

	t.Run("zero-history transition rejects root without writes", func(t *testing.T) {
		database := newTask4TestDatabase(t, 138, "UTC")
		draft, err := db.InspectExternalExecutionTimestamps(database.ctx)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		manifest := task5ApproveTimestampManifest(t, *draft, 0)
		beforeWrites := task5ReadWriteState(t, database)
		beforeExpandState := task5ReadExpandStateSnapshot(t, database)

		report, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(manifest),
		)

		g.Expect(err).To(MatchError(ContainSubstring(
			"first manifest requires MANIFEST_REQUIRED expand state",
		)))
		g.Expect(report).To(BeNil())
		g.Expect(task5ReadWriteState(t, database)).To(Equal(beforeWrites))
		g.Expect(task5ReadExpandStateSnapshot(t, database)).To(Equal(
			beforeExpandState,
		))
	})

	t.Run("transition count mismatch rejects root without writes", func(t *testing.T) {
		database := newTask4TestDatabase(t, 137, "UTC")
		fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
		database.migrateTo(t)
		_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampExpandState
DISABLE TRIGGER ExternalExecutionTimestampExpandState_append_only;
UPDATE ExternalExecutionTimestampExpandState
SET transition_execution_count=transition_execution_count + 1,
    transition_raw_cell_count=transition_raw_cell_count + 5;
ALTER TABLE ExternalExecutionTimestampExpandState
ENABLE TRIGGER ExternalExecutionTimestampExpandState_append_only`)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		beforeWrites := task5ReadWriteState(t, database)
		beforeExpandState := task5ReadExpandStateSnapshot(t, database)

		report, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(fixture.Manifest),
		)

		g.Expect(err).To(MatchError(ContainSubstring(
			"first manifest counts differ from migration-138 transition counts",
		)))
		g.Expect(report).To(BeNil())
		g.Expect(task5ReadWriteState(t, database)).To(Equal(beforeWrites))
		g.Expect(task5ReadExpandStateSnapshot(t, database)).To(Equal(
			beforeExpandState,
		))
	})
}

func TestApplyExternalExecutionTimestampManifestRejectsUnexpectedBusinessTriggerBeforeWrites(
	t *testing.T,
) {
	for _, test := range []struct {
		name      string
		arrange   string
		wantError string
	}{
		{
			name:      "missing execution pair guard",
			arrange:   `DROP TRIGGER ExternalExecution_timestamp_pair_guard ON ExternalExecution`,
			wantError: task8ExecutionTimestampPairGuardError,
		},
		{
			name: "disabled event pair guard",
			arrange: `ALTER TABLE ExternalExecutionEvent
DISABLE TRIGGER ExternalExecutionEvent_timestamp_pair_guard`,
			wantError: "event timestamp pair guard",
		},
		{
			name: "extra later sorting execution trigger",
			arrange: `
CREATE FUNCTION z_task5_corrupt_execution_timestamp_pair()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at_instant := NEW.updated_at_instant + INTERVAL '1 hour';
  RETURN NEW;
END;
$$;
CREATE TRIGGER z_task5_corrupt_execution_timestamp_pair
BEFORE UPDATE OF updated_at, updated_at_instant ON ExternalExecution
FOR EACH ROW EXECUTE FUNCTION z_task5_corrupt_execution_timestamp_pair()`,
			wantError: "externalexecution non-internal trigger set",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := newTask4TestDatabase(t, 137, "UTC")
			fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
			database.migrateTo(t)
			_, err := database.pool.Exec(context.Background(), test.arrange)
			g := NewWithT(t)
			g.Expect(err).NotTo(HaveOccurred())
			beforeWrites := task5ReadWriteState(t, database)
			beforeExpandState := task5ReadExpandStateSnapshot(t, database)

			report, err := db.ApplyExternalExecutionTimestampManifest(
				database.ctx, task5ApplyRequest(fixture.Manifest),
			)

			g.Expect(err).To(MatchError(ContainSubstring(test.wantError)))
			g.Expect(report).To(BeNil())
			g.Expect(task5ReadWriteState(t, database)).To(Equal(beforeWrites))
			g.Expect(task5ReadExpandStateSnapshot(t, database)).To(Equal(
				beforeExpandState,
			))
		})
	}
}

func TestApplyExternalExecutionTimestampManifestFirstRootPreflightRejectsFilledShadow(
	t *testing.T,
) {
	database := newTask4TestDatabase(t, 137, "UTC")
	fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
	database.migrateTo(t)
	var unresolved types.ExternalExecutionTimestampCellDecision
	for _, cell := range fixture.Manifest.Cells {
		if cell.Decision == types.ExternalExecutionTimestampDecisionUnresolved {
			unresolved = cell
			break
		}
	}
	NewWithT(t).Expect(unresolved.SourceTable).NotTo(BeEmpty())
	task5CorruptShadowForCell(
		t, database, unresolved, task5UTCShadowValueForCell(t, unresolved),
	)
	before := task5ReadWriteState(t, database)

	_, err := db.ApplyExternalExecutionTimestampManifest(
		database.ctx,
		types.ExternalExecutionTimestampApplyRequest{Manifest: fixture.Manifest},
	)

	g := NewWithT(t)
	g.Expect(err).To(MatchError(ContainSubstring(
		"first manifest requires every historical timestamp shadow to be null",
	)))
	g.Expect(task5ReadWriteState(t, database)).To(Equal(before))
}

func task5ApplyRequest(
	manifest types.ExternalExecutionTimestampManifest,
) types.ExternalExecutionTimestampApplyRequest {
	return types.ExternalExecutionTimestampApplyRequest{
		Manifest:                     manifest,
		Apply:                        true,
		WriterFenceIdentifier:        "fence:fixture-42",
		BackupReference:              "backup:postgres-42",
		BackupChecksum:               "sha256:" + strings.Repeat("d", 64),
		RestoreVerificationReference: "restore:fixture-42",
		RestoreVerificationChecksum:  "sha256:" + strings.Repeat("e", 64),
	}
}

type task5WriteState struct {
	ManifestRows   uint64
	ProvenanceRows uint64
	ShadowCells    uint64
}

func task5ReadWriteState(
	t *testing.T,
	database *task4TestDatabase,
) task5WriteState {
	t.Helper()
	state := task5WriteState{}
	err := database.pool.QueryRow(context.Background(), `
SELECT
  (SELECT count(*) FROM ExternalExecutionTimestampManifest),
  (SELECT count(*) FROM ExternalExecutionTimestampCellProvenance),
  (SELECT COALESCE(sum(
      (created_at_instant IS NOT NULL)::int +
      (updated_at_instant IS NOT NULL)::int +
      (started_at_instant IS NOT NULL)::int +
      (completed_at_instant IS NOT NULL)::int +
      (callback_deadline_at_instant IS NOT NULL)::int
    ), 0) FROM ExternalExecution) +
  (SELECT count(*) FROM ExternalExecutionEvent
   WHERE created_at_instant IS NOT NULL)`).Scan(
		&state.ManifestRows,
		&state.ProvenanceRows,
		&state.ShadowCells,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return state
}

func task5ReadExpandStateSnapshot(
	t *testing.T,
	database *task4TestDatabase,
) string {
	t.Helper()
	var snapshot string
	err := database.pool.QueryRow(context.Background(), `
SELECT COALESCE(
  jsonb_agg(to_jsonb(expand_state) ORDER BY expand_state.singleton),
  '[]'::jsonb
)::text
FROM ExternalExecutionTimestampExpandState expand_state`).Scan(&snapshot)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return snapshot
}

func task5ExpectNoSessionAdvisoryLock(
	t *testing.T,
	database *task4TestDatabase,
) {
	t.Helper()
	g := NewWithT(t)
	acquireContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connections := make([]*pgxpool.Conn, 0, database.pool.Config().MaxConns)
	defer func() {
		for _, connection := range connections {
			connection.Release()
		}
	}()
	for range database.pool.Config().MaxConns {
		connection, err := database.pool.Acquire(acquireContext)
		g.Expect(err).NotTo(HaveOccurred())
		connections = append(connections, connection)
	}
	for _, connection := range connections {
		var unexpectedlyUnlocked bool
		err := connection.QueryRow(
			acquireContext,
			`SELECT pg_advisory_unlock($1)`,
			externalexecutiontimestamp.MigrationAdvisoryLockKey,
		).Scan(&unexpectedlyUnlocked)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(unexpectedlyUnlocked).To(BeFalse())
	}
}

func task5ReadNonTimestampSnapshot(
	t *testing.T,
	database *task4TestDatabase,
) []string {
	t.Helper()
	rows, err := database.pool.Query(context.Background(), `
SELECT kind || '/' || row_id::text || '/' || payload
FROM (
  SELECT 'execution'::text AS kind, execution.id AS row_id,
    (to_jsonb(execution) - ARRAY[
      'created_at', 'updated_at', 'started_at', 'completed_at',
      'callback_deadline_at', 'created_at_instant', 'updated_at_instant',
      'started_at_instant', 'completed_at_instant',
      'callback_deadline_at_instant'
    ])::text AS payload
  FROM ExternalExecution execution
  UNION ALL
  SELECT 'event'::text, event.id,
    (to_jsonb(event) - ARRAY['created_at', 'created_at_instant'])::text
  FROM ExternalExecutionEvent event
) rows
ORDER BY kind, row_id`)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	values, err := pgx.CollectRows(rows, pgx.RowTo[string])
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return values
}

func task5RefreshDecisionChecksum(
	t *testing.T,
	manifest *types.ExternalExecutionTimestampManifest,
) {
	t.Helper()
	checksum, err := externalexecutiontimestamp.ComputeDecisionContentChecksum(*manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	manifest.DecisionContentChecksum = checksum
}

func TestApplyExternalExecutionTimestampManifestAtomic(t *testing.T) {
	database := newTask4TestDatabase(t, 137, "UTC")
	fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
	database.migrateTo(t)
	g := NewWithT(t)
	beforeNonTimestamp := task5ReadNonTimestampSnapshot(t, database)
	beforeRawSet, err := db.InspectExternalExecutionTimestamps(database.ctx)
	g.Expect(err).NotTo(HaveOccurred())

	report, err := db.ApplyExternalExecutionTimestampManifest(
		database.ctx,
		task5ApplyRequest(fixture.Manifest),
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(report.DryRun).To(BeFalse())
	g.Expect(report.Idempotent).To(BeFalse())
	g.Expect(report.ProvenanceRows).To(Equal(uint64(30)))
	g.Expect(report.WouldPopulateCount).To(Equal(uint64(18)))
	g.Expect(report.PopulatedShadowCount).To(Equal(uint64(18)))
	g.Expect(task5ReadWriteState(t, database)).To(Equal(task5WriteState{
		ManifestRows: 1, ProvenanceRows: 30, ShadowCells: 18,
	}))
	g.Expect(task5ReadNonTimestampSnapshot(t, database)).To(Equal(beforeNonTimestamp))
	afterRawSet, err := db.InspectExternalExecutionTimestamps(database.ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(afterRawSet.RawCellChecksum).To(Equal(beforeRawSet.RawCellChecksum))
	var state types.ExternalExecutionTimestampManifestState
	var appliedAt *time.Time
	var verifiedAt *time.Time
	g.Expect(database.pool.QueryRow(context.Background(), `
	SELECT state, applied_at, verified_at
	FROM ExternalExecutionTimestampManifest WHERE id=$1`,
		fixture.Manifest.ID,
	).Scan(&state, &appliedAt, &verifiedAt)).To(Succeed())
	g.Expect(state).To(Equal(types.ExternalExecutionTimestampManifestStateVerified))
	g.Expect(appliedAt).NotTo(BeNil())
	g.Expect(verifiedAt).NotTo(BeNil())
	g.Expect(appliedAt.Before(*verifiedAt)).To(BeTrue())
	task5ExpectNoSessionAdvisoryLock(t, database)
}

func TestApplyExternalExecutionTimestampManifestFillsHistoricalLifecycleOffsets(
	t *testing.T,
) {
	database := newTask4TestDatabase(t, 137, "UTC")
	fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
	g := NewWithT(t)
	offset := int32(7 * 60 * 60)
	selected := make(map[string]types.ExternalExecutionTimestampCellDecision)
	for index := range fixture.Manifest.Cells {
		cell := &fixture.Manifest.Cells[index]
		if cell.SourceTable != "externalexecution" ||
			(cell.SourceColumn != "started_at" && cell.SourceColumn != "completed_at") ||
			(cell.Decision != types.ExternalExecutionTimestampDecisionProven &&
				cell.Decision != types.ExternalExecutionTimestampDecisionAttested) {
			continue
		}
		if _, exists := selected[cell.SourceColumn]; exists {
			continue
		}
		converted, err := externalexecutiontimestamp.ConvertWallClock(
			*cell.RawValue,
			offset,
		)
		g.Expect(err).NotTo(HaveOccurred())
		convertedValue := externalexecutiontimestamp.FormatInstant(converted)
		cell.SourceZone = "Asia/Bangkok"
		cell.SourceOffsetSeconds = &offset
		cell.ConvertedValue = &convertedValue
		selected[cell.SourceColumn] = *cell
	}
	g.Expect(selected).To(HaveLen(2))
	task5RefreshDecisionChecksum(t, &fixture.Manifest)
	database.migrateTo(t)

	report, err := db.ApplyExternalExecutionTimestampManifest(
		database.ctx,
		task5ApplyRequest(fixture.Manifest),
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(report.PopulatedShadowCount).To(Equal(uint64(18)))
	for column, cell := range selected {
		var rawUnchanged, shadowMatches bool
		err = database.pool.QueryRow(context.Background(), fmt.Sprintf(`
SELECT %s = $2::timestamp, %s_instant = $3::timestamptz
FROM ExternalExecution WHERE id = $1`, column, column),
			cell.SourceRowID, *cell.RawValue, *cell.ConvertedValue,
		).Scan(&rawUnchanged, &shadowMatches)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(rawUnchanged).To(BeTrue())
		g.Expect(shadowMatches).To(BeTrue())
	}
	_, err = db.VerifyExternalExecutionTimestampManifest(
		database.ctx,
		fixture.Manifest.ID,
	)
	g.Expect(err).NotTo(HaveOccurred())
}

func TestApplyExternalExecutionTimestampManifestTakesFreshSnapshotAfterWriter(
	t *testing.T,
) {
	database := newTask4TestDatabase(t, 137, "UTC")
	fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
	database.migrateTo(t)
	g := NewWithT(t)

	writerConnection, err := database.pool.Acquire(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer writerConnection.Release()
	writer, err := writerConnection.Begin(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = writer.Rollback(context.Background()) }()
	_, err = writer.Exec(context.Background(), `
INSERT INTO ExternalExecutionEvent (
  id, created_at, created_at_instant, organization_id,
  external_execution_id, sequence, status, payload_hash
) VALUES (
  $1, TIMESTAMP '2026-07-20 12:00:00.000001',
  TIMESTAMPTZ '2026-07-20 12:00:00.000001+00', $2,
  $3, 99, 'RUNNING', 'sha256:' || repeat('8', 64)
)`, uuid.New(), uuid.New(), fixture.ExecutionIDs[0])
	g.Expect(err).NotTo(HaveOccurred())

	type applyResult struct {
		report *types.ExternalExecutionTimestampApplyReport
		err    error
	}
	resultChannel := make(chan applyResult, 1)
	go func() {
		report, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			task5ApplyRequest(fixture.Manifest),
		)
		resultChannel <- applyResult{report: report, err: err}
	}()

	waitContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err = database.pool.QueryRow(waitContext, `
SELECT EXISTS (
  SELECT 1
  FROM pg_stat_activity
  WHERE datname = current_database()
    AND pid <> pg_backend_pid()
    AND query LIKE '%LOCK TABLE ExternalExecution, ExternalExecutionEvent%'
    AND wait_event_type = 'Lock'
)`).Scan(&waiting)
		g.Expect(err).NotTo(HaveOccurred())
		if waiting {
			break
		}
		select {
		case <-waitContext.Done():
			t.Fatalf("apply did not reach the table-lock wait: %v", waitContext.Err())
		case <-ticker.C:
		}
	}

	g.Expect(writer.Commit(context.Background())).To(Succeed())
	result := <-resultChannel
	g.Expect(result.err).To(MatchError(ContainSubstring("current raw snapshot")))
	g.Expect(result.report).To(BeNil())
	g.Expect(task5ReadWriteState(t, database)).To(Equal(task5WriteState{
		ShadowCells: 1,
	}))
}

func TestApplyExternalExecutionTimestampManifestSessionLockContentionIsBounded(
	t *testing.T,
) {
	database := newTask4TestDatabase(t, 137, "UTC")
	fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
	database.migrateTo(t)
	g := NewWithT(t)

	holder, err := database.pool.Acquire(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer holder.Release()
	var locked bool
	err = holder.QueryRow(
		context.Background(),
		`SELECT pg_try_advisory_lock($1)`,
		externalexecutiontimestamp.MigrationAdvisoryLockKey,
	).Scan(&locked)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(locked).To(BeTrue())

	applyContext, cancel := context.WithTimeout(database.ctx, 250*time.Millisecond)
	startedAt := time.Now()
	_, err = db.ApplyExternalExecutionTimestampManifest(
		applyContext,
		task5ApplyRequest(fixture.Manifest),
	)
	cancel()
	g.Expect(err).To(MatchError(ContainSubstring("context deadline exceeded")))
	g.Expect(time.Since(startedAt)).To(BeNumerically("<", 2*time.Second))
	g.Expect(task5ReadWriteState(t, database)).To(Equal(task5WriteState{}))

	var unlocked bool
	err = holder.QueryRow(
		context.Background(),
		`SELECT pg_advisory_unlock($1)`,
		externalexecutiontimestamp.MigrationAdvisoryLockKey,
	).Scan(&unlocked)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(unlocked).To(BeTrue())
	holder.Release()
	task5ExpectNoSessionAdvisoryLock(t, database)
}

func TestApplyExternalExecutionTimestampManifestCancellationDoesNotLeakSessionLock(
	t *testing.T,
) {
	database := newTask4TestDatabase(t, 137, "UTC")
	fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
	database.migrateTo(t)
	g := NewWithT(t)

	writerConnection, err := database.pool.Acquire(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer writerConnection.Release()
	writer, err := writerConnection.Begin(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = writer.Rollback(context.Background()) }()
	_, err = writer.Exec(
		context.Background(),
		`LOCK TABLE ExternalExecution IN ROW EXCLUSIVE MODE`,
	)
	g.Expect(err).NotTo(HaveOccurred())

	applyContext, cancelApply := context.WithCancel(database.ctx)
	errChannel := make(chan error, 1)
	go func() {
		_, err := db.ApplyExternalExecutionTimestampManifest(
			applyContext,
			task5ApplyRequest(fixture.Manifest),
		)
		errChannel <- err
	}()

	waitContext, cancelWait := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelWait()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err = database.pool.QueryRow(waitContext, `
SELECT EXISTS (
  SELECT 1 FROM pg_stat_activity
  WHERE datname = current_database()
    AND pid <> pg_backend_pid()
    AND query LIKE '%LOCK TABLE ExternalExecution, ExternalExecutionEvent%'
    AND wait_event_type = 'Lock'
)`).Scan(&waiting)
		g.Expect(err).NotTo(HaveOccurred())
		if waiting {
			break
		}
		select {
		case <-waitContext.Done():
			t.Fatalf("apply did not reach the table-lock wait: %v", waitContext.Err())
		case <-ticker.C:
		}
	}

	cancelApply()
	g.Expect(<-errChannel).To(MatchError(ContainSubstring("context canceled")))
	g.Expect(writer.Rollback(context.Background())).To(Succeed())
	writerConnection.Release()
	g.Expect(task5ReadWriteState(t, database)).To(Equal(task5WriteState{}))
	task5ExpectNoSessionAdvisoryLock(t, database)
}

func TestApplyExternalExecutionTimestampManifestRollsBack(t *testing.T) {
	type mutateFunc func(
		*testing.T,
		*types.ExternalExecutionTimestampApplyRequest,
		*task4TestDatabase,
		task5TimestampFixture,
	)
	tests := []struct {
		name      string
		mutate    mutateFunc
		wantError string
	}{
		{
			name: "raw changed",
			mutate: func(t *testing.T, _ *types.ExternalExecutionTimestampApplyRequest,
				database *task4TestDatabase, fixture task5TimestampFixture,
			) {
				task5ExecWithPairGuardDisabled(t, database, "externalexecution", `
UPDATE ExternalExecution
SET created_at=TIMESTAMP '2030-01-01 00:00:00.000001'
WHERE id=$1`, fixture.ExecutionIDs[0])
			},
			wantError: "current raw snapshot",
		},
		{
			name: "missing decision",
			mutate: func(_ *testing.T, request *types.ExternalExecutionTimestampApplyRequest,
				_ *task4TestDatabase, _ task5TimestampFixture,
			) {
				request.Manifest.Cells = request.Manifest.Cells[:len(request.Manifest.Cells)-1]
			},
			wantError: "document has",
		},
		{
			name: "extra decision",
			mutate: func(_ *testing.T, request *types.ExternalExecutionTimestampApplyRequest,
				_ *task4TestDatabase, _ task5TimestampFixture,
			) {
				extra := request.Manifest.Cells[0]
				extra.SourceRowID = uuid.New()
				request.Manifest.Cells = append(request.Manifest.Cells, extra)
			},
			wantError: "document has",
		},
		{
			name: "wrong conversion",
			mutate: func(t *testing.T, request *types.ExternalExecutionTimestampApplyRequest,
				_ *task4TestDatabase, _ task5TimestampFixture,
			) {
				wrong := "2030-01-01T00:00:00.000000Z"
				request.Manifest.Cells[0].ConvertedValue = &wrong
				task5RefreshDecisionChecksum(t, &request.Manifest)
			},
			wantError: "does not reproduce",
		},
		{
			name: "missing fence",
			mutate: func(_ *testing.T, request *types.ExternalExecutionTimestampApplyRequest,
				_ *task4TestDatabase, _ task5TimestampFixture,
			) {
				request.WriterFenceIdentifier = ""
			},
			wantError: "writer fence is required",
		},
		{
			name: "missing backup",
			mutate: func(_ *testing.T, request *types.ExternalExecutionTimestampApplyRequest,
				_ *task4TestDatabase, _ task5TimestampFixture,
			) {
				request.BackupReference = ""
			},
			wantError: "backup reference is required",
		},
		{
			name: "missing restore",
			mutate: func(_ *testing.T, request *types.ExternalExecutionTimestampApplyRequest,
				_ *task4TestDatabase, _ task5TimestampFixture,
			) {
				request.RestoreVerificationReference = ""
			},
			wantError: "restore reference is required",
		},
		{
			name: "conflicting shadow",
			mutate: func(t *testing.T, _ *types.ExternalExecutionTimestampApplyRequest,
				database *task4TestDatabase, fixture task5TimestampFixture,
			) {
				task5ExecWithPairGuardDisabled(t, database, "externalexecution", `
UPDATE ExternalExecution
SET created_at_instant=TIMESTAMPTZ '2030-01-01 00:00:00+00'
WHERE id=$1`, fixture.ExecutionIDs[0])
			},
			wantError: "shadow",
		},
		{
			name: "concurrent writer",
			mutate: func(t *testing.T, _ *types.ExternalExecutionTimestampApplyRequest,
				database *task4TestDatabase, fixture task5TimestampFixture,
			) {
				task5ExecWithPairGuardDisabled(t, database, "externalexecutionevent", `
INSERT INTO ExternalExecutionEvent (
  id, created_at, created_at_instant, organization_id,
  external_execution_id, sequence, status, payload_hash
) VALUES (
  $1, TIMESTAMP '2026-07-20 12:00:00.000001', NULL, $2,
  $3, 2, 'RUNNING', 'sha256:' || repeat('9', 64)
)`, uuid.New(), uuid.New(), fixture.ExecutionIDs[0])
			},
			wantError: "current raw snapshot",
		},
		{
			name: "schema 138 recomputed as catalog version",
			mutate: func(t *testing.T, request *types.ExternalExecutionTimestampApplyRequest,
				_ *task4TestDatabase, _ task5TimestampFixture,
			) {
				request.Manifest.SourceSchemaVersion = 138
				task4RecomputeManifest(t, &request.Manifest)
			},
			wantError: "source schema version must be 137",
		},
		{
			name: "missing target commit",
			mutate: func(_ *testing.T, request *types.ExternalExecutionTimestampApplyRequest,
				_ *task4TestDatabase, _ task5TimestampFixture,
			) {
				request.Manifest.TargetReleaseCommit = ""
			},
			wantError: "target release commit",
		},
		{
			name: "missing target image digest",
			mutate: func(_ *testing.T, request *types.ExternalExecutionTimestampApplyRequest,
				_ *task4TestDatabase, _ task5TimestampFixture,
			) {
				request.Manifest.TargetImageDigest = ""
			},
			wantError: "target image digest",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTask4TestDatabase(t, 137, "UTC")
			fixture := createFiveExecutionTimestampFixture(
				t, database, task5Statuses(),
			)
			database.migrateTo(t)
			request := task5ApplyRequest(fixture.Manifest)
			test.mutate(t, &request, database, fixture)
			before := task5ReadWriteState(t, database)
			beforeNonTimestamp := task5ReadNonTimestampSnapshot(t, database)

			_, err := db.ApplyExternalExecutionTimestampManifest(database.ctx, request)

			g := NewWithT(t)
			g.Expect(err).To(MatchError(ContainSubstring(test.wantError)))
			g.Expect(task5ReadWriteState(t, database)).To(Equal(before))
			g.Expect(task5ReadNonTimestampSnapshot(t, database)).To(Equal(
				beforeNonTimestamp,
			))
		})
	}
}

func TestApplyExternalExecutionTimestampManifestRollsBackLateVerifiedTransition(
	t *testing.T,
) {
	database := newTask4TestDatabase(t, 137, "UTC")
	fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
	database.migrateTo(t)
	g := NewWithT(t)
	_, err := database.pool.Exec(context.Background(), `
CREATE FUNCTION reject_task5_verified_transition()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.state = 'APPLIED' AND NEW.state = 'VERIFIED' THEN
    RAISE EXCEPTION 'injected late verified transition failure';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER reject_task5_verified_transition
BEFORE UPDATE ON ExternalExecutionTimestampManifest
FOR EACH ROW EXECUTE FUNCTION reject_task5_verified_transition()`)
	g.Expect(err).NotTo(HaveOccurred())
	beforeWrites := task5ReadWriteState(t, database)
	beforeBusinessFields := task5ReadNonTimestampSnapshot(t, database)

	_, err = db.ApplyExternalExecutionTimestampManifest(
		database.ctx,
		task5ApplyRequest(fixture.Manifest),
	)

	g.Expect(err).To(MatchError(ContainSubstring(
		"injected late verified transition failure",
	)))
	g.Expect(task5ReadWriteState(t, database)).To(Equal(beforeWrites))
	g.Expect(task5ReadNonTimestampSnapshot(t, database)).To(Equal(
		beforeBusinessFields,
	))
}

func task5CloneManifest(
	manifest types.ExternalExecutionTimestampManifest,
) types.ExternalExecutionTimestampManifest {
	cloned := manifest
	if manifest.SupersedesManifestID != nil {
		parent := *manifest.SupersedesManifestID
		cloned.SupersedesManifestID = &parent
	}
	cloned.Cells = make([]types.ExternalExecutionTimestampCellDecision, len(manifest.Cells))
	copy(cloned.Cells, manifest.Cells)
	for index := range cloned.Cells {
		if manifest.Cells[index].RawValue != nil {
			value := *manifest.Cells[index].RawValue
			cloned.Cells[index].RawValue = &value
		}
		if manifest.Cells[index].SourceOffsetSeconds != nil {
			value := *manifest.Cells[index].SourceOffsetSeconds
			cloned.Cells[index].SourceOffsetSeconds = &value
		}
		if manifest.Cells[index].ConvertedValue != nil {
			value := *manifest.Cells[index].ConvertedValue
			cloned.Cells[index].ConvertedValue = &value
		}
	}
	return cloned
}

func task5DraftRevision(
	t *testing.T,
	previous types.ExternalExecutionTimestampManifest,
	parentID *uuid.UUID,
) types.ExternalExecutionTimestampManifest {
	t.Helper()
	draft := task5CloneManifest(previous)
	draft.ID = uuid.New()
	draft.SupersedesManifestID = parentID
	draft.EvidenceBundleReference = ""
	draft.EvidenceBundleChecksum = ""
	draft.AuthorIdentity = ""
	draft.ReviewerIdentity = ""
	draft.ApprovedAt = ""
	draft.TargetReleaseCommit = ""
	draft.TargetImageDigest = ""
	draft.State = types.ExternalExecutionTimestampManifestStateDraft
	task5RefreshDecisionChecksum(t, &draft)
	return draft
}

func task5SealRevision(
	t *testing.T,
	draft types.ExternalExecutionTimestampManifest,
) types.ExternalExecutionTimestampManifest {
	t.Helper()
	sealed, err := externalexecutiontimestamp.SealManifest(
		draft,
		types.ExternalExecutionTimestampSealOptions{
			AuthorIdentity:          "timestamp-author-v2@example.test",
			ReviewerIdentity:        "timestamp-reviewer-v2@example.test",
			EvidenceBundleReference: "evidence:fixture-bundle-v2",
			EvidenceBundleChecksum:  "sha256:" + strings.Repeat("4", 64),
			TargetReleaseCommit:     strings.Repeat("5", 40),
			TargetImageDigest:       "sha256:" + strings.Repeat("6", 64),
		},
		time.Now().UTC().Add(-time.Millisecond),
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return sealed
}

func task5PromoteFirstUnresolvedCellAs(
	t *testing.T,
	manifest *types.ExternalExecutionTimestampManifest,
	skipKey string,
	decision types.ExternalExecutionTimestampDecision,
) string {
	t.Helper()
	for index := range manifest.Cells {
		cell := &manifest.Cells[index]
		key := timestampTestCellKey(*cell)
		if cell.Decision != types.ExternalExecutionTimestampDecisionUnresolved ||
			key == skipKey {
			continue
		}
		offset := int32(0)
		converted, err := externalexecutiontimestamp.ConvertWallClock(
			*cell.RawValue, offset,
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		convertedValue := externalexecutiontimestamp.FormatInstant(converted)
		cell.Decision = decision
		cell.SourceZone = "UTC"
		cell.SourceOffsetSeconds = &offset
		cell.ConvertedValue = &convertedValue
		cell.EvidenceReference = "evidence:superseding-resolution"
		cell.EvidenceChecksum = "sha256:" + strings.Repeat("7", 64)
		cell.ApprovingIdentity = "timestamp-reviewer-v2@example.test"
		return key
	}
	t.Fatal("no unresolved timestamp cell available for promotion")
	return ""
}

func timestampTestCellKey(
	cell types.ExternalExecutionTimestampCellDecision,
) string {
	return fmt.Sprintf(
		"%s/%s/%s/%d",
		cell.SourceTable,
		cell.SourceRowID,
		cell.SourceColumn,
		cell.ColumnOrdinal,
	)
}

func task5SupersedingManifest(
	t *testing.T,
	previous types.ExternalExecutionTimestampManifest,
	promote bool,
	skipKey string,
) (types.ExternalExecutionTimestampManifest, string) {
	t.Helper()
	return task5SupersedingManifestWithDecision(
		t,
		previous,
		promote,
		skipKey,
		types.ExternalExecutionTimestampDecisionProven,
	)
}

func task5SupersedingManifestWithDecision(
	t *testing.T,
	previous types.ExternalExecutionTimestampManifest,
	promote bool,
	skipKey string,
	decision types.ExternalExecutionTimestampDecision,
) (types.ExternalExecutionTimestampManifest, string) {
	t.Helper()
	parentID := previous.ID
	draft := task5DraftRevision(t, previous, &parentID)
	promotedKey := ""
	if promote {
		promotedKey = task5PromoteFirstUnresolvedCellAs(
			t,
			&draft,
			skipKey,
			decision,
		)
		task5RefreshDecisionChecksum(t, &draft)
	}
	return task5SealRevision(t, draft), promotedKey
}

func task12ManifestCellByKey(
	t *testing.T,
	manifest types.ExternalExecutionTimestampManifest,
	key string,
) types.ExternalExecutionTimestampCellDecision {
	t.Helper()
	for _, cell := range manifest.Cells {
		if timestampTestCellKey(cell) == key {
			return cell
		}
	}
	t.Fatalf("manifest cell %s was not found", key)
	return types.ExternalExecutionTimestampCellDecision{}
}

func task12DeleteTimestampSourceForRetention(
	t *testing.T,
	database *task4TestDatabase,
	cell types.ExternalExecutionTimestampCellDecision,
) {
	t.Helper()
	g := NewWithT(t)
	tx, err := database.pool.Begin(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(context.Background(), `
SELECT set_config(
  'distr.external_execution_timestamp_deletion_reason',
  'ORGANIZATION_RETENTION',
  true
), set_config(
  'distr.external_execution_timestamp_deletion_operation_id',
  $1,
  true
)`, uuid.NewString())
	g.Expect(err).NotTo(HaveOccurred())
	var statement string
	switch cell.SourceTable {
	case "externalexecution":
		statement = "DELETE FROM ExternalExecution WHERE id=$1"
	case "externalexecutionevent":
		statement = "DELETE FROM ExternalExecutionEvent WHERE id=$1"
	default:
		t.Fatalf("unsupported timestamp source %q", cell.SourceTable)
	}
	result, err := tx.Exec(context.Background(), statement, cell.SourceRowID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.RowsAffected()).To(Equal(int64(1)))
	g.Expect(tx.Commit(context.Background())).To(Succeed())
}

func task12TombstoneSnapshot(
	t *testing.T,
	database *task4TestDatabase,
) string {
	t.Helper()
	var snapshot string
	err := database.pool.QueryRow(context.Background(), `
SELECT COALESCE(
  jsonb_agg(to_jsonb(row) ORDER BY source_table, source_row_id, column_ordinal),
  '[]'::jsonb
)::text
FROM ExternalExecutionTimestampDeletionTombstone row`).Scan(&snapshot)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return snapshot
}

func task12JSONUint64(t *testing.T, value any, field string) uint64 {
	t.Helper()
	encoded, err := json.Marshal(value)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	var document map[string]any
	NewWithT(t).Expect(json.Unmarshal(encoded, &document)).To(Succeed())
	number, ok := document[field].(float64)
	NewWithT(t).Expect(ok).To(BeTrue(), "missing numeric JSON field %s", field)
	return uint64(number)
}

type task12RealExecutionTopology struct {
	OrganizationID uuid.UUID
	ExecutionID    uuid.UUID
	EventID        uuid.UUID
}

func task12InsertRealExecutionTopology(
	t *testing.T,
	database *task4TestDatabase,
	name string,
	deletedAt *time.Time,
) task12RealExecutionTopology {
	t.Helper()
	g := NewWithT(t)
	ids := struct {
		organization, user, application, target, environment, lifecycle uuid.UUID
		channel, bundle, plan, planTarget, planStep, task, stepRun      uuid.UUID
		execution, event                                                uuid.UUID
	}{
		organization: uuid.New(),
		user:         uuid.New(),
		application:  uuid.New(),
		target:       uuid.New(),
		environment:  uuid.New(),
		lifecycle:    uuid.New(),
		channel:      uuid.New(),
		bundle:       uuid.New(),
		plan:         uuid.New(),
		planTarget:   uuid.New(),
		planStep:     uuid.New(),
		task:         uuid.New(),
		stepRun:      uuid.New(),
		execution:    uuid.New(),
		event:        uuid.New(),
	}
	tx, err := database.pool.Begin(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = tx.Rollback(context.Background()) }()
	exec := func(statement string, arguments ...any) {
		t.Helper()
		_, execErr := tx.Exec(context.Background(), statement, arguments...)
		g.Expect(execErr).NotTo(HaveOccurred())
	}
	exec(
		`INSERT INTO Organization (id, name, deleted_at) VALUES ($1, $2, $3)`,
		ids.organization,
		name,
		deletedAt,
	)
	exec(
		`INSERT INTO UserAccount (id, email) VALUES ($1, $2)`,
		ids.user,
		name+"@example.test",
	)
	exec(`
INSERT INTO Application (id, name, type, organization_id)
VALUES ($1, $2, 'docker', $3)`,
		ids.application,
		name+"-app",
		ids.organization,
	)
	exec(`
INSERT INTO DeploymentTarget (
  id, name, type, organization_id, agent_version_id
) VALUES ($1, $2, 'docker', $3, (SELECT id FROM AgentVersion LIMIT 1))`,
		ids.target,
		name+"-target",
		ids.organization,
	)
	exec(`
INSERT INTO Environment (id, organization_id, name)
VALUES ($1, $2, $3)`,
		ids.environment,
		ids.organization,
		name+"-environment",
	)
	exec(`
INSERT INTO Lifecycle (id, organization_id, name)
VALUES ($1, $2, $3)`,
		ids.lifecycle,
		ids.organization,
		name+"-lifecycle",
	)
	exec(`
INSERT INTO Channel (
  id, organization_id, application_id, lifecycle_id, name, is_default
) VALUES ($1, $2, $3, $4, $5, TRUE)`,
		ids.channel,
		ids.organization,
		ids.application,
		ids.lifecycle,
		name+"-channel",
	)
	exec(`
INSERT INTO ReleaseBundle (
  id, organization_id, application_id, channel_id,
  release_number, status, canonical_checksum, canonical_payload
) VALUES (
  $1, $2, $3, $4, '1.0.0', 'PUBLISHED',
  'sha256:' || repeat('a', 64), convert_to('{}', 'UTF8')
)`,
		ids.bundle,
		ids.organization,
		ids.application,
		ids.channel,
	)
	exec(`
INSERT INTO DeploymentPlan (
  id, organization_id, release_bundle_id, application_id,
  channel_id, environment_id, status, canonical_checksum,
  canonical_payload
) VALUES (
  $1, $2, $3, $4, $5, $6, 'EXECUTED',
  'sha256:' || repeat('b', 64), convert_to('{}', 'UTF8')
)`,
		ids.plan,
		ids.organization,
		ids.bundle,
		ids.application,
		ids.channel,
		ids.environment,
	)
	exec(`
INSERT INTO DeploymentPlanTarget (
  id, deployment_plan_id, organization_id, deployment_target_id,
  name, type, sort_order
) VALUES ($1, $2, $3, $4, $5, 'docker', 0)`,
		ids.planTarget,
		ids.plan,
		ids.organization,
		ids.target,
		name+"-plan-target",
	)
	exec(`
INSERT INTO DeploymentPlanStep (
  id, deployment_plan_id, organization_id, step_key, name,
  action_type, action_name, execution_location, sort_order
) VALUES (
  $1, $2, $3, 'deploy', $4, 'external', 'deploy',
  'server', 0
)`,
		ids.planStep,
		ids.plan,
		ids.organization,
		name+"-plan-step",
	)
	exec(`
INSERT INTO Task (
  id, organization_id, deployment_plan_id, deployment_plan_target_id,
  deployment_target_id, application_id, release_bundle_id,
  channel_id, environment_id, status
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, 'SUCCEEDED'
)`,
		ids.task,
		ids.organization,
		ids.plan,
		ids.planTarget,
		ids.target,
		ids.application,
		ids.bundle,
		ids.channel,
		ids.environment,
	)
	exec(`
INSERT INTO StepRun (
  id, organization_id, task_id, deployment_plan_id,
  deployment_plan_step_id, step_key, name, action_type,
  status, sort_order
) VALUES (
  $1, $2, $3, $4, $5, 'deploy', $6, 'external',
  'SUCCEEDED', 0
)`,
		ids.stepRun,
		ids.organization,
		ids.task,
		ids.plan,
		ids.planStep,
		name+"-step-run",
	)
	var baseline time.Time
	g.Expect(tx.QueryRow(context.Background(), `SELECT clock_timestamp()`).
		Scan(&baseline)).To(Succeed())
	created := baseline.UTC().Add(time.Second)
	updated := created.Add(time.Second)
	started := updated.Add(time.Second)
	completed := started.Add(time.Second)
	deadline := completed.Add(time.Second)
	eventCreated := deadline.Add(time.Second)
	raw := func(value time.Time) string {
		return value.UTC().Format("2006-01-02T15:04:05.000000")
	}
	instant := func(value time.Time) string {
		return externalexecutiontimestamp.FormatInstant(value.UTC())
	}
	exec(`
INSERT INTO ExternalExecution (
  id,
  created_at, created_at_instant,
  updated_at, updated_at_instant,
  started_at, started_at_instant,
  completed_at, completed_at_instant,
  callback_deadline_at, callback_deadline_at_instant,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum, status
) VALUES (
  $1,
  $2::timestamp, $3::timestamptz,
  $4::timestamp, $5::timestamptz,
  $6::timestamp, $7::timestamptz,
  $8::timestamp, $9::timestamptz,
  $10::timestamp, $11::timestamptz,
  $12, $13, $14, $15, $16, $17, $18, $19,
  'api-image', 'sha256:' || repeat('c', 64), $20,
  0, '1.0.0', 'repo/image@sha256:' || repeat('d', 64), 'linux/amd64',
  'config:real-topology', 'sha256:' || repeat('e', 64), 'SUCCEEDED'
)`,
		ids.execution,
		raw(created),
		instant(created),
		raw(updated),
		instant(updated),
		raw(started),
		instant(started),
		raw(completed),
		instant(completed),
		raw(deadline),
		instant(deadline),
		ids.organization,
		ids.stepRun,
		ids.task,
		ids.plan,
		ids.planTarget,
		ids.target,
		ids.application,
		ids.bundle,
		"real-topology-"+ids.execution.String(),
	)
	exec(`
INSERT INTO ExternalExecutionEvent (
  id, created_at, created_at_instant, organization_id,
  external_execution_id, sequence, status, payload_hash
) VALUES (
  $1, $2::timestamp, $3::timestamptz, $4,
  $5, 1, 'SUCCEEDED', 'sha256:' || repeat('f', 64)
)`,
		ids.event,
		raw(eventCreated),
		instant(eventCreated),
		ids.organization,
		ids.execution,
	)
	// Keep every production FK object intact while isolating the three cascade
	// edges under test. The fixture is first inserted as a fully valid graph;
	// unrelated plan/task parents are then removed with replication triggers
	// suppressed so their pre-existing RESTRICT cascade ordering does not turn
	// this timestamp-retention test into an organization-purge redesign.
	exec(`SET LOCAL session_replication_role = 'replica'`)
	for _, deletion := range []struct {
		statement string
		id        uuid.UUID
	}{
		{"DELETE FROM StepRun WHERE id=$1", ids.stepRun},
		{"DELETE FROM Task WHERE id=$1", ids.task},
		{"DELETE FROM DeploymentPlanStep WHERE id=$1", ids.planStep},
		{"DELETE FROM DeploymentPlanTarget WHERE id=$1", ids.planTarget},
		{"DELETE FROM DeploymentPlan WHERE id=$1", ids.plan},
		{"DELETE FROM ReleaseBundle WHERE id=$1", ids.bundle},
		{"DELETE FROM Channel WHERE id=$1", ids.channel},
		{"DELETE FROM Lifecycle WHERE id=$1", ids.lifecycle},
		{"DELETE FROM Environment WHERE id=$1", ids.environment},
		{"DELETE FROM DeploymentTarget WHERE id=$1", ids.target},
		{"DELETE FROM Application WHERE id=$1", ids.application},
	} {
		exec(deletion.statement, deletion.id)
	}
	exec(`SET LOCAL session_replication_role = 'origin'`)
	g.Expect(tx.Commit(context.Background())).To(Succeed())
	return task12RealExecutionTopology{
		OrganizationID: ids.organization,
		ExecutionID:    ids.execution,
		EventID:        ids.event,
	}
}

func task5ApplyRoot(
	t *testing.T,
	database *task4TestDatabase,
	manifest types.ExternalExecutionTimestampManifest,
) *types.ExternalExecutionTimestampApplyReport {
	t.Helper()
	report, err := db.ApplyExternalExecutionTimestampManifest(
		database.ctx,
		task5ApplyRequest(manifest),
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return report
}

func task5VerifiedAt(
	t *testing.T,
	database *task4TestDatabase,
	manifestID uuid.UUID,
) time.Time {
	t.Helper()
	var verifiedAt time.Time
	err := database.pool.QueryRow(context.Background(), `
SELECT verified_at FROM ExternalExecutionTimestampManifest WHERE id=$1`,
		manifestID,
	).Scan(&verifiedAt)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return verifiedAt.UTC()
}

func task5UpdateExactLifecyclePair(
	t *testing.T,
	database *task4TestDatabase,
	executionID uuid.UUID,
	column string,
	instant time.Time,
	withShadow bool,
) {
	t.Helper()
	NewWithT(t).Expect(task5TryUpdateExactLifecyclePair(
		t,
		database,
		executionID,
		column,
		instant,
		withShadow,
	)).NotTo(HaveOccurred())
}

func task5TryUpdateExactLifecyclePair(
	t *testing.T,
	database *task4TestDatabase,
	executionID uuid.UUID,
	column string,
	instant time.Time,
	withShadow bool,
) error {
	raw := instant.UTC().Format("2006-01-02T15:04:05.000000")
	converted := externalexecutiontimestamp.FormatInstant(instant.UTC())
	shadow := any(converted)
	if !withShadow {
		shadow = nil
	}
	statements := map[string]string{
		"updated_at": `UPDATE ExternalExecution
SET updated_at=$2::timestamp, updated_at_instant=$3::timestamptz WHERE id=$1`,
		"started_at": `UPDATE ExternalExecution
SET started_at=$2::timestamp, started_at_instant=$3::timestamptz WHERE id=$1`,
		"completed_at": `UPDATE ExternalExecution
SET completed_at=$2::timestamp, completed_at_instant=$3::timestamptz WHERE id=$1`,
	}
	statement, exists := statements[column]
	if !exists {
		return fmt.Errorf("unsupported lifecycle test column %q", column)
	}
	if !withShadow {
		task5ExecWithPairGuardDisabled(
			t, database, "externalexecution", statement,
			executionID, raw, shadow,
		)
		return nil
	}
	_, err := database.pool.Exec(context.Background(), statement, executionID, raw, shadow)
	return err
}

func task5UTCShadowValueForCell(
	t *testing.T,
	cell types.ExternalExecutionTimestampCellDecision,
) string {
	t.Helper()
	converted, err := externalexecutiontimestamp.ConvertWallClock(*cell.RawValue, 0)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return externalexecutiontimestamp.FormatInstant(converted)
}

func task5ExecWithPairGuardDisabled(
	t *testing.T,
	database *task4TestDatabase,
	sourceTable string,
	statement string,
	arguments ...any,
) {
	t.Helper()
	type pairGuardTarget struct {
		table   string
		trigger string
	}
	targets := map[string]pairGuardTarget{
		"externalexecution": {
			table:   "ExternalExecution",
			trigger: "ExternalExecution_timestamp_pair_guard",
		},
		"externalexecutionevent": {
			table:   "ExternalExecutionEvent",
			trigger: "ExternalExecutionEvent_timestamp_pair_guard",
		},
	}
	target, exists := targets[sourceTable]
	if !exists {
		t.Fatalf("unsupported timestamp pair guard table %q", sourceTable)
	}
	g := NewWithT(t)
	_, err := database.pool.Exec(context.Background(), fmt.Sprintf(
		"ALTER TABLE %s DISABLE TRIGGER %s", target.table, target.trigger,
	))
	g.Expect(err).NotTo(HaveOccurred())
	enabled := false
	t.Cleanup(func() {
		if enabled {
			return
		}
		_, cleanupErr := database.pool.Exec(context.Background(), fmt.Sprintf(
			"ALTER TABLE %s ENABLE TRIGGER %s", target.table, target.trigger,
		))
		g.Expect(cleanupErr).NotTo(HaveOccurred())
	})
	_, err = database.pool.Exec(context.Background(), statement, arguments...)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), fmt.Sprintf(
		"ALTER TABLE %s ENABLE TRIGGER %s", target.table, target.trigger,
	))
	g.Expect(err).NotTo(HaveOccurred())
	enabled = true
}

func task5CorruptShadowForCell(
	t *testing.T,
	database *task4TestDatabase,
	cell types.ExternalExecutionTimestampCellDecision,
	value string,
) {
	t.Helper()
	statements := map[string]string{
		"externalexecution/created_at": `UPDATE ExternalExecution
SET created_at_instant=$2::timestamptz WHERE id=$1`,
		"externalexecution/updated_at": `UPDATE ExternalExecution
SET updated_at_instant=$2::timestamptz WHERE id=$1`,
		"externalexecution/started_at": `UPDATE ExternalExecution
SET started_at_instant=$2::timestamptz WHERE id=$1`,
		"externalexecution/completed_at": `UPDATE ExternalExecution
SET completed_at_instant=$2::timestamptz WHERE id=$1`,
		"externalexecution/callback_deadline_at": `UPDATE ExternalExecution
SET callback_deadline_at_instant=$2::timestamptz WHERE id=$1`,
		"externalexecutionevent/created_at": `UPDATE ExternalExecutionEvent
SET created_at_instant=$2::timestamptz WHERE id=$1`,
	}
	statement, exists := statements[cell.SourceTable+"/"+cell.SourceColumn]
	if !exists {
		t.Fatalf("unsupported timestamp test cell %s/%s", cell.SourceTable, cell.SourceColumn)
	}
	task5ExecWithPairGuardDisabled(
		t, database, cell.SourceTable, statement, cell.SourceRowID, value,
	)
}

func task5InsertPostManifestExecution(
	t *testing.T,
	database *task4TestDatabase,
	baseline time.Time,
	paired bool,
) uuid.UUID {
	t.Helper()
	executionID := uuid.New()
	created := baseline.Add(2 * time.Second).UTC()
	updated := created.Add(time.Second)
	deadline := updated.Add(time.Second)
	createdRaw := created.Format("2006-01-02T15:04:05.000000")
	updatedRaw := updated.Format("2006-01-02T15:04:05.000000")
	deadlineRaw := deadline.Format("2006-01-02T15:04:05.000000")
	deadlineInstant := any(externalexecutiontimestamp.FormatInstant(deadline))
	if !paired {
		deadlineInstant = nil
	}
	statement := `
INSERT INTO ExternalExecution (
  id, created_at, created_at_instant, updated_at, updated_at_instant,
  started_at, started_at_instant, completed_at, completed_at_instant,
  callback_deadline_at, callback_deadline_at_instant,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum
) VALUES (
  $1, $2::timestamp, $3::timestamptz, $4::timestamp, $5::timestamptz,
  NULL, CAST(NULL AS timestamptz), NULL, CAST(NULL AS timestamptz),
  $6::timestamp, $7::timestamptz,
  $8, $9, $10, $11, $12, $13, $14, $15,
  'api-image', 'sha256:' || repeat('1', 64), $16, 0, '2.0.0',
  'repo/image@sha256:' || repeat('2', 64), 'linux/amd64',
  'config:post-manifest', 'sha256:' || repeat('3', 64)
)`
	arguments := []any{
		executionID, createdRaw, externalexecutiontimestamp.FormatInstant(created),
		updatedRaw, externalexecutiontimestamp.FormatInstant(updated),
		deadlineRaw, deadlineInstant,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		uuid.New(), uuid.New(), "post-manifest-" + executionID.String(),
	}
	if paired {
		_, err := database.pool.Exec(context.Background(), statement, arguments...)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	} else {
		task5ExecWithPairGuardDisabled(
			t, database, "externalexecution", statement, arguments...,
		)
	}
	return executionID
}

func task5RootFixture(
	t *testing.T,
) (*task4TestDatabase, task5TimestampFixture) {
	t.Helper()
	database := newTask4TestDatabase(t, 137, "UTC")
	fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
	database.migrateTo(t)
	task5ApplyRoot(t, database, fixture.Manifest)
	return database, fixture
}

func task5NullableLifecycleRootFixture(
	t *testing.T,
) (*task4TestDatabase, task5TimestampFixture) {
	t.Helper()
	database := newTask4TestDatabase(t, 137, "UTC")
	task4InsertFixture(t, database.pool)
	draft, err := db.InspectExternalExecutionTimestamps(database.ctx)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	manifest := task5ApproveTimestampManifest(t, *draft, 9)
	fixture := task5TimestampFixture{
		Manifest: manifest,
		ExecutionIDs: []uuid.UUID{
			task4ExecutionOne,
			task4ExecutionTwo,
		},
		EventIDs: []uuid.UUID{task4EventOne},
	}
	database.migrateTo(t)
	task5ApplyRoot(t, database, manifest)
	return database, fixture
}

func TestVerifyExternalExecutionTimestampManifestIdempotency(t *testing.T) {
	t.Run("exact reapply is verified no-op", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		before := task5ReadWriteState(t, database)
		report, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(fixture.Manifest),
		)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(report.Idempotent).To(BeTrue())
		g.Expect(report.WouldPopulateCount).To(BeZero())
		g.Expect(report.PopulatedShadowCount).To(BeZero())
		g.Expect(task5ReadWriteState(t, database)).To(Equal(before))
	})

	t.Run("canonical reapply tolerates cell order", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		reordered := task5CloneManifest(fixture.Manifest)
		for left, right := 0, len(reordered.Cells)-1; left < right; left, right = left+1, right-1 {
			reordered.Cells[left], reordered.Cells[right] = reordered.Cells[right], reordered.Cells[left]
		}
		before := task5ReadWriteState(t, database)

		report, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(reordered),
		)

		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(report.Idempotent).To(BeTrue())
		g.Expect(task5ReadWriteState(t, database)).To(Equal(before))
	})

	t.Run("checksum collision aborts", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		collision := task5CloneManifest(fixture.Manifest)
		collision.ToolVersion += "-different"
		task5RefreshDecisionChecksum(t, &collision)
		before := task5ReadWriteState(t, database)

		_, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(collision),
		)

		g := NewWithT(t)
		g.Expect(err).To(MatchError(ContainSubstring("collision")))
		g.Expect(task5ReadWriteState(t, database)).To(Equal(before))
	})
}

func TestSupersedingExternalExecutionTimestampManifest(t *testing.T) {
	t.Run("fills one unchanged unresolved shadow", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		child, promotedKey := task5SupersedingManifest(
			t, fixture.Manifest, true, "",
		)
		before := task5ReadWriteState(t, database)

		dryRun, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			types.ExternalExecutionTimestampApplyRequest{Manifest: child},
		)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(dryRun.WouldPopulateCount).To(Equal(uint64(1)))
		report, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(child),
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(report.WouldPopulateCount).To(Equal(uint64(1)))
		g.Expect(report.PopulatedShadowCount).To(Equal(uint64(1)))
		g.Expect(task5ReadWriteState(t, database)).To(Equal(task5WriteState{
			ManifestRows:   before.ManifestRows + 1,
			ProvenanceRows: before.ProvenanceRows + child.RawCellCount,
			ShadowCells:    before.ShadowCells + 1,
		}))
		g.Expect(promotedKey).NotTo(BeEmpty())
		_, err = db.VerifyExternalExecutionTimestampManifest(database.ctx, child.ID)
		g.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("promotes a retained unresolved cell as provenance only", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		child, promotedKey := task5SupersedingManifest(
			t, fixture.Manifest, true, "",
		)
		promoted := task12ManifestCellByKey(t, child, promotedKey)
		task12DeleteTimestampSourceForRetention(t, database, promoted)
		beforeTombstones := task12TombstoneSnapshot(t, database)
		beforeWrites := task5ReadWriteState(t, database)

		dryRun, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			types.ExternalExecutionTimestampApplyRequest{Manifest: child},
		)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(dryRun.WouldPopulateCount).To(BeZero())
		report, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			task5ApplyRequest(child),
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(report.WouldPopulateCount).To(BeZero())
		g.Expect(report.PopulatedShadowCount).To(BeZero())
		g.Expect(task5ReadWriteState(t, database)).To(Equal(task5WriteState{
			ManifestRows:   beforeWrites.ManifestRows + 1,
			ProvenanceRows: beforeWrites.ProvenanceRows + child.RawCellCount,
			ShadowCells:    beforeWrites.ShadowCells,
		}))
		g.Expect(task12TombstoneSnapshot(t, database)).To(Equal(beforeTombstones))

		var rawValue string
		var instantIsNull bool
		g.Expect(database.pool.QueryRow(context.Background(), `
SELECT
  to_char(raw_value, 'YYYY-MM-DD"T"HH24:MI:SS.US'),
  instant_value IS NULL
FROM ExternalExecutionTimestampDeletionTombstone
WHERE source_table=$1 AND source_row_id=$2 AND source_column=$3`,
			promoted.SourceTable,
			promoted.SourceRowID,
			promoted.SourceColumn,
		).Scan(&rawValue, &instantIsNull)).To(Succeed())
		g.Expect(promoted.RawValue).NotTo(BeNil())
		g.Expect(rawValue).To(Equal(*promoted.RawValue))
		g.Expect(instantIsNull).To(BeTrue())

		var resolvedTotal, deletedResolved, unresolvedTotal, deletedUnresolved uint64
		for _, cell := range child.Cells {
			deleted := cell.SourceTable == promoted.SourceTable &&
				cell.SourceRowID == promoted.SourceRowID
			switch cell.Decision {
			case types.ExternalExecutionTimestampDecisionProven,
				types.ExternalExecutionTimestampDecisionAttested:
				resolvedTotal++
				if deleted {
					deletedResolved++
				}
			case types.ExternalExecutionTimestampDecisionUnresolved:
				unresolvedTotal++
				if deleted {
					deletedUnresolved++
				}
			}
		}
		verified, err := db.VerifyExternalExecutionTimestampManifest(
			database.ctx,
			child.ID,
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(verified.ResolvedShadowCount).To(Equal(
			resolvedTotal - deletedResolved,
		))
		g.Expect(verified.UnresolvedShadowCount).To(Equal(
			unresolvedTotal - deletedUnresolved,
		))
		g.Expect(task12JSONUint64(
			t,
			verified,
			"resolvedDeletedEvidenceCount",
		)).To(Equal(deletedResolved))
		g.Expect(task12JSONUint64(
			t,
			verified,
			"unresolvedDeletedEvidenceCount",
		)).To(Equal(deletedUnresolved))

		readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(readiness.ManifestID).NotTo(BeNil())
		g.Expect(*readiness.ManifestID).To(Equal(child.ID))
		g.Expect(readiness.ResolvedShadowCount).To(Equal(
			verified.ResolvedShadowCount,
		))
		g.Expect(readiness.UnresolvedShadowCount).To(Equal(
			verified.UnresolvedShadowCount,
		))
		g.Expect(task12JSONUint64(
			t,
			readiness,
			"resolvedDeletedEvidenceCount",
		)).To(Equal(deletedResolved))
		g.Expect(task12JSONUint64(
			t,
			readiness,
			"unresolvedDeletedEvidenceCount",
		)).To(Equal(deletedUnresolved))

		beforeIdempotent := task5ReadWriteState(t, database)
		idempotent, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			task5ApplyRequest(child),
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(idempotent.Idempotent).To(BeTrue())
		g.Expect(idempotent.PopulatedShadowCount).To(BeZero())
		g.Expect(task5ReadWriteState(t, database)).To(Equal(beforeIdempotent))
		g.Expect(task12TombstoneSnapshot(t, database)).To(Equal(beforeTombstones))
	})

	t.Run("promotes a retained unresolved cell as attested provenance", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		child, promotedKey := task5SupersedingManifestWithDecision(
			t,
			fixture.Manifest,
			true,
			"",
			types.ExternalExecutionTimestampDecisionAttested,
		)
		promoted := task12ManifestCellByKey(t, child, promotedKey)
		g := NewWithT(t)
		g.Expect(promoted.Decision).To(Equal(
			types.ExternalExecutionTimestampDecisionAttested,
		))
		task12DeleteTimestampSourceForRetention(t, database, promoted)

		dryRun, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			types.ExternalExecutionTimestampApplyRequest{Manifest: child},
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(dryRun.WouldPopulateCount).To(BeZero())
		report, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			task5ApplyRequest(child),
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(report.PopulatedShadowCount).To(BeZero())

		verified, err := db.VerifyExternalExecutionTimestampManifest(
			database.ctx,
			child.ID,
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(verified.ResolvedDeletedEvidenceCount).To(BeNumerically(">", 0))
		readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(
			database.ctx,
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(readiness.ResolvedDeletedEvidenceCount).To(Equal(
			verified.ResolvedDeletedEvidenceCount,
		))
	})

	t.Run("retained promotion rejects changed manifest raw facts", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		child, promotedKey := task5SupersedingManifest(
			t, fixture.Manifest, true, "",
		)
		promoted := task12ManifestCellByKey(t, child, promotedKey)
		task12DeleteTimestampSourceForRetention(t, database, promoted)
		changed := task5CloneManifest(child)
		for index := range changed.Cells {
			if timestampTestCellKey(changed.Cells[index]) != promotedKey {
				continue
			}
			changedRaw := "2031-01-01T00:00:00.000001"
			changed.Cells[index].RawValue = &changedRaw
			offset := int32(0)
			converted, convertErr := externalexecutiontimestamp.ConvertWallClock(
				changedRaw,
				offset,
			)
			NewWithT(t).Expect(convertErr).NotTo(HaveOccurred())
			convertedValue := externalexecutiontimestamp.FormatInstant(converted)
			changed.Cells[index].SourceOffsetSeconds = &offset
			changed.Cells[index].ConvertedValue = &convertedValue
			break
		}
		task4RecomputeManifest(t, &changed)

		_, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			task5ApplyRequest(changed),
		)

		g := NewWithT(t)
		g.Expect(err).To(Or(
			MatchError(ContainSubstring("raw-set checksum")),
			MatchError(ContainSubstring("raw value and checksum")),
		))
		g.Expect(task12TombstoneSnapshot(t, database)).NotTo(BeEmpty())
	})

	t.Run("retained promotion rejects tombstone raw drift", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		child, promotedKey := task5SupersedingManifest(
			t, fixture.Manifest, true, "",
		)
		promoted := task12ManifestCellByKey(t, child, promotedKey)
		task12DeleteTimestampSourceForRetention(t, database, promoted)
		_, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			task5ApplyRequest(child),
		)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampDeletionTombstone
DISABLE TRIGGER ExternalExecutionTimestampDeletionTombstone_append_only`)
		g.Expect(err).NotTo(HaveOccurred())
		t.Cleanup(func() {
			_, cleanupErr := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampDeletionTombstone
ENABLE TRIGGER ExternalExecutionTimestampDeletionTombstone_append_only`)
			NewWithT(t).Expect(cleanupErr).NotTo(HaveOccurred())
		})
		_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampDeletionTombstone
SET raw_value=raw_value + INTERVAL '1 second'
WHERE source_table=$1 AND source_row_id=$2 AND source_column=$3`,
			promoted.SourceTable,
			promoted.SourceRowID,
			promoted.SourceColumn,
		)
		g.Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampDeletionTombstone
ENABLE TRIGGER ExternalExecutionTimestampDeletionTombstone_append_only`)
		g.Expect(err).NotTo(HaveOccurred())

		_, err = db.VerifyExternalExecutionTimestampManifest(
			database.ctx,
			child.ID,
		)
		g.Expect(err).To(MatchError(ContainSubstring("raw timestamp changed")))
		_, err = db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		g.Expect(err).To(MatchError(ContainSubstring("raw timestamp changed")))
		_, err = db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			task5ApplyRequest(child),
		)
		g.Expect(err).To(MatchError(ContainSubstring("raw timestamp changed")))
	})

	t.Run("live promotion followed by deletion cannot erase the instant", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		child, promotedKey := task5SupersedingManifest(
			t, fixture.Manifest, true, "",
		)
		promoted := task12ManifestCellByKey(t, child, promotedKey)
		retentionTx, err := database.pool.Begin(context.Background())
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = retentionTx.Rollback(context.Background()) }()
		_, err = retentionTx.Exec(context.Background(), `
SELECT set_config(
  'distr.external_execution_timestamp_deletion_reason',
  'ORGANIZATION_RETENTION',
  true
), set_config(
  'distr.external_execution_timestamp_deletion_operation_id',
  $1,
  true
)`, uuid.NewString())
		g.Expect(err).NotTo(HaveOccurred())
		_, err = db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			task5ApplyRequest(child),
		)
		g.Expect(err).NotTo(HaveOccurred())
		var deleteStatement string
		switch promoted.SourceTable {
		case "externalexecution":
			deleteStatement = "DELETE FROM ExternalExecution WHERE id=$1"
		case "externalexecutionevent":
			deleteStatement = "DELETE FROM ExternalExecutionEvent WHERE id=$1"
		default:
			t.Fatalf("unsupported timestamp source %q", promoted.SourceTable)
		}
		result, err := retentionTx.Exec(
			context.Background(),
			deleteStatement,
			promoted.SourceRowID,
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.RowsAffected()).To(Equal(int64(1)))
		g.Expect(retentionTx.Commit(context.Background())).To(Succeed())

		var deletionFollowsPromotion bool
		g.Expect(database.pool.QueryRow(context.Background(), `
SELECT tombstone.deleted_at > manifest.applied_at
FROM ExternalExecutionTimestampDeletionTombstone tombstone
JOIN ExternalExecutionTimestampManifest manifest ON manifest.id=$4
WHERE tombstone.source_table=$1
  AND tombstone.source_row_id=$2
  AND tombstone.source_column=$3`,
			promoted.SourceTable,
			promoted.SourceRowID,
			promoted.SourceColumn,
			child.ID,
		).Scan(&deletionFollowsPromotion)).To(Succeed())
		g.Expect(deletionFollowsPromotion).To(BeTrue())

		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampDeletionTombstone
DISABLE TRIGGER ExternalExecutionTimestampDeletionTombstone_append_only`)
		g.Expect(err).NotTo(HaveOccurred())
		t.Cleanup(func() {
			_, cleanupErr := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampDeletionTombstone
ENABLE TRIGGER ExternalExecutionTimestampDeletionTombstone_append_only`)
			NewWithT(t).Expect(cleanupErr).NotTo(HaveOccurred())
		})
		_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampDeletionTombstone
SET instant_value=NULL
WHERE source_table=$1 AND source_row_id=$2 AND source_column=$3`,
			promoted.SourceTable,
			promoted.SourceRowID,
			promoted.SourceColumn,
		)
		g.Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampDeletionTombstone
ENABLE TRIGGER ExternalExecutionTimestampDeletionTombstone_append_only`)
		g.Expect(err).NotTo(HaveOccurred())

		for _, operation := range []func() error{
			func() error {
				_, verifyErr := db.VerifyExternalExecutionTimestampManifest(
					database.ctx,
					child.ID,
				)
				return verifyErr
			},
			func() error {
				_, readinessErr := db.CheckExternalExecutionTimestampExpandReadiness(
					database.ctx,
				)
				return readinessErr
			},
			func() error {
				_, applyErr := db.ApplyExternalExecutionTimestampManifest(
					database.ctx,
					task5ApplyRequest(child),
				)
				return applyErr
			},
		} {
			g.Expect(operation()).To(MatchError(ContainSubstring(
				"deletion follows or coincides with provenance promotion",
			)))
		}
	})

	t.Run("resolved instant cannot be rewritten", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		child, _ := task5SupersedingManifest(t, fixture.Manifest, false, "")
		for index := range child.Cells {
			cell := &child.Cells[index]
			if cell.Decision != types.ExternalExecutionTimestampDecisionProven &&
				cell.Decision != types.ExternalExecutionTimestampDecisionAttested {
				continue
			}
			offset := int32(3600)
			converted, err := externalexecutiontimestamp.ConvertWallClock(
				*cell.RawValue, offset,
			)
			NewWithT(t).Expect(err).NotTo(HaveOccurred())
			convertedText := externalexecutiontimestamp.FormatInstant(converted)
			cell.SourceOffsetSeconds = &offset
			cell.ConvertedValue = &convertedText
			break
		}
		task5RefreshDecisionChecksum(t, &child)
		before := task5ReadWriteState(t, database)

		_, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(child),
		)

		g := NewWithT(t)
		g.Expect(err).To(MatchError(ContainSubstring("resolved instant")))
		g.Expect(task5ReadWriteState(t, database)).To(Equal(before))
	})

	t.Run("second active root is rejected", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		secondRoot := task5SealRevision(
			t, task5DraftRevision(t, fixture.Manifest, nil),
		)
		_, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(secondRoot),
		)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("verified tip")))
	})

	t.Run("fork of verified ancestor is rejected", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		firstChild, promotedKey := task5SupersedingManifest(
			t, fixture.Manifest, true, "",
		)
		_, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(firstChild),
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		fork, _ := task5SupersedingManifest(
			t, fixture.Manifest, true, promotedKey,
		)

		_, err = db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(fork),
		)

		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("verified tip")))
	})

	t.Run("evolved updated pair survives child promotion", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5UpdateExactLifecyclePair(
			t, database, fixture.ExecutionIDs[0], "updated_at",
			baseline.Add(time.Second), true,
		)
		evolvedKey := fmt.Sprintf(
			"externalexecution/%s/updated_at/2",
			fixture.ExecutionIDs[0],
		)
		var evolvedRaw string
		var evolvedInstant time.Time
		g := NewWithT(t)
		g.Expect(database.pool.QueryRow(context.Background(), `
SELECT to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SS.US'), updated_at_instant
FROM ExternalExecution WHERE id=$1`, fixture.ExecutionIDs[0]).Scan(
			&evolvedRaw, &evolvedInstant,
		)).To(Succeed())
		child, _ := task5SupersedingManifest(
			t, fixture.Manifest, true, evolvedKey,
		)

		dryRun, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx,
			types.ExternalExecutionTimestampApplyRequest{Manifest: child},
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(dryRun.WouldPopulateCount).To(Equal(uint64(1)))
		report, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(child),
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(report.PopulatedShadowCount).To(Equal(uint64(1)))
		var afterRaw string
		var afterInstant time.Time
		g.Expect(database.pool.QueryRow(context.Background(), `
SELECT to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SS.US'), updated_at_instant
FROM ExternalExecution WHERE id=$1`, fixture.ExecutionIDs[0]).Scan(
			&afterRaw, &afterInstant,
		)).To(Succeed())
		g.Expect(afterRaw).To(Equal(evolvedRaw))
		g.Expect(afterInstant).To(Equal(evolvedInstant))
		_, err = db.VerifyExternalExecutionTimestampManifest(database.ctx, child.ID)
		g.Expect(err).NotTo(HaveOccurred())
	})
}

func TestVerifyExternalExecutionTimestampManifestLifecycle(t *testing.T) {
	t.Run("complete root report and read-only state", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		beforeVerifiedAt := task5VerifiedAt(t, database, fixture.Manifest.ID)

		report, err := db.VerifyExternalExecutionTimestampManifest(
			database.ctx, fixture.Manifest.ID,
		)

		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(report.ManifestID).To(Equal(fixture.Manifest.ID))
		g.Expect(report.SchemaVersion).To(Equal(uint(138)))
		g.Expect(report.SourceExecutionCount).To(Equal(uint64(5)))
		g.Expect(report.SourceEventCount).To(Equal(uint64(5)))
		g.Expect(report.CurrentExecutionCount).To(Equal(uint64(5)))
		g.Expect(report.CurrentEventCount).To(Equal(uint64(5)))
		g.Expect(report.ProvenanceRows).To(Equal(uint64(30)))
		g.Expect(report.ResolvedShadowCount).To(Equal(uint64(18)))
		g.Expect(report.UnresolvedShadowCount).To(Equal(uint64(12)))
		g.Expect(report.RawSetChecksum).To(Equal(fixture.Manifest.RawCellChecksum))
		g.Expect(task5VerifiedAt(t, database, fixture.Manifest.ID)).To(Equal(
			beforeVerifiedAt,
		))
	})

	t.Run("immutable created raw drift", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		task5ExecWithPairGuardDisabled(t, database, "externalexecution", `
UPDATE ExternalExecution
SET created_at=TIMESTAMP '2031-01-01 00:00:00.000001'
WHERE id=$1`, fixture.ExecutionIDs[0])
		_, err := db.VerifyExternalExecutionTimestampManifest(
			database.ctx, fixture.Manifest.ID,
		)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("immutable raw")))
	})

	t.Run("paired updated lifecycle", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5UpdateExactLifecyclePair(
			t, database, fixture.ExecutionIDs[0], "updated_at",
			baseline.Add(time.Second), true,
		)
		_, err := db.VerifyExternalExecutionTimestampManifest(
			database.ctx, fixture.Manifest.ID,
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	})

	t.Run("unpaired updated lifecycle", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5UpdateExactLifecyclePair(
			t, database, fixture.ExecutionIDs[0], "updated_at",
			baseline.Add(time.Second), false,
		)
		_, err := db.VerifyExternalExecutionTimestampManifest(
			database.ctx, fixture.Manifest.ID,
		)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("updated_at pair")))
	})

	t.Run("null started becomes paired", func(t *testing.T) {
		database, fixture := task5NullableLifecycleRootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5UpdateExactLifecyclePair(
			t, database, task4ExecutionTwo, "started_at",
			baseline.Add(time.Second), true,
		)
		_, err := db.VerifyExternalExecutionTimestampManifest(
			database.ctx, fixture.Manifest.ID,
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		err = task5TryUpdateExactLifecyclePair(
			t, database, task4ExecutionTwo, "started_at",
			baseline.Add(2*time.Second), true,
		)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
			"external execution started_at pair is immutable",
		)))
		_, err = db.VerifyExternalExecutionTimestampManifest(
			database.ctx, fixture.Manifest.ID,
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	})

	t.Run("nonnull started cannot be rewritten", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		err := task5TryUpdateExactLifecyclePair(
			t, database, fixture.ExecutionIDs[0], "started_at",
			baseline.Add(time.Second), true,
		)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
			"external execution started_at pair is immutable",
		)))
		_, err = db.VerifyExternalExecutionTimestampManifest(
			database.ctx, fixture.Manifest.ID,
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	})

	t.Run("unresolved shadow cannot be filled outside revision", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		var unresolved types.ExternalExecutionTimestampCellDecision
		for _, cell := range fixture.Manifest.Cells {
			if cell.Decision == types.ExternalExecutionTimestampDecisionUnresolved {
				unresolved = cell
				break
			}
		}
		NewWithT(t).Expect(unresolved.SourceTable).NotTo(BeEmpty())
		task5CorruptShadowForCell(
			t, database, unresolved, task5UTCShadowValueForCell(t, unresolved),
		)
		_, err := db.VerifyExternalExecutionTimestampManifest(
			database.ctx, fixture.Manifest.ID,
		)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("unresolved shadow")))
	})

	t.Run("missing provenance fails", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampCellProvenance
	DISABLE TRIGGER ExternalExecutionTimestampCellProvenance_append_only`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
DELETE FROM ExternalExecutionTimestampCellProvenance
WHERE manifest_id=$1 AND source_table='externalexecution'
  AND source_row_id=$2 AND source_column='created_at'`,
			fixture.Manifest.ID, fixture.ExecutionIDs[0])
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = db.VerifyExternalExecutionTimestampManifest(
			database.ctx, fixture.Manifest.ID,
		)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("provenance")))
	})

	t.Run("paired post-manifest row", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5InsertPostManifestExecution(t, database, baseline, true)
		report, err := db.VerifyExternalExecutionTimestampManifest(
			database.ctx, fixture.Manifest.ID,
		)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(report.CurrentExecutionCount).To(Equal(uint64(6)))
		g.Expect(report.PostManifestPairedCount).To(Equal(uint64(5)))
	})

	t.Run("unpaired post-manifest row", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5InsertPostManifestExecution(t, database, baseline, false)
		_, err := db.VerifyExternalExecutionTimestampManifest(
			database.ctx, fixture.Manifest.ID,
		)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("post-manifest pair")))
	})
}

func TestCheckExternalExecutionTimestampExpandReadinessCatalog(t *testing.T) {
	tests := []struct {
		name      string
		version   int
		arrange   string
		wantError string
	}{
		{name: "clean zero history", version: 138},
		{
			name:    "later additive shape",
			version: 139,
			arrange: `CREATE TABLE Task8AdditiveFeature (id UUID PRIMARY KEY)`,
		},
		{name: "schema 137", version: 137, wantError: "schema version"},
		{
			name:      "dirty 138",
			version:   138,
			arrange:   `UPDATE schema_migrations SET dirty=TRUE`,
			wantError: "dirty",
		},
		{
			name:    "schema ledger view",
			version: 138,
			arrange: `DROP TABLE schema_migrations;
CREATE VIEW schema_migrations AS
SELECT 138::BIGINT AS version, FALSE::BOOLEAN AS dirty`,
			wantError: "ordinary table",
		},
		{
			name:      "schema ledger missing primary key",
			version:   138,
			arrange:   `ALTER TABLE schema_migrations DROP CONSTRAINT schema_migrations_pkey`,
			wantError: "primary key",
		},
		{
			name:    "schema ledger wrong primary key",
			version: 138,
			arrange: `ALTER TABLE schema_migrations
DROP CONSTRAINT schema_migrations_pkey;
ALTER TABLE schema_migrations ADD PRIMARY KEY (dirty)`,
			wantError: "primary key",
		},
		{
			name:    "missing shadow",
			version: 138,
			arrange: `ALTER TABLE ExternalExecution
DROP COLUMN created_at_instant CASCADE`,
			wantError: "column shape",
		},
		{
			name:    "wrong shadow type",
			version: 138,
			arrange: `DROP TRIGGER ExternalExecution_lifecycle_pair_one_shot
ON ExternalExecution;
DROP TRIGGER ExternalExecution_timestamp_pair_guard
ON ExternalExecution;
ALTER TABLE ExternalExecution
ALTER COLUMN started_at_instant TYPE TIMESTAMP WITHOUT TIME ZONE
USING started_at_instant AT TIME ZONE 'UTC'`,
			wantError: "column shape",
		},
		{
			name:    "contracted legacy shape",
			version: 138,
			arrange: `DROP TRIGGER ExternalExecution_timestamp_pair_guard
ON ExternalExecution;
ALTER TABLE ExternalExecution
  ALTER COLUMN created_at DROP DEFAULT,
  ALTER COLUMN created_at TYPE TIMESTAMPTZ
    USING created_at AT TIME ZONE 'UTC'`,
			wantError: "column shape",
		},
		{
			name:    "weakened tombstone retention reason",
			version: 138,
			arrange: `ALTER TABLE ExternalExecutionTimestampDeletionTombstone
DROP CONSTRAINT externalexecutiontimestampdeletiontombstone_reason;
ALTER TABLE ExternalExecutionTimestampDeletionTombstone
ADD CONSTRAINT externalexecutiontimestampdeletiontombstone_reason
CHECK (length(deletion_reason) > 0)`,
			wantError: "expand-compatible",
		},
		{
			name:    "weakened tombstone cell allowlist",
			version: 138,
			arrange: `ALTER TABLE ExternalExecutionTimestampDeletionTombstone
DROP CONSTRAINT externalexecutiontimestampdeletiontombstone_allowlist;
ALTER TABLE ExternalExecutionTimestampDeletionTombstone
ADD CONSTRAINT externalexecutiontimestampdeletiontombstone_allowlist
CHECK (column_ordinal > 0)`,
			wantError: "expand-compatible",
		},
		{
			name:    "missing tombstone nonzero operation constraint",
			version: 138,
			arrange: `ALTER TABLE ExternalExecutionTimestampDeletionTombstone
DROP CONSTRAINT externalexecutiontimestampdeletiontombstone_operation_nonzero`,
			wantError: "expand-compatible",
		},
		{
			name:    "changed tombstone deletion time default",
			version: 138,
			arrange: `ALTER TABLE ExternalExecutionTimestampDeletionTombstone
ALTER COLUMN deleted_at SET DEFAULT (
  CURRENT_TIMESTAMP + INTERVAL '1 hour'
)`,
			wantError: "expand-compatible",
		},
		{
			name:      "missing future index",
			version:   138,
			arrange:   `DROP INDEX ExternalExecution_task_instant_next`,
			wantError: "index shape",
		},
		{
			name:    "same-name partial future index",
			version: 138,
			arrange: `DROP INDEX ExternalExecution_task_instant_next;
CREATE INDEX ExternalExecution_task_instant_next
ON ExternalExecution (task_id, created_at_instant, id) WHERE FALSE`,
			wantError: "index shape",
		},
		{
			name:    "same-name wrong-order legacy index",
			version: 138,
			arrange: `DROP INDEX ExternalExecutionEvent_execution_sequence;
CREATE INDEX ExternalExecutionEvent_execution_sequence
ON ExternalExecutionEvent (sequence, external_execution_id, id)`,
			wantError: "index shape",
		},
		{
			name:      "missing execution timestamp pair guard",
			version:   138,
			arrange:   `DROP TRIGGER ExternalExecution_timestamp_pair_guard ON ExternalExecution`,
			wantError: task8ExecutionTimestampPairGuardError,
		},
		{
			name:    "disabled event timestamp pair guard",
			version: 138,
			arrange: `ALTER TABLE ExternalExecutionEvent
DISABLE TRIGGER ExternalExecutionEvent_timestamp_pair_guard`,
			wantError: "event timestamp pair guard",
		},
		{
			name:    "no-op timestamp pair guard function",
			version: 138,
			arrange: `CREATE OR REPLACE FUNCTION
external_execution_timestamp_pair_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RETURN NEW;
END;
$$`,
			wantError: task8ExecutionTimestampPairGuardError,
		},
		{
			name:    "timestamp pair guard has altered search path",
			version: 138,
			arrange: `ALTER FUNCTION external_execution_timestamp_pair_guard()
SET search_path = pg_catalog`,
			wantError: task8ExecutionTimestampPairGuardError,
		},
		{
			name:    "case variant execution timestamp pair guard name",
			version: 138,
			arrange: `ALTER TRIGGER ExternalExecution_timestamp_pair_guard
ON ExternalExecution RENAME TO "ExternalExecution_timestamp_pair_guard"`,
			wantError: task8ExecutionTimestampPairGuardError,
		},
		{
			name:    "later sorting corrupting execution trigger",
			version: 138,
			arrange: `CREATE FUNCTION z_task8_corrupt_execution_timestamp_pair()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at_instant := NEW.updated_at_instant + INTERVAL '1 hour';
  RETURN NEW;
END;
$$;
CREATE TRIGGER z_task8_corrupt_execution_timestamp_pair
BEFORE UPDATE OF updated_at, updated_at_instant ON ExternalExecution
FOR EACH ROW EXECUTE FUNCTION z_task8_corrupt_execution_timestamp_pair()`,
			wantError: "externalexecution non-internal trigger set",
		},
		{
			name:      "missing lifecycle trigger",
			version:   138,
			arrange:   `DROP TRIGGER ExternalExecution_lifecycle_pair_one_shot ON ExternalExecution`,
			wantError: "lifecycle trigger",
		},
		{
			name:    "disabled lifecycle trigger",
			version: 138,
			arrange: `ALTER TABLE ExternalExecution
DISABLE TRIGGER ExternalExecution_lifecycle_pair_one_shot`,
			wantError: "lifecycle trigger",
		},
		{
			name:    "no-op lifecycle function",
			version: 138,
			arrange: `CREATE OR REPLACE FUNCTION
external_execution_lifecycle_pair_one_shot()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RETURN NEW;
END;
$$`,
			wantError: "lifecycle trigger",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTask4TestDatabase(t, test.version, "UTC")
			if test.arrange != "" {
				_, err := database.pool.Exec(context.Background(), test.arrange)
				NewWithT(t).Expect(err).NotTo(HaveOccurred())
			}

			readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)

			g := NewWithT(t)
			if test.wantError != "" {
				g.Expect(err).To(MatchError(ContainSubstring(test.wantError)))
				g.Expect(readiness).To(BeNil())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(readiness.SchemaVersion).To(Equal(uint(test.version)))
			g.Expect(readiness.TransitionKind).To(Equal("ZERO_HISTORY"))
			g.Expect(readiness.ManifestID).To(BeNil())
		})
	}
}

func TestCheckExternalExecutionTimestampExpandReadinessExpandState(t *testing.T) {
	tests := []struct {
		name      string
		arrange   string
		wantError string
	}{
		{
			name: "missing marker",
			arrange: `ALTER TABLE ExternalExecutionTimestampExpandState
DISABLE TRIGGER ExternalExecutionTimestampExpandState_append_only;
DELETE FROM ExternalExecutionTimestampExpandState;
ALTER TABLE ExternalExecutionTimestampExpandState
ENABLE TRIGGER ExternalExecutionTimestampExpandState_append_only`,
			wantError: "expand state",
		},
		{
			name: "missing expand-state guard",
			arrange: `DROP TRIGGER ExternalExecutionTimestampExpandState_append_only
ON ExternalExecutionTimestampExpandState`,
			wantError: "expand state",
		},
		{
			name: "forged marker with disabled guard",
			arrange: `ALTER TABLE ExternalExecutionTimestampExpandState
DISABLE TRIGGER ExternalExecutionTimestampExpandState_append_only;
UPDATE ExternalExecutionTimestampExpandState
SET transitioned_at=transitioned_at + INTERVAL '1 hour'`,
			wantError: "expand state",
		},
		{
			name: "no-op expand-state function",
			arrange: `CREATE OR REPLACE FUNCTION
external_execution_timestamp_expand_state_append_only()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RETURN NEW;
END;
$$`,
			wantError: "expand state",
		},
		{
			name: "missing expand-state truncate guard",
			arrange: `DROP TRIGGER
ExternalExecutionTimestampExpandState_reject_truncate
ON ExternalExecutionTimestampExpandState`,
			wantError: "expand state truncate guard",
		},
		{
			name: "disabled expand-state truncate guard",
			arrange: `ALTER TABLE ExternalExecutionTimestampExpandState
DISABLE TRIGGER ExternalExecutionTimestampExpandState_reject_truncate`,
			wantError: "expand state truncate guard",
		},
		{
			name: "missing provenance guard",
			arrange: `DROP TRIGGER ExternalExecutionTimestampCellProvenance_append_only
ON ExternalExecutionTimestampCellProvenance`,
			wantError: "provenance guard",
		},
		{
			name: "disabled provenance guard",
			arrange: `ALTER TABLE ExternalExecutionTimestampCellProvenance
DISABLE TRIGGER ExternalExecutionTimestampCellProvenance_append_only`,
			wantError: "provenance guard",
		},
		{
			name: "no-op provenance function",
			arrange: `CREATE OR REPLACE FUNCTION
external_execution_timestamp_provenance_append_only()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RETURN NEW;
END;
$$`,
			wantError: "provenance guard",
		},
		{
			name: "missing provenance truncate guard",
			arrange: `DROP TRIGGER
ExternalExecutionTimestampCellProvenance_reject_truncate
ON ExternalExecutionTimestampCellProvenance`,
			wantError: "provenance truncate guard",
		},
		{
			name: "disabled provenance truncate guard",
			arrange: `ALTER TABLE ExternalExecutionTimestampCellProvenance
DISABLE TRIGGER ExternalExecutionTimestampCellProvenance_reject_truncate`,
			wantError: "provenance truncate guard",
		},
		{
			name: "missing manifest lifecycle guard",
			arrange: `DROP TRIGGER ExternalExecutionTimestampManifest_lifecycle
ON ExternalExecutionTimestampManifest`,
			wantError: "manifest lifecycle",
		},
		{
			name: "disabled manifest lifecycle guard",
			arrange: `ALTER TABLE ExternalExecutionTimestampManifest
DISABLE TRIGGER ExternalExecutionTimestampManifest_lifecycle`,
			wantError: "manifest lifecycle",
		},
		{
			name: "no-op manifest lifecycle function",
			arrange: `CREATE OR REPLACE FUNCTION
external_execution_timestamp_manifest_lifecycle()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RETURN NEW;
END;
$$`,
			wantError: "manifest lifecycle",
		},
		{
			name: "missing manifest truncate guard",
			arrange: `DROP TRIGGER
ExternalExecutionTimestampManifest_reject_truncate
ON ExternalExecutionTimestampManifest`,
			wantError: "manifest truncate guard",
		},
		{
			name: "disabled manifest truncate guard",
			arrange: `ALTER TABLE ExternalExecutionTimestampManifest
DISABLE TRIGGER ExternalExecutionTimestampManifest_reject_truncate`,
			wantError: "manifest truncate guard",
		},
		{
			name: "no-op evidence truncate function",
			arrange: `CREATE OR REPLACE FUNCTION
external_execution_timestamp_reject_truncate()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RETURN NULL;
END;
$$`,
			wantError: "expand state truncate guard",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTask4TestDatabase(t, 138, "UTC")
			_, arrangeErr := database.pool.Exec(context.Background(), test.arrange)
			NewWithT(t).Expect(arrangeErr).NotTo(HaveOccurred())

			readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)

			g := NewWithT(t)
			g.Expect(err).To(MatchError(ContainSubstring(test.wantError)))
			g.Expect(readiness).To(BeNil())
		})
	}
}

func TestCheckExternalExecutionTimestampExpandReadinessRejectsEvidenceTruncate(
	t *testing.T,
) {
	t.Run("zero-history marker truncate and reinsert", func(t *testing.T) {
		database := newTask4TestDatabase(t, 138, "UTC")
		_, err := database.pool.Exec(context.Background(), `
TRUNCATE ExternalExecutionTimestampExpandState;
INSERT INTO ExternalExecutionTimestampExpandState (
  singleton, transition_kind, source_schema_version,
  transition_execution_count, transition_event_count,
  transition_raw_cell_count, transitioned_at
) VALUES (TRUE, 'ZERO_HISTORY', 137, 0, 0, 0,
  clock_timestamp() + INTERVAL '1 hour')`)
		g := NewWithT(t)
		g.Expect(err).To(MatchError(ContainSubstring(
			"external execution timestamp evidence cannot be truncated",
		)))
		_, err = db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		g.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("verified manifest evidence truncate and reinsert", func(t *testing.T) {
		database, _ := task5RootFixture(t)
		for _, statement := range []string{
			"TRUNCATE ExternalExecutionTimestampManifest CASCADE",
			"TRUNCATE ExternalExecutionTimestampCellProvenance",
		} {
			_, err := database.pool.Exec(context.Background(), statement)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
				"external execution timestamp evidence cannot be truncated",
			)))
		}
		_, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	})
}

func task8TransitionedAt(
	t *testing.T,
	database *task4TestDatabase,
) time.Time {
	t.Helper()
	var transitionedAt time.Time
	err := database.pool.QueryRow(context.Background(), `
SELECT transitioned_at FROM ExternalExecutionTimestampExpandState`).Scan(
		&transitionedAt,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return transitionedAt.UTC()
}

func task8InsertPostTransitionEvent(
	t *testing.T,
	database *task4TestDatabase,
	executionID uuid.UUID,
	instant time.Time,
) uuid.UUID {
	t.Helper()
	eventID := uuid.New()
	raw := instant.UTC().Format("2006-01-02T15:04:05.000000")
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionEvent (
  id, created_at, created_at_instant, organization_id,
  external_execution_id, sequence, status, payload_hash
) VALUES (
  $1, $2::timestamp, $3::timestamptz, $4,
  $5, 1, 'RUNNING', 'sha256:' || repeat('8', 64)
)`, eventID, raw, externalexecutiontimestamp.FormatInstant(instant.UTC()),
		uuid.New(), executionID)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return eventID
}

func TestCheckExternalExecutionTimestampExpandReadinessZeroHistory(t *testing.T) {
	tests := []struct {
		name      string
		arrange   func(*testing.T, *task4TestDatabase, time.Time)
		wantError string
		wantExec  uint64
		wantEvent uint64
		wantPairs uint64
	}{
		{name: "empty"},
		{
			name: "later pairs",
			arrange: func(
				t *testing.T,
				database *task4TestDatabase,
				transitionedAt time.Time,
			) {
				executionID := task5InsertPostManifestExecution(
					t, database, transitionedAt, true,
				)
				task8InsertPostTransitionEvent(
					t, database, executionID, transitionedAt.Add(6*time.Second),
				)
			},
			wantExec:  1,
			wantEvent: 1,
			wantPairs: 6,
		},
		{
			name: "row before transition",
			arrange: func(
				t *testing.T,
				database *task4TestDatabase,
				transitionedAt time.Time,
			) {
				task5InsertPostManifestExecution(
					t, database, transitionedAt.Add(-10*time.Second), true,
				)
			},
			wantError: "post-transition creation",
		},
		{
			name: "missing post-transition shadow",
			arrange: func(
				t *testing.T,
				database *task4TestDatabase,
				transitionedAt time.Time,
			) {
				executionID := task5InsertPostManifestExecution(
					t, database, transitionedAt, true,
				)
				task5ExecWithPairGuardDisabled(t, database, "externalexecution", `
UPDATE ExternalExecution SET updated_at_instant=NULL WHERE id=$1`, executionID)
			},
			wantError: "unpaired",
		},
		{
			name: "different post-transition shadow",
			arrange: func(
				t *testing.T,
				database *task4TestDatabase,
				transitionedAt time.Time,
			) {
				executionID := task5InsertPostManifestExecution(
					t, database, transitionedAt, true,
				)
				task5ExecWithPairGuardDisabled(t, database, "externalexecution", `
UPDATE ExternalExecution
SET updated_at_instant=updated_at_instant + INTERVAL '1 hour'
WHERE id=$1`, executionID)
			},
			wantError: "unpaired",
		},
		{
			name: "manifest exists",
			arrange: func(
				t *testing.T,
				database *task4TestDatabase,
				transitionedAt time.Time,
			) {
				task5InsertPostManifestExecution(t, database, transitionedAt, true)
				draft, err := db.InspectExternalExecutionTimestamps(database.ctx)
				NewWithT(t).Expect(err).NotTo(HaveOccurred())
				manifest := task5ApproveTimestampManifest(
					t, *draft, int(draft.PopulatedCellCount),
				)
				task8InsertVerifiedManifestDirect(t, database, manifest)
			},
			wantError: "zero-history ledger",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newTask4TestDatabase(t, 138, "UTC")
			task4DropFixtureForeignKeys(t, database.pool)
			transitionedAt := task8TransitionedAt(t, database)
			if test.arrange != nil {
				test.arrange(t, database, transitionedAt)
			}

			readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)

			g := NewWithT(t)
			if test.wantError != "" {
				g.Expect(err).To(MatchError(ContainSubstring(test.wantError)))
				g.Expect(readiness).To(BeNil())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(readiness.TransitionKind).To(Equal("ZERO_HISTORY"))
			g.Expect(readiness.ManifestID).To(BeNil())
			g.Expect(readiness.ExecutionCount).To(Equal(test.wantExec))
			g.Expect(readiness.EventCount).To(Equal(test.wantEvent))
			g.Expect(readiness.ProvenanceRows).To(BeZero())
			g.Expect(readiness.PostTransitionPairCount).To(Equal(test.wantPairs))
		})
	}
}

func task8UnappliedManifestRequiredFixture(
	t *testing.T,
) (*task4TestDatabase, task5TimestampFixture) {
	t.Helper()
	database := newTask4TestDatabase(t, 137, "UTC")
	fixture := createFiveExecutionTimestampFixture(t, database, task5Statuses())
	database.migrateTo(t)
	return database, fixture
}

func task8NullableText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func task8InsertManifestRow(
	t *testing.T,
	database *task4TestDatabase,
	manifest types.ExternalExecutionTimestampManifest,
	state types.ExternalExecutionTimestampManifestState,
) {
	t.Helper()
	appliedAt := any(nil)
	verifiedAt := any(nil)
	insertState := state
	if state == types.ExternalExecutionTimestampManifestStateApplied ||
		state == types.ExternalExecutionTimestampManifestStateVerified {
		approvedAt, err := time.Parse(time.RFC3339Nano, manifest.ApprovedAt)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		appliedAt = approvedAt.UTC().Add(time.Microsecond)
	}
	if state == types.ExternalExecutionTimestampManifestStateVerified {
		verifiedAt = appliedAt.(time.Time).Add(time.Microsecond)
	}
	insertAppliedAt := appliedAt
	insertVerifiedAt := verifiedAt
	if state == types.ExternalExecutionTimestampManifestStateApplied {
		insertState = types.ExternalExecutionTimestampManifestStateApproved
		insertAppliedAt = nil
		insertVerifiedAt = nil
	}
	if state == types.ExternalExecutionTimestampManifestStateVerified {
		_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampManifest
DISABLE TRIGGER ExternalExecutionTimestampManifest_lifecycle`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	}
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampManifest (
  id, supersedes_manifest_id, database_identity_checksum,
  source_schema_version, snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity, approved_at, target_release_commit,
  target_image_digest, state, decision_content_checksum,
  applied_at, verified_at
) VALUES (
  $1, $2, $3, $4, $5::timestamptz, $6::timestamptz,
  $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
  $17, $18::timestamptz, $19, $20, $21, $22, $23, $24
)`,
		manifest.ID, manifest.SupersedesManifestID,
		manifest.DatabaseIdentityChecksum, manifest.SourceSchemaVersion,
		manifest.SnapshotStartedAt, manifest.SnapshotEndedAt,
		manifest.ExecutionCount, manifest.EventCount, manifest.RawCellCount,
		manifest.PopulatedCellCount, manifest.RawCellChecksum,
		task8NullableText(manifest.EvidenceBundleReference),
		task8NullableText(manifest.EvidenceBundleChecksum), manifest.ToolVersion,
		manifest.ConversionExpressionVersion,
		task8NullableText(manifest.AuthorIdentity),
		task8NullableText(manifest.ReviewerIdentity),
		task8NullableText(manifest.ApprovedAt),
		task8NullableText(manifest.TargetReleaseCommit),
		task8NullableText(manifest.TargetImageDigest), insertState,
		manifest.DecisionContentChecksum, insertAppliedAt, insertVerifiedAt,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	if state == types.ExternalExecutionTimestampManifestStateVerified {
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampManifest
ENABLE TRIGGER ExternalExecutionTimestampManifest_lifecycle`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	}
	if state == types.ExternalExecutionTimestampManifestStateApplied {
		_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state='APPLIED', applied_at=$2
WHERE id=$1`, manifest.ID, appliedAt)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	}
}

func task8InsertManifestProvenance(
	t *testing.T,
	database *task4TestDatabase,
	manifest types.ExternalExecutionTimestampManifest,
) {
	t.Helper()
	for _, cell := range manifest.Cells {
		_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampCellProvenance (
  manifest_id, source_table, source_row_id, source_column,
  column_ordinal, raw_value, raw_is_null, decision, source_zone,
  source_offset_seconds, converted_value, evidence_reference,
  evidence_checksum, approving_identity, raw_cell_checksum,
  parent_manifest_checksum, conversion_expression_version
) VALUES (
  $1, $2, $3, $4, $5, $6::timestamp, $7, $8, $9, $10,
  $11::timestamptz, $12, $13, $14, $15, $16, $17
)`, manifest.ID, cell.SourceTable, cell.SourceRowID, cell.SourceColumn,
			cell.ColumnOrdinal, cell.RawValue, cell.RawValue == nil, cell.Decision,
			task8NullableText(cell.SourceZone), cell.SourceOffsetSeconds,
			cell.ConvertedValue, task8NullableText(cell.EvidenceReference),
			task8NullableText(cell.EvidenceChecksum),
			task8NullableText(cell.ApprovingIdentity), cell.RawCellChecksum,
			manifest.DecisionContentChecksum, cell.ConversionExpressionVersion,
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	}
}

func task8InsertVerifiedManifestDirect(
	t *testing.T,
	database *task4TestDatabase,
	manifest types.ExternalExecutionTimestampManifest,
) {
	t.Helper()
	task8InsertManifestRow(
		t, database, manifest,
		types.ExternalExecutionTimestampManifestStateVerified,
	)
	task8InsertManifestProvenance(t, database, manifest)
}

func TestCheckExternalExecutionTimestampExpandReadinessManifestRequired(
	t *testing.T,
) {
	t.Run("missing verified manifest", func(t *testing.T) {
		database, _ := task8UnappliedManifestRequiredFixture(t)
		readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		g := NewWithT(t)
		g.Expect(err).To(MatchError(ContainSubstring(
			"verified manifest tip is required",
		)))
		g.Expect(readiness).To(BeNil())
	})

	for _, state := range []types.ExternalExecutionTimestampManifestState{
		types.ExternalExecutionTimestampManifestStateApproved,
		types.ExternalExecutionTimestampManifestStateApplied,
	} {
		t.Run(strings.ToLower(string(state))+" is not verified", func(t *testing.T) {
			database, fixture := task8UnappliedManifestRequiredFixture(t)
			task8InsertManifestRow(t, database, fixture.Manifest, state)
			readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
			g := NewWithT(t)
			g.Expect(err).To(MatchError(ContainSubstring("VERIFIED")))
			g.Expect(readiness).To(BeNil())
		})
	}

	t.Run("verified root", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(readiness.SchemaVersion).To(Equal(uint(138)))
		g.Expect(readiness.TransitionKind).To(Equal("MANIFEST_REQUIRED"))
		g.Expect(readiness.ManifestID).NotTo(BeNil())
		g.Expect(*readiness.ManifestID).To(Equal(fixture.Manifest.ID))
		g.Expect(readiness.ExecutionCount).To(Equal(uint64(5)))
		g.Expect(readiness.EventCount).To(Equal(uint64(5)))
		g.Expect(readiness.ProvenanceRows).To(Equal(uint64(30)))
		g.Expect(readiness.PostTransitionPairCount).To(BeZero())
	})

	t.Run("organization retention records tombstones without affecting another tenant", func(t *testing.T) {
		database := newTask4TestDatabase(t, 138, "UTC")
		old := time.Now().UTC().Add(-2 * time.Hour)
		deleted := task12InsertRealExecutionTopology(
			t,
			database,
			"deleted-timestamp-tenant",
			&old,
		)
		retained := task12InsertRealExecutionTopology(
			t,
			database,
			"retained-timestamp-tenant",
			nil,
		)
		var transitionKind string
		var externalExecutionForeignKeys int64
		g := NewWithT(t)
		g.Expect(database.pool.QueryRow(context.Background(), `
SELECT transition_kind
FROM ExternalExecutionTimestampExpandState
WHERE singleton`).Scan(&transitionKind)).To(Succeed())
		g.Expect(transitionKind).To(Equal("ZERO_HISTORY"))
		g.Expect(database.pool.QueryRow(context.Background(), `
SELECT count(*)
FROM pg_constraint constraint_row
JOIN pg_class relation ON relation.oid=constraint_row.conrelid
WHERE relation.relnamespace=to_regnamespace(current_schema())
  AND relation.relname IN ('externalexecution', 'externalexecutionevent')
  AND constraint_row.contype='f'`).Scan(
			&externalExecutionForeignKeys,
		)).To(Succeed())
		g.Expect(externalExecutionForeignKeys).To(Equal(int64(9)))

		deletedCount, err := db.DeleteOrganizationsOlderThan(database.ctx, time.Hour)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(deletedCount).To(Equal(int64(1)))
		readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(readiness.TransitionKind).To(Equal("ZERO_HISTORY"))
		g.Expect(readiness.ExecutionCount).To(Equal(uint64(1)))
		g.Expect(readiness.EventCount).To(Equal(uint64(1)))
		g.Expect(readiness.PostTransitionPairCount).To(Equal(uint64(12)))
		g.Expect(readiness.DeletionTombstoneRows).To(Equal(uint64(6)))
		g.Expect(readiness.DeletedExecutionCount).To(Equal(uint64(1)))
		g.Expect(readiness.DeletedEventCount).To(Equal(uint64(1)))
		var tombstoneRows, deletedOrganizations, deletedExecutions int64
		var deletedEvents, retainedExecutions, retainedEvents int64
		err = database.pool.QueryRow(context.Background(), `
SELECT
  (SELECT count(*) FROM ExternalExecutionTimestampDeletionTombstone),
  (SELECT count(*) FROM Organization WHERE id=$1),
  (SELECT count(*) FROM ExternalExecution WHERE id=$2),
  (SELECT count(*) FROM ExternalExecutionEvent WHERE id=$3),
  (SELECT count(*) FROM ExternalExecution
   WHERE id=$4 AND organization_id=$5),
  (SELECT count(*) FROM ExternalExecutionEvent
   WHERE id=$6 AND organization_id=$5)`,
			deleted.OrganizationID,
			deleted.ExecutionID,
			deleted.EventID,
			retained.ExecutionID,
			retained.OrganizationID,
			retained.EventID,
		).Scan(
			&tombstoneRows,
			&deletedOrganizations,
			&deletedExecutions,
			&deletedEvents,
			&retainedExecutions,
			&retainedEvents,
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(tombstoneRows).To(Equal(int64(6)))
		g.Expect(deletedOrganizations).To(BeZero())
		g.Expect(deletedExecutions).To(BeZero())
		g.Expect(deletedEvents).To(BeZero())
		g.Expect(retainedExecutions).To(Equal(int64(1)))
		g.Expect(retainedEvents).To(Equal(int64(1)))
	})

	t.Run("unexplained source deletion still fails readiness", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionEvent
DISABLE TRIGGER ExternalExecutionEvent_timestamp_deletion_tombstone`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(
			context.Background(),
			`DELETE FROM ExternalExecutionEvent WHERE id = $1`,
			fixture.EventIDs[0],
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionEvent
ENABLE TRIGGER ExternalExecutionEvent_timestamp_deletion_tombstone`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())

		readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)

		g := NewWithT(t)
		g.Expect(err).To(MatchError(ContainSubstring("has no source row or authorized tombstone")))
		g.Expect(readiness).To(BeNil())
	})

	t.Run("transition count mismatch", func(t *testing.T) {
		database, _ := task5RootFixture(t)
		_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampExpandState
DISABLE TRIGGER ExternalExecutionTimestampExpandState_append_only;
UPDATE ExternalExecutionTimestampExpandState
SET transition_execution_count=transition_execution_count + 1,
    transition_raw_cell_count=transition_raw_cell_count + 5;
ALTER TABLE ExternalExecutionTimestampExpandState
ENABLE TRIGGER ExternalExecutionTimestampExpandState_append_only`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("transition counts")))
	})

	t.Run("root snapshot ends after transition", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		snapshotEndedAt, err := externalexecutiontimestamp.ParseInstant(
			fixture.Manifest.SnapshotEndedAt,
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampExpandState
DISABLE TRIGGER ExternalExecutionTimestampExpandState_append_only`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		triggerEnabled := false
		t.Cleanup(func() {
			if triggerEnabled {
				return
			}
			_, cleanupErr := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampExpandState
ENABLE TRIGGER ExternalExecutionTimestampExpandState_append_only`)
			NewWithT(t).Expect(cleanupErr).NotTo(HaveOccurred())
		})
		_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampExpandState
SET transitioned_at=$1::timestamptz`, snapshotEndedAt.Add(-time.Microsecond))
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampExpandState
ENABLE TRIGGER ExternalExecutionTimestampExpandState_append_only`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		triggerEnabled = true

		readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)

		g := NewWithT(t)
		g.Expect(err).To(MatchError(ContainSubstring(
			"first manifest snapshot must end at or before migration-138 transition",
		)))
		g.Expect(readiness).To(BeNil())
	})

	t.Run("missing provenance", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampCellProvenance
DISABLE TRIGGER ExternalExecutionTimestampCellProvenance_append_only`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
DELETE FROM ExternalExecutionTimestampCellProvenance
WHERE manifest_id=$1 AND source_table='externalexecution'
	  AND source_row_id=$2 AND source_column='created_at'`,
			fixture.Manifest.ID, fixture.ExecutionIDs[0])
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampCellProvenance
ENABLE TRIGGER ExternalExecutionTimestampCellProvenance_append_only`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("provenance")))
	})

	t.Run("provenance raw drift", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampCellProvenance
DISABLE TRIGGER ExternalExecutionTimestampCellProvenance_append_only`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampCellProvenance
SET raw_value=raw_value + INTERVAL '1 second'
WHERE manifest_id=$1 AND source_table='externalexecution'
  AND source_row_id=$2 AND source_column='created_at'`,
			fixture.Manifest.ID, fixture.ExecutionIDs[0])
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampCellProvenance
ENABLE TRIGGER ExternalExecutionTimestampCellProvenance_append_only`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("checksum")))
	})

	t.Run("immutable raw drift", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		task5ExecWithPairGuardDisabled(t, database, "externalexecution", `
UPDATE ExternalExecution
SET created_at=TIMESTAMP '2031-01-01 00:00:00.000001'
WHERE id=$1`, fixture.ExecutionIDs[0])
		_, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("immutable raw")))
	})

	t.Run("paired updated evolution", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5UpdateExactLifecyclePair(
			t, database, fixture.ExecutionIDs[0], "updated_at",
			baseline.Add(time.Second), true,
		)
		_, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	})

	t.Run("unpaired updated evolution", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5UpdateExactLifecyclePair(
			t, database, fixture.ExecutionIDs[0], "updated_at",
			baseline.Add(time.Second), false,
		)
		_, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("updated_at pair")))
	})

	t.Run("null completed becomes paired", func(t *testing.T) {
		database, fixture := task5NullableLifecycleRootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5UpdateExactLifecyclePair(
			t, database, task4ExecutionTwo, "completed_at",
			baseline.Add(time.Second), true,
		)
		_, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
	})

	t.Run("nonnull lifecycle rewrite", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		instant := externalexecutiontimestamp.FormatInstant(baseline.Add(time.Second))
		raw := baseline.Add(time.Second).Format("2006-01-02T15:04:05.000000")
		_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecution
DISABLE TRIGGER ExternalExecution_lifecycle_pair_one_shot`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecution
SET started_at=$2::timestamp, started_at_instant=$3::timestamptz
WHERE id=$1`, fixture.ExecutionIDs[0], raw, instant)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecution
ENABLE TRIGGER ExternalExecution_lifecycle_pair_one_shot`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("immutable lifecycle")))
	})

	t.Run("filled unresolved shadow", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		var unresolved types.ExternalExecutionTimestampCellDecision
		for _, cell := range fixture.Manifest.Cells {
			if cell.Decision == types.ExternalExecutionTimestampDecisionUnresolved {
				unresolved = cell
				break
			}
		}
		NewWithT(t).Expect(unresolved.SourceTable).NotTo(BeEmpty())
		task5CorruptShadowForCell(
			t, database, unresolved, task5UTCShadowValueForCell(t, unresolved),
		)
		_, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("unresolved shadow")))
	})

	t.Run("paired later row", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5InsertPostManifestExecution(t, database, baseline, true)
		readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(readiness.ExecutionCount).To(Equal(uint64(6)))
		g.Expect(readiness.EventCount).To(Equal(uint64(5)))
		g.Expect(readiness.PostTransitionPairCount).To(Equal(uint64(5)))
	})

	t.Run("unpaired later row", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5InsertPostManifestExecution(t, database, baseline, false)
		_, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
			"post-manifest pair",
		)))
	})

	t.Run("evolved updated pair with superseding tip", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		baseline := task5VerifiedAt(t, database, fixture.Manifest.ID)
		task5UpdateExactLifecyclePair(
			t, database, fixture.ExecutionIDs[0], "updated_at",
			baseline.Add(time.Second), true,
		)
		evolvedKey := fmt.Sprintf(
			"externalexecution/%s/updated_at/2", fixture.ExecutionIDs[0],
		)
		child, _ := task5SupersedingManifest(
			t, fixture.Manifest, true, evolvedKey,
		)
		_, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(child),
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		g := NewWithT(t)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(readiness.ManifestID).NotTo(BeNil())
		g.Expect(*readiness.ManifestID).To(Equal(child.ID))
	})

	t.Run("second verified root", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		secondRoot := task5SealRevision(
			t, task5DraftRevision(t, fixture.Manifest, nil),
		)
		_, err := database.pool.Exec(context.Background(), `
DROP INDEX externalexecutiontimestampmanifest_active_parent_unique`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		task8InsertVerifiedManifestDirect(t, database, secondRoot)
		_, err = db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("manifest tip")))
	})

	t.Run("disconnected verified cycle", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		_, err := database.pool.Exec(context.Background(), `
DROP INDEX externalexecutiontimestampmanifest_active_parent_unique`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		cycleA := task5SealRevision(
			t, task5DraftRevision(t, fixture.Manifest, nil),
		)
		cycleB, _ := task5SupersedingManifest(t, cycleA, false, "")
		task8InsertVerifiedManifestDirect(t, database, cycleA)
		task8InsertVerifiedManifestDirect(t, database, cycleB)
		cycleAParent := cycleB.ID
		cycleA.SupersedesManifestID = &cycleAParent
		task5RefreshDecisionChecksum(t, &cycleA)

		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampManifest
DISABLE TRIGGER ExternalExecutionTimestampManifest_lifecycle;
ALTER TABLE ExternalExecutionTimestampCellProvenance
DISABLE TRIGGER ExternalExecutionTimestampCellProvenance_append_only;
ALTER TABLE ExternalExecutionTimestampCellProvenance
DROP CONSTRAINT externalexecutiontimestampcell_manifest_checksum_fk`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET supersedes_manifest_id=$2, decision_content_checksum=$3
WHERE id=$1`, cycleA.ID, cycleB.ID, cycleA.DecisionContentChecksum)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampCellProvenance
SET parent_manifest_checksum=$2
WHERE manifest_id=$1`, cycleA.ID, cycleA.DecisionContentChecksum)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampCellProvenance
ADD CONSTRAINT externalexecutiontimestampcell_manifest_checksum_fk
FOREIGN KEY (manifest_id, parent_manifest_checksum)
REFERENCES ExternalExecutionTimestampManifest(id, decision_content_checksum)
ON UPDATE RESTRICT ON DELETE RESTRICT;
ALTER TABLE ExternalExecutionTimestampManifest
ENABLE TRIGGER ExternalExecutionTimestampManifest_lifecycle;
ALTER TABLE ExternalExecutionTimestampCellProvenance
ENABLE TRIGGER ExternalExecutionTimestampCellProvenance_append_only`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())

		readiness, err := db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		g := NewWithT(t)
		g.Expect(err).To(MatchError(ContainSubstring("all VERIFIED")))
		g.Expect(readiness).To(BeNil())
	})

	t.Run("verified fork", func(t *testing.T) {
		database, fixture := task5RootFixture(t)
		child, promotedKey := task5SupersedingManifest(
			t, fixture.Manifest, true, "",
		)
		_, err := db.ApplyExternalExecutionTimestampManifest(
			database.ctx, task5ApplyRequest(child),
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		fork, _ := task5SupersedingManifest(
			t, fixture.Manifest, true, promotedKey,
		)
		_, err = database.pool.Exec(context.Background(), `
DROP INDEX externalexecutiontimestampmanifest_active_parent_unique`)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		task8InsertVerifiedManifestDirect(t, database, fork)
		_, err = db.CheckExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("manifest tip")))
	})
}

func TestRequireExternalExecutionTimestampExpandReadiness(t *testing.T) {
	t.Run("accepts compatible zero-history schema", func(t *testing.T) {
		database := newTask4TestDatabase(t, 138, "UTC")
		NewWithT(t).Expect(
			db.RequireExternalExecutionTimestampExpandReadiness(database.ctx),
		).To(Succeed())
	})
	t.Run("rejects pre-expand schema", func(t *testing.T) {
		database := newTask4TestDatabase(t, 137, "UTC")
		err := db.RequireExternalExecutionTimestampExpandReadiness(database.ctx)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring("schema version")))
	})
}
