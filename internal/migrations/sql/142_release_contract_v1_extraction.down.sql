LOCK TABLE
  ReleaseContractV1ExtractionLineage,
  BackfillCheckpointSourceMembership,
  BackfillCheckpoint
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM ReleaseContractV1ExtractionLineage
    LIMIT 1
  )
  OR EXISTS (
    SELECT 1
    FROM BackfillCheckpointSourceMembership
    LIMIT 1
  )
  OR EXISTS (
    SELECT 1
    FROM BackfillCheckpoint
    LIMIT 1
  ) THEN
    RAISE EXCEPTION
      'downgrade crossing 142 is forbidden while v1 extraction evidence exists';
  END IF;
END;
$$;

DROP TRIGGER ReleaseContractV1ExtractionLineage_immutable
  ON ReleaseContractV1ExtractionLineage;
DROP TRIGGER BackfillCheckpointSourceMembership_immutable
  ON BackfillCheckpointSourceMembership;
DROP TRIGGER BackfillCheckpoint_immutable
  ON BackfillCheckpoint;
DROP FUNCTION release_contract_v1_extraction_reject_mutation();

DROP TABLE ReleaseContractV1ExtractionLineage;
DROP TABLE BackfillCheckpointSourceMembership;
DROP TABLE BackfillCheckpoint;

ALTER TABLE TargetConfigSnapshot
  DROP CONSTRAINT targetconfigsnapshot_id_organization_checksum_unique;

ALTER TABLE DeploymentPlan
  DROP CONSTRAINT deploymentplan_id_organization_checksum_unique;

ALTER TABLE ReleaseBundle
  DROP CONSTRAINT releasebundle_id_organization_checksum_unique;
