DROP INDEX IF EXISTS StepRunOutput_step_name;
DROP INDEX IF EXISTS StepRunLogChunk_task_order;
DROP INDEX IF EXISTS StepRunEvent_step_sequence;
DROP INDEX IF EXISTS StepRunEvent_task_sequence;

DROP TABLE IF EXISTS StepRunOutput;
DROP TABLE IF EXISTS StepRunLogChunk;
DROP TABLE IF EXISTS StepRunEvent;

ALTER TABLE TaskLease
  DROP CONSTRAINT IF EXISTS tasklease_id_task_agent_organization_unique;

ALTER TABLE StepRun
  DROP CONSTRAINT IF EXISTS steprun_id_task_organization_unique;
