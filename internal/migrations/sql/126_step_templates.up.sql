CREATE TABLE StepTemplate (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  source_type TEXT NOT NULL CHECK (source_type IN ('builtin', 'oci')),
  source_ref TEXT NOT NULL CHECK (length(trim(source_ref)) > 0),
  name TEXT NOT NULL CHECK (length(trim(name)) > 0),
  description TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT '',
  installed_at TIMESTAMP NOT NULL DEFAULT now(),
  installed_by_useraccount_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL,
  CONSTRAINT steptemplate_id_organization_unique UNIQUE (id, organization_id),
  CONSTRAINT steptemplate_organization_source_unique UNIQUE (organization_id, source_type, source_ref)
);

CREATE TABLE StepTemplateVersion (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  step_template_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  version TEXT NOT NULL CHECK (length(trim(version)) > 0),
  action_type TEXT NOT NULL CHECK (length(trim(action_type)) > 0),
  execution_location TEXT NOT NULL CHECK (length(trim(execution_location)) > 0),
  input_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
  output_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
  default_input_bindings JSONB NOT NULL DEFAULT '{}'::jsonb,
  minimum_agent_version TEXT NOT NULL DEFAULT '',
  compatible_action_version TEXT NOT NULL DEFAULT '',
  runtime_compatibility_notes TEXT NOT NULL DEFAULT '',
  deprecated BOOLEAN NOT NULL DEFAULT false,
  CONSTRAINT steptemplateversion_template_fk
    FOREIGN KEY (step_template_id, organization_id)
    REFERENCES StepTemplate(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT steptemplateversion_template_version_unique UNIQUE (step_template_id, version),
  CONSTRAINT steptemplateversion_id_organization_unique UNIQUE (id, organization_id)
);

CREATE INDEX StepTemplate_organization_name
  ON StepTemplate (organization_id, category, name, source_ref);

CREATE INDEX StepTemplateVersion_template_version
  ON StepTemplateVersion (step_template_id, version);
