DROP TABLE IF EXISTS TaskLease;

ALTER TABLE Task
  DROP CONSTRAINT IF EXISTS task_id_target_organization_unique;
