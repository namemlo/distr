ALTER TABLE TargetComponentObservation
  DROP CONSTRAINT IF EXISTS targetcomponentobservation_external_execution_fk;

UPDATE TargetComponentObservation
SET external_execution_id = NULL
WHERE external_execution_id IS NOT NULL;

DROP TABLE IF EXISTS ExternalExecutionEvent;
DROP TABLE IF EXISTS ExternalExecution;

ALTER TABLE ReleaseBundle
  DROP CONSTRAINT IF EXISTS releasebundle_id_organization_unique;

ALTER TABLE TargetComponentObservation
  DROP COLUMN IF EXISTS config_reference;

ALTER TABLE TargetComponentState
  DROP COLUMN IF EXISTS config_reference;
