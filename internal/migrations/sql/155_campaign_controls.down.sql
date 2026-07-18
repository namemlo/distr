LOCK TABLE
  CampaignExclusion,
  CampaignControlRequest,
  DeploymentCampaignMemberRun,
  DeploymentCampaignRun
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM CampaignControlRequest)
     OR EXISTS (SELECT 1 FROM CampaignExclusion) THEN
    RAISE EXCEPTION 'downgrade crossing 155 is forbidden while campaign control rows exist';
  END IF;
END;
$$;

DROP TRIGGER CampaignExclusion_append_only ON CampaignExclusion;
DROP TABLE CampaignExclusion;
DROP TRIGGER CampaignControlRequest_append_only ON CampaignControlRequest;
DROP TABLE CampaignControlRequest;
DROP FUNCTION campaign_control_append_only();

ALTER TABLE DeploymentCampaignMemberRun
  DROP CONSTRAINT deploymentcampaignmemberrun_control_identity_unique,
  DROP COLUMN active_steps_cancellable,
  DROP COLUMN execution_uncertain;

ALTER TABLE DeploymentCampaignRun
  DROP COLUMN reconciliation_required,
  DROP COLUMN pause_requested;
