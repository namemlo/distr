DROP INDEX IF EXISTS TargetComponentObservation_history;
DROP INDEX IF EXISTS TargetComponentState_target_application;
DROP INDEX IF EXISTS DeploymentPlanTargetComponent_plan_sort;

DROP TABLE IF EXISTS TargetComponentObservation;
DROP TABLE IF EXISTS TargetComponentState;
DROP TABLE IF EXISTS DeploymentPlanTargetComponent;

ALTER TABLE DeploymentPlanTarget
  DROP COLUMN IF EXISTS platform;

ALTER TABLE DeploymentTarget
  DROP COLUMN IF EXISTS platform;
