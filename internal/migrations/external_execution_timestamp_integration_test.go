package migrations

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestExternalExecutionTimestampMigration138LeavesHistoryUnchanged(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 137)
	executionID, eventID := insertHistoricalExecutionFixture(t, database)
	database.migrateTo(t, 138)

	var allShadowsNull bool
	err := database.pool.QueryRow(context.Background(), `
SELECT
  execution.created_at_instant IS NULL
  AND execution.updated_at_instant IS NULL
  AND execution.started_at_instant IS NULL
  AND execution.completed_at_instant IS NULL
  AND execution.callback_deadline_at_instant IS NULL
  AND event.created_at_instant IS NULL
FROM ExternalExecution execution
JOIN ExternalExecutionEvent event
  ON event.external_execution_id = execution.id
WHERE execution.id = $1 AND event.id = $2`,
		executionID, eventID,
	).Scan(&allShadowsNull)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(allShadowsNull).To(BeTrue())

	var createdAt, updatedAt, startedAt, completedAt, callbackDeadlineAt string
	var eventCreatedAt string
	err = database.pool.QueryRow(context.Background(), `
SELECT
  to_char(execution.created_at, 'YYYY-MM-DD HH24:MI:SS.US'),
  to_char(execution.updated_at, 'YYYY-MM-DD HH24:MI:SS.US'),
  to_char(execution.started_at, 'YYYY-MM-DD HH24:MI:SS.US'),
  to_char(execution.completed_at, 'YYYY-MM-DD HH24:MI:SS.US'),
  to_char(execution.callback_deadline_at, 'YYYY-MM-DD HH24:MI:SS.US'),
  to_char(event.created_at, 'YYYY-MM-DD HH24:MI:SS.US')
FROM ExternalExecution execution
JOIN ExternalExecutionEvent event
  ON event.external_execution_id = execution.id
WHERE execution.id = $1 AND event.id = $2`,
		executionID, eventID,
	).Scan(
		&createdAt, &updatedAt, &startedAt, &completedAt,
		&callbackDeadlineAt, &eventCreatedAt,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect([]string{
		createdAt, updatedAt, startedAt, completedAt,
		callbackDeadlineAt, eventCreatedAt,
	}).To(Equal([]string{
		"2026-07-15 10:00:00.000001",
		"2026-07-15 10:01:00.000002",
		"2026-07-15 10:02:00.000003",
		"2026-07-15 10:03:00.000004",
		"2026-07-15 10:04:00.000005",
		"2026-07-15 10:05:00.000006",
	}))

	var kind string
	var sourceVersion, executionCount, eventCount, rawCellCount int64
	err = database.pool.QueryRow(context.Background(), `
SELECT transition_kind, source_schema_version,
       transition_execution_count, transition_event_count,
       transition_raw_cell_count
FROM ExternalExecutionTimestampExpandState
WHERE singleton`).Scan(
		&kind, &sourceVersion, &executionCount, &eventCount, &rawCellCount,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(kind).To(Equal("MANIFEST_REQUIRED"))
	g.Expect([]int64{
		sourceVersion, executionCount, eventCount, rawCellCount,
	}).To(Equal([]int64{137, 1, 1, 6}))
}

func TestExternalExecutionTimestampMigration138RecordsDurableZeroHistory(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 138)

	var kind string
	var executionCount, eventCount, rawCellCount int64
	err := database.pool.QueryRow(context.Background(), `
SELECT transition_kind, transition_execution_count,
       transition_event_count, transition_raw_cell_count
FROM ExternalExecutionTimestampExpandState
WHERE singleton`).Scan(
		&kind, &executionCount, &eventCount, &rawCellCount,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(kind).To(Equal("ZERO_HISTORY"))
	g.Expect([]int64{
		executionCount, eventCount, rawCellCount,
	}).To(Equal([]int64{0, 0, 0}))

	_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampExpandState
SET transition_kind = 'MANIFEST_REQUIRED'
WHERE singleton`)
	g.Expect(err).To(MatchError(ContainSubstring("expand state is append-only")))
	_, err = database.pool.Exec(context.Background(), `
DELETE FROM ExternalExecutionTimestampExpandState WHERE singleton`)
	g.Expect(err).To(MatchError(ContainSubstring("expand state is append-only")))
}

func TestExternalExecutionTimestampMigration138DefaultsIgnoreSessionTimezone(
	t *testing.T,
) {
	for _, zone := range []string{"UTC", "Asia/Bangkok", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			g := NewWithT(t)
			database := newMigrationTestDatabase(t)
			database.migrateTo(t, 138)
			dropExternalExecutionFixtureForeignKeys(t, database)
			connection, err := database.pool.Acquire(context.Background())
			g.Expect(err).NotTo(HaveOccurred())
			defer connection.Release()
			_, err = connection.Exec(
				context.Background(),
				`SELECT set_config('TimeZone', $1, false)`,
				zone,
			)
			g.Expect(err).NotTo(HaveOccurred())
			executionID := uuid.New()
			eventID := uuid.New()
			organizationID := uuid.New()
			_, err = connection.Exec(context.Background(), `
INSERT INTO ExternalExecution (
  id, callback_deadline_at, callback_deadline_at_instant,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum
) VALUES (
  $1::uuid, CURRENT_TIMESTAMP AT TIME ZONE 'UTC', CURRENT_TIMESTAMP,
  $2, $3, $4, $5, $6, $7, $8, $9,
  'api-image', 'sha256:' || repeat('a', 64),
  'default-' || $1::uuid::text,
  0, '1.0.0', 'repo/image@sha256:' || repeat('b', 64), 'linux/amd64',
  'config:fixture', 'sha256:' || repeat('c', 64)
)`,
				executionID, organizationID, uuid.New(), uuid.New(), uuid.New(),
				uuid.New(), uuid.New(), uuid.New(), uuid.New(),
			)
			g.Expect(err).NotTo(HaveOccurred())
			_, err = connection.Exec(context.Background(), `
INSERT INTO ExternalExecutionEvent (
  id, organization_id, external_execution_id, sequence, status, payload_hash
) VALUES (
  $1, $2, $3, 1, 'RUNNING', 'sha256:' || repeat('d', 64)
)`, eventID, organizationID, executionID)
			g.Expect(err).NotTo(HaveOccurred())

			var paired bool
			err = connection.QueryRow(context.Background(), `
SELECT
  execution.created_at =
    execution.created_at_instant AT TIME ZONE 'UTC'
  AND execution.updated_at =
    execution.updated_at_instant AT TIME ZONE 'UTC'
  AND event.created_at =
    event.created_at_instant AT TIME ZONE 'UTC'
FROM ExternalExecution execution
JOIN ExternalExecutionEvent event
  ON event.external_execution_id = execution.id
WHERE execution.id = $1 AND event.id = $2`,
				executionID, eventID,
			).Scan(&paired)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(paired).To(BeTrue())
		})
	}
}

func insertCurrentTimestampPairFixture(
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
WITH write_clock AS (
  SELECT TIMESTAMPTZ '2026-07-16 10:00:00.000001+00' AS instant
)
INSERT INTO ExternalExecution (
  id, callback_deadline_at, callback_deadline_at_instant,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum,
  created_at, created_at_instant, updated_at, updated_at_instant
)
SELECT
  $1, write_clock.instant AT TIME ZONE 'UTC', write_clock.instant,
  $2, $3, $4, $5, $6, $7, $8, $9,
  'api-image', 'sha256:' || repeat('a', 64),
  'current-' || $1::uuid::text,
  0, '1.0.0', 'repo/image@sha256:' || repeat('b', 64), 'linux/amd64',
  'config:fixture', 'sha256:' || repeat('c', 64),
  write_clock.instant AT TIME ZONE 'UTC', write_clock.instant,
  write_clock.instant AT TIME ZONE 'UTC', write_clock.instant
FROM write_clock`,
		executionID, organizationID, uuid.New(), uuid.New(), uuid.New(),
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
	)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
WITH write_clock AS (
  SELECT TIMESTAMPTZ '2026-07-16 10:01:00.000002+00' AS instant
)
INSERT INTO ExternalExecutionEvent (
  id, created_at, created_at_instant, organization_id,
  external_execution_id, sequence, status, payload_hash
)
SELECT
  $1, write_clock.instant AT TIME ZONE 'UTC', write_clock.instant,
  $2, $3, 1, 'RUNNING', 'sha256:' || repeat('d', 64)
FROM write_clock`, eventID, organizationID, executionID)
	g.Expect(err).NotTo(HaveOccurred())
	return executionID, eventID
}

func TestExternalExecutionTimestampMigration138WritePairGuards(t *testing.T) {
	t.Run("rejects legacy execution insert without callback instant", func(t *testing.T) {
		g := NewWithT(t)
		database := newMigrationTestDatabase(t)
		database.migrateTo(t, 138)
		dropExternalExecutionFixtureForeignKeys(t, database)
		_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecution (
  id, callback_deadline_at,
  organization_id, step_run_id, task_id, deployment_plan_id,
  deployment_plan_target_id, deployment_target_id, application_id,
  release_bundle_id, component, plan_checksum, idempotency_key,
  expected_state_version, expected_version, expected_image, expected_platform,
  expected_config_reference, expected_config_checksum
) VALUES (
  $1, TIMESTAMP '2026-07-16 11:00:00',
  $2, $3, $4, $5, $6, $7, $8, $9,
  'api-image', 'sha256:' || repeat('a', 64),
  'legacy-' || $1::uuid::text,
  0, '1.0.0', 'repo/image@sha256:' || repeat('b', 64), 'linux/amd64',
  'config:fixture', 'sha256:' || repeat('c', 64)
)`, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
			uuid.New(), uuid.New(), uuid.New(), uuid.New())
		g.Expect(err).To(MatchError(ContainSubstring(
			"callback_deadline_at must be one exact UTC pair",
		)))
	})

	t.Run("rejects legacy raw-only updates and accepts dual writes", func(t *testing.T) {
		g := NewWithT(t)
		database := newMigrationTestDatabase(t)
		database.migrateTo(t, 138)
		executionID, eventID := insertCurrentTimestampPairFixture(t, database)

		_, err := database.pool.Exec(context.Background(), `
UPDATE ExternalExecution
SET updated_at = TIMESTAMP '2026-07-16 11:01:00.000003'
WHERE id = $1`, executionID)
		g.Expect(err).To(MatchError(ContainSubstring(
			"updated_at raw update requires its instant pair",
		)))
		_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionEvent
SET created_at = TIMESTAMP '2026-07-16 11:02:00.000004'
WHERE id = $1`, eventID)
		g.Expect(err).To(MatchError(ContainSubstring(
			"event created_at raw update requires its instant pair",
		)))

		_, err = database.pool.Exec(context.Background(), `
WITH write_clock AS (
  SELECT TIMESTAMPTZ '2026-07-16 11:03:00.000005+00' AS instant
)
UPDATE ExternalExecution AS execution
SET updated_at = write_clock.instant AT TIME ZONE 'UTC',
    updated_at_instant = write_clock.instant,
    started_at = write_clock.instant AT TIME ZONE 'UTC',
    started_at_instant = write_clock.instant
FROM write_clock
WHERE execution.id = $1`, executionID)
		g.Expect(err).NotTo(HaveOccurred())
		_, err = database.pool.Exec(context.Background(), `
WITH write_clock AS (
  SELECT TIMESTAMPTZ '2026-07-16 11:04:00.000006+00' AS instant
)
UPDATE ExternalExecutionEvent AS event
SET created_at = write_clock.instant AT TIME ZONE 'UTC',
    created_at_instant = write_clock.instant
FROM write_clock
WHERE event.id = $1`, eventID)
		g.Expect(err).NotTo(HaveOccurred())
	})
}

type timestampPairGuardProvenance struct {
	SourceTable   string
	SourceRowID   uuid.UUID
	SourceColumn  string
	ColumnOrdinal int16
	RawValue      string
	Converted     string
	ManifestState string
}

func insertTimestampPairGuardProvenance(
	t *testing.T,
	transaction pgx.Tx,
	provenance timestampPairGuardProvenance,
) {
	t.Helper()
	g := NewWithT(t)
	manifestID := uuid.New()
	checksum := "sha256:" + strings.Repeat("e", 64)
	_, err := transaction.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampManifest (
  id, database_identity_checksum, source_schema_version,
  snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version,
  author_identity, reviewer_identity, approved_at,
  target_release_commit, target_image_digest,
  state, decision_content_checksum
) VALUES (
  $1, $2, 137,
  CURRENT_TIMESTAMP - INTERVAL '2 minutes',
  CURRENT_TIMESTAMP - INTERVAL '1 minute',
  1, 1, 6, 1,
  $2, 'evidence:pair-guard', $2,
  'distr-pair-guard-test', 'external-execution-offset/v1',
  'author@example.test', 'reviewer@example.test',
  CASE WHEN $3 = 'APPROVED' THEN CURRENT_TIMESTAMP ELSE NULL END,
  $4, $5, $3, $2
)`, manifestID, checksum, provenance.ManifestState,
		strings.Repeat("a", 40), "sha256:"+strings.Repeat("b", 64))
	g.Expect(err).NotTo(HaveOccurred())
	_, err = transaction.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampCellProvenance (
  manifest_id, source_table, source_row_id, source_column, column_ordinal,
  raw_value, raw_is_null, decision, source_zone, source_offset_seconds,
  converted_value, evidence_reference, evidence_checksum, approving_identity,
  raw_cell_checksum, parent_manifest_checksum, conversion_expression_version
) VALUES (
  $1, $2, $3, $4, $5,
  $6::timestamp, FALSE, 'PROVEN', 'Asia/Bangkok', 25200,
  $7::timestamptz, 'evidence:pair-guard', $8, 'reviewer@example.test',
  $8, $8, 'external-execution-offset/v1'
)`, manifestID, provenance.SourceTable, provenance.SourceRowID,
		provenance.SourceColumn, provenance.ColumnOrdinal,
		provenance.RawValue, provenance.Converted, checksum)
	g.Expect(err).NotTo(HaveOccurred())
}

func TestExternalExecutionTimestampMigration138ShadowBackfillRequiresExactCurrentProvenance(
	t *testing.T,
) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 137)
	executionID, eventID := insertHistoricalExecutionFixture(t, database)
	database.migrateTo(t, 138)
	_, err := database.pool.Exec(context.Background(), `
ALTER TABLE ExternalExecutionTimestampCellProvenance
DROP CONSTRAINT externalexecutiontimestampcell_allowlist`)
	g.Expect(err).NotTo(HaveOccurred())

	exact := timestampPairGuardProvenance{
		SourceTable:   "externalexecution",
		SourceRowID:   executionID,
		SourceColumn:  "created_at",
		ColumnOrdinal: 1,
		RawValue:      "2026-07-15 10:00:00.000001",
		Converted:     "2026-07-15 03:00:00.000001+00",
		ManifestState: "APPROVED",
	}
	negativeCases := []struct {
		name   string
		mutate func(*timestampPairGuardProvenance)
	}{
		{name: "wrong source table", mutate: func(value *timestampPairGuardProvenance) {
			value.SourceTable = "externalexecutionevent"
		}},
		{name: "wrong source row", mutate: func(value *timestampPairGuardProvenance) {
			value.SourceRowID = uuid.New()
		}},
		{name: "wrong source column", mutate: func(value *timestampPairGuardProvenance) {
			value.SourceColumn = "updated_at"
		}},
		{name: "wrong column ordinal", mutate: func(value *timestampPairGuardProvenance) {
			value.ColumnOrdinal = 2
		}},
		{name: "wrong raw value", mutate: func(value *timestampPairGuardProvenance) {
			value.RawValue = "2026-07-15 10:00:01.000001"
		}},
		{name: "wrong converted value", mutate: func(value *timestampPairGuardProvenance) {
			value.Converted = "2026-07-15 03:00:01.000001+00"
		}},
		{name: "manifest is not approved", mutate: func(value *timestampPairGuardProvenance) {
			value.ManifestState = "DRAFT"
		}},
	}
	for _, test := range negativeCases {
		t.Run(test.name, func(t *testing.T) {
			candidate := exact
			test.mutate(&candidate)
			transaction, beginErr := database.pool.Begin(context.Background())
			NewWithT(t).Expect(beginErr).NotTo(HaveOccurred())
			defer func() { _ = transaction.Rollback(context.Background()) }()
			insertTimestampPairGuardProvenance(t, transaction, candidate)
			_, updateErr := transaction.Exec(context.Background(), `
UPDATE ExternalExecution
SET created_at_instant = $2::timestamptz
WHERE id = $1`, executionID, exact.Converted)
			NewWithT(t).Expect(updateErr).To(MatchError(ContainSubstring(
				"shadow-only update requires exact current-transaction provenance",
			)))
		})
	}

	t.Run("missing provenance", func(t *testing.T) {
		_, updateErr := database.pool.Exec(context.Background(), `
UPDATE ExternalExecution
SET created_at_instant = $2::timestamptz
WHERE id = $1`, executionID, exact.Converted)
		NewWithT(t).Expect(updateErr).To(MatchError(ContainSubstring(
			"shadow-only update requires exact current-transaction provenance",
		)))
	})

	t.Run("exact current transaction provenance permits Bangkok conversion", func(t *testing.T) {
		transaction, beginErr := database.pool.Begin(context.Background())
		NewWithT(t).Expect(beginErr).NotTo(HaveOccurred())
		defer func() { _ = transaction.Rollback(context.Background()) }()
		insertTimestampPairGuardProvenance(t, transaction, exact)
		_, updateErr := transaction.Exec(context.Background(), `
UPDATE ExternalExecution
SET created_at_instant = $2::timestamptz
WHERE id = $1`, executionID, exact.Converted)
		NewWithT(t).Expect(updateErr).NotTo(HaveOccurred())
	})

	t.Run("event requires matching current transaction provenance", func(t *testing.T) {
		eventProvenance := exact
		eventProvenance.SourceTable = "externalexecutionevent"
		eventProvenance.SourceRowID = eventID
		eventProvenance.ColumnOrdinal = 6
		eventProvenance.RawValue = "2026-07-15 10:05:00.000006"
		eventProvenance.Converted = "2026-07-15 03:05:00.000006+00"
		_, updateErr := database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionEvent
SET created_at_instant = $2::timestamptz
WHERE id = $1`, eventID, eventProvenance.Converted)
		NewWithT(t).Expect(updateErr).To(MatchError(ContainSubstring(
			"shadow-only update requires exact current-transaction provenance",
		)))

		transaction, beginErr := database.pool.Begin(context.Background())
		NewWithT(t).Expect(beginErr).NotTo(HaveOccurred())
		defer func() { _ = transaction.Rollback(context.Background()) }()
		insertTimestampPairGuardProvenance(t, transaction, eventProvenance)
		_, updateErr = transaction.Exec(context.Background(), `
UPDATE ExternalExecutionEvent
SET created_at_instant = $2::timestamptz
WHERE id = $1`, eventID, eventProvenance.Converted)
		NewWithT(t).Expect(updateErr).NotTo(HaveOccurred())
	})

	t.Run("committed provenance cannot authorize a later transaction", func(t *testing.T) {
		transaction, beginErr := database.pool.Begin(context.Background())
		NewWithT(t).Expect(beginErr).NotTo(HaveOccurred())
		insertTimestampPairGuardProvenance(t, transaction, exact)
		NewWithT(t).Expect(transaction.Commit(context.Background())).To(Succeed())
		_, updateErr := database.pool.Exec(context.Background(), `
UPDATE ExternalExecution
SET created_at_instant = $2::timestamptz
WHERE id = $1`, executionID, exact.Converted)
		NewWithT(t).Expect(updateErr).To(MatchError(ContainSubstring(
			"shadow-only update requires exact current-transaction provenance",
		)))
	})
}

