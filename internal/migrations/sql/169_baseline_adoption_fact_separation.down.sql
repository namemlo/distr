SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

LOCK TABLE BaselineAdoption, BaselineAdoptionComponent
  IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM BaselineAdoptionComponent component
    WHERE component.application_version IS DISTINCT FROM component.schema_version
       OR component.component_release_checksum
            IS DISTINCT FROM component.capability_checksum
  ) THEN
    RAISE EXCEPTION
      'refusing migration 169 rollback: separated baseline adoption facts exist'
      USING ERRCODE = '23514';
  END IF;
END;
$$;

DROP TRIGGER BaselineAdoption_commit_guard ON BaselineAdoption;

CREATE CONSTRAINT TRIGGER BaselineAdoption_commit_guard
AFTER INSERT ON BaselineAdoption
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION baseline_adoption_commit_guard();

DROP FUNCTION baseline_adoption_commit_guard_v2();

ALTER TABLE BaselineAdoptionComponent
  DROP CONSTRAINT baselineadoptioncomponent_application_version_check,
  DROP COLUMN application_version;
