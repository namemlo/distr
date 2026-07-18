CREATE FUNCTION deployment_policy_normalize_text()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.name := btrim(NEW.name);
  RETURN NEW;
END;
$$;

CREATE TABLE DeploymentPolicy (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  key TEXT NOT NULL CHECK (
    key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
    AND length(key) <= 128
  ),
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) BETWEEN 1 AND 256
  ),
  description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),
  CONSTRAINT deploymentpolicy_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT deploymentpolicy_organization_key_unique
    UNIQUE (organization_id, key)
);

CREATE TRIGGER DeploymentPolicy_normalize_text
BEFORE INSERT OR UPDATE OF name ON DeploymentPolicy
FOR EACH ROW EXECUTE FUNCTION deployment_policy_normalize_text();

CREATE INDEX DeploymentPolicy_organization_order
  ON DeploymentPolicy (organization_id, key, id);

CREATE TABLE DeploymentPolicyVersion (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_policy_id UUID NOT NULL,
  version_number INTEGER NOT NULL CHECK (version_number > 0),
  state TEXT NOT NULL CHECK (state IN ('DRAFT', 'PUBLISHED')),
  document JSONB NOT NULL CHECK (
    jsonb_typeof(document) = 'object'
    AND document->>'schema' = 'distr.deployment-policy/v1'
    AND pg_column_size(document) <= 1048576
  ),
  canonical_checksum TEXT NOT NULL CHECK (
    canonical_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  canonical_payload BYTEA NOT NULL CHECK (
    octet_length(canonical_payload) <= 1048576
    AND convert_from(canonical_payload, 'UTF8')::jsonb = document
    AND canonical_checksum =
      'sha256:' || encode(sha256(canonical_payload), 'hex')
  ),
  created_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  published_by_useraccount_id UUID
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  published_at TIMESTAMPTZ,
  CONSTRAINT deploymentpolicyversion_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT deploymentpolicyversion_policy_fk
    FOREIGN KEY (deployment_policy_id, organization_id)
    REFERENCES DeploymentPolicy(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentpolicyversion_policy_version_unique
    UNIQUE (deployment_policy_id, version_number),
  CONSTRAINT deploymentpolicyversion_publication_check CHECK (
    (
      state = 'DRAFT'
      AND published_by_useraccount_id IS NULL
      AND published_at IS NULL
    )
    OR
    (
      state = 'PUBLISHED'
      AND published_by_useraccount_id IS NOT NULL
      AND published_at IS NOT NULL
    )
  )
);

CREATE INDEX DeploymentPolicyVersion_organization_policy_order
  ON DeploymentPolicyVersion (
    organization_id,
    deployment_policy_id,
    version_number DESC,
    id
  );

CREATE FUNCTION deployment_policy_version_published_immutable()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF current_setting(
      'distr.deployment_policy_deletion_reason',
      true
    ) = 'ORGANIZATION_RETENTION' THEN
      RETURN OLD;
    END IF;
    IF OLD.state = 'PUBLISHED' THEN
      RAISE EXCEPTION 'published deployment policy versions are immutable'
        USING ERRCODE = '23514';
    END IF;
    RETURN OLD;
  END IF;

  IF OLD.state = 'PUBLISHED' THEN
    RAISE EXCEPTION 'published deployment policy versions are immutable'
      USING ERRCODE = '23514';
  END IF;

  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.deployment_policy_id IS DISTINCT FROM OLD.deployment_policy_id
     OR NEW.version_number IS DISTINCT FROM OLD.version_number
     OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR NEW.created_by_useraccount_id IS DISTINCT FROM
       OLD.created_by_useraccount_id THEN
    RAISE EXCEPTION 'deployment policy version identity is immutable'
      USING ERRCODE = '23514';
  END IF;

  IF NEW.state = 'PUBLISHED'
     AND (
       NEW.document IS DISTINCT FROM OLD.document
       OR NEW.canonical_checksum IS DISTINCT FROM OLD.canonical_checksum
       OR NEW.canonical_payload IS DISTINCT FROM OLD.canonical_payload
     ) THEN
    RAISE EXCEPTION
      'publishing cannot mutate deployment policy version content'
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER DeploymentPolicyVersion_published_immutable
BEFORE UPDATE OR DELETE ON DeploymentPolicyVersion
FOR EACH ROW EXECUTE FUNCTION deployment_policy_version_published_immutable();

CREATE TABLE DeploymentPolicyBinding (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_policy_version_id UUID NOT NULL,
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
  binding_role TEXT NOT NULL CHECK (
    binding_role IN ('owner', 'subscriber')
  ),
  created_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  retired_at TIMESTAMPTZ,
  CONSTRAINT deploymentpolicybinding_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT deploymentpolicybinding_version_fk
    FOREIGN KEY (deployment_policy_version_id, organization_id)
    REFERENCES DeploymentPolicyVersion(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentpolicybinding_subscriber_scope_check CHECK (
    binding_role <> 'subscriber' OR scope_kind = 'customer'
  )
);

CREATE UNIQUE INDEX DeploymentPolicyBinding_active_identity
  ON DeploymentPolicyBinding (
    organization_id,
    deployment_policy_version_id,
    scope_kind,
    scope_id,
    binding_role
  )
  WHERE retired_at IS NULL;

CREATE INDEX DeploymentPolicyBinding_scope_resolution
  ON DeploymentPolicyBinding (
    organization_id,
    scope_kind,
    scope_id,
    binding_role,
    deployment_policy_version_id
  )
  WHERE retired_at IS NULL;

CREATE FUNCTION deployment_policy_binding_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  version_state TEXT;
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF current_setting(
      'distr.deployment_policy_deletion_reason',
      true
    ) = 'ORGANIZATION_RETENTION' THEN
      RETURN OLD;
    END IF;
    RAISE EXCEPTION 'deployment policy binding history is append-only'
      USING ERRCODE = '23514';
  END IF;

  IF TG_OP = 'INSERT' THEN
    SELECT version.state
    INTO version_state
    FROM DeploymentPolicyVersion version
    WHERE version.id = NEW.deployment_policy_version_id
      AND version.organization_id = NEW.organization_id;

    IF version_state IS DISTINCT FROM 'PUBLISHED' THEN
      RAISE EXCEPTION
        'deployment policy bindings require a published immutable version'
        USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
  END IF;

  IF OLD.retired_at IS NOT NULL THEN
    RAISE EXCEPTION 'retired deployment policy bindings are immutable'
      USING ERRCODE = '23514';
  END IF;

  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.deployment_policy_version_id IS DISTINCT FROM
       OLD.deployment_policy_version_id
     OR NEW.scope_kind IS DISTINCT FROM OLD.scope_kind
     OR NEW.scope_id IS DISTINCT FROM OLD.scope_id
     OR NEW.binding_role IS DISTINCT FROM OLD.binding_role
     OR NEW.created_by_useraccount_id IS DISTINCT FROM
       OLD.created_by_useraccount_id
     OR NEW.retired_at IS NULL THEN
    RAISE EXCEPTION
      'deployment policy bindings may only transition once to retired'
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER DeploymentPolicyBinding_guard
BEFORE INSERT OR UPDATE OR DELETE ON DeploymentPolicyBinding
FOR EACH ROW EXECUTE FUNCTION deployment_policy_binding_guard();

ALTER TABLE DeploymentPlan
  ADD COLUMN deployment_unit_id UUID,
  ADD COLUMN effective_policy JSONB,
  ADD COLUMN effective_policy_checksum TEXT,
  ADD COLUMN subscriber_set_checksum TEXT,
  ADD CONSTRAINT deploymentplan_deployment_unit_fk
    FOREIGN KEY (deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  ADD CONSTRAINT deploymentplan_effective_policy_shape_check CHECK (
    (
      deployment_unit_id IS NULL
      AND effective_policy IS NULL
      AND effective_policy_checksum IS NULL
      AND subscriber_set_checksum IS NULL
    )
    OR
    (
      deployment_unit_id IS NOT NULL
      AND jsonb_typeof(effective_policy) = 'object'
      AND pg_column_size(effective_policy) <= 1048576
      AND effective_policy_checksum ~ '^sha256:[0-9a-f]{64}$'
      AND subscriber_set_checksum ~ '^sha256:[0-9a-f]{64}$'
      AND effective_policy->>'checksum' = effective_policy_checksum
      AND effective_policy->>'subscriberSetChecksum' =
        subscriber_set_checksum
    )
  );

CREATE INDEX DeploymentPlan_effective_policy
  ON DeploymentPlan (
    organization_id,
    deployment_unit_id,
    effective_policy_checksum
  )
  WHERE deployment_unit_id IS NOT NULL;
