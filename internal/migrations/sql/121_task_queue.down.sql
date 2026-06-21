DROP INDEX IF EXISTS StepRun_organization_status;
DROP INDEX IF EXISTS StepRun_task_sort;
DROP INDEX IF EXISTS Task_deployment_plan;
DROP INDEX IF EXISTS Task_organization_queue;

DROP TABLE IF EXISTS StepRun;
DROP TABLE IF EXISTS Task;

ALTER TABLE DeploymentPlanStep
  DROP CONSTRAINT IF EXISTS deploymentplanstep_plan_id_organization_unique;

ALTER TABLE DeploymentPlanTarget
  DROP CONSTRAINT IF EXISTS deploymentplantarget_plan_id_organization_unique;

DROP SEQUENCE IF EXISTS Task_queue_order_seq;

