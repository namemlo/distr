LOCK TABLE
  ExecutionAttempt,
  ExecutionIntent,
  ExecutionRuntimeEvidence
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (
       SELECT 1
       FROM ExecutionAttempt
       WHERE runtime_contract_version = 'v3'
     )
     OR EXISTS (SELECT 1 FROM ExecutionRuntimeEvidence) THEN
    RAISE EXCEPTION
      'refusing migration 167 rollback while v3 runtime trust contracts or evidence exist';
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS ExecutionRuntimeEvidence_no_truncate
  ON ExecutionRuntimeEvidence;
DROP TRIGGER IF EXISTS ExecutionRuntimeEvidence_append_only
  ON ExecutionRuntimeEvidence;
DROP INDEX IF EXISTS ExecutionRuntimeEvidence_organization_captured;
DROP TABLE IF EXISTS ExecutionRuntimeEvidence;

ALTER TABLE ExecutionIntent
  DROP CONSTRAINT IF EXISTS executionintent_attempt_org_checksum_unique;

ALTER TABLE ExecutionAttempt
  DROP CONSTRAINT IF EXISTS executionattempt_runtime_evidence_lineage_unique;

DROP TRIGGER IF EXISTS ExecutionAttempt_runtime_contract_immutable
  ON ExecutionAttempt;
DROP FUNCTION IF EXISTS execution_attempt_runtime_contract_immutable_guard();

ALTER TABLE ExecutionAttempt
  DROP CONSTRAINT IF EXISTS executionattempt_runtime_contract_shape_check,
  DROP CONSTRAINT IF EXISTS executionattempt_runtime_contract_version_check,
  DROP COLUMN IF EXISTS intent_audience,
  DROP COLUMN IF EXISTS intent_caller,
  DROP COLUMN IF EXISTS expected_platform,
  DROP COLUMN IF EXISTS expected_current_config_checksum,
  DROP COLUMN IF EXISTS expected_current_image_digest,
  DROP COLUMN IF EXISTS expected_observed_state_checksum,
  DROP COLUMN IF EXISTS expected_observed_state_revision,
  DROP COLUMN IF EXISTS runtime_contract_version;
