LOCK TABLE DeploymentPlanBaseline, DeploymentPlanResolvedRequirement
  IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM DeploymentPlanBaseline
    WHERE active_desired_revision_id IS NOT NULL
       OR observed_component_state_id IS NOT NULL
  ) OR EXISTS (
    SELECT 1
    FROM DeploymentPlanResolvedRequirement
    WHERE active_desired_revision_id IS NOT NULL
       OR observed_component_state_id IS NOT NULL
  ) THEN
    RAISE EXCEPTION
      'refusing migration 164 rollback: native planning lineage exists';
  END IF;
END
$$;

ALTER TABLE DeploymentPlanBaseline
  DROP CONSTRAINT deploymentplanbaseline_shape_check,
  ADD CONSTRAINT deploymentplanbaseline_shape_check CHECK (
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
  );

ALTER TABLE DeploymentPlanResolvedRequirement
  DROP CONSTRAINT deploymentplanresolvedrequirement_mode_shape_check,
  ADD CONSTRAINT deploymentplanresolvedrequirement_mode_shape_check CHECK (
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
  );

ALTER TABLE DeploymentPlanResolvedRequirement
  DROP CONSTRAINT deploymentplanresolvedrequirement_native_pair_check,
  DROP CONSTRAINT deploymentplanresolvedrequirement_observed_component_fk,
  DROP CONSTRAINT deploymentplanresolvedrequirement_active_desired_fk,
  DROP COLUMN observed_component_state_id,
  DROP COLUMN active_desired_revision_id;

ALTER TABLE DeploymentPlanBaseline
  DROP CONSTRAINT deploymentplanbaseline_observed_component_fk,
  DROP CONSTRAINT deploymentplanbaseline_active_desired_fk,
  DROP COLUMN observed_component_state_id,
  DROP COLUMN active_desired_revision_id;
