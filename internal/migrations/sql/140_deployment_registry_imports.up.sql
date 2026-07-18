CREATE TABLE RegistryImport (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  source_kind TEXT NOT NULL CHECK (
    source_kind = btrim(source_kind) AND length(source_kind) BETWEEN 1 AND 128
  ),
  tool_name TEXT NOT NULL CHECK (
    tool_name = btrim(tool_name) AND length(tool_name) BETWEEN 1 AND 128
  ),
  tool_version TEXT NOT NULL CHECK (
    tool_version = btrim(tool_version) AND length(tool_version) BETWEEN 1 AND 128
  ),
  source_commit TEXT CHECK (
    source_commit IS NULL
    OR source_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'
  ),
  canonical_parameters JSONB NOT NULL CHECK (
    jsonb_typeof(canonical_parameters) = 'object'
    AND pg_column_size(canonical_parameters) <= 65536
  ),
  evidence_reference TEXT NOT NULL CHECK (
    evidence_reference ~ '^evidence://sha256/[0-9a-f]{64}$'
  ),
  evidence_checksum TEXT NOT NULL CHECK (
    evidence_checksum ~ '^[0-9a-f]{64}$'
    AND evidence_reference = 'evidence://sha256/' || evidence_checksum
  ),
  preview_checksum TEXT NOT NULL CHECK (
    preview_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  counts JSONB NOT NULL CHECK (
    jsonb_typeof(counts) = 'object' AND pg_column_size(counts) <= 16384
  ),
  diff JSONB NOT NULL CHECK (
    jsonb_typeof(diff) = 'object' AND pg_column_size(diff) <= 1048576
  ),
  omissions JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (
    jsonb_typeof(omissions) = 'array' AND pg_column_size(omissions) <= 1048576
  ),
  diagnostics JSONB NOT NULL CHECK (
    jsonb_typeof(diagnostics) = 'array'
    AND pg_column_size(diagnostics) <= 262144
  ),
  diagnostics_truncated BOOLEAN NOT NULL DEFAULT false,
  state TEXT NOT NULL CHECK (
    state IN ('previewed', 'applying', 'applied', 'failed')
  ),
  actor_useraccount_id UUID NOT NULL REFERENCES UserAccount(id) ON DELETE RESTRICT,
  applied_by_useraccount_id UUID REFERENCES UserAccount(id) ON DELETE RESTRICT,
  last_committed_checkpoint INTEGER NOT NULL DEFAULT 0 CHECK (
    last_committed_checkpoint >= 0
  ),
  apply_claim_id UUID,
  apply_claimed_at TIMESTAMPTZ,
  applied_at TIMESTAMPTZ,
  CONSTRAINT registryimport_id_organization_unique UNIQUE (id, organization_id),
  CONSTRAINT registryimport_applied_state_check CHECK (
    (state = 'applied') = (
      applied_at IS NOT NULL AND applied_by_useraccount_id IS NOT NULL
    )
  ),
  CONSTRAINT registryimport_apply_claim_state_check CHECK (
    (state = 'applying') = (
      apply_claim_id IS NOT NULL AND apply_claimed_at IS NOT NULL
    )
  )
);

CREATE INDEX RegistryImport_org_created
  ON RegistryImport (organization_id, created_at DESC, id DESC);

CREATE TABLE RegistryImportRoot (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  registry_import_id UUID NOT NULL,
  root_key TEXT NOT NULL CHECK (
    root_key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
  ),
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) BETWEEN 1 AND 256
  ),
  delivery_model TEXT NOT NULL CHECK (
    delivery_model IN ('dedicated', 'shared', 'external')
  ),
  customer_organization_id UUID,
  deployment_target_id UUID,
  environment_id UUID,
  subscriber_customer_organization_ids UUID[] NOT NULL DEFAULT '{}',
  physical_identity TEXT NOT NULL CHECK (
    physical_identity = btrim(physical_identity)
    AND length(physical_identity) BETWEEN 1 AND 512
  ),
  candidate_checksum TEXT NOT NULL CHECK (
    candidate_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT registryimportroot_import_fk FOREIGN KEY (
    registry_import_id, organization_id
  ) REFERENCES RegistryImport(id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT registryimportroot_import_key_unique UNIQUE (
    registry_import_id, root_key
  ),
  CONSTRAINT registryimportroot_id_import_organization_unique UNIQUE (
    id, registry_import_id, organization_id
  ),
  CONSTRAINT registryimportroot_id_organization_unique UNIQUE (
    id, organization_id
  ),
  CONSTRAINT registryimportroot_subscriber_ids_check CHECK (
    array_position(subscriber_customer_organization_ids, NULL) IS NULL
  )
);

CREATE FUNCTION registry_import_root_validate_org_references()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF EXISTS (
    SELECT subscriber_id
    FROM unnest(NEW.subscriber_customer_organization_ids) subscriber_id
    GROUP BY subscriber_id
    HAVING count(*) > 1
  ) OR NEW.subscriber_customer_organization_ids IS DISTINCT FROM ARRAY(
    SELECT subscriber_id
    FROM unnest(NEW.subscriber_customer_organization_ids) subscriber_id
    ORDER BY subscriber_id
  ) THEN
    RAISE EXCEPTION 'registry import subscribers must be sorted and unique'
      USING ERRCODE = '23514';
  END IF;
  IF NEW.customer_organization_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM CustomerOrganization customer
    WHERE customer.id = NEW.customer_organization_id
      AND customer.organization_id = NEW.organization_id
  ) THEN
    RAISE EXCEPTION 'registry import customer is outside its organization'
      USING ERRCODE = '23503';
  END IF;
  IF NEW.deployment_target_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM DeploymentTarget target
    WHERE target.id = NEW.deployment_target_id
      AND target.organization_id = NEW.organization_id
  ) THEN
    RAISE EXCEPTION 'registry import target is outside its organization'
      USING ERRCODE = '23503';
  END IF;
  IF NEW.environment_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM Environment environment
    WHERE environment.id = NEW.environment_id
      AND environment.organization_id = NEW.organization_id
  ) THEN
    RAISE EXCEPTION 'registry import environment is outside its organization'
      USING ERRCODE = '23503';
  END IF;
  IF EXISTS (
    SELECT 1
    FROM unnest(NEW.subscriber_customer_organization_ids) subscriber_id
    WHERE NOT EXISTS (
      SELECT 1 FROM CustomerOrganization customer
      WHERE customer.id = subscriber_id
        AND customer.organization_id = NEW.organization_id
    )
  ) THEN
    RAISE EXCEPTION 'registry import subscriber is outside its organization'
      USING ERRCODE = '23503';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER RegistryImportRoot_validate_org_references