func TestExternalExecutionTimestampMigration138Catalog(t *testing.T) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 138)
	var columnCount int64
	var allTimestamptz, allNullable bool
	err := database.pool.QueryRow(context.Background(), `
SELECT
  count(*),
  bool_and(data_type = 'timestamp with time zone'),
  bool_and(is_nullable = 'YES')
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND (
    (table_name = 'externalexecution' AND column_name IN (
      'created_at_instant', 'updated_at_instant', 'started_at_instant',
      'completed_at_instant', 'callback_deadline_at_instant'
    ))
    OR
    (table_name = 'externalexecutionevent' AND column_name = 'created_at_instant')
  )`).Scan(&columnCount, &allTimestamptz, &allNullable)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(columnCount).To(Equal(int64(6)))
	g.Expect(allTimestamptz).To(BeTrue())
	g.Expect(allNullable).To(BeTrue())

	for indexName, expected := range map[string]string{
		"externalexecution_organization_status_instant_next": "(organization_id, status, updated_at_instant DESC, id)",
		"externalexecution_task_instant_next":                "(task_id, created_at_instant, id)",
	} {
		var definition string
		err = database.pool.QueryRow(context.Background(), `
SELECT pg_get_indexdef(index_row.indexrelid)
FROM pg_index index_row
JOIN pg_class index_class ON index_class.oid = index_row.indexrelid
WHERE index_class.relnamespace = to_regnamespace(current_schema())
  AND index_class.relname = $1`, indexName).Scan(&definition)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(definition).To(ContainSubstring(expected))
	}

	var manifestSourceConstraint string
	err = database.pool.QueryRow(context.Background(), `
SELECT pg_get_constraintdef(constraint_row.oid)
FROM pg_constraint constraint_row
JOIN pg_class relation ON relation.oid = constraint_row.conrelid
WHERE relation.relnamespace = to_regnamespace(current_schema())
  AND relation.relname = 'externalexecutiontimestampmanifest'
  AND constraint_row.conname =
    'externalexecutiontimestampmanifest_source_schema_137'`).Scan(
		&manifestSourceConstraint,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(manifestSourceConstraint).To(Equal(
		"CHECK ((source_schema_version = 137))",
	))
}

