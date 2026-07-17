package db

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/buildconfig"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	externalExecutionTimestampTrySessionLockSQL      = `SELECT pg_try_advisory_lock(@migrationAdvisoryLockKey)`
	externalExecutionTimestampUnlockSessionSQL       = `SELECT pg_advisory_unlock(@migrationAdvisoryLockKey)`
	externalExecutionTimestampSessionLockTimeout     = 10 * time.Second
	externalExecutionTimestampSessionLockPoll        = 100 * time.Millisecond
	externalExecutionTimestampCleanupTimeout         = 5 * time.Second
	externalExecutionTimestampRejectTruncateFunction = "external_execution_timestamp_reject_truncate"
	externalExecutionTimestampRejectTruncateBodyHash = "f3e3a6a3efe319c199d432db99ca0151bfae72a44523e31100b4253419e41852"
)

const insertExternalExecutionTimestampManifestSQL = `
INSERT INTO ExternalExecutionTimestampManifest (
  id, supersedes_manifest_id, database_identity_checksum,
  source_schema_version, snapshot_started_at, snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity, approved_at, target_release_commit,
  target_image_digest, state, decision_content_checksum
) VALUES (
  @id, @supersedesManifestId, @databaseIdentityChecksum,
  @sourceSchemaVersion, CAST(@snapshotStartedAt AS timestamptz),
  CAST(@snapshotEndedAt AS timestamptz), @executionCount, @eventCount,
  @rawCellCount, @populatedCellCount, @rawCellChecksum,
  @evidenceBundleReference, @evidenceBundleChecksum, @toolVersion,
  @conversionExpressionVersion, @authorIdentity, @reviewerIdentity,
  CAST(@approvedAt AS timestamptz), @targetReleaseCommit,
  @targetImageDigest, 'APPROVED', @decisionContentChecksum
)`

const insertExternalExecutionTimestampProvenanceSQL = `
INSERT INTO ExternalExecutionTimestampCellProvenance (
  manifest_id, source_table, source_row_id, source_column,
  column_ordinal, raw_value, raw_is_null, decision, source_zone,
  source_offset_seconds, converted_value, evidence_reference,
  evidence_checksum, approving_identity, raw_cell_checksum,
  parent_manifest_checksum, conversion_expression_version
) VALUES (
  @manifestId, @sourceTable, @sourceRowId, @sourceColumn,
  @columnOrdinal, CAST(@rawValue AS timestamp without time zone),
  @rawIsNull, @decision, @sourceZone, @sourceOffsetSeconds,
  CAST(@convertedValue AS timestamptz), @evidenceReference,
  @evidenceChecksum, @approvingIdentity, @rawCellChecksum,
  @parentManifestChecksum, @conversionExpressionVersion
)`

const updateExecutionCreatedInstantSQL = `
UPDATE ExternalExecution SET created_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND created_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (created_at_instant IS NULL OR
     created_at_instant=CAST(@converted AS timestamptz))`

const updateExecutionUpdatedInstantSQL = `
UPDATE ExternalExecution SET updated_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND updated_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (updated_at_instant IS NULL OR
     updated_at_instant=CAST(@converted AS timestamptz))`

const updateExecutionStartedInstantSQL = `
UPDATE ExternalExecution SET started_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND started_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (started_at_instant IS NULL OR
     started_at_instant=CAST(@converted AS timestamptz))`

const updateExecutionCompletedInstantSQL = `
UPDATE ExternalExecution SET completed_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND completed_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (completed_at_instant IS NULL OR
     completed_at_instant=CAST(@converted AS timestamptz))`

const updateExecutionDeadlineInstantSQL = `
UPDATE ExternalExecution
SET callback_deadline_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND callback_deadline_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (callback_deadline_at_instant IS NULL OR
     callback_deadline_at_instant=CAST(@converted AS timestamptz))`

const updateEventCreatedInstantSQL = `
UPDATE ExternalExecutionEvent
SET created_at_instant=CAST(@converted AS timestamptz)
WHERE id=@rowId AND created_at IS NOT DISTINCT FROM
  CAST(@raw AS timestamp without time zone)
AND (created_at_instant IS NULL OR
     created_at_instant=CAST(@converted AS timestamptz))`

const externalExecutionTimestampVerifiedTipsSQL = `
SELECT manifest.id
FROM ExternalExecutionTimestampManifest manifest
WHERE manifest.state = 'VERIFIED'
  AND NOT EXISTS (
    SELECT 1 FROM ExternalExecutionTimestampManifest child
    WHERE child.supersedes_manifest_id = manifest.id
      AND child.state <> 'REVOKED_BEFORE_APPLY'
  )
ORDER BY manifest.verified_at DESC, manifest.id`

const storedExternalExecutionTimestampManifestSQL = `
SELECT id, supersedes_manifest_id, database_identity_checksum,
  source_schema_version,
  to_char(snapshot_started_at AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' AS snapshot_started_at,
  to_char(snapshot_ended_at AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' AS snapshot_ended_at,
  execution_count, event_count, raw_cell_count, populated_cell_count,
  raw_cell_checksum, evidence_bundle_reference, evidence_bundle_checksum,
  tool_version, conversion_expression_version, author_identity,
  reviewer_identity,
  to_char(approved_at AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' AS approved_at,
  target_release_commit, target_image_digest, state,
  decision_content_checksum,
  to_char(verified_at AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' AS verified_at
FROM ExternalExecutionTimestampManifest WHERE id=@manifestId`

const storedExternalExecutionTimestampProvenanceSQL = `
SELECT source_table, source_row_id, source_column, column_ordinal,
  CASE WHEN raw_is_null THEN NULL ELSE
    to_char(raw_value, 'YYYY-MM-DD"T"HH24:MI:SS.US') END AS raw_value,
  decision, source_zone, source_offset_seconds,
  CASE WHEN converted_value IS NULL THEN NULL ELSE
    to_char(converted_value AT TIME ZONE 'UTC',
      'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' END AS converted_value,
  evidence_reference, evidence_checksum, approving_identity,
  raw_cell_checksum, conversion_expression_version
FROM ExternalExecutionTimestampCellProvenance
WHERE manifest_id=@manifestId
ORDER BY source_table, source_row_id, column_ordinal`

const currentExternalExecutionTimestampCellsSQL = `
SELECT source_table, source_row_id, source_column, column_ordinal,
  CASE WHEN raw_value IS NULL THEN NULL ELSE
    to_char(raw_value, 'YYYY-MM-DD"T"HH24:MI:SS.US') END AS raw_value,
  CASE WHEN instant_value IS NULL THEN NULL ELSE
    to_char(instant_value AT TIME ZONE 'UTC',
      'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' END AS instant_value,
  CASE WHEN row_created_instant IS NULL THEN NULL ELSE
    to_char(row_created_instant AT TIME ZONE 'UTC',
      'YYYY-MM-DD"T"HH24:MI:SS.US') || 'Z' END AS row_created_instant
FROM (
  SELECT 'externalexecution'::text AS source_table,
    execution.id AS source_row_id, cell.source_column,
    cell.column_ordinal, cell.raw_value, cell.instant_value,
    execution.created_at_instant AS row_created_instant
  FROM ExternalExecution execution
  CROSS JOIN LATERAL (VALUES
    ('created_at'::text, 1::smallint,
      execution.created_at, execution.created_at_instant),
    ('updated_at'::text, 2::smallint,
      execution.updated_at, execution.updated_at_instant),
    ('started_at'::text, 3::smallint,
      execution.started_at, execution.started_at_instant),
    ('completed_at'::text, 4::smallint,
      execution.completed_at, execution.completed_at_instant),
    ('callback_deadline_at'::text, 5::smallint,
      execution.callback_deadline_at,
      execution.callback_deadline_at_instant)
  ) AS cell(source_column, column_ordinal, raw_value, instant_value)
  UNION ALL
  SELECT 'externalexecutionevent'::text, event.id, 'created_at'::text,
    6::smallint, event.created_at, event.created_at_instant,
    event.created_at_instant
  FROM ExternalExecutionEvent event
) current_cells
ORDER BY source_table, source_row_id, column_ordinal`

type storedExternalExecutionTimestampManifestRow struct {
	ID                          uuid.UUID                                     `db:"id"`
	SupersedesManifestID        *uuid.UUID                                    `db:"supersedes_manifest_id"`
	DatabaseIdentityChecksum    string                                        `db:"database_identity_checksum"`
	SourceSchemaVersion         uint                                          `db:"source_schema_version"`
	SnapshotStartedAt           string                                        `db:"snapshot_started_at"`
	SnapshotEndedAt             string                                        `db:"snapshot_ended_at"`
	ExecutionCount              uint64                                        `db:"execution_count"`
	EventCount                  uint64                                        `db:"event_count"`
	RawCellCount                uint64                                        `db:"raw_cell_count"`
	PopulatedCellCount          uint64                                        `db:"populated_cell_count"`
	RawCellChecksum             string                                        `db:"raw_cell_checksum"`
	EvidenceBundleReference     string                                        `db:"evidence_bundle_reference"`
	EvidenceBundleChecksum      string                                        `db:"evidence_bundle_checksum"`
	ToolVersion                 string                                        `db:"tool_version"`
	ConversionExpressionVersion string                                        `db:"conversion_expression_version"`
	AuthorIdentity              string                                        `db:"author_identity"`
	ReviewerIdentity            string                                        `db:"reviewer_identity"`
	ApprovedAt                  string                                        `db:"approved_at"`
	TargetReleaseCommit         string                                        `db:"target_release_commit"`
	TargetImageDigest           string                                        `db:"target_image_digest"`
	State                       types.ExternalExecutionTimestampManifestState `db:"state"`
	DecisionContentChecksum     string                                        `db:"decision_content_checksum"`
	VerifiedAt                  *string                                       `db:"verified_at"`
}

type storedExternalExecutionTimestampProvenanceRow struct {
	SourceTable                 string                                   `db:"source_table"`
	SourceRowID                 uuid.UUID                                `db:"source_row_id"`
	SourceColumn                string                                   `db:"source_column"`
	ColumnOrdinal               int16                                    `db:"column_ordinal"`
	RawValue                    *string                                  `db:"raw_value"`
	Decision                    types.ExternalExecutionTimestampDecision `db:"decision"`
	SourceZone                  *string                                  `db:"source_zone"`
	SourceOffsetSeconds         *int32                                   `db:"source_offset_seconds"`
	ConvertedValue              *string                                  `db:"converted_value"`
	EvidenceReference           *string                                  `db:"evidence_reference"`
	EvidenceChecksum            *string                                  `db:"evidence_checksum"`
	ApprovingIdentity           *string                                  `db:"approving_identity"`
	RawCellChecksum             string                                   `db:"raw_cell_checksum"`
	ConversionExpressionVersion string                                   `db:"conversion_expression_version"`
}

type currentExternalExecutionTimestampCell struct {
	SourceTable       string    `db:"source_table"`
	SourceRowID       uuid.UUID `db:"source_row_id"`
	SourceColumn      string    `db:"source_column"`
	ColumnOrdinal     int16     `db:"column_ordinal"`
	RawValue          *string   `db:"raw_value"`
	InstantValue      *string   `db:"instant_value"`
	RowCreatedInstant *string   `db:"row_created_instant"`
}

type externalExecutionTimestampExpandState struct {
	TransitionKind string    `db:"transition_kind"`
	SourceVersion  uint      `db:"source_schema_version"`
	ExecutionCount uint64    `db:"transition_execution_count"`
	EventCount     uint64    `db:"transition_event_count"`
	RawCellCount   uint64    `db:"transition_raw_cell_count"`
	TransitionedAt time.Time `db:"transitioned_at"`
}

type externalExecutionTimestampGuardSpec struct {
	errorLabel       string
	functionName     string
	functionBodyHash string
	tableName        string
	triggerName      string
	triggerType      int16
	updateColumns    []string
}

type externalExecutionTimestampTriggerSetSpec struct {
	tableName    string
	triggerCount int64
}

