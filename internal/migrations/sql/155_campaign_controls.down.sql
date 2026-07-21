LOCK TABLE
  DeploymentCampaignRun,
  DeploymentCampaignMemberRun,
  CampaignControlRequest,
  CampaignExclusion
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM CampaignControlRequest)
     OR EXISTS (SELECT 1 FROM CampaignExclusion)
     OR EXISTS (
       SELECT 1 FROM DeploymentCampaignRun
       WHERE pause_requested
          OR reconciliation_required
          OR resume_state IS NOT NULL
     )
     OR EXISTS (
       SELECT 1 FROM DeploymentCampaignMemberRun
       WHERE execution_uncertain
          OR active_steps_cancellable
     ) THEN
    RAISE EXCEPTION 'downgrade crossing 155 is forbidden while non-default campaign control state exists';
  END IF;
END;
$$;

DROP TRIGGER CampaignExclusion_append_only ON CampaignExclusion;
DROP TRIGGER CampaignExclusion_no_truncate ON CampaignExclusion;
DROP TABLE CampaignExclusion;
DROP TRIGGER CampaignControlRequest_append_only ON CampaignControlRequest;
DROP TRIGGER CampaignControlRequest_no_truncate ON CampaignControlRequest;
DROP TABLE CampaignControlRequest;

ALTER TABLE DeploymentCampaignMemberRun
  DROP CONSTRAINT deploymentcampaignmemberrun_control_identity_unique,
  DROP COLUMN active_steps_cancellable,
  DROP COLUMN execution_uncertain;

ALTER TABLE DeploymentCampaignRun
  DROP COLUMN reconciliation_required,
  DROP COLUMN pause_requested,
  DROP COLUMN resume_state;
