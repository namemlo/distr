SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

LOCK TABLE DeploymentPlanResolvedRequirement IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM DeploymentPlanResolvedRequirement
    WHERE provider_evidence_version = 2
  ) THEN
    RAISE EXCEPTION
      'refusing migration 168 rollback: checksum-bound provider evidence exists'
      USING ERRCODE = '23514';
  END IF;
END;
$$;

ALTER TABLE DeploymentPlanResolvedRequirement
  DROP CONSTRAINT deploymentplanresolvedrequirement_provider_evidence_shape,
  DROP CONSTRAINT deploymentplanresolvedrequirement_contract_probe_fk,
  DROP CONSTRAINT deploymentplanresolvedrequirement_provider_approval_fk,
  DROP COLUMN contract_probe_evidence_checksum,
  DROP COLUMN contract_probe_observation_id,
  DROP COLUMN provider_approval_checksum,
  DROP COLUMN provider_approval_request_id,
  DROP COLUMN observation_current,
  DROP COLUMN observation_trusted,
  DROP COLUMN observation_fresh_until,
  DROP COLUMN provider_evidence_version;
