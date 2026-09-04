SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

LOCK TABLE ExecutionAttempt, ExecutionRuntimeEvidence, ProtectedHistoryArtifact
  IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (
       SELECT 1
       FROM ExecutionAttempt
       WHERE runtime_contract_version = 'v4'
     )
     OR EXISTS (
       SELECT 1
       FROM ExecutionRuntimeEvidence
       WHERE schema_version = 'distr.execution-runtime-evidence/v2'
          OR pre_execution_service_config_checksum IS NOT NULL
          OR result_service_config_checksum IS NOT NULL
     )
     OR EXISTS (
       SELECT 1
       FROM ProtectedHistoryArtifact
       WHERE source_schema_version > 171
     ) THEN
    RAISE EXCEPTION
      'refusing migration 172 rollback while v4 runtime checksum contracts or evidence exist or schema-172 protected history exists';
  END IF;
END;
$$;

ALTER TABLE ProtectedHistoryArtifact
  DROP CONSTRAINT protectedhistoryartifact_source_schema_version_check,
  ADD CONSTRAINT protectedhistoryartifact_source_schema_version_check CHECK (
    source_schema_version BETWEEN 138 AND 171
  );

DROP TRIGGER ExecutionAttempt_runtime_contract_immutable
  ON ExecutionAttempt;

CREATE OR REPLACE FUNCTION execution_attempt_runtime_contract_immutable_guard()
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

ALTER TABLE ExecutionRuntimeEvidence
  DROP CONSTRAINT executionruntimeevidence_service_config_shape_check,
  DROP CONSTRAINT executionruntimeevidence_schema_version_check,
  DROP COLUMN pre_execution_service_config_checksum,
  DROP COLUMN result_service_config_checksum,
  ADD CONSTRAINT executionruntimeevidence_schema_version_check CHECK (
    schema_version = 'distr.execution-runtime-evidence/v1'
  );

ALTER TABLE ExecutionAttempt
  DROP CONSTRAINT executionattempt_runtime_contract_shape_check,
  DROP CONSTRAINT executionattempt_runtime_contract_version_check,
  DROP COLUMN runtime_manifest_checksum,
  DROP COLUMN desired_service_config_checksum,
  DROP COLUMN expected_current_service_config_checksum,
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
  ),
  ALTER COLUMN runtime_contract_version SET DEFAULT 'v3';

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
