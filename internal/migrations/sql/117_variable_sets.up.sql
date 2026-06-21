ALTER TABLE Secret
  ADD CONSTRAINT secret_id_organization_unique UNIQUE (id, organization_id);

CREATE TABLE VariableSet (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  CONSTRAINT variableset_organization_name_unique UNIQUE (organization_id, name),
  CONSTRAINT variableset_id_organization_unique UNIQUE (id, organization_id)
);

CREATE TABLE VariableSetApplication (
  variable_set_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  application_id UUID NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  PRIMARY KEY (variable_set_id, application_id),
  CONSTRAINT variablesetapplication_variable_set_fk
    FOREIGN KEY (variable_set_id, organization_id)
    REFERENCES VariableSet(id, organization_id) ON DELETE CASCADE,
  CONSTRAINT variablesetapplication_application_fk
    FOREIGN KEY (application_id, organization_id)
    REFERENCES Application(id, organization_id) ON DELETE CASCADE
);

CREATE TABLE Variable (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  variable_set_id UUID NOT NULL,
  key TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
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
  default_value JSONB,
  secret_reference_id UUID,
  reference_id TEXT,
  reference_name TEXT NOT NULL DEFAULT '',
  CONSTRAINT variable_variable_set_fk
    FOREIGN KEY (variable_set_id, organization_id)
    REFERENCES VariableSet(id, organization_id) ON DELETE CASCADE,
  CONSTRAINT variable_secret_reference_org_fk
    FOREIGN KEY (secret_reference_id, organization_id)
    REFERENCES Secret(id, organization_id) ON DELETE RESTRICT,
  CONSTRAINT variable_variable_set_key_unique UNIQUE (variable_set_id, key),
  CONSTRAINT variable_value_type_check CHECK (
    (
      type = 'string'
      AND secret_reference_id IS NULL
      AND reference_id IS NULL
      AND (default_value IS NULL OR jsonb_typeof(default_value) = 'string')
    )
    OR (
      type = 'number'
      AND secret_reference_id IS NULL
      AND reference_id IS NULL
      AND (default_value IS NULL OR jsonb_typeof(default_value) = 'number')
    )
    OR (
      type = 'boolean'
      AND secret_reference_id IS NULL
      AND reference_id IS NULL
      AND (default_value IS NULL OR jsonb_typeof(default_value) = 'boolean')
    )
    OR (
      type = 'json'
      AND secret_reference_id IS NULL
      AND reference_id IS NULL
      AND (default_value IS NULL OR jsonb_typeof(default_value) <> 'null')
    )
    OR (
      type = 'secret_reference'
      AND default_value IS NULL
      AND reference_id IS NULL
    )
    OR (
      type IN ('account_reference', 'certificate_reference')
      AND default_value IS NULL
      AND secret_reference_id IS NULL
    )
  )
);

CREATE INDEX VariableSet_organization_sort_name
  ON VariableSet (organization_id, sort_order, name, id);

CREATE INDEX VariableSetApplication_application
  ON VariableSetApplication (organization_id, application_id);

CREATE INDEX Variable_variable_set_key
  ON Variable (variable_set_id, key);

CREATE INDEX Variable_secret_reference
  ON Variable (secret_reference_id)
  WHERE secret_reference_id IS NOT NULL;