const externalExecutionTimestampReadinessIndexesSQL = `
WITH expected(index_name, table_name, key_columns, key_options) AS (
  VALUES
    ('externalexecution_organization_status', 'externalexecution',
      ARRAY['organization_id', 'status', 'updated_at', 'id']::text[],
      ARRAY[0, 0, 3, 0]::smallint[]),
    ('externalexecution_task', 'externalexecution',
      ARRAY['task_id', 'created_at', 'id']::text[],
      ARRAY[0, 0, 0]::smallint[]),
    ('externalexecutionevent_execution_sequence', 'externalexecutionevent',
      ARRAY['external_execution_id', 'sequence', 'id']::text[],
      ARRAY[0, 0, 0]::smallint[]),
    ('externalexecution_organization_status_instant_next',
      'externalexecution',
      ARRAY['organization_id', 'status', 'updated_at_instant', 'id']::text[],
      ARRAY[0, 0, 3, 0]::smallint[]),
    ('externalexecution_task_instant_next', 'externalexecution',
      ARRAY['task_id', 'created_at_instant', 'id']::text[],
      ARRAY[0, 0, 0]::smallint[])
)
SELECT count(*)
FROM expected
JOIN pg_class index_class
  ON index_class.relname=expected.index_name
 AND index_class.relkind='i'
JOIN pg_namespace index_namespace
  ON index_namespace.oid=index_class.relnamespace
 AND index_namespace.nspname=current_schema()
JOIN pg_index index_row ON index_row.indexrelid=index_class.oid
JOIN pg_class table_class
  ON table_class.oid=index_row.indrelid
 AND table_class.relname=expected.table_name
 AND table_class.relkind='r'
JOIN pg_namespace table_namespace
  ON table_namespace.oid=table_class.relnamespace
 AND table_namespace.nspname=current_schema()
JOIN pg_am access_method
  ON access_method.oid=index_class.relam
 AND access_method.amname='btree'
WHERE index_row.indisvalid
  AND index_row.indisready
  AND index_row.indislive
  AND NOT index_row.indisunique
  AND NOT index_row.indisprimary
  AND NOT index_row.indisexclusion
  AND index_row.indpred IS NULL
  AND index_row.indexprs IS NULL
  AND index_row.indnatts=index_row.indnkeyatts
  AND index_row.indnkeyatts=cardinality(expected.key_columns)
  AND (
    SELECT array_agg(
      lower(pg_get_indexdef(index_row.indexrelid, ordinal, true))
      ORDER BY ordinal
    )
    FROM generate_series(1, index_row.indnkeyatts::integer) ordinal
  )=expected.key_columns
  AND array_to_string(index_row.indoption::smallint[], ',') =
    array_to_string(expected.key_options, ',')`

const externalExecutionTimestampExpandStateSQL = `
SELECT transition_kind, source_schema_version,
  transition_execution_count, transition_event_count,
  transition_raw_cell_count, transitioned_at
FROM ExternalExecutionTimestampExpandState`

const externalExecutionTimestampGuardFunctionSQL = `
SELECT function_row.prosrc
FROM pg_proc function_row
JOIN pg_namespace function_namespace
  ON function_namespace.oid=function_row.pronamespace
 AND function_namespace.nspname=current_schema()
JOIN pg_language function_language
  ON function_language.oid=function_row.prolang
 AND function_language.lanname='plpgsql'
WHERE function_row.proname=@functionName
  AND function_row.pronargs=0
  AND function_row.prorettype='trigger'::regtype
  AND function_row.prokind='f'
  AND function_row.provolatile='v'
  AND NOT function_row.prosecdef
  AND function_row.proconfig IS NULL
  AND NOT function_row.proleakproof
  AND NOT function_row.proisstrict
  AND function_row.proparallel='u'`

const externalExecutionTimestampGuardTriggerSQL = `
SELECT count(*)
FROM pg_trigger trigger_row
JOIN pg_class table_row
  ON table_row.oid=trigger_row.tgrelid
 AND table_row.relname=@tableName
 AND table_row.relkind='r'
JOIN pg_namespace table_namespace
  ON table_namespace.oid=table_row.relnamespace
 AND table_namespace.nspname=current_schema()
JOIN pg_proc function_row
  ON function_row.oid=trigger_row.tgfoid
 AND function_row.proname=@functionName
JOIN pg_namespace function_namespace
  ON function_namespace.oid=function_row.pronamespace
 AND function_namespace.nspname=current_schema()
WHERE trigger_row.tgname=@triggerName
  AND NOT trigger_row.tgisinternal
  AND trigger_row.tgenabled='O'
  AND trigger_row.tgtype=@triggerType
  AND trigger_row.tgnargs=0
  AND trigger_row.tgqual IS NULL
  AND array_to_string(trigger_row.tgattr::smallint[], ',') = COALESCE((
    SELECT string_agg(attribute_row.attnum::text, ',' ORDER BY requested.ordinal)
    FROM unnest(@updateColumns::text[]) WITH ORDINALITY
      requested(column_name, ordinal)
    JOIN pg_attribute attribute_row
      ON attribute_row.attrelid=table_row.oid
     AND attribute_row.attname=requested.column_name
     AND NOT attribute_row.attisdropped
  ), '')`

const externalExecutionTimestampNonInternalTriggerCountSQL = `
SELECT count(*)
FROM pg_trigger trigger_row
JOIN pg_class table_row
  ON table_row.oid=trigger_row.tgrelid
 AND table_row.relname=@tableName
 AND table_row.relkind='r'
JOIN pg_namespace table_namespace
  ON table_namespace.oid=table_row.relnamespace
 AND table_namespace.nspname=current_schema()
WHERE NOT trigger_row.tgisinternal`

var externalExecutionTimestampGuardSpecs = []externalExecutionTimestampGuardSpec{
	{
		errorLabel:       "provenance guard",
		functionName:     "external_execution_timestamp_provenance_append_only",
		functionBodyHash: "a07a7f8724882ba9dd00aa22f1d1c9808daa22c3233a8fa8cdbeb49ce9259739",
		tableName:        "externalexecutiontimestampcellprovenance",
		triggerName:      "externalexecutiontimestampcellprovenance_append_only",
		triggerType:      27,
	},
	{
		errorLabel:       "expand state",
		functionName:     "external_execution_timestamp_expand_state_append_only",
		functionBodyHash: "59160ab340a6d88434a559ae95ac44ad19656e4dd9622449c805626cddeb9ec9",
		tableName:        "externalexecutiontimestampexpandstate",
		triggerName:      "externalexecutiontimestampexpandstate_append_only",
		triggerType:      27,
	},
	{
		errorLabel:       "execution timestamp pair guard",
		functionName:     "external_execution_timestamp_pair_guard",
		functionBodyHash: "3203c6ef209d179789667ba3e153013ea0009a944ca4d3f40e99a2ecdf2b27d9",
		tableName:        "externalexecution",
		triggerName:      "externalexecution_timestamp_pair_guard",
		triggerType:      23,
		updateColumns: []string{
			"created_at", "created_at_instant",
			"updated_at", "updated_at_instant",
			"started_at", "started_at_instant",
			"completed_at", "completed_at_instant",
			"callback_deadline_at", "callback_deadline_at_instant",
		},
	},
	{
		errorLabel:       "event timestamp pair guard",
		functionName:     "external_execution_timestamp_pair_guard",
		functionBodyHash: "3203c6ef209d179789667ba3e153013ea0009a944ca4d3f40e99a2ecdf2b27d9",
		tableName:        "externalexecutionevent",
		triggerName:      "externalexecutionevent_timestamp_pair_guard",
		triggerType:      23,
		updateColumns:    []string{"created_at", "created_at_instant"},
	},
	{
		errorLabel:       "lifecycle trigger",
		functionName:     "external_execution_lifecycle_pair_one_shot",
		functionBodyHash: "add0ab160477869f8e3ff881623ec7a6fd8d37cd28864cbabdd6be0acdf3d2e3",
		tableName:        "externalexecution",
		triggerName:      "externalexecution_lifecycle_pair_one_shot",
		triggerType:      19,
		updateColumns: []string{
			"started_at", "started_at_instant",
			"completed_at", "completed_at_instant",
		},
	},
	{
		errorLabel:       "manifest lifecycle",
		functionName:     "external_execution_timestamp_manifest_lifecycle",
		functionBodyHash: "a496fb4bb76835006a10ee70a2dc9fde1bd930508cf1ccbf60a0c395ff230632",
		tableName:        "externalexecutiontimestampmanifest",
		triggerName:      "externalexecutiontimestampmanifest_lifecycle",
		triggerType:      31,
	},
	{
		errorLabel:       "expand state truncate guard",
		functionName:     externalExecutionTimestampRejectTruncateFunction,
		functionBodyHash: externalExecutionTimestampRejectTruncateBodyHash,
		tableName:        "externalexecutiontimestampexpandstate",
		triggerName:      "externalexecutiontimestampexpandstate_reject_truncate",
		triggerType:      34,
	},
	{
		errorLabel:       "manifest truncate guard",
		functionName:     externalExecutionTimestampRejectTruncateFunction,
		functionBodyHash: externalExecutionTimestampRejectTruncateBodyHash,
		tableName:        "externalexecutiontimestampmanifest",
		triggerName:      "externalexecutiontimestampmanifest_reject_truncate",
		triggerType:      34,
	},
	{
		errorLabel:       "provenance truncate guard",
		functionName:     externalExecutionTimestampRejectTruncateFunction,
		functionBodyHash: externalExecutionTimestampRejectTruncateBodyHash,
		tableName:        "externalexecutiontimestampcellprovenance",
		triggerName:      "externalexecutiontimestampcellprovenance_reject_truncate",
		triggerType:      34,
	},
}

var externalExecutionTimestampBusinessTriggerSetSpecs = []externalExecutionTimestampTriggerSetSpec{
	{tableName: "externalexecution", triggerCount: 2},
	{tableName: "externalexecutionevent", triggerCount: 1},
}

