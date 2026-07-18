LOCK TABLE
  DeploymentPlan,
  DeploymentPolicyBinding,
  DeploymentPolicyVersion,
  DeploymentPolicy
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM DeploymentPlan
    WHERE effective_policy IS NOT NULL
  )
     OR EXISTS (SELECT 1 FROM DeploymentPolicyBinding)
     OR EXISTS (SELECT 1 FROM DeploymentPolicyVersion)
     OR EXISTS (SELECT 1 FROM DeploymentPolicy) THEN
    RAISE EXCEPTION
      'downgrade crossing 149 is forbidden while deployment policy evidence exists';
  END IF;
END;
$$;

DROP INDEX DeploymentPlan_effective_policy;

ALTER TABLE DeploymentPlan
  DROP CONSTRAINT deploymentplan_effective_policy_shape_check,
  DROP CONSTRAINT deploymentplan_deployment_unit_fk,
  DROP COLUMN subscriber_set_checksum,
  DROP COLUMN effective_policy_checksum,
  DROP COLUMN effective_policy,
  DROP COLUMN deployment_unit_id;

DROP TRIGGER DeploymentPolicyBinding_guard ON DeploymentPolicyBinding;
DROP FUNCTION deployment_policy_binding_guard();
DROP TABLE DeploymentPolicyBinding;

DROP TRIGGER DeploymentPolicyVersion_published_immutable
  ON DeploymentPolicyVersion;
DROP FUNCTION deployment_policy_version_published_immutable();
DROP TABLE DeploymentPolicyVersion;

DROP TRIGGER DeploymentPolicy_normalize_text ON DeploymentPolicy;
DROP TABLE DeploymentPolicy;
DROP FUNCTION deployment_policy_normalize_text();
