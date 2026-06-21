ALTER TABLE Task
  ADD CONSTRAINT task_id_target_organization_unique
  UNIQUE (id, deployment_target_id, organization_id);

CREATE TABLE TaskLease (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  task_id UUID NOT NULL,
  agent_id UUID NOT NULL,
  lease_token_hash TEXT NOT NULL CHECK (length(trim(lease_token_hash)) > 0),
  leased_at TIMESTAMP NOT NULL DEFAULT now(),
  expires_at TIMESTAMP NOT NULL,
  heartbeat_at TIMESTAMP NOT NULL DEFAULT now(),
  attempt INTEGER NOT NULL CHECK (attempt > 0),
  released_at TIMESTAMP,
  CONSTRAINT tasklease_task_fk
    FOREIGN KEY (task_id, organization_id)
    REFERENCES Task(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT tasklease_agent_fk
    FOREIGN KEY (agent_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT tasklease_task_agent_fk
    FOREIGN KEY (task_id, agent_id, organization_id)
    REFERENCES Task(id, deployment_target_id, organization_id)
    ON DELETE CASCADE
);

CREATE UNIQUE INDEX TaskLease_active_task
  ON TaskLease (task_id)
  WHERE released_at IS NULL;

CREATE INDEX TaskLease_agent_active
  ON TaskLease (organization_id, agent_id, expires_at, task_id)
  WHERE released_at IS NULL;

CREATE INDEX TaskLease_task_attempt
  ON TaskLease (task_id, attempt DESC);
