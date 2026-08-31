SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE DeploymentPlanResolvedRequirement
  ADD COLUMN provider_evidence_version SMALLINT NOT NULL DEFAULT 1 CHECK (
    provider_evidence_version IN (1, 2)
  ),
  ADD COLUMN observation_fresh_until TIMESTAMPTZ,
  ADD COLUMN observation_trusted BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN observation_current BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN provider_approval_request_id UUID,
  ADD COLUMN provider_approval_checksum TEXT NOT NULL DEFAULT '' CHECK (
    provider_approval_checksum = ''
    OR provider_approval_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  ADD COLUMN contract_probe_observation_id UUID,
  ADD COLUMN contract_probe_evidence_checksum TEXT NOT NULL DEFAULT '' CHECK (
    contract_probe_evidence_checksum = ''
    OR contract_probe_evidence_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  ADD CONSTRAINT deploymentplanresolvedrequirement_provider_approval_fk
    FOREIGN KEY (provider_approval_request_id, organization_id)
    REFERENCES ApprovalRequest(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  ADD CONSTRAINT deploymentplanresolvedrequirement_contract_probe_fk
    FOREIGN KEY (contract_probe_observation_id, organization_id)
    REFERENCES ObservedComponentState(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  ADD CONSTRAINT deploymentplanresolvedrequirement_provider_evidence_shape CHECK (
    (
      provider_evidence_version = 1
      AND observation_fresh_until IS NULL
      AND NOT observation_trusted
      AND NOT observation_current
      AND provider_approval_request_id IS NULL
      AND provider_approval_checksum = ''
      AND contract_probe_observation_id IS NULL
      AND contract_probe_evidence_checksum = ''
    )
    OR (
      provider_evidence_version = 2
      AND (
        (
          mode IN ('included', 'feature_disabled')
          AND observation_fresh_until IS NULL
          AND NOT observation_trusted
          AND NOT observation_current
          AND provider_approval_request_id IS NULL
          AND provider_approval_checksum = ''
          AND contract_probe_observation_id IS NULL
          AND contract_probe_evidence_checksum = ''
        )
        OR (
          mode IN ('pinned_existing', 'shared_provider')
          AND observation_fresh_until IS NOT NULL
          AND observation_trusted
          AND observation_current
          AND provider_approval_request_id IS NULL
          AND provider_approval_checksum = ''
          AND contract_probe_observation_id IS NULL
          AND contract_probe_evidence_checksum = ''
        )
        OR (
          mode = 'approved_external'
          AND observation_fresh_until IS NOT NULL
          AND observation_trusted
          AND observation_current
          AND observation_id IS NULL
          AND active_desired_revision_id IS NOT NULL
          AND observed_component_state_id IS NOT NULL
          AND provider_approval_request_id IS NOT NULL
          AND provider_approval_checksum <> ''
          AND contract_probe_observation_id = observed_component_state_id
          AND contract_probe_evidence_checksum <> ''
        )
      )
    )
  );
