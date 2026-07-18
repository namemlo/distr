LOCK TABLE
  ReleaseContractV2BackfillLineage,
  ReleaseContractV2BackfillCheckpoint,
  ComponentReleaseEvidenceVerification,
  ComponentReleaseMigrationDeclaration,
  ComponentReleaseCapability,
  ComponentReleaseEvidence,
  ComponentReleaseArtifact,
  ReleaseBundle
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM ReleaseBundle
    WHERE kind <> 'legacy'
       OR release_contract_schema <> 'distr.release/v1'
  )
  OR EXISTS (SELECT 1 FROM ComponentReleaseArtifact)
  OR EXISTS (SELECT 1 FROM ComponentReleaseEvidence)
  OR EXISTS (SELECT 1 FROM ComponentReleaseEvidenceVerification)
  OR EXISTS (SELECT 1 FROM ComponentReleaseCapability)
  OR EXISTS (SELECT 1 FROM ComponentReleaseMigrationDeclaration)
  OR EXISTS (SELECT 1 FROM ReleaseContractV2BackfillLineage)
  OR EXISTS (SELECT 1 FROM ReleaseContractV2BackfillCheckpoint) THEN
    RAISE EXCEPTION
      'downgrade crossing 143 is forbidden while component or product release facts exist';
  END IF;
END;
$$;

DROP TRIGGER ReleaseContractV2BackfillLineage_append_only
  ON ReleaseContractV2BackfillLineage;
DROP TRIGGER ReleaseContractV2BackfillLineage_no_truncate
  ON ReleaseContractV2BackfillLineage;
DROP TRIGGER ReleaseContractV2BackfillCheckpoint_append_only
  ON ReleaseContractV2BackfillCheckpoint;
DROP TRIGGER ReleaseContractV2BackfillCheckpoint_no_truncate
  ON ReleaseContractV2BackfillCheckpoint;
DROP TRIGGER ComponentReleaseEvidenceVerification_append_only
  ON ComponentReleaseEvidenceVerification;
DROP TRIGGER ComponentReleaseEvidenceVerification_no_truncate
  ON ComponentReleaseEvidenceVerification;
DROP TRIGGER ComponentReleaseEvidenceVerification_insert_guard
  ON ComponentReleaseEvidenceVerification;
DROP FUNCTION release_contract_v2_evidence_append_only();
DROP FUNCTION component_release_verification_insert_guard();

DROP TABLE ReleaseContractV2BackfillLineage;
DROP TABLE ReleaseContractV2BackfillCheckpoint;
DROP TABLE ComponentReleaseMigrationDeclaration;
DROP TABLE ComponentReleaseCapability;
DROP TABLE ComponentReleaseEvidenceVerification;
DROP TABLE ComponentReleaseEvidence;
DROP TABLE ComponentReleaseArtifact;

DROP INDEX releasebundle_contract_v2_backfill_cursor_idx;

ALTER TABLE ReleaseBundle
  DROP CONSTRAINT releasebundle_contract_schema_check,
  DROP CONSTRAINT releasebundle_kind_check,
  DROP COLUMN release_contract_schema,
  DROP COLUMN kind;