BEFORE INSERT OR UPDATE OF
  organization_id,
  customer_organization_id,
  deployment_target_id,
  environment_id,
  subscriber_customer_organization_ids
ON RegistryImportRoot
FOR EACH ROW EXECUTE FUNCTION registry_import_root_validate_org_references();

CREATE INDEX RegistryImportRoot_import_order
  ON RegistryImportRoot (organization_id, registry_import_id, root_key, id);

CREATE TABLE RegistryImportPlacement (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  registry_import_id UUID NOT NULL,
  registry_import_root_id UUID NOT NULL,
  component_key TEXT NOT NULL CHECK (
    component_key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
  ),
  physical_name TEXT NOT NULL CHECK (
    physical_name = btrim(physical_name)
    AND length(physical_name) BETWEEN 1 AND 512
  ),
  config_namespace TEXT NOT NULL DEFAULT '' CHECK (
    config_namespace = btrim(config_namespace)
    AND length(config_namespace) <= 512
  ),
  database_boundary TEXT NOT NULL DEFAULT '' CHECK (
    database_boundary = btrim(database_boundary)
    AND length(database_boundary) <= 512
  ),
  health_adapter TEXT NOT NULL DEFAULT '' CHECK (
    health_adapter = btrim(health_adapter)
    AND length(health_adapter) <= 256
  ),
  renamed_from TEXT CHECK (
    renamed_from IS NULL
    OR (
      renamed_from = btrim(renamed_from)
      AND length(renamed_from) BETWEEN 1 AND 512
    )
  ),
  candidate_checksum TEXT NOT NULL CHECK (
    candidate_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT registryimportplacement_import_fk FOREIGN KEY (
    registry_import_id, organization_id
  ) REFERENCES RegistryImport(id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT registryimportplacement_root_fk FOREIGN KEY (
    registry_import_root_id, registry_import_id, organization_id
  ) REFERENCES RegistryImportRoot(id, registry_import_id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT registryimportplacement_identity_unique UNIQUE (
    registry_import_root_id, component_key
  )
);

CREATE UNIQUE INDEX RegistryImportPlacement_root_physical_identity
  ON RegistryImportPlacement (
    registry_import_root_id, lower(physical_name)
  );

CREATE INDEX RegistryImportPlacement_import_order
  ON RegistryImportPlacement (
    organization_id, registry_import_id, registry_import_root_id,
    component_key, physical_name, id
  );

CREATE TABLE RegistryImportDecision (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  registry_import_id UUID NOT NULL,
  registry_import_root_id UUID NOT NULL,
  decision_ordinal INTEGER NOT NULL CHECK (decision_ordinal > 0),
  classification TEXT NOT NULL CHECK (
    classification IN (
      'standard', 'shared', 'external', 'observe_only',
      'ignored', 'needs_decision'
    )
  ),
  actor_useraccount_id UUID NOT NULL REFERENCES UserAccount(id) ON DELETE RESTRICT,
  CONSTRAINT registryimportdecision_import_fk FOREIGN KEY (
    registry_import_id, organization_id
  ) REFERENCES RegistryImport(id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT registryimportdecision_root_fk FOREIGN KEY (
    registry_import_root_id, registry_import_id, organization_id
  ) REFERENCES RegistryImportRoot(id, registry_import_id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT registryimportdecision_root_ordinal_unique UNIQUE (
    registry_import_root_id, decision_ordinal
  )
);

CREATE INDEX RegistryImportDecision_latest
  ON RegistryImportDecision (
    organization_id, registry_import_id, registry_import_root_id,
    decision_ordinal DESC
  );

CREATE FUNCTION registry_import_decision_append_only()
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
  RAISE EXCEPTION 'registry import decisions are append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER RegistryImportDecision_append_only
BEFORE UPDATE OR DELETE ON RegistryImportDecision
FOR EACH ROW EXECUTE FUNCTION registry_import_decision_append_only();