const externalExecutionTimestampCatalogSQL = `
WITH required_legacy(table_name, column_name, nullable) AS (
  VALUES
    ('externalexecution', 'created_at', false),
    ('externalexecution', 'updated_at', false),
    ('externalexecution', 'started_at', true),
    ('externalexecution', 'completed_at', true),
    ('externalexecution', 'callback_deadline_at', false),
    ('externalexecutionevent', 'created_at', false)
), required_shadow(table_name, column_name) AS (
  VALUES
    ('externalexecution', 'created_at_instant'),
    ('externalexecution', 'updated_at_instant'),
    ('externalexecution', 'started_at_instant'),
    ('externalexecution', 'completed_at_instant'),
    ('externalexecution', 'callback_deadline_at_instant'),
    ('externalexecutionevent', 'created_at_instant')
), required_expand_table(table_name) AS (
  VALUES
    ('externalexecutiontimestampmanifest'),
    ('externalexecutiontimestampcellprovenance'),
    ('externalexecutiontimestampexpandstate'),
    ('externalexecutiontimestampcontractgate')
), required_expand_column(table_name, column_name, data_type, nullable) AS (
  VALUES
    ('externalexecutiontimestampmanifest', 'id', 'uuid', false),
    ('externalexecutiontimestampmanifest', 'supersedes_manifest_id', 'uuid', true),
    ('externalexecutiontimestampmanifest', 'database_identity_checksum', 'text', false),
    ('externalexecutiontimestampmanifest', 'source_schema_version', 'integer', false),
    ('externalexecutiontimestampmanifest', 'snapshot_started_at', 'timestamp with time zone', false),
    ('externalexecutiontimestampmanifest', 'snapshot_ended_at', 'timestamp with time zone', false),
    ('externalexecutiontimestampmanifest', 'execution_count', 'bigint', false),
    ('externalexecutiontimestampmanifest', 'event_count', 'bigint', false),
    ('externalexecutiontimestampmanifest', 'raw_cell_count', 'bigint', false),
    ('externalexecutiontimestampmanifest', 'populated_cell_count', 'bigint', false),
    ('externalexecutiontimestampmanifest', 'raw_cell_checksum', 'text', false),
    ('externalexecutiontimestampmanifest', 'evidence_bundle_reference', 'text', true),
    ('externalexecutiontimestampmanifest', 'evidence_bundle_checksum', 'text', true),
    ('externalexecutiontimestampmanifest', 'tool_version', 'text', false),
    ('externalexecutiontimestampmanifest', 'conversion_expression_version', 'text', false),
    ('externalexecutiontimestampmanifest', 'author_identity', 'text', true),
    ('externalexecutiontimestampmanifest', 'reviewer_identity', 'text', true),
    ('externalexecutiontimestampmanifest', 'approved_at', 'timestamp with time zone', true),
    ('externalexecutiontimestampmanifest', 'target_release_commit', 'text', true),
    ('externalexecutiontimestampmanifest', 'target_image_digest', 'text', true),
    ('externalexecutiontimestampmanifest', 'state', 'text', false),
    ('externalexecutiontimestampmanifest', 'decision_content_checksum', 'text', false),
    ('externalexecutiontimestampmanifest', 'created_at', 'timestamp with time zone', false),
    ('externalexecutiontimestampmanifest', 'applied_at', 'timestamp with time zone', true),
    ('externalexecutiontimestampmanifest', 'verified_at', 'timestamp with time zone', true),
    ('externalexecutiontimestampmanifest', 'revoked_at', 'timestamp with time zone', true),
    ('externalexecutiontimestampcellprovenance', 'manifest_id', 'uuid', false),
    ('externalexecutiontimestampcellprovenance', 'source_table', 'text', false),
    ('externalexecutiontimestampcellprovenance', 'source_row_id', 'uuid', false),
    ('externalexecutiontimestampcellprovenance', 'source_column', 'text', false),
    ('externalexecutiontimestampcellprovenance', 'column_ordinal', 'smallint', false),
    ('externalexecutiontimestampcellprovenance', 'raw_value', 'timestamp without time zone', true),
    ('externalexecutiontimestampcellprovenance', 'raw_is_null', 'boolean', false),
    ('externalexecutiontimestampcellprovenance', 'decision', 'text', false),
    ('externalexecutiontimestampcellprovenance', 'source_zone', 'text', true),
    ('externalexecutiontimestampcellprovenance', 'source_offset_seconds', 'integer', true),
    ('externalexecutiontimestampcellprovenance', 'converted_value', 'timestamp with time zone', true),
    ('externalexecutiontimestampcellprovenance', 'evidence_reference', 'text', true),
    ('externalexecutiontimestampcellprovenance', 'evidence_checksum', 'text', true),
    ('externalexecutiontimestampcellprovenance', 'approving_identity', 'text', true),
    ('externalexecutiontimestampcellprovenance', 'raw_cell_checksum', 'text', false),
    ('externalexecutiontimestampcellprovenance', 'parent_manifest_checksum', 'text', false),
    ('externalexecutiontimestampcellprovenance', 'conversion_expression_version', 'text', false),
    ('externalexecutiontimestampcellprovenance', 'created_at', 'timestamp with time zone', false),
    ('externalexecutiontimestampexpandstate', 'singleton', 'boolean', false),
    ('externalexecutiontimestampexpandstate', 'transition_kind', 'text', false),
    ('externalexecutiontimestampexpandstate', 'source_schema_version', 'integer', false),
    ('externalexecutiontimestampexpandstate', 'transition_execution_count', 'bigint', false),
    ('externalexecutiontimestampexpandstate', 'transition_event_count', 'bigint', false),
    ('externalexecutiontimestampexpandstate', 'transition_raw_cell_count', 'bigint', false),
    ('externalexecutiontimestampexpandstate', 'transitioned_at', 'timestamp with time zone', false),
    ('externalexecutiontimestampcontractgate', 'id', 'uuid', false),
    ('externalexecutiontimestampcontractgate', 'manifest_id', 'uuid', false),
    ('externalexecutiontimestampcontractgate', 'expected_schema_version', 'integer', false),
    ('externalexecutiontimestampcontractgate', 'contract_migration_version', 'integer', false),
    ('externalexecutiontimestampcontractgate', 'target_release_commit', 'text', false),
    ('externalexecutiontimestampcontractgate', 'target_image_digest', 'text', false),
    ('externalexecutiontimestampcontractgate', 'backup_reference', 'text', false),
    ('externalexecutiontimestampcontractgate', 'backup_checksum', 'text', false),
    ('externalexecutiontimestampcontractgate', 'restore_verification_reference', 'text', false),
    ('externalexecutiontimestampcontractgate', 'restore_verification_checksum', 'text', false),
    ('externalexecutiontimestampcontractgate', 'writer_fence_identifier', 'text', false),
    ('externalexecutiontimestampcontractgate', 'prepared_by', 'text', false),
    ('externalexecutiontimestampcontractgate', 'prepared_at', 'timestamp with time zone', false),
    ('externalexecutiontimestampcontractgate', 'expires_at', 'timestamp with time zone', false),
    ('externalexecutiontimestampcontractgate', 'consumed_at', 'timestamp with time zone', true)
), required_expand_constraint(
  table_name, constraint_name, constraint_type, constraint_definition
) AS (
  VALUES
    ('externalexecutiontimestampmanifest',
      'externalexecutiontimestampmanifest_pkey', 'p', 'PRIMARY KEY (id)'),
    ('externalexecutiontimestampmanifest',
      'externalexecutiontimestampmanifest_supersedes_manifest_id_fkey', 'f',
      'FOREIGN KEY (supersedes_manifest_id) REFERENCES ' ||
      'externalexecutiontimestampmanifest(id) ON UPDATE RESTRICT ON DELETE RESTRICT'),
    ('externalexecutiontimestampmanifest',
      'externalexecutiontimestampmanifest_id_checksum_unique', 'u',
      'UNIQUE (id, decision_content_checksum)'),
    ('externalexecutiontimestampmanifest',
      'externalexecutiontimestampmanifest_source_schema_137', 'c',
      'CHECK ((source_schema_version = 137))'),
    ('externalexecutiontimestampcellprovenance',
      'externalexecutiontimestampcellprovenance_pkey', 'p',
      'PRIMARY KEY (manifest_id, source_table, source_row_id, source_column)'),
    ('externalexecutiontimestampcellprovenance',
      'externalexecutiontimestampcell_manifest_checksum_fk', 'f',
      'FOREIGN KEY (manifest_id, parent_manifest_checksum) REFERENCES ' ||
      'externalexecutiontimestampmanifest(id, decision_content_checksum) ' ||
      'ON UPDATE RESTRICT ON DELETE RESTRICT'),
    ('externalexecutiontimestampcellprovenance',
      'externalexecutiontimestampcell_raw_null_match', 'c',
      'CHECK ((raw_is_null = (raw_value IS NULL)))'),
    ('externalexecutiontimestampexpandstate',
      'externalexecutiontimestampexpandstate_pkey', 'p',
      'PRIMARY KEY (singleton)'),
    ('externalexecutiontimestampexpandstate',
      'externalexecutiontimestampexpandstate_cell_count', 'c',
      'CHECK ((transition_raw_cell_count = ((5 * transition_execution_count) + transition_event_count)))'),
    ('externalexecutiontimestampexpandstate',
      'externalexecutiontimestampexpandstate_kind_matches_counts', 'c',
      'CHECK ((((transition_kind = ''ZERO_HISTORY''::text) AND ' ||
      '(transition_execution_count = 0) AND (transition_event_count = 0) AND ' ||
      '(transition_raw_cell_count = 0)) OR ' ||
      '((transition_kind = ''MANIFEST_REQUIRED''::text) AND ' ||
      '((transition_execution_count > 0) OR (transition_event_count > 0)))))'),
    ('externalexecutiontimestampcontractgate',
      'externalexecutiontimestampcontractgate_pkey', 'p', 'PRIMARY KEY (id)'),
    ('externalexecutiontimestampcontractgate',
      'externalexecutiontimestampcontractgate_manifest_id_fkey', 'f',
      'FOREIGN KEY (manifest_id) REFERENCES externalexecutiontimestampmanifest(id) ' ||
      'ON UPDATE RESTRICT ON DELETE RESTRICT'),
    ('externalexecutiontimestampcontractgate',
      'externalexecutiontimestampcontractgate_expiry', 'c',
      'CHECK ((expires_at > prepared_at))'),
    ('externalexecutiontimestampcontractgate',
      'externalexecutiontimestampcontractgate_consumption_window', 'c',
      'CHECK (((consumed_at IS NULL) OR ((consumed_at >= prepared_at) AND (consumed_at <= expires_at))))')
)
SELECT
  (SELECT count(*) FROM required_legacy required
   JOIN information_schema.columns column_row
     ON column_row.table_schema = current_schema()
    AND column_row.table_name = required.table_name
    AND column_row.column_name = required.column_name
    AND column_row.data_type = 'timestamp without time zone'
    AND (column_row.is_nullable = 'YES') = required.nullable),
  (SELECT count(*) FROM required_shadow required
   JOIN information_schema.columns column_row
     ON column_row.table_schema = current_schema()
    AND column_row.table_name = required.table_name
    AND column_row.column_name = required.column_name
    AND column_row.data_type = 'timestamp with time zone'
    AND column_row.is_nullable = 'YES'),
  (SELECT count(*) FROM required_shadow required
   JOIN information_schema.columns column_row
     ON column_row.table_schema = current_schema()
    AND column_row.table_name = required.table_name
    AND column_row.column_name = required.column_name),
  (SELECT count(*) FROM required_expand_table required
   JOIN pg_class relation ON relation.relname = required.table_name
   JOIN pg_namespace namespace_row ON namespace_row.oid = relation.relnamespace
    AND namespace_row.nspname = current_schema()),
  (SELECT count(*) FROM required_expand_table required
   JOIN pg_class relation ON relation.relname = required.table_name
    AND relation.relkind = 'r'
   JOIN pg_namespace namespace_row ON namespace_row.oid = relation.relnamespace
    AND namespace_row.nspname = current_schema()),
  (SELECT count(*) FROM required_expand_column required
   JOIN information_schema.columns column_row
     ON column_row.table_schema = current_schema()
    AND column_row.table_name = required.table_name
    AND column_row.column_name = required.column_name
    AND column_row.data_type = required.data_type
    AND (column_row.is_nullable = 'YES') = required.nullable),
  (SELECT count(*) FROM required_expand_constraint required
   JOIN pg_class relation ON relation.relname = required.table_name
    AND relation.relkind = 'r'
   JOIN pg_namespace namespace_row ON namespace_row.oid = relation.relnamespace
    AND namespace_row.nspname = current_schema()
   JOIN pg_constraint constraint_row
     ON constraint_row.conrelid = relation.oid
    AND constraint_row.conname = required.constraint_name
    AND constraint_row.contype::text = required.constraint_type
    AND constraint_row.convalidated
    AND pg_get_constraintdef(constraint_row.oid) = required.constraint_definition)`

const externalExecutionTimestampRawCellsSQL = `
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

type externalExecutionTimestampSchemaContract struct {
	CatalogVersion        uint
	IdentitySourceVersion uint
}

type externalExecutionTimestampRawCellRow struct {
	SourceTable   string    `db:"source_table"`
	SourceRowID   uuid.UUID `db:"source_row_id"`
	SourceColumn  string    `db:"source_column"`
	ColumnOrdinal int16     `db:"column_ordinal"`
	RawValue      *string   `db:"raw_value"`
}

func readExternalExecutionTimestampSchemaContractInTx(
	ctx context.Context,
) (externalExecutionTimestampSchemaContract, error) {
	database := internalctx.GetDb(ctx)
	var versionTableExists bool
	if err := database.QueryRow(ctx, `
SELECT to_regclass(format('%I.schema_migrations', current_schema()))
       IS NOT NULL`).Scan(&versionTableExists); err != nil {
		return externalExecutionTimestampSchemaContract{}, err
	}
	if !versionTableExists {
		return externalExecutionTimestampSchemaContract{},
			errors.New("schema_migrations is absent")
	}
	var migrationRelationKind string
	if err := database.QueryRow(ctx, `
SELECT relation.relkind::text
FROM pg_catalog.pg_class relation
WHERE relation.oid =
  to_regclass(format('%I.schema_migrations', current_schema()))`).Scan(
		&migrationRelationKind,
	); err != nil {
		return externalExecutionTimestampSchemaContract{}, fmt.Errorf(
			"read schema_migrations relation kind: %w", err,
		)
	}
	if migrationRelationKind != "r" {
		return externalExecutionTimestampSchemaContract{}, errors.New(
			"schema_migrations must be an ordinary table",
		)
	}

	var migrationColumnCount, matchingMigrationColumnCount int64
	if err := database.QueryRow(ctx, `
SELECT
  count(*),
  count(*) FILTER (WHERE
    (column_name = 'version' AND data_type = 'bigint' AND is_nullable = 'NO')
    OR
    (column_name = 'dirty' AND data_type = 'boolean' AND is_nullable = 'NO')
  )
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = 'schema_migrations'`).Scan(
		&migrationColumnCount, &matchingMigrationColumnCount,
	); err != nil {
		return externalExecutionTimestampSchemaContract{},
			fmt.Errorf("read schema_migrations catalog: %w", err)
	}
	if migrationColumnCount != 2 || matchingMigrationColumnCount != 2 {
		return externalExecutionTimestampSchemaContract{}, errors.New(
			"schema_migrations catalog must contain exactly version BIGINT NOT NULL and dirty BOOLEAN NOT NULL",
		)
	}
	var migrationPrimaryKeyCount int64
	if err := database.QueryRow(ctx, `
