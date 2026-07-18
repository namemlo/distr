LOCK TABLE RoleDefinition IN ACCESS EXCLUSIVE MODE;
LOCK TABLE RolePermission IN ACCESS EXCLUSIVE MODE;
LOCK TABLE RoleBinding IN ACCESS EXCLUSIVE MODE;
LOCK TABLE RoleBindingRevision IN ACCESS EXCLUSIVE MODE;
LOCK TABLE PrincipalGroup IN ACCESS EXCLUSIVE MODE;
LOCK TABLE PrincipalGroupMember IN ACCESS EXCLUSIVE MODE;
LOCK TABLE PrincipalGroupMemberRevision IN ACCESS EXCLUSIVE MODE;
LOCK TABLE ControlPlaneEnrollment IN ACCESS EXCLUSIVE MODE;
LOCK TABLE AuthorizationBackfillCheckpoint IN ACCESS EXCLUSIVE MODE;

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
DROP TABLE PrincipalGroupMemberRevision;
DROP TABLE RoleBindingRevision;
DROP TABLE RoleBinding;
DROP TABLE PrincipalGroupMember;
DROP TABLE PrincipalGroup;
DROP TABLE RolePermission;
DROP TABLE RoleDefinition;
DROP FUNCTION authorization_prevent_immutable_mutation();
DROP FUNCTION authorization_validate_enrollment_boundary();
DROP FUNCTION authorization_validate_role_binding_boundary();
DROP FUNCTION authorization_validate_group_member_boundary();
DROP FUNCTION authorization_validate_membership_at_write();
