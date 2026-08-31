SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE ObservedComponentState
  ADD COLUMN health_evidence_kind TEXT,
  ADD COLUMN health_evidence_use TEXT,
  ADD COLUMN health_policy_checksum TEXT,
  ADD CONSTRAINT observedcomponentstate_health_evidence_shape CHECK (
    (
      health_evidence_kind IS NULL
      AND health_evidence_use IS NULL
      AND health_policy_checksum IS NULL
    ) OR (
      health_policy_checksum ~ '^sha256:[0-9a-f]{64}$'
      AND evidence_reference ~ '^evidence://sha256/[0-9a-f]{64}$'
      AND evidence_reference = 'evidence://sha256/' ||
        substring(evidence_checksum FROM 8)
      AND (
        (
          health_evidence_kind = 'STANDARD_READINESS'
          AND health_evidence_use = 'STANDARD_PROMOTION_ELIGIBLE'
        ) OR (
          health_evidence_kind = 'LEGACY_LIVENESS_ONLY'
          AND health_evidence_use = 'BASELINE_OR_ROLLBACK_ONLY'
        )
      )
    )
  );

CREATE FUNCTION observed_component_health_evidence_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.health_evidence_kind IS NOT DISTINCT FROM OLD.health_evidence_kind
     AND NEW.health_evidence_use IS NOT DISTINCT FROM OLD.health_evidence_use
     AND NEW.health_policy_checksum IS NOT DISTINCT FROM OLD.health_policy_checksum THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'observed health evidence is immutable'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER ObservedComponentState_health_evidence_immutable
BEFORE UPDATE OF health_evidence_kind, health_evidence_use,
  health_policy_checksum ON ObservedComponentState
FOR EACH ROW EXECUTE FUNCTION observed_component_health_evidence_guard();

