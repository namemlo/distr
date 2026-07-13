ALTER TABLE ExternalExecution
  DROP CONSTRAINT IF EXISTS externalexecution_compose_identity_complete,
  DROP COLUMN IF EXISTS expected_compose_checksum,
  DROP COLUMN IF EXISTS expected_compose_reference;
