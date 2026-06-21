DROP TABLE IF EXISTS VariableScopedValue;

ALTER TABLE CustomerOrganization
  DROP CONSTRAINT IF EXISTS customerorganization_id_organization_unique;

ALTER TABLE DeploymentTarget
  DROP CONSTRAINT IF EXISTS deploymenttarget_id_organization_unique;

ALTER TABLE Channel
  DROP CONSTRAINT IF EXISTS channel_id_organization_unique;

ALTER TABLE Environment
  DROP CONSTRAINT IF EXISTS environment_id_organization_unique;

ALTER TABLE Variable
  DROP CONSTRAINT IF EXISTS variable_id_set_organization_unique;
