ALTER TABLE ReleaseBundle
    ADD COLUMN published_by_user_account_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL,
    ADD COLUMN published_at TIMESTAMP;

CREATE TABLE ReleaseBundleAuditEvent (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
    release_bundle_id UUID NOT NULL REFERENCES ReleaseBundle(id) ON DELETE CASCADE,
    actor_user_account_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    from_status TEXT NOT NULL,
    to_status TEXT,
    reason TEXT NOT NULL DEFAULT '',
    CONSTRAINT releasebundleauditevent_type_check CHECK (
        event_type IN ('published', 'blocked', 'archived', 'state_transition_rejected')
    )
);

CREATE INDEX releasebundleauditevent_bundle_created_idx
    ON ReleaseBundleAuditEvent (organization_id, release_bundle_id, created_at, id);
