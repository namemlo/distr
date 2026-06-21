ALTER TABLE ReleaseBundle
  ADD CONSTRAINT releasebundle_id_application_channel_organization_unique
  UNIQUE (id, application_id, channel_id, organization_id);

CREATE TABLE VariableSnapshot (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  release_bundle_id UUID NOT NULL,
  application_id UUID NOT NULL,
  channel_id UUID NOT NULL,
  canonical_checksum TEXT NOT NULL,
  canonical_payload BYTEA NOT NULL,
  CONSTRAINT variablesnapshot_release_bundle_unique UNIQUE (release_bundle_id),
  CONSTRAINT variablesnapshot_id_organization_unique UNIQUE (id, organization_id),
  CONSTRAINT variablesnapshot_id_application_channel_organization_unique
    UNIQUE (id, application_id, channel_id, organization_id),
  CONSTRAINT variablesnapshot_release_bundle_fk
    FOREIGN KEY (release_bundle_id, application_id, channel_id, organization_id)
    REFERENCES ReleaseBundle(id, application_id, channel_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT
);

CREATE TABLE VariableSnapshotValue (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  variable_snapshot_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  variable_set_id UUID NOT NULL,
  variable_id UUID NOT NULL,
  key TEXT NOT NULL,
  type TEXT NOT NULL CHECK (
    type IN (
      'string',
      'number',
      'boolean',
      'json',
      'secret_reference',
      'account_reference',
      'certificate_reference'
    )
  ),
  is_required BOOLEAN NOT NULL DEFAULT false,
  status TEXT NOT NULL CHECK (status IN ('resolved', 'unresolved')),
  source TEXT NOT NULL,
  value JSONB,
  reference_id TEXT NOT NULL DEFAULT '',
  reference_name TEXT NOT NULL DEFAULT '',
  redacted BOOLEAN NOT NULL DEFAULT false,
  trace JSONB NOT NULL DEFAULT '[]'::jsonb,
  CONSTRAINT variablesnapshotvalue_snapshot_fk
    FOREIGN KEY (variable_snapshot_id, organization_id)
    REFERENCES VariableSnapshot(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT variablesnapshotvalue_snapshot_variable_unique UNIQUE (variable_snapshot_id, variable_id),
  CONSTRAINT variablesnapshotvalue_secret_redaction_check CHECK (NOT redacted OR value IS NULL)
);

ALTER TABLE ReleaseBundle
  ADD COLUMN variable_snapshot_id UUID,
  ADD CONSTRAINT releasebundle_variable_snapshot_application_channel_organization_fk
    FOREIGN KEY (variable_snapshot_id, application_id, channel_id, organization_id)
    REFERENCES VariableSnapshot(id, application_id, channel_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT;

CREATE INDEX VariableSnapshot_organization_application_created
  ON VariableSnapshot (organization_id, application_id, created_at, id);

CREATE INDEX VariableSnapshot_release_bundle
  ON VariableSnapshot (release_bundle_id);

CREATE INDEX VariableSnapshotValue_snapshot_key
  ON VariableSnapshotValue (variable_snapshot_id, key);

CREATE INDEX ReleaseBundle_variable_snapshot_idx
  ON ReleaseBundle (variable_snapshot_id);
