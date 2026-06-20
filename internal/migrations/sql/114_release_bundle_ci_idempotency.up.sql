ALTER TABLE ReleaseBundle
    ADD COLUMN source_repository TEXT NOT NULL DEFAULT '',
    ADD COLUMN source_branch TEXT NOT NULL DEFAULT '',
    ADD COLUMN source_tag TEXT NOT NULL DEFAULT '',
    ADD COLUMN ci_provider TEXT NOT NULL DEFAULT '',
    ADD COLUMN ci_run_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN ci_run_url TEXT NOT NULL DEFAULT '';

CREATE TABLE ReleaseBundleIdempotencyKey (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
    key_hash TEXT NOT NULL,
    request_checksum TEXT NOT NULL,
    release_bundle_id UUID NOT NULL REFERENCES ReleaseBundle(id) ON DELETE RESTRICT,
    CONSTRAINT releasebundleidempotencykey_organization_key_unique UNIQUE (organization_id, key_hash)
);

CREATE INDEX releasebundleidempotencykey_release_bundle_idx
    ON ReleaseBundleIdempotencyKey (organization_id, release_bundle_id);