SELECT count(*)
FROM pg_catalog.pg_constraint constraint_row
WHERE constraint_row.conrelid =
      to_regclass(format('%I.schema_migrations', current_schema()))
  AND constraint_row.contype = 'p'
  AND constraint_row.convalidated
  AND pg_get_constraintdef(constraint_row.oid) = 'PRIMARY KEY (version)'`).Scan(
		&migrationPrimaryKeyCount,
	); err != nil {
		return externalExecutionTimestampSchemaContract{}, fmt.Errorf(
			"read schema_migrations primary key: %w", err,
		)
	}
	if migrationPrimaryKeyCount != 1 {
		return externalExecutionTimestampSchemaContract{}, errors.New(
			"schema_migrations must have exactly one primary key on version",
		)
	}

	var rowCount int64
	if err := database.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(
		&rowCount,
	); err != nil {
		return externalExecutionTimestampSchemaContract{},
			fmt.Errorf("count schema_migrations: %w", err)
	}
	if rowCount != 1 {
		return externalExecutionTimestampSchemaContract{}, fmt.Errorf(
			"schema_migrations must contain exactly one row; found %d", rowCount,
		)
	}
	var version int64
	var dirty bool
	if err := database.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(
		&version, &dirty,
	); err != nil {
		return externalExecutionTimestampSchemaContract{},
			fmt.Errorf("read schema_migrations row: %w", err)
	}
	if dirty {
		return externalExecutionTimestampSchemaContract{},
			fmt.Errorf("schema version %d is dirty", version)
	}
	if version != 137 && version < 138 {
		return externalExecutionTimestampSchemaContract{},
			fmt.Errorf("schema %d is not supported", version)
	}

	var legacyCount, shadowShapeCount, shadowNameCount int64
	var expandObjectCount, ordinaryTableCount, expandColumnCount int64
	var expandConstraintCount int64
	if err := database.QueryRow(ctx, externalExecutionTimestampCatalogSQL).Scan(
		&legacyCount,
		&shadowShapeCount,
		&shadowNameCount,
		&expandObjectCount,
		&ordinaryTableCount,
		&expandColumnCount,
		&expandConstraintCount,
	); err != nil {
		return externalExecutionTimestampSchemaContract{}, err
	}
	if legacyCount != 6 {
		return externalExecutionTimestampSchemaContract{}, fmt.Errorf(
			"legacy timestamp catalog has %d of 6 required columns", legacyCount,
		)
	}
	if version == 137 {
		if shadowNameCount != 0 || expandObjectCount != 0 {
			return externalExecutionTimestampSchemaContract{},
				errors.New("schema 137 has unexpected expand objects")
		}
		return externalExecutionTimestampSchemaContract{
			CatalogVersion: 137, IdentitySourceVersion: 137,
		}, nil
	}
	if shadowNameCount != 6 || shadowShapeCount != 6 || ordinaryTableCount != 4 ||
		expandColumnCount != 66 || expandConstraintCount != 14 {
		return externalExecutionTimestampSchemaContract{},
			fmt.Errorf("schema %d is not expand-compatible", version)
	}

	var sourceVersion uint
	var stateRows int64
	if err := database.QueryRow(ctx, `
SELECT count(*), COALESCE(min(source_schema_version), 0)
FROM ExternalExecutionTimestampExpandState`).Scan(
		&stateRows, &sourceVersion,
	); err != nil {
		return externalExecutionTimestampSchemaContract{}, err
	}
	if stateRows != 1 || sourceVersion != 137 {
		return externalExecutionTimestampSchemaContract{}, errors.New(
			"expand state must contain exactly one source version 137 row",
		)
	}
	return externalExecutionTimestampSchemaContract{
		CatalogVersion: uint(version), IdentitySourceVersion: sourceVersion,
	}, nil
}

func externalExecutionTimestampReadinessContractError(err error) error {
	switch {
	case strings.Contains(err.Error(), "dirty"):
		return err
	case strings.Contains(err.Error(), "expand state"):
		return fmt.Errorf("expand state: %w", err)
	case strings.Contains(err.Error(), "schema_migrations"):
		return fmt.Errorf("schema version ledger: %w", err)
	default:
		return fmt.Errorf("column shape: %w", err)
	}
}

func requireExternalExecutionTimestampIndexesInTx(ctx context.Context) error {
	var validIndexes int64
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		externalExecutionTimestampReadinessIndexesSQL,
	).Scan(&validIndexes); err != nil {
		return fmt.Errorf("read index shape: %w", err)
	}
	if validIndexes != 5 {
		return fmt.Errorf("index shape has %d of 5 exact required indexes", validIndexes)
	}
	return nil
}

func normalizedExternalExecutionTimestampFunctionHash(body string) string {
	normalized := strings.Join(strings.Fields(body), " ")
	hash := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", hash)
}

func requireExternalExecutionTimestampGuardInTx(
	ctx context.Context,
	spec externalExecutionTimestampGuardSpec,
) error {
	database := internalctx.GetDb(ctx)
	var functionBody string
	if err := database.QueryRow(
		ctx,
		externalExecutionTimestampGuardFunctionSQL,
		pgx.NamedArgs{"functionName": spec.functionName},
	).Scan(&functionBody); err != nil {
		return fmt.Errorf("%s function is absent or malformed: %w", spec.errorLabel, err)
	}
	if normalizedExternalExecutionTimestampFunctionHash(functionBody) !=
		spec.functionBodyHash {
		return fmt.Errorf("%s function body differs from migration 138", spec.errorLabel)
	}

	var triggerCount int64
	if err := database.QueryRow(
		ctx,
		externalExecutionTimestampGuardTriggerSQL,
		pgx.NamedArgs{
			"tableName":     spec.tableName,
			"functionName":  spec.functionName,
			"triggerName":   spec.triggerName,
			"triggerType":   spec.triggerType,
			"updateColumns": spec.updateColumns,
		},
	).Scan(&triggerCount); err != nil {
		return fmt.Errorf("read %s trigger: %w", spec.errorLabel, err)
	}
	if triggerCount != 1 {
		return fmt.Errorf("%s must have one exact enabled migration-138 trigger", spec.errorLabel)
	}
	return nil
}

func requireExternalExecutionTimestampGuardsInTx(ctx context.Context) error {
	for _, spec := range externalExecutionTimestampGuardSpecs {
		if err := requireExternalExecutionTimestampGuardInTx(ctx, spec); err != nil {
			return err
		}
	}
	database := internalctx.GetDb(ctx)
	for _, spec := range externalExecutionTimestampBusinessTriggerSetSpecs {
		var triggerCount int64
		if err := database.QueryRow(
			ctx,
			externalExecutionTimestampNonInternalTriggerCountSQL,
			pgx.NamedArgs{"tableName": spec.tableName},
		).Scan(&triggerCount); err != nil {
			return fmt.Errorf("read %s non-internal trigger set: %w", spec.tableName, err)
		}
		if triggerCount != spec.triggerCount {
			return fmt.Errorf(
				"%s non-internal trigger set has %d triggers; expected exactly %d",
				spec.tableName,
				triggerCount,
				spec.triggerCount,
			)
		}
	}
	return nil
}

func readExternalExecutionTimestampExpandStateInTx(
	ctx context.Context,
) (externalExecutionTimestampExpandState, error) {
	rows, err := internalctx.GetDb(ctx).Query(
		ctx,
		externalExecutionTimestampExpandStateSQL,
	)
	if err != nil {
		return externalExecutionTimestampExpandState{},
			fmt.Errorf("read expand state: %w", err)
	}
	state, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[externalExecutionTimestampExpandState],
	)
	if err != nil {
		return externalExecutionTimestampExpandState{}, fmt.Errorf(
			"expand state must contain exactly one row: %w",
			err,
		)
	}
	if state.SourceVersion != 137 ||
		state.RawCellCount != 5*state.ExecutionCount+state.EventCount {
		return externalExecutionTimestampExpandState{}, errors.New(
			"expand state counts/source version are invalid",
		)
	}
	switch state.TransitionKind {
	case "ZERO_HISTORY":
		if state.ExecutionCount != 0 || state.EventCount != 0 ||
			state.RawCellCount != 0 {
			return externalExecutionTimestampExpandState{}, errors.New(
				"expand state ZERO_HISTORY counts are invalid",
			)
		}
	case "MANIFEST_REQUIRED":
		if state.ExecutionCount == 0 && state.EventCount == 0 {
			return externalExecutionTimestampExpandState{}, errors.New(
				"expand state MANIFEST_REQUIRED counts are invalid",
			)
		}
	default:
		return externalExecutionTimestampExpandState{}, fmt.Errorf(
			"expand state transition kind %q is unsupported",
			state.TransitionKind,
		)
	}
	return state, nil
}

func requireRootManifestMatchesExpandState(
	manifest types.ExternalExecutionTimestampManifest,
	state externalExecutionTimestampExpandState,
) error {
	if state.TransitionKind != "MANIFEST_REQUIRED" {
		return errors.New(
			"first manifest requires MANIFEST_REQUIRED expand state",
		)
	}
	if manifest.ExecutionCount != state.ExecutionCount ||
		manifest.EventCount != state.EventCount ||
		manifest.RawCellCount != state.RawCellCount {
		return errors.New(
			"first manifest counts differ from migration-138 transition counts",
		)
	}
	snapshotEndedAt, err := externalexecutiontimestamp.ParseInstant(
		manifest.SnapshotEndedAt,
	)
	if err != nil {
		return fmt.Errorf("first manifest snapshot end: %w", err)
	}
	if snapshotEndedAt.After(state.TransitionedAt) {
		return errors.New(
			"first manifest snapshot must end at or before migration-138 transition",
		)
	}
	return nil
}

func checkExternalExecutionTimestampZeroHistoryInTx(
	ctx context.Context,
	schemaVersion uint,
	state externalExecutionTimestampExpandState,
) (*types.ExternalExecutionTimestampReadiness, error) {
	if state.ExecutionCount != 0 || state.EventCount != 0 ||
		state.RawCellCount != 0 {
		return nil, errors.New("zero-history transition counts must be zero")
	}
	var manifestCount, provenanceCount uint64
	if err := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM ExternalExecutionTimestampManifest),
  (SELECT count(*) FROM ExternalExecutionTimestampCellProvenance)`).Scan(
		&manifestCount,
		&provenanceCount,
	); err != nil {
		return nil, err
	}
	if manifestCount != 0 || provenanceCount != 0 {
		return nil, errors.New("zero-history ledger must remain empty")
	}
	cells, err := readCurrentExternalExecutionTimestampCellsInTx(ctx)
	if err != nil {
		return nil, err
	}
	executionIDs := make(map[uuid.UUID]struct{})
	eventIDs := make(map[uuid.UUID]struct{})
	for _, cell := range cells {
		if cell.RowCreatedInstant == nil {
			return nil, errors.New("post-transition creation instant is absent")
		}
		createdAt, err := externalexecutiontimestamp.ParseInstant(
			*cell.RowCreatedInstant,
		)
		if err != nil {
			return nil, fmt.Errorf("post-transition creation instant is invalid: %w", err)
		}
		if createdAt.Before(state.TransitionedAt) {
			return nil, errors.New(
				"post-transition creation predates zero-history transition",
			)
		}
		if err := requireExactUTCTimestampPair(
			cell.RawValue,
			cell.InstantValue,
		); err != nil {
			return nil, fmt.Errorf(
				"zero-history %s pair is unpaired: %w",
				cell.SourceColumn,
				err,
			)
		}
		switch cell.SourceTable {
		case "externalexecution":
			executionIDs[cell.SourceRowID] = struct{}{}
		case "externalexecutionevent":
			eventIDs[cell.SourceRowID] = struct{}{}
		default:
			return nil, fmt.Errorf(
				"zero-history row uses unsupported source table %q",
				cell.SourceTable,
			)
		}
	}
	return &types.ExternalExecutionTimestampReadiness{
		SchemaVersion:           schemaVersion,
		TransitionKind:          state.TransitionKind,
		ExecutionCount:          uint64(len(executionIDs)),
		EventCount:              uint64(len(eventIDs)),
		PostTransitionPairCount: uint64(len(cells)),
	}, nil
}

