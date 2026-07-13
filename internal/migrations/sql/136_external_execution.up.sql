ALTER TABLE TargetComponentState
  ADD COLUMN config_reference TEXT NOT NULL DEFAULT '';

ALTER TABLE TargetComponentObservation
  ADD COLUMN config_reference TEXT NOT NULL DEFAULT '';

ALTER TABLE ReleaseBundle
  ADD CONSTRAINT releasebundle_id_organization_unique
  UNIQUE (id, organization_id);

CREATE TABLE ExternalExecution (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  started_at TIMESTAMP,
  completed_at TIMESTAMP,
  callback_deadline_at TIMESTAMP NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  step_run_id UUID NOT NULL,
  task_id UUID NOT NULL,
  deployment_plan_id UUID NOT NULL,
  deployment_plan_target_id UUID NOT NULL,
  deployment_target_id UUID NOT NULL,
  application_id UUID NOT NULL,
  release_bundle_id UUID NOT NULL,
  component TEXT NOT NULL CHECK (length(trim(component)) > 0),
  plan_checksum TEXT NOT NULL CHECK (plan_checksum ~ '^sha256:[0-9a-f]{64}$'),
  idempotency_key TEXT NOT NULL CHECK (length(trim(idempotency_key)) > 0 AND length(idempotency_key) <= 128),
  expected_state_version BIGINT NOT NULL CHECK (expected_state_version >= 0),
  expected_state_checksum TEXT NOT NULL DEFAULT '' CHECK (
    expected_state_checksum = '' OR expected_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  expected_version TEXT NOT NULL CHECK (length(trim(expected_version)) > 0),
  expected_image TEXT NOT NULL CHECK (length(trim(expected_image)) > 0),
  expected_platform TEXT NOT NULL CHECK (expected_platform IN ('linux/amd64', 'linux/arm64')),
  expected_contracts TEXT[] NOT NULL DEFAULT '{}',
  expected_config_reference TEXT NOT NULL CHECK (length(trim(expected_config_reference)) > 0),
  expected_config_checksum TEXT NOT NULL CHECK (expected_config_checksum ~ '^sha256:[0-9a-f]{64}$'),
  status TEXT NOT NULL DEFAULT 'QUEUED' CHECK (
    status IN ('QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELED', 'TIMED_OUT')
  ),
  provider_reference TEXT NOT NULL DEFAULT '' CHECK (length(provider_reference) <= 512),
  provider_url TEXT NOT NULL DEFAULT '' CHECK (length(provider_url) <= 2048),
  trigger_attempts INTEGER NOT NULL DEFAULT 0 CHECK (trigger_attempts >= 0),
  last_callback_sequence BIGINT NOT NULL DEFAULT 0 CHECK (last_callback_sequence >= 0),
  last_message TEXT NOT NULL DEFAULT '' CHECK (length(last_message) <= 2048),
  error_summary TEXT NOT NULL DEFAULT '' CHECK (length(error_summary) <= 2048),
  actual_version TEXT NOT NULL DEFAULT '',
  actual_image TEXT NOT NULL DEFAULT '',
  actual_platform TEXT CHECK (actual_platform IS NULL OR actual_platform IN ('linux/amd64', 'linux/arm64')),
  actual_contracts TEXT[] NOT NULL DEFAULT '{}',
  actual_config_reference TEXT NOT NULL DEFAULT '',
  actual_config_checksum TEXT NOT NULL DEFAULT '' CHECK (
    actual_config_checksum = '' OR actual_config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  actual_health TEXT CHECK (actual_health IS NULL OR actual_health IN ('UNKNOWN', 'HEALTHY', 'UNHEALTHY')),
  observed_state_checksum TEXT NOT NULL DEFAULT '' CHECK (
    observed_state_checksum = '' OR observed_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT externalexecution_id_organization_unique UNIQUE (id, organization_id),
  CONSTRAINT externalexecution_step_run_unique UNIQUE (step_run_id),
  CONSTRAINT externalexecution_idempotency_unique UNIQUE (organization_id, idempotency_key),
  CONSTRAINT externalexecution_step_run_fk
    FOREIGN KEY (step_run_id, task_id, organization_id)
    REFERENCES StepRun(id, task_id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT externalexecution_task_fk
    FOREIGN KEY (task_id, deployment_plan_id, organization_id)
    REFERENCES Task(id, deployment_plan_id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT externalexecution_plan_target_fk
    FOREIGN KEY (deployment_plan_id, deployment_plan_target_id, organization_id)
    REFERENCES DeploymentPlanTarget(deployment_plan_id, id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT externalexecution_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT externalexecution_application_fk
    FOREIGN KEY (application_id, organization_id)
    REFERENCES Application(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT externalexecution_release_bundle_fk
    FOREIGN KEY (release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT
);

CREATE TABLE ExternalExecutionEvent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  external_execution_id UUID NOT NULL,
  sequence BIGINT NOT NULL CHECK (sequence BETWEEN 1 AND 256),
  status TEXT NOT NULL CHECK (status IN ('RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELED', 'TIMED_OUT')),
  provider_reference TEXT NOT NULL DEFAULT '' CHECK (length(provider_reference) <= 512),
  provider_url TEXT NOT NULL DEFAULT '' CHECK (length(provider_url) <= 2048),
  message TEXT NOT NULL DEFAULT '' CHECK (length(message) <= 2048),
  observed_state JSONB,
  payload_hash TEXT NOT NULL CHECK (payload_hash ~ '^sha256:[0-9a-f]{64}$'),
  CONSTRAINT externalexecutionevent_execution_fk
    FOREIGN KEY (external_execution_id, organization_id)
    REFERENCES ExternalExecution(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT externalexecutionevent_sequence_unique UNIQUE (external_execution_id, sequence)
);

ALTER TABLE TargetComponentObservation
  ADD CONSTRAINT targetcomponentobservation_external_execution_fk
  FOREIGN KEY (external_execution_id, organization_id)
  REFERENCES ExternalExecution(id, organization_id)
  ON DELETE SET NULL (external_execution_id);

CREATE INDEX ExternalExecution_organization_status
  ON ExternalExecution (organization_id, status, updated_at DESC, id);

CREATE INDEX ExternalExecution_task
  ON ExternalExecution (task_id, created_at, id);

CREATE INDEX ExternalExecutionEvent_execution_sequence
  ON ExternalExecutionEvent (external_execution_id, sequence, id);
