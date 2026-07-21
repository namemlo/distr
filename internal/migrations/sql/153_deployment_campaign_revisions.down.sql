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

-- Migration 149 rejects campaign bindings after migration 153 is removed.
CREATE OR REPLACE FUNCTION deployment_policy_binding_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  version_state TEXT;
  scope_belongs_to_organization BOOLEAN := false;
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF current_setting(
      'distr.deployment_policy_deletion_reason',
      true
    ) = 'ORGANIZATION_RETENTION' THEN
      RETURN OLD;
    END IF;
    RAISE EXCEPTION 'deployment policy binding history is append-only'
      USING ERRCODE = '23514';
  END IF;

  IF TG_OP = 'INSERT' THEN
    SELECT version.state
    INTO version_state
    FROM DeploymentPolicyVersion version
    WHERE version.id = NEW.deployment_policy_version_id
      AND version.organization_id = NEW.organization_id;

    IF version_state IS DISTINCT FROM 'PUBLISHED' THEN
      RAISE EXCEPTION
        'deployment policy bindings require a published immutable version'
        USING ERRCODE = '23514';
    END IF;

    CASE NEW.scope_kind
      WHEN 'organization' THEN
        SELECT EXISTS (
          SELECT 1
          FROM Organization organization
          WHERE organization.id = NEW.scope_id
            AND organization.id = NEW.organization_id
        ) INTO scope_belongs_to_organization;
      WHEN 'customer' THEN
        SELECT EXISTS (
          SELECT 1
          FROM CustomerOrganization customer
          WHERE customer.id = NEW.scope_id
            AND customer.organization_id = NEW.organization_id
        ) INTO scope_belongs_to_organization;
      WHEN 'environment' THEN
        SELECT EXISTS (
          SELECT 1
          FROM Environment scoped_environment
          WHERE scoped_environment.id = NEW.scope_id
            AND scoped_environment.organization_id = NEW.organization_id
        ) INTO scope_belongs_to_organization;
      WHEN 'deployment_unit' THEN
        SELECT EXISTS (
          SELECT 1
          FROM DeploymentUnit deployment_unit
          WHERE deployment_unit.id = NEW.scope_id
            AND deployment_unit.organization_id = NEW.organization_id
        ) INTO scope_belongs_to_organization;
      WHEN 'component' THEN
        SELECT EXISTS (
          SELECT 1
          FROM ComponentDefinition component
          WHERE component.id = NEW.scope_id
            AND component.organization_id = NEW.organization_id
        ) INTO scope_belongs_to_organization;
      ELSE
        scope_belongs_to_organization := false;
    END CASE;

    IF NOT scope_belongs_to_organization THEN
      RAISE EXCEPTION
        'deployment policy binding scope does not belong to organization'
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF OLD.retired_at IS NOT NULL THEN
    RAISE EXCEPTION 'retired deployment policy bindings are immutable'
      USING ERRCODE = '23514';
  END IF;

  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.deployment_policy_version_id IS DISTINCT FROM
       OLD.deployment_policy_version_id
     OR NEW.scope_kind IS DISTINCT FROM OLD.scope_kind
     OR NEW.scope_id IS DISTINCT FROM OLD.scope_id
     OR NEW.binding_role IS DISTINCT FROM OLD.binding_role
     OR NEW.created_by_useraccount_id IS DISTINCT FROM
       OLD.created_by_useraccount_id
     OR NEW.retired_at IS NULL THEN
    RAISE EXCEPTION
      'deployment policy bindings may only transition once to retired'
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

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
