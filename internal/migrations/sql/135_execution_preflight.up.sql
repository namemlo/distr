ALTER TABLE TaskResourceLock
  DROP CONSTRAINT IF EXISTS taskresourcelock_resource_type_check;

ALTER TABLE TaskResourceLock
  ADD CONSTRAINT taskresourcelock_resource_type_check
  CHECK (resource_type IN (
    'deployment_target',
    'target_component',
    'tenant_environment',
    'application_environment',
    'custom'
  ));

CREATE TABLE DeploymentPreflightRun (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_plan_id UUID NOT NULL,
  plan_checksum TEXT NOT NULL CHECK (plan_checksum ~ '^sha256:[0-9a-f]{64}$'),
  actor_user_account_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL,
  status TEXT NOT NULL CHECK (status IN ('PASSED', 'FAILED')),
  CONSTRAINT deploymentpreflightrun_id_organization_unique UNIQUE (id, organization_id),
  CONSTRAINT deploymentpreflightrun_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON DELETE CASCADE
);

CREATE TABLE DeploymentPreflightCheck (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_preflight_run_id UUID NOT NULL,
  deployment_plan_id UUID NOT NULL,
  deployment_plan_target_id UUID,
  deployment_target_id UUID,
  task_id UUID,
  component TEXT NOT NULL DEFAULT '',
  check_key TEXT NOT NULL CHECK (length(trim(check_key)) > 0),
  status TEXT NOT NULL CHECK (status IN ('PASSED', 'FAILED')),
  expected JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(expected) = 'object'),
  actual JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(actual) = 'object'),
  message TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  CONSTRAINT deploymentpreflightcheck_run_fk
    FOREIGN KEY (deployment_preflight_run_id, organization_id)
    REFERENCES DeploymentPreflightRun(id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT deploymentpreflightcheck_plan_target_fk
    FOREIGN KEY (deployment_plan_id, deployment_plan_target_id, organization_id)
    REFERENCES DeploymentPlanTarget(deployment_plan_id, id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE SET NULL (deployment_plan_target_id),
  CONSTRAINT deploymentpreflightcheck_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE SET NULL (deployment_target_id),
  CONSTRAINT deploymentpreflightcheck_task_fk
    FOREIGN KEY (task_id, organization_id)
    REFERENCES Task(id, organization_id)
    ON DELETE SET NULL (task_id)
);

CREATE INDEX DeploymentPreflightRun_plan_created
  ON DeploymentPreflightRun (deployment_plan_id, created_at DESC, id);

CREATE INDEX DeploymentPreflightCheck_run_sort
  ON DeploymentPreflightCheck (deployment_preflight_run_id, sort_order, id);
