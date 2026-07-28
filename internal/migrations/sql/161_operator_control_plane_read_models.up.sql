-- PR-079 exposes tenant-scoped operator read models over the existing control-plane
-- source tables. These indexes support their filters and descending keyset cursors;
-- they intentionally introduce no projection tables or alternate write authority.

CREATE INDEX OperatorFleet_component_page
  ON ComponentInstance (organization_id, created_at DESC, id DESC)
  WHERE retired_at IS NULL;

CREATE INDEX OperatorFleet_customer_unit
  ON DeploymentUnitSubscriber (
    organization_id,
    customer_organization_id,
    deployment_unit_id
  )
  WHERE retired_at IS NULL;

CREATE INDEX OperatorFleet_component_definition
  ON ComponentInstance (
    organization_id,
    component_definition_id,
    deployment_unit_id
  )
  WHERE retired_at IS NULL;

CREATE INDEX OperatorFleet_assignment_environment
  ON TargetEnvironmentAssignment (
    organization_id,
    environment_id,
    deployment_target_id,
    active_from DESC,
    id DESC
  )
  WHERE active_until IS NULL;

CREATE INDEX OperatorFleet_open_drift
  ON DriftCase (
    organization_id,
    deployment_unit_id,
    component_instance_id,
    updated_at DESC,
    id DESC
  )
  WHERE status IN ('OPEN', 'ASSIGNED', 'EXCEPTION');

CREATE INDEX OperatorRelease_page
  ON ReleaseBundle (organization_id, created_at DESC, id DESC)
  INCLUDE (
    application_id,
    kind,
    status,
    release_number,
    canonical_checksum,
    published_at
  );

CREATE INDEX OperatorRelease_status_page
  ON ReleaseBundle (organization_id, status, created_at DESC, id DESC);

CREATE INDEX OperatorRelease_application_page
  ON ReleaseBundle (organization_id, application_id, created_at DESC, id DESC);

CREATE INDEX OperatorRelease_kind_page
  ON ReleaseBundle (organization_id, kind, created_at DESC, id DESC);

CREATE INDEX OperatorPlan_page
  ON DeploymentPlan (organization_id, created_at DESC, id DESC);

CREATE INDEX OperatorPlan_status_page
  ON DeploymentPlan (organization_id, status, created_at DESC, id DESC);

CREATE INDEX OperatorPlan_environment_page
  ON DeploymentPlan (organization_id, environment_id, created_at DESC, id DESC);

CREATE INDEX OperatorPlan_unit_page
  ON DeploymentPlan (organization_id, deployment_unit_id, created_at DESC, id DESC);

CREATE INDEX OperatorPlan_release_page
  ON DeploymentPlan (organization_id, release_bundle_id, created_at DESC, id DESC);

CREATE INDEX OperatorPlan_campaign_member
  ON DeploymentCampaignMember (
    organization_id,
    deployment_plan_id,
    campaign_revision_id
  );

CREATE INDEX OperatorCampaign_run_page
  ON DeploymentCampaignRun (
    organization_id,
    campaign_revision_id,
    created_at DESC,
    id DESC
  );

CREATE INDEX OperatorCampaign_member_scope
  ON DeploymentCampaignMember (
    organization_id,
    campaign_revision_id,
    deployment_plan_id,
    deployment_unit_id
  );

CREATE INDEX OperatorCampaign_member_run_filter
  ON DeploymentCampaignMemberRun (
    organization_id,
    campaign_revision_id,
    campaign_run_id,
    status,
    execution_uncertain
  );

CREATE INDEX OperatorCampaign_prerequisite_latest
  ON CampaignPrerequisiteEvaluation (
    organization_id,
    campaign_run_id,
    member_run_id,
    evaluated_at DESC,
    id DESC
  );

CREATE INDEX OperatorCampaign_threshold_latest
  ON CampaignThresholdEvaluation (
    organization_id,
    campaign_run_id,
    evaluated_at DESC,
    id DESC
  );

CREATE INDEX OperatorExecution_page
  ON ExecutionAttempt (organization_id, created_at DESC, id DESC);

CREATE INDEX OperatorExecution_target_page
  ON ExecutionAttempt (
    organization_id,
    deployment_target_id,
    created_at DESC,
    id DESC
  );

CREATE INDEX OperatorExecution_task_plan
  ON Task (organization_id, deployment_plan_id, id);

CREATE INDEX OperatorExecution_task_target
  ON Task (organization_id, deployment_target_id, id);

CREATE INDEX OperatorReconciliation_page
  ON DriftCase (organization_id, updated_at DESC, id DESC);

CREATE INDEX OperatorAudit_type_page
  ON ControlPlaneAuditEvent (
    organization_id,
    event_type,
    created_at DESC,
    id DESC
  );

CREATE INDEX OperatorAudit_actor_page
  ON ControlPlaneAuditEvent (
    organization_id,
    actor_id,
    created_at DESC,
    id DESC
  );
