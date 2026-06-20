ALTER TABLE Application
  ADD CONSTRAINT application_id_organization_unique UNIQUE (id, organization_id);

CREATE TABLE DeploymentProcess (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  application_id UUID NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  CONSTRAINT deploymentprocess_organization_application_name_unique UNIQUE (organization_id, application_id, name),
  CONSTRAINT deploymentprocess_id_organization_unique UNIQUE (id, organization_id),
  CONSTRAINT deploymentprocess_application_organization_fk FOREIGN KEY (application_id, organization_id)
    REFERENCES Application(id, organization_id) ON DELETE CASCADE
);

CREATE INDEX DeploymentProcess_organization_application_sort_name
  ON DeploymentProcess (organization_id, application_id, sort_order, name);

CREATE TABLE DeploymentProcessRevision (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_process_id UUID NOT NULL,
  revision_number INTEGER NOT NULL CHECK (revision_number > 0),
  description TEXT NOT NULL DEFAULT '',
  CONSTRAINT deploymentprocessrevision_process_number_unique UNIQUE (deployment_process_id, revision_number),
  CONSTRAINT deploymentprocessrevision_id_process_unique UNIQUE (id, deployment_process_id),
  CONSTRAINT deploymentprocessrevision_process_organization_fk FOREIGN KEY (deployment_process_id, organization_id)
    REFERENCES DeploymentProcess(id, organization_id) ON DELETE CASCADE
);

CREATE INDEX DeploymentProcessRevision_process_number
  ON DeploymentProcessRevision (deployment_process_id, revision_number);

CREATE TABLE DeploymentProcessStep (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  deployment_process_revision_id UUID NOT NULL REFERENCES DeploymentProcessRevision(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  name TEXT NOT NULL,
  action_type TEXT NOT NULL,
  step_template_version_id UUID,
  execution_location TEXT NOT NULL,
  input_bindings JSONB NOT NULL DEFAULT '{}',
  condition TEXT NOT NULL DEFAULT '',
  target_tags TEXT[] NOT NULL DEFAULT '{}',
  failure_mode TEXT NOT NULL DEFAULT 'fail',
  timeout_seconds INTEGER NOT NULL DEFAULT 0 CHECK (timeout_seconds >= 0),
  retry_max_attempts INTEGER NOT NULL DEFAULT 0 CHECK (retry_max_attempts >= 0),
  retry_interval_seconds INTEGER NOT NULL DEFAULT 0 CHECK (retry_interval_seconds >= 0),
  required_permissions TEXT[] NOT NULL DEFAULT '{}',
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  CONSTRAINT deploymentprocessstep_revision_key_unique UNIQUE (deployment_process_revision_id, key),
  CONSTRAINT deploymentprocessstep_revision_sort_order_unique UNIQUE (deployment_process_revision_id, sort_order)
);

CREATE INDEX DeploymentProcessStep_revision_sort_key
  ON DeploymentProcessStep (deployment_process_revision_id, sort_order, key);

CREATE TABLE DeploymentProcessStepDependency (
  deployment_process_revision_id UUID NOT NULL,
  step_key TEXT NOT NULL,
  depends_on_step_key TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  PRIMARY KEY (deployment_process_revision_id, step_key, depends_on_step_key),
  CONSTRAINT deploymentprocessstepdependency_no_self CHECK (step_key <> depends_on_step_key),
  CONSTRAINT deploymentprocessstepdependency_step_fk FOREIGN KEY (deployment_process_revision_id, step_key)
    REFERENCES DeploymentProcessStep(deployment_process_revision_id, key) ON DELETE CASCADE,
  CONSTRAINT deploymentprocessstepdependency_depends_on_fk FOREIGN KEY (deployment_process_revision_id, depends_on_step_key)
    REFERENCES DeploymentProcessStep(deployment_process_revision_id, key) ON DELETE CASCADE
);

CREATE INDEX DeploymentProcessStepDependency_revision_step_sort
  ON DeploymentProcessStepDependency (deployment_process_revision_id, step_key, sort_order);

CREATE TABLE DeploymentProcessStepChannel (
  deployment_process_step_id UUID NOT NULL REFERENCES DeploymentProcessStep(id) ON DELETE CASCADE,
  organization_id UUID NOT NULL,
  application_id UUID NOT NULL,
  channel_id UUID NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  PRIMARY KEY (deployment_process_step_id, channel_id),
  CONSTRAINT deploymentprocessstepchannel_channel_application_organization_fk
    FOREIGN KEY (channel_id, application_id, organization_id)
    REFERENCES Channel(id, application_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT
);

CREATE INDEX DeploymentProcessStepChannel_channel
  ON DeploymentProcessStepChannel (organization_id, application_id, channel_id);

CREATE TABLE DeploymentProcessStepEnvironment (
  deployment_process_step_id UUID NOT NULL REFERENCES DeploymentProcessStep(id) ON DELETE CASCADE,
  environment_id UUID NOT NULL REFERENCES Environment(id) ON DELETE RESTRICT,
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  PRIMARY KEY (deployment_process_step_id, environment_id)
);

CREATE INDEX DeploymentProcessStepEnvironment_environment
  ON DeploymentProcessStepEnvironment (environment_id);