func checkExternalExecutionTimestampManifestReadinessInTx(
	ctx context.Context,
	schemaVersion uint,
	state externalExecutionTimestampExpandState,
) (*types.ExternalExecutionTimestampReadiness, error) {
	if state.ExecutionCount == 0 && state.EventCount == 0 {
		return nil, errors.New("manifest-required transition has zero counts")
	}
	tip, err := readUniqueVerifiedManifestTipInTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("VERIFIED manifest tip: %w", err)
	}
	if tip == nil {
		return nil, errors.New("verified manifest tip is required")
	}

	currentID := *tip
	seen := make(map[uuid.UUID]struct{})
	var root *types.ExternalExecutionTimestampManifest
	var child *types.ExternalExecutionTimestampManifest
	for {
		if _, duplicate := seen[currentID]; duplicate {
			return nil, errors.New("manifest chain contains a cycle")
		}
		seen[currentID] = struct{}{}
		manifest, manifestState, err := readStoredExternalExecutionTimestampManifestInTx(ctx, currentID)
		if err != nil {
			return nil, err
		}
		if manifestState != types.ExternalExecutionTimestampManifestStateVerified {
			return nil, errors.New("manifest chain contains a non-VERIFIED row")
		}
		if uint64(len(manifest.Cells)) != manifest.RawCellCount {
			return nil, errors.New("manifest provenance row count is incomplete")
		}
		if problems := externalexecutiontimestamp.ValidateManifestDocument(
			*manifest,
		); len(problems) != 0 {
			return nil, errors.Join(problems...)
		}
		if manifest.SourceSchemaVersion != state.SourceVersion {
			return nil, errors.New(
				"manifest chain source version differs from expand state",
			)
		}
		if child != nil {
			if problems := externalexecutiontimestamp.ValidateSupersession(
				*manifest,
				*child,
			); len(problems) != 0 {
				return nil, errors.Join(problems...)
			}
		}
		if manifest.SupersedesManifestID == nil {
			root = manifest
			break
		}
		child = manifest
		currentID = *manifest.SupersedesManifestID
	}
	var verifiedManifestCount uint64
	if err := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT count(*) FROM ExternalExecutionTimestampManifest
WHERE state='VERIFIED'`).Scan(&verifiedManifestCount); err != nil {
		return nil, err
	}
	if verifiedManifestCount != uint64(len(seen)) {
		return nil, fmt.Errorf(
			"manifest chain contains %d of %d VERIFIED rows; it must include all VERIFIED rows",
			len(seen),
			verifiedManifestCount,
		)
	}
	if err := requireRootManifestMatchesExpandState(*root, state); err != nil {
		return nil, err
	}
	verified, err := verifyExternalExecutionTimestampManifestInTx(ctx, *tip)
	if err != nil {
		return nil, err
	}
	manifestID := *tip
	return &types.ExternalExecutionTimestampReadiness{
		SchemaVersion:           schemaVersion,
		TransitionKind:          state.TransitionKind,
		ManifestID:              &manifestID,
		ExecutionCount:          verified.CurrentExecutionCount,
		EventCount:              verified.CurrentEventCount,
		ProvenanceRows:          verified.ProvenanceRows,
		PostTransitionPairCount: verified.PostManifestPairedCount,
	}, nil
}

func checkExternalExecutionTimestampExpandReadinessInTx(
	ctx context.Context,
) (*types.ExternalExecutionTimestampReadiness, error) {
	contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
	if err != nil {
		return nil, externalExecutionTimestampReadinessContractError(err)
	}
	if contract.CatalogVersion < 138 {
		return nil, fmt.Errorf(
			"schema version %d is pre-expand",
			contract.CatalogVersion,
		)
	}
	if err := requireExternalExecutionTimestampIndexesInTx(ctx); err != nil {
		return nil, err
	}
	if err := requireExternalExecutionTimestampGuardsInTx(ctx); err != nil {
		return nil, err
	}
	state, err := readExternalExecutionTimestampExpandStateInTx(ctx)
	if err != nil {
		return nil, err
	}
	switch state.TransitionKind {
	case "ZERO_HISTORY":
		return checkExternalExecutionTimestampZeroHistoryInTx(
			ctx,
			contract.CatalogVersion,
			state,
		)
	case "MANIFEST_REQUIRED":
		return checkExternalExecutionTimestampManifestReadinessInTx(
			ctx,
			contract.CatalogVersion,
			state,
		)
	default:
		return nil, fmt.Errorf(
			"unsupported expand transition %q",
			state.TransitionKind,
		)
	}
}

func CheckExternalExecutionTimestampExpandReadiness(
	ctx context.Context,
) (out *types.ExternalExecutionTimestampReadiness, finalErr error) {
	finalErr = RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
		checked, err := checkExternalExecutionTimestampExpandReadinessInTx(ctx)
		out = checked
		return err
	})
	return out, finalErr
}

func RequireExternalExecutionTimestampExpandReadiness(ctx context.Context) error {
	_, err := CheckExternalExecutionTimestampExpandReadiness(ctx)
	return err
}

func inspectExternalExecutionTimestampsInTx(
	ctx context.Context,
) (*types.ExternalExecutionTimestampManifest, error) {
	database := internalctx.GetDb(ctx)
	contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
	if err != nil {
		return nil, err
	}
	var startedAt time.Time
	if err := database.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&startedAt); err != nil {
		return nil, err
	}
	rows, err := database.Query(ctx, externalExecutionTimestampRawCellsSQL)
	if err != nil {
		return nil, err
	}
	rawRows, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[externalExecutionTimestampRawCellRow],
	)
	if err != nil {
		return nil, err
	}
	var endedAt time.Time
	if err := database.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&endedAt); err != nil {
		return nil, err
	}

	rawCells := make([]types.ExternalExecutionTimestampRawCell, 0, len(rawRows))
	decisions := make([]types.ExternalExecutionTimestampCellDecision, 0, len(rawRows))
	executionIDs := make([]uuid.UUID, 0)
	eventIDs := make([]uuid.UUID, 0)
	seenExecutions := make(map[uuid.UUID]struct{})
	seenEvents := make(map[uuid.UUID]struct{})
	var populated uint64
	for _, row := range rawRows {
		raw := types.ExternalExecutionTimestampRawCell{
			SourceTable:   row.SourceTable,
			SourceRowID:   row.SourceRowID,
			SourceColumn:  row.SourceColumn,
			ColumnOrdinal: uint8(row.ColumnOrdinal),
			RawValue:      row.RawValue,
		}
		raw.RawCellChecksum, err = externalexecutiontimestamp.ComputeRawCellChecksum(raw)
		if err != nil {
			return nil, err
		}
		decision := types.ExternalExecutionTimestampDecisionUnresolved
		if raw.RawValue == nil {
			decision = types.ExternalExecutionTimestampDecisionNull
		} else {
			populated++
		}
		rawCells = append(rawCells, raw)
		decisions = append(decisions, types.ExternalExecutionTimestampCellDecision{
			ExternalExecutionTimestampRawCell: raw,
			Decision:                          decision,
			ConversionExpressionVersion:       externalexecutiontimestamp.ConversionExpressionVersion,
		})
		switch row.SourceTable {
		case "externalexecution":
			if _, exists := seenExecutions[row.SourceRowID]; !exists {
				seenExecutions[row.SourceRowID] = struct{}{}
				executionIDs = append(executionIDs, row.SourceRowID)
			}
		case "externalexecutionevent":
			if _, exists := seenEvents[row.SourceRowID]; !exists {
				seenEvents[row.SourceRowID] = struct{}{}
				eventIDs = append(eventIDs, row.SourceRowID)
			}
		default:
			return nil, fmt.Errorf("unexpected timestamp source table %q", row.SourceTable)
		}
	}

	rawSetChecksum, err := externalexecutiontimestamp.ComputeRawSetChecksum(rawCells)
	if err != nil {
		return nil, err
	}
	identity, err := externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
		contract.IdentitySourceVersion,
		executionIDs,
		eventIDs,
		uint64(len(rawCells)),
		rawSetChecksum,
	)
	if err != nil {
		return nil, err
	}
	manifest := &types.ExternalExecutionTimestampManifest{
		ID:                          uuid.New(),
		DatabaseIdentityChecksum:    identity,
		SourceSchemaVersion:         contract.IdentitySourceVersion,
		SnapshotStartedAt:           externalexecutiontimestamp.FormatInstant(startedAt),
		SnapshotEndedAt:             externalexecutiontimestamp.FormatInstant(endedAt),
		ExecutionCount:              uint64(len(executionIDs)),
		EventCount:                  uint64(len(eventIDs)),
		RawCellCount:                uint64(len(rawCells)),
		PopulatedCellCount:          populated,
		RawCellChecksum:             rawSetChecksum,
		ToolVersion:                 buildconfig.Version(),
		ConversionExpressionVersion: externalexecutiontimestamp.ConversionExpressionVersion,
		State:                       types.ExternalExecutionTimestampManifestStateDraft,
		Cells:                       decisions,
	}
	manifest.DecisionContentChecksum, err = externalexecutiontimestamp.ComputeDecisionContentChecksum(*manifest)
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

func InspectExternalExecutionTimestamps(
	ctx context.Context,
) (manifest *types.ExternalExecutionTimestampManifest, finalErr error) {
	finalErr = RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
		inspected, err := inspectExternalExecutionTimestampsInTx(ctx)
		manifest = inspected
		return err
	})
	return manifest, finalErr
}

func requireManifestMatchesSnapshot(
	manifest types.ExternalExecutionTimestampManifest,
	snapshot types.ExternalExecutionTimestampManifest,
) error {
	if manifest.SourceSchemaVersion != snapshot.SourceSchemaVersion ||
		manifest.ExecutionCount != snapshot.ExecutionCount ||
		manifest.EventCount != snapshot.EventCount ||
		manifest.RawCellCount != snapshot.RawCellCount ||
		manifest.PopulatedCellCount != snapshot.PopulatedCellCount ||
		manifest.RawCellChecksum != snapshot.RawCellChecksum ||
		manifest.DatabaseIdentityChecksum != snapshot.DatabaseIdentityChecksum {
		return errors.New("manifest does not match the current raw snapshot")
	}
	current := make(map[string]types.ExternalExecutionTimestampRawCell, len(snapshot.Cells))
	for _, cell := range snapshot.Cells {
		key := fmt.Sprintf(
			"%s/%s/%s/%d",
			cell.SourceTable,
			cell.SourceRowID,
			cell.SourceColumn,
			cell.ColumnOrdinal,
		)
		current[key] = cell.ExternalExecutionTimestampRawCell
	}
	for _, cell := range manifest.Cells {
		key := fmt.Sprintf(
			"%s/%s/%s/%d",
			cell.SourceTable,
			cell.SourceRowID,
			cell.SourceColumn,
			cell.ColumnOrdinal,
		)
		raw, exists := current[key]
		sameRaw := raw.RawValue == nil && cell.RawValue == nil
		if raw.RawValue != nil && cell.RawValue != nil {
			sameRaw = *raw.RawValue == *cell.RawValue
		}
		if !exists || !sameRaw || raw.RawCellChecksum != cell.RawCellChecksum {
			return fmt.Errorf("manifest raw cell %s does not match snapshot", key)
		}
		delete(current, key)
	}
	if len(current) != 0 {
		return errors.New("manifest omits current raw cells")
	}
	return nil
}

func validationReportFromManifest(
	manifest types.ExternalExecutionTimestampManifest,
	catalogVersion uint,
) *types.ExternalExecutionTimestampValidationReport {
	var unresolved uint64
	for _, cell := range manifest.Cells {
		if cell.Decision == types.ExternalExecutionTimestampDecisionUnresolved {
			unresolved++
		}
	}
	return &types.ExternalExecutionTimestampValidationReport{
		ManifestID:               manifest.ID,
		SchemaVersion:            catalogVersion,
		ExecutionCount:           manifest.ExecutionCount,
		EventCount:               manifest.EventCount,
		RawCellCount:             manifest.RawCellCount,
		PopulatedCellCount:       manifest.PopulatedCellCount,
		UnresolvedCellCount:      unresolved,
		RawSetChecksum:           manifest.RawCellChecksum,
		DatabaseIdentityChecksum: manifest.DatabaseIdentityChecksum,
		DecisionContentChecksum:  manifest.DecisionContentChecksum,
	}
}

func ValidateExternalExecutionTimestampManifest(
	ctx context.Context,
	manifest types.ExternalExecutionTimestampManifest,
) (report *types.ExternalExecutionTimestampValidationReport, finalErr error) {
	if manifest.SupersedesManifestID != nil {
		return nil, errors.New(
			"superseding manifest live validation requires verified-tip provenance; use the apply workflow",
		)
	}
	if problems := externalexecutiontimestamp.ValidateManifestDocument(manifest); len(problems) != 0 {
		return nil, errors.Join(problems...)
	}
	finalErr = RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
		contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
		if err != nil {
			return err
		}
		snapshot, err := inspectExternalExecutionTimestampsInTx(ctx)
		if err != nil {
			return err
		}
		if err := requireManifestMatchesSnapshot(manifest, *snapshot); err != nil {
			return err
		}
		report = validationReportFromManifest(manifest, contract.CatalogVersion)
		return nil
	})
	return report, finalErr
}

func dereferenceTimestampText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func readStoredExternalExecutionTimestampManifestInTx(
	ctx context.Context,
	manifestID uuid.UUID,
) (*types.ExternalExecutionTimestampManifest,
	types.ExternalExecutionTimestampManifestState, error,
) {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(
		ctx,
		storedExternalExecutionTimestampManifestSQL,
		pgx.NamedArgs{"manifestId": manifestID},
	)
	if err != nil {
		return nil, "", err
	}
	stored, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[storedExternalExecutionTimestampManifestRow],
	)
	if err != nil {
		return nil, "", err
	}
	cellRows, err := database.Query(
		ctx,
		storedExternalExecutionTimestampProvenanceSQL,
		pgx.NamedArgs{"manifestId": manifestID},
	)
	if err != nil {
		return nil, "", err
	}
	provenance, err := pgx.CollectRows(
		cellRows,
		pgx.RowToStructByName[storedExternalExecutionTimestampProvenanceRow],
	)
	if err != nil {
		return nil, "", err
	}
	manifest := &types.ExternalExecutionTimestampManifest{
		ID:                          stored.ID,
		SupersedesManifestID:        stored.SupersedesManifestID,
		DatabaseIdentityChecksum:    stored.DatabaseIdentityChecksum,
		SourceSchemaVersion:         stored.SourceSchemaVersion,
		SnapshotStartedAt:           stored.SnapshotStartedAt,
		SnapshotEndedAt:             stored.SnapshotEndedAt,
		ExecutionCount:              stored.ExecutionCount,
		EventCount:                  stored.EventCount,
		RawCellCount:                stored.RawCellCount,
		PopulatedCellCount:          stored.PopulatedCellCount,
		RawCellChecksum:             stored.RawCellChecksum,
		EvidenceBundleReference:     stored.EvidenceBundleReference,
		EvidenceBundleChecksum:      stored.EvidenceBundleChecksum,
		ToolVersion:                 stored.ToolVersion,
		ConversionExpressionVersion: stored.ConversionExpressionVersion,
		AuthorIdentity:              stored.AuthorIdentity,
		ReviewerIdentity:            stored.ReviewerIdentity,
		ApprovedAt:                  stored.ApprovedAt,
		TargetReleaseCommit:         stored.TargetReleaseCommit,
		TargetImageDigest:           stored.TargetImageDigest,
		State:                       stored.State,
		DecisionContentChecksum:     stored.DecisionContentChecksum,
		Cells:                       make([]types.ExternalExecutionTimestampCellDecision, 0, len(provenance)),
	}
	for _, row := range provenance {
		manifest.Cells = append(
			manifest.Cells,
			types.ExternalExecutionTimestampCellDecision{
				ExternalExecutionTimestampRawCell: types.ExternalExecutionTimestampRawCell{
					SourceTable:     row.SourceTable,
					SourceRowID:     row.SourceRowID,
					SourceColumn:    row.SourceColumn,
					ColumnOrdinal:   uint8(row.ColumnOrdinal),
					RawValue:        row.RawValue,
					RawCellChecksum: row.RawCellChecksum,
				},
				Decision:                    row.Decision,
				SourceZone:                  dereferenceTimestampText(row.SourceZone),
				SourceOffsetSeconds:         row.SourceOffsetSeconds,
				ConvertedValue:              row.ConvertedValue,
				EvidenceReference:           dereferenceTimestampText(row.EvidenceReference),
				EvidenceChecksum:            dereferenceTimestampText(row.EvidenceChecksum),
				ApprovingIdentity:           dereferenceTimestampText(row.ApprovingIdentity),
				ConversionExpressionVersion: row.ConversionExpressionVersion,
			},
		)
	}
	return manifest, stored.State, nil
}

func readCurrentExternalExecutionTimestampCellsInTx(
	ctx context.Context,
) ([]currentExternalExecutionTimestampCell, error) {
	rows, err := internalctx.GetDb(ctx).Query(
		ctx,
		currentExternalExecutionTimestampCellsSQL,
	)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(
		rows,
		pgx.RowToStructByName[currentExternalExecutionTimestampCell],
	)
}

func requireExactUTCTimestampPair(raw *string, instant *string) error {
	if raw == nil && instant == nil {
		return nil
	}
	if raw == nil || instant == nil {
		return errors.New("timestamp pair is incomplete")
	}
	expected, err := externalexecutiontimestamp.ConvertWallClock(*raw, 0)
	if err != nil {
		return err
	}
	if externalexecutiontimestamp.FormatInstant(expected) != *instant {
		return errors.New("timestamp pair does not represent one UTC instant")
	}
	return nil
}

func equalTimestampText(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func verifyHistoricalTimestampCell(
	provenance types.ExternalExecutionTimestampCellDecision,
	current currentExternalExecutionTimestampCell,
	lifecycleBaselineAt time.Time,
) error {
	sameRaw := equalTimestampText(provenance.RawValue, current.RawValue)
	expectedInstant := provenance.ConvertedValue
	if provenance.Decision == types.ExternalExecutionTimestampDecisionUnresolved ||
		provenance.Decision == types.ExternalExecutionTimestampDecisionNull {
		expectedInstant = nil
	}
	unchanged := func() error {
		if !sameRaw {
			return errors.New("immutable raw timestamp changed")
		}
		if provenance.Decision == types.ExternalExecutionTimestampDecisionUnresolved &&
			current.InstantValue != nil {
			return errors.New("unresolved shadow must remain null")
		}
		if !equalTimestampText(expectedInstant, current.InstantValue) {
			return errors.New("historical shadow differs from provenance")
		}
		return nil
	}
	immutable := provenance.SourceTable == "externalexecutionevent" ||
		provenance.SourceColumn == "created_at" ||
		provenance.SourceColumn == "callback_deadline_at"
	if immutable {
		return unchanged()
	}
	if (provenance.SourceColumn == "started_at" ||
		provenance.SourceColumn == "completed_at") &&
		provenance.RawValue != nil {
		if !sameRaw {
			return errors.New("immutable lifecycle raw timestamp changed")
		}
		return unchanged()
	}
	if sameRaw {
		return unchanged()
	}
	if provenance.SourceColumn != "updated_at" &&
		provenance.SourceColumn != "started_at" &&
		provenance.SourceColumn != "completed_at" {
		return errors.New("unsupported lifecycle evolution")
	}
	if provenance.RawValue != nil && provenance.SourceColumn != "updated_at" {
		return errors.New("immutable lifecycle raw timestamp changed")
	}
	if err := requireExactUTCTimestampPair(
		current.RawValue,
		current.InstantValue,
	); err != nil {
		return err
	}
	if current.InstantValue == nil {
		return errors.New("evolved lifecycle instant is absent")
	}
	instant, err := externalexecutiontimestamp.ParseInstant(*current.InstantValue)
	if err != nil {
		return err
	}
	if instant.Before(lifecycleBaselineAt) {
		return errors.New("evolved lifecycle instant predates verification")
	}
	return nil
}

func readExternalExecutionTimestampLifecycleBaselineInTx(
	ctx context.Context,
	manifestID uuid.UUID,
) (time.Time, error) {
	currentID := manifestID
	seen := make(map[uuid.UUID]struct{})
	for {
		if _, duplicate := seen[currentID]; duplicate {
			return time.Time{}, errors.New("manifest chain contains a cycle")
		}
		seen[currentID] = struct{}{}
		var parentID *uuid.UUID
		var state types.ExternalExecutionTimestampManifestState
		var verifiedAt *time.Time
		if err := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT supersedes_manifest_id, state, verified_at
FROM ExternalExecutionTimestampManifest WHERE id=@id`,
			pgx.NamedArgs{"id": currentID},
		).Scan(&parentID, &state, &verifiedAt); err != nil {
			return time.Time{}, err
		}
		if state != types.ExternalExecutionTimestampManifestStateVerified || verifiedAt == nil {
			return time.Time{}, errors.New(
				"manifest lifecycle baseline must be VERIFIED",
			)
		}
		if parentID == nil {
			return verifiedAt.UTC(), nil
		}
		currentID = *parentID
	}
}

