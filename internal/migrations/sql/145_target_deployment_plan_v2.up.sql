ALTER TABLE TargetComponentObservation
  ADD CONSTRAINT targetcomponentobservation_id_organization_unique
  UNIQUE (id, organization_id);

ALTER TABLE DeploymentPlanStep
  ADD CONSTRAINT deploymentplanstep_plan_key_organization_unique
  UNIQUE (deployment_plan_id, step_key, organization_id);

CREATE TABLE DeploymentPlanDraft (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
  product_release_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  environment_assignment_id UUID NOT NULL,
  target_config_snapshot_id UUID NOT NULL,
  protocol_version TEXT NOT NULL CHECK (protocol_version IN ('v1', 'v2')),
  supersedes_deployment_plan_id UUID,
  supersede_reason TEXT NOT NULL DEFAULT '' CHECK (
    length(supersede_reason) <= 2048
    AND supersede_reason !~ E'[\\r\\n]'
  ),
  preview_checksum TEXT NOT NULL DEFAULT '' CHECK (
    preview_checksum = '' OR preview_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  preview_payload BYTEA,
  CONSTRAINT deploymentplandraft_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT deploymentplandraft_product_release_fk
    FOREIGN KEY (product_release_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentplandraft_unit_assignment_fk
    FOREIGN KEY (
      deployment_unit_id,
      environment_assignment_id,
      organization_id
    )
    REFERENCES DeploymentUnit(
      id,
      target_environment_assignment_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentplandraft_config_unit_fk
    FOREIGN KEY (
      target_config_snapshot_id,
      deployment_unit_id,
      organization_id
    )
    REFERENCES TargetConfigSnapshot(id, deployment_unit_id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentplandraft_supersedes_fk
    FOREIGN KEY (supersedes_deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentplandraft_supersede_reason_check CHECK (
    (
      supersedes_deployment_plan_id IS NULL
      AND supersede_reason = ''
    )
    OR (
      supersedes_deployment_plan_id IS NOT NULL
      AND length(btrim(supersede_reason)) > 0
    )
  ),
  CONSTRAINT deploymentplandraft_preview_check CHECK (
    (
      preview_checksum = ''
      AND preview_payload IS NULL
    )
    OR (
      preview_checksum <> ''
      AND preview_payload IS NOT NULL
      AND octet_length(preview_payload) BETWEEN 2 AND 4194304
      AND preview_checksum = 'sha256:' || encode(sha256(preview_payload), 'hex')
    )
  )
);

CREATE INDEX DeploymentPlanDraft_organization_updated
  ON DeploymentPlanDraft (organization_id, updated_at DESC, id DESC);

CREATE INDEX DeploymentPlanDraft_placement
  ON DeploymentPlanDraft (
    organization_id,
    deployment_unit_id,
    environment_assignment_id,
    updated_at DESC
  );

ALTER TABLE DeploymentPlan
  ADD COLUMN plan_schema TEXT NOT NULL DEFAULT 'distr.deployment-plan/v1',
  ADD COLUMN draft_id UUID,
  ADD COLUMN deployment_unit_id UUID,
  ADD COLUMN target_config_snapshot_id UUID,
  ADD COLUMN protocol_version TEXT NOT NULL DEFAULT 'v1',
  ADD COLUMN supersedes_deployment_plan_id UUID,
  ADD COLUMN supersede_reason TEXT NOT NULL DEFAULT '',
  ADD CONSTRAINT deploymentplan_plan_schema_check CHECK (
    plan_schema IN (
      'distr.deployment-plan/v1',
      'distr.target-deployment-plan/v2'
    )
  ),
  ADD CONSTRAINT deploymentplan_protocol_version_check CHECK (
    protocol_version IN ('v1', 'v2')
  ),
  ADD CONSTRAINT deploymentplan_draft_fk
    FOREIGN KEY (draft_id, organization_id)
    REFERENCES DeploymentPlanDraft(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  ADD CONSTRAINT deploymentplan_unit_fk
    FOREIGN KEY (deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  ADD CONSTRAINT deploymentplan_config_unit_fk
    FOREIGN KEY (
      target_config_snapshot_id,
      deployment_unit_id,
      organization_id
    )
    REFERENCES TargetConfigSnapshot(id, deployment_unit_id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  ADD CONSTRAINT deploymentplan_supersedes_fk
    FOREIGN KEY (supersedes_deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  ADD CONSTRAINT deploymentplan_v2_shape_check CHECK (
    (
      plan_schema = 'distr.deployment-plan/v1'
      AND draft_id IS NULL
      AND deployment_unit_id IS NULL
      AND target_config_snapshot_id IS NULL
      AND supersedes_deployment_plan_id IS NULL
      AND supersede_reason = ''
      AND protocol_version = 'v1'
    )
    OR (
      plan_schema = 'distr.target-deployment-plan/v2'
      AND draft_id IS NOT NULL
      AND deployment_unit_id IS NOT NULL
      AND target_config_snapshot_id IS NOT NULL
      AND canonical_checksum ~ '^sha256:[0-9a-f]{64}$'
      AND canonical_checksum = 'sha256:' || encode(sha256(canonical_payload), 'hex')
      AND (
        (
          supersedes_deployment_plan_id IS NULL
          AND supersede_reason = ''
        )
        OR (
          supersedes_deployment_plan_id IS NOT NULL
          AND length(btrim(supersede_reason)) BETWEEN 1 AND 2048
          AND supersede_reason !~ E'[\\r\\n]'
        )
      )
    )
  );

CREATE UNIQUE INDEX DeploymentPlan_v2_draft_unique
  ON DeploymentPlan (draft_id)
  WHERE draft_id IS NOT NULL;

CREATE INDEX DeploymentPlan_v2_placement
  ON DeploymentPlan (
    organization_id,
    deployment_unit_id,
    environment_id,
    created_at DESC,
    id DESC
  )
  WHERE plan_schema = 'distr.target-deployment-plan/v2';

CREATE TABLE DeploymentPlanResolvedRequirement (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  deployment_plan_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  requirement_key TEXT NOT NULL CHECK (
    length(btrim(requirement_key)) BETWEEN 1 AND 512
  ),
  consumer_key TEXT NOT NULL CHECK (
    consumer_key ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
  ),
  capability TEXT NOT NULL CHECK (
    capability ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
  ),
  version_range TEXT NOT NULL CHECK (
    length(btrim(version_range)) BETWEEN 1 AND 128
  ),
  mode TEXT NOT NULL CHECK (
    mode IN (
      'included',
      'pinned_existing',
      'shared_provider',
      'approved_external',
      'feature_disabled'
    )
  ),
  provider_release_id UUID,
  observation_id UUID,
  provider_version TEXT NOT NULL CHECK (
    length(btrim(provider_version)) BETWEEN 1 AND 128
  ),
  provider_platform TEXT NOT NULL CHECK (
    provider_platform ~ '^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$'
  ),
  provider_release_checksum TEXT NOT NULL DEFAULT '' CHECK (
    provider_release_checksum = ''
    OR provider_release_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  provenance_binding_checksum TEXT NOT NULL DEFAULT '' CHECK (
    provenance_binding_checksum = ''
    OR provenance_binding_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  provider_deployment_unit_id UUID,
  component_instance_id UUID,
  subscriber_set_checksum TEXT NOT NULL DEFAULT '' CHECK (
    subscriber_set_checksum = ''
    OR subscriber_set_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  expected_state_version BIGINT NOT NULL CHECK (expected_state_version >= 0),
  expected_state_checksum TEXT NOT NULL CHECK (
    expected_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  binding_checksum TEXT NOT NULL CHECK (
    binding_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
  CONSTRAINT deploymentplanresolvedrequirement_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT deploymentplanresolvedrequirement_release_fk
    FOREIGN KEY (provider_release_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentplanresolvedrequirement_observation_fk
    FOREIGN KEY (observation_id, organization_id)
    REFERENCES TargetComponentObservation(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentplanresolvedrequirement_unit_fk
    FOREIGN KEY (provider_deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentplanresolvedrequirement_instance_fk
    FOREIGN KEY (component_instance_id, organization_id)
    REFERENCES ComponentInstance(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentplanresolvedrequirement_plan_key_unique
    UNIQUE (deployment_plan_id, requirement_key),
  CONSTRAINT deploymentplanresolvedrequirement_plan_order_unique
    UNIQUE (deployment_plan_id, sort_order),
  CONSTRAINT deploymentplanresolvedrequirement_mode_shape_check CHECK (
    (
      mode = 'included'
      AND provider_release_id IS NOT NULL
      AND provider_release_checksum <> ''
      AND provenance_binding_checksum <> ''
      AND component_instance_id IS NOT NULL
      AND provider_deployment_unit_id IS NOT NULL
    )
    OR (
      mode = 'pinned_existing'
      AND provider_release_id IS NOT NULL
      AND provider_release_checksum <> ''
      AND provenance_binding_checksum <> ''
      AND observation_id IS NOT NULL
      AND component_instance_id IS NOT NULL
      AND provider_deployment_unit_id IS NOT NULL
    )
    OR (
      mode = 'shared_provider'
      AND provider_release_id IS NOT NULL
      AND provider_release_checksum <> ''
      AND provenance_binding_checksum <> ''
      AND observation_id IS NOT NULL
      AND provider_deployment_unit_id IS NOT NULL
      AND subscriber_set_checksum <> ''
    )
    OR (
      mode = 'approved_external'
      AND observation_id IS NOT NULL
      AND component_instance_id IS NULL
      AND (
        (
          provider_release_id IS NULL
          AND provider_release_checksum = ''
          AND provenance_binding_checksum = ''
        )
        OR (
          provider_release_id IS NOT NULL
          AND provider_release_checksum <> ''
          AND provenance_binding_checksum <> ''
        )
      )
    )
    OR (
      mode = 'feature_disabled'
      AND provider_release_id IS NULL
      AND provider_release_checksum = ''
      AND provenance_binding_checksum = ''
      AND observation_id IS NULL
      AND provider_deployment_unit_id IS NULL
      AND component_instance_id IS NULL
    )
  )
);

CREATE INDEX DeploymentPlanResolvedRequirement_plan_order
  ON DeploymentPlanResolvedRequirement (
    organization_id,
    deployment_plan_id,
    sort_order,
    requirement_key
  );

CREATE TABLE DeploymentPlanStepEdge (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  deployment_plan_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  edge_key TEXT NOT NULL CHECK (
    length(btrim(edge_key)) BETWEEN 1 AND 1024
  ),
  from_step_key TEXT NOT NULL CHECK (
    length(btrim(from_step_key)) BETWEEN 1 AND 512
  ),
  to_step_key TEXT NOT NULL CHECK (
    length(btrim(to_step_key)) BETWEEN 1 AND 512
  ),
  CONSTRAINT deploymentplanstepedge_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT deploymentplanstepedge_from_fk
    FOREIGN KEY (deployment_plan_id, from_step_key, organization_id)
    REFERENCES DeploymentPlanStep(
      deployment_plan_id,
      step_key,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanstepedge_to_fk
    FOREIGN KEY (deployment_plan_id, to_step_key, organization_id)
    REFERENCES DeploymentPlanStep(
      deployment_plan_id,
      step_key,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentplanstepedge_plan_edge_unique
    UNIQUE (deployment_plan_id, edge_key),
  CONSTRAINT deploymentplanstepedge_no_self_loop
    CHECK (from_step_key <> to_step_key)
);

CREATE INDEX DeploymentPlanStepEdge_plan
  ON DeploymentPlanStepEdge (
    organization_id,
    deployment_plan_id,
    from_step_key,
    to_step_key
  );

CREATE FUNCTION deployment_plan_draft_publication_guard()
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
  IF EXISTS (
    SELECT 1
    FROM DeploymentPlan published
    WHERE published.draft_id = OLD.id
      AND published.organization_id = OLD.organization_id
  ) THEN
    RAISE EXCEPTION 'published deployment plan draft is immutable'
      USING ERRCODE = '23514';
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'deployment plan drafts retain publication audit lineage'
      USING ERRCODE = '23514';
  END IF;
  IF NEW.revision <> OLD.revision + 1 THEN
    RAISE EXCEPTION 'deployment plan draft revision must increment by one'
      USING ERRCODE = '40001';
  END IF;
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;

CREATE TRIGGER DeploymentPlanDraft_publication_guard
BEFORE UPDATE OR DELETE ON DeploymentPlanDraft
FOR EACH ROW EXECUTE FUNCTION deployment_plan_draft_publication_guard();

CREATE FUNCTION deployment_plan_v2_immutable_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF OLD.plan_schema <> 'distr.target-deployment-plan/v2' THEN
      RETURN OLD;
    END IF;
    IF current_setting(
      'distr.deployment_registry_deletion_reason',
      true
    ) = 'ORGANIZATION_RETENTION' THEN
      RETURN OLD;
    END IF;
    RAISE EXCEPTION 'published target deployment plans retain audit lineage'
      USING ERRCODE = '23514';
  END IF;
  IF (
       OLD.plan_schema = 'distr.target-deployment-plan/v2'
       OR NEW.plan_schema = 'distr.target-deployment-plan/v2'
     )
     AND (
       NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.release_bundle_id IS DISTINCT FROM OLD.release_bundle_id
       OR NEW.application_id IS DISTINCT FROM OLD.application_id
       OR NEW.channel_id IS DISTINCT FROM OLD.channel_id
       OR NEW.environment_id IS DISTINCT FROM OLD.environment_id
       OR NEW.canonical_checksum IS DISTINCT FROM OLD.canonical_checksum
       OR NEW.canonical_payload IS DISTINCT FROM OLD.canonical_payload
       OR NEW.plan_schema IS DISTINCT FROM OLD.plan_schema
       OR NEW.draft_id IS DISTINCT FROM OLD.draft_id
       OR NEW.deployment_unit_id IS DISTINCT FROM OLD.deployment_unit_id
       OR NEW.target_config_snapshot_id IS DISTINCT FROM OLD.target_config_snapshot_id
       OR NEW.protocol_version IS DISTINCT FROM OLD.protocol_version
       OR NEW.supersedes_deployment_plan_id IS DISTINCT
          FROM OLD.supersedes_deployment_plan_id
       OR NEW.supersede_reason IS DISTINCT FROM OLD.supersede_reason
     ) THEN
    RAISE EXCEPTION 'published target deployment plan is immutable'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER DeploymentPlan_v2_immutable_guard
BEFORE UPDATE OR DELETE ON DeploymentPlan
FOR EACH ROW EXECUTE FUNCTION deployment_plan_v2_immutable_guard();

CREATE FUNCTION deployment_plan_v2_append_only_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'TRUNCATE' THEN
    RAISE EXCEPTION 'published target deployment plan facts are append-only'
      USING ERRCODE = '23514';
  END IF;
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.deployment_registry_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'published target deployment plan facts are append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER DeploymentPlanResolvedRequirement_append_only
BEFORE UPDATE OR DELETE ON DeploymentPlanResolvedRequirement
FOR EACH ROW EXECUTE FUNCTION deployment_plan_v2_append_only_guard();

CREATE TRIGGER DeploymentPlanResolvedRequirement_no_truncate
BEFORE TRUNCATE ON DeploymentPlanResolvedRequirement
FOR EACH STATEMENT EXECUTE FUNCTION deployment_plan_v2_append_only_guard();

CREATE TRIGGER DeploymentPlanStepEdge_append_only
BEFORE UPDATE OR DELETE ON DeploymentPlanStepEdge
FOR EACH ROW EXECUTE FUNCTION deployment_plan_v2_append_only_guard();

CREATE TRIGGER DeploymentPlanStepEdge_no_truncate
BEFORE TRUNCATE ON DeploymentPlanStepEdge
FOR EACH STATEMENT EXECUTE FUNCTION deployment_plan_v2_append_only_guard();
