CREATE TABLE Lifecycle (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  CONSTRAINT lifecycle_organization_name_unique UNIQUE (organization_id, name)
);

CREATE TABLE LifecyclePhase (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  lifecycle_id UUID NOT NULL REFERENCES Lifecycle(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  optional BOOLEAN NOT NULL DEFAULT false,
  automatic_promotion BOOLEAN NOT NULL DEFAULT false,
  minimum_successful_deployments INTEGER NOT NULL DEFAULT 0 CHECK (minimum_successful_deployments >= 0),
  approval_policy_id UUID,
  retention_policy_id UUID,
  CONSTRAINT lifecycle_phase_lifecycle_name_unique UNIQUE (lifecycle_id, name),
  CONSTRAINT lifecycle_phase_lifecycle_sort_order_unique UNIQUE (lifecycle_id, sort_order)
);

CREATE TABLE LifecyclePhaseEnvironment (
  lifecycle_phase_id UUID NOT NULL REFERENCES LifecyclePhase(id) ON DELETE CASCADE,
  environment_id UUID NOT NULL REFERENCES Environment(id) ON DELETE RESTRICT,
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  PRIMARY KEY (lifecycle_phase_id, environment_id)
);

CREATE INDEX Lifecycle_organization_sort_name
  ON Lifecycle (organization_id, sort_order, name);

CREATE INDEX LifecyclePhase_lifecycle_sort_name
  ON LifecyclePhase (lifecycle_id, sort_order, name);

CREATE INDEX LifecyclePhaseEnvironment_environment
  ON LifecyclePhaseEnvironment (environment_id);
