DROP TABLE IF EXISTS ReleaseBundleComponent;
DROP TABLE IF EXISTS ReleaseBundle;
ALTER TABLE Channel DROP CONSTRAINT IF EXISTS channel_id_application_organization_unique;
