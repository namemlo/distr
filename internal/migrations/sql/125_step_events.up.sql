ALTER TABLE StepRun
  ADD CONSTRAINT steprun_id_task_organization_unique
  UNIQUE (id, task_id, organization_id);

ALTER TABLE TaskLease
  ADD CONSTRAINT tasklease_id_task_agent_organization_unique
  UNIQUE (id, task_id, agent_id, organization_id);

CREATE TABLE StepRunEvent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  occurred_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  task_id UUID NOT NULL,
  step_run_id UUID NOT NULL,
  task_lease_id UUID NOT NULL,
  agent_id UUID NOT NULL,
  sequence BIGINT NOT NULL CHECK (sequence > 0),
  event_type TEXT NOT NULL CHECK (event_type IN ('STARTED', 'PROGRESS', 'LOG', 'OUTPUT', 'SUCCEEDED', 'FAILED')),
  message TEXT NOT NULL DEFAULT '' CHECK (length(message) <= 2048),
  progress_percent INTEGER CHECK (progress_percent >= 0 AND progress_percent <= 100),
  details JSONB NOT NULL DEFAULT '{}' CHECK (octet_length(details::text) <= 16384),
  redacted BOOLEAN NOT NULL DEFAULT false,
  CONSTRAINT steprunevent_task_fk
    FOREIGN KEY (task_id, organization_id)
    REFERENCES Task(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT steprunevent_step_run_fk
    FOREIGN KEY (step_run_id, task_id, organization_id)
    REFERENCES StepRun(id, task_id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT steprunevent_task_lease_fk
    FOREIGN KEY (task_lease_id, task_id, agent_id, organization_id)
    REFERENCES TaskLease(id, task_id, agent_id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT steprunevent_agent_fk
    FOREIGN KEY (agent_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT steprunevent_sequence_unique
    UNIQUE (step_run_id, task_lease_id, sequence)
);

CREATE TABLE StepRunLogChunk (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  occurred_at TIMESTAMP NOT NULL DEFAULT now(),
  event_id UUID NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  task_id UUID NOT NULL,
  step_run_id UUID NOT NULL,
  task_lease_id UUID NOT NULL,
  agent_id UUID NOT NULL,
  chunk_index INTEGER NOT NULL CHECK (chunk_index >= 0),
  stream TEXT NOT NULL CHECK (stream IN ('stdout', 'stderr', 'system')),
  severity TEXT NOT NULL CHECK (severity IN ('debug', 'info', 'warn', 'error')),
  body TEXT NOT NULL CHECK (length(trim(body)) > 0 AND octet_length(body) <= 8192),
  redacted BOOLEAN NOT NULL DEFAULT false,
  CONSTRAINT steprunlogchunk_event_index_unique UNIQUE (event_id, chunk_index),
  CONSTRAINT steprunlogchunk_event_fk
    FOREIGN KEY (event_id)
    REFERENCES StepRunEvent(id)
    ON DELETE CASCADE
);

CREATE TABLE StepRunOutput (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  event_id UUID NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  task_id UUID NOT NULL,
  step_run_id UUID NOT NULL,
  task_lease_id UUID NOT NULL,
  agent_id UUID NOT NULL,
  name TEXT NOT NULL CHECK (length(trim(name)) > 0 AND length(name) <= 128),
  value JSONB,
  sensitive BOOLEAN NOT NULL DEFAULT false,
  redacted BOOLEAN NOT NULL DEFAULT false,
  CONSTRAINT steprunoutput_step_name_unique UNIQUE (step_run_id, name),
  CONSTRAINT steprunoutput_event_fk
    FOREIGN KEY (event_id)
    REFERENCES StepRunEvent(id)
    ON DELETE CASCADE
);

CREATE INDEX StepRunEvent_task_sequence
  ON StepRunEvent (task_id, sequence, id);

CREATE INDEX StepRunEvent_step_sequence
  ON StepRunEvent (step_run_id, task_lease_id, sequence);

CREATE INDEX StepRunLogChunk_task_order
  ON StepRunLogChunk (task_id, occurred_at, event_id, chunk_index);

CREATE INDEX StepRunOutput_step_name
  ON StepRunOutput (step_run_id, name);
