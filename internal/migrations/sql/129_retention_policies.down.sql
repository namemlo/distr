DROP INDEX IF EXISTS ReleaseBundle_retention_protected;
DROP INDEX IF EXISTS RetentionCleanupJob_organization_created;
DROP INDEX IF EXISTS RetentionPolicy_organization_created;

DROP TABLE IF EXISTS RetentionCleanupJob;

ALTER TABLE ReleaseBundle
  DROP COLUMN retention_protected;

DROP TABLE IF EXISTS RetentionPolicy;
