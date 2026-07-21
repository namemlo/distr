LOCK TABLE
  DeploymentPlanStep,
  AdapterImplementation,
  AdapterCapability,
  AdapterAssignment,
  DeploymentPlanStepAdapter
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM DeploymentPlanStepAdapter)
     OR EXISTS (SELECT 1 FROM AdapterAssignment)
     OR EXISTS (SELECT 1 FROM AdapterCapability)
     OR EXISTS (SELECT 1 FROM AdapterImplementation) THEN
    RAISE EXCEPTION
      'refusing migration 156 rollback while adapter catalog, assignment, or frozen plan evidence exists';
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS DeploymentPlanStepAdapter_no_truncate
  ON DeploymentPlanStepAdapter;
DROP TRIGGER IF EXISTS DeploymentPlanStepAdapter_append_only
  ON DeploymentPlanStepAdapter;
DROP FUNCTION IF EXISTS deployment_plan_step_adapter_append_only_guard();

DROP INDEX IF EXISTS DeploymentPlanStepAdapter_plan_order;
DROP TABLE IF EXISTS DeploymentPlanStepAdapter;
ALTER TABLE DeploymentPlanStep
  DROP CONSTRAINT IF EXISTS deploymentplanstep_id_plan_organization_unique;
DROP INDEX IF EXISTS AdapterAssignment_resolution;
DROP TABLE IF EXISTS AdapterAssignment;
DROP INDEX IF EXISTS AdapterCapability_resolution;
DROP TABLE IF EXISTS AdapterCapability;
DROP INDEX IF EXISTS AdapterImplementation_organization_key;
DROP TABLE IF EXISTS AdapterImplementation;
