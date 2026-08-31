SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE DeploymentPlan
  DROP CONSTRAINT deploymentplan_v2_shape_check,
  ADD CONSTRAINT deploymentplan_v2_shape_check CHECK (
    (
      plan_schema = 'distr.deployment-plan/v1'
      AND draft_id IS NULL
      AND deployment_unit_id IS NULL
      AND target_config_snapshot_id IS NULL
      AND supersedes_deployment_plan_id IS NULL
      AND supersede_reason = ''
      AND protocol_version = 'v1'
      AND published_by_user_account_id IS NULL
      AND sealed_at IS NULL
    )
    OR (
      plan_schema = 'distr.target-deployment-plan/v2'
      AND draft_id IS NOT NULL
      AND deployment_unit_id IS NOT NULL
      AND target_config_snapshot_id IS NOT NULL
      AND published_by_user_account_id IS NOT NULL
      AND status IN ('BLOCKED', 'READY', 'EXECUTED')
      AND canonical_checksum ~ '^sha256:[0-9a-f]{64}$'
      AND octet_length(canonical_payload) BETWEEN 2 AND 4194304
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

  IF TG_OP = 'INSERT' THEN
    IF NEW.plan_schema = 'distr.target-deployment-plan/v2'
       AND NEW.sealed_at IS NOT NULL THEN
      RAISE EXCEPTION 'target deployment plan must be inserted unsealed'
        USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
  END IF;

  IF OLD.plan_schema <> 'distr.target-deployment-plan/v2'
     AND NEW.plan_schema = 'distr.target-deployment-plan/v2' THEN
    RAISE EXCEPTION 'legacy deployment plans cannot become target plans'
      USING ERRCODE = '23514';
  END IF;
  IF OLD.plan_schema <> 'distr.target-deployment-plan/v2' THEN
    RETURN NEW;
  END IF;

  IF OLD.sealed_at IS NOT NULL THEN
    IF OLD.status = 'READY'
       AND NEW.status = 'EXECUTED'
       AND (to_jsonb(NEW) - 'status') = (to_jsonb(OLD) - 'status') THEN
      RETURN NEW;
    END IF;
    IF NEW IS DISTINCT FROM OLD THEN
      RAISE EXCEPTION 'published target deployment plan is immutable'
        USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.sealed_at IS NULL
     OR NEW.status <> 'READY'
     OR (to_jsonb(NEW) - 'sealed_at') IS DISTINCT FROM
        (to_jsonb(OLD) - 'sealed_at') THEN
    RAISE EXCEPTION 'target deployment plan may only transition atomically to sealed and READY'
      USING ERRCODE = '23514';
  END IF;

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
  IF EXISTS (
    SELECT 1
    FROM DeploymentPlanIssue issue
    WHERE issue.deployment_plan_id = NEW.id
      AND issue.organization_id = NEW.organization_id
      AND issue.severity = 'blocker'
  ) THEN
    RAISE EXCEPTION 'target deployment plan cannot seal with blocker issues'
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
  RETURN NEW;
END;
$$;

DROP TRIGGER DeploymentPlan_v2_immutable_guard ON DeploymentPlan;

CREATE TRIGGER DeploymentPlan_v2_immutable_guard
BEFORE INSERT OR UPDATE OR DELETE ON DeploymentPlan
FOR EACH ROW EXECUTE FUNCTION deployment_plan_v2_immutable_guard();

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
  IF persisted_sealed_at IS NULL
     OR persisted_status NOT IN ('READY', 'EXECUTED') THEN
    RAISE EXCEPTION 'target deployment plan must commit sealed and executable'
      USING ERRCODE = '23514';
  END IF;
  RETURN NULL;
END;
$$;
