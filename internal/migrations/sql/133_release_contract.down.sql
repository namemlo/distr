ALTER TABLE ReleaseBundle
  DROP CONSTRAINT IF EXISTS releasebundle_release_contract_object;

ALTER TABLE DeploymentPlan
  DROP CONSTRAINT IF EXISTS deploymentplan_release_contract_object;

ALTER TABLE DeploymentPlan
  DROP COLUMN IF EXISTS release_contract;

ALTER TABLE ReleaseBundle
  DROP COLUMN IF EXISTS release_contract;
