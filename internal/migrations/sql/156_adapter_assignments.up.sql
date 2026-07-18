CREATE TABLE AdapterImplementation (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  adapter_key TEXT NOT NULL CHECK (
    adapter_key ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
  ),
  name TEXT NOT NULL CHECK (
    length(btrim(name)) BETWEEN 1 AND 256
    AND name !~ E'[\\r\\n]'
  ),
  version TEXT NOT NULL CHECK (
    version ~ '^(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?([+][0-9A-Za-z.-]+)?$'
  ),
  enabled BOOLEAN NOT NULL DEFAULT true,
  CONSTRAINT adapterimplementation_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT adapterimplementation_key_version_unique
    UNIQUE (organization_id, adapter_key, version)
);

CREATE INDEX AdapterImplementation_organization_key
  ON AdapterImplementation (organization_id, adapter_key, version, id);

CREATE TABLE AdapterCapability (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  adapter_implementation_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  capability TEXT NOT NULL CHECK (
    capability ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
  ),
  version TEXT NOT NULL CHECK (
    version ~ '^(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?([+][0-9A-Za-z.-]+)?$'
  ),
  CONSTRAINT adaptercapability_implementation_fk
    FOREIGN KEY (adapter_implementation_id, organization_id)
    REFERENCES AdapterImplementation(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT adaptercapability_implementation_capability_unique
    UNIQUE (adapter_implementation_id, capability, version)
);

CREATE INDEX AdapterCapability_resolution
  ON AdapterCapability (
    organization_id,
    capability,
    version,
    adapter_implementation_id
  );

CREATE TABLE AdapterAssignment (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  adapter_implementation_id UUID NOT NULL,
  scope_type TEXT NOT NULL CHECK (
    scope_type IN (
      'deployment_target',
      'deployment_unit',
      'component_instance',
      'database_resource',
      'observer_registration'
    )
  ),
  scope_id UUID NOT NULL,
  config_snapshot_id UUID NOT NULL,
  config_checksum TEXT NOT NULL CHECK (
    config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  key_id TEXT NOT NULL CHECK (
    length(btrim(key_id)) BETWEEN 1 AND 256
    AND key_id !~ E'[\\r\\n]'
  ),
  public_key_fingerprint TEXT NOT NULL CHECK (
    public_key_fingerprint ~ '^sha256:[0-9a-f]{64}$'
  ),
  signing_key_reference TEXT NOT NULL CHECK (
    signing_key_reference ~ '^secret-provider://[^[:space:]]{1,1000}$'
    AND signing_key_reference !~ 'PRIVATE KEY'
  ),
  signing_key_version_fingerprint TEXT NOT NULL CHECK (
    signing_key_version_fingerprint ~ '^sha256:[0-9a-f]{64}$'
  ),
  enabled BOOLEAN NOT NULL DEFAULT true,
  CONSTRAINT adapterassignment_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT adapterassignment_implementation_fk
    FOREIGN KEY (adapter_implementation_id, organization_id)
    REFERENCES AdapterImplementation(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT adapterassignment_exact_unique
    UNIQUE (
      organization_id,
      scope_type,
      scope_id,
      adapter_implementation_id,
      config_snapshot_id
    )
);

CREATE INDEX AdapterAssignment_resolution
  ON AdapterAssignment (
    organization_id,
    scope_type,
    scope_id,
    config_snapshot_id,
    enabled,
    adapter_implementation_id,
    id
  );

ALTER TABLE DeploymentPlanStep
  ADD CONSTRAINT deploymentplanstep_id_plan_organization_unique
  UNIQUE (id, deployment_plan_id, organization_id);

CREATE TABLE DeploymentPlanStepAdapter (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  deployment_plan_id UUID NOT NULL,
  deployment_plan_step_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  step_key TEXT NOT NULL CHECK (
    length(btrim(step_key)) BETWEEN 1 AND 512
  ),
  adapter_assignment_id UUID NOT NULL,
  adapter_implementation_id UUID NOT NULL,
  implementation_version TEXT NOT NULL CHECK (
    implementation_version ~ '^(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?([+][0-9A-Za-z.-]+)?$'
  ),
  capability TEXT NOT NULL CHECK (
    capability ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
  ),
  capability_version TEXT NOT NULL CHECK (
    capability_version ~ '^(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?([+][0-9A-Za-z.-]+)?$'
  ),
  scope_type TEXT NOT NULL CHECK (
    scope_type IN (
      'deployment_target',
      'deployment_unit',
      'component_instance',
      'database_resource',
      'observer_registration'
    )
  ),
  scope_id UUID NOT NULL,
  config_snapshot_id UUID NOT NULL,
  config_checksum TEXT NOT NULL CHECK (
    config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  key_id TEXT NOT NULL CHECK (
    length(btrim(key_id)) BETWEEN 1 AND 256
  ),
  public_key_fingerprint TEXT NOT NULL CHECK (
    public_key_fingerprint ~ '^sha256:[0-9a-f]{64}$'
  ),
  signing_key_reference TEXT NOT NULL CHECK (
    signing_key_reference ~ '^secret-provider://[^[:space:]]{1,1000}$'
    AND signing_key_reference !~ 'PRIVATE KEY'
  ),
  signing_key_version_fingerprint TEXT NOT NULL CHECK (
    signing_key_version_fingerprint ~ '^sha256:[0-9a-f]{64}$'
  ),
  sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
  CONSTRAINT deploymentplanstepadapter_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT deploymentplanstepadapter_step_fk
    FOREIGN KEY (
      deployment_plan_step_id,
      deployment_plan_id,
      organization_id
    )
    REFERENCES DeploymentPlanStep(
      id,
      deployment_plan_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanstepadapter_assignment_fk
    FOREIGN KEY (adapter_assignment_id, organization_id)
    REFERENCES AdapterAssignment(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanstepadapter_implementation_fk
    FOREIGN KEY (adapter_implementation_id, organization_id)
    REFERENCES AdapterImplementation(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanstepadapter_step_unique
    UNIQUE (deployment_plan_id, step_key),
  CONSTRAINT deploymentplanstepadapter_step_id_unique
    UNIQUE (deployment_plan_step_id)
);

CREATE INDEX DeploymentPlanStepAdapter_plan_order
  ON DeploymentPlanStepAdapter (
    organization_id,
    deployment_plan_id,
    sort_order,
    step_key
  );

CREATE FUNCTION deployment_plan_step_adapter_append_only_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'published deployment plan adapter facts are append-only'
      USING ERRCODE = '23514';
  END IF;
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.deployment_registry_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'published deployment plan adapter facts are append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER DeploymentPlanStepAdapter_append_only
BEFORE UPDATE OR DELETE ON DeploymentPlanStepAdapter
FOR EACH ROW EXECUTE FUNCTION deployment_plan_step_adapter_append_only_guard();

CREATE TRIGGER DeploymentPlanStepAdapter_no_truncate
BEFORE TRUNCATE ON DeploymentPlanStepAdapter
FOR EACH STATEMENT EXECUTE FUNCTION deployment_plan_step_adapter_append_only_guard();
