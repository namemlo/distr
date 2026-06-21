ALTER TABLE Variable
  ADD CONSTRAINT variable_id_set_organization_unique UNIQUE (id, variable_set_id, organization_id);

ALTER TABLE Environment
  ADD CONSTRAINT environment_id_organization_unique UNIQUE (id, organization_id);

ALTER TABLE Channel
  ADD CONSTRAINT channel_id_organization_unique UNIQUE (id, organization_id);

ALTER TABLE DeploymentTarget
  ADD CONSTRAINT deploymenttarget_id_organization_unique UNIQUE (id, organization_id);

ALTER TABLE CustomerOrganization
  ADD CONSTRAINT customerorganization_id_organization_unique UNIQUE (id, organization_id);

CREATE TABLE VariableScopedValue (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  variable_set_id UUID NOT NULL,
  variable_id UUID NOT NULL,
  customer_organization_id UUID,
  environment_id UUID,
  channel_id UUID,
  deployment_target_id UUID,
  application_id UUID,
  target_tag TEXT NOT NULL DEFAULT '',
  process_step_key TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  value JSONB,
  secret_reference_id UUID,
  reference_id TEXT,
  reference_name TEXT NOT NULL DEFAULT '',
  CONSTRAINT variablescopedvalue_variable_fk
    FOREIGN KEY (variable_id, variable_set_id, organization_id)
    REFERENCES Variable(id, variable_set_id, organization_id) ON DELETE CASCADE,
  CONSTRAINT variablescopedvalue_customer_org_fk
    FOREIGN KEY (customer_organization_id, organization_id)
    REFERENCES CustomerOrganization(id, organization_id) ON DELETE RESTRICT,
  CONSTRAINT variablescopedvalue_environment_fk
    FOREIGN KEY (environment_id, organization_id)
    REFERENCES Environment(id, organization_id) ON DELETE RESTRICT,
  CONSTRAINT variablescopedvalue_channel_fk
    FOREIGN KEY (channel_id, organization_id)
    REFERENCES Channel(id, organization_id) ON DELETE RESTRICT,
  CONSTRAINT variablescopedvalue_deployment_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id) ON DELETE RESTRICT,
  CONSTRAINT variablescopedvalue_application_fk
    FOREIGN KEY (application_id, organization_id)
    REFERENCES Application(id, organization_id) ON DELETE RESTRICT,
  CONSTRAINT variablescopedvalue_secret_reference_org_fk
    FOREIGN KEY (secret_reference_id, organization_id)
    REFERENCES Secret(id, organization_id) ON DELETE RESTRICT,
  CONSTRAINT variablescopedvalue_payload_check CHECK (
    ((value IS NOT NULL)::integer +
     (secret_reference_id IS NOT NULL)::integer +
     (reference_id IS NOT NULL)::integer) = 1
  ),
  CONSTRAINT variablescopedvalue_scope_shape_check CHECK (
    (
      customer_organization_id IS NOT NULL
      AND environment_id IS NOT NULL
      AND deployment_target_id IS NOT NULL
      AND channel_id IS NOT NULL
      AND process_step_key <> ''
      AND application_id IS NULL
      AND target_tag = ''
    )
    OR (
      customer_organization_id IS NOT NULL
      AND environment_id IS NOT NULL
      AND deployment_target_id IS NOT NULL
      AND channel_id IS NULL
      AND process_step_key = ''
      AND application_id IS NULL
      AND target_tag = ''
    )
    OR (
      customer_organization_id IS NOT NULL
      AND environment_id IS NOT NULL
      AND deployment_target_id IS NULL
      AND channel_id IS NOT NULL
      AND process_step_key = ''
      AND application_id IS NULL
      AND target_tag = ''
    )
    OR (
      customer_organization_id IS NOT NULL
      AND environment_id IS NOT NULL
      AND deployment_target_id IS NULL
      AND channel_id IS NULL
      AND process_step_key = ''
      AND application_id IS NULL
      AND target_tag = ''
    )
    OR (
      customer_organization_id IS NULL
      AND environment_id IS NOT NULL
      AND deployment_target_id IS NULL
      AND channel_id IS NULL
      AND process_step_key = ''
      AND application_id IS NULL
      AND target_tag <> ''
    )
    OR (
      customer_organization_id IS NULL
      AND environment_id IS NOT NULL
      AND deployment_target_id IS NULL
      AND channel_id IS NULL
      AND process_step_key = ''
      AND application_id IS NULL
      AND target_tag = ''
    )
    OR (
      customer_organization_id IS NULL
      AND environment_id IS NULL
      AND deployment_target_id IS NULL
      AND channel_id IS NOT NULL
      AND process_step_key = ''
      AND application_id IS NULL
      AND target_tag = ''
    )
    OR (
      customer_organization_id IS NULL
      AND environment_id IS NULL
      AND deployment_target_id IS NULL
      AND channel_id IS NULL
      AND process_step_key = ''
      AND application_id IS NOT NULL
      AND target_tag = ''
    )
  ),
  CONSTRAINT variablescopedvalue_scope_unique UNIQUE NULLS NOT DISTINCT (
    variable_id,
    customer_organization_id,
    environment_id,
    channel_id,
    deployment_target_id,
    application_id,
    target_tag,
    process_step_key
  )
);

CREATE INDEX VariableScopedValue_variable_sort
  ON VariableScopedValue (variable_id, sort_order, id);

CREATE INDEX VariableScopedValue_scope_lookup
  ON VariableScopedValue (
    organization_id,
    application_id,
    channel_id,
    environment_id,
    deployment_target_id,
    customer_organization_id
  );

CREATE INDEX VariableScopedValue_secret_reference
  ON VariableScopedValue (secret_reference_id)
  WHERE secret_reference_id IS NOT NULL;
