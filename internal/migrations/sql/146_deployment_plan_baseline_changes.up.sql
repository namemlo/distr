ALTER TABLE DeploymentPlan
  ADD COLUMN bootstrap BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN previous_state_source_plan_id UUID,
  ADD CONSTRAINT deploymentplan_previous_state_source_fk
    FOREIGN KEY (previous_state_source_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  ADD CONSTRAINT deploymentplan_previous_state_shape_check CHECK (
    previous_state_source_plan_id IS NULL
    OR (
      plan_schema = 'distr.target-deployment-plan/v2'
      AND supersedes_deployment_plan_id IS NOT NULL
      AND previous_state_source_plan_id <> supersedes_deployment_plan_id
    )
  );

CREATE UNIQUE INDEX DeploymentPlan_previous_state_unique
  ON DeploymentPlan (
    organization_id,
    supersedes_deployment_plan_id,
    previous_state_source_plan_id
  )
  WHERE previous_state_source_plan_id IS NOT NULL;

CREATE TABLE DeploymentPlanBaseline (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deployment_plan_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  component_instance_id UUID NOT NULL,
  component_key TEXT NOT NULL CHECK (
    component_key ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
  ),
  source_deployment_plan_id UUID,
  external_execution_id UUID,
  observation_id UUID,
  observed_at TIMESTAMPTZ,
  desired_revision BIGINT NOT NULL CHECK (desired_revision > 0),
  desired_checksum TEXT NOT NULL CHECK (
    desired_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  observation_checksum TEXT NOT NULL DEFAULT '' CHECK (
    observation_checksum = ''
    OR observation_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  release_bundle_id UUID,
  version TEXT NOT NULL DEFAULT '' CHECK (length(version) <= 128),
  image TEXT NOT NULL DEFAULT '' CHECK (length(image) <= 4096),
  platform TEXT NOT NULL DEFAULT '' CHECK (
    platform IN ('', 'linux/amd64', 'linux/arm64')
  ),
  target_config_snapshot_id UUID,
  config_checksum TEXT NOT NULL DEFAULT '' CHECK (
    config_checksum = ''
    OR config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  provider_binding_checksum TEXT NOT NULL DEFAULT '' CHECK (
    provider_binding_checksum = ''
    OR provider_binding_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  schema_state TEXT NOT NULL DEFAULT '' CHECK (length(schema_state) <= 4096),
  schema_checksum TEXT NOT NULL DEFAULT '' CHECK (
    schema_checksum = ''
    OR schema_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  topology_checksum TEXT NOT NULL DEFAULT '' CHECK (
    topology_checksum = ''
    OR topology_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  projection TEXT NOT NULL CHECK (
    projection IN ('verified_v2', 'legacy_projection', 'bootstrap')
  ),
  authorizes_v2_execution BOOLEAN NOT NULL DEFAULT false,
  bootstrap BOOLEAN NOT NULL DEFAULT false,
  actor_user_account_id UUID NOT NULL,
  canonical_checksum TEXT NOT NULL CHECK (
    canonical_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
  CONSTRAINT deploymentplanbaseline_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT deploymentplanbaseline_source_plan_fk
    FOREIGN KEY (source_deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanbaseline_instance_fk
    FOREIGN KEY (component_instance_id, organization_id)
    REFERENCES ComponentInstance(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanbaseline_execution_fk
    FOREIGN KEY (external_execution_id, organization_id)
    REFERENCES ExternalExecution(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanbaseline_observation_fk
    FOREIGN KEY (observation_id, organization_id)
    REFERENCES TargetComponentObservation(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanbaseline_release_fk
    FOREIGN KEY (release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanbaseline_config_fk
    FOREIGN KEY (target_config_snapshot_id, organization_id)
    REFERENCES TargetConfigSnapshot(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanbaseline_actor_fk
    FOREIGN KEY (organization_id, actor_user_account_id)
    REFERENCES Organization_UserAccount(organization_id, user_account_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanbaseline_plan_component_unique
    UNIQUE (deployment_plan_id, component_instance_id),
  CONSTRAINT deploymentplanbaseline_plan_order_unique
    UNIQUE (deployment_plan_id, sort_order),
  CONSTRAINT deploymentplanbaseline_shape_check CHECK (
    (
      projection = 'bootstrap'
      AND bootstrap
      AND NOT authorizes_v2_execution
      AND source_deployment_plan_id IS NULL
      AND external_execution_id IS NULL
      AND observation_id IS NULL
      AND observed_at IS NULL
      AND observation_checksum = ''
      AND release_bundle_id IS NULL
    )
    OR (
      projection = 'legacy_projection'
      AND NOT bootstrap
      AND NOT authorizes_v2_execution
      AND observation_id IS NOT NULL
      AND observed_at IS NOT NULL
      AND observation_checksum <> ''
      AND release_bundle_id IS NOT NULL
      AND version <> ''
      AND image <> ''
      AND platform <> ''
      AND config_checksum <> ''
    )
    OR (
      projection = 'verified_v2'
      AND NOT bootstrap
      AND authorizes_v2_execution
      AND source_deployment_plan_id IS NOT NULL
      AND external_execution_id IS NOT NULL
      AND observation_id IS NOT NULL
      AND observed_at IS NOT NULL
      AND observation_checksum <> ''
      AND release_bundle_id IS NOT NULL
      AND version <> ''
      AND image <> ''
      AND platform <> ''
      AND target_config_snapshot_id IS NOT NULL
      AND config_checksum <> ''
    )
  )
);

CREATE INDEX DeploymentPlanBaseline_plan_order
  ON DeploymentPlanBaseline (
    organization_id,
    deployment_plan_id,
    sort_order,
    component_key
  );

CREATE TABLE DeploymentPlanChangeEntry (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deployment_plan_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  component_instance_id UUID,
  component_key TEXT NOT NULL CHECK (length(component_key) <= 128),
  kind TEXT NOT NULL CHECK (
    kind IN (
      'bootstrap',
      'baseline_authority',
      'image',
      'config',
      'provider',
      'schema',
      'topology',
      'source_notes',
      'previous_state',
      'planning_limit_exceeded'
    )
  ),
  before_value TEXT NOT NULL CHECK (length(before_value) <= 4096),
  after_value TEXT NOT NULL CHECK (length(after_value) <= 4096),
  release_notes JSONB NOT NULL DEFAULT '[]'::JSONB CHECK (
    jsonb_typeof(release_notes) = 'array'
    AND octet_length(release_notes::TEXT) <= 1048576
  ),
  forward_only BOOLEAN NOT NULL DEFAULT false,
  actor_user_account_id UUID NOT NULL,
  canonical_checksum TEXT NOT NULL CHECK (
    canonical_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
  CONSTRAINT deploymentplanchangeentry_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT deploymentplanchangeentry_instance_fk
    FOREIGN KEY (component_instance_id, organization_id)
    REFERENCES ComponentInstance(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanchangeentry_actor_fk
    FOREIGN KEY (organization_id, actor_user_account_id)
    REFERENCES Organization_UserAccount(organization_id, user_account_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanchangeentry_plan_order_unique
    UNIQUE (deployment_plan_id, sort_order)
);

CREATE INDEX DeploymentPlanChangeEntry_plan_order
  ON DeploymentPlanChangeEntry (
    organization_id,
    deployment_plan_id,
    sort_order,
    kind
  );

CREATE TABLE DeploymentPlanRiskEntry (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deployment_plan_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  component_key TEXT NOT NULL CHECK (length(component_key) <= 128),
  code TEXT NOT NULL CHECK (
    code ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
  ),
  level TEXT NOT NULL CHECK (
    level IN ('low', 'medium', 'high', 'critical')
  ),
  blocking BOOLEAN NOT NULL DEFAULT false,
  message TEXT NOT NULL CHECK (
    length(btrim(message)) BETWEEN 1 AND 2048
    AND message !~ E'[\\r\\n]'
  ),
  actor_user_account_id UUID NOT NULL,
  canonical_checksum TEXT NOT NULL CHECK (
    canonical_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
  CONSTRAINT deploymentplanriskentry_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT deploymentplanriskentry_actor_fk
    FOREIGN KEY (organization_id, actor_user_account_id)
    REFERENCES Organization_UserAccount(organization_id, user_account_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanriskentry_plan_order_unique
    UNIQUE (deployment_plan_id, sort_order)
);

CREATE INDEX DeploymentPlanRiskEntry_plan_order
  ON DeploymentPlanRiskEntry (
    organization_id,
    deployment_plan_id,
    sort_order,
    level
  );

CREATE FUNCTION deployment_plan_change_evidence_append_only_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'deployment plan change evidence is append-only'
      USING ERRCODE = '23514';
  END IF;
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.deployment_registry_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'deployment plan change evidence is append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER DeploymentPlanBaseline_append_only
BEFORE UPDATE OR DELETE ON DeploymentPlanBaseline
FOR EACH ROW EXECUTE FUNCTION deployment_plan_change_evidence_append_only_guard();
CREATE TRIGGER DeploymentPlanBaseline_no_truncate
BEFORE TRUNCATE ON DeploymentPlanBaseline
FOR EACH STATEMENT EXECUTE FUNCTION deployment_plan_change_evidence_append_only_guard();

CREATE TRIGGER DeploymentPlanChangeEntry_append_only
BEFORE UPDATE OR DELETE ON DeploymentPlanChangeEntry
FOR EACH ROW EXECUTE FUNCTION deployment_plan_change_evidence_append_only_guard();
CREATE TRIGGER DeploymentPlanChangeEntry_no_truncate
BEFORE TRUNCATE ON DeploymentPlanChangeEntry
FOR EACH STATEMENT EXECUTE FUNCTION deployment_plan_change_evidence_append_only_guard();

CREATE TRIGGER DeploymentPlanRiskEntry_append_only
BEFORE UPDATE OR DELETE ON DeploymentPlanRiskEntry
FOR EACH ROW EXECUTE FUNCTION deployment_plan_change_evidence_append_only_guard();
CREATE TRIGGER DeploymentPlanRiskEntry_no_truncate
BEFORE TRUNCATE ON DeploymentPlanRiskEntry
FOR EACH STATEMENT EXECUTE FUNCTION deployment_plan_change_evidence_append_only_guard();

CREATE FUNCTION deployment_plan_bootstrap_immutable_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF OLD.plan_schema = 'distr.target-deployment-plan/v2'
     AND (
       NEW.bootstrap IS DISTINCT FROM OLD.bootstrap
       OR NEW.previous_state_source_plan_id IS DISTINCT
          FROM OLD.previous_state_source_plan_id
     ) THEN
    RAISE EXCEPTION 'published target deployment plan change lineage is immutable'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER DeploymentPlan_bootstrap_immutable_guard
BEFORE UPDATE OF bootstrap, previous_state_source_plan_id ON DeploymentPlan
FOR EACH ROW EXECUTE FUNCTION deployment_plan_bootstrap_immutable_guard();
