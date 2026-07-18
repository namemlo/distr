package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	stdfs "io/fs"
	"strconv"
	"strings"

	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const externalExecutionTimestampExpandVersion uint = 138

type migrationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func latestMigrationVersion() (uint, error) {
	entries, err := stdfs.ReadDir(fs, "sql")
	if err != nil {
		return 0, fmt.Errorf("read embedded migrations: %w", err)
	}
	var latest uint64
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		separator := strings.IndexByte(name, '_')
		if separator <= 0 {
			return 0, fmt.Errorf("invalid embedded migration name %q", name)
		}
		version, err := strconv.ParseUint(name[:separator], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse embedded migration %q: %w", name, err)
		}
		if version > latest {
			latest = version
		}
	}
	if latest == 0 {
		return 0, errors.New("no embedded migrations found")
	}
	return uint(latest), nil
}

func desiredVersion(options RunOptions, latest uint) uint {
	if options.Down {
		return 0
	}
	if options.Target != nil {
		return *options.Target
	}
	return latest
}

func externalExecutionTimestampPreflight(
	ctx context.Context,
	database migrationQueryer,
	status SchemaStatus,
	options RunOptions,
	latest uint,
) error {
	if status.Dirty {
		return fmt.Errorf("schema version %d is dirty", status.Version)
	}
	if status.Version > int(latest) {
		return fmt.Errorf(
			"current schema %d is newer than latest embedded schema %d",
			status.Version, latest,
		)
	}
	target := desiredVersion(options, latest)
	if options.ExpandManifest != nil &&
		(options.Down || options.Target == nil ||
			*options.Target != externalExecutionTimestampExpandVersion) {
		return errors.New(
			"manifest-assisted migration requires explicit target 138",
		)
	}
	if target > latest && options.ExpandManifest == nil {
		return fmt.Errorf(
			"target schema %d exceeds latest embedded schema %d", target, latest,
		)
	}
	executionExists, err := validateExternalExecutionSchemaState(
		ctx, database, status,
	)
	if err != nil {
		return err
	}

	upwardCrossing := status.Version < int(externalExecutionTimestampExpandVersion) &&
		target >= externalExecutionTimestampExpandVersion
	downwardCrossing := status.Version >= int(externalExecutionTimestampExpandVersion) &&
		target < externalExecutionTimestampExpandVersion
	if options.ExpandManifest != nil && !upwardCrossing {
		return errors.New("timestamp manifest is only valid while crossing schema 138")
	}
	if upwardCrossing {
		return preflightExternalExecutionTimestampUp(
			ctx, database, status, options.ExpandManifest, executionExists,
		)
	}
	if downwardCrossing {
		return preflightExternalExecutionTimestampDown(ctx, database)
	}
	return nil
}

func preflightExternalExecutionTimestampUp(
	ctx context.Context,
	database migrationQueryer,
	status SchemaStatus,
	manifest *types.ExternalExecutionTimestampManifest,
	executionExists bool,
) error {
	if !executionExists {
		if manifest != nil {
			return errors.New("timestamp manifest is not valid for a clean install")
		}
		return nil
	}

	snapshot, err := readExternalExecutionTimestampIdentity(ctx, database)
	if err != nil {
		return err
	}
	if snapshot.ExecutionCount == 0 && snapshot.EventCount == 0 {
		if manifest != nil {
			return errors.New("timestamp manifest is not valid for zero history")
		}
		return nil
	}
	if status.Version < 137 {
		return errors.New(
			"historical external execution data must migrate to exact schema 137 before inspection",
		)
	}
	if status.Version != 137 {
		return fmt.Errorf(
			"schema %d cannot use the schema 138 timestamp preflight", status.Version,
		)
	}
	if manifest == nil {
		return errors.New(
			"an approved manifest is required for historical external execution data",
		)
	}
	return requireExternalExecutionTimestampManifestMatches(*manifest, snapshot)
}

func validateExternalExecutionSchemaState(
	ctx context.Context,
	database migrationQueryer,
	status SchemaStatus,
) (bool, error) {
	executionExists, executionTable, err := relationStatus(
		ctx, database, "externalexecution",
	)
	if err != nil {
		return false, err
	}
	eventExists, eventTable, err := relationStatus(
		ctx, database, "externalexecutionevent",
	)
	if err != nil {
		return false, err
	}
	if executionExists != eventExists ||
		(executionExists && (!executionTable || !eventTable)) {
		return false, errors.New("partial external execution schema")
	}
	if status.Version >= 136 && !executionExists {
		return false, errors.New(
			"schema 136 or later requires ExternalExecution and ExternalExecutionEvent",
		)
	}
	if status.Version < 136 && executionExists {
		if status.Version == -1 {
			return false, errors.New(
				"schema_migrations is absent for an existing application schema",
			)
		}
		return false, errors.New(
			"ExternalExecution and ExternalExecutionEvent exist before schema 136",
		)
	}
	if status.Version == -1 {
		hasApplicationTable, err := applicationOrdinaryTableExists(ctx, database)
		if err != nil {
			return false, err
		}
		if hasApplicationTable {
			return false, errors.New(
				"schema_migrations is absent for an existing application schema",
			)
		}
	}
	return executionExists, nil
}

