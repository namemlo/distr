CREATE TABLE RoleDefinition (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  role_key TEXT NOT NULL CHECK (
    role_key = lower(btrim(role_key))
    AND role_key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
  ),
  display_name TEXT NOT NULL CHECK (
    display_name = btrim(display_name) AND length(display_name) > 0
  ),
  description TEXT NOT NULL DEFAULT '',
  built_in BOOLEAN NOT NULL DEFAULT false,
  source_legacy_role USER_ROLE,
  revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
  created_by_useraccount_id UUID,
  CONSTRAINT roledefinition_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT roledefinition_organization_key_unique
    UNIQUE (organization_id, role_key),
  CONSTRAINT roledefinition_legacy_role_unique
    UNIQUE (organization_id, source_legacy_role),
  CONSTRAINT roledefinition_builtin_source_check CHECK (
    built_in = (source_legacy_role IS NOT NULL)
  ),
  CONSTRAINT roledefinition_reserved_key_check CHECK (
    (
      built_in
      AND (
        (role_key = 'legacy.read_only' AND source_legacy_role = 'read_only')
        OR
        (role_key = 'legacy.read_write' AND source_legacy_role = 'read_write')
        OR
        (role_key = 'legacy.admin' AND source_legacy_role = 'admin')
      )
    )
    OR
    (
      NOT built_in
      AND role_key NOT IN (
        'legacy.read_only',
        'legacy.read_write',
        'legacy.admin'
      )
    )
  ),
  CONSTRAINT roledefinition_actor_fk
    FOREIGN KEY (created_by_useraccount_id)
    REFERENCES UserAccount(id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX RoleDefinition_organization_order
  ON RoleDefinition (organization_id, built_in DESC, role_key, id);

CREATE TABLE RolePermission (
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  role_definition_id UUID NOT NULL,
  action TEXT NOT NULL CHECK (
    action IN (
      'release.create',
      'release.publish',
      'release.block',
      'registry.manage',
      'config.manage',
      'plan.create',
      'plan.publish',
      'plan.execute',
      'approval.decide',
      'policy.manage',
      'calendar.manage',
      'freeze.manage',
      'emergency.override',
      'campaign.control',
      'observer.manage',
      'reconciliation.decide',
      'audit.view',
      'audit.export',
      'sample.retire',
      'authorization.manage'
    )
  ),
  PRIMARY KEY (role_definition_id, action),
  CONSTRAINT rolepermission_role_fk
    FOREIGN KEY (role_definition_id, organization_id)
    REFERENCES RoleDefinition(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX RolePermission_organization_action
  ON RolePermission (organization_id, action, role_definition_id);

CREATE TABLE PrincipalGroup (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  group_key TEXT NOT NULL CHECK (
    group_key = lower(btrim(group_key))
    AND group_key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
  ),
  display_name TEXT NOT NULL CHECK (
    display_name = btrim(display_name) AND length(display_name) > 0
  ),
  description TEXT NOT NULL DEFAULT '',
  created_by_useraccount_id UUID,
  CONSTRAINT principalgroup_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT principalgroup_organization_key_unique
    UNIQUE (organization_id, group_key),
  CONSTRAINT principalgroup_actor_fk
    FOREIGN KEY (created_by_useraccount_id)
    REFERENCES UserAccount(id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX PrincipalGroup_organization_order
  ON PrincipalGroup (organization_id, group_key, id);

CREATE TABLE PrincipalGroupMember (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  group_id UUID NOT NULL,
  user_account_id UUID NOT NULL,
  user_membership_created_at TIMESTAMP NOT NULL,
  effective_from TIMESTAMPTZ NOT NULL,
  effective_until TIMESTAMPTZ,
  added_by_useraccount_id UUID,
  reason TEXT NOT NULL CHECK (
    reason = btrim(reason) AND length(reason) > 0
  ),
  CONSTRAINT principalgroupmember_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT principalgroupmember_interval_check CHECK (
    effective_until IS NULL OR effective_until > effective_from
  ),
  CONSTRAINT principalgroupmember_identity_unique
    UNIQUE (organization_id, group_id, user_account_id, effective_from),
  CONSTRAINT principalgroupmember_group_fk
    FOREIGN KEY (group_id, organization_id)
    REFERENCES PrincipalGroup(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT principalgroupmember_user_fk
    FOREIGN KEY (user_account_id)
    REFERENCES UserAccount(id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT principalgroupmember_actor_fk
    FOREIGN KEY (added_by_useraccount_id)
    REFERENCES UserAccount(id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX PrincipalGroupMember_effective_lookup
  ON PrincipalGroupMember (
    organization_id,
    user_account_id,
    effective_from,
    effective_until,
    group_id
  );

CREATE TABLE RoleBinding (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  role_definition_id UUID NOT NULL,
  principal_kind TEXT NOT NULL CHECK (
    principal_kind IN ('user', 'group')
  ),
  principal_id UUID NOT NULL,
  principal_membership_created_at TIMESTAMP,
  scope_kind TEXT NOT NULL CHECK (
    scope_kind IN (
      'organization',
      'customer',
      'environment',
      'deployment_unit',
      'component',
      'campaign'
    )
  ),
  scope_id UUID NOT NULL,
  effective_from TIMESTAMPTZ NOT NULL,
  effective_until TIMESTAMPTZ,
  reason TEXT NOT NULL CHECK (
    reason = btrim(reason) AND length(reason) > 0
  ),
  revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
  created_by_useraccount_id UUID,
  source TEXT NOT NULL DEFAULT 'admin_api' CHECK (source = 'admin_api'),
  CONSTRAINT rolebinding_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT rolebinding_interval_check CHECK (
    effective_until IS NULL OR effective_until > effective_from
  ),
  CONSTRAINT rolebinding_identity_unique
    UNIQUE (
      organization_id,
      role_definition_id,
      principal_kind,
      principal_id,
      scope_kind,
      scope_id,
      effective_from
    ),
  CONSTRAINT rolebinding_organization_scope_check CHECK (
    scope_kind <> 'organization' OR scope_id = organization_id
  ),
  CONSTRAINT rolebinding_membership_created_at_check CHECK (
    (principal_kind = 'user' AND principal_membership_created_at IS NOT NULL)
    OR
    (principal_kind = 'group' AND principal_membership_created_at IS NULL)
  ),
  CONSTRAINT rolebinding_role_fk
    FOREIGN KEY (role_definition_id, organization_id)
    REFERENCES RoleDefinition(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT rolebinding_actor_fk
    FOREIGN KEY (created_by_useraccount_id)
    REFERENCES UserAccount(id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX RoleBinding_principal_effective_lookup
  ON RoleBinding (
    organization_id,
    principal_kind,
    principal_id,
    effective_from,
    effective_until,
    scope_kind,
    scope_id
  );

CREATE INDEX RoleBinding_role_lookup
  ON RoleBinding (organization_id, role_definition_id, id);

CREATE TABLE RoleBindingRevision (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  role_binding_id UUID NOT NULL,
  revision BIGINT NOT NULL CHECK (revision > 0),
  state TEXT NOT NULL CHECK (state IN ('active', 'revoked')),
  effective_from TIMESTAMPTZ NOT NULL,
  actor_useraccount_id UUID NOT NULL,
  reason TEXT NOT NULL CHECK (
    reason = btrim(reason) AND length(reason) > 0
  ),
  CONSTRAINT rolebindingrevision_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT rolebindingrevision_revision_unique
    UNIQUE (role_binding_id, revision),
  CONSTRAINT rolebindingrevision_binding_fk
    FOREIGN KEY (role_binding_id, organization_id)
    REFERENCES RoleBinding(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT rolebindingrevision_actor_fk
    FOREIGN KEY (actor_useraccount_id)
    REFERENCES UserAccount(id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX RoleBindingRevision_effective_lookup
  ON RoleBindingRevision (
    organization_id,
    role_binding_id,
    effective_from,
    revision DESC
  );

CREATE TABLE PrincipalGroupMemberRevision (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  principal_group_member_id UUID NOT NULL,
  revision BIGINT NOT NULL CHECK (revision > 0),
  state TEXT NOT NULL CHECK (state IN ('active', 'revoked')),
  effective_from TIMESTAMPTZ NOT NULL,
  actor_useraccount_id UUID NOT NULL,
  reason TEXT NOT NULL CHECK (
    reason = btrim(reason) AND length(reason) > 0
  ),
  CONSTRAINT principalgroupmemberrevision_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT principalgroupmemberrevision_revision_unique
    UNIQUE (principal_group_member_id, revision),
  CONSTRAINT principalgroupmemberrevision_member_fk
    FOREIGN KEY (principal_group_member_id, organization_id)
    REFERENCES PrincipalGroupMember(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT principalgroupmemberrevision_actor_fk
    FOREIGN KEY (actor_useraccount_id)
    REFERENCES UserAccount(id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX PrincipalGroupMemberRevision_effective_lookup
  ON PrincipalGroupMemberRevision (
    organization_id,
    principal_group_member_id,
    effective_from,
    revision DESC
  );

CREATE TABLE ControlPlaneEnrollment (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  scope_kind TEXT NOT NULL CHECK (
    scope_kind IN ('organization', 'environment')
  ),
  scope_id UUID NOT NULL,
  enabled BOOLEAN NOT NULL,
  effective_from TIMESTAMPTZ NOT NULL,
  effective_until TIMESTAMPTZ,
  actor_useraccount_id UUID NOT NULL,
  reason TEXT NOT NULL CHECK (
    reason = btrim(reason) AND length(reason) > 0
  ),
  revision BIGINT NOT NULL CHECK (revision > 0),
  CONSTRAINT controlplaneenrollment_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT controlplaneenrollment_revision_unique
    UNIQUE (organization_id, scope_kind, scope_id, revision),
  CONSTRAINT controlplaneenrollment_interval_check CHECK (
    effective_until IS NULL OR effective_until > effective_from
  ),
  CONSTRAINT controlplaneenrollment_organization_scope_check CHECK (
    scope_kind <> 'organization' OR scope_id = organization_id
  ),
  CONSTRAINT controlplaneenrollment_actor_fk
    FOREIGN KEY (actor_useraccount_id)
    REFERENCES UserAccount(id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX ControlPlaneEnrollment_effective_lookup
  ON ControlPlaneEnrollment (
    organization_id,
    scope_kind,
    scope_id,
    revision DESC,
    effective_from,
    effective_until
  );

CREATE TABLE AuthorizationBackfillCheckpoint (
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  checkpoint_key TEXT NOT NULL,
  completed BOOLEAN NOT NULL DEFAULT false,
  completed_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, checkpoint_key),
  CONSTRAINT authorizationbackfillcheckpoint_completed_check CHECK (
    completed = (completed_at IS NOT NULL)
  )
);

CREATE FUNCTION authorization_validate_membership_at_write()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  column_index INTEGER;
  candidate_text TEXT;
  candidate_id UUID;
BEGIN
  FOR column_index IN 0..(TG_NARGS - 1) LOOP
    candidate_text := to_jsonb(NEW) ->> TG_ARGV[column_index];
    IF candidate_text IS NULL THEN
      CONTINUE;
    END IF;
    candidate_id := candidate_text::UUID;
    IF NOT EXISTS (
      SELECT 1
      FROM Organization_UserAccount membership
      WHERE membership.organization_id = NEW.organization_id
        AND membership.user_account_id = candidate_id
    ) THEN
      RAISE EXCEPTION
        'authorization principal or actor is outside the organization'
        USING
          ERRCODE = '23503',
          CONSTRAINT = 'authorization_organization_membership_guard';
    END IF;
  END LOOP;
  RETURN NEW;
END;
$$;

CREATE TRIGGER RoleDefinition_validate_actor_membership
BEFORE INSERT ON RoleDefinition
FOR EACH ROW
EXECUTE FUNCTION authorization_validate_membership_at_write(
  'created_by_useraccount_id'
);

CREATE TRIGGER PrincipalGroup_validate_actor_membership
BEFORE INSERT ON PrincipalGroup
FOR EACH ROW
EXECUTE FUNCTION authorization_validate_membership_at_write(
  'created_by_useraccount_id'
);

CREATE TRIGGER PrincipalGroupMember_validate_membership
BEFORE INSERT ON PrincipalGroupMember
FOR EACH ROW
EXECUTE FUNCTION authorization_validate_membership_at_write(
  'user_account_id',
  'added_by_useraccount_id'
);

CREATE TRIGGER RoleBinding_validate_actor_membership
BEFORE INSERT ON RoleBinding
FOR EACH ROW
EXECUTE FUNCTION authorization_validate_membership_at_write(
  'created_by_useraccount_id'
);

CREATE TRIGGER RoleBindingRevision_validate_actor_membership
BEFORE INSERT ON RoleBindingRevision
FOR EACH ROW
EXECUTE FUNCTION authorization_validate_membership_at_write(
  'actor_useraccount_id'
);

CREATE TRIGGER PrincipalGroupMemberRevision_validate_actor_membership
BEFORE INSERT ON PrincipalGroupMemberRevision
FOR EACH ROW
EXECUTE FUNCTION authorization_validate_membership_at_write(
  'actor_useraccount_id'
);

CREATE TRIGGER ControlPlaneEnrollment_validate_actor_membership
BEFORE INSERT ON ControlPlaneEnrollment
FOR EACH ROW
EXECUTE FUNCTION authorization_validate_membership_at_write(
  'actor_useraccount_id'
);

CREATE FUNCTION authorization_validate_group_member_boundary()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM Organization_UserAccount membership
    WHERE membership.organization_id = NEW.organization_id
      AND membership.user_account_id = NEW.user_account_id
      AND membership.created_at = NEW.user_membership_created_at
  ) THEN
    RAISE EXCEPTION
      'authorization group member epoch is outside the organization'
      USING
        ERRCODE = '23503',
        CONSTRAINT = 'principalgroupmember_membership_created_at_guard';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER PrincipalGroupMember_validate_boundary
BEFORE INSERT ON PrincipalGroupMember
FOR EACH ROW
EXECUTE FUNCTION authorization_validate_group_member_boundary();

CREATE FUNCTION authorization_validate_role_binding_boundary()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.principal_kind = 'user' AND NOT EXISTS (
    SELECT 1
    FROM Organization_UserAccount membership
    WHERE membership.organization_id = NEW.organization_id
      AND membership.user_account_id = NEW.principal_id
      AND membership.created_at = NEW.principal_membership_created_at
  ) THEN
    RAISE EXCEPTION
      'authorization principal is outside the organization'
      USING
        ERRCODE = '23503',
        CONSTRAINT = 'rolebinding_principal_organization_guard';
  ELSIF NEW.principal_kind = 'group' AND NOT EXISTS (
    SELECT 1
    FROM PrincipalGroup principal_group
    WHERE principal_group.organization_id = NEW.organization_id
      AND principal_group.id = NEW.principal_id
  ) THEN
    RAISE EXCEPTION
      'authorization group is outside the organization'
      USING
        ERRCODE = '23503',
        CONSTRAINT = 'rolebinding_group_organization_guard';
  END IF;

  IF NEW.scope_kind = 'customer' AND NOT EXISTS (
    SELECT 1
    FROM CustomerOrganization customer
    WHERE customer.organization_id = NEW.organization_id
      AND customer.id = NEW.scope_id
  ) THEN
    RAISE EXCEPTION
      'authorization scope is outside the organization'
      USING ERRCODE = '23503', CONSTRAINT = 'rolebinding_customer_scope_guard';
  ELSIF NEW.scope_kind = 'environment' AND NOT EXISTS (
    SELECT 1
    FROM Environment environment
    WHERE environment.organization_id = NEW.organization_id
      AND environment.id = NEW.scope_id
  ) THEN
    RAISE EXCEPTION
      'authorization scope is outside the organization'
      USING ERRCODE = '23503', CONSTRAINT = 'rolebinding_environment_scope_guard';
  ELSIF NEW.scope_kind = 'deployment_unit' AND NOT EXISTS (
    SELECT 1
    FROM DeploymentUnit unit
    WHERE unit.organization_id = NEW.organization_id
      AND unit.id = NEW.scope_id
  ) THEN
    RAISE EXCEPTION
      'authorization scope is outside the organization'
      USING ERRCODE = '23503', CONSTRAINT = 'rolebinding_unit_scope_guard';
  ELSIF NEW.scope_kind = 'component' AND NOT EXISTS (
    SELECT 1
    FROM ComponentDefinition component
    WHERE component.organization_id = NEW.organization_id
      AND component.id = NEW.scope_id
  ) THEN
    RAISE EXCEPTION
      'authorization scope is outside the organization'
      USING ERRCODE = '23503', CONSTRAINT = 'rolebinding_component_scope_guard';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER RoleBinding_validate_boundary
BEFORE INSERT ON RoleBinding
FOR EACH ROW
EXECUTE FUNCTION authorization_validate_role_binding_boundary();

CREATE FUNCTION authorization_validate_enrollment_boundary()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.scope_kind = 'environment' AND NOT EXISTS (
    SELECT 1
    FROM Environment environment
    WHERE environment.organization_id = NEW.organization_id
      AND environment.id = NEW.scope_id
  ) THEN
    RAISE EXCEPTION
      'control-plane enrollment scope is outside the organization'
      USING
        ERRCODE = '23503',
        CONSTRAINT = 'controlplaneenrollment_environment_scope_guard';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER ControlPlaneEnrollment_validate_boundary
BEFORE INSERT ON ControlPlaneEnrollment
FOR EACH ROW
EXECUTE FUNCTION authorization_validate_enrollment_boundary();

CREATE FUNCTION authorization_prevent_immutable_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.deployment_registry_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION
    'scoped authorization evidence is immutable'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER RoleDefinition_immutable
BEFORE UPDATE OR DELETE ON RoleDefinition
FOR EACH ROW
EXECUTE FUNCTION authorization_prevent_immutable_mutation();

CREATE TRIGGER RolePermission_immutable
BEFORE UPDATE OR DELETE ON RolePermission
FOR EACH ROW
EXECUTE FUNCTION authorization_prevent_immutable_mutation();

CREATE TRIGGER RoleBinding_immutable
BEFORE UPDATE OR DELETE ON RoleBinding
FOR EACH ROW
EXECUTE FUNCTION authorization_prevent_immutable_mutation();

CREATE TRIGGER RoleBindingRevision_immutable
BEFORE UPDATE OR DELETE ON RoleBindingRevision
FOR EACH ROW
EXECUTE FUNCTION authorization_prevent_immutable_mutation();

CREATE TRIGGER PrincipalGroup_immutable
BEFORE UPDATE OR DELETE ON PrincipalGroup
FOR EACH ROW
EXECUTE FUNCTION authorization_prevent_immutable_mutation();

CREATE TRIGGER PrincipalGroupMember_immutable
BEFORE UPDATE OR DELETE ON PrincipalGroupMember
FOR EACH ROW
EXECUTE FUNCTION authorization_prevent_immutable_mutation();

CREATE TRIGGER PrincipalGroupMemberRevision_immutable
BEFORE UPDATE OR DELETE ON PrincipalGroupMemberRevision
FOR EACH ROW
EXECUTE FUNCTION authorization_prevent_immutable_mutation();

CREATE TRIGGER ControlPlaneEnrollment_immutable
BEFORE UPDATE OR DELETE ON ControlPlaneEnrollment
FOR EACH ROW
EXECUTE FUNCTION authorization_prevent_immutable_mutation();

INSERT INTO RoleDefinition (
  organization_id,
  role_key,
  display_name,
  description,
  built_in,
  source_legacy_role,
  revision
)
SELECT
  organization.id,
  role.role_key,
  role.display_name,
  role.description,
  true,
  role.legacy_role::USER_ROLE,
  1
FROM Organization organization
CROSS JOIN (
  VALUES
    ('legacy.read_only', 'Viewer', 'Built-in compatibility role for read-only users.', 'read_only'),
    ('legacy.read_write', 'Developer', 'Built-in compatibility role for read-write users.', 'read_write'),
    ('legacy.admin', 'Administrator', 'Built-in compatibility role for administrators.', 'admin')
) AS role(role_key, display_name, description, legacy_role)
ON CONFLICT (organization_id, role_key) DO NOTHING;

INSERT INTO RolePermission (
  organization_id,
  role_definition_id,
  action
)
SELECT
  definition.organization_id,
  definition.id,
  permission.action
FROM RoleDefinition definition
JOIN (
  VALUES
    ('read_only', 'audit.view'),
    ('read_only', 'audit.export'),
    ('read_write', 'release.create'),
    ('read_write', 'release.publish'),
    ('read_write', 'registry.manage'),
    ('read_write', 'config.manage'),
    ('read_write', 'plan.create'),
    ('read_write', 'plan.publish'),
    ('read_write', 'plan.execute'),
    ('read_write', 'campaign.control'),
    ('read_write', 'audit.view'),
    ('read_write', 'audit.export'),
    ('admin', 'release.create'),
    ('admin', 'release.publish'),
    ('admin', 'release.block'),
    ('admin', 'registry.manage'),
    ('admin', 'config.manage'),
    ('admin', 'plan.create'),
    ('admin', 'plan.publish'),
    ('admin', 'plan.execute'),
    ('admin', 'approval.decide'),
    ('admin', 'policy.manage'),
    ('admin', 'calendar.manage'),
    ('admin', 'freeze.manage'),
    ('admin', 'emergency.override'),
    ('admin', 'campaign.control'),
    ('admin', 'observer.manage'),
    ('admin', 'reconciliation.decide'),
    ('admin', 'audit.view'),
    ('admin', 'audit.export'),
    ('admin', 'sample.retire'),
    ('admin', 'authorization.manage')
) AS permission(legacy_role, action)
  ON permission.legacy_role = definition.source_legacy_role::TEXT
WHERE definition.built_in
ON CONFLICT (role_definition_id, action) DO NOTHING;

INSERT INTO AuthorizationBackfillCheckpoint (
  organization_id,
  checkpoint_key,
  completed,
  completed_at
)
SELECT
  organization.id,
  'built_in_roles_v1',
  true,
  now()
FROM Organization organization
ON CONFLICT (organization_id, checkpoint_key) DO UPDATE
SET
  completed = true,
  completed_at = COALESCE(
    AuthorizationBackfillCheckpoint.completed_at,
    EXCLUDED.completed_at
  ),
  updated_at = now();
