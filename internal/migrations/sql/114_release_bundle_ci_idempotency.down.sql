DROP TABLE IF EXISTS ReleaseBundleIdempotencyKey;

ALTER TABLE ReleaseBundle
    DROP COLUMN IF EXISTS source_repository,
    DROP COLUMN IF EXISTS source_branch,
    DROP COLUMN IF EXISTS source_tag,
    DROP COLUMN IF EXISTS ci_provider,
    DROP COLUMN IF EXISTS ci_run_id,
    DROP COLUMN IF EXISTS ci_run_url;

