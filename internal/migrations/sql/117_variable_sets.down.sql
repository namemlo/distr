DROP TABLE IF EXISTS Variable;
DROP TABLE IF EXISTS VariableSetApplication;
DROP TABLE IF EXISTS VariableSet;

ALTER TABLE Secret
  DROP CONSTRAINT IF EXISTS secret_id_organization_unique;