func verifyExternalExecutionTimestampManifestInTx(
	ctx context.Context,
	manifestID uuid.UUID,
) (*types.ExternalExecutionTimestampVerificationReport, error) {
	manifest, state, err := readStoredExternalExecutionTimestampManifestInTx(
		ctx,
		manifestID,
	)
	if err != nil {
		return nil, err
	}
	if state != types.ExternalExecutionTimestampManifestStateVerified {
		return nil, errors.New("manifest state must be VERIFIED")
	}
	if uint64(len(manifest.Cells)) != manifest.RawCellCount {
		return nil, errors.New("provenance row count is incomplete")
	}
	if problems := externalexecutiontimestamp.ValidateManifestDocument(
		*manifest,
	); len(problems) != 0 {
		return nil, errors.Join(problems...)
	}
	lifecycleBaselineAt, err := readExternalExecutionTimestampLifecycleBaselineInTx(
		ctx,
		manifestID,
	)
	if err != nil {
		return nil, err
	}
	currentRows, err := readCurrentExternalExecutionTimestampCellsInTx(ctx)
	if err != nil {
		return nil, err
	}
	current := make(
		map[string]currentExternalExecutionTimestampCell,
		len(currentRows),
	)
	executionIDs := make(map[uuid.UUID]struct{})
	eventIDs := make(map[uuid.UUID]struct{})
	for _, cell := range currentRows {
		key := timestampCellKey(
			cell.SourceTable,
			cell.SourceRowID,
			cell.SourceColumn,
			uint8(cell.ColumnOrdinal),
		)
		current[key] = cell
		if cell.SourceTable == "externalexecution" {
			executionIDs[cell.SourceRowID] = struct{}{}
		} else {
			eventIDs[cell.SourceRowID] = struct{}{}
		}
	}
	report := &types.ExternalExecutionTimestampVerificationReport{
		ManifestID:              manifest.ID,
		SourceExecutionCount:    manifest.ExecutionCount,
		SourceEventCount:        manifest.EventCount,
		CurrentExecutionCount:   uint64(len(executionIDs)),
		CurrentEventCount:       uint64(len(eventIDs)),
		ProvenanceRows:          uint64(len(manifest.Cells)),
		RawSetChecksum:          manifest.RawCellChecksum,
		DecisionContentChecksum: manifest.DecisionContentChecksum,
	}
	contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
	if err != nil {
		return nil, err
	}
	report.SchemaVersion = contract.CatalogVersion
	for _, provenance := range manifest.Cells {
		key := timestampCellKey(
			provenance.SourceTable,
			provenance.SourceRowID,
			provenance.SourceColumn,
			provenance.ColumnOrdinal,
		)
		currentCell, exists := current[key]
		if !exists {
			return nil, fmt.Errorf("provenance cell %s has no source row", key)
		}
		if err := verifyHistoricalTimestampCell(
			provenance,
			currentCell,
			lifecycleBaselineAt,
		); err != nil {
			return nil, fmt.Errorf(
				"cell %s %s pair: %w",
				key,
				provenance.SourceColumn,
				err,
			)
		}
		switch provenance.Decision {
		case types.ExternalExecutionTimestampDecisionProven,
			types.ExternalExecutionTimestampDecisionAttested:
			report.ResolvedShadowCount++
		case types.ExternalExecutionTimestampDecisionUnresolved:
			report.UnresolvedShadowCount++
		}
		delete(current, key)
	}
	for key, cell := range current {
		if cell.RowCreatedInstant == nil {
			return nil, fmt.Errorf(
				"post-manifest pair %s has no creation instant",
				key,
			)
		}
		created, err := externalexecutiontimestamp.ParseInstant(
			*cell.RowCreatedInstant,
		)
		if err != nil || created.Before(lifecycleBaselineAt) {
			return nil, fmt.Errorf("post-manifest pair %s predates verification", key)
		}
		if err := requireExactUTCTimestampPair(
			cell.RawValue,
			cell.InstantValue,
		); err != nil {
			return nil, fmt.Errorf("post-manifest pair %s: %w", key, err)
		}
		report.PostManifestPairedCount++
	}
	return report, nil
}

func VerifyExternalExecutionTimestampManifest(
	ctx context.Context,
	manifestID uuid.UUID,
) (report *types.ExternalExecutionTimestampVerificationReport, finalErr error) {
	finalErr = RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
		verified, err := verifyExternalExecutionTimestampManifestInTx(
			ctx,
			manifestID,
		)
		report = verified
		return err
	})
	return report, finalErr
}

func timestampCellKey(
	table string,
	rowID uuid.UUID,
	column string,
	ordinal uint8,
) string {
	return fmt.Sprintf("%s/%s/%s/%d", table, rowID, column, ordinal)
}

func resolvedTimestampCellKeys(
	manifest types.ExternalExecutionTimestampManifest,
) map[string]struct{} {
	resolved := make(map[string]struct{})
	for _, cell := range manifest.Cells {
		if cell.Decision != types.ExternalExecutionTimestampDecisionProven &&
			cell.Decision != types.ExternalExecutionTimestampDecisionAttested {
			continue
		}
		resolved[timestampCellKey(
			cell.SourceTable,
			cell.SourceRowID,
			cell.SourceColumn,
			cell.ColumnOrdinal,
		)] = struct{}{}
	}
	return resolved
}

