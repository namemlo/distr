CREATE SEQUENCE Task_queue_order_seq;

ALTER TABLE DeploymentPlanTarget
  ADD CONSTRAINT deploymentplantarget_plan_id_organization_unique
  UNIQUE (deployment_plan_id, id, organization_id);

ALTER TABLE DeploymentPlanStep
  ADD CONSTRAINT deploymentplanstep_plan_id_organization_unique
  UNIQUE (deployment_plan_id, id, organization_id);

CREATE TABLE Task (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  queued_at TIMESTAMP NOT NULL DEFAULT now(),
  started_at TIMESTAMP,
  completed_at TIMESTAMP,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_plan_id UUID NOT NULL,
  deployment_plan_target_id UUID NOT NULL,
  deployment_target_id UUID NOT NULL,
  application_id UUID NOT NULL,
  release_bundle_id UUID NOT NULL,
  channel_id UUID NOT NULL,
  environment_id UUID NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED')),
  queue_order BIGINT NOT NULL DEFAULT nextval('Task_queue_order_seq'),
  CONSTRAINT task_id_organization_unique UNIQUE (id, organization_id),
  CONSTRAINT task_id_plan_organization_unique UNIQUE (id, deployment_plan_id, organization_id),
  CONSTRAINT task_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT task_plan_target_fk
    FOREIGN KEY (deployment_plan_id, deployment_plan_target_id, organization_id)
    REFERENCES DeploymentPlanTarget(deployment_plan_id, id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT task_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT task_plan_target_unique UNIQUE (deployment_plan_id, deployment_plan_target_id)
);

CREATE TABLE StepRun (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  started_at TIMESTAMP,
  completed_at TIMESTAMP,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  task_id UUID NOT NULL,
  deployment_plan_id UUID NOT NULL,
  deployment_plan_step_id UUID NOT NULL,
  step_key TEXT NOT NULL,
  name TEXT NOT NULL,
  action_type TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'SKIPPED')),
  sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
  skipped_reason TEXT NOT NULL DEFAULT '',
  CONSTRAINT steprun_task_fk
    FOREIGN KEY (task_id, deployment_plan_id, organization_id)
    REFERENCES Task(id, deployment_plan_id, organization_id)
    ON DELETE CASCADE,
  CONSTRAINT steprun_plan_step_fk
    FOREIGN KEY (deployment_plan_id, deployment_plan_step_id, organization_id)
    REFERENCES DeploymentPlanStep(deployment_plan_id, id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT steprun_task_step_unique UNIQUE (task_id, deployment_plan_step_id)
);

CREATE INDEX Task_organization_queue
  ON Task (organization_id, status, queue_order, id);

CREATE INDEX Task_deployment_plan
  ON Task (deployment_plan_id, queue_order, id);

CREATE INDEX StepRun_task_sort
  ON StepRun (task_id, sort_order, step_key);

CREATE INDEX StepRun_organization_status
  ON StepRun (organization_id, status, updated_at DESC, id);
