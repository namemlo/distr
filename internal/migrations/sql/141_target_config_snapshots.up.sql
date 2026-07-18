ALTER TABLE TargetEnvironmentAssignment
  ADD CONSTRAINT targetenvironmentassignment_id_environment_organization_unique
  UNIQUE (id, environment_id, organization_id);

ALTER TABLE DeploymentUnit
  ADD CONSTRAINT deploymentunit_id_assignment_organization_unique
  UNIQUE (id, target_environment_assignment_id, organization_id);

ALTER TABLE ComponentInstance
  ADD CONSTRAINT componentinstance_id_unit_organization_unique
  UNIQUE (id, deployment_unit_id, organization_id);

CREATE TABLE TargetConfigSnapshot (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  created_by_user_account_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  target_environment_assignment_id UUID NOT NULL,
  environment_id UUID NOT NULL,
  source_repository TEXT NOT NULL CHECK (
    length(source_repository) BETWEEN 1 AND 2048
    AND source_repository !~ E'[\\r\\n]'
    AND source_repository ~ '^(https|ssh)://[^/@?#]+(/[^?#]*)?$'
  ),
  source_commit TEXT NOT NULL CHECK (
    source_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'
  ),
  source_adapter TEXT NOT NULL CHECK (
    length(btrim(source_adapter)) BETWEEN 1 AND 128
  ),
  adapter_version TEXT NOT NULL CHECK (
    length(btrim(adapter_version)) BETWEEN 1 AND 128
  ),
  target_platform TEXT NOT NULL CHECK (
    target_platform ~ '^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$'
  ),
  runtime_constraints JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (
    jsonb_typeof(runtime_constraints) = 'object'
    AND octet_length(runtime_constraints::text) <= 131072
    AND runtime_constraints::text !~* '(password|passwd|secret|api[_-]?key|access[_-]?token|authorization|private[_-]?key|credential)'
  ),
  schema TEXT NOT NULL CHECK (
    schema = 'distr.target-config/v1'
  ),
  canonical_payload BYTEA NOT NULL CHECK (
    octet_length(canonical_payload) <= 1048576
    AND convert_from(canonical_payload, 'UTF8')::jsonb ->> 'schema'
      = 'distr.target-config/v1'
  ),
  canonical_checksum TEXT NOT NULL CHECK (
    canonical_checksum ~ '^sha256:[0-9a-f]{64}$'
    AND canonical_checksum
      = 'sha256:' || encode(sha256(canonical_payload), 'hex')
  ),
  CONSTRAINT targetconfigsnapshot_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT targetconfigsnapshot_id_unit_organization_unique
    UNIQUE (id, deployment_unit_id, organization_id),
  CONSTRAINT targetconfigsnapshot_creator_organization_fk
    FOREIGN KEY (
      organization_id,
      created_by_user_account_id
    )
    REFERENCES Organization_UserAccount(
      organization_id,
      user_account_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT targetconfigsnapshot_unit_assignment_fk
    FOREIGN KEY (
      deployment_unit_id,
      target_environment_assignment_id,
      organization_id
    )
    REFERENCES DeploymentUnit(
      id,
      target_environment_assignment_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT targetconfigsnapshot_assignment_environment_fk
    FOREIGN KEY (
      target_environment_assignment_id,
      environment_id,
      organization_id
    )
    REFERENCES TargetEnvironmentAssignment(
      id,
      environment_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT targetconfigsnapshot_organization_checksum_unique
    UNIQUE (organization_id, canonical_checksum)
);

CREATE INDEX TargetConfigSnapshot_page
  ON TargetConfigSnapshot (organization_id, created_at DESC, id DESC);

CREATE INDEX TargetConfigSnapshot_unit_page
  ON TargetConfigSnapshot (
    organization_id,
    deployment_unit_id,
    created_at DESC,
    id DESC
  );

CREATE INDEX TargetConfigSnapshot_assignment_page
  ON TargetConfigSnapshot (
    organization_id,
    target_environment_assignment_id,
    created_at DESC,
    id DESC
  );

CREATE TABLE TargetConfigSnapshotObject (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  target_config_snapshot_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  key TEXT NOT NULL CHECK (
    key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
    AND length(key) <= 128
  ),
  kind TEXT NOT NULL CHECK (
    kind IN ('deployment_descriptor', 'service_config', 'adapter_input')
  ),
  reference TEXT NOT NULL CHECK (
    length(reference) BETWEEN 1 AND 2048
    AND reference ~ '^s3://[^/?#]+/[^?#]+$'
    AND reference !~ E'\\\\'
    AND reference !~ '/\\.\\.?(/|$)'
  ),
  version_id TEXT NOT NULL DEFAULT '' CHECK (
    octet_length(version_id) <= 1024
    AND version_id = btrim(version_id)
    AND version_id !~ '[[:cntrl:]]'
    AND version_id !~* '(password|passwd|secret|api[_-]?key|access[_-]?token|authorization|private[_-]?key|credential)[[:space:]]*[:=]'
    AND version_id !~* '^(gh[pousr]_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{20,}|(AKIA|ASIA)[A-Z0-9]{16})$'
  ),
  media_type TEXT NOT NULL CHECK (
    length(media_type) <= 128
    AND media_type ~ '^[a-z0-9][a-z0-9.+-]*/[a-z0-9][a-z0-9.+-]*$'
  ),
  size_bytes BIGINT NOT NULL CHECK (
    size_bytes BETWEEN 0 AND 16777216
  ),
  checksum TEXT NOT NULL CHECK (
    checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT targetconfigsnapshotobject_snapshot_fk
    FOREIGN KEY (target_config_snapshot_id, organization_id)
    REFERENCES TargetConfigSnapshot(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT targetconfigsnapshotobject_immutable_identity_check CHECK (
    length(version_id) > 0
    OR (
      reference ~ '^s3://[^/?#]+/_immutable/sha256/[0-9a-f]{64}/[^?#]+$'
      AND substring(
        reference FROM '/_immutable/sha256/([0-9a-f]{64})/'
      ) = substring(checksum FROM 8)
    )
  ),
  CONSTRAINT targetconfigsnapshotobject_snapshot_key_unique
    UNIQUE (target_config_snapshot_id, key)
);

CREATE INDEX TargetConfigSnapshotObject_snapshot_order
  ON TargetConfigSnapshotObject (
    organization_id,
    target_config_snapshot_id,
    key,
    id
  );

CREATE TABLE TargetConfigSnapshotComponent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  target_config_snapshot_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  component_instance_id UUID NOT NULL,
  physical_name TEXT NOT NULL CHECK (
    physical_name = btrim(physical_name)
    AND length(physical_name) BETWEEN 1 AND 255
  ),
  CONSTRAINT targetconfigsnapshotcomponent_snapshot_fk
    FOREIGN KEY (target_config_snapshot_id, organization_id)
    REFERENCES TargetConfigSnapshot(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT targetconfigsnapshotcomponent_instance_unit_fk
    FOREIGN KEY (
      component_instance_id,
      deployment_unit_id,
      organization_id
    )
    REFERENCES ComponentInstance(id, deployment_unit_id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT targetconfigsnapshotcomponent_snapshot_unit_fk
    FOREIGN KEY (
      target_config_snapshot_id,
      deployment_unit_id,
      organization_id
    )
    REFERENCES TargetConfigSnapshot(id, deployment_unit_id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT targetconfigsnapshotcomponent_snapshot_physical_unique
    UNIQUE (target_config_snapshot_id, physical_name),
  CONSTRAINT targetconfigsnapshotcomponent_snapshot_instance_unique
    UNIQUE (target_config_snapshot_id, component_instance_id)
);

CREATE INDEX TargetConfigSnapshotComponent_snapshot_order
  ON TargetConfigSnapshotComponent (
    organization_id,
    target_config_snapshot_id,
    physical_name,
    id
  );

CREATE TABLE TargetConfigSnapshotSecretReference (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  target_config_snapshot_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  key TEXT NOT NULL CHECK (
    key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
    AND length(key) <= 128
  ),
  provider TEXT NOT NULL CHECK (
    octet_length(provider) BETWEEN 1 AND 128
    AND provider = btrim(provider)
    AND provider ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
    AND provider !~* '(password|passwd|secret|api[_-]?key|access[_-]?token|authorization|private[_-]?key|credential)[[:space:]]*[:=]'
    AND provider !~ '^(gh[pousr]_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{20,}|(AKIA|ASIA)[A-Z0-9]{16})$'
  ),
  reference TEXT NOT NULL CHECK (
    length(reference) BETWEEN 1 AND 1024
    AND reference !~ E'[\\\\\\r\\n]'
    AND reference !~ '(^/|^[A-Za-z]:[\\/]|(^|/)\\.\\.?(/|$)|://|[?#])'
    AND reference !~* '(password|passwd|secret|api[_-]?key|access[_-]?token|authorization|private[_-]?key|credential)[[:space:]]*[:=]'
    AND reference !~* '^(client|clients|config|configs|etc|opt|home|var|tmp)/'
    AND reference !~* '\\.(json|ya?ml|env|ini|toml|config|xml|properties)$'
  ),
  version_fingerprint TEXT NOT NULL CHECK (
    version_fingerprint ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT targetconfigsnapshotsecretreference_snapshot_fk
    FOREIGN KEY (target_config_snapshot_id, organization_id)
    REFERENCES TargetConfigSnapshot(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT targetconfigsnapshotsecretreference_snapshot_key_unique
    UNIQUE (target_config_snapshot_id, key)
);

CREATE INDEX TargetConfigSnapshotSecretReference_snapshot_order
  ON TargetConfigSnapshotSecretReference (
    organization_id,
    target_config_snapshot_id,
    key,
    id
  );

CREATE TABLE TargetConfigSnapshotFeatureFlag (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  target_config_snapshot_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  key TEXT NOT NULL CHECK (
    key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
    AND length(key) <= 128
  ),
  enabled BOOLEAN NOT NULL,
  CONSTRAINT targetconfigsnapshotfeatureflag_snapshot_fk
    FOREIGN KEY (target_config_snapshot_id, organization_id)
    REFERENCES TargetConfigSnapshot(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT targetconfigsnapshotfeatureflag_snapshot_key_unique
    UNIQUE (target_config_snapshot_id, key)
);

CREATE INDEX TargetConfigSnapshotFeatureFlag_snapshot_order
  ON TargetConfigSnapshotFeatureFlag (
    organization_id,
    target_config_snapshot_id,
    key,
    id
  );

CREATE FUNCTION target_config_snapshot_reject_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.target_config_snapshot_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;

  RAISE EXCEPTION 'target config snapshots are immutable'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER TargetConfigSnapshot_immutable
BEFORE UPDATE OR DELETE ON TargetConfigSnapshot
FOR EACH ROW EXECUTE FUNCTION target_config_snapshot_reject_mutation();

CREATE TRIGGER TargetConfigSnapshotObject_immutable
BEFORE UPDATE OR DELETE ON TargetConfigSnapshotObject
FOR EACH ROW EXECUTE FUNCTION target_config_snapshot_reject_mutation();

CREATE TRIGGER TargetConfigSnapshotComponent_immutable
BEFORE UPDATE OR DELETE ON TargetConfigSnapshotComponent
FOR EACH ROW EXECUTE FUNCTION target_config_snapshot_reject_mutation();

CREATE TRIGGER TargetConfigSnapshotSecretReference_immutable
BEFORE UPDATE OR DELETE ON TargetConfigSnapshotSecretReference
FOR EACH ROW EXECUTE FUNCTION target_config_snapshot_reject_mutation();

CREATE TRIGGER TargetConfigSnapshotFeatureFlag_immutable
BEFORE UPDATE OR DELETE ON TargetConfigSnapshotFeatureFlag
FOR EACH ROW EXECUTE FUNCTION target_config_snapshot_reject_mutation();
