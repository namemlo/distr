LOCK TABLE
  DeploymentCampaignPrerequisite,
  DeploymentCampaignMember,
  DeploymentCampaignWave,
  DeploymentCampaignRevision,
  DeploymentCampaignDraft
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM DeploymentCampaignDraft)
     OR EXISTS (SELECT 1 FROM DeploymentCampaignRevision)
     OR EXISTS (SELECT 1 FROM DeploymentCampaignWave)
     OR EXISTS (SELECT 1 FROM DeploymentCampaignMember)
     OR EXISTS (SELECT 1 FROM DeploymentCampaignPrerequisite) THEN
    RAISE EXCEPTION
      'downgrade crossing 153 is forbidden while deployment campaign rows exist';
  END IF;
END;
$$;

DROP TRIGGER DeploymentCampaignPrerequisite_immutable
  ON DeploymentCampaignPrerequisite;
DROP TRIGGER DeploymentCampaignMember_immutable
  ON DeploymentCampaignMember;
DROP TRIGGER DeploymentCampaignWave_immutable
  ON DeploymentCampaignWave;
DROP TRIGGER DeploymentCampaignRevision_immutable
  ON DeploymentCampaignRevision;
DROP TRIGGER DeploymentCampaignPrerequisite_no_truncate
  ON DeploymentCampaignPrerequisite;
DROP TRIGGER DeploymentCampaignMember_no_truncate
  ON DeploymentCampaignMember;
DROP TRIGGER DeploymentCampaignWave_no_truncate
  ON DeploymentCampaignWave;
DROP TRIGGER DeploymentCampaignRevision_no_truncate
  ON DeploymentCampaignRevision;

ALTER TABLE DeploymentCampaignDraft
  DROP CONSTRAINT deploymentcampaigndraft_last_published_fk;
DROP TABLE DeploymentCampaignPrerequisite;
DROP TABLE DeploymentCampaignMember;
DROP TABLE DeploymentCampaignWave;
DROP TABLE DeploymentCampaignRevision;
DROP TABLE DeploymentCampaignDraft;
ALTER TABLE DeploymentPlanTargetComponent
  DROP CONSTRAINT deploymentplantargetcomponent_id_plan_organization_unique;
ALTER TABLE DeploymentPlan
  DROP CONSTRAINT deploymentplan_id_unit_organization_unique;
ALTER TABLE ApprovalRequest
  DROP CONSTRAINT approvalrequest_id_plan_organization_unique;
ALTER TABLE AdmissionEvaluation
  DROP CONSTRAINT admissionevaluation_id_plan_organization_unique;
DROP FUNCTION deploymentcampaign_published_no_truncate();
DROP FUNCTION deploymentcampaign_published_immutable();
