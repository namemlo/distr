ALTER TABLE Task
  DROP CONSTRAINT IF EXISTS task_status_check;

ALTER TABLE Task
  ADD CONSTRAINT task_status_check
  CHECK (status IN ('QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELED'));

CREATE TABLE TaskResourceLock (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  acquired_at TIMESTAMP,
  released_at TIMESTAMP,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  task_id UUID NOT NULL,
  resource_type TEXT NOT NULL CHECK (resource_type IN ('deployment_target', 'tenant_environment', 'application_environment', 'custom')),
  resource_key TEXT NOT NULL CHECK (length(trim(resource_key)) > 0),
  concurrency_policy TEXT NOT NULL CHECK (concurrency_policy IN ('QUEUE', 'CANCEL_OLDER', 'REJECT_NEW', 'ALLOW_PARALLEL')),
  CONSTRAINT taskresourcelock_task_fk
    FOREIGN KEY (task_id, organization_id)
    REFERENCES Task(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT taskresourcelock_task_resource_unique
    UNIQUE (task_id, resource_type, resource_key)
);

CREATE INDEX TaskResourceLock_task
  ON TaskResourceLock (task_id, resource_type, resource_key);

CREATE INDEX TaskResourceLock_resource
  ON TaskResourceLock (organization_id, resource_type, resource_key, task_id);

CREATE INDEX TaskResourceLock_active_resource
  ON TaskResourceLock (organization_id, resource_type, resource_key, acquired_at)
  WHERE released_at IS NULL;

INSERT INTO TaskResourceLock (
  organization_id,
  task_id,
  resource_type,
  resource_key,
  concurrency_policy
)
SELECT
  organization_id,
  id,
  'deployment_target',
  deployment_target_id::TEXT,
  'QUEUE'
FROM Task
ON CONFLICT (task_id, resource_type, resource_key) DO NOTHING;
