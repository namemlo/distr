ALTER TABLE DeploymentPlanBaseline
  ADD COLUMN active_desired_revision_id UUID,
  ADD COLUMN observed_component_state_id UUID,
  ADD CONSTRAINT deploymentplanbaseline_active_desired_fk
    FOREIGN KEY (active_desired_revision_id, organization_id)
    REFERENCES ActiveDesiredRevision(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  ADD CONSTRAINT deploymentplanbaseline_observed_component_fk
    FOREIGN KEY (observed_component_state_id, organization_id)
    REFERENCES ObservedComponentState(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION;

ALTER TABLE DeploymentPlanBaseline
  DROP CONSTRAINT deploymentplanbaseline_shape_check,
  ADD CONSTRAINT deploymentplanbaseline_shape_check CHECK (
    (
      projection = 'bootstrap'
      AND bootstrap
      AND NOT authorizes_v2_execution
      AND source_deployment_plan_id IS NULL
      AND external_execution_id IS NULL
      AND active_desired_revision_id IS NULL
      AND observed_component_state_id IS NULL
      AND observation_id IS NULL
      AND observed_at IS NULL
      AND observation_checksum = ''
      AND release_bundle_id IS NULL
    )
    OR (
      projection = 'legacy_projection'
      AND NOT bootstrap
      AND NOT authorizes_v2_execution
      AND active_desired_revision_id IS NULL
      AND observed_component_state_id IS NULL
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
      AND (
        (
          external_execution_id IS NOT NULL
          AND observation_id IS NOT NULL
          AND active_desired_revision_id IS NULL
          AND observed_component_state_id IS NULL
        )
        OR (
          external_execution_id IS NULL
          AND observation_id IS NULL
          AND active_desired_revision_id IS NOT NULL
          AND observed_component_state_id IS NOT NULL
        )
      )
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
  ADD COLUMN active_desired_revision_id UUID,
  ADD COLUMN observed_component_state_id UUID,
  ADD CONSTRAINT deploymentplanresolvedrequirement_active_desired_fk
    FOREIGN KEY (active_desired_revision_id, organization_id)
    REFERENCES ActiveDesiredRevision(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  ADD CONSTRAINT deploymentplanresolvedrequirement_observed_component_fk
    FOREIGN KEY (observed_component_state_id, organization_id)
    REFERENCES ObservedComponentState(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  ADD CONSTRAINT deploymentplanresolvedrequirement_native_pair_check CHECK (
    (active_desired_revision_id IS NULL) =
    (observed_component_state_id IS NULL)
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
      AND (
        (
          observation_id IS NOT NULL
          AND active_desired_revision_id IS NULL
        )
        OR (
          observation_id IS NULL
          AND active_desired_revision_id IS NOT NULL
        )
      )
      AND component_instance_id IS NOT NULL
      AND provider_deployment_unit_id IS NOT NULL
    )
    OR (
      mode = 'shared_provider'
      AND provider_release_id IS NOT NULL
      AND provider_release_checksum <> ''
      AND provenance_binding_checksum <> ''
      AND (
        (
          observation_id IS NOT NULL
          AND active_desired_revision_id IS NULL
        )
        OR (
          observation_id IS NULL
          AND active_desired_revision_id IS NOT NULL
        )
      )
      AND provider_deployment_unit_id IS NOT NULL
      AND subscriber_set_checksum <> ''
    )
    OR (
      mode = 'approved_external'
      AND (
        (
          observation_id IS NOT NULL
          AND active_desired_revision_id IS NULL
        )
        OR (
          observation_id IS NULL
          AND active_desired_revision_id IS NOT NULL
        )
      )
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
      AND active_desired_revision_id IS NULL
      AND provider_deployment_unit_id IS NULL
      AND component_instance_id IS NULL
    )
  );
