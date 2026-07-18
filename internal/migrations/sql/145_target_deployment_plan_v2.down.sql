DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM DeploymentPlanDraft)
     OR EXISTS (
       SELECT 1
       FROM DeploymentPlan
       WHERE plan_schema = 'distr.target-deployment-plan/v2'
     ) THEN
    RAISE EXCEPTION
      'refusing migration 145 rollback while target deployment plan v2 evidence exists';
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS DeploymentPlanStepEdge_append_only
  ON DeploymentPlanStepEdge;
DROP TRIGGER IF EXISTS DeploymentPlanStepEdge_no_truncate
  ON DeploymentPlanStepEdge;
DROP TRIGGER IF EXISTS DeploymentPlanResolvedRequirement_append_only
  ON DeploymentPlanResolvedRequirement;
DROP TRIGGER IF EXISTS DeploymentPlanResolvedRequirement_no_truncate
  ON DeploymentPlanResolvedRequirement;
DROP FUNCTION IF EXISTS deployment_plan_v2_append_only_guard();

DROP TRIGGER IF EXISTS DeploymentPlan_v2_immutable_guard ON DeploymentPlan;
DROP FUNCTION IF EXISTS deployment_plan_v2_immutable_guard();

DROP TRIGGER IF EXISTS DeploymentPlanDraft_publication_guard
  ON DeploymentPlanDraft;
DROP FUNCTION IF EXISTS deployment_plan_draft_publication_guard();

DROP INDEX IF EXISTS DeploymentPlanStepEdge_plan;
DROP TABLE IF EXISTS DeploymentPlanStepEdge;

DROP INDEX IF EXISTS DeploymentPlanResolvedRequirement_plan_order;
DROP TABLE IF EXISTS DeploymentPlanResolvedRequirement;

DROP INDEX IF EXISTS DeploymentPlan_v2_placement;
DROP INDEX IF EXISTS DeploymentPlan_v2_draft_unique;

ALTER TABLE DeploymentPlan
  DROP CONSTRAINT IF EXISTS deploymentplan_v2_shape_check,
  DROP CONSTRAINT IF EXISTS deploymentplan_supersedes_fk,
  DROP CONSTRAINT IF EXISTS deploymentplan_config_unit_fk,
  DROP CONSTRAINT IF EXISTS deploymentplan_unit_fk,
  DROP CONSTRAINT IF EXISTS deploymentplan_draft_fk,
  DROP CONSTRAINT IF EXISTS deploymentplan_protocol_version_check,
  DROP CONSTRAINT IF EXISTS deploymentplan_plan_schema_check,
  DROP COLUMN IF EXISTS supersede_reason,
  DROP COLUMN IF EXISTS supersedes_deployment_plan_id,
  DROP COLUMN IF EXISTS protocol_version,
  DROP COLUMN IF EXISTS target_config_snapshot_id,
  DROP COLUMN IF EXISTS deployment_unit_id,
  DROP COLUMN IF EXISTS draft_id,
  DROP COLUMN IF EXISTS plan_schema;

DROP INDEX IF EXISTS DeploymentPlanDraft_placement;
DROP INDEX IF EXISTS DeploymentPlanDraft_organization_updated;
DROP TABLE IF EXISTS DeploymentPlanDraft;

ALTER TABLE DeploymentPlanStep
  DROP CONSTRAINT IF EXISTS deploymentplanstep_plan_key_organization_unique;

ALTER TABLE TargetComponentObservation
  DROP CONSTRAINT IF EXISTS targetcomponentobservation_id_organization_unique;