CREATE TABLE BaselineAdoption (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  deployment_plan_id UUID NOT NULL,
  product_release_id UUID NOT NULL,
  target_config_snapshot_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  environment_id UUID NOT NULL,
  deployment_target_id UUID NOT NULL,
  actor_user_account_id UUID NOT NULL,
  authorization_action TEXT NOT NULL CHECK (
    authorization_action = 'plan.execute'
  ),
  idempotency_key TEXT NOT NULL CHECK (
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
  ),
  reason TEXT NOT NULL CHECK (
    length(btrim(reason)) BETWEEN 1 AND 2048
    AND reason !~ E'[\\r\\n]'
  ),
  plan_checksum TEXT NOT NULL CHECK (
    plan_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  product_release_checksum TEXT NOT NULL CHECK (
    product_release_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  target_config_checksum TEXT NOT NULL CHECK (
    target_config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  request_payload BYTEA NOT NULL CHECK (
    octet_length(request_payload) BETWEEN 2 AND 4194304
  ),
  request_checksum TEXT NOT NULL CHECK (
    request_checksum = 'sha256:' || encode(sha256(request_payload), 'hex')
  ),
  outcome_checksum TEXT NOT NULL CHECK (
    outcome_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  status TEXT NOT NULL CHECK (status = 'ADOPTED'),
  deployment_performed BOOLEAN NOT NULL DEFAULT FALSE CHECK (
    deployment_performed = FALSE
  ),
  task_count INTEGER NOT NULL DEFAULT 0 CHECK (task_count = 0),
  lock_count INTEGER NOT NULL DEFAULT 0 CHECK (lock_count = 0),
  execution_count INTEGER NOT NULL DEFAULT 0 CHECK (execution_count = 0),
  CONSTRAINT baselineadoption_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT baselineadoption_plan_unique
    UNIQUE (deployment_plan_id, organization_id),
  CONSTRAINT baselineadoption_idempotency_unique
    UNIQUE (organization_id, idempotency_key),
  CONSTRAINT baselineadoption_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id) ON DELETE CASCADE,
  CONSTRAINT baselineadoption_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoption_product_release_fk
    FOREIGN KEY (product_release_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoption_target_config_fk
    FOREIGN KEY (target_config_snapshot_id, organization_id)
    REFERENCES TargetConfigSnapshot(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoption_unit_fk
    FOREIGN KEY (deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoption_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoption_actor_fk
    FOREIGN KEY (organization_id, actor_user_account_id)
    REFERENCES Organization_UserAccount(organization_id, user_account_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE TABLE BaselineAdoptionComponent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  baseline_adoption_id UUID NOT NULL,
  deployment_plan_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  component_instance_id UUID NOT NULL,
  component_key TEXT NOT NULL CHECK (
    component_key ~ '^[a-z0-9][a-z0-9._-]{0,127}$'
  ),
  component_release_id UUID NOT NULL,
  component_release_checksum TEXT NOT NULL CHECK (
    component_release_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  source_commit TEXT NOT NULL CHECK (source_commit ~ '^[0-9a-f]{40}$'),
  build_id TEXT NOT NULL CHECK (
    length(btrim(build_id)) BETWEEN 1 AND 1024
    AND build_id !~ E'[\\r\\n]'
  ),
  provenance_verification_id UUID NOT NULL,
  provenance_evidence_digest TEXT NOT NULL CHECK (
    provenance_evidence_digest ~ '^sha256:[0-9a-f]{64}$'
  ),
  provenance_policy_checksum TEXT NOT NULL CHECK (
    provenance_policy_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  artifact_digest TEXT NOT NULL CHECK (
    artifact_digest ~ '^sha256:[0-9a-f]{64}$'
  ),
  platform TEXT NOT NULL CHECK (
    platform ~ '^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$'
  ),
  target_config_snapshot_id UUID NOT NULL,
  config_checksum TEXT NOT NULL CHECK (
    config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  schema_version TEXT NOT NULL CHECK (
    length(btrim(schema_version)) BETWEEN 1 AND 256
  ),
  capability_checksum TEXT NOT NULL CHECK (
    capability_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  topology_checksum TEXT NOT NULL CHECK (
    topology_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  observation_id UUID NOT NULL,
  observer_id UUID NOT NULL,
  observation_evidence_checksum TEXT NOT NULL CHECK (
    observation_evidence_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  observation_evidence_reference TEXT NOT NULL CHECK (
    observation_evidence_reference ~ '^evidence://sha256/[0-9a-f]{64}$'
    AND observation_evidence_reference = 'evidence://sha256/' ||
      substring(observation_evidence_checksum FROM 8)
  ),
  observation_state_checksum TEXT NOT NULL CHECK (
    observation_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  observation_runtime_state_checksum TEXT NOT NULL CHECK (
    observation_runtime_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  health_evidence_kind TEXT NOT NULL CHECK (
    health_evidence_kind IN ('STANDARD_READINESS', 'LEGACY_LIVENESS_ONLY')
  ),
  health_evidence_use TEXT NOT NULL CHECK (
    (
      health_evidence_kind = 'STANDARD_READINESS'
      AND health_evidence_use = 'STANDARD_PROMOTION_ELIGIBLE'
    ) OR (
      health_evidence_kind = 'LEGACY_LIVENESS_ONLY'
      AND health_evidence_use = 'BASELINE_OR_ROLLBACK_ONLY'
    )
  ),
  health_policy_checksum TEXT NOT NULL CHECK (
    health_policy_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  observation_captured_at TIMESTAMPTZ NOT NULL,
  observation_fresh_until TIMESTAMPTZ NOT NULL CHECK (
    observation_fresh_until >= observation_captured_at
  ),
  active_desired_revision_id UUID NOT NULL,
  desired_revision BIGINT NOT NULL CHECK (desired_revision > 0),
  CONSTRAINT baselineadoptioncomponent_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT baselineadoptioncomponent_adoption_instance_unique
    UNIQUE (baseline_adoption_id, component_instance_id),
  CONSTRAINT baselineadoptioncomponent_adoption_key_unique
    UNIQUE (baseline_adoption_id, component_key),
  CONSTRAINT baselineadoptioncomponent_active_unique
    UNIQUE (active_desired_revision_id),
  CONSTRAINT baselineadoptioncomponent_adoption_fk
    FOREIGN KEY (baseline_adoption_id, organization_id)
    REFERENCES BaselineAdoption(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoptioncomponent_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoptioncomponent_release_fk
    FOREIGN KEY (component_release_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoptioncomponent_instance_fk
    FOREIGN KEY (
      component_instance_id, deployment_unit_id, organization_id
    ) REFERENCES ComponentInstance(
      id, deployment_unit_id, organization_id
    )
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoptioncomponent_config_fk
    FOREIGN KEY (
      target_config_snapshot_id, deployment_unit_id, organization_id
    ) REFERENCES TargetConfigSnapshot(
      id, deployment_unit_id, organization_id
    )
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoptioncomponent_provenance_fk
    FOREIGN KEY (provenance_verification_id)
    REFERENCES ComponentReleaseEvidenceVerification(id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoptioncomponent_observer_fk
    FOREIGN KEY (observer_id, organization_id)
    REFERENCES ObserverRegistration(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT baselineadoptioncomponent_observation_fk
    FOREIGN KEY (
      observation_id, deployment_unit_id,
      component_instance_id, organization_id
    ) REFERENCES ObservedComponentState(
      id, deployment_unit_id, component_instance_id, organization_id
    ) ON UPDATE NO ACTION ON DELETE NO ACTION
);

ALTER TABLE ActiveDesiredRevision
  ALTER COLUMN pending_revision_id DROP NOT NULL,
  ALTER COLUMN execution_id DROP NOT NULL,
  ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'EXECUTION' CHECK (
    source_kind IN ('EXECUTION', 'BASELINE_ADOPTION')
  ),
  ADD COLUMN baseline_adoption_component_id UUID,
  ADD COLUMN health_evidence_kind TEXT NOT NULL DEFAULT 'STANDARD_READINESS',
  ADD COLUMN health_evidence_use TEXT NOT NULL DEFAULT 'STANDARD_PROMOTION_ELIGIBLE',
  ADD CONSTRAINT activedesiredrevision_baseline_component_unique
    UNIQUE (baseline_adoption_component_id),
  ADD CONSTRAINT activedesiredrevision_source_shape_check CHECK (
    (
      source_kind = 'EXECUTION'
      AND pending_revision_id IS NOT NULL
      AND execution_id IS NOT NULL
      AND baseline_adoption_component_id IS NULL
      AND health_evidence_kind = 'STANDARD_READINESS'
      AND health_evidence_use = 'STANDARD_PROMOTION_ELIGIBLE'
    ) OR (
      source_kind = 'BASELINE_ADOPTION'
      AND pending_revision_id IS NULL
      AND execution_id IS NULL
      AND baseline_adoption_component_id IS NOT NULL
      AND (
        (
          health_evidence_kind = 'STANDARD_READINESS'
          AND health_evidence_use = 'STANDARD_PROMOTION_ELIGIBLE'
        ) OR (
          health_evidence_kind = 'LEGACY_LIVENESS_ONLY'
          AND health_evidence_use = 'BASELINE_OR_ROLLBACK_ONLY'
        )
      )
    )
  ),
  ADD CONSTRAINT activedesiredrevision_baseline_component_fk
    FOREIGN KEY (baseline_adoption_component_id, organization_id)
    REFERENCES BaselineAdoptionComponent(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE BaselineAdoptionComponent
  ADD CONSTRAINT baselineadoptioncomponent_active_fk
    FOREIGN KEY (active_desired_revision_id, organization_id)
    REFERENCES ActiveDesiredRevision(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
    DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION baseline_adoption_insert_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  plan_status TEXT;
  plan_schema TEXT;
  protocol_version TEXT;
BEGIN
  SELECT status, DeploymentPlan.plan_schema, DeploymentPlan.protocol_version
  INTO plan_status, plan_schema, protocol_version
  FROM DeploymentPlan
  WHERE id = NEW.deployment_plan_id
    AND organization_id = NEW.organization_id
  FOR UPDATE;
  IF NOT FOUND
     OR plan_status <> 'READY'
     OR plan_schema <> 'distr.target-deployment-plan/v2'
     OR protocol_version <> 'v2' THEN
    RAISE EXCEPTION 'baseline adoption requires a READY native v2 plan'
      USING ERRCODE = '23514';
  END IF;
  IF EXISTS (
       SELECT 1 FROM Task
       WHERE deployment_plan_id = NEW.deployment_plan_id
         AND organization_id = NEW.organization_id
     ) OR EXISTS (
       SELECT 1 FROM ExternalExecution
       WHERE deployment_plan_id = NEW.deployment_plan_id
         AND organization_id = NEW.organization_id
     ) THEN
    RAISE EXCEPTION
      'baseline adoption cannot coexist with deployment tasks or executions'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER BaselineAdoption_insert_guard
BEFORE INSERT ON BaselineAdoption
FOR EACH ROW EXECUTE FUNCTION baseline_adoption_insert_guard();

CREATE FUNCTION baseline_adoption_execution_exclusion_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM BaselineAdoption
    WHERE deployment_plan_id = NEW.deployment_plan_id
      AND organization_id = NEW.organization_id
  ) THEN
    RAISE EXCEPTION
      'baseline adoption cannot coexist with deployment tasks or executions'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER Task_baseline_adoption_exclusion
BEFORE INSERT OR UPDATE OF deployment_plan_id, organization_id ON Task
FOR EACH ROW EXECUTE FUNCTION baseline_adoption_execution_exclusion_guard();

CREATE TRIGGER ExternalExecution_baseline_adoption_exclusion
BEFORE INSERT OR UPDATE OF deployment_plan_id, organization_id ON ExternalExecution
FOR EACH ROW EXECUTE FUNCTION baseline_adoption_execution_exclusion_guard();

CREATE FUNCTION baseline_adoption_plan_status_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  has_tasks BOOLEAN;
  has_adoption BOOLEAN;
BEGIN
  IF OLD.plan_schema = 'distr.target-deployment-plan/v2'
     AND OLD.status = 'READY'
     AND NEW.status = 'EXECUTED' THEN
    SELECT EXISTS (
      SELECT 1 FROM Task
      WHERE deployment_plan_id = NEW.id
        AND organization_id = NEW.organization_id
    ), EXISTS (
      SELECT 1 FROM BaselineAdoption
      WHERE deployment_plan_id = NEW.id
        AND organization_id = NEW.organization_id
        AND status = 'ADOPTED'
    ) INTO has_tasks, has_adoption;
    IF has_tasks = has_adoption THEN
      RAISE EXCEPTION
        'native v2 plan outcome requires exactly one execution or baseline-adoption source'
        USING ERRCODE = '23514';
    END IF;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER DeploymentPlan_baseline_adoption_status_guard
BEFORE UPDATE ON DeploymentPlan
FOR EACH ROW EXECUTE FUNCTION baseline_adoption_plan_status_guard();

CREATE FUNCTION baseline_adoption_commit_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  adopted_components BIGINT;
  configured_components BIGINT;
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM DeploymentPlan plan
    JOIN ReleaseBundle product
      ON product.id = plan.release_bundle_id
     AND product.organization_id = plan.organization_id
     AND product.id = NEW.product_release_id
     AND product.kind = 'product'
     AND product.status = 'PUBLISHED'
     AND product.canonical_checksum = NEW.product_release_checksum
    JOIN TargetConfigSnapshot config
      ON config.id = plan.target_config_snapshot_id
     AND config.organization_id = plan.organization_id
     AND config.id = NEW.target_config_snapshot_id
     AND config.deployment_unit_id = NEW.deployment_unit_id
     AND config.environment_id = NEW.environment_id
     AND config.canonical_checksum = NEW.target_config_checksum
    JOIN DeploymentPlanTarget target
      ON target.deployment_plan_id = plan.id
     AND target.organization_id = plan.organization_id
     AND target.deployment_target_id = NEW.deployment_target_id
    WHERE plan.id = NEW.deployment_plan_id
      AND plan.organization_id = NEW.organization_id
      AND plan.status = 'EXECUTED'
      AND plan.plan_schema = 'distr.target-deployment-plan/v2'
      AND plan.protocol_version = 'v2'
      AND plan.deployment_unit_id = NEW.deployment_unit_id
      AND plan.environment_id = NEW.environment_id
      AND plan.canonical_checksum = NEW.plan_checksum
      AND convert_from(plan.canonical_payload, 'UTF8')::JSONB
            ->> 'productReleaseId' = NEW.product_release_id::TEXT
      AND convert_from(plan.canonical_payload, 'UTF8')::JSONB
            ->> 'productReleaseChecksum' = NEW.product_release_checksum
      AND convert_from(plan.canonical_payload, 'UTF8')::JSONB
            ->> 'deploymentUnitId' = NEW.deployment_unit_id::TEXT
      AND convert_from(plan.canonical_payload, 'UTF8')::JSONB
            ->> 'environmentId' = NEW.environment_id::TEXT
      AND convert_from(plan.canonical_payload, 'UTF8')::JSONB
            ->> 'deploymentTargetId' = NEW.deployment_target_id::TEXT
      AND convert_from(plan.canonical_payload, 'UTF8')::JSONB
            ->> 'targetConfigSnapshotId' = NEW.target_config_snapshot_id::TEXT
      AND convert_from(plan.canonical_payload, 'UTF8')::JSONB
            ->> 'targetConfigSnapshotChecksum' = NEW.target_config_checksum
      AND convert_from(plan.canonical_payload, 'UTF8')::JSONB
            ->> 'bootstrap' = 'true'
      AND NOT (
        convert_from(plan.canonical_payload, 'UTF8')::JSONB
          ? 'previousStateSourcePlanId'
      )
  ) THEN
    RAISE EXCEPTION 'baseline adoption must commit with an exact successful plan outcome'
      USING ERRCODE = '23514';
  END IF;
  IF EXISTS (
       SELECT 1 FROM Task
       WHERE deployment_plan_id = NEW.deployment_plan_id
         AND organization_id = NEW.organization_id
     ) OR EXISTS (
       SELECT 1 FROM ExternalExecution
       WHERE deployment_plan_id = NEW.deployment_plan_id
         AND organization_id = NEW.organization_id
     ) THEN
    RAISE EXCEPTION
      'baseline adoption cannot coexist with deployment tasks or executions'
      USING ERRCODE = '23514';
  END IF;
  SELECT count(*) INTO adopted_components
  FROM BaselineAdoptionComponent component
  WHERE component.baseline_adoption_id = NEW.id
    AND component.organization_id = NEW.organization_id;
  SELECT jsonb_array_length(
    convert_from(plan.canonical_payload, 'UTF8')::JSONB
      -> 'componentReleasePins'
  ) INTO configured_components
  FROM DeploymentPlan plan
  WHERE plan.id = NEW.deployment_plan_id
    AND plan.organization_id = NEW.organization_id;
  IF adopted_components = 0 OR adopted_components <> configured_components THEN
    RAISE EXCEPTION 'baseline adoption component coverage is incomplete'
      USING ERRCODE = '23514';
  END IF;
  IF EXISTS (
    SELECT 1
    FROM BaselineAdoptionComponent component
    LEFT JOIN ActiveDesiredRevision active
      ON active.id = component.active_desired_revision_id
     AND active.organization_id = component.organization_id
     AND active.baseline_adoption_component_id = component.id
     AND active.source_kind = 'BASELINE_ADOPTION'
     AND active.pending_revision_id IS NULL
     AND active.execution_id IS NULL
     AND active.deployment_plan_id = component.deployment_plan_id
     AND active.deployment_unit_id = component.deployment_unit_id
     AND active.component_instance_id = component.component_instance_id
     AND active.component_key = component.component_key
     AND active.revision = component.desired_revision
     AND active.artifact_digest = component.artifact_digest
     AND active.config_checksum = component.config_checksum
     AND active.schema_version = component.schema_version
     AND active.capability_checksum = component.capability_checksum
     AND active.platform = component.platform
     AND active.topology_checksum = component.topology_checksum
     AND active.verified_observation_id = component.observation_id
     AND active.health_evidence_kind = component.health_evidence_kind
     AND active.health_evidence_use = component.health_evidence_use
    LEFT JOIN ComponentDesiredStateHead head
      ON head.organization_id = component.organization_id
     AND head.deployment_unit_id = NEW.deployment_unit_id
     AND head.component_instance_id = component.component_instance_id
     AND head.active_revision_id = active.id
     AND head.pending_revision_id IS NULL
     AND NOT head.quarantined
    LEFT JOIN ObservedComponentState observation
      ON observation.id = component.observation_id
     AND observation.organization_id = component.organization_id
     AND observation.observer_id = component.observer_id
     AND observation.deployment_unit_id = component.deployment_unit_id
     AND observation.component_instance_id = component.component_instance_id
     AND observation.component_key = component.component_key
     AND observation.evidence_checksum = component.observation_evidence_checksum
     AND observation.evidence_reference
       = component.observation_evidence_reference
     AND observation.evidence_reference ~ '^evidence://sha256/[0-9a-f]{64}$'
     AND observation.evidence_reference = 'evidence://sha256/' ||
       substring(observation.evidence_checksum FROM 8)
     AND observation.artifact_digest = component.artifact_digest
     AND observation.config_checksum = component.config_checksum
     AND observation.schema_version = component.schema_version
     AND observation.capability_checksum = component.capability_checksum
     AND observation.platform = component.platform
     AND observation.topology_checksum = component.topology_checksum
     AND observation.state_checksum = component.observation_state_checksum
     AND observation.runtime_state_checksum
       = component.observation_runtime_state_checksum
     AND observation.health_evidence_kind = component.health_evidence_kind
     AND observation.health_evidence_use = component.health_evidence_use
     AND observation.health_policy_checksum = component.health_policy_checksum
     AND observation.is_current
     AND observation.trusted
     AND observation.disposition = 'ACCEPTED'
     AND observation.health = 'HEALTHY'
     AND observation.outcome = 'COMPLETE'
     AND observation.executor_outcome = ''
     AND observation.captured_at = component.observation_captured_at
     AND observation.fresh_until = component.observation_fresh_until
     AND observation.fresh_until >= clock_timestamp()
    LEFT JOIN ComponentObservationHead observation_head
      ON observation_head.organization_id = observation.organization_id
     AND observation_head.observer_id = observation.observer_id
     AND observation_head.deployment_unit_id = observation.deployment_unit_id
     AND observation_head.component_instance_id = observation.component_instance_id
     AND observation_head.observation_id = observation.id
     AND observation_head.evidence_checksum = observation.evidence_checksum
     AND observation_head.captured_at = observation.captured_at
     AND observation.health_evidence_kind IS NOT NULL
     AND observation.health_evidence_use IS NOT NULL
     AND observation.health_policy_checksum IS NOT NULL
    WHERE component.baseline_adoption_id = NEW.id
      AND component.organization_id = NEW.organization_id
      AND (
        active.id IS NULL
        OR head.active_revision_id IS NULL
        OR observation.id IS NULL
        OR observation_head.observation_id IS NULL
      )
  ) THEN
    RAISE EXCEPTION 'baseline adoption desired or observed lineage is incomplete'
      USING ERRCODE = '23514';
  END IF;
  IF EXISTS (
    SELECT 1
    FROM BaselineAdoptionComponent component
    WHERE component.baseline_adoption_id = NEW.id
      AND component.organization_id = NEW.organization_id
      AND (
        component.config_checksum <> NEW.target_config_checksum
        OR component.capability_checksum <> component.component_release_checksum
        OR NOT EXISTS (
          SELECT 1
          FROM DeploymentPlan plan
          CROSS JOIN LATERAL jsonb_array_elements(
            convert_from(plan.canonical_payload, 'UTF8')::JSONB
              -> 'componentReleasePins'
          ) pin
          WHERE plan.id = component.deployment_plan_id
            AND plan.organization_id = component.organization_id
            AND pin ->> 'componentKey' = component.component_key
            AND pin ->> 'componentReleaseId'
              = component.component_release_id::TEXT
            AND pin ->> 'releaseChecksum'
              = component.component_release_checksum
            AND pin ->> 'version' = component.schema_version
            AND pin ->> 'platformDigest' = component.artifact_digest
            AND pin ->> 'provenanceVerified' = 'true'
            AND convert_from(plan.canonical_payload, 'UTF8')::JSONB
                  ->> 'targetPlatform' = component.platform
            AND EXISTS (
              SELECT 1
              FROM jsonb_array_elements_text(pin -> 'platforms') platform
              WHERE platform = component.platform
            )
            AND EXISTS (
              SELECT 1
              FROM jsonb_array_elements(pin -> 'artifacts') artifact
              WHERE artifact ->> 'platform' = component.platform
                AND artifact ->> 'platformDigest'
                  = component.artifact_digest
            )
            AND EXISTS (
              SELECT 1
              FROM jsonb_array_elements(pin -> 'provenanceFacts') fact
              WHERE fact ->> 'verificationId'
                  = component.provenance_verification_id::TEXT
                AND fact ->> 'platform' = component.platform
                AND fact ->> 'artifactDigest' = component.artifact_digest
                AND fact ->> 'evidenceDigest'
                  = component.provenance_evidence_digest
                AND fact ->> 'policyChecksum'
                  = component.provenance_policy_checksum
            )
        )
        OR NOT EXISTS (
          SELECT 1
          FROM DeploymentPlan plan
          CROSS JOIN LATERAL jsonb_array_elements(
            convert_from(plan.canonical_payload, 'UTF8')::JSONB
              -> 'componentBindings'
          ) binding
          WHERE plan.id = component.deployment_plan_id
            AND plan.organization_id = component.organization_id
            AND binding ->> 'componentKey' = component.component_key
            AND binding ->> 'componentInstanceId'
              = component.component_instance_id::TEXT
        )
        OR NOT EXISTS (
          SELECT 1
          FROM ProductReleaseComponent product_component
          JOIN ReleaseBundle component_release
            ON component_release.id
              = product_component.component_release_bundle_id
           AND component_release.organization_id
              = product_component.organization_id
           AND component_release.kind = 'component'
           AND component_release.status = 'PUBLISHED'
           AND component_release.canonical_checksum
              = component.component_release_checksum
          WHERE product_component.product_release_bundle_id
              = NEW.product_release_id
            AND product_component.organization_id = NEW.organization_id
            AND product_component.component_release_bundle_id
              = component.component_release_id
            AND product_component.component_release_checksum
              = component.component_release_checksum
            AND product_component.component_key = component.component_key
        )
        OR NOT EXISTS (
          SELECT 1
          FROM ComponentReleaseEvidenceVerification verification
          JOIN ComponentReleaseArtifact artifact
            ON artifact.release_bundle_id = verification.release_bundle_id
           AND artifact.organization_id = verification.organization_id
           AND artifact.artifact_key = verification.artifact_key
           AND artifact.platform = verification.platform
           AND artifact.platform_digest = verification.artifact_digest
          WHERE verification.id = component.provenance_verification_id
            AND verification.organization_id = component.organization_id
            AND verification.release_bundle_id = component.component_release_id
            AND verification.platform = component.platform
            AND verification.artifact_digest = component.artifact_digest
            AND verification.source_commit = component.source_commit
            AND verification.build_id = component.build_id
            AND verification.evidence_digest
              = component.provenance_evidence_digest
            AND verification.policy_checksum
              = component.provenance_policy_checksum
        )
        OR NOT EXISTS (
          SELECT 1
          FROM TargetConfigSnapshotComponent config_component
          WHERE config_component.target_config_snapshot_id
              = component.target_config_snapshot_id
            AND config_component.organization_id = component.organization_id
            AND config_component.deployment_unit_id
              = component.deployment_unit_id
            AND config_component.component_instance_id
              = component.component_instance_id
        )
      )
  ) THEN
    RAISE EXCEPTION 'baseline adoption release, provenance, or config lineage is incomplete'
      USING ERRCODE = '23514';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM ControlPlaneAuditEvent event
    WHERE event.organization_id = NEW.organization_id
      AND event.deployment_plan_id = NEW.deployment_plan_id
      AND event.event_type = 'baseline_adoption.adopted'
      AND event.outcome = 'ADOPTED'
      AND event.deployment_plan_checksum = NEW.plan_checksum
      AND event.product_release_checksum = NEW.product_release_checksum
      AND event.target_config_checksum = NEW.target_config_checksum
      AND event.payload ->> 'baselineAdoptionId' = NEW.id::TEXT
      AND event.payload ->> 'outcomeChecksum' = NEW.outcome_checksum
      AND event.payload ->> 'outcome' = 'ADOPTED'
      AND event.payload ->> 'deploymentPerformed' = 'false'
      AND event.payload ->> 'taskCount' = '0'
      AND event.payload ->> 'lockCount' = '0'
      AND event.payload ->> 'executionCount' = '0'
  ) THEN
    RAISE EXCEPTION 'baseline adoption audit event is required'
      USING ERRCODE = '23514';
  END IF;
  RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER BaselineAdoption_commit_guard
AFTER INSERT ON BaselineAdoption
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION baseline_adoption_commit_guard();

CREATE TRIGGER BaselineAdoption_append_only
BEFORE UPDATE OR DELETE ON BaselineAdoption
FOR EACH ROW EXECUTE FUNCTION desired_observed_append_only_guard();
CREATE TRIGGER BaselineAdoption_no_truncate
BEFORE TRUNCATE ON BaselineAdoption
FOR EACH STATEMENT EXECUTE FUNCTION desired_observed_append_only_guard();

CREATE TRIGGER BaselineAdoptionComponent_append_only
BEFORE UPDATE OR DELETE ON BaselineAdoptionComponent
FOR EACH ROW EXECUTE FUNCTION desired_observed_append_only_guard();
CREATE TRIGGER BaselineAdoptionComponent_no_truncate
BEFORE TRUNCATE ON BaselineAdoptionComponent
FOR EACH STATEMENT EXECUTE FUNCTION desired_observed_append_only_guard();
