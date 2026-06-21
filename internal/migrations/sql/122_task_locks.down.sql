DROP INDEX IF EXISTS TaskResourceLock_active_resource;
DROP INDEX IF EXISTS TaskResourceLock_resource;
DROP INDEX IF EXISTS TaskResourceLock_task;

DROP TABLE IF EXISTS TaskResourceLock;

ALTER TABLE Task
  DROP CONSTRAINT IF EXISTS task_status_check;

UPDATE Task
SET
  status = 'FAILED',
  updated_at = now(),
  completed_at = COALESCE(completed_at, now())
WHERE status = 'CANCELED';

ALTER TABLE Task
  ADD CONSTRAINT task_status_check
  CHECK (status IN ('QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED'));
