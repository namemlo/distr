LOCK TABLE
  ComponentReleaseMigrationDeclaration,
  ComponentReleaseCapability,
  ComponentReleaseEvidence,
  ComponentReleaseArtifact,
  ReleaseBundle
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM ReleaseBundle
    WHERE kind <> 'legacy'
       OR release_contract_schema <> 'distr.release/v1'
  )
  OR EXISTS (SELECT 1 FROM ComponentReleaseArtifact)
  OR EXISTS (SELECT 1 FROM ComponentReleaseEvidence)
  OR EXISTS (SELECT 1 FROM ComponentReleaseCapability)
  OR EXISTS (SELECT 1 FROM ComponentReleaseMigrationDeclaration) THEN
    RAISE EXCEPTION
      'downgrade crossing 143 is forbidden while component or product release facts exist';
  END IF;
END;
$$;

DROP TABLE ComponentReleaseMigrationDeclaration;
DROP TABLE ComponentReleaseCapability;
DROP TABLE ComponentReleaseEvidence;
DROP TABLE ComponentReleaseArtifact;

ALTER TABLE ReleaseBundle
  DROP CONSTRAINT releasebundle_contract_schema_check,
  DROP CONSTRAINT releasebundle_kind_check,
  DROP COLUMN release_contract_schema,
  DROP COLUMN kind;
