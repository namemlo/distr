DROP INDEX IF EXISTS DeploymentPreflightCheck_run_sort;
DROP INDEX IF EXISTS DeploymentPreflightRun_plan_created;

DROP TABLE IF EXISTS DeploymentPreflightCheck;
DROP TABLE IF EXISTS DeploymentPreflightRun;

DELETE FROM TaskResourceLock
WHERE resource_type = 'target_component';

ALTER TABLE TaskResourceLock
  DROP CONSTRAINT IF EXISTS taskresourcelock_resource_type_check;

ALTER TABLE TaskResourceLock
  ADD CONSTRAINT taskresourcelock_resource_type_check
  CHECK (resource_type IN (
    'deployment_target',
    'tenant_environment',
    'application_environment',
    'custom'
  ));
