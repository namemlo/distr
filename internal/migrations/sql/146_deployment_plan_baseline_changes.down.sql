DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM DeploymentPlanBaseline)
     OR EXISTS (SELECT 1 FROM DeploymentPlanChangeEntry)
     OR EXISTS (SELECT 1 FROM DeploymentPlanRiskEntry)
     OR EXISTS (
       SELECT 1
       FROM TargetComponentObservation
       WHERE component_instance_id IS NOT NULL
     )
     OR EXISTS (
       SELECT 1
       FROM DeploymentPlan
       WHERE plan_schema = 'distr.target-deployment-plan/v2'
         AND bootstrap
     ) THEN
    RAISE EXCEPTION
      'refusing migration 146 rollback while deployment plan change evidence exists';
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS DeploymentPlan_bootstrap_immutable_guard
  ON DeploymentPlan;
DROP FUNCTION IF EXISTS deployment_plan_bootstrap_immutable_guard();

DROP TRIGGER IF EXISTS DeploymentPlanRiskEntry_append_only
  ON DeploymentPlanRiskEntry;
DROP TRIGGER IF EXISTS DeploymentPlanRiskEntry_no_truncate
  ON DeploymentPlanRiskEntry;
DROP TRIGGER IF EXISTS DeploymentPlanChangeEntry_append_only
  ON DeploymentPlanChangeEntry;
DROP TRIGGER IF EXISTS DeploymentPlanChangeEntry_no_truncate
  ON DeploymentPlanChangeEntry;
DROP TRIGGER IF EXISTS DeploymentPlanBaseline_append_only
  ON DeploymentPlanBaseline;
DROP TRIGGER IF EXISTS DeploymentPlanBaseline_no_truncate
  ON DeploymentPlanBaseline;
DROP FUNCTION IF EXISTS deployment_plan_change_evidence_append_only_guard();

DROP INDEX IF EXISTS DeploymentPlanRiskEntry_plan_order;
DROP TABLE IF EXISTS DeploymentPlanRiskEntry;
DROP INDEX IF EXISTS DeploymentPlanChangeEntry_plan_order;
DROP TABLE IF EXISTS DeploymentPlanChangeEntry;
DROP INDEX IF EXISTS DeploymentPlanBaseline_plan_order;
DROP TABLE IF EXISTS DeploymentPlanBaseline;

DROP INDEX IF EXISTS DeploymentPlan_previous_state_unique;

DROP INDEX IF EXISTS TargetComponentObservation_instance_history;
ALTER TABLE TargetComponentObservation
  DROP CONSTRAINT IF EXISTS targetcomponentobservation_instance_fk,
  DROP COLUMN IF EXISTS component_instance_id;

ALTER TABLE DeploymentPlan
  DROP CONSTRAINT IF EXISTS deploymentplan_previous_state_shape_check,
  DROP CONSTRAINT IF EXISTS deploymentplan_previous_state_source_fk,
  DROP COLUMN IF EXISTS previous_state_source_plan_id,
  DROP COLUMN IF EXISTS bootstrap;
