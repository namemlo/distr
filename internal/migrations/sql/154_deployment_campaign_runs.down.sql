LOCK TABLE
  CampaignThresholdEvaluation,
  CampaignPrerequisiteEvaluation,
  DeploymentCampaignMemberRun,
  DeploymentCampaignWaveRun,
  DeploymentCampaignRun
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM DeploymentCampaignRun) THEN
    RAISE EXCEPTION 'downgrade crossing 154 is forbidden while campaign run rows exist';
  END IF;
END;
$$;

DROP TABLE CampaignThresholdEvaluation;
DROP TABLE CampaignPrerequisiteEvaluation;
DROP TABLE DeploymentCampaignMemberRun;
DROP TABLE DeploymentCampaignWaveRun;
DROP TABLE DeploymentCampaignRun;
