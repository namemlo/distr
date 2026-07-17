SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE ExternalExecution
  ADD COLUMN created_at_instant TIMESTAMPTZ,
  ADD COLUMN updated_at_instant TIMESTAMPTZ,
  ADD COLUMN started_at_instant TIMESTAMPTZ,
  ADD COLUMN completed_at_instant TIMESTAMPTZ,
  ADD COLUMN callback_deadline_at_instant TIMESTAMPTZ;

ALTER TABLE ExternalExecutionEvent
  ADD COLUMN created_at_instant TIMESTAMPTZ;

ALTER TABLE ExternalExecution
  ALTER COLUMN created_at SET DEFAULT (CURRENT_TIMESTAMP AT TIME ZONE 'UTC'),
  ALTER COLUMN updated_at SET DEFAULT (CURRENT_TIMESTAMP AT TIME ZONE 'UTC'),
  ALTER COLUMN created_at_instant SET DEFAULT CURRENT_TIMESTAMP,
  ALTER COLUMN updated_at_instant SET DEFAULT CURRENT_TIMESTAMP;

ALTER TABLE ExternalExecutionEvent
  ALTER COLUMN created_at SET DEFAULT (CURRENT_TIMESTAMP AT TIME ZONE 'UTC'),
  ALTER COLUMN created_at_instant SET DEFAULT CURRENT_TIMESTAMP;

CREATE INDEX ExternalExecution_organization_status_instant_next
  ON ExternalExecution (organization_id, status, updated_at_instant DESC, id);
CREATE INDEX ExternalExecution_task_instant_next
  ON ExternalExecution (task_id, created_at_instant, id);