func TestExternalExecutionTimestampMigration138EnforcesLifecyclePairOneShot(
	t *testing.T,
) {
	testColumns := []struct {
		raw    string
		shadow string
	}{
		{raw: "started_at", shadow: "started_at_instant"},
		{raw: "completed_at", shadow: "completed_at_instant"},
	}

	for _, columns := range testColumns {
		t.Run(columns.raw+" originally null resolves once", func(t *testing.T) {
			g := NewWithT(t)
			database := newMigrationTestDatabase(t)
			database.migrateTo(t, 137)
			executionID, _ := insertHistoricalExecutionFixture(t, database)
			_, err := database.pool.Exec(context.Background(), fmt.Sprintf(`
UPDATE ExternalExecution SET %s = NULL WHERE id = $1`, columns.raw), executionID)
			g.Expect(err).NotTo(HaveOccurred())
			database.migrateTo(t, 138)

			_, err = database.pool.Exec(context.Background(), fmt.Sprintf(`
UPDATE ExternalExecution
SET %s = COALESCE(NULL::timestamp, %s),
    %s = COALESCE(NULL::timestamptz, %s)
WHERE id = $1`, columns.raw, columns.raw, columns.shadow, columns.shadow), executionID)
			g.Expect(err).NotTo(HaveOccurred())

			firstRaw := "2026-07-16 11:00:00.000001"
			firstInstant := "2026-07-16 11:00:00.000001+00"
			_, err = database.pool.Exec(context.Background(), fmt.Sprintf(`
UPDATE ExternalExecution SET %s = $2::timestamp, %s = $3::timestamptz
WHERE id = $1`, columns.raw, columns.shadow), executionID, firstRaw,
				"2026-07-16 12:00:00.000001+00")
			g.Expect(err).To(MatchError(ContainSubstring(
				"external execution " + columns.raw + " must resolve to one exact UTC pair",
			)))

			_, err = database.pool.Exec(context.Background(), fmt.Sprintf(`
UPDATE ExternalExecution SET %s = $2::timestamp, %s = $3::timestamptz
WHERE id = $1`, columns.raw, columns.shadow), executionID, firstRaw, firstInstant)
			g.Expect(err).NotTo(HaveOccurred())
			var exactPair bool
			err = database.pool.QueryRow(context.Background(), fmt.Sprintf(`
SELECT %s = $2::timestamp AND %s = $3::timestamptz
FROM ExternalExecution WHERE id = $1`, columns.raw, columns.shadow),
				executionID, firstRaw, firstInstant).Scan(&exactPair)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(exactPair).To(BeTrue())

			_, err = database.pool.Exec(context.Background(), fmt.Sprintf(`
UPDATE ExternalExecution
SET %s = COALESCE($2::timestamp, %s),
    %s = COALESCE($3::timestamptz, %s)
WHERE id = $1`, columns.raw, columns.raw, columns.shadow, columns.shadow),
				executionID, firstRaw, firstInstant)
			g.Expect(err).NotTo(HaveOccurred())

			_, err = database.pool.Exec(context.Background(), fmt.Sprintf(`
UPDATE ExternalExecution SET %s = $2::timestamp, %s = $3::timestamptz
WHERE id = $1`, columns.raw, columns.shadow), executionID,
				"2026-07-16 12:00:00.000002", "2026-07-16 12:00:00.000002+00")
			g.Expect(err).To(MatchError(ContainSubstring(
				"external execution " + columns.raw + " pair is immutable",
			)))
		})

		t.Run(columns.raw+" historical value rejects unproven shadow fill", func(t *testing.T) {
			g := NewWithT(t)
			database := newMigrationTestDatabase(t)
			database.migrateTo(t, 137)
			executionID, _ := insertHistoricalExecutionFixture(t, database)
			database.migrateTo(t, 138)

			var shadowIsNull bool
			err := database.pool.QueryRow(context.Background(), fmt.Sprintf(`
SELECT %s IS NULL FROM ExternalExecution WHERE id = $1`, columns.shadow),
				executionID).Scan(&shadowIsNull)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(shadowIsNull).To(BeTrue())

			nonzeroOffsetInstant := map[string]string{
				"started_at":   "2026-07-15 02:02:00.000003+00",
				"completed_at": "2026-07-15 02:03:00.000004+00",
			}[columns.raw]
			_, err = database.pool.Exec(context.Background(), fmt.Sprintf(`
UPDATE ExternalExecution SET %s = $2::timestamptz
WHERE id = $1`, columns.shadow), executionID, nonzeroOffsetInstant)
			g.Expect(err).To(MatchError(ContainSubstring(
				"shadow-only update requires exact current-transaction provenance",
			)))
			var shadowRemainsNull bool
			err = database.pool.QueryRow(context.Background(), fmt.Sprintf(`
SELECT %s IS NULL FROM ExternalExecution WHERE id = $1`,
				columns.shadow), executionID).Scan(
				&shadowRemainsNull,
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(shadowRemainsNull).To(BeTrue())
		})
	}
}

func insertTimestampManifestFixture(
	t *testing.T,
	database *migrationTestDatabase,
) (uuid.UUID, string) {
	t.Helper()
	manifestID := uuid.New()
	checksum := "sha256:" + strings.Repeat("a", 64)
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampManifest (
  id, database_identity_checksum, source_schema_version,
  snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version,
  author_identity, reviewer_identity, approved_at,
  target_release_commit, target_image_digest,
  state, decision_content_checksum
) VALUES (
  $1, $2, 137,
  CURRENT_TIMESTAMP - INTERVAL '2 minutes',
  CURRENT_TIMESTAMP - INTERVAL '1 minute',
  1, 0, 5, 5,
  $2, 'evidence:fixture', $2,
  'distr-test', 'external-execution-offset/v1',
  'author@example.test', 'reviewer@example.test',
  CURRENT_TIMESTAMP - INTERVAL '30 seconds',
  $3, $4, 'APPROVED', $2
)`,
		manifestID, checksum, strings.Repeat("b", 40),
		"sha256:"+strings.Repeat("c", 64),
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampCellProvenance (
  manifest_id, source_table, source_row_id, source_column, column_ordinal,
  raw_value, raw_is_null, decision, source_zone, source_offset_seconds,
  converted_value, evidence_reference, evidence_checksum, approving_identity,
  raw_cell_checksum, parent_manifest_checksum, conversion_expression_version
) VALUES (
  $1, 'externalexecution', $2, 'created_at', 1,
  TIMESTAMP '2026-07-15 10:00:00', FALSE, 'PROVEN', 'UTC', 0,
  TIMESTAMPTZ '2026-07-15 10:00:00+00', 'evidence:fixture', $3,
  'reviewer@example.test', $3, $3, 'external-execution-offset/v1'
)`, manifestID, uuid.New(), checksum)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return manifestID, checksum
}

