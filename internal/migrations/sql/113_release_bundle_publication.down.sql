DROP TABLE IF EXISTS ReleaseBundleAuditEvent;

ALTER TABLE ReleaseBundle
    DROP COLUMN published_by_user_account_id,
    DROP COLUMN published_at;