CREATE TABLE ExternalExecutionTimestampManifest (
  id UUID PRIMARY KEY,
  supersedes_manifest_id UUID
    REFERENCES ExternalExecutionTimestampManifest(id)
    ON UPDATE RESTRICT ON DELETE RESTRICT,
  database_identity_checksum TEXT NOT NULL
    CHECK (database_identity_checksum ~ '^sha256:[0-9a-f]{64}$'),
  source_schema_version INTEGER NOT NULL,
  snapshot_started_at TIMESTAMPTZ NOT NULL,
  snapshot_ended_at TIMESTAMPTZ NOT NULL,
  execution_count BIGINT NOT NULL CHECK (execution_count >= 0),
  event_count BIGINT NOT NULL CHECK (event_count >= 0),
  raw_cell_count BIGINT NOT NULL CHECK (raw_cell_count >= 0),
  populated_cell_count BIGINT NOT NULL
    CHECK (populated_cell_count >= 0 AND populated_cell_count <= raw_cell_count),
  raw_cell_checksum TEXT NOT NULL
    CHECK (raw_cell_checksum ~ '^sha256:[0-9a-f]{64}$'),
  evidence_bundle_reference TEXT,
  evidence_bundle_checksum TEXT,
  tool_version TEXT NOT NULL CHECK (length(btrim(tool_version)) > 0),
  conversion_expression_version TEXT NOT NULL
    CHECK (conversion_expression_version = 'external-execution-offset/v1'),
  author_identity TEXT,
  reviewer_identity TEXT,
  approved_at TIMESTAMPTZ,
  target_release_commit TEXT,
  target_image_digest TEXT,
  state TEXT NOT NULL CHECK (
    state IN ('DRAFT', 'APPROVED', 'APPLIED', 'VERIFIED', 'REVOKED_BEFORE_APPLY')
  ),
  decision_content_checksum TEXT NOT NULL
    CHECK (decision_content_checksum ~ '^sha256:[0-9a-f]{64}$'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  applied_at TIMESTAMPTZ,
  verified_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ,
  CONSTRAINT externalexecutiontimestampmanifest_source_schema_137
    CHECK (source_schema_version = 137),
  CONSTRAINT externalexecutiontimestampmanifest_not_self_superseding
    CHECK (supersedes_manifest_id IS NULL OR supersedes_manifest_id <> id),
  CONSTRAINT externalexecutiontimestampmanifest_snapshot_order
    CHECK (snapshot_ended_at >= snapshot_started_at),
  CONSTRAINT externalexecutiontimestampmanifest_expected_cell_count
    CHECK (raw_cell_count = 5 * execution_count + event_count),
  CONSTRAINT externalexecutiontimestampmanifest_metadata_all_or_none CHECK (
    (
      evidence_bundle_reference IS NULL
      AND evidence_bundle_checksum IS NULL
      AND author_identity IS NULL
      AND reviewer_identity IS NULL
      AND target_release_commit IS NULL
      AND target_image_digest IS NULL
    )
    OR
    (
      evidence_bundle_reference IS NOT NULL
      AND evidence_bundle_checksum IS NOT NULL
      AND author_identity IS NOT NULL
      AND reviewer_identity IS NOT NULL
      AND target_release_commit IS NOT NULL
      AND target_image_digest IS NOT NULL
      AND length(btrim(evidence_bundle_reference)) > 0
      AND evidence_bundle_checksum ~ '^sha256:[0-9a-f]{64}$'
      AND length(btrim(author_identity)) > 0
      AND length(btrim(reviewer_identity)) > 0
      AND author_identity <> reviewer_identity
      AND target_release_commit ~ '^[0-9a-f]{40}$'
      AND target_image_digest ~ '^sha256:[0-9a-f]{64}$'
    )
  ),
  CONSTRAINT externalexecutiontimestampmanifest_lifecycle_shape CHECK (
    (
      state = 'DRAFT'
      AND approved_at IS NULL AND applied_at IS NULL
      AND verified_at IS NULL AND revoked_at IS NULL
    )
    OR
    (
      state = 'APPROVED'
      AND approved_at IS NOT NULL
      AND evidence_bundle_reference IS NOT NULL
      AND applied_at IS NULL AND verified_at IS NULL AND revoked_at IS NULL
    )
    OR
    (
      state = 'APPLIED'
      AND approved_at IS NOT NULL AND applied_at IS NOT NULL
      AND applied_at >= approved_at
      AND evidence_bundle_reference IS NOT NULL
      AND verified_at IS NULL AND revoked_at IS NULL
    )
    OR
    (
      state = 'VERIFIED'
      AND approved_at IS NOT NULL AND applied_at IS NOT NULL
      AND verified_at IS NOT NULL
      AND applied_at >= approved_at AND verified_at >= applied_at
      AND evidence_bundle_reference IS NOT NULL
      AND revoked_at IS NULL
    )
    OR
    (
      state = 'REVOKED_BEFORE_APPLY'
      AND applied_at IS NULL AND verified_at IS NULL
      AND revoked_at IS NOT NULL AND revoked_at >= created_at
      AND (
        approved_at IS NULL
        OR (
          evidence_bundle_reference IS NOT NULL
          AND revoked_at >= approved_at
        )
      )
    )
  ),
  CONSTRAINT externalexecutiontimestampmanifest_id_checksum_unique
    UNIQUE (id, decision_content_checksum)
);

CREATE UNIQUE INDEX externalexecutiontimestampmanifest_active_parent_unique
  ON ExternalExecutionTimestampManifest (supersedes_manifest_id)
  NULLS NOT DISTINCT
  WHERE state <> 'REVOKED_BEFORE_APPLY';

CREATE TABLE ExternalExecutionTimestampCellProvenance (
  manifest_id UUID NOT NULL,
  source_table TEXT NOT NULL,
  source_row_id UUID NOT NULL,
  source_column TEXT NOT NULL,
  column_ordinal SMALLINT NOT NULL,
  raw_value TIMESTAMP WITHOUT TIME ZONE,
  raw_is_null BOOLEAN NOT NULL,
  decision TEXT NOT NULL
    CHECK (decision IN ('PROVEN', 'ATTESTED', 'UNRESOLVED', 'NULL_VALUE')),
  source_zone TEXT,
  source_offset_seconds INTEGER
    CHECK (source_offset_seconds BETWEEN -64800 AND 64800),
  converted_value TIMESTAMPTZ,
  evidence_reference TEXT,
  evidence_checksum TEXT CHECK (
    evidence_checksum IS NULL
    OR evidence_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  approving_identity TEXT,
  raw_cell_checksum TEXT NOT NULL
    CHECK (raw_cell_checksum ~ '^sha256:[0-9a-f]{64}$'),
  parent_manifest_checksum TEXT NOT NULL
    CHECK (parent_manifest_checksum ~ '^sha256:[0-9a-f]{64}$'),
  conversion_expression_version TEXT NOT NULL
    CHECK (conversion_expression_version = 'external-execution-offset/v1'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (manifest_id, source_table, source_row_id, source_column),
  CONSTRAINT externalexecutiontimestampcell_manifest_checksum_fk
    FOREIGN KEY (manifest_id, parent_manifest_checksum)
    REFERENCES ExternalExecutionTimestampManifest(id, decision_content_checksum)
    ON UPDATE RESTRICT ON DELETE RESTRICT,
  CONSTRAINT externalexecutiontimestampcell_raw_null_match
    CHECK (raw_is_null = (raw_value IS NULL)),
  CONSTRAINT externalexecutiontimestampcell_allowlist CHECK (
    (
      source_table = 'externalexecution'
      AND (source_column, column_ordinal) IN (
        ('created_at', 1),
        ('updated_at', 2),
        ('started_at', 3),
        ('completed_at', 4),
        ('callback_deadline_at', 5)
      )
    )
    OR
    (
      source_table = 'externalexecutionevent'
      AND source_column = 'created_at'
      AND column_ordinal = 6
    )
  ),
  CONSTRAINT externalexecutiontimestampcell_decision_shape CHECK (
    (
      decision = 'NULL_VALUE'
      AND raw_is_null AND raw_value IS NULL
      AND source_zone IS NULL AND source_offset_seconds IS NULL
      AND converted_value IS NULL AND evidence_reference IS NULL
      AND evidence_checksum IS NULL AND approving_identity IS NULL
    )
    OR
    (
      decision = 'UNRESOLVED'
      AND NOT raw_is_null AND raw_value IS NOT NULL
      AND source_zone IS NULL AND source_offset_seconds IS NULL
      AND converted_value IS NULL AND evidence_reference IS NULL
      AND evidence_checksum IS NULL AND approving_identity IS NULL
    )
    OR
    (
      decision IN ('PROVEN', 'ATTESTED')
      AND NOT raw_is_null AND raw_value IS NOT NULL
      AND source_offset_seconds IS NOT NULL
      AND converted_value IS NOT NULL
      AND evidence_reference IS NOT NULL
      AND evidence_checksum IS NOT NULL
      AND approving_identity IS NOT NULL
      AND length(btrim(evidence_reference)) > 0
      AND evidence_checksum ~ '^sha256:[0-9a-f]{64}$'
      AND length(btrim(approving_identity)) > 0
      AND (source_zone IS NULL OR length(btrim(source_zone)) > 0)
    )
  )
);

CREATE TABLE ExternalExecutionTimestampExpandState (
  singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
  transition_kind TEXT NOT NULL
    CHECK (transition_kind IN ('ZERO_HISTORY', 'MANIFEST_REQUIRED')),
  source_schema_version INTEGER NOT NULL CHECK (source_schema_version = 137),
  transition_execution_count BIGINT NOT NULL
    CHECK (transition_execution_count >= 0),
  transition_event_count BIGINT NOT NULL CHECK (transition_event_count >= 0),
  transition_raw_cell_count BIGINT NOT NULL
    CHECK (transition_raw_cell_count >= 0),
  transitioned_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT externalexecutiontimestampexpandstate_cell_count CHECK (
    transition_raw_cell_count =
      5 * transition_execution_count + transition_event_count
  ),
  CONSTRAINT externalexecutiontimestampexpandstate_kind_matches_counts CHECK (
    (
      transition_kind = 'ZERO_HISTORY'
      AND transition_execution_count = 0
      AND transition_event_count = 0
      AND transition_raw_cell_count = 0
    )
    OR
    (
      transition_kind = 'MANIFEST_REQUIRED'
      AND (transition_execution_count > 0 OR transition_event_count > 0)
    )
  )
);

CREATE TABLE ExternalExecutionTimestampContractGate (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  manifest_id UUID NOT NULL
    REFERENCES ExternalExecutionTimestampManifest(id)
    ON UPDATE RESTRICT ON DELETE RESTRICT,
  expected_schema_version INTEGER NOT NULL
    CHECK (expected_schema_version >= 138),
  contract_migration_version INTEGER NOT NULL
    CHECK (contract_migration_version > expected_schema_version),
  target_release_commit TEXT NOT NULL
    CHECK (target_release_commit ~ '^[0-9a-f]{40}$'),
  target_image_digest TEXT NOT NULL
    CHECK (target_image_digest ~ '^sha256:[0-9a-f]{64}$'),
  backup_reference TEXT NOT NULL CHECK (length(btrim(backup_reference)) > 0),
  backup_checksum TEXT NOT NULL
    CHECK (backup_checksum ~ '^sha256:[0-9a-f]{64}$'),
  restore_verification_reference TEXT NOT NULL
    CHECK (length(btrim(restore_verification_reference)) > 0),
  restore_verification_checksum TEXT NOT NULL
    CHECK (restore_verification_checksum ~ '^sha256:[0-9a-f]{64}$'),
  writer_fence_identifier TEXT NOT NULL
    CHECK (length(btrim(writer_fence_identifier)) > 0),
  prepared_by TEXT NOT NULL CHECK (length(btrim(prepared_by)) > 0),
  prepared_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  CONSTRAINT externalexecutiontimestampcontractgate_expiry
    CHECK (expires_at > prepared_at),
  CONSTRAINT externalexecutiontimestampcontractgate_consumption_window CHECK (
    consumed_at IS NULL
    OR (consumed_at >= prepared_at AND consumed_at <= expires_at)
  )
);

CREATE FUNCTION external_execution_timestamp_provenance_append_only()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'external execution timestamp provenance is append-only';
END;
$$;

CREATE TRIGGER ExternalExecutionTimestampCellProvenance_append_only
BEFORE UPDATE OR DELETE ON ExternalExecutionTimestampCellProvenance
FOR EACH ROW
EXECUTE FUNCTION external_execution_timestamp_provenance_append_only();

CREATE FUNCTION external_execution_timestamp_expand_state_append_only()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'external execution timestamp expand state is append-only';
END;
$$;

CREATE TRIGGER ExternalExecutionTimestampExpandState_append_only
BEFORE UPDATE OR DELETE ON ExternalExecutionTimestampExpandState
FOR EACH ROW
EXECUTE FUNCTION external_execution_timestamp_expand_state_append_only();

CREATE FUNCTION external_execution_timestamp_pair_guard()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
  source_table TEXT;
  error_prefix TEXT;
  raw_columns TEXT[];
  instant_columns TEXT[];
  column_ordinals SMALLINT[];
  pair_index INTEGER;
  raw_column TEXT;
  instant_column TEXT;
  column_ordinal SMALLINT;
  old_row JSONB;
  new_row JSONB;
  old_raw TIMESTAMP WITHOUT TIME ZONE;
  old_instant TIMESTAMPTZ;
  new_raw TIMESTAMP WITHOUT TIME ZONE;
  new_instant TIMESTAMPTZ;
  raw_changed BOOLEAN;
  instant_changed BOOLEAN;
  has_current_provenance BOOLEAN;
BEGIN
  CASE TG_TABLE_NAME
    WHEN 'externalexecution' THEN
      source_table := 'externalexecution';
      error_prefix := 'external execution';
      raw_columns := ARRAY[
        'created_at', 'updated_at', 'started_at', 'completed_at',
        'callback_deadline_at'
      ];
      instant_columns := ARRAY[
        'created_at_instant', 'updated_at_instant', 'started_at_instant',
        'completed_at_instant', 'callback_deadline_at_instant'
      ];
      column_ordinals := ARRAY[1, 2, 3, 4, 5]::SMALLINT[];
    WHEN 'externalexecutionevent' THEN
      source_table := 'externalexecutionevent';
      error_prefix := 'external execution event';
      raw_columns := ARRAY['created_at'];
      instant_columns := ARRAY['created_at_instant'];
      column_ordinals := ARRAY[6]::SMALLINT[];
    ELSE
      RAISE EXCEPTION 'external execution timestamp pair guard is attached to unsupported table %',
        TG_TABLE_NAME;
  END CASE;

  new_row := to_jsonb(NEW);
  IF TG_OP <> 'INSERT' THEN
    old_row := to_jsonb(OLD);
  END IF;

  FOR pair_index IN 1..array_length(raw_columns, 1) LOOP
    raw_column := raw_columns[pair_index];
    instant_column := instant_columns[pair_index];
    column_ordinal := column_ordinals[pair_index];
    new_raw := (new_row ->> raw_column)::TIMESTAMP WITHOUT TIME ZONE;
    new_instant := (new_row ->> instant_column)::TIMESTAMPTZ;

    IF TG_OP = 'INSERT' THEN
      IF (new_raw IS NULL) <> (new_instant IS NULL)
         OR (
           new_raw IS NOT NULL
           AND new_raw IS DISTINCT FROM (new_instant AT TIME ZONE 'UTC')
         ) THEN
        RAISE EXCEPTION '% % must be one exact UTC pair',
          error_prefix, raw_column;
      END IF;
      CONTINUE;
    END IF;

    old_raw := (old_row ->> raw_column)::TIMESTAMP WITHOUT TIME ZONE;
    old_instant := (old_row ->> instant_column)::TIMESTAMPTZ;
    raw_changed := new_raw IS DISTINCT FROM old_raw;
    instant_changed := new_instant IS DISTINCT FROM old_instant;

    IF NOT raw_changed AND NOT instant_changed THEN
      CONTINUE;
    END IF;

    IF NOT raw_changed
       AND old_raw IS NOT NULL
       AND old_instant IS NULL
       AND new_instant IS NOT NULL THEN
      EXECUTE format(
        'SELECT EXISTS (
           SELECT 1
           FROM %I.ExternalExecutionTimestampCellProvenance provenance
           JOIN %I.ExternalExecutionTimestampManifest manifest
             ON manifest.id = provenance.manifest_id
           WHERE provenance.source_table = $1
             AND provenance.source_row_id = $2
             AND provenance.source_column = $3
             AND provenance.column_ordinal = $4
             AND provenance.raw_value IS NOT DISTINCT FROM $5
             AND provenance.decision IN (''PROVEN'', ''ATTESTED'')
             AND provenance.converted_value IS NOT DISTINCT FROM $6
             AND manifest.state = ''APPROVED''
             AND provenance.xmin::text::numeric = mod(
               pg_current_xact_id()::text::numeric,
               4294967296::numeric
             )
         )',
        TG_TABLE_SCHEMA,
        TG_TABLE_SCHEMA
      )
      INTO has_current_provenance
      USING source_table, NEW.id, raw_column, column_ordinal,
        new_raw, new_instant;
      IF has_current_provenance THEN
        CONTINUE;
      END IF;
      RAISE EXCEPTION '% % shadow-only update requires exact current-transaction provenance',
        error_prefix, raw_column;
    END IF;

    IF raw_changed <> instant_changed THEN
      RAISE EXCEPTION '% % raw update requires its instant pair',
        error_prefix, raw_column;
    END IF;
    IF (new_raw IS NULL) <> (new_instant IS NULL)
       OR (
         new_raw IS NOT NULL
         AND new_raw IS DISTINCT FROM (new_instant AT TIME ZONE 'UTC')
       ) THEN
      RAISE EXCEPTION '% % update must be one exact UTC pair',
        error_prefix, raw_column;
    END IF;
  END LOOP;

  RETURN NEW;
END;
$$;

CREATE TRIGGER ExternalExecution_timestamp_pair_guard
BEFORE INSERT OR UPDATE OF
  created_at, created_at_instant,
  updated_at, updated_at_instant,
  started_at, started_at_instant,
  completed_at, completed_at_instant,
  callback_deadline_at, callback_deadline_at_instant
ON ExternalExecution
FOR EACH ROW
EXECUTE FUNCTION external_execution_timestamp_pair_guard();

CREATE TRIGGER ExternalExecutionEvent_timestamp_pair_guard
BEFORE INSERT OR UPDATE OF created_at, created_at_instant
ON ExternalExecutionEvent
FOR EACH ROW
EXECUTE FUNCTION external_execution_timestamp_pair_guard();

CREATE FUNCTION external_execution_lifecycle_pair_one_shot()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.started_at_instant IS NOT NULL THEN
    IF NEW.started_at IS DISTINCT FROM OLD.started_at
       OR NEW.started_at_instant IS DISTINCT FROM OLD.started_at_instant THEN
      RAISE EXCEPTION 'external execution started_at pair is immutable';
    END IF;
  ELSIF OLD.started_at IS NOT NULL THEN
    IF NEW.started_at IS DISTINCT FROM OLD.started_at THEN
      RAISE EXCEPTION 'external execution started_at pair is immutable';
    END IF;
  ELSIF (NEW.started_at IS NULL) <> (NEW.started_at_instant IS NULL)
     OR (
       NEW.started_at IS NOT NULL
       AND NEW.started_at IS DISTINCT FROM
         (NEW.started_at_instant AT TIME ZONE 'UTC')
     ) THEN
    RAISE EXCEPTION 'external execution started_at must resolve to one exact UTC pair';
  END IF;

  IF OLD.completed_at_instant IS NOT NULL THEN
    IF NEW.completed_at IS DISTINCT FROM OLD.completed_at
       OR NEW.completed_at_instant IS DISTINCT FROM OLD.completed_at_instant THEN
      RAISE EXCEPTION 'external execution completed_at pair is immutable';
    END IF;
  ELSIF OLD.completed_at IS NOT NULL THEN
    IF NEW.completed_at IS DISTINCT FROM OLD.completed_at THEN
      RAISE EXCEPTION 'external execution completed_at pair is immutable';
    END IF;
  ELSIF (NEW.completed_at IS NULL) <> (NEW.completed_at_instant IS NULL)
     OR (
       NEW.completed_at IS NOT NULL
       AND NEW.completed_at IS DISTINCT FROM
         (NEW.completed_at_instant AT TIME ZONE 'UTC')
     ) THEN
    RAISE EXCEPTION 'external execution completed_at must resolve to one exact UTC pair';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER ExternalExecution_lifecycle_pair_one_shot
BEFORE UPDATE OF
  started_at, started_at_instant, completed_at, completed_at_instant
ON ExternalExecution
FOR EACH ROW
EXECUTE FUNCTION external_execution_lifecycle_pair_one_shot();

CREATE FUNCTION external_execution_timestamp_manifest_lifecycle()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state IN ('DRAFT', 'APPROVED') THEN
      RETURN NEW;
    END IF;
    RAISE EXCEPTION 'invalid initial manifest lifecycle state %', NEW.state;
  END IF;

  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'external execution timestamp manifest is append-only';
  END IF;

  IF ROW(
    OLD.id, OLD.supersedes_manifest_id, OLD.database_identity_checksum,
    OLD.source_schema_version, OLD.snapshot_started_at, OLD.snapshot_ended_at,
    OLD.execution_count, OLD.event_count, OLD.raw_cell_count,
    OLD.populated_cell_count, OLD.raw_cell_checksum,
    OLD.evidence_bundle_reference, OLD.evidence_bundle_checksum,
    OLD.tool_version, OLD.conversion_expression_version,
    OLD.author_identity, OLD.reviewer_identity, OLD.target_release_commit,
    OLD.target_image_digest, OLD.decision_content_checksum, OLD.created_at
  ) IS DISTINCT FROM ROW(
    NEW.id, NEW.supersedes_manifest_id, NEW.database_identity_checksum,
    NEW.source_schema_version, NEW.snapshot_started_at, NEW.snapshot_ended_at,
    NEW.execution_count, NEW.event_count, NEW.raw_cell_count,
    NEW.populated_cell_count, NEW.raw_cell_checksum,
    NEW.evidence_bundle_reference, NEW.evidence_bundle_checksum,
    NEW.tool_version, NEW.conversion_expression_version,
    NEW.author_identity, NEW.reviewer_identity, NEW.target_release_commit,
    NEW.target_image_digest, NEW.decision_content_checksum, NEW.created_at
  ) THEN
    RAISE EXCEPTION 'external execution timestamp manifest content is immutable';
  END IF;

  IF OLD.state = 'DRAFT' AND NEW.state = 'APPROVED'
     AND OLD.approved_at IS NULL AND NEW.approved_at IS NOT NULL
     AND NEW.applied_at IS NOT DISTINCT FROM OLD.applied_at
     AND NEW.verified_at IS NOT DISTINCT FROM OLD.verified_at
     AND NEW.revoked_at IS NOT DISTINCT FROM OLD.revoked_at THEN
    RETURN NEW;
  END IF;
  IF OLD.state IN ('DRAFT', 'APPROVED')
     AND NEW.state = 'REVOKED_BEFORE_APPLY'
     AND NEW.approved_at IS NOT DISTINCT FROM OLD.approved_at
     AND NEW.applied_at IS NOT DISTINCT FROM OLD.applied_at
     AND NEW.verified_at IS NOT DISTINCT FROM OLD.verified_at
     AND OLD.revoked_at IS NULL AND NEW.revoked_at IS NOT NULL THEN
    RETURN NEW;
  END IF;
  IF OLD.state = 'APPROVED' AND NEW.state = 'APPLIED'
     AND NEW.approved_at IS NOT DISTINCT FROM OLD.approved_at
     AND OLD.applied_at IS NULL AND NEW.applied_at IS NOT NULL
     AND NEW.verified_at IS NOT DISTINCT FROM OLD.verified_at
     AND NEW.revoked_at IS NOT DISTINCT FROM OLD.revoked_at THEN
    RETURN NEW;
  END IF;
  IF OLD.state = 'APPLIED' AND NEW.state = 'VERIFIED'
     AND NEW.approved_at IS NOT DISTINCT FROM OLD.approved_at
     AND NEW.applied_at IS NOT DISTINCT FROM OLD.applied_at
     AND OLD.verified_at IS NULL AND NEW.verified_at IS NOT NULL
     AND NEW.revoked_at IS NOT DISTINCT FROM OLD.revoked_at THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'invalid manifest lifecycle transition from % to %',
    OLD.state, NEW.state;
END;
$$;

CREATE TRIGGER ExternalExecutionTimestampManifest_lifecycle
BEFORE INSERT OR UPDATE OR DELETE ON ExternalExecutionTimestampManifest
FOR EACH ROW
EXECUTE FUNCTION external_execution_timestamp_manifest_lifecycle();

CREATE FUNCTION external_execution_timestamp_reject_truncate()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'external execution timestamp evidence cannot be truncated';
END;
$$;

CREATE TRIGGER ExternalExecutionTimestampExpandState_reject_truncate
BEFORE TRUNCATE ON ExternalExecutionTimestampExpandState
FOR EACH STATEMENT
EXECUTE FUNCTION external_execution_timestamp_reject_truncate();

CREATE TRIGGER ExternalExecutionTimestampManifest_reject_truncate
BEFORE TRUNCATE ON ExternalExecutionTimestampManifest
FOR EACH STATEMENT
EXECUTE FUNCTION external_execution_timestamp_reject_truncate();

CREATE TRIGGER ExternalExecutionTimestampCellProvenance_reject_truncate
BEFORE TRUNCATE ON ExternalExecutionTimestampCellProvenance
FOR EACH STATEMENT
EXECUTE FUNCTION external_execution_timestamp_reject_truncate();

WITH transition_counts AS (
  SELECT
    (SELECT count(*) FROM ExternalExecution) AS execution_count,
    (SELECT count(*) FROM ExternalExecutionEvent) AS event_count
)
INSERT INTO ExternalExecutionTimestampExpandState (
  singleton, transition_kind, source_schema_version,
  transition_execution_count, transition_event_count,
  transition_raw_cell_count, transitioned_at
)
SELECT
  TRUE,
  CASE
    WHEN execution_count = 0 AND event_count = 0 THEN 'ZERO_HISTORY'
    ELSE 'MANIFEST_REQUIRED'
  END,
  137,
  execution_count,
  event_count,
  5 * execution_count + event_count,
  CURRENT_TIMESTAMP
FROM transition_counts;
