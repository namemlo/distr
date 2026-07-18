LOCK TABLE
  ComponentInstanceRename,
  ComponentInstance,
  ComponentAlias,
  ComponentDefinition,
  DeploymentUnitSubscriber,
  DeploymentUnit,
  TargetEnvironmentAssignment,
  DeploymentScope
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM ComponentInstanceRename)
     OR EXISTS (SELECT 1 FROM ComponentInstance)
     OR EXISTS (SELECT 1 FROM ComponentAlias)
     OR EXISTS (SELECT 1 FROM ComponentDefinition)
     OR EXISTS (SELECT 1 FROM DeploymentUnitSubscriber)
     OR EXISTS (SELECT 1 FROM DeploymentUnit)
     OR EXISTS (SELECT 1 FROM TargetEnvironmentAssignment)
     OR EXISTS (SELECT 1 FROM DeploymentScope) THEN
    RAISE EXCEPTION
      'downgrade crossing 139 is forbidden while deployment registry rows exist';
  END IF;
END;
$$;

DROP TRIGGER ComponentAlias_rename_history_guard
  ON ComponentAlias;
DROP FUNCTION component_alias_rename_history_guard();

DROP TRIGGER ComponentInstanceRename_append_only
  ON ComponentInstanceRename;
DROP FUNCTION component_instance_rename_append_only();
DROP TABLE ComponentInstanceRename;

DROP TABLE ComponentInstance;
DROP TABLE ComponentAlias;
DROP TABLE ComponentDefinition;

DROP TRIGGER DeploymentUnitSubscriber_set_matches
  ON DeploymentUnitSubscriber;
DROP FUNCTION deployment_unit_subscriber_set_from_member_constraint();

DROP TRIGGER DeploymentUnitSubscriber_mutation_guard
  ON DeploymentUnitSubscriber;
DROP FUNCTION deployment_unit_subscriber_set_mutation_guard();

DROP TABLE DeploymentUnitSubscriber;

DROP TRIGGER DeploymentUnit_subscriber_set_matches
  ON DeploymentUnit;
DROP FUNCTION deployment_unit_subscriber_set_from_unit_constraint();
DROP FUNCTION deployment_unit_validate_subscriber_set(UUID, UUID);
DROP FUNCTION deployment_unit_subscriber_set_checksum(UUID, UUID);

DROP TRIGGER DeploymentUnit_subscriber_checksum_immutable
  ON DeploymentUnit;
DROP FUNCTION deployment_unit_subscriber_checksum_immutable();

DROP TABLE DeploymentUnit;

DROP TRIGGER TargetEnvironmentAssignment_prevent_overlap
  ON TargetEnvironmentAssignment;
DROP FUNCTION target_environment_assignment_prevent_overlap();

DROP TABLE TargetEnvironmentAssignment;
DROP TABLE DeploymentScope;

DROP FUNCTION deployment_registry_normalize_physical_name();
DROP FUNCTION deployment_registry_normalize_alias();
DROP FUNCTION deployment_registry_normalize_physical_identity();
DROP FUNCTION deployment_registry_normalize_name();
