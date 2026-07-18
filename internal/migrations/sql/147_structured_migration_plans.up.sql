ALTER TABLE DeploymentPlanStep
  ADD COLUMN step_input_checksum TEXT NOT NULL DEFAULT '',
  ADD COLUMN retry_class TEXT NOT NULL DEFAULT '',
  ADD COLUMN cancellation_behavior TEXT NOT NULL DEFAULT '',
  ADD COLUMN observation_requirement TEXT NOT NULL DEFAULT '',
  ADD COLUMN target_lock_key TEXT NOT NULL DEFAULT '',
  ADD COLUMN database_lock_key TEXT NOT NULL DEFAULT '',
  ADD CONSTRAINT deploymentplanstep_input_checksum_check CHECK (
    step_input_checksum = ''
    OR step_input_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  ADD CONSTRAINT deploymentplanstep_retry_class_check CHECK (
    retry_class IN ('', 'none', 'bounded', 'safe')
  ),
  ADD CONSTRAINT deploymentplanstep_cancellation_behavior_check CHECK (
    cancellation_behavior IN (
      '',
      'safe',
      'cooperative',
      'forward_fix_only',
      'manual_terminal'
    )
  ),
  ADD CONSTRAINT deploymentplanstep_observation_requirement_check CHECK (
    length(observation_requirement) <= 2048
    AND observation_requirement !~ E'[\\r\\n]'
  ),
  ADD CONSTRAINT deploymentplanstep_target_lock_key_check CHECK (
    length(target_lock_key) <= 512
    AND target_lock_key !~ E'[\\r\\n]'
  ),
  ADD CONSTRAINT deploymentplanstep_database_lock_key_check CHECK (
    length(database_lock_key) <= 512
    AND database_lock_key !~ E'[\\r\\n]'
    AND (database_lock_key = '' OR target_lock_key <> '')
  );

CREATE TABLE DeploymentPlanMigration (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deployment_plan_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  migration_id TEXT NOT NULL CHECK (
    migration_id ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
  ),
  contract_checksum TEXT NOT NULL CHECK (
    contract_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  component_key TEXT NOT NULL CHECK (
    component_key ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
  ),
  database_resource_key TEXT NOT NULL CHECK (
    length(btrim(database_resource_key)) BETWEEN 1 AND 256
    AND database_resource_key !~ E'[\\r\\n]'
  ),
  expected_source_version TEXT NOT NULL CHECK (
    length(btrim(expected_source_version)) BETWEEN 1 AND 128
    AND expected_source_version !~ E'[\\r\\n]'
  ),
  expected_source_checksum TEXT NOT NULL CHECK (
    expected_source_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  resulting_version TEXT NOT NULL CHECK (
    length(btrim(resulting_version)) BETWEEN 1 AND 128
    AND resulting_version !~ E'[\\r\\n]'
  ),
  resulting_schema_checksum TEXT NOT NULL CHECK (
    resulting_schema_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  phase TEXT NOT NULL CHECK (
    phase IN ('expand', 'data', 'switch', 'contract')
  ),
  depends_on TEXT[] NOT NULL DEFAULT '{}' CHECK (
    cardinality(depends_on) <= 64
  ),
  lock_type TEXT NOT NULL CHECK (
    lock_type IN ('shared', 'exclusive')
  ),
  lock_timeout_seconds INTEGER NOT NULL CHECK (
    lock_timeout_seconds BETWEEN 1 AND 86400
  ),
  operational_impact TEXT NOT NULL CHECK (
    length(btrim(operational_impact)) BETWEEN 1 AND 256
    AND operational_impact !~ E'[\\r\\n]'
  ),
  backup_required BOOLEAN NOT NULL,
  backup_verifier TEXT NOT NULL DEFAULT '' CHECK (
    length(backup_verifier) <= 256
    AND backup_verifier !~ E'[\\r\\n]'
    AND (NOT backup_required OR length(btrim(backup_verifier)) > 0)
  ),
  retry_class TEXT NOT NULL CHECK (
    retry_class IN ('none', 'bounded', 'safe')
  ),
  idempotency_key TEXT NOT NULL DEFAULT '' CHECK (
    length(idempotency_key) <= 128
    AND idempotency_key !~ E'[\\r\\n]'
    AND (
      retry_class = 'none'
      OR idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
    )
  ),
  reversibility TEXT NOT NULL CHECK (
    reversibility IN ('reversible', 'manual', 'forward_only')
  ),
  previous_application_compatibility TEXT NOT NULL CHECK (
    length(btrim(previous_application_compatibility)) BETWEEN 1 AND 256
    AND previous_application_compatibility !~ E'[\\r\\n]'
  ),
  recovery_procedure_reference TEXT NOT NULL CHECK (
    length(btrim(recovery_procedure_reference)) BETWEEN 1 AND 256
    AND recovery_procedure_reference !~ E'[\\r\\n]'
  ),
  requires_forward_fix BOOLEAN NOT NULL,
  adapter_type TEXT NOT NULL CHECK (
    adapter_type ~ '^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$'
  ),
  artifact_digest TEXT NOT NULL CHECK (
    artifact_digest ~ '^[^[:space:]]+@sha256:[A-Fa-f0-9]{64}$'
  ),
  precondition_probes JSONB NOT NULL CHECK (
    jsonb_typeof(precondition_probes) = 'array'
    AND jsonb_array_length(precondition_probes) BETWEEN 1 AND 32
    AND octet_length(precondition_probes::text) <= 65536
  ),
  postcondition_probes JSONB NOT NULL CHECK (
    jsonb_typeof(postcondition_probes) = 'array'
    AND jsonb_array_length(postcondition_probes) BETWEEN 1 AND 32
    AND octet_length(postcondition_probes::text) <= 65536
  ),
  evidence_retention_days INTEGER NOT NULL CHECK (
    evidence_retention_days BETWEEN 1 AND 3650
  ),
  apply_step_key TEXT NOT NULL CHECK (
    length(btrim(apply_step_key)) BETWEEN 1 AND 512
  ),
  validate_step_key TEXT NOT NULL CHECK (
    length(btrim(validate_step_key)) BETWEEN 1 AND 512
  ),
  sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
  CONSTRAINT deploymentplanmigration_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT deploymentplanmigration_apply_step_fk
    FOREIGN KEY (deployment_plan_id, apply_step_key, organization_id)
    REFERENCES DeploymentPlanStep(
      deployment_plan_id,
      step_key,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanmigration_validate_step_fk
    FOREIGN KEY (deployment_plan_id, validate_step_key, organization_id)
    REFERENCES DeploymentPlanStep(
      deployment_plan_id,
      step_key,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanmigration_plan_id_unique
    UNIQUE (deployment_plan_id, migration_id),
  CONSTRAINT deploymentplanmigration_plan_order_unique
    UNIQUE (deployment_plan_id, sort_order),
  CONSTRAINT deploymentplanmigration_forward_fix_check CHECK (
    reversibility <> 'forward_only' OR requires_forward_fix
  )
);

CREATE INDEX DeploymentPlanMigration_plan_order
  ON DeploymentPlanMigration (
    organization_id,
    deployment_plan_id,
    sort_order,
    migration_id
  );

CREATE INDEX DeploymentPlanMigration_database_resource
  ON DeploymentPlanMigration (
    organization_id,
    database_resource_key,
    deployment_plan_id
  );

CREATE TRIGGER DeploymentPlanMigration_append_only
BEFORE UPDATE OR DELETE ON DeploymentPlanMigration
FOR EACH ROW EXECUTE FUNCTION deployment_plan_v2_append_only_guard();

CREATE TRIGGER DeploymentPlanMigration_no_truncate
BEFORE TRUNCATE ON DeploymentPlanMigration
FOR EACH STATEMENT EXECUTE FUNCTION deployment_plan_v2_append_only_guard();
