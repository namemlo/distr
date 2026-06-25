CREATE TABLE DeploymentCompatibilityMetadata (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  legacy_deployment_id UUID NOT NULL REFERENCES Deployment(id) ON DELETE CASCADE,
  legacy_deployment_revision_id UUID NOT NULL REFERENCES DeploymentRevision(id) ON DELETE CASCADE,
  deployment_target_id UUID NOT NULL REFERENCES DeploymentTarget(id) ON DELETE CASCADE,
  application_id UUID NOT NULL REFERENCES Application(id) ON DELETE CASCADE,
  application_version_id UUID NOT NULL REFERENCES ApplicationVersion(id) ON DELETE RESTRICT,
  synthetic_release_id UUID NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('legacy_direct_deployment')),
  canonical_checksum TEXT NOT NULL,
  canonical_payload BYTEA NOT NULL,
  process_snapshot_available BOOLEAN NOT NULL DEFAULT false,
  variable_snapshot_available BOOLEAN NOT NULL DEFAULT false,
  channel_available BOOLEAN NOT NULL DEFAULT false,
  environment_available BOOLEAN NOT NULL DEFAULT false,
  task_logs_available BOOLEAN NOT NULL DEFAULT false,
  redeploy_plan_available BOOLEAN NOT NULL DEFAULT false,
  CONSTRAINT deploymentcompatibility_revision_unique
    UNIQUE (organization_id, legacy_deployment_revision_id),
  CONSTRAINT deploymentcompatibility_synthetic_release_unique
    UNIQUE (organization_id, synthetic_release_id)
);

CREATE INDEX DeploymentCompatibilityMetadata_organization_created
  ON DeploymentCompatibilityMetadata (organization_id, created_at, legacy_deployment_revision_id);

CREATE INDEX DeploymentCompatibilityMetadata_target_created
  ON DeploymentCompatibilityMetadata (organization_id, deployment_target_id, created_at, legacy_deployment_revision_id);

CREATE INDEX DeploymentCompatibilityMetadata_application_created
  ON DeploymentCompatibilityMetadata (organization_id, application_id, created_at, legacy_deployment_revision_id);
