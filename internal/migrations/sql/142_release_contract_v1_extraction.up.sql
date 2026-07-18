ALTER TABLE ReleaseBundle
  ADD CONSTRAINT releasebundle_id_organization_checksum_unique
  UNIQUE (id, organization_id, canonical_checksum);

ALTER TABLE DeploymentPlan
  ADD CONSTRAINT deploymentplan_id_organization_checksum_unique
  UNIQUE (id, organization_id, canonical_checksum);

ALTER TABLE TargetConfigSnapshot
  ADD CONSTRAINT targetconfigsnapshot_id_organization_checksum_unique
  UNIQUE (id, organization_id, canonical_checksum);

CREATE TABLE BackfillCheckpoint (
  id UUID PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  actor_user_account_id UUID NOT NULL,
  extractor_version TEXT NOT NULL CHECK (
    extractor_version ~ '^[a-z0-9][a-z0-9._/-]{0,127}$'
  ),
  source_state_checksum TEXT NOT NULL CHECK (
    source_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  dry_run_checksum TEXT NOT NULL CHECK (
    dry_run_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  source_after_plan_id UUID,
  source_through_plan_id UUID,
  has_more BOOLEAN NOT NULL DEFAULT false,
  source_count INTEGER NOT NULL CHECK (source_count >= 0),
  candidate_count INTEGER NOT NULL CHECK (candidate_count >= 0),
  blocked_count INTEGER NOT NULL CHECK (blocked_count >= 0),
  batch_size INTEGER NOT NULL CHECK (batch_size BETWEEN 1 AND 1000),
  CONSTRAINT backfillcheckpoint_count_check CHECK (
    source_count = candidate_count + blocked_count
  ),
  CONSTRAINT backfillcheckpoint_batch_bound_check CHECK (
    source_count <= batch_size
  ),
  CONSTRAINT backfillcheckpoint_cursor_check CHECK (
    (
      source_count = 0
      AND source_through_plan_id IS NULL
      AND has_more = false
    )
    OR (
      source_count > 0
      AND source_through_plan_id IS NOT NULL
      AND (
        source_after_plan_id IS NULL
        OR source_through_plan_id > source_after_plan_id
      )
    )
  ),
  CONSTRAINT backfillcheckpoint_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT backfillcheckpoint_actor_organization_fk
    FOREIGN KEY (organization_id, actor_user_account_id)
    REFERENCES Organization_UserAccount(organization_id, user_account_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT backfillcheckpoint_dry_run_unique
    UNIQUE (organization_id, extractor_version, dry_run_checksum)
);

CREATE INDEX BackfillCheckpoint_organization_created
  ON BackfillCheckpoint (organization_id, created_at DESC, id DESC);

CREATE TABLE ReleaseContractV1ExtractionLineage (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  checkpoint_id UUID NOT NULL,
  original_release_bundle_id UUID NOT NULL,
  original_release_checksum TEXT NOT NULL CHECK (
    original_release_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  original_plan_id UUID NOT NULL,
  original_plan_checksum TEXT NOT NULL CHECK (
    original_plan_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  derived_snapshot_id UUID,
  derived_snapshot_checksum TEXT NOT NULL DEFAULT '' CHECK (
    derived_snapshot_checksum = ''
    OR derived_snapshot_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  extractor_version TEXT NOT NULL CHECK (
    extractor_version ~ '^[a-z0-9][a-z0-9._/-]{0,127}$'
  ),
  status TEXT NOT NULL CHECK (
    status IN ('candidate', 'applied', 'blocked')
  ),
  blocked_reason_code TEXT NOT NULL DEFAULT '' CHECK (
    blocked_reason_code = ''
    OR (
      length(blocked_reason_code) <= 64
      AND blocked_reason_code ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
    )
  ),
  CONSTRAINT releasecontractv1extractionlineage_checkpoint_fk
    FOREIGN KEY (checkpoint_id, organization_id)
    REFERENCES BackfillCheckpoint(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT releasecontractv1extractionlineage_release_fk
    FOREIGN KEY (
      original_release_bundle_id,
      organization_id,
      original_release_checksum
    )
    REFERENCES ReleaseBundle(id, organization_id, canonical_checksum)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT releasecontractv1extractionlineage_plan_fk
    FOREIGN KEY (
      original_plan_id,
      organization_id,
      original_plan_checksum
    )
    REFERENCES DeploymentPlan(id, organization_id, canonical_checksum)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT releasecontractv1extractionlineage_snapshot_fk
    FOREIGN KEY (
      derived_snapshot_id,
      organization_id,
      derived_snapshot_checksum
    )
    REFERENCES TargetConfigSnapshot(id, organization_id, canonical_checksum)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT releasecontractv1extractionlineage_status_check CHECK (
    (
      status = 'candidate'
      AND derived_snapshot_id IS NULL
      AND derived_snapshot_checksum <> ''
      AND blocked_reason_code = ''
    )
    OR (
      status = 'applied'
      AND derived_snapshot_id IS NOT NULL
      AND derived_snapshot_checksum <> ''
      AND blocked_reason_code = ''
    )
    OR (
      status = 'blocked'
      AND derived_snapshot_id IS NULL
      AND derived_snapshot_checksum = ''
      AND blocked_reason_code <> ''
    )
  ),
  CONSTRAINT releasecontractv1extractionlineage_source_status_unique
    UNIQUE (
      checkpoint_id,
      organization_id,
      original_plan_id,
      status
    )
);

CREATE INDEX ReleaseContractV1ExtractionLineage_checkpoint_order
  ON ReleaseContractV1ExtractionLineage (
    organization_id,
    checkpoint_id,
    original_plan_id,
    status,
    id
  );

CREATE INDEX ReleaseContractV1ExtractionLineage_release
  ON ReleaseContractV1ExtractionLineage (
    organization_id,
    original_release_bundle_id,
    original_plan_id
  );

CREATE FUNCTION release_contract_v1_extraction_reject_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.target_config_snapshot_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;

  RAISE EXCEPTION 'v1 extraction checkpoints and lineage are immutable'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER BackfillCheckpoint_immutable
BEFORE UPDATE OR DELETE ON BackfillCheckpoint
FOR EACH ROW EXECUTE FUNCTION release_contract_v1_extraction_reject_mutation();

CREATE TRIGGER ReleaseContractV1ExtractionLineage_immutable
BEFORE UPDATE OR DELETE ON ReleaseContractV1ExtractionLineage
FOR EACH ROW EXECUTE FUNCTION release_contract_v1_extraction_reject_mutation();