func TestExternalExecutionTimestampMigration138EnforcesImmutableLifecycle(
	t *testing.T,
) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 138)
	manifestID, _ := insertTimestampManifestFixture(t, database)

	_, err := database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampCellProvenance
SET approving_identity = 'changed@example.test'
WHERE manifest_id = $1`, manifestID)
	g.Expect(err).To(MatchError(ContainSubstring("provenance is append-only")))
	_, err = database.pool.Exec(context.Background(), `
DELETE FROM ExternalExecutionTimestampCellProvenance
WHERE manifest_id = $1`, manifestID)
	g.Expect(err).To(MatchError(ContainSubstring("provenance is append-only")))
	_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET tool_version = 'changed'
WHERE id = $1`, manifestID)
	g.Expect(err).To(MatchError(ContainSubstring("manifest content is immutable")))
	_, err = database.pool.Exec(context.Background(), `
DELETE FROM ExternalExecutionTimestampManifest
WHERE id = $1`, manifestID)
	g.Expect(err).To(MatchError(ContainSubstring("manifest is append-only")))
	_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state = 'VERIFIED', verified_at = CURRENT_TIMESTAMP
WHERE id = $1`, manifestID)
	g.Expect(err).To(MatchError(ContainSubstring("invalid manifest lifecycle transition")))

	_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state = 'APPLIED', applied_at = CURRENT_TIMESTAMP
WHERE id = $1`, manifestID)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state = 'VERIFIED', verified_at = CURRENT_TIMESTAMP
WHERE id = $1`, manifestID)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state = 'APPROVED', verified_at = NULL
WHERE id = $1`, manifestID)
	g.Expect(err).To(MatchError(ContainSubstring("invalid manifest lifecycle transition")))
}

