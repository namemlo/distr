ALTER TABLE DeploymentTarget
  ADD COLUMN platform TEXT NOT NULL DEFAULT 'linux/amd64'
  CHECK (platform IN ('linux/amd64', 'linux/arm64'));

ALTER TABLE DeploymentPlanTarget
  ADD COLUMN platform TEXT NOT NULL DEFAULT 'linux/amd64'
  CHECK (platform IN ('linux/amd64', 'linux/arm64'));

CREATE TABLE DeploymentPlanTargetComponent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  deployment_plan_id UUID NOT NULL,
  deployment_plan_target_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  deployment_target_id UUID NOT NULL,
  component TEXT NOT NULL CHECK (length(trim(component)) > 0),
  version TEXT NOT NULL CHECK (length(trim(version)) > 0),
  image TEXT NOT NULL CHECK (length(trim(image)) > 0),
  platform TEXT NOT NULL CHECK (platform IN ('linux/amd64', 'linux/arm64')),
  contracts TEXT[] NOT NULL DEFAULT '{}',
  config_checksum TEXT NOT NULL CHECK (config_checksum ~ '^sha256:[0-9a-f]{64}$'),
  expected_state_version BIGINT NOT NULL DEFAULT 0 CHECK (expected_state_version >= 0),
  expected_state_checksum TEXT NOT NULL DEFAULT '' CHECK (
    expected_state_checksum = '' OR expected_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  expected_release_bundle_id UUID,
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  CONSTRAINT deploymentplantargetcomponent_plan_target_fk
    FOREIGN KEY (deployment_plan_id, deployment_plan_target_id, organization_id)
    REFERENCES DeploymentPlanTarget(deployment_plan_id, id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT deploymentplantargetcomponent_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT deploymentplantargetcomponent_unique
    UNIQUE (deployment_plan_id, deployment_plan_target_id, component)
);

CREATE TABLE TargetComponentState (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_target_id UUID NOT NULL,
  application_id UUID NOT NULL,
  component TEXT NOT NULL CHECK (length(trim(component)) > 0),
  state_version BIGINT NOT NULL DEFAULT 1 CHECK (state_version > 0),
  state_checksum TEXT NOT NULL CHECK (state_checksum ~ '^sha256:[0-9a-f]{64}$'),
  release_bundle_id UUID NOT NULL,
  version TEXT NOT NULL CHECK (length(trim(version)) > 0),
  image TEXT NOT NULL CHECK (length(trim(image)) > 0),
  platform TEXT NOT NULL CHECK (platform IN ('linux/amd64', 'linux/arm64')),
  contracts TEXT[] NOT NULL DEFAULT '{}',
  config_checksum TEXT NOT NULL CHECK (config_checksum ~ '^sha256:[0-9a-f]{64}$'),
  health TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK (health IN ('UNKNOWN', 'HEALTHY', 'UNHEALTHY')),
  observed_at TIMESTAMP NOT NULL DEFAULT now(),
  CONSTRAINT targetcomponentstate_id_organization_unique UNIQUE (id, organization_id),
  CONSTRAINT targetcomponentstate_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE CASCADE,
  CONSTRAINT targetcomponentstate_application_fk
    FOREIGN KEY (application_id, organization_id)
    REFERENCES Application(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT targetcomponentstate_scope_unique
    UNIQUE (organization_id, deployment_target_id, application_id, component)
);

CREATE TABLE TargetComponentObservation (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  target_component_state_id UUID NOT NULL,
  deployment_target_id UUID NOT NULL,
  application_id UUID NOT NULL,
  component TEXT NOT NULL CHECK (length(trim(component)) > 0),
  state_version BIGINT NOT NULL CHECK (state_version > 0),
  state_checksum TEXT NOT NULL CHECK (state_checksum ~ '^sha256:[0-9a-f]{64}$'),
  release_bundle_id UUID NOT NULL,
  version TEXT NOT NULL CHECK (length(trim(version)) > 0),
  image TEXT NOT NULL CHECK (length(trim(image)) > 0),
  platform TEXT NOT NULL CHECK (platform IN ('linux/amd64', 'linux/arm64')),
  contracts TEXT[] NOT NULL DEFAULT '{}',
  config_checksum TEXT NOT NULL CHECK (config_checksum ~ '^sha256:[0-9a-f]{64}$'),
  health TEXT NOT NULL CHECK (health IN ('UNKNOWN', 'HEALTHY', 'UNHEALTHY')),
  observed_at TIMESTAMP NOT NULL DEFAULT now(),
  external_execution_id UUID,
  CONSTRAINT targetcomponentobservation_state_fk
    FOREIGN KEY (target_component_state_id, organization_id)
    REFERENCES TargetComponentState(id, organization_id)
    ON DELETE CASCADE
);

CREATE INDEX DeploymentPlanTargetComponent_plan_sort
  ON DeploymentPlanTargetComponent (deployment_plan_id, deployment_plan_target_id, sort_order, component);

CREATE INDEX TargetComponentState_target_application
  ON TargetComponentState (organization_id, deployment_target_id, application_id, component);

CREATE INDEX TargetComponentObservation_history
  ON TargetComponentObservation (
    organization_id,
    deployment_target_id,
    application_id,
    component,
    observed_at DESC,
    state_version DESC
  );
