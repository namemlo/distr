CREATE FUNCTION deployment_registry_normalize_name()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.name := btrim(NEW.name);
  RETURN NEW;
END;
$$;

CREATE FUNCTION deployment_registry_normalize_physical_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.physical_identity := btrim(NEW.physical_identity);
  RETURN NEW;
END;
$$;

CREATE FUNCTION deployment_registry_normalize_alias()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.alias := lower(btrim(NEW.alias));
  RETURN NEW;
END;
$$;

CREATE FUNCTION deployment_registry_normalize_physical_name()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.physical_name := btrim(NEW.physical_name);
  RETURN NEW;
END;
$$;

CREATE TABLE DeploymentScope (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  customer_organization_id UUID,
  key TEXT NOT NULL CHECK (
    key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
  ),
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) > 0
  ),
  description TEXT NOT NULL DEFAULT '',
  delivery_model TEXT NOT NULL CHECK (
    delivery_model IN ('dedicated', 'shared', 'external')
  ),
  management_state TEXT NOT NULL CHECK (
    management_state IN (
      'managed',
      'observe_only',
      'external',
      'legacy_cutover',
      'backup',
      'retired',
      'unclassified'
    )
  ),
  retired_at TIMESTAMPTZ,
  CONSTRAINT deploymentscope_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT deploymentscope_customer_organization_fk
    FOREIGN KEY (customer_organization_id, organization_id)
    REFERENCES CustomerOrganization(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentscope_delivery_customer_check CHECK (
    (delivery_model = 'dedicated' AND customer_organization_id IS NOT NULL)
    OR
    (delivery_model IN ('shared', 'external') AND customer_organization_id IS NULL)
  ),
  CONSTRAINT deploymentscope_retirement_check CHECK (
    (management_state = 'retired') = (retired_at IS NOT NULL)
  ),
  CONSTRAINT deploymentscope_organization_key_unique
    UNIQUE (organization_id, key)
);

CREATE TRIGGER DeploymentScope_normalize_name
BEFORE INSERT OR UPDATE OF name ON DeploymentScope
FOR EACH ROW
EXECUTE FUNCTION deployment_registry_normalize_name();

CREATE INDEX DeploymentScope_registry_page
  ON DeploymentScope (organization_id, created_at DESC, id DESC);

