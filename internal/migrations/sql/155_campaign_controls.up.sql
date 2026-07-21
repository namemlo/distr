ALTER TABLE DeploymentCampaignRun
  ADD COLUMN pause_requested BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN reconciliation_required BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN resume_state TEXT CHECK (resume_state IN ('SCHEDULED', 'RUNNING'));

ALTER TABLE DeploymentCampaignMemberRun
  ADD COLUMN execution_uncertain BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN active_steps_cancellable BOOLEAN NOT NULL DEFAULT FALSE,
  ADD CONSTRAINT deploymentcampaignmemberrun_control_identity_unique
    UNIQUE (id, organization_id, campaign_run_id);

CREATE TABLE CampaignControlRequest (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  request_id UUID NOT NULL,
  requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  campaign_run_id UUID NOT NULL,
  member_run_id UUID,
  actor_useraccount_id UUID NOT NULL REFERENCES UserAccount(id) ON DELETE RESTRICT,
  control_kind TEXT NOT NULL CHECK (
    control_kind IN ('PAUSE', 'RESUME', 'RETRY', 'EXCLUDE', 'CANCEL')
  ),
  expected_run_version BIGINT NOT NULL CHECK (expected_run_version > 0),
  reason TEXT NOT NULL CHECK (
    reason = btrim(reason) AND length(reason) BETWEEN 1 AND 4000
  ),
  request_checksum TEXT NOT NULL CHECK (
    request_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  status TEXT NOT NULL CHECK (
    status IN ('APPLIED', 'PENDING_SAFE_POINT', 'PENDING_RECONCILIATION', 'REJECTED')
  ),
  resulting_run_version BIGINT NOT NULL CHECK (resulting_run_version > 0),
  response_snapshot JSONB NOT NULL CHECK (
    jsonb_typeof(response_snapshot) = 'object'
    AND pg_column_size(response_snapshot) <= 1048576
  ),
  CONSTRAINT campaigncontrolrequest_run_fk
    FOREIGN KEY (campaign_run_id, organization_id)
    REFERENCES DeploymentCampaignRun(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT campaigncontrolrequest_member_fk
    FOREIGN KEY (member_run_id, organization_id, campaign_run_id)
    REFERENCES DeploymentCampaignMemberRun(id, organization_id, campaign_run_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT campaigncontrolrequest_idempotency_unique
    UNIQUE (organization_id, request_id),
  CONSTRAINT campaigncontrolrequest_control_identity_unique
    UNIQUE (id, organization_id, campaign_run_id)
);

CREATE INDEX CampaignControlRequest_run_history
  ON CampaignControlRequest (organization_id, campaign_run_id, requested_at, id);

CREATE TRIGGER CampaignControlRequest_append_only
BEFORE UPDATE OR DELETE ON CampaignControlRequest
FOR EACH ROW EXECUTE FUNCTION deploymentcampaign_published_immutable();

CREATE TRIGGER CampaignControlRequest_no_truncate
BEFORE TRUNCATE ON CampaignControlRequest
FOR EACH STATEMENT EXECUTE FUNCTION deploymentcampaign_published_no_truncate();

CREATE TABLE CampaignExclusion (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  excluded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  campaign_run_id UUID NOT NULL,
  member_run_id UUID NOT NULL,
  control_request_id UUID NOT NULL,
  excluded_by_useraccount_id UUID NOT NULL REFERENCES UserAccount(id) ON DELETE RESTRICT,
  reason TEXT NOT NULL CHECK (
    reason = btrim(reason) AND length(reason) BETWEEN 1 AND 4000
  ),
  visible_incomplete BOOLEAN NOT NULL,
  drift_reason TEXT NOT NULL DEFAULT '' CHECK (length(drift_reason) <= 4000),
  CONSTRAINT campaignexclusion_run_fk
    FOREIGN KEY (campaign_run_id, organization_id)
    REFERENCES DeploymentCampaignRun(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT campaignexclusion_member_fk
    FOREIGN KEY (member_run_id, organization_id, campaign_run_id)
    REFERENCES DeploymentCampaignMemberRun(id, organization_id, campaign_run_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT campaignexclusion_control_fk
    FOREIGN KEY (control_request_id, organization_id, campaign_run_id)
    REFERENCES CampaignControlRequest(id, organization_id, campaign_run_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT campaignexclusion_member_unique UNIQUE (campaign_run_id, member_run_id)
);

CREATE INDEX CampaignExclusion_visible
  ON CampaignExclusion (organization_id, campaign_run_id, visible_incomplete, excluded_at, id);

CREATE TRIGGER CampaignExclusion_append_only
BEFORE UPDATE OR DELETE ON CampaignExclusion
FOR EACH ROW EXECUTE FUNCTION deploymentcampaign_published_immutable();

CREATE TRIGGER CampaignExclusion_no_truncate
BEFORE TRUNCATE ON CampaignExclusion
FOR EACH STATEMENT EXECUTE FUNCTION deploymentcampaign_published_no_truncate();