func readUniqueVerifiedManifestTipInTx(
	ctx context.Context,
) (*uuid.UUID, error) {
	database := internalctx.GetDb(ctx)
	var incomplete int64
	if err := database.QueryRow(ctx, `
SELECT count(*) FROM ExternalExecutionTimestampManifest
WHERE state IN ('DRAFT','APPROVED','APPLIED')`).Scan(&incomplete); err != nil {
		return nil, err
	}
	if incomplete != 0 {
		return nil, fmt.Errorf("found %d incomplete manifests", incomplete)
	}
	rows, err := database.Query(ctx, externalExecutionTimestampVerifiedTipsSQL)
	if err != nil {
		return nil, err
	}
	tips, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	if len(tips) > 1 {
		return nil, fmt.Errorf("found %d verified manifest tips", len(tips))
	}
	if len(tips) == 0 {
		return nil, nil
	}
	return &tips[0], nil
}

func newlyPromotedTimestampCellsRequiringShadowFillInTx(
	ctx context.Context,
	previous types.ExternalExecutionTimestampManifest,
	next types.ExternalExecutionTimestampManifest,
) (map[string]struct{}, error) {
	previousCells := make(
		map[string]types.ExternalExecutionTimestampCellDecision,
		len(previous.Cells),
	)
	for _, cell := range previous.Cells {
		previousCells[timestampCellKey(
			cell.SourceTable,
			cell.SourceRowID,
			cell.SourceColumn,
			cell.ColumnOrdinal,
		)] = cell
	}
	currentRows, err := readCurrentExternalExecutionTimestampCellsInTx(ctx)
	if err != nil {
		return nil, err
	}
	current := make(
		map[string]currentExternalExecutionTimestampCell,
		len(currentRows),
	)
	for _, cell := range currentRows {
		current[timestampCellKey(
			cell.SourceTable,
			cell.SourceRowID,
			cell.SourceColumn,
			uint8(cell.ColumnOrdinal),
		)] = cell
	}
	promoted := make(map[string]struct{})
	for _, nextCell := range next.Cells {
		key := timestampCellKey(
			nextCell.SourceTable,
			nextCell.SourceRowID,
			nextCell.SourceColumn,
			nextCell.ColumnOrdinal,
		)
		previousCell, exists := previousCells[key]
		if !exists {
			return nil, fmt.Errorf("superseding cell %s has no prior decision", key)
		}
		isPromotion := previousCell.Decision ==
			types.ExternalExecutionTimestampDecisionUnresolved &&
			(nextCell.Decision == types.ExternalExecutionTimestampDecisionProven ||
				nextCell.Decision == types.ExternalExecutionTimestampDecisionAttested)
		if !isPromotion {
			continue
		}
		currentCell, exists := current[key]
		if !exists {
			return nil, fmt.Errorf("promoted provenance cell %s has no source row", key)
		}
		if equalTimestampText(previousCell.RawValue, currentCell.RawValue) {
			if currentCell.InstantValue != nil {
				return nil, fmt.Errorf(
					"newly promoted timestamp shadow %s is already non-null",
					key,
				)
			}
			promoted[key] = struct{}{}
		}
	}
	return promoted, nil
}

func rootTimestampCellsRequiringShadowFillInTx(
	ctx context.Context,
	manifest types.ExternalExecutionTimestampManifest,
) (map[string]struct{}, error) {
	currentRows, err := readCurrentExternalExecutionTimestampCellsInTx(ctx)
	if err != nil {
		return nil, err
	}
	current := make(
		map[string]currentExternalExecutionTimestampCell,
		len(currentRows),
	)
	for _, cell := range currentRows {
		current[timestampCellKey(
			cell.SourceTable,
			cell.SourceRowID,
			cell.SourceColumn,
			uint8(cell.ColumnOrdinal),
		)] = cell
	}
	toPopulate := make(map[string]struct{})
	for _, decision := range manifest.Cells {
		key := timestampCellKey(
			decision.SourceTable,
			decision.SourceRowID,
			decision.SourceColumn,
			decision.ColumnOrdinal,
		)
		currentCell, exists := current[key]
		if !exists {
			return nil, fmt.Errorf("manifest cell %s has no current source row", key)
		}
		if !equalTimestampText(decision.RawValue, currentCell.RawValue) {
			return nil, fmt.Errorf("manifest raw cell %s changed during preflight", key)
		}
		if currentCell.InstantValue != nil {
			return nil, fmt.Errorf(
				"first manifest requires every historical timestamp shadow to be null: %s",
				key,
			)
		}
		switch decision.Decision {
		case types.ExternalExecutionTimestampDecisionProven,
			types.ExternalExecutionTimestampDecisionAttested:
			if currentCell.InstantValue == nil {
				toPopulate[key] = struct{}{}
				continue
			}
			if !equalTimestampText(decision.ConvertedValue, currentCell.InstantValue) {
				return nil, fmt.Errorf(
					"resolved timestamp shadow %s differs from approved instant",
					key,
				)
			}
		case types.ExternalExecutionTimestampDecisionUnresolved:
			if currentCell.InstantValue != nil {
				return nil, fmt.Errorf("unresolved shadow %s must remain null", key)
			}
		case types.ExternalExecutionTimestampDecisionNull:
			if currentCell.InstantValue != nil {
				return nil, fmt.Errorf("null-value shadow %s must remain null", key)
			}
		default:
			return nil, fmt.Errorf("timestamp cell %s has unsupported decision", key)
		}
	}
	return toPopulate, nil
}

func requireManifestSnapshotForApplyInTx(
	ctx context.Context,
	manifest types.ExternalExecutionTimestampManifest,
) (map[string]struct{}, error) {
	contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
	if err != nil {
		return nil, err
	}
	if contract.CatalogVersion == 137 {
		if manifest.SupersedesManifestID != nil {
			return nil, errors.New(
				"schema-137 dry-run manifest cannot supersede another manifest",
			)
		}
		liveSnapshot, err := inspectExternalExecutionTimestampsInTx(ctx)
		if err != nil {
			return nil, err
		}
		if err := requireManifestMatchesSnapshot(manifest, *liveSnapshot); err != nil {
			return nil, err
		}
		return resolvedTimestampCellKeys(manifest), nil
	}

	tip, err := readUniqueVerifiedManifestTipInTx(ctx)
	if err != nil {
		return nil, err
	}
	if tip == nil {
		if manifest.SupersedesManifestID != nil {
			return nil, errors.New("first manifest cannot supersede another manifest")
		}
		state, err := readExternalExecutionTimestampExpandStateInTx(ctx)
		if err != nil {
			return nil, err
		}
		if err := requireRootManifestMatchesExpandState(manifest, state); err != nil {
			return nil, err
		}
		liveSnapshot, err := inspectExternalExecutionTimestampsInTx(ctx)
		if err != nil {
			return nil, err
		}
		if err := requireManifestMatchesSnapshot(manifest, *liveSnapshot); err != nil {
			return nil, err
		}
		return rootTimestampCellsRequiringShadowFillInTx(ctx, manifest)
	}
	if manifest.SupersedesManifestID == nil || *manifest.SupersedesManifestID != *tip {
		return nil, fmt.Errorf("manifest must supersede verified tip %s", *tip)
	}
	previous, _, err := readStoredExternalExecutionTimestampManifestInTx(ctx, *tip)
	if err != nil {
		return nil, err
	}
	if problems := externalexecutiontimestamp.ValidateSupersession(
		*previous,
		manifest,
	); len(problems) != 0 {
		return nil, errors.Join(problems...)
	}
	if _, err := verifyExternalExecutionTimestampManifestInTx(ctx, *tip); err != nil {
		return nil, fmt.Errorf(
			"live lifecycle verification against verified tip: %w",
			err,
		)
	}
	return newlyPromotedTimestampCellsRequiringShadowFillInTx(
		ctx,
		*previous,
		manifest,
	)
}

func externalExecutionTimestampApplyReport(
	manifest types.ExternalExecutionTimestampManifest,
	dryRun bool,
	idempotent bool,
) *types.ExternalExecutionTimestampApplyReport {
	report := &types.ExternalExecutionTimestampApplyReport{
		ManifestID:               manifest.ID,
		DryRun:                   dryRun,
		Idempotent:               idempotent,
		ProvenanceRows:           manifest.RawCellCount,
		RawSetChecksum:           manifest.RawCellChecksum,
		DatabaseIdentityChecksum: manifest.DatabaseIdentityChecksum,
	}
	for _, cell := range manifest.Cells {
		switch cell.Decision {
		case types.ExternalExecutionTimestampDecisionProven:
			report.ProvenCount++
			report.WouldPopulateCount++
		case types.ExternalExecutionTimestampDecisionAttested:
			report.AttestedCount++
			report.WouldPopulateCount++
		case types.ExternalExecutionTimestampDecisionUnresolved:
			report.UnresolvedCount++
		case types.ExternalExecutionTimestampDecisionNull:
			report.NullCount++
		}
	}
	if dryRun {
		report.ProvenanceRows = 0
	}
	return report
}

func dryRunExternalExecutionTimestampManifest(
	ctx context.Context,
	manifest types.ExternalExecutionTimestampManifest,
) (report *types.ExternalExecutionTimestampApplyReport, finalErr error) {
	if problems := externalexecutiontimestamp.ValidateManifestDocument(manifest); len(problems) != 0 {
		return nil, errors.Join(problems...)
	}
	finalErr = RunReadOnlyTxRR(ctx, func(ctx context.Context) error {
		cellsToPopulate, err := requireManifestSnapshotForApplyInTx(ctx, manifest)
		if err != nil {
			return err
		}
		report = externalExecutionTimestampApplyReport(manifest, true, false)
		report.WouldPopulateCount = uint64(len(cellsToPopulate))
		return nil
	})
	return report, finalErr
}

var timestampSHA256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func nullableTimestampText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableTimestampUUID(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}

func requireApplyEvidence(
	request types.ExternalExecutionTimestampApplyRequest,
) error {
	if request.Manifest.State != types.ExternalExecutionTimestampManifestStateApproved {
		return errors.New("mutating apply requires an APPROVED manifest")
	}
	if problems := externalexecutiontimestamp.ValidateManifestDocument(
		request.Manifest,
	); len(problems) != 0 {
		return errors.Join(problems...)
	}
	for name, value := range map[string]string{
		"writer fence":      request.WriterFenceIdentifier,
		"backup reference":  request.BackupReference,
		"restore reference": request.RestoreVerificationReference,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	for name, value := range map[string]string{
		"backup checksum":  request.BackupChecksum,
		"restore checksum": request.RestoreVerificationChecksum,
	} {
		if !timestampSHA256Pattern.MatchString(value) {
			return fmt.Errorf("%s must be canonical SHA-256", name)
		}
	}
	return nil
}

func manifestInsertArgs(
	manifest types.ExternalExecutionTimestampManifest,
) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                          manifest.ID,
		"supersedesManifestId":        nullableTimestampUUID(manifest.SupersedesManifestID),
		"databaseIdentityChecksum":    manifest.DatabaseIdentityChecksum,
		"sourceSchemaVersion":         manifest.SourceSchemaVersion,
		"snapshotStartedAt":           manifest.SnapshotStartedAt,
		"snapshotEndedAt":             manifest.SnapshotEndedAt,
		"executionCount":              manifest.ExecutionCount,
		"eventCount":                  manifest.EventCount,
		"rawCellCount":                manifest.RawCellCount,
		"populatedCellCount":          manifest.PopulatedCellCount,
		"rawCellChecksum":             manifest.RawCellChecksum,
		"evidenceBundleReference":     manifest.EvidenceBundleReference,
		"evidenceBundleChecksum":      manifest.EvidenceBundleChecksum,
		"toolVersion":                 manifest.ToolVersion,
		"conversionExpressionVersion": manifest.ConversionExpressionVersion,
		"authorIdentity":              manifest.AuthorIdentity,
		"reviewerIdentity":            manifest.ReviewerIdentity,
		"approvedAt":                  manifest.ApprovedAt,
		"targetReleaseCommit":         manifest.TargetReleaseCommit,
		"targetImageDigest":           manifest.TargetImageDigest,
		"decisionContentChecksum":     manifest.DecisionContentChecksum,
	}
}

