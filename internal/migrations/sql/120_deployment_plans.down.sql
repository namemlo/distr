DROP INDEX IF EXISTS DeploymentPlanIssue_plan_sort;
DROP INDEX IF EXISTS DeploymentPlanVariable_plan_key;
DROP INDEX IF EXISTS DeploymentPlanStep_plan_sort;
DROP INDEX IF EXISTS DeploymentPlanTarget_plan_sort;
DROP INDEX IF EXISTS DeploymentPlan_release_bundle;
DROP INDEX IF EXISTS DeploymentPlan_organization_created;

DROP TABLE IF EXISTS DeploymentPlanIssue;
DROP TABLE IF EXISTS DeploymentPlanVariable;
DROP TABLE IF EXISTS DeploymentPlanStep;
DROP TABLE IF EXISTS DeploymentPlanTarget;
DROP TABLE IF EXISTS DeploymentPlan;
