ALTER TABLE Task
  ADD COLUMN protocol_version TEXT NOT NULL DEFAULT 'v1',
  ADD CONSTRAINT task_protocol_version_check
    CHECK (protocol_version IN ('v1', 'v2'));

UPDATE Task t
SET protocol_version = dp.protocol_version
FROM DeploymentPlan dp
WHERE dp.id = t.deployment_plan_id
  AND dp.organization_id = t.organization_id;

CREATE TABLE ExecutionAttempt (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_target_id UUID NOT NULL,
  task_id UUID NOT NULL,
  step_run_id UUID NOT NULL,
  execution_id UUID NOT NULL,
  attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
  step_key TEXT NOT NULL CHECK (
    length(btrim(step_key)) BETWEEN 1 AND 128
    AND step_key !~ E'[\r\n]'
  ),
  status TEXT NOT NULL DEFAULT 'PENDING' CHECK (
    status IN (
      'PENDING', 'CLAIMED', 'RUNNING', 'SUCCEEDED', 'FAILED',
      'CANCELED', 'TIMED_OUT', 'FENCED'
    )
  ),
  claimed_by TEXT NOT NULL DEFAULT '' CHECK (
    length(claimed_by) <= 128 AND claimed_by !~ E'[\r\n]'
  ),
  plan_checksum TEXT NOT NULL CHECK (
    plan_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  artifact_digest TEXT NOT NULL CHECK (
    artifact_digest ~ '^sha256:[0-9a-f]{64}$'
  ),
  config_checksum TEXT NOT NULL CHECK (
    config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  adapter_revision TEXT NOT NULL CHECK (
    length(btrim(adapter_revision)) BETWEEN 1 AND 256
    AND adapter_revision !~ E'[\r\n]'
  ),
  intent_issued_at TIMESTAMPTZ NOT NULL,
  intent_expires_at TIMESTAMPTZ NOT NULL,
  last_event_sequence BIGINT NOT NULL DEFAULT 0 CHECK (
    last_event_sequence >= 0
  ),
  acknowledged_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  cancellable BOOLEAN NOT NULL DEFAULT false,
  retry_safe BOOLEAN NOT NULL DEFAULT false,
  failure_reason TEXT NOT NULL DEFAULT '' CHECK (
    length(failure_reason) <= 2048 AND failure_reason !~ E'[\r\n]'
  ),
  CONSTRAINT executionattempt_identity_unique
    UNIQUE (execution_id, attempt_number, step_key),
  CONSTRAINT executionattempt_identity_id_org_unique
    UNIQUE (
      id,
      organization_id,
      deployment_target_id,
      execution_id,
      attempt_number,
      step_key
    ),
  CONSTRAINT executionattempt_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT executionattempt_id_org_target_unique
    UNIQUE (id, organization_id, deployment_target_id),
  CONSTRAINT executionattempt_task_fk
    FOREIGN KEY (task_id, deployment_target_id, organization_id)
    REFERENCES Task(id, deployment_target_id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT executionattempt_step_run_fk
    FOREIGN KEY (step_run_id, task_id, organization_id)
    REFERENCES StepRun(id, task_id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT executionattempt_validity_check CHECK (
    intent_expires_at > intent_issued_at
  ),
  CONSTRAINT executionattempt_completion_check CHECK (
    (
      status IN ('SUCCEEDED', 'FAILED', 'CANCELED', 'TIMED_OUT', 'FENCED')
      AND completed_at IS NOT NULL
    )
    OR (
      status IN ('PENDING', 'CLAIMED', 'RUNNING')
      AND completed_at IS NULL
    )
  )
);

CREATE TABLE ExecutionFence (
  execution_attempt_id UUID PRIMARY KEY,
  organization_id UUID NOT NULL,
  resource_key TEXT NOT NULL CHECK (
    length(btrim(resource_key)) BETWEEN 1 AND 512
    AND resource_key !~ E'[\r\n]'
  ),
  generation BIGINT NOT NULL CHECK (generation > 0),
  lease_expires_at TIMESTAMPTZ,
  released_at TIMESTAMPTZ,
  CONSTRAINT executionfence_attempt_fk
    FOREIGN KEY (execution_attempt_id, organization_id)
    REFERENCES ExecutionAttempt(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
);

CREATE UNIQUE INDEX ExecutionFence_active_resource
  ON ExecutionFence (organization_id, resource_key)
  WHERE released_at IS NULL;

CREATE TABLE ExecutionIntent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  execution_attempt_id UUID NOT NULL,
  payload BYTEA NOT NULL CHECK (
    octet_length(payload) BETWEEN 2 AND 1048576
  ),
  checksum TEXT NOT NULL CHECK (
    checksum ~ '^sha256:[0-9a-f]{64}$'
    AND checksum = 'sha256:' || encode(sha256(payload), 'hex')
  ),
  key_id TEXT NOT NULL CHECK (
    key_id ~ '^sha256:[0-9a-f]{64}$'
  ),
  signature TEXT NOT NULL CHECK (
    length(signature) BETWEEN 80 AND 128
    AND signature !~ E'[\r\n]'
  ),
  CONSTRAINT executionintent_attempt_unique
    UNIQUE (execution_attempt_id),
  CONSTRAINT executionintent_attempt_fk
    FOREIGN KEY (execution_attempt_id, organization_id)
    REFERENCES ExecutionAttempt(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
);

CREATE TABLE ExecutionEvent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_target_id UUID NOT NULL,
  execution_attempt_id UUID NOT NULL,
  execution_id UUID NOT NULL,
  attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
  step_key TEXT NOT NULL CHECK (
    length(btrim(step_key)) BETWEEN 1 AND 128
    AND step_key !~ E'[\r\n]'
  ),
  fence_generation BIGINT NOT NULL CHECK (fence_generation > 0),
  event_sequence BIGINT NOT NULL CHECK (event_sequence > 0),
  status TEXT NOT NULL CHECK (
    status IN ('RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELED')
  ),
  payload_checksum TEXT NOT NULL CHECK (
    payload_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  message TEXT NOT NULL DEFAULT '' CHECK (
    length(message) <= 2048 AND message !~ E'[\r\n]'
  ),
  occurred_at TIMESTAMPTZ NOT NULL,
  CONSTRAINT executionevent_attempt_fk
    FOREIGN KEY (
      execution_attempt_id,
      organization_id,
      deployment_target_id,
      execution_id,
      attempt_number,
      step_key
    )
    REFERENCES ExecutionAttempt(
      id,
      organization_id,
      deployment_target_id,
      execution_id,
      attempt_number,
      step_key
    )
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT executionevent_identity_unique
    UNIQUE (
      organization_id,
      deployment_target_id,
      execution_id,
      attempt_number,
      step_key,
      event_sequence
    ),
  CONSTRAINT executionevent_attempt_sequence_unique
    UNIQUE (
      organization_id,
      deployment_target_id,
      execution_attempt_id,
      event_sequence
    )
);

CREATE INDEX ExecutionAttempt_organization_status
  ON ExecutionAttempt (organization_id, status, created_at, id);

CREATE INDEX ExecutionEvent_attempt_sequence
  ON ExecutionEvent (execution_attempt_id, event_sequence, id);

CREATE FUNCTION execution_protocol_v2_append_only_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.deployment_registry_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'execution protocol v2 evidence is append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER ExecutionIntent_append_only
BEFORE UPDATE OR DELETE ON ExecutionIntent
FOR EACH ROW EXECUTE FUNCTION execution_protocol_v2_append_only_guard();

CREATE TRIGGER ExecutionIntent_no_truncate
BEFORE TRUNCATE ON ExecutionIntent
FOR EACH STATEMENT EXECUTE FUNCTION execution_protocol_v2_append_only_guard();

CREATE TRIGGER ExecutionEvent_append_only
BEFORE UPDATE OR DELETE ON ExecutionEvent
FOR EACH ROW EXECUTE FUNCTION execution_protocol_v2_append_only_guard();

CREATE TRIGGER ExecutionEvent_no_truncate
BEFORE TRUNCATE ON ExecutionEvent
FOR EACH STATEMENT EXECUTE FUNCTION execution_protocol_v2_append_only_guard();
