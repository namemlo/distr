DO $$
BEGIN
  LOCK TABLE
    ExecutionAttempt,
    ExecutionFence,
    ExecutionIntent,
    ExecutionEvent
  IN ACCESS EXCLUSIVE MODE;

  IF EXISTS (SELECT 1 FROM ExecutionAttempt)
     OR EXISTS (SELECT 1 FROM ExecutionFence)
     OR EXISTS (SELECT 1 FROM ExecutionIntent)
     OR EXISTS (SELECT 1 FROM ExecutionEvent) THEN
    RAISE EXCEPTION
      'refusing migration 157 rollback while execution v2 evidence exists';
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS ExecutionEvent_no_truncate ON ExecutionEvent;
DROP TRIGGER IF EXISTS ExecutionEvent_append_only ON ExecutionEvent;
DROP TRIGGER IF EXISTS ExecutionIntent_no_truncate ON ExecutionIntent;
DROP TRIGGER IF EXISTS ExecutionIntent_append_only ON ExecutionIntent;
DROP FUNCTION IF EXISTS execution_protocol_v2_append_only_guard();

DROP INDEX IF EXISTS ExecutionEvent_attempt_sequence;
DROP INDEX IF EXISTS ExecutionAttempt_organization_status;
DROP TABLE IF EXISTS ExecutionEvent;
DROP TABLE IF EXISTS ExecutionIntent;
DROP INDEX IF EXISTS ExecutionFence_active_resource;
DROP TABLE IF EXISTS ExecutionFence;
DROP TABLE IF EXISTS ExecutionAttempt;

ALTER TABLE Task
  DROP CONSTRAINT IF EXISTS task_protocol_version_check,
  DROP COLUMN IF EXISTS protocol_version;
