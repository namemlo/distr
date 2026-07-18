LOCK TABLE
  TargetConfigSnapshot,
  TargetConfigSnapshotObject,
  TargetConfigSnapshotComponent,
  TargetConfigSnapshotSecretReference,
  TargetConfigSnapshotFeatureFlag
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM TargetConfigSnapshot LIMIT 1)
     OR EXISTS (SELECT 1 FROM TargetConfigSnapshotObject LIMIT 1)
     OR EXISTS (SELECT 1 FROM TargetConfigSnapshotComponent LIMIT 1)
     OR EXISTS (SELECT 1 FROM TargetConfigSnapshotSecretReference LIMIT 1)
     OR EXISTS (SELECT 1 FROM TargetConfigSnapshotFeatureFlag LIMIT 1) THEN
    RAISE EXCEPTION
      'downgrade crossing 141 is forbidden while target config snapshots exist';
  END IF;
END;
$$;

DROP TRIGGER TargetConfigSnapshotFeatureFlag_immutable
  ON TargetConfigSnapshotFeatureFlag;
DROP TRIGGER TargetConfigSnapshotSecretReference_immutable
  ON TargetConfigSnapshotSecretReference;
DROP TRIGGER TargetConfigSnapshotComponent_immutable
  ON TargetConfigSnapshotComponent;
DROP TRIGGER TargetConfigSnapshotObject_immutable
  ON TargetConfigSnapshotObject;
DROP TRIGGER TargetConfigSnapshot_immutable
  ON TargetConfigSnapshot;
DROP FUNCTION target_config_snapshot_reject_mutation();

DROP TABLE TargetConfigSnapshotFeatureFlag;
DROP TABLE TargetConfigSnapshotSecretReference;
DROP TABLE TargetConfigSnapshotComponent;
DROP TABLE TargetConfigSnapshotObject;
DROP TABLE TargetConfigSnapshot;

ALTER TABLE ComponentInstance
  DROP CONSTRAINT componentinstance_id_unit_organization_unique;

ALTER TABLE DeploymentUnit
  DROP CONSTRAINT deploymentunit_id_assignment_organization_unique;

ALTER TABLE TargetEnvironmentAssignment
  DROP CONSTRAINT targetenvironmentassignment_id_environment_organization_unique;
