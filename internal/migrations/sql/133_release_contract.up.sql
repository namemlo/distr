ALTER TABLE ReleaseBundle
  ADD COLUMN release_contract JSONB;

ALTER TABLE ReleaseBundle
  ADD CONSTRAINT releasebundle_release_contract_object
  CHECK (release_contract IS NULL OR jsonb_typeof(release_contract) = 'object');

ALTER TABLE DeploymentPlan
  ADD COLUMN release_contract JSONB;

ALTER TABLE DeploymentPlan
  ADD CONSTRAINT deploymentplan_release_contract_object
  CHECK (release_contract IS NULL OR jsonb_typeof(release_contract) = 'object');
