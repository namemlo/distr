CREATE TABLE ReleaseBundle (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now(),
    organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
    application_id UUID NOT NULL REFERENCES Application(id) ON DELETE RESTRICT,
    channel_id UUID NOT NULL REFERENCES Channel(id) ON DELETE RESTRICT,
    release_number TEXT NOT NULL,
    release_notes TEXT NOT NULL DEFAULT '',
    source_revision TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT', 'VALIDATING', 'PUBLISHED', 'BLOCKED', 'ARCHIVED')),
    canonical_checksum TEXT NOT NULL,
    canonical_payload JSONB NOT NULL,
    CONSTRAINT releasebundle_organization_application_number_unique UNIQUE (organization_id, application_id, release_number)
);

CREATE INDEX releasebundle_organization_application_idx ON ReleaseBundle (organization_id, application_id);
CREATE INDEX releasebundle_channel_idx ON ReleaseBundle (channel_id);

CREATE TABLE ReleaseBundleComponent (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_bundle_id UUID NOT NULL REFERENCES ReleaseBundle(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    component_type TEXT NOT NULL CHECK (component_type IN ('application_version', 'oci_image', 'oci_artifact', 'helm_chart', 'child_release_bundle', 'external_artifact')),
    version TEXT NOT NULL DEFAULT '',
    application_version_id UUID REFERENCES ApplicationVersion(id) ON DELETE RESTRICT,
    package_ref TEXT NOT NULL DEFAULT '',
    digest TEXT NOT NULL DEFAULT '',
    checksum TEXT NOT NULL DEFAULT '',
    child_release_bundle_id UUID REFERENCES ReleaseBundle(id) ON DELETE RESTRICT,
    CONSTRAINT releasebundlecomponent_bundle_key_unique UNIQUE (release_bundle_id, key)
);

CREATE INDEX releasebundlecomponent_application_version_idx ON ReleaseBundleComponent (application_version_id);
CREATE INDEX releasebundlecomponent_child_release_bundle_idx ON ReleaseBundleComponent (child_release_bundle_id);
