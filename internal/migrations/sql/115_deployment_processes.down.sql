DROP TABLE IF EXISTS DeploymentProcessStepEnvironment;
DROP TABLE IF EXISTS DeploymentProcessStepChannel;
DROP TABLE IF EXISTS DeploymentProcessStepDependency;
DROP TABLE IF EXISTS DeploymentProcessStep;
DROP TABLE IF EXISTS DeploymentProcessRevision;
DROP TABLE IF EXISTS DeploymentProcess;
ALTER TABLE Application DROP CONSTRAINT IF EXISTS application_id_organization_unique;
