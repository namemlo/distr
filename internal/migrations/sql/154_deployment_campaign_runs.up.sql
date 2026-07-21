CREATE TABLE DeploymentCampaignRun (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  campaign_revision_id UUID NOT NULL,
  state TEXT NOT NULL DEFAULT 'DRAFT' CHECK (
    state IN (
      'DRAFT',
      'VALIDATED',
      'AWAITING_APPROVAL',
      'SCHEDULED',
      'RUNNING',
      'PAUSED',
      'FAILED',
      'COMPLETED',
      'CANCELED'
    )
  ),
  version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
  current_wave_order INTEGER NOT NULL DEFAULT 0 CHECK (current_wave_order >= 0),
  current_member_order INTEGER NOT NULL DEFAULT 0 CHECK (current_member_order >= 0),
  admissions_blocked BOOLEAN NOT NULL DEFAULT FALSE,
  lease_holder TEXT,
  lease_expires_at TIMESTAMPTZ,
  fencing_token BIGINT NOT NULL DEFAULT 0 CHECK (fencing_token >= 0),
  transition_evidence JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (
    jsonb_typeof(transition_evidence) = 'array'
    AND pg_column_size(transition_evidence) <= 1048576
  ),
  CONSTRAINT deploymentcampaignrun_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT deploymentcampaignrun_revision_fk
    FOREIGN KEY (campaign_revision_id, organization_id)
    REFERENCES DeploymentCampaignRevision(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignrun_lease_complete CHECK (
    (lease_holder IS NULL AND lease_expires_at IS NULL)
    OR (lease_holder IS NOT NULL AND lease_expires_at IS NOT NULL)
  )
);

CREATE INDEX DeploymentCampaignRun_schedulable
  ON DeploymentCampaignRun (state, admissions_blocked, lease_expires_at, id)
  WHERE state IN ('SCHEDULED', 'RUNNING', 'PAUSED');

CREATE TABLE DeploymentCampaignWaveRun (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  campaign_run_id UUID NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  campaign_wave_id UUID NOT NULL,
  campaign_revision_id UUID NOT NULL,
  wave_order INTEGER NOT NULL CHECK (wave_order > 0),
  maximum_concurrency INTEGER NOT NULL CHECK (
    maximum_concurrency BETWEEN 1 AND 1000
  ),
  status TEXT NOT NULL DEFAULT 'PENDING' CHECK (
    status IN ('PENDING', 'RUNNING', 'BAKING', 'PAUSED', 'FAILED', 'COMPLETED', 'CANCELED')
  ),
  bake_duration_seconds BIGINT NOT NULL CHECK (bake_duration_seconds >= 0),
  started_at TIMESTAMPTZ,
  bake_started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  CONSTRAINT deploymentcampaignwaverun_run_fk
    FOREIGN KEY (campaign_run_id, organization_id)
    REFERENCES DeploymentCampaignRun(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT deploymentcampaignwaverun_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT deploymentcampaignwaverun_run_identity_unique
    UNIQUE (id, organization_id, campaign_run_id),
  CONSTRAINT deploymentcampaignwaverun_wave_fk
    FOREIGN KEY (campaign_wave_id)
    REFERENCES DeploymentCampaignWave(id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentcampaignwaverun_frozen_wave_fk
    FOREIGN KEY (campaign_revision_id, wave_order, organization_id)
    REFERENCES DeploymentCampaignWave(
      campaign_revision_id,
      wave_order,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentcampaignwaverun_wave_unique
    UNIQUE (campaign_run_id, campaign_wave_id),
  CONSTRAINT deploymentcampaignwaverun_order_unique
    UNIQUE (campaign_run_id, wave_order)
);

CREATE TABLE DeploymentCampaignMemberRun (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  campaign_run_id UUID NOT NULL,
  wave_run_id UUID NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  campaign_member_id UUID NOT NULL,
  campaign_revision_id UUID NOT NULL,
  deployment_plan_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  wave_order INTEGER NOT NULL CHECK (wave_order > 0),
  member_order INTEGER NOT NULL CHECK (member_order > 0),
  status TEXT NOT NULL DEFAULT 'PENDING' CHECK (
    status IN ('PENDING', 'ADMITTED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'EXCLUDED', 'CANCELED')
  ),
  admitted_at TIMESTAMPTZ,
  admitted_fencing_token BIGINT CHECK (admitted_fencing_token > 0),
  completed_at TIMESTAMPTZ,
  CONSTRAINT deploymentcampaignmemberrun_run_fk
    FOREIGN KEY (campaign_run_id, organization_id)
    REFERENCES DeploymentCampaignRun(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT deploymentcampaignmemberrun_wave_run_fk
    FOREIGN KEY (wave_run_id, organization_id, campaign_run_id)
    REFERENCES DeploymentCampaignWaveRun(id, organization_id, campaign_run_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT deploymentcampaignmemberrun_member_fk
    FOREIGN KEY (campaign_member_id)
    REFERENCES DeploymentCampaignMember(id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentcampaignmemberrun_frozen_member_fk
    FOREIGN KEY (
      campaign_revision_id,
      deployment_plan_id,
      organization_id
    )
    REFERENCES DeploymentCampaignMember(
      campaign_revision_id,
      deployment_plan_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT deploymentcampaignmemberrun_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE RESTRICT,
  CONSTRAINT deploymentcampaignmemberrun_id_run_organization_unique
    UNIQUE (id, organization_id, campaign_run_id),
  CONSTRAINT deploymentcampaignmemberrun_order_unique
    UNIQUE (campaign_run_id, wave_order, member_order, deployment_plan_id)
);

CREATE INDEX DeploymentCampaignMemberRun_next
  ON DeploymentCampaignMemberRun (
    campaign_run_id,
    wave_order,
    member_order,
    deployment_plan_id
  )
  WHERE status = 'PENDING';

CREATE TABLE CampaignPrerequisiteEvaluation (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  evaluated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  campaign_run_id UUID NOT NULL,
  member_run_id UUID NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  upstream_plan_id UUID NOT NULL,
  step_key TEXT NOT NULL CHECK (step_key = btrim(step_key) AND length(step_key) BETWEEN 1 AND 200),
  expected_runtime_state_checksum TEXT NOT NULL CHECK (
    expected_runtime_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  actual_observation_id UUID,
  actual_observation_organization_id UUID,
  actual_runtime_state_checksum TEXT CHECK (
    actual_runtime_state_checksum IS NULL
    OR actual_runtime_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  matched BOOLEAN NOT NULL,
  reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4000),
  fencing_token BIGINT NOT NULL CHECK (fencing_token > 0),
  CONSTRAINT campaignprerequisiteevaluation_member_fk
    FOREIGN KEY (member_run_id, organization_id, campaign_run_id)
    REFERENCES DeploymentCampaignMemberRun(id, organization_id, campaign_run_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT campaignprerequisiteevaluation_upstream_plan_fk
    FOREIGN KEY (upstream_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE RESTRICT,
  CONSTRAINT campaignprerequisiteevaluation_observation_scope_check CHECK (
    (
      actual_observation_id IS NULL
      AND actual_observation_organization_id IS NULL
    )
    OR (
      actual_observation_id IS NOT NULL
      AND actual_observation_organization_id = organization_id
    )
  ),
  CONSTRAINT campaignprerequisiteevaluation_run_fk
    FOREIGN KEY (campaign_run_id, organization_id)
    REFERENCES DeploymentCampaignRun(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
);

CREATE INDEX CampaignPrerequisiteEvaluation_evidence
  ON CampaignPrerequisiteEvaluation (campaign_run_id, member_run_id, evaluated_at, id);

CREATE TABLE CampaignThresholdEvaluation (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  evaluated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  campaign_run_id UUID NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  samples INTEGER NOT NULL CHECK (samples >= 0),
  successful INTEGER NOT NULL CHECK (successful >= 0),
  failed INTEGER NOT NULL CHECK (failed >= 0),
  failure_rate DOUBLE PRECISION NOT NULL CHECK (failure_rate BETWEEN 0 AND 1),
  maximum_failure_rate DOUBLE PRECISION NOT NULL CHECK (maximum_failure_rate BETWEEN 0 AND 1),
  breached BOOLEAN NOT NULL,
  fencing_token BIGINT NOT NULL CHECK (fencing_token > 0),
  CONSTRAINT campaignthresholdevaluation_counts_check CHECK (
    samples = successful + failed
  ),
  CONSTRAINT campaignthresholdevaluation_run_fk
    FOREIGN KEY (campaign_run_id, organization_id)
    REFERENCES DeploymentCampaignRun(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
);

CREATE INDEX CampaignThresholdEvaluation_evidence
  ON CampaignThresholdEvaluation (campaign_run_id, evaluated_at, id);

CREATE TRIGGER DeploymentCampaignRun_delete_guard
BEFORE DELETE ON DeploymentCampaignRun
FOR EACH ROW EXECUTE FUNCTION deploymentcampaign_published_immutable();
CREATE TRIGGER DeploymentCampaignRun_no_truncate
BEFORE TRUNCATE ON DeploymentCampaignRun
FOR EACH STATEMENT EXECUTE FUNCTION deploymentcampaign_published_no_truncate();
CREATE TRIGGER DeploymentCampaignWaveRun_delete_guard
BEFORE DELETE ON DeploymentCampaignWaveRun
FOR EACH ROW EXECUTE FUNCTION deploymentcampaign_published_immutable();
CREATE TRIGGER DeploymentCampaignWaveRun_no_truncate
BEFORE TRUNCATE ON DeploymentCampaignWaveRun
FOR EACH STATEMENT EXECUTE FUNCTION deploymentcampaign_published_no_truncate();
CREATE TRIGGER DeploymentCampaignMemberRun_delete_guard
BEFORE DELETE ON DeploymentCampaignMemberRun
FOR EACH ROW EXECUTE FUNCTION deploymentcampaign_published_immutable();
CREATE TRIGGER DeploymentCampaignMemberRun_no_truncate
BEFORE TRUNCATE ON DeploymentCampaignMemberRun
FOR EACH STATEMENT EXECUTE FUNCTION deploymentcampaign_published_no_truncate();

CREATE TRIGGER CampaignPrerequisiteEvaluation_immutable
BEFORE UPDATE OR DELETE ON CampaignPrerequisiteEvaluation
FOR EACH ROW EXECUTE FUNCTION deploymentcampaign_published_immutable();
CREATE TRIGGER CampaignPrerequisiteEvaluation_no_truncate
BEFORE TRUNCATE ON CampaignPrerequisiteEvaluation
FOR EACH STATEMENT EXECUTE FUNCTION deploymentcampaign_published_no_truncate();
CREATE TRIGGER CampaignThresholdEvaluation_immutable
BEFORE UPDATE OR DELETE ON CampaignThresholdEvaluation
FOR EACH ROW EXECUTE FUNCTION deploymentcampaign_published_immutable();
CREATE TRIGGER CampaignThresholdEvaluation_no_truncate
BEFORE TRUNCATE ON CampaignThresholdEvaluation
FOR EACH STATEMENT EXECUTE FUNCTION deploymentcampaign_published_no_truncate();
