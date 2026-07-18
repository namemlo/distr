DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM ExecutionCancelRequest)
     OR EXISTS (SELECT 1 FROM ExecutionStatusQuery)
     OR EXISTS (SELECT 1 FROM ExecutionReconciliationEvent)
     OR EXISTS (
       SELECT 1
       FROM ExecutionAttempt
       WHERE status = 'UNKNOWN'
     ) THEN
    RAISE EXCEPTION
      'refusing migration 158 rollback while execution control evidence exists';
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS ExecutionReconciliationEvent_no_truncate
  ON ExecutionReconciliationEvent;
DROP TRIGGER IF EXISTS ExecutionReconciliationEvent_append_only
  ON ExecutionReconciliationEvent;
DROP FUNCTION IF EXISTS execution_reconciliation_append_only_guard();

DROP INDEX IF EXISTS ExecutionReconciliationEvent_execution_created;
DROP INDEX IF EXISTS ExecutionStatusQuery_execution_status;
DROP INDEX IF EXISTS ExecutionCancelRequest_execution_status;

DROP TABLE IF EXISTS ExecutionReconciliationEvent;
DROP TABLE IF EXISTS ExecutionStatusQuery;
DROP TABLE IF EXISTS ExecutionCancelRequest;

ALTER TABLE ExecutionAttempt
  DROP CONSTRAINT IF EXISTS executionattempt_id_org_execution_unique,
  DROP CONSTRAINT executionattempt_status_check,
  ADD CONSTRAINT executionattempt_status_check CHECK (
    status IN (
      'PENDING', 'CLAIMED', 'RUNNING', 'SUCCEEDED', 'FAILED',
      'CANCELED', 'TIMED_OUT', 'FENCED'
    )
  );
