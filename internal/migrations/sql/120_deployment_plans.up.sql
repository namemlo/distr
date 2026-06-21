CREATE TABLE DeploymentPlan (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  release_bundle_id UUID NOT NULL,
  application_id UUID NOT NULL,
  channel_id UUID NOT NULL,
  environment_id UUID NOT NULL,
  process_snapshot_id UUID,
  variable_snapshot_id UUID,
  status TEXT NOT NULL CHECK (
    status IN ('DRAFT', 'VALIDATING', 'BLOCKED', 'READY', 'EXPIRED', 'EXECUTED')
  ),
  canonical_checksum TEXT NOT NULL,
  canonical_payload BYTEA NOT NULL,
  CONSTRAINT deploymentplan_id_organization_unique UNIQUE (id, organization_id),
  CONSTRAINT deploymentplan_release_bundle_fk
    FOREIGN KEY (release_bundle_id, application_id, channel_id, organization_id)
    REFERENCES ReleaseBundle(id, application_id, channel_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT deploymentplan_environment_fk
    FOREIGN KEY (environment_id, organization_id)
    REFERENCES Environment(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT deploymentplan_process_snapshot_fk
    FOREIGN KEY (process_snapshot_id, application_id, organization_id)
    REFERENCES ProcessSnapshot(id, application_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT deploymentplan_variable_snapshot_fk
    FOREIGN KEY (variable_snapshot_id, application_id, channel_id, organization_id)
    REFERENCES VariableSnapshot(id, application_id, channel_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT
);

CREATE TABLE DeploymentPlanTarget (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  deployment_plan_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  deployment_target_id UUID NOT NULL,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  customer_organization_id UUID,
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  CONSTRAINT deploymentplantarget_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT deploymentplantarget_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT deploymentplantarget_customer_org_fk
    FOREIGN KEY (customer_organization_id, organization_id)
    REFERENCES CustomerOrganization(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT deploymentplantarget_plan_target_unique UNIQUE (deployment_plan_id, deployment_target_id)
);

CREATE TABLE DeploymentPlanStep (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  deployment_plan_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  step_key TEXT NOT NULL,
  name TEXT NOT NULL,
  action_type TEXT NOT NULL,
  action_name TEXT NOT NULL,
  execution_location TEXT NOT NULL,
  input_bindings JSONB NOT NULL DEFAULT '{}'::jsonb,
  condition TEXT NOT NULL DEFAULT '',
  target_tags TEXT[] NOT NULL DEFAULT '{}',
  failure_mode TEXT NOT NULL DEFAULT '',
  timeout_seconds INTEGER NOT NULL DEFAULT 0 CHECK (timeout_seconds >= 0),
  retry_max_attempts INTEGER NOT NULL DEFAULT 0 CHECK (retry_max_attempts >= 0),
  retry_interval_seconds INTEGER NOT NULL DEFAULT 0 CHECK (retry_interval_seconds >= 0),
  required_permissions TEXT[] NOT NULL DEFAULT '{}',
  sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
  dependencies TEXT[] NOT NULL DEFAULT '{}',
  included BOOLEAN NOT NULL DEFAULT true,
  excluded_reason TEXT NOT NULL DEFAULT '',
  CONSTRAINT deploymentplanstep_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT deploymentplanstep_plan_key_unique UNIQUE (deployment_plan_id, step_key)
);

CREATE TABLE DeploymentPlanVariable (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  deployment_plan_id UUID NOT NULL,
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
  CONSTRAINT deploymentplanvariable_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT deploymentplanvariable_variable_fk
    FOREIGN KEY (variable_id, variable_set_id, organization_id)
    REFERENCES Variable(id, variable_set_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT deploymentplanvariable_plan_variable_unique UNIQUE (deployment_plan_id, variable_id),
  CONSTRAINT deploymentplanvariable_redaction_check CHECK (NOT redacted OR value IS NULL)
);

CREATE TABLE DeploymentPlanIssue (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  deployment_plan_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  severity TEXT NOT NULL CHECK (severity IN ('blocker', 'warning')),
  code TEXT NOT NULL,
  field TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  CONSTRAINT deploymentplanissue_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON DELETE CASCADE
);

CREATE INDEX DeploymentPlan_organization_created
  ON DeploymentPlan (organization_id, created_at DESC, id);

CREATE INDEX DeploymentPlan_release_bundle
  ON DeploymentPlan (release_bundle_id);

CREATE INDEX DeploymentPlanTarget_plan_sort
  ON DeploymentPlanTarget (deployment_plan_id, sort_order, deployment_target_id);

CREATE INDEX DeploymentPlanStep_plan_sort
  ON DeploymentPlanStep (deployment_plan_id, sort_order, step_key);

CREATE INDEX DeploymentPlanVariable_plan_key
  ON DeploymentPlanVariable (deployment_plan_id, key);

CREATE INDEX DeploymentPlanIssue_plan_sort
  ON DeploymentPlanIssue (deployment_plan_id, severity, sort_order, code);
