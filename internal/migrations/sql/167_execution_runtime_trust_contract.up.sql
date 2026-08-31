ALTER TABLE ExecutionAttempt
  ADD COLUMN runtime_contract_version TEXT NOT NULL DEFAULT 'legacy-v2',
  ADD COLUMN expected_observed_state_revision BIGINT,
  ADD COLUMN expected_observed_state_checksum TEXT,
  ADD COLUMN expected_current_image_digest TEXT,
  ADD COLUMN expected_current_config_checksum TEXT,
  ADD COLUMN expected_platform TEXT,
  ADD COLUMN intent_caller TEXT,
  ADD COLUMN intent_audience TEXT;

ALTER TABLE ExecutionAttempt
  ADD CONSTRAINT executionattempt_runtime_contract_version_check CHECK (
    runtime_contract_version IN ('legacy-v2', 'v3')
  ),
  ADD CONSTRAINT executionattempt_runtime_contract_shape_check CHECK (
    (
      runtime_contract_version = 'legacy-v2'
      AND expected_observed_state_revision IS NULL
      AND expected_observed_state_checksum IS NULL
      AND expected_current_image_digest IS NULL
      AND expected_current_config_checksum IS NULL
      AND expected_platform IS NULL
      AND intent_caller IS NULL
      AND intent_audience IS NULL
    )
    OR (
      runtime_contract_version = 'v3'
      AND expected_observed_state_revision IS NOT NULL
      AND expected_observed_state_revision > 0
      AND expected_observed_state_checksum IS NOT NULL
      AND expected_observed_state_checksum ~ '^sha256:[0-9a-f]{64}$'
      AND expected_current_image_digest IS NOT NULL
      AND expected_current_image_digest ~ '^sha256:[0-9a-f]{64}$'
      AND expected_current_config_checksum IS NOT NULL
      AND expected_current_config_checksum ~ '^sha256:[0-9a-f]{64}$'
      AND expected_platform IS NOT NULL
      AND expected_platform IN ('linux/amd64', 'linux/arm64')
      AND intent_caller IS NOT NULL
      AND intent_caller = btrim(intent_caller)
      AND length(intent_caller) BETWEEN 1 AND 512
      AND intent_caller !~ E'[\r\n]'
      AND intent_audience IS NOT NULL
      AND intent_audience = btrim(intent_audience)
      AND length(intent_audience) BETWEEN 1 AND 512
      AND intent_audience !~ E'[\r\n]'
    )
  );

-- Existing attempts retain an explicit legacy shape. New inserts fail closed
-- unless the complete v3 runtime trust contract is supplied.
ALTER TABLE ExecutionAttempt
  ALTER COLUMN runtime_contract_version SET DEFAULT 'v3';

ALTER TABLE ExecutionAttempt
  ADD CONSTRAINT executionattempt_runtime_evidence_lineage_unique
  UNIQUE (
    id,
    organization_id,
    deployment_target_id,
    execution_id,
    attempt_number,
    step_key
  );

CREATE FUNCTION execution_attempt_runtime_contract_immutable_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.runtime_contract_version IS DISTINCT FROM OLD.runtime_contract_version
     OR NEW.expected_observed_state_revision IS DISTINCT FROM OLD.expected_observed_state_revision
     OR NEW.expected_observed_state_checksum IS DISTINCT FROM OLD.expected_observed_state_checksum
     OR NEW.expected_current_image_digest IS DISTINCT FROM OLD.expected_current_image_digest
     OR NEW.expected_current_config_checksum IS DISTINCT FROM OLD.expected_current_config_checksum
     OR NEW.expected_platform IS DISTINCT FROM OLD.expected_platform
     OR NEW.intent_caller IS DISTINCT FROM OLD.intent_caller
     OR NEW.intent_audience IS DISTINCT FROM OLD.intent_audience THEN
    RAISE EXCEPTION 'execution attempt runtime trust contract is immutable'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER ExecutionAttempt_runtime_contract_immutable
BEFORE UPDATE OF
  runtime_contract_version,
  expected_observed_state_revision,
  expected_observed_state_checksum,
  expected_current_image_digest,
  expected_current_config_checksum,
  expected_platform,
  intent_caller,
  intent_audience
ON ExecutionAttempt
FOR EACH ROW EXECUTE FUNCTION execution_attempt_runtime_contract_immutable_guard();