func TestExternalExecutionTimestampMigration138RejectsEvidenceTruncate(
	t *testing.T,
) {
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 138)
	insertTimestampManifestFixture(t, database)

	for _, test := range []struct {
		name      string
		statement string
	}{
		{
			name:      "expand state",
			statement: "TRUNCATE ExternalExecutionTimestampExpandState",
		},
		{
			name:      "manifest",
			statement: "TRUNCATE ExternalExecutionTimestampManifest CASCADE",
		},
		{
			name:      "provenance",
			statement: "TRUNCATE ExternalExecutionTimestampCellProvenance",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := database.pool.Exec(context.Background(), test.statement)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
				"external execution timestamp evidence cannot be truncated",
			)))
		})
	}
}

func TestExternalExecutionTimestampMigration138RejectsTerminalManifestInsert(
	t *testing.T,
) {
	for _, state := range []string{
		"APPLIED",
		"VERIFIED",
		"REVOKED_BEFORE_APPLY",
	} {
		t.Run(state, func(t *testing.T) {
			g := NewWithT(t)
			database := newMigrationTestDatabase(t)
			database.migrateTo(t, 138)
			rootID, _ := insertTimestampManifestFixture(t, database)

			_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampManifest (
  id, supersedes_manifest_id, database_identity_checksum,
  source_schema_version, snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity, approved_at, applied_at, verified_at, revoked_at,
  target_release_commit, target_image_digest, state, decision_content_checksum
) SELECT
  $1, id, database_identity_checksum,
  source_schema_version, snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
	  reviewer_identity, approved_at,
	  CASE WHEN $3 IN ('APPLIED', 'VERIFIED') THEN CURRENT_TIMESTAMP END,
	  CASE WHEN $3 = 'VERIFIED' THEN CURRENT_TIMESTAMP END,
	  CASE WHEN $3 = 'REVOKED_BEFORE_APPLY' THEN CURRENT_TIMESTAMP END,
	  target_release_commit, target_image_digest, $3,
	  'sha256:' || repeat('e', 64)
FROM ExternalExecutionTimestampManifest WHERE id = $2`,
				uuid.New(), rootID, state,
			)
			g.Expect(err).To(MatchError(ContainSubstring(
				"invalid initial manifest lifecycle state " + state,
			)))
		})
	}
}

func TestExternalExecutionTimestampMigration138RejectsManifestForks(
	t *testing.T,
) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 138)
	rootID, checksum := insertTimestampManifestFixture(t, database)
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampManifest (
  id, database_identity_checksum, source_schema_version,
  snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity, approved_at, target_release_commit,
  target_image_digest, state, decision_content_checksum
) SELECT
  $1, database_identity_checksum, source_schema_version,
  snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity, approved_at, target_release_commit,
  target_image_digest, state, decision_content_checksum
FROM ExternalExecutionTimestampManifest WHERE id = $2`, uuid.New(), rootID)
	g.Expect(err).To(
		MatchError(ContainSubstring("externalexecutiontimestampmanifest_active_parent_unique")),
	)

	insertChild := func(id uuid.UUID, state string) error {
		_, err := database.pool.Exec(context.Background(), `
INSERT INTO ExternalExecutionTimestampManifest (
  id, supersedes_manifest_id, database_identity_checksum,
  source_schema_version, snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity, approved_at, target_release_commit,
  target_image_digest, state, decision_content_checksum, revoked_at
) SELECT
  $1::uuid, $2, $3, source_schema_version,
  CURRENT_TIMESTAMP - INTERVAL '2 minutes',
  CURRENT_TIMESTAMP - INTERVAL '1 minute',
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity, CURRENT_TIMESTAMP - INTERVAL '30 seconds',
  target_release_commit, target_image_digest, $4,
  'sha256:' || repeat(substr(md5($1::uuid::text), 1, 1), 64),
  CASE WHEN $4 = 'REVOKED_BEFORE_APPLY' THEN CURRENT_TIMESTAMP ELSE NULL END
FROM ExternalExecutionTimestampManifest WHERE id = $2`,
			id, rootID, checksum, state,
		)
		return err
	}

	firstChild := uuid.New()
	g.Expect(insertChild(firstChild, "APPROVED")).To(Succeed())
	g.Expect(insertChild(uuid.New(), "APPROVED")).To(
		MatchError(ContainSubstring("externalexecutiontimestampmanifest_active_parent_unique")),
	)

	_, err = database.pool.Exec(context.Background(), `
UPDATE ExternalExecutionTimestampManifest
SET state = 'REVOKED_BEFORE_APPLY', revoked_at = CURRENT_TIMESTAMP
WHERE id = $1`, firstChild)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(insertChild(uuid.New(), "APPROVED")).To(Succeed())
}

func TestExternalExecutionTimestampMigration138DownRestores137BeforeApply(
	t *testing.T,
) {
	g := NewWithT(t)
	database := newMigrationTestDatabase(t)
	database.migrateTo(t, 137)
	executionID, eventID := insertHistoricalExecutionFixture(t, database)
	database.migrateTo(t, 138)
	database.migrateTo(t, 137)

	var executionExists, eventExists bool
	g.Expect(database.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM ExternalExecution WHERE id = $1)`,
		executionID,
	).Scan(&executionExists)).To(Succeed())
	g.Expect(database.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM ExternalExecutionEvent WHERE id = $1)`,
		eventID,
	).Scan(&eventExists)).To(Succeed())
	g.Expect(executionExists).To(BeTrue())
	g.Expect(eventExists).To(BeTrue())

	var shadowCount int64
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT count(*)
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name IN ('externalexecution', 'externalexecutionevent')
  AND column_name LIKE '%_instant'`).Scan(&shadowCount)).To(Succeed())
	g.Expect(shadowCount).To(Equal(int64(0)))
	var markerExists bool
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT to_regclass('externalexecutiontimestampexpandstate') IS NOT NULL`).
		Scan(&markerExists)).To(Succeed())
	g.Expect(markerExists).To(BeFalse())
	var pairGuardFunctionExists bool
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT to_regprocedure('external_execution_timestamp_pair_guard()') IS NOT NULL`).
		Scan(&pairGuardFunctionExists)).To(Succeed())
	g.Expect(pairGuardFunctionExists).To(BeFalse())
	var pairGuardTriggerCount int64
	g.Expect(database.pool.QueryRow(context.Background(), `
SELECT count(*)
FROM pg_trigger
WHERE NOT tgisinternal
  AND lower(tgname) IN (
    'externalexecution_timestamp_pair_guard',
    'externalexecutionevent_timestamp_pair_guard'
  )`).Scan(&pairGuardTriggerCount)).To(Succeed())
	g.Expect(pairGuardTriggerCount).To(BeZero())
}
