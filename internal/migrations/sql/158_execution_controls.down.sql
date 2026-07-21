DO $$
BEGIN
  LOCK TABLE
	Task,
	DeploymentCampaignMemberRun,
    ExecutionAttempt,
    ExecutionCancelRequest,
    ExecutionStatusQuery,
    ExecutionReconciliationEvent,
    CampaignMemberTaskExecution,
    ExecutionCampaignControlHandoff
  IN ACCESS EXCLUSIVE MODE;

  IF EXISTS (SELECT 1 FROM ExecutionCancelRequest)
     OR EXISTS (SELECT 1 FROM ExecutionStatusQuery)
     OR EXISTS (SELECT 1 FROM ExecutionReconciliationEvent)
     OR EXISTS (SELECT 1 FROM CampaignMemberTaskExecution)
     OR EXISTS (SELECT 1 FROM ExecutionCampaignControlHandoff)
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

DROP TRIGGER IF EXISTS ExecutionCampaignControlHandoff_no_truncate
  ON ExecutionCampaignControlHandoff;
DROP TRIGGER IF EXISTS ExecutionCampaignControlHandoff_append_only
  ON ExecutionCampaignControlHandoff;
DROP TRIGGER IF EXISTS CampaignMemberTaskExecution_no_truncate
  ON CampaignMemberTaskExecution;
DROP TRIGGER IF EXISTS CampaignMemberTaskExecution_append_only
  ON CampaignMemberTaskExecution;

DROP TABLE IF EXISTS ExecutionCampaignControlHandoff;
DROP TABLE IF EXISTS CampaignMemberTaskExecution;

ALTER TABLE DeploymentCampaignMemberRun
  DROP CONSTRAINT IF EXISTS deploymentcampaignmemberrun_execution_lineage_unique;

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
  DROP CONSTRAINT executionattempt_completion_check,
  ADD CONSTRAINT executionattempt_completion_check CHECK (
    (
      status IN ('SUCCEEDED', 'FAILED', 'CANCELED', 'TIMED_OUT', 'FENCED')
      AND completed_at IS NOT NULL
    )
    OR (
      status IN ('PENDING', 'CLAIMED', 'RUNNING')
      AND completed_at IS NULL
    )
  ),
  DROP CONSTRAINT executionattempt_status_check,
  ADD CONSTRAINT executionattempt_status_check CHECK (
    status IN (
      'PENDING', 'CLAIMED', 'RUNNING', 'SUCCEEDED', 'FAILED',
      'CANCELED', 'TIMED_OUT', 'FENCED'
    )
  );
