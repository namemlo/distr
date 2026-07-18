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
  wave_order INTEGER NOT NULL CHECK (wave_order >= 0),
  status TEXT NOT NULL DEFAULT 'PENDING' CHECK (
    status IN ('PENDING', 'RUNNING', 'BAKING', 'PAUSED', 'FAILED', 'COMPLETED', 'CANCELED')
  ),
  bake_duration_seconds BIGINT NOT NULL CHECK (bake_duration_seconds > 0),
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
  CONSTRAINT deploymentcampaignwaverun_wave_fk
    FOREIGN KEY (campaign_wave_id, organization_id)
    REFERENCES DeploymentCampaignWave(id, organization_id)
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
  deployment_plan_id UUID NOT NULL,
  wave_order INTEGER NOT NULL CHECK (wave_order >= 0),
  member_order INTEGER NOT NULL CHECK (member_order >= 0),
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
  CONSTRAINT deploymentcampaignmemberrun_wave_fk
    FOREIGN KEY (wave_run_id, organization_id)
    REFERENCES DeploymentCampaignWaveRun(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT deploymentcampaignmemberrun_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE RESTRICT,
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
  member_run_id UUID NOT NULL REFERENCES DeploymentCampaignMemberRun(id) ON DELETE CASCADE,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  upstream_plan_id UUID NOT NULL REFERENCES DeploymentPlan(id) ON DELETE RESTRICT,
  step_key TEXT NOT NULL CHECK (step_key = btrim(step_key) AND length(step_key) BETWEEN 1 AND 200),
  expected_checksum TEXT NOT NULL CHECK (expected_checksum ~ '^sha256:[0-9a-f]{64}$'),
  actual_observation_id UUID,
  actual_checksum TEXT CHECK (
    actual_checksum IS NULL OR actual_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  matched BOOLEAN NOT NULL,
  reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4000),
  fencing_token BIGINT NOT NULL CHECK (fencing_token > 0),
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