func applicationOrdinaryTableExists(
	ctx context.Context,
	database migrationQueryer,
) (bool, error) {
	var exists bool
	if err := database.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM pg_catalog.pg_class relation
  JOIN pg_catalog.pg_namespace namespace
    ON namespace.oid=relation.relnamespace
  WHERE namespace.nspname=current_schema()
    AND relation.relkind='r'
    AND relation.relname <> 'schema_migrations'
    AND NOT EXISTS (
      SELECT 1
      FROM pg_catalog.pg_depend dependency
      WHERE dependency.classid='pg_catalog.pg_class'::regclass
        AND dependency.objid=relation.oid
        AND dependency.refclassid='pg_catalog.pg_extension'::regclass
        AND dependency.deptype='e'
    )
)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect existing application schema: %w", err)
	}
	return exists, nil
}

func preflightExternalExecutionTimestampDown(
	ctx context.Context,
	database migrationQueryer,
) error {
	exists, ordinaryTable, err := relationStatus(
		ctx, database, "externalexecutiontimestampmanifest",
	)
	if err != nil {
		return err
	}
	if !exists || !ordinaryTable {
		return errors.New(
			"schema 138 timestamp manifest table is absent or invalid",
		)
	}
	var protectedCount uint64
	if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM ExternalExecutionTimestampManifest
WHERE state IN ('APPLIED', 'VERIFIED')`).Scan(&protectedCount); err != nil {
		return fmt.Errorf("read timestamp manifest lifecycle: %w", err)
	}
	if protectedCount != 0 {
		return errors.New(
			"downgrade crossing 138 is forbidden after timestamp manifest application",
		)
	}
	var tombstoneCount uint64
	if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM ExternalExecutionTimestampDeletionTombstone`).Scan(
		&tombstoneCount,
	); err != nil {
		return fmt.Errorf("read timestamp retention evidence: %w", err)
	}
	if tombstoneCount != 0 {
		return errors.New(
			"downgrade crossing 138 is forbidden after timestamp retention",
		)
	}
	var transitionKind string
	if err := database.QueryRowContext(ctx, `
SELECT transition_kind
FROM ExternalExecutionTimestampExpandState
WHERE singleton`).Scan(&transitionKind); err != nil {
		return fmt.Errorf("read timestamp expand state: %w", err)
	}
	if transitionKind == "ZERO_HISTORY" {
		var executionCount, eventCount uint64
		if err := database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM ExternalExecution),
  (SELECT count(*) FROM ExternalExecutionEvent)`).Scan(
			&executionCount,
			&eventCount,
		); err != nil {
			return fmt.Errorf("read ZERO_HISTORY timestamp rows: %w", err)
		}
		if executionCount != 0 || eventCount != 0 {
			return errors.New(
				"downgrade crossing 138 is forbidden after ZERO_HISTORY timestamp rows",
			)
		}
	}
	return nil
}

func relationStatus(
	ctx context.Context,
	database migrationQueryer,
	relationName string,
) (exists bool, ordinaryTable bool, err error) {
	err = database.QueryRowContext(ctx, `
