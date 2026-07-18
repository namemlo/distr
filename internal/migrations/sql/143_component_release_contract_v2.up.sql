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

CREATE INDEX releasebundle_contract_v2_backfill_cursor_idx
  ON ReleaseBundle (organization_id, created_at, id)
  WHERE kind = 'legacy'
    AND release_contract_schema = 'distr.release/v1'
    AND status IN ('PUBLISHED', 'BLOCKED', 'ARCHIVED');

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
  CONSTRAINT componentreleaseartifact_verification_identity_unique
    UNIQUE (
      release_bundle_id,
      artifact_key,
      platform,
      platform_digest
    ),
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

CREATE TABLE ComponentReleaseEvidenceVerification (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  release_bundle_id UUID NOT NULL,
  artifact_key TEXT NOT NULL,
  platform TEXT NOT NULL,
  artifact_digest TEXT NOT NULL,
  evidence_reference TEXT NOT NULL,
  evidence_digest TEXT NOT NULL,
  policy_checksum TEXT NOT NULL,
  trust_root_id TEXT NOT NULL,
  predicate_type TEXT NOT NULL,
  builder_id TEXT NOT NULL,
  source_uri TEXT NOT NULL,
  build_type TEXT NOT NULL,
  external_parameters_checksum TEXT NOT NULL,
  signer_issuer TEXT NOT NULL,
  signer_identity TEXT NOT NULL,
  verified_at TIMESTAMPTZ NOT NULL,
  CONSTRAINT componentreleaseverification_bundle_fk
    FOREIGN KEY (release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT componentreleaseverification_artifact_fk
    FOREIGN KEY (
      release_bundle_id,
      artifact_key,
      platform,
      artifact_digest
    )
    REFERENCES ComponentReleaseArtifact(
      release_bundle_id,
      artifact_key,
      platform,
      platform_digest
    )
    ON DELETE CASCADE,
  CONSTRAINT componentreleaseverification_identity_unique
    UNIQUE (
      release_bundle_id,
      artifact_key,
      platform
    ),
  CONSTRAINT componentreleaseverification_platform_check
    CHECK (platform IN ('linux/amd64', 'linux/arm64')),
  CONSTRAINT componentreleaseverification_artifact_digest_check
    CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$'),
  CONSTRAINT componentreleaseverification_evidence_digest_check
    CHECK (evidence_digest ~ '^sha256:[0-9a-f]{64}$'),
  CONSTRAINT componentreleaseverification_policy_checksum_check
    CHECK (policy_checksum ~ '^sha256:[0-9a-f]{64}$'),
  CONSTRAINT componentreleaseverification_external_parameters_checksum_check
    CHECK (external_parameters_checksum ~ '^sha256:[0-9a-f]{64}$'),
  CONSTRAINT componentreleaseverification_text_bounds_check
    CHECK (
      artifact_key = btrim(artifact_key)
      AND length(artifact_key) BETWEEN 1 AND 128
      AND evidence_reference = btrim(evidence_reference)
      AND length(evidence_reference) BETWEEN 1 AND 2048
      AND trust_root_id = btrim(trust_root_id)
      AND length(trust_root_id) BETWEEN 1 AND 256
      AND predicate_type = btrim(predicate_type)
      AND length(predicate_type) BETWEEN 1 AND 1024
      AND builder_id = btrim(builder_id)
      AND length(builder_id) BETWEEN 1 AND 1024
      AND source_uri = btrim(source_uri)
      AND length(source_uri) BETWEEN 1 AND 2048
      AND build_type = btrim(build_type)
      AND length(build_type) BETWEEN 1 AND 1024
      AND signer_issuer = btrim(signer_issuer)
      AND length(signer_issuer) BETWEEN 1 AND 1024
      AND signer_identity = btrim(signer_identity)
      AND length(signer_identity) BETWEEN 1 AND 1024
      AND artifact_key !~ '[[:cntrl:]]'
      AND evidence_reference !~ '[[:cntrl:]]'
      AND trust_root_id !~ '[[:cntrl:]]'
      AND predicate_type !~ '[[:cntrl:]]'
      AND builder_id !~ '[[:cntrl:]]'
      AND source_uri !~ '[[:cntrl:]]'
      AND build_type !~ '[[:cntrl:]]'
      AND signer_issuer !~ '[[:cntrl:]]'
      AND signer_identity !~ '[[:cntrl:]]'
    )
);

CREATE INDEX componentreleaseverification_preflight_idx
  ON ComponentReleaseEvidenceVerification (
    organization_id,
    release_bundle_id,
    artifact_key,
    platform,
    artifact_digest,
    policy_checksum
  );

CREATE FUNCTION component_release_verification_insert_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  PERFORM 1
  FROM ReleaseBundle
  WHERE id = NEW.release_bundle_id
    AND organization_id = NEW.organization_id
    AND kind = 'component'
    AND status = 'DRAFT'
  FOR KEY SHARE;

  IF NOT FOUND OR NOT EXISTS (
    SELECT 1
    FROM ComponentReleaseEvidence
    WHERE release_bundle_id = NEW.release_bundle_id
      AND organization_id = NEW.organization_id
      AND evidence_type = 'provenance'
      AND reference = NEW.evidence_reference
  ) THEN
    RAISE EXCEPTION
      'component release verification requires a draft component and declared provenance'
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER ComponentReleaseEvidenceVerification_insert_guard
BEFORE INSERT ON ComponentReleaseEvidenceVerification
FOR EACH ROW EXECUTE FUNCTION component_release_verification_insert_guard();

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

CREATE TABLE ReleaseContractV2BackfillLineage (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  checkpoint_id UUID NOT NULL,
  source_release_bundle_id UUID NOT NULL,
  source_canonical_checksum TEXT NOT NULL,
  derived_release_bundle_id UUID,
  derived_canonical_checksum TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  reason_code TEXT NOT NULL DEFAULT '',
  CONSTRAINT releasecontractv2backfill_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id)
    ON DELETE CASCADE,
  CONSTRAINT releasecontractv2backfill_source_fk
    FOREIGN KEY (source_release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    DEFERRABLE INITIALLY DEFERRED,
  CONSTRAINT releasecontractv2backfill_derived_fk
    FOREIGN KEY (derived_release_bundle_id, organization_id)
    REFERENCES ReleaseBundle(id, organization_id)
    DEFERRABLE INITIALLY DEFERRED,
  CONSTRAINT releasecontractv2backfill_source_unique
    UNIQUE (organization_id, source_release_bundle_id),
  CONSTRAINT releasecontractv2backfill_state_check
    CHECK (state IN ('derived', 'blocked')),
  CONSTRAINT releasecontractv2backfill_state_shape_check
    CHECK (
      (
        state = 'derived'
        AND derived_release_bundle_id IS NOT NULL
        AND derived_canonical_checksum ~ '^sha256:[0-9a-f]{64}$'
        AND reason_code = ''
      )
      OR (
        state = 'blocked'
        AND derived_release_bundle_id IS NULL
        AND length(derived_canonical_checksum) = 0
        AND reason_code ~ '^[a-z][a-z0-9_]{0,63}$'
      )
    ),
  CONSTRAINT releasecontractv2backfill_source_checksum_check
    CHECK (source_canonical_checksum ~ '^sha256:[0-9a-f]{64}$')
);

CREATE UNIQUE INDEX releasecontractv2backfill_derived_unique
  ON ReleaseContractV2BackfillLineage (
    organization_id,
    derived_release_bundle_id
  )
  WHERE derived_release_bundle_id IS NOT NULL;

CREATE INDEX releasecontractv2backfill_checkpoint_idx
  ON ReleaseContractV2BackfillLineage (
    organization_id,
    checkpoint_id,
    created_at,
    source_release_bundle_id
  );

CREATE FUNCTION release_contract_v2_evidence_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.release_evidence_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;

  RAISE EXCEPTION 'component release verification and backfill lineage are append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER ComponentReleaseEvidenceVerification_append_only
BEFORE UPDATE OR DELETE ON ComponentReleaseEvidenceVerification
FOR EACH ROW EXECUTE FUNCTION release_contract_v2_evidence_append_only();

CREATE TRIGGER ComponentReleaseEvidenceVerification_no_truncate
BEFORE TRUNCATE ON ComponentReleaseEvidenceVerification
FOR EACH STATEMENT EXECUTE FUNCTION release_contract_v2_evidence_append_only();

CREATE TRIGGER ReleaseContractV2BackfillLineage_append_only
BEFORE UPDATE OR DELETE ON ReleaseContractV2BackfillLineage
FOR EACH ROW EXECUTE FUNCTION release_contract_v2_evidence_append_only();

CREATE TRIGGER ReleaseContractV2BackfillLineage_no_truncate
BEFORE TRUNCATE ON ReleaseContractV2BackfillLineage
FOR EACH STATEMENT EXECUTE FUNCTION release_contract_v2_evidence_append_only();
