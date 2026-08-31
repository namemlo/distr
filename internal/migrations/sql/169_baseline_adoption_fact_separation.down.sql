LOCK TABLE BaselineAdoption, BaselineAdoptionComponent
  IN SHARE ROW EXCLUSIVE MODE;

DROP TRIGGER BaselineAdoption_commit_guard ON BaselineAdoption;

CREATE CONSTRAINT TRIGGER BaselineAdoption_commit_guard
AFTER INSERT ON BaselineAdoption
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION baseline_adoption_commit_guard();

DROP FUNCTION baseline_adoption_commit_guard_v2();

ALTER TABLE BaselineAdoptionComponent
  DROP CONSTRAINT baselineadoptioncomponent_application_version_check,
  DROP COLUMN application_version;
