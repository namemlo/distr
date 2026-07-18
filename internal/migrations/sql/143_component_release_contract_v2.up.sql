ALTER TABLE ReleaseBundle
  ADD COLUMN kind TEXT NOT NULL DEFAULT 'legacy',
  ADD COLUMN release_contract_schema TEXT NOT NULL DEFAULT 'distr.release/v1',
  ADD CONSTRAINT releasebundle_kind_check
    CHECK (kind IN ('legacy', 'component', 'product')),
  ADD CONSTRAINT releasebundle_contract_schema_check
    CHECK (
      (kind = 'legacy' AND release_contract_schema = 'distr.release/v1')
      OR (
        kind = 'component'
        AND release_contract_schema = 'distr.component-release/v2'
      )
      OR (
        kind = 'product'
        AND release_contract_schema = 'distr.product-release/v1'
      )
    );

CREATE TABLE ComponentReleaseArtifact (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  release_bundle_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  component_key TEXT NOT NULL,
  component_version TEXT NOT NULL,
  artifact_key TEXT NOT NULL,
  artifact_type TEXT NOT NULL,
  media_type TEXT NOT NULL,
  manifest_digest TEXT NOT NULL,
  platform TEXT NOT NULL,
  platform_digest TEXT NOT NULL,
  CONSTRAINT componentreleaseartifact_bundle_fk
    FOREIGN KEY (release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT componentreleaseartifact_bundle_artifact_platform_unique
    UNIQUE (release_bundle_id, artifact_key, platform),
  CONSTRAINT componentreleaseartifact_kind_check
    CHECK (artifact_type IN ('oci-image', 'oci-artifact', 'helm-chart')),
  CONSTRAINT componentreleaseartifact_media_type_check
    CHECK (
      (
        artifact_type = 'oci-image'
        AND media_type IN (
          'application/vnd.oci.image.index.v1+json',
          'application/vnd.oci.image.manifest.v1+json'
        )
      )
      OR (
        artifact_type = 'oci-artifact'
        AND media_type = 'application/vnd.oci.artifact.manifest.v1+json'
      )
      OR (
        artifact_type = 'helm-chart'
        AND media_type = 'application/vnd.cncf.helm.chart.content.v1.tar+gzip'
      )
    ),
  CONSTRAINT componentreleaseartifact_platform_check
    CHECK (platform IN ('linux/amd64', 'linux/arm64')),
  CONSTRAINT componentreleaseartifact_manifest_digest_check
    CHECK (manifest_digest ~ '^sha256:[0-9a-f]{64}$'),
  CONSTRAINT componentreleaseartifact_platform_digest_check
    CHECK (platform_digest ~ '^sha256:[0-9a-f]{64}$')
);

CREATE INDEX componentreleaseartifact_identity_idx
  ON ComponentReleaseArtifact (
    organization_id,
    component_key,
    component_version,
    platform,
    platform_digest
  );

CREATE TABLE ComponentReleaseEvidence (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  release_bundle_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  evidence_type TEXT NOT NULL,
  reference TEXT NOT NULL,
  CONSTRAINT componentreleaseevidence_bundle_fk
    FOREIGN KEY (release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT componentreleaseevidence_bundle_type_reference_unique
    UNIQUE (release_bundle_id, evidence_type, reference),
  CONSTRAINT componentreleaseevidence_type_check
    CHECK (evidence_type IN ('provenance', 'sbom', 'signature', 'test'))
);

CREATE INDEX componentreleaseevidence_bundle_idx
  ON ComponentReleaseEvidence (organization_id, release_bundle_id, evidence_type);

CREATE TABLE ComponentReleaseCapability (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  release_bundle_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  direction TEXT NOT NULL,
  name TEXT NOT NULL,
  version_or_range TEXT NOT NULL,
  resolution_stage TEXT NOT NULL DEFAULT '',
  allowed_modes TEXT[] NOT NULL DEFAULT '{}',
  CONSTRAINT componentreleasecapability_bundle_fk
    FOREIGN KEY (release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT componentreleasecapability_bundle_direction_name_unique
    UNIQUE (release_bundle_id, direction, name),
  CONSTRAINT componentreleasecapability_direction_check
    CHECK (direction IN ('provides', 'requires')),
  CONSTRAINT componentreleasecapability_stage_check
    CHECK (resolution_stage IN ('', 'product', 'target'))
);

CREATE INDEX componentreleasecapability_bundle_idx
  ON ComponentReleaseCapability (organization_id, release_bundle_id, direction);

CREATE TABLE ComponentReleaseMigrationDeclaration (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  release_bundle_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  key TEXT NOT NULL,
  migration_type TEXT NOT NULL,
  sort_order INTEGER NOT NULL,
  compatibility TEXT NOT NULL,
  failure_policy TEXT NOT NULL,
  description TEXT NOT NULL,
  CONSTRAINT componentreleasemigration_bundle_fk
    FOREIGN KEY (release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT componentreleasemigration_bundle_key_unique
    UNIQUE (release_bundle_id, key),
  CONSTRAINT componentreleasemigration_bundle_order_unique
    UNIQUE (release_bundle_id, sort_order),
  CONSTRAINT componentreleasemigration_order_check
    CHECK (sort_order > 0),
  CONSTRAINT componentreleasemigration_type_check
    CHECK (migration_type IN ('database', 'data', 'runtime')),
  CONSTRAINT componentreleasemigration_compatibility_check
    CHECK (
      compatibility IN (
        'backward-compatible',
        'forward-compatible',
        'breaking'
      )
    ),
  CONSTRAINT componentreleasemigration_failure_policy_check
    CHECK (failure_policy IN ('stop', 'retry', 'forward-fix'))
);

CREATE INDEX componentreleasemigration_bundle_idx
  ON ComponentReleaseMigrationDeclaration (
    organization_id,
    release_bundle_id,
    sort_order
  );
