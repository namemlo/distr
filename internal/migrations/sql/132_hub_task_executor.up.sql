ALTER TABLE TaskLease
  ADD COLUMN executor_type TEXT NOT NULL DEFAULT 'AGENT'
  CHECK (executor_type IN ('AGENT', 'HUB'));

CREATE INDEX TaskLease_executor_active
  ON TaskLease (executor_type, organization_id, expires_at, task_id)
  WHERE released_at IS NULL;