ALTER TABLE ExecutionIntent
  ADD CONSTRAINT executionintent_attempt_org_checksum_unique
  UNIQUE (execution_attempt_id, organization_id, checksum);

CREATE TABLE ExecutionRuntimeEvidence (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  deployment_target_id UUID NOT NULL,
  execution_attempt_id UUID NOT NULL,
  execution_id UUID NOT NULL,
  attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
  step_key TEXT NOT NULL CHECK (
    step_key = btrim(step_key)
    AND length(step_key) BETWEEN 1 AND 128
    AND step_key !~ E'[\r\n]'
  ),
  event_identity UUID NOT NULL,
  schema_version TEXT NOT NULL CHECK (
    schema_version = 'distr.execution-runtime-evidence/v1'
  ),
  intent_checksum TEXT NOT NULL CHECK (
    intent_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  executor_id TEXT NOT NULL CHECK (
    executor_id = btrim(executor_id)
    AND length(executor_id) BETWEEN 1 AND 128
    AND executor_id !~ E'[\r\n]'
  ),
  caller_identity TEXT NOT NULL CHECK (
    caller_identity = btrim(caller_identity)
    AND length(caller_identity) BETWEEN 1 AND 512
    AND caller_identity !~ E'[\r\n]'
  ),
  audience TEXT NOT NULL CHECK (
    audience = btrim(audience)
    AND length(audience) BETWEEN 1 AND 512
    AND audience !~ E'[\r\n]'
  ),
  fence_generation BIGINT NOT NULL CHECK (fence_generation > 0),
  expected_observed_state_revision BIGINT NOT NULL CHECK (
    expected_observed_state_revision > 0
  ),
  expected_observed_state_checksum TEXT NOT NULL CHECK (
    expected_observed_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  pre_execution_image_digest TEXT NOT NULL CHECK (
    pre_execution_image_digest ~ '^sha256:[0-9a-f]{64}$'
  ),
  pre_execution_config_checksum TEXT NOT NULL CHECK (
    pre_execution_config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  result_image_digest TEXT NOT NULL CHECK (
    result_image_digest ~ '^sha256:[0-9a-f]{64}$'
  ),
  result_config_checksum TEXT NOT NULL CHECK (
    result_config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  platform TEXT NOT NULL CHECK (
    platform IN ('linux/amd64', 'linux/arm64')
  ),
  health_status TEXT NOT NULL CHECK (
    health_status IN ('HEALTHY', 'UNHEALTHY')
  ),
  result_checksum TEXT NOT NULL CHECK (
    result_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  evidence_reference TEXT NOT NULL CHECK (
    evidence_reference = btrim(evidence_reference)
    AND length(evidence_reference) BETWEEN 1 AND 2048
    AND evidence_reference !~ E'[\r\n]'
  ),
  evidence_checksum TEXT NOT NULL CHECK (
    evidence_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  canonical_checksum TEXT NOT NULL CHECK (
    canonical_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  captured_at TIMESTAMPTZ NOT NULL,
  CONSTRAINT executionruntimeevidence_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT executionruntimeevidence_attempt_unique
    UNIQUE (execution_attempt_id),
  CONSTRAINT executionruntimeevidence_event_identity_unique
    UNIQUE (organization_id, event_identity),
  CONSTRAINT executionruntimeevidence_attempt_fk
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
  CONSTRAINT executionruntimeevidence_intent_fk
    FOREIGN KEY (
      execution_attempt_id,
      organization_id,
      intent_checksum
    )
    REFERENCES ExecutionIntent(
      execution_attempt_id,
      organization_id,
      checksum
    )
    ON UPDATE NO ACTION
    ON DELETE CASCADE
);

CREATE INDEX ExecutionRuntimeEvidence_organization_captured
  ON ExecutionRuntimeEvidence (organization_id, captured_at DESC, id);

CREATE TRIGGER ExecutionRuntimeEvidence_append_only
BEFORE UPDATE OR DELETE ON ExecutionRuntimeEvidence
FOR EACH ROW EXECUTE FUNCTION execution_protocol_v2_append_only_guard();

CREATE TRIGGER ExecutionRuntimeEvidence_no_truncate
BEFORE TRUNCATE ON ExecutionRuntimeEvidence
FOR EACH STATEMENT EXECUTE FUNCTION execution_protocol_v2_append_only_guard();
