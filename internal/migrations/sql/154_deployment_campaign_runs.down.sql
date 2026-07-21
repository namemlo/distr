LOCK TABLE
  DeploymentCampaignRun,
  DeploymentCampaignWaveRun,
  DeploymentCampaignMemberRun,
  CampaignPrerequisiteEvaluation,
  CampaignThresholdEvaluation
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM DeploymentCampaignRun) THEN
    RAISE EXCEPTION 'downgrade crossing 154 is forbidden while campaign run rows exist';
  END IF;
END;
$$;

DROP TRIGGER CampaignThresholdEvaluation_no_truncate ON CampaignThresholdEvaluation;
DROP TRIGGER CampaignThresholdEvaluation_immutable ON CampaignThresholdEvaluation;
DROP TRIGGER CampaignPrerequisiteEvaluation_no_truncate ON CampaignPrerequisiteEvaluation;
DROP TRIGGER CampaignPrerequisiteEvaluation_immutable ON CampaignPrerequisiteEvaluation;
DROP TRIGGER DeploymentCampaignMemberRun_no_truncate ON DeploymentCampaignMemberRun;
DROP TRIGGER DeploymentCampaignMemberRun_delete_guard ON DeploymentCampaignMemberRun;
DROP TRIGGER DeploymentCampaignWaveRun_no_truncate ON DeploymentCampaignWaveRun;
DROP TRIGGER DeploymentCampaignWaveRun_delete_guard ON DeploymentCampaignWaveRun;
DROP TRIGGER DeploymentCampaignRun_no_truncate ON DeploymentCampaignRun;
DROP TRIGGER DeploymentCampaignRun_delete_guard ON DeploymentCampaignRun;
DROP TABLE CampaignThresholdEvaluation;
DROP TABLE CampaignPrerequisiteEvaluation;
DROP TABLE DeploymentCampaignMemberRun;
DROP TABLE DeploymentCampaignWaveRun;
DROP TABLE DeploymentCampaignRun;
