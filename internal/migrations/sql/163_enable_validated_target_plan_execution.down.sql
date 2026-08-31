LOCK TABLE DeploymentPlan IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM DeploymentPlan
    WHERE plan_schema = 'distr.target-deployment-plan/v2'
      AND status <> 'BLOCKED'
  ) THEN
    RAISE EXCEPTION
      'refusing migration 163 rollback: executable target deployment plans exist';
  END IF;
END
$$;

CREATE OR REPLACE FUNCTION deployment_plan_v2_immutable_guard()
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
  IF OLD.plan_schema <> 'distr.target-deployment-plan/v2'
     AND NEW.plan_schema = 'distr.target-deployment-plan/v2' THEN
    RAISE EXCEPTION 'legacy deployment plans cannot become target plans'
      USING ERRCODE = '23514';
  END IF;
  IF OLD.plan_schema <> 'distr.target-deployment-plan/v2' THEN
    RETURN NEW;
  END IF;
  IF NEW.status <> 'BLOCKED' THEN
    RAISE EXCEPTION 'target deployment plans remain BLOCKED until execution enablement'
      USING ERRCODE = '23514';
  END IF;
  IF OLD.sealed_at IS NOT NULL AND NEW IS DISTINCT FROM OLD THEN
    RAISE EXCEPTION 'published target deployment plan is immutable'
      USING ERRCODE = '23514';
  END IF;
  IF OLD.sealed_at IS NULL
     AND (
       NEW.sealed_at IS NULL
       OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.release_bundle_id IS DISTINCT FROM OLD.release_bundle_id
       OR NEW.application_id IS DISTINCT FROM OLD.application_id
       OR NEW.channel_id IS DISTINCT FROM OLD.channel_id
       OR NEW.environment_id IS DISTINCT FROM OLD.environment_id
       OR NEW.process_snapshot_id IS DISTINCT FROM OLD.process_snapshot_id
       OR NEW.variable_snapshot_id IS DISTINCT FROM OLD.variable_snapshot_id
       OR NEW.release_contract IS DISTINCT FROM OLD.release_contract
       OR NEW.published_by_user_account_id IS DISTINCT FROM OLD.published_by_user_account_id
       OR NEW.canonical_checksum IS DISTINCT FROM OLD.canonical_checksum
       OR NEW.canonical_payload IS DISTINCT FROM OLD.canonical_payload
       OR NEW.plan_schema IS DISTINCT FROM OLD.plan_schema
       OR NEW.draft_id IS DISTINCT FROM OLD.draft_id
       OR NEW.deployment_unit_id IS DISTINCT FROM OLD.deployment_unit_id
       OR NEW.target_config_snapshot_id IS DISTINCT FROM OLD.target_config_snapshot_id
       OR NEW.protocol_version IS DISTINCT FROM OLD.protocol_version
       OR NEW.supersedes_deployment_plan_id IS DISTINCT FROM OLD.supersedes_deployment_plan_id
       OR NEW.supersede_reason IS DISTINCT FROM OLD.supersede_reason
       OR NEW.status IS DISTINCT FROM OLD.status
     ) THEN
    RAISE EXCEPTION 'target deployment plan may only transition atomically to sealed'
      USING ERRCODE = '23514';
  END IF;
  IF OLD.sealed_at IS NULL AND NEW.sealed_at IS NOT NULL THEN
    IF (
      SELECT count(*)
      FROM DeploymentPlanTarget target
      WHERE target.deployment_plan_id = NEW.id
        AND target.organization_id = NEW.organization_id
    ) <> 1 THEN
      RAISE EXCEPTION 'target deployment plan must seal exactly one target'
        USING ERRCODE = '23514';
    END IF;
    IF (
      SELECT count(*)
      FROM DeploymentPlanStep step
      WHERE step.deployment_plan_id = NEW.id
        AND step.organization_id = NEW.organization_id
    ) NOT BETWEEN 1 AND 4096 THEN
      RAISE EXCEPTION 'target deployment plan step set is incomplete or oversized'
        USING ERRCODE = '23514';
    END IF;
    IF (
      SELECT count(*)
      FROM DeploymentPlanResolvedRequirement requirement
      WHERE requirement.deployment_plan_id = NEW.id
        AND requirement.organization_id = NEW.organization_id
    ) > 1024 THEN
      RAISE EXCEPTION 'target deployment plan requirement set is oversized'
        USING ERRCODE = '23514';
    END IF;
    IF (
      SELECT count(*)
      FROM DeploymentPlanStepEdge edge
      WHERE edge.deployment_plan_id = NEW.id
        AND edge.organization_id = NEW.organization_id
    ) > 8192 THEN
      RAISE EXCEPTION 'target deployment plan edge set is oversized'
        USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
      SELECT 1
      FROM DeploymentPlanIssue issue
      WHERE issue.deployment_plan_id = NEW.id
        AND issue.organization_id = NEW.organization_id
        AND issue.severity = 'blocker'
        AND issue.code = 'target_plan_execution_deferred'
    ) THEN
      RAISE EXCEPTION 'target deployment plan execution blocker is required'
        USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
      SELECT 1
      FROM DeploymentPlanDraftAuditEvent event
      WHERE event.deployment_plan_draft_id = NEW.draft_id
        AND event.organization_id = NEW.organization_id
        AND event.event_type = 'PUBLISHED'
        AND event.published_deployment_plan_id = NEW.id
        AND event.actor_user_account_id = NEW.published_by_user_account_id
    ) THEN
      RAISE EXCEPTION 'target deployment plan publication audit event is required'
        USING ERRCODE = '23514';
    END IF;
    IF NEW.supersedes_deployment_plan_id IS NOT NULL THEN
      IF NOT EXISTS (
        SELECT 1
        FROM DeploymentPlan predecessor
        JOIN DeploymentPlanTarget predecessor_target
          ON predecessor_target.deployment_plan_id = predecessor.id
         AND predecessor_target.organization_id = predecessor.organization_id
        JOIN DeploymentPlanTarget successor_target
          ON successor_target.deployment_plan_id = NEW.id
         AND successor_target.organization_id = NEW.organization_id
        WHERE predecessor.id = NEW.supersedes_deployment_plan_id
          AND predecessor.organization_id = NEW.organization_id
          AND predecessor.plan_schema = 'distr.target-deployment-plan/v2'
          AND predecessor.sealed_at IS NOT NULL
          AND predecessor.deployment_unit_id = NEW.deployment_unit_id
          AND predecessor.environment_id = NEW.environment_id
          AND predecessor.application_id = NEW.application_id
          AND predecessor_target.deployment_target_id
            = successor_target.deployment_target_id
      ) THEN
        RAISE EXCEPTION
          'supersession must preserve unit, environment, application, and target'
          USING ERRCODE = '23514';
      END IF;
    END IF;
  END IF;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION deployment_plan_v2_sealed_commit_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  persisted_sealed_at TIMESTAMPTZ;
  persisted_status TEXT;
BEGIN
  IF NEW.plan_schema <> 'distr.target-deployment-plan/v2' THEN
    RETURN NULL;
  END IF;
  SELECT sealed_at, status
  INTO persisted_sealed_at, persisted_status
  FROM DeploymentPlan
  WHERE id = NEW.id
    AND organization_id = NEW.organization_id;
  IF persisted_sealed_at IS NULL OR persisted_status <> 'BLOCKED' THEN
    RAISE EXCEPTION 'target deployment plan must commit sealed and BLOCKED'
      USING ERRCODE = '23514';
  END IF;
  RETURN NULL;
END;
$$;
