DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM RoleDefinition
    WHERE NOT built_in
  ) OR EXISTS (
    SELECT 1
    FROM RoleBinding
  ) OR EXISTS (
    SELECT 1
    FROM PrincipalGroup
  ) OR EXISTS (
    SELECT 1
    FROM ControlPlaneEnrollment
  ) THEN
    RAISE EXCEPTION
      'refusing migration 148 rollback while scoped authorization or enrollment evidence exists';
  END IF;
END;
$$;

DROP TABLE AuthorizationBackfillCheckpoint;
DROP TABLE ControlPlaneEnrollment;
DROP TABLE RoleBinding;
DROP TABLE PrincipalGroupMember;
DROP TABLE PrincipalGroup;
DROP TABLE RolePermission;
DROP TABLE RoleDefinition;
DROP FUNCTION authorization_prevent_immutable_mutation();
DROP FUNCTION authorization_validate_enrollment_boundary();
DROP FUNCTION authorization_validate_role_binding_boundary();
DROP FUNCTION authorization_validate_membership_at_write();