SELECT
  to_regclass(format('%I.' || $1, current_schema())) IS NOT NULL,
  COALESCE((
    SELECT relation.relkind = 'r'
    FROM pg_catalog.pg_class relation
    WHERE relation.oid =
      to_regclass(format('%I.' || $1, current_schema()))
  ), FALSE)`, relationName).Scan(&exists, &ordinaryTable)
	if err != nil {
		return false, false, fmt.Errorf(
			"inspect relation %s: %w", relationName, err,
		)
	}
	return exists, ordinaryTable, nil
}

type externalExecutionTimestampIdentity struct {
	SourceSchemaVersion      uint
	ExecutionCount           uint64
	EventCount               uint64
	RawCellCount             uint64
	RawCellChecksum          string
	DatabaseIdentityChecksum string
}

const migrationExternalExecutionTimestampRawCellsSQL = `
SELECT source_table, source_row_id, source_column, column_ordinal, raw_value
FROM (
  SELECT
    'externalexecution'::text AS source_table,
    execution.id AS source_row_id,
    cell.source_column,
    cell.column_ordinal,
    CASE WHEN cell.raw_value IS NULL THEN NULL ELSE
      to_char(cell.raw_value, 'YYYY-MM-DD"T"HH24:MI:SS.US')
    END AS raw_value
  FROM ExternalExecution execution
  CROSS JOIN LATERAL (VALUES
    ('created_at'::text, 1::smallint, execution.created_at),
    ('updated_at'::text, 2::smallint, execution.updated_at),
    ('started_at'::text, 3::smallint, execution.started_at),
    ('completed_at'::text, 4::smallint, execution.completed_at),
    ('callback_deadline_at'::text, 5::smallint,
      execution.callback_deadline_at)
  ) AS cell(source_column, column_ordinal, raw_value)
  UNION ALL
  SELECT
    'externalexecutionevent'::text,
    event.id,
    'created_at'::text,
    6::smallint,
    CASE WHEN event.created_at IS NULL THEN NULL ELSE
      to_char(event.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US')
    END
  FROM ExternalExecutionEvent event
) cells
ORDER BY source_table, source_row_id, column_ordinal`

//nolint:dupl // Preflight intentionally keeps an independent read-only identity path from dirty recovery.
func readExternalExecutionTimestampIdentity(
	ctx context.Context,
	database migrationQueryer,
) (externalExecutionTimestampIdentity, error) {
	rows, err := database.QueryContext(
		ctx, migrationExternalExecutionTimestampRawCellsSQL,
	)
	if err != nil {
		return externalExecutionTimestampIdentity{},
			fmt.Errorf("read external execution timestamp cells: %w", err)
	}
	defer rows.Close()
	rawCells := make([]types.ExternalExecutionTimestampRawCell, 0)
	executionIDs := make([]uuid.UUID, 0)
	eventIDs := make([]uuid.UUID, 0)
	seenExecutions := make(map[uuid.UUID]struct{})
	seenEvents := make(map[uuid.UUID]struct{})
	for rows.Next() {
		var sourceTable, sourceColumn string
		var sourceRowID uuid.UUID
		var columnOrdinal int16
		var rawValue sql.NullString
		if err := rows.Scan(
			&sourceTable,
			&sourceRowID,
			&sourceColumn,
			&columnOrdinal,
			&rawValue,
		); err != nil {
			return externalExecutionTimestampIdentity{},
				fmt.Errorf("scan external execution timestamp cell: %w", err)
		}
		cell := types.ExternalExecutionTimestampRawCell{
			SourceTable:   sourceTable,
			SourceRowID:   sourceRowID,
			SourceColumn:  sourceColumn,
			ColumnOrdinal: uint8(columnOrdinal),
		}
		if rawValue.Valid {
			value := rawValue.String
			cell.RawValue = &value
		}
		cell.RawCellChecksum, err = externalexecutiontimestamp.ComputeRawCellChecksum(cell)
		if err != nil {
			return externalExecutionTimestampIdentity{}, err
		}
		rawCells = append(rawCells, cell)
		switch sourceTable {
		case "externalexecution":
			if _, exists := seenExecutions[sourceRowID]; !exists {
				seenExecutions[sourceRowID] = struct{}{}
				executionIDs = append(executionIDs, sourceRowID)
			}
		case "externalexecutionevent":
			if _, exists := seenEvents[sourceRowID]; !exists {
				seenEvents[sourceRowID] = struct{}{}
				eventIDs = append(eventIDs, sourceRowID)
			}
		default:
			return externalExecutionTimestampIdentity{}, fmt.Errorf(
				"unexpected timestamp source table %q", sourceTable,
			)
		}
	}
	if err := rows.Err(); err != nil {
		return externalExecutionTimestampIdentity{},
			fmt.Errorf("iterate external execution timestamp cells: %w", err)
	}
	rawChecksum, err := externalexecutiontimestamp.ComputeRawSetChecksum(rawCells)
	if err != nil {
		return externalExecutionTimestampIdentity{}, err
	}
	identityChecksum, err := externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
		137,
		executionIDs,
		eventIDs,
		uint64(len(rawCells)),
		rawChecksum,
	)
	if err != nil {
		return externalExecutionTimestampIdentity{}, err
	}
	return externalExecutionTimestampIdentity{
		SourceSchemaVersion:      137,
		ExecutionCount:           uint64(len(executionIDs)),
		EventCount:               uint64(len(eventIDs)),
		RawCellCount:             uint64(len(rawCells)),
		RawCellChecksum:          rawChecksum,
		DatabaseIdentityChecksum: identityChecksum,
	}, nil
}

func requireExternalExecutionTimestampManifestMatches(
	manifest types.ExternalExecutionTimestampManifest,
	snapshot externalExecutionTimestampIdentity,
) error {
	if manifest.State != types.ExternalExecutionTimestampManifestStateApproved {
		return errors.New("approved manifest is required")
	}
	if manifest.SupersedesManifestID != nil {
		return errors.New("schema 138 crossing requires a root approved manifest")
	}
	if problems := externalexecutiontimestamp.ValidateManifestDocument(manifest); len(problems) != 0 {
		return fmt.Errorf("approved manifest is invalid: %w", errors.Join(problems...))
	}
	if manifest.SourceSchemaVersion != snapshot.SourceSchemaVersion {
		return errors.New("manifest source schema does not match fresh database identity")
	}
	if manifest.ExecutionCount != snapshot.ExecutionCount ||
		manifest.EventCount != snapshot.EventCount ||
		manifest.RawCellCount != snapshot.RawCellCount {
		return errors.New("manifest row counts do not match fresh database identity")
	}
	if manifest.RawCellChecksum != snapshot.RawCellChecksum {
		return errors.New("manifest raw checksum does not match fresh database identity")
	}
	if manifest.DatabaseIdentityChecksum != snapshot.DatabaseIdentityChecksum {
		return errors.New("manifest database identity checksum does not match fresh database identity")
	}
	return nil
}