CREATE TABLE TargetEnvironmentAssignment (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_target_id UUID NOT NULL,
  environment_id UUID NOT NULL,
  active_from TIMESTAMPTZ NOT NULL,
  active_until TIMESTAMPTZ,
  policy_constraints JSONB NOT NULL DEFAULT '{}'::jsonb,
  CONSTRAINT targetenvironmentassignment_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT targetenvironmentassignment_id_target_organization_unique
    UNIQUE (id, deployment_target_id, organization_id),
  CONSTRAINT targetenvironmentassignment_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT targetenvironmentassignment_environment_fk
    FOREIGN KEY (environment_id, organization_id)
    REFERENCES Environment(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT targetenvironmentassignment_interval_check CHECK (
    active_until IS NULL OR active_until > active_from
  ),
  CONSTRAINT targetenvironmentassignment_identity_unique
    UNIQUE (organization_id, deployment_target_id, environment_id, active_from)
);

CREATE UNIQUE INDEX TargetEnvironmentAssignment_open_identity
  ON TargetEnvironmentAssignment (
    organization_id,
    deployment_target_id,
    environment_id
  )
  WHERE active_until IS NULL;

CREATE INDEX TargetEnvironmentAssignment_registry_page
  ON TargetEnvironmentAssignment (
    organization_id,
    created_at DESC,
    id DESC
  );

CREATE FUNCTION target_environment_assignment_prevent_overlap()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(
    hashtextextended(
      NEW.organization_id::text || ':' || NEW.deployment_target_id::text,
      0
    )
  );

  IF EXISTS (
    SELECT 1
    FROM TargetEnvironmentAssignment existing
    WHERE existing.organization_id = NEW.organization_id
      AND existing.deployment_target_id = NEW.deployment_target_id
      AND existing.id <> NEW.id
      AND tstzrange(
        existing.active_from,
        existing.active_until,
        '[)'
      ) && tstzrange(NEW.active_from, NEW.active_until, '[)')
  ) THEN
    RAISE EXCEPTION
      'target has overlapping active environment assignments'
      USING ERRCODE = '23P01';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER TargetEnvironmentAssignment_prevent_overlap
BEFORE INSERT OR UPDATE OF
  organization_id,
  deployment_target_id,
  active_from,
  active_until
ON TargetEnvironmentAssignment
FOR EACH ROW EXECUTE FUNCTION target_environment_assignment_prevent_overlap();

CREATE TABLE DeploymentUnit (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_scope_id UUID NOT NULL,
  target_environment_assignment_id UUID NOT NULL,
  deployment_target_id UUID NOT NULL,
  key TEXT NOT NULL CHECK (
    key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
  ),
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) > 0
  ),
  physical_identity TEXT NOT NULL CHECK (
    physical_identity = btrim(physical_identity)
    AND length(physical_identity) > 0
  ),
  management_state TEXT NOT NULL CHECK (
    management_state IN (
      'managed',
      'observe_only',
      'external',
      'legacy_cutover',
      'backup',
      'retired',
      'unclassified'
    )
  ),
  subscriber_set_checksum TEXT NOT NULL CHECK (
    subscriber_set_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  subscriber_set_sealed_at TIMESTAMPTZ,
  retired_at TIMESTAMPTZ,
  CONSTRAINT deploymentunit_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT deploymentunit_scope_fk
    FOREIGN KEY (deployment_scope_id, organization_id)
    REFERENCES DeploymentScope(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentunit_assignment_target_fk
    FOREIGN KEY (
      target_environment_assignment_id,
      deployment_target_id,
      organization_id
    )
    REFERENCES TargetEnvironmentAssignment(
      id,
      deployment_target_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentunit_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentunit_retirement_check CHECK (
    (management_state = 'retired') = (retired_at IS NOT NULL)
  )
);

CREATE TRIGGER DeploymentUnit_normalize_name
BEFORE INSERT OR UPDATE OF name ON DeploymentUnit
FOR EACH ROW
EXECUTE FUNCTION deployment_registry_normalize_name();

CREATE TRIGGER DeploymentUnit_normalize_physical_identity
BEFORE INSERT OR UPDATE OF physical_identity ON DeploymentUnit
FOR EACH ROW
EXECUTE FUNCTION deployment_registry_normalize_physical_identity();

CREATE UNIQUE INDEX DeploymentUnit_active_physical_identity
  ON DeploymentUnit (
    organization_id,
    deployment_target_id,
    deployment_scope_id,
    lower(physical_identity)
  )
  WHERE retired_at IS NULL;

CREATE UNIQUE INDEX DeploymentUnit_active_key
  ON DeploymentUnit (organization_id, deployment_scope_id, key)
  WHERE retired_at IS NULL;

CREATE INDEX DeploymentUnit_registry_page
  ON DeploymentUnit (organization_id, created_at DESC, id DESC);

CREATE FUNCTION deployment_unit_subscriber_checksum_immutable()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.subscriber_set_checksum IS DISTINCT FROM OLD.subscriber_set_checksum THEN
    RAISE EXCEPTION 'deployment unit subscriber-set checksum is immutable'
      USING ERRCODE = '23514';
  END IF;
  IF OLD.subscriber_set_sealed_at IS NOT NULL
     AND NEW.subscriber_set_sealed_at IS DISTINCT
       FROM OLD.subscriber_set_sealed_at THEN
    RAISE EXCEPTION
      'deployment unit subscriber set cannot be unsealed after initialization'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER DeploymentUnit_subscriber_checksum_immutable
BEFORE UPDATE OF subscriber_set_checksum, subscriber_set_sealed_at
ON DeploymentUnit
FOR EACH ROW EXECUTE FUNCTION deployment_unit_subscriber_checksum_immutable();

CREATE TABLE DeploymentUnitSubscriber (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_unit_id UUID NOT NULL,
  customer_organization_id UUID NOT NULL,
  retired_at TIMESTAMPTZ,
  CONSTRAINT deploymentunitsubscriber_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT deploymentunitsubscriber_unit_fk
    FOREIGN KEY (deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentunitsubscriber_customer_fk
    FOREIGN KEY (customer_organization_id, organization_id)
    REFERENCES CustomerOrganization(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentunitsubscriber_unit_customer_unique
    UNIQUE (deployment_unit_id, customer_organization_id)
);

CREATE INDEX DeploymentUnitSubscriber_unit_customer
  ON DeploymentUnitSubscriber (
    organization_id,
    deployment_unit_id,
    customer_organization_id,
    id
  );

CREATE INDEX DeploymentUnitSubscriber_registry_page
  ON DeploymentUnitSubscriber (
    organization_id,
    created_at DESC,
    id DESC
  );

CREATE FUNCTION deployment_unit_subscriber_set_mutation_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  subscriber_set_sealed_at_value TIMESTAMPTZ;
BEGIN
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.deployment_registry_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;

  IF TG_OP <> 'DELETE' THEN
    SELECT unit.subscriber_set_sealed_at
    INTO subscriber_set_sealed_at_value
    FROM DeploymentUnit unit
    WHERE unit.id = NEW.deployment_unit_id
      AND unit.organization_id = NEW.organization_id;

    IF FOUND AND subscriber_set_sealed_at_value IS NOT NULL THEN
      RAISE EXCEPTION
        'deployment unit subscriber set is immutable after atomic initialization'
        USING ERRCODE = '23514';
    END IF;
  END IF;

  IF TG_OP <> 'INSERT'
     AND (
       TG_OP = 'DELETE'
       OR OLD.organization_id IS DISTINCT FROM NEW.organization_id
       OR OLD.deployment_unit_id IS DISTINCT FROM NEW.deployment_unit_id
     ) THEN
    SELECT unit.subscriber_set_sealed_at
    INTO subscriber_set_sealed_at_value
    FROM DeploymentUnit unit
    WHERE unit.id = OLD.deployment_unit_id
      AND unit.organization_id = OLD.organization_id;

    IF FOUND AND subscriber_set_sealed_at_value IS NOT NULL THEN
      RAISE EXCEPTION
        'deployment unit subscriber set is immutable after atomic initialization'
        USING ERRCODE = '23514';
    END IF;
  END IF;

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER DeploymentUnitSubscriber_mutation_guard
BEFORE INSERT OR UPDATE OR DELETE ON DeploymentUnitSubscriber
FOR EACH ROW
EXECUTE FUNCTION deployment_unit_subscriber_set_mutation_guard();

CREATE FUNCTION deployment_unit_subscriber_set_checksum(
  requested_organization_id UUID,
  requested_deployment_unit_id UUID
)
RETURNS TEXT
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
  payload BYTEA := convert_to(
    'distr.deployment-unit-subscriber-set/v1',
    'UTF8'
  );
  customer_id UUID;
  customer_id_text TEXT;
BEGIN
  FOR customer_id IN
    SELECT subscriber.customer_organization_id
    FROM DeploymentUnitSubscriber subscriber
    WHERE subscriber.organization_id = requested_organization_id
      AND subscriber.deployment_unit_id = requested_deployment_unit_id
      AND subscriber.retired_at IS NULL
    ORDER BY subscriber.customer_organization_id
  LOOP
    customer_id_text := customer_id::text;
    payload := payload
      || decode(
        lpad(to_hex(octet_length(customer_id_text)), 8, '0'),
        'hex'
      )
      || convert_to(customer_id_text, 'UTF8');
  END LOOP;

  RETURN 'sha256:' || encode(sha256(payload), 'hex');
END;
$$;

CREATE FUNCTION deployment_unit_validate_subscriber_set(
  requested_organization_id UUID,
  requested_deployment_unit_id UUID
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
  expected_checksum TEXT;
  actual_checksum TEXT;
  delivery_model_value TEXT;
  active_subscriber_count BIGINT;
  subscriber_set_sealed_at_value TIMESTAMPTZ;
BEGIN
  SELECT
    unit.subscriber_set_checksum,
    scope.delivery_model,
    unit.subscriber_set_sealed_at
  INTO
    expected_checksum,
    delivery_model_value,
    subscriber_set_sealed_at_value
  FROM DeploymentUnit unit
  JOIN DeploymentScope scope
    ON scope.id = unit.deployment_scope_id
   AND scope.organization_id = unit.organization_id
  WHERE unit.id = requested_deployment_unit_id
    AND unit.organization_id = requested_organization_id;

  IF NOT FOUND THEN
    RETURN;
  END IF;

  IF subscriber_set_sealed_at_value IS NULL THEN
    RAISE EXCEPTION
      'deployment unit requires atomically sealed subscriber initialization'
      USING ERRCODE = '23514';
  END IF;

  SELECT count(*)
  INTO active_subscriber_count
  FROM DeploymentUnitSubscriber subscriber
  WHERE subscriber.organization_id = requested_organization_id
    AND subscriber.deployment_unit_id = requested_deployment_unit_id
    AND subscriber.retired_at IS NULL;

  actual_checksum := deployment_unit_subscriber_set_checksum(
    requested_organization_id,
    requested_deployment_unit_id
  );

  IF delivery_model_value = 'shared'
     AND active_subscriber_count = 0 THEN
    RAISE EXCEPTION
      'shared deployment unit requires an atomically initialized subscriber set'
      USING ERRCODE = '23514';
  END IF;

  IF delivery_model_value <> 'shared'
     AND active_subscriber_count <> 0 THEN
    RAISE EXCEPTION
      'only shared deployment units may have subscribers'
      USING ERRCODE = '23514';
  END IF;

  IF expected_checksum IS DISTINCT FROM actual_checksum THEN
    RAISE EXCEPTION
      'deployment unit subscriber set does not match its immutable checksum'
      USING ERRCODE = '23514';
  END IF;
END;
$$;

CREATE FUNCTION deployment_unit_subscriber_set_from_unit_constraint()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  PERFORM deployment_unit_validate_subscriber_set(
    NEW.organization_id,
    NEW.id
  );
  RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER DeploymentUnit_subscriber_set_matches
AFTER INSERT OR UPDATE ON DeploymentUnit
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION deployment_unit_subscriber_set_from_unit_constraint();

CREATE FUNCTION deployment_unit_subscriber_set_from_member_constraint()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP <> 'DELETE' THEN
    PERFORM deployment_unit_validate_subscriber_set(
      NEW.organization_id,
      NEW.deployment_unit_id
    );
  END IF;

  IF TG_OP <> 'INSERT'
     AND (
       TG_OP = 'DELETE'
       OR OLD.organization_id IS DISTINCT FROM NEW.organization_id
       OR OLD.deployment_unit_id IS DISTINCT FROM NEW.deployment_unit_id
     ) THEN
    PERFORM deployment_unit_validate_subscriber_set(
      OLD.organization_id,
      OLD.deployment_unit_id
    );
  END IF;

  RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER DeploymentUnitSubscriber_set_matches
AFTER INSERT OR UPDATE OR DELETE ON DeploymentUnitSubscriber
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION deployment_unit_subscriber_set_from_member_constraint();

CREATE TABLE ComponentDefinition (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  key TEXT NOT NULL CHECK (
    key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
  ),
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) > 0
  ),
  description TEXT NOT NULL DEFAULT '',
  capability_scope TEXT NOT NULL DEFAULT '',
  management_state TEXT NOT NULL CHECK (
    management_state IN (
      'managed',
      'observe_only',
      'external',
      'legacy_cutover',
      'backup',
      'retired',
      'unclassified'
    )
  ),
  retired_at TIMESTAMPTZ,
  CONSTRAINT componentdefinition_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT componentdefinition_organization_key_unique
    UNIQUE (organization_id, key),
  CONSTRAINT componentdefinition_retirement_check CHECK (
    (management_state = 'retired') = (retired_at IS NOT NULL)
  )
);

CREATE TRIGGER ComponentDefinition_normalize_name
BEFORE INSERT OR UPDATE OF name ON ComponentDefinition
FOR EACH ROW
EXECUTE FUNCTION deployment_registry_normalize_name();

CREATE INDEX ComponentDefinition_registry_order
  ON ComponentDefinition (organization_id, key, id);

CREATE INDEX ComponentDefinition_registry_page
  ON ComponentDefinition (organization_id, created_at DESC, id DESC);

CREATE TABLE ComponentAlias (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  component_definition_id UUID NOT NULL,
  alias TEXT NOT NULL CHECK (
    alias = lower(btrim(alias)) AND length(alias) > 0
  ),
  retired_at TIMESTAMPTZ,
  CONSTRAINT componentalias_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT componentalias_definition_fk
    FOREIGN KEY (component_definition_id, organization_id)
    REFERENCES ComponentDefinition(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT componentalias_organization_alias_unique
    UNIQUE (organization_id, alias)
);

CREATE TRIGGER ComponentAlias_normalize_alias
BEFORE INSERT OR UPDATE OF alias ON ComponentAlias
FOR EACH ROW
EXECUTE FUNCTION deployment_registry_normalize_alias();

CREATE INDEX ComponentAlias_definition_alias
  ON ComponentAlias (
    organization_id,
    component_definition_id,
    alias,
    id
  );

CREATE INDEX ComponentAlias_registry_page
  ON ComponentAlias (organization_id, created_at DESC, id DESC);

CREATE TABLE ComponentInstance (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_unit_id UUID NOT NULL,
  component_definition_id UUID NOT NULL,
  physical_name TEXT NOT NULL CHECK (
    physical_name = btrim(physical_name)
    AND length(physical_name) > 0
  ),
  config_namespace TEXT NOT NULL DEFAULT '',
  database_boundary TEXT NOT NULL DEFAULT '',
  health_adapter TEXT NOT NULL DEFAULT '',
  management_state TEXT NOT NULL CHECK (
    management_state IN (
      'managed',
      'observe_only',
      'external',
      'legacy_cutover',
      'backup',
      'retired',
      'unclassified'
    )
  ),
  retired_at TIMESTAMPTZ,
  CONSTRAINT componentinstance_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT componentinstance_unit_fk
    FOREIGN KEY (deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT componentinstance_definition_fk
    FOREIGN KEY (component_definition_id, organization_id)
    REFERENCES ComponentDefinition(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT componentinstance_retirement_check CHECK (
    (management_state = 'retired') = (retired_at IS NOT NULL)
  )
);

CREATE TRIGGER ComponentInstance_normalize_physical_name
BEFORE INSERT OR UPDATE OF physical_name ON ComponentInstance
FOR EACH ROW
EXECUTE FUNCTION deployment_registry_normalize_physical_name();

CREATE UNIQUE INDEX ComponentInstance_active_physical_name
  ON ComponentInstance (
    organization_id,
    deployment_unit_id,
    lower(physical_name)
  )
  WHERE retired_at IS NULL;

CREATE UNIQUE INDEX ComponentInstance_active_definition
  ON ComponentInstance (
    organization_id,
    deployment_unit_id,
    component_definition_id
  )
  WHERE retired_at IS NULL;

CREATE INDEX ComponentInstance_registry_order
  ON ComponentInstance (
    organization_id,
    deployment_unit_id,
    physical_name,
    id
  );

CREATE INDEX ComponentInstance_registry_page
  ON ComponentInstance (organization_id, created_at DESC, id DESC);

CREATE TABLE ComponentInstanceRename (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL
    REFERENCES Organization(id) ON DELETE CASCADE,
  component_instance_id UUID NOT NULL,
  component_alias_id UUID NOT NULL,
  from_physical_name TEXT NOT NULL CHECK (
    from_physical_name = btrim(from_physical_name)
    AND length(from_physical_name) > 0
  ),
  to_physical_name TEXT NOT NULL CHECK (
    to_physical_name = btrim(to_physical_name)
    AND length(to_physical_name) > 0
  ),
  CONSTRAINT componentinstancerename_name_change_check CHECK (
    from_physical_name <> to_physical_name
  ),
  CONSTRAINT componentinstancerename_instance_fk
    FOREIGN KEY (component_instance_id, organization_id)
    REFERENCES ComponentInstance(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT componentinstancerename_alias_fk
    FOREIGN KEY (component_alias_id, organization_id)
    REFERENCES ComponentAlias(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX ComponentInstanceRename_instance_history
  ON ComponentInstanceRename (
    organization_id,
    component_instance_id,
    created_at,
    id
  );

CREATE INDEX ComponentInstanceRename_alias_history
  ON ComponentInstanceRename (
    organization_id,
    component_alias_id,
    created_at,
    id
  );

CREATE FUNCTION component_instance_rename_append_only()
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

  RAISE EXCEPTION 'component instance rename history is append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER ComponentInstanceRename_append_only
BEFORE UPDATE OR DELETE ON ComponentInstanceRename
FOR EACH ROW EXECUTE FUNCTION component_instance_rename_append_only();

CREATE FUNCTION component_alias_rename_history_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.retired_at IS NOT NULL
     AND NEW.retired_at IS DISTINCT FROM OLD.retired_at
     AND EXISTS (
       SELECT 1
       FROM ComponentInstanceRename history
       WHERE history.organization_id = OLD.organization_id
         AND history.component_alias_id = OLD.id
     ) THEN
    RAISE EXCEPTION
      'component alias retirement would erase rename evidence'
      USING
        ERRCODE = '23514',
        CONSTRAINT = 'componentalias_rename_history_guard';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER ComponentAlias_rename_history_guard
BEFORE UPDATE OF retired_at ON ComponentAlias
FOR EACH ROW EXECUTE FUNCTION component_alias_rename_history_guard();