func provenanceInsertArgs(
	manifest types.ExternalExecutionTimestampManifest,
	cell types.ExternalExecutionTimestampCellDecision,
) pgx.NamedArgs {
	return pgx.NamedArgs{
		"manifestId":                  manifest.ID,
		"sourceTable":                 cell.SourceTable,
		"sourceRowId":                 cell.SourceRowID,
		"sourceColumn":                cell.SourceColumn,
		"columnOrdinal":               cell.ColumnOrdinal,
		"rawValue":                    cell.RawValue,
		"rawIsNull":                   cell.RawValue == nil,
		"decision":                    cell.Decision,
		"sourceZone":                  nullableTimestampText(cell.SourceZone),
		"sourceOffsetSeconds":         cell.SourceOffsetSeconds,
		"convertedValue":              cell.ConvertedValue,
		"evidenceReference":           nullableTimestampText(cell.EvidenceReference),
		"evidenceChecksum":            nullableTimestampText(cell.EvidenceChecksum),
		"approvingIdentity":           nullableTimestampText(cell.ApprovingIdentity),
		"rawCellChecksum":             cell.RawCellChecksum,
		"parentManifestChecksum":      manifest.DecisionContentChecksum,
		"conversionExpressionVersion": cell.ConversionExpressionVersion,
	}
}

func timestampShadowUpdateSQL(
	cell types.ExternalExecutionTimestampCellDecision,
) (string, error) {
	switch cell.SourceTable + "/" + cell.SourceColumn {
	case "externalexecution/created_at":
		return updateExecutionCreatedInstantSQL, nil
	case "externalexecution/updated_at":
		return updateExecutionUpdatedInstantSQL, nil
	case "externalexecution/started_at":
		return updateExecutionStartedInstantSQL, nil
	case "externalexecution/completed_at":
		return updateExecutionCompletedInstantSQL, nil
	case "externalexecution/callback_deadline_at":
		return updateExecutionDeadlineInstantSQL, nil
	case "externalexecutionevent/created_at":
		return updateEventCreatedInstantSQL, nil
	default:
		return "", errors.New("timestamp cell is outside the update allowlist")
	}
}

func applyTimestampShadowInTx(
	ctx context.Context,
	cell types.ExternalExecutionTimestampCellDecision,
) error {
	if cell.Decision != types.ExternalExecutionTimestampDecisionProven &&
		cell.Decision != types.ExternalExecutionTimestampDecisionAttested {
		return nil
	}
	statement, err := timestampShadowUpdateSQL(cell)
	if err != nil {
		return err
	}
	result, err := internalctx.GetDb(ctx).Exec(ctx, statement, pgx.NamedArgs{
		"rowId":     cell.SourceRowID,
		"raw":       *cell.RawValue,
		"converted": *cell.ConvertedValue,
	})
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf(
			"resolved timestamp shadow update affected %d rows",
			result.RowsAffected(),
		)
	}
	return nil
}

func storedManifestMatchesApplyRequest(
	stored types.ExternalExecutionTimestampManifest,
	requested types.ExternalExecutionTimestampManifest,
) bool {
	stored.State = types.ExternalExecutionTimestampManifestStateApproved
	canonicalize := func(
		cells []types.ExternalExecutionTimestampCellDecision,
	) []types.ExternalExecutionTimestampCellDecision {
		ordered := append([]types.ExternalExecutionTimestampCellDecision(nil), cells...)
		sort.Slice(ordered, func(left, right int) bool {
			return timestampCellKey(
				ordered[left].SourceTable,
				ordered[left].SourceRowID,
				ordered[left].SourceColumn,
				ordered[left].ColumnOrdinal,
			) < timestampCellKey(
				ordered[right].SourceTable,
				ordered[right].SourceRowID,
				ordered[right].SourceColumn,
				ordered[right].ColumnOrdinal,
			)
		})
		return ordered
	}
	stored.Cells = canonicalize(stored.Cells)
	requested.Cells = canonicalize(requested.Cells)
	return reflect.DeepEqual(stored, requested)
}

type externalExecutionTimestampSessionPool interface {
	Acquire(context.Context) (*pgxpool.Conn, error)
}

func acquireExternalExecutionTimestampSessionLock(
	ctx context.Context,
	connection *pgxpool.Conn,
) (acquired bool, connectionStateUncertain bool, err error) {
	lockContext, cancel := context.WithTimeout(
		ctx,
		externalExecutionTimestampSessionLockTimeout,
	)
	defer cancel()

	for {
		var locked bool
		if err := connection.QueryRow(
			lockContext,
			externalExecutionTimestampTrySessionLockSQL,
			pgx.NamedArgs{
				"migrationAdvisoryLockKey": externalexecutiontimestamp.MigrationAdvisoryLockKey,
			},
		).Scan(&locked); err != nil {
			return false, true, fmt.Errorf(
				"acquire external execution timestamp advisory lock: %w",
				err,
			)
		}
		if locked {
			return true, false, nil
		}

		timer := time.NewTimer(externalExecutionTimestampSessionLockPoll)
		select {
		case <-lockContext.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return false, false, fmt.Errorf(
				"acquire external execution timestamp advisory lock: %w",
				lockContext.Err(),
			)
		case <-timer.C:
		}
	}
}

func closeHijackedExternalExecutionTimestampConnection(
	ctx context.Context,
	connection *pgxpool.Conn,
) error {
	closeContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		externalExecutionTimestampCleanupTimeout,
	)
	defer cancel()
	return connection.Hijack().Close(closeContext)
}

func runExternalExecutionTimestampApplyTransaction(
	ctx context.Context,
	f func(context.Context) error,
) (finalErr error) {
	databasePool, ok := internalctx.GetDb(ctx).(externalExecutionTimestampSessionPool)
	if !ok {
		return errors.New(
			"external execution timestamp apply requires a database connection pool",
		)
	}
	connection, err := databasePool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf(
			"acquire external execution timestamp database connection: %w",
			err,
		)
	}

	lockHeld := false
	discardConnection := false
	transactionOpen := false
	defer func() {
		if transactionOpen {
			discardConnection = true
		}
		if discardConnection {
			if err := closeHijackedExternalExecutionTimestampConnection(ctx, connection); err != nil {
				finalErr = errors.Join(
					finalErr,
					fmt.Errorf("close uncertain timestamp apply connection: %w", err),
				)
			}
			return
		}
		if lockHeld {
			unlockContext, cancel := context.WithTimeout(
				context.WithoutCancel(ctx),
				externalExecutionTimestampCleanupTimeout,
			)
			var unlocked bool
			unlockErr := connection.QueryRow(
				unlockContext,
				externalExecutionTimestampUnlockSessionSQL,
				pgx.NamedArgs{
					"migrationAdvisoryLockKey": externalexecutiontimestamp.MigrationAdvisoryLockKey,
				},
			).Scan(&unlocked)
			cancel()
			if unlockErr != nil || !unlocked {
				if unlockErr == nil {
					unlockErr = errors.New("postgresql reported that the advisory lock was not held")
				}
				finalErr = errors.Join(
					finalErr,
					fmt.Errorf(
						"release external execution timestamp advisory lock: %w",
						unlockErr,
					),
				)
				if err := closeHijackedExternalExecutionTimestampConnection(ctx, connection); err != nil {
					finalErr = errors.Join(
						finalErr,
						fmt.Errorf("close timestamp apply connection after unlock failure: %w", err),
					)
				}
				return
			}
		}
		connection.Release()
	}()

	acquired, uncertain, err := acquireExternalExecutionTimestampSessionLock(
		ctx,
		connection,
	)
	if err != nil {
		discardConnection = uncertain
		return err
	}
	lockHeld = acquired

	tx, err := connection.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		discardConnection = true
		return fmt.Errorf("begin timestamp apply transaction: %w", err)
	}
	transactionOpen = true
	txWithAfterFunc := WithAfterFunc{Queryable: tx}
	transactionContext := internalctx.WithDb(ctx, &txWithAfterFunc)
	if err := f(transactionContext); err != nil {
		rollbackContext, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			externalExecutionTimestampCleanupTimeout,
		)
		rollbackErr := tx.Rollback(rollbackContext)
		cancel()
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			discardConnection = true
			return errors.Join(
				err,
				fmt.Errorf("rollback timestamp apply transaction: %w", rollbackErr),
			)
		}
		transactionOpen = false
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		discardConnection = true
		return fmt.Errorf("commit timestamp apply transaction: %w", err)
	}
	transactionOpen = false
	for _, afterCommit := range txWithAfterFunc.AfterFunc {
		afterCommit(ctx)
	}
	return nil
}

func ApplyExternalExecutionTimestampManifest(
	ctx context.Context,
	request types.ExternalExecutionTimestampApplyRequest,
) (report *types.ExternalExecutionTimestampApplyReport, finalErr error) {
	if !request.Apply {
		return dryRunExternalExecutionTimestampManifest(ctx, request.Manifest)
	}
	if err := requireApplyEvidence(request); err != nil {
		return nil, err
	}
	finalErr = runExternalExecutionTimestampApplyTransaction(
		ctx,
		func(ctx context.Context) error {
			database := internalctx.GetDb(ctx)
			for _, statement := range []string{
				`SET LOCAL statement_timeout = '5min'`,
				`SET LOCAL lock_timeout = '10s'`,
			} {
				if _, err := database.Exec(ctx, statement); err != nil {
					return err
				}
			}
			if _, err := database.Exec(ctx, `
LOCK TABLE ExternalExecution, ExternalExecutionEvent
IN SHARE ROW EXCLUSIVE MODE`); err != nil {
				return err
			}
			contract, err := readExternalExecutionTimestampSchemaContractInTx(ctx)
			if err != nil {
				return err
			}
			if contract.CatalogVersion < 138 ||
				request.Manifest.SourceSchemaVersion != contract.IdentitySourceVersion {
				return errors.New("mutating apply requires compatible schema 138")
			}
			if err := requireExternalExecutionTimestampGuardsInTx(ctx); err != nil {
				return err
			}
			existing, existingState, err := readStoredExternalExecutionTimestampManifestInTx(
				ctx,
				request.Manifest.ID,
			)
			if err == nil {
				if existingState != types.ExternalExecutionTimestampManifestStateVerified ||
					existing.DecisionContentChecksum != request.Manifest.DecisionContentChecksum ||
					!storedManifestMatchesApplyRequest(*existing, request.Manifest) {
					return errors.New("manifest id collision")
				}
				verified, err := verifyExternalExecutionTimestampManifestInTx(
					ctx,
					request.Manifest.ID,
				)
				if err != nil {
					return err
				}
				report = externalExecutionTimestampApplyReport(
					request.Manifest,
					false,
					true,
				)
				report.WouldPopulateCount = 0
				report.PopulatedShadowCount = 0
				report.ProvenanceRows = verified.ProvenanceRows
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			cellsToPopulate, err := requireManifestSnapshotForApplyInTx(
				ctx, request.Manifest,
			)
			if err != nil {
				return err
			}
			if _, err := database.Exec(
				ctx,
				insertExternalExecutionTimestampManifestSQL,
				manifestInsertArgs(request.Manifest),
			); err != nil {
				return err
			}
			for _, cell := range request.Manifest.Cells {
				if _, err := database.Exec(
					ctx,
					insertExternalExecutionTimestampProvenanceSQL,
					provenanceInsertArgs(request.Manifest, cell),
				); err != nil {
					return err
				}
				key := timestampCellKey(
					cell.SourceTable,
					cell.SourceRowID,
					cell.SourceColumn,
					cell.ColumnOrdinal,
				)
				if _, shouldPopulate := cellsToPopulate[key]; shouldPopulate {
					if err := applyTimestampShadowInTx(ctx, cell); err != nil {
						return err
					}
				}
			}
			for _, transition := range []struct {
				statement     string
				expectedState string
			}{
				{`UPDATE ExternalExecutionTimestampManifest
		      SET state='APPLIED', applied_at=clock_timestamp()
		      WHERE id=@id AND state='APPROVED'`, "APPLIED"},
				{`UPDATE ExternalExecutionTimestampManifest
		      SET state='VERIFIED', verified_at=clock_timestamp()
		      WHERE id=@id AND state='APPLIED'`, "VERIFIED"},
			} {
				result, err := database.Exec(
					ctx,
					transition.statement,
					pgx.NamedArgs{"id": request.Manifest.ID},
				)
				if err != nil {
					return err
				}
				if result.RowsAffected() != 1 {
					return fmt.Errorf(
						"could not advance manifest to %s",
						transition.expectedState,
					)
				}
			}
			verified, err := verifyExternalExecutionTimestampManifestInTx(
				ctx,
				request.Manifest.ID,
			)
			if err != nil {
				return err
			}
			report = externalExecutionTimestampApplyReport(
				request.Manifest, false, false,
			)
			report.WouldPopulateCount = uint64(len(cellsToPopulate))
			report.PopulatedShadowCount = uint64(len(cellsToPopulate))
			report.ProvenanceRows = verified.ProvenanceRows
			return nil
		},
	)
	return report, finalErr
}
