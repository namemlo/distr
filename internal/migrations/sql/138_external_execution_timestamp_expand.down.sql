SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

LOCK TABLE
  ExternalExecution,
  ExternalExecutionEvent,
  ExternalExecutionTimestampManifest,
  ExternalExecutionTimestampDeletionTombstone,
  ExternalExecutionTimestampExpandState
IN ACCESS EXCLUSIVE MODE;

DO $$
DECLARE
  transition_kind TEXT;
BEGIN
  IF EXISTS (
    SELECT 1
    FROM ExternalExecutionTimestampManifest
    WHERE state IN ('APPLIED', 'VERIFIED')
  ) THEN
    RAISE EXCEPTION
      'downgrade crossing 138 is forbidden after timestamp manifest application';
  END IF;

  IF EXISTS (
    SELECT 1 FROM ExternalExecutionTimestampDeletionTombstone
  ) THEN
    RAISE EXCEPTION
      'downgrade crossing 138 is forbidden after timestamp retention';
  END IF;

  SELECT state.transition_kind
  INTO transition_kind
  FROM ExternalExecutionTimestampExpandState state
  WHERE state.singleton;

  IF transition_kind = 'ZERO_HISTORY'
     AND (
       EXISTS (SELECT 1 FROM ExternalExecution)
       OR EXISTS (SELECT 1 FROM ExternalExecutionEvent)
     ) THEN
    RAISE EXCEPTION
      'downgrade crossing 138 is forbidden after ZERO_HISTORY timestamp rows';
  END IF;
END;
$$;

DROP TABLE ExternalExecutionTimestampContractGate;

DROP TRIGGER ExternalExecution_timestamp_deletion_tombstone
  ON ExternalExecution;
DROP TRIGGER ExternalExecutionEvent_timestamp_deletion_tombstone
  ON ExternalExecutionEvent;
DROP FUNCTION external_execution_timestamp_capture_deletion();

DROP TRIGGER ExternalExecutionTimestampDeletionTombstone_reject_truncate
  ON ExternalExecutionTimestampDeletionTombstone;
DROP TRIGGER ExternalExecutionTimestampCellProvenance_reject_truncate
  ON ExternalExecutionTimestampCellProvenance;
DROP TRIGGER ExternalExecutionTimestampManifest_reject_truncate
  ON ExternalExecutionTimestampManifest;
DROP TRIGGER ExternalExecutionTimestampExpandState_reject_truncate
  ON ExternalExecutionTimestampExpandState;
DROP FUNCTION external_execution_timestamp_reject_truncate();

DROP TRIGGER ExternalExecutionTimestampCellProvenance_append_only
  ON ExternalExecutionTimestampCellProvenance;
DROP FUNCTION external_execution_timestamp_provenance_append_only();

DROP TRIGGER ExternalExecutionTimestampDeletionTombstone_append_only
  ON ExternalExecutionTimestampDeletionTombstone;
DROP FUNCTION external_execution_timestamp_tombstone_append_only();
DROP TABLE ExternalExecutionTimestampDeletionTombstone;

DROP TRIGGER ExternalExecutionTimestampManifest_lifecycle
  ON ExternalExecutionTimestampManifest;
DROP FUNCTION external_execution_timestamp_manifest_lifecycle();
DROP TABLE ExternalExecutionTimestampCellProvenance;
DROP TABLE ExternalExecutionTimestampManifest;

DROP TRIGGER ExternalExecutionTimestampExpandState_append_only
  ON ExternalExecutionTimestampExpandState;
DROP FUNCTION external_execution_timestamp_expand_state_append_only();
DROP TABLE ExternalExecutionTimestampExpandState;

DROP TRIGGER ExternalExecutionEvent_timestamp_pair_guard
  ON ExternalExecutionEvent;
DROP TRIGGER ExternalExecution_timestamp_pair_guard
  ON ExternalExecution;
DROP FUNCTION external_execution_timestamp_pair_guard();

DROP TRIGGER ExternalExecution_lifecycle_pair_one_shot
  ON ExternalExecution;
DROP FUNCTION external_execution_lifecycle_pair_one_shot();

DROP INDEX ExternalExecution_task_instant_next;
DROP INDEX ExternalExecution_organization_status_instant_next;

ALTER TABLE ExternalExecutionEvent
  DROP COLUMN created_at_instant,
  ALTER COLUMN created_at SET DEFAULT now();

ALTER TABLE ExternalExecution
  DROP COLUMN callback_deadline_at_instant,
  DROP COLUMN completed_at_instant,
  DROP COLUMN started_at_instant,
  DROP COLUMN updated_at_instant,
  DROP COLUMN created_at_instant,
  ALTER COLUMN created_at SET DEFAULT now(),
  ALTER COLUMN updated_at SET DEFAULT now();
