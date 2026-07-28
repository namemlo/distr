LOCK TABLE
  ApprovalRequest,
  SampleRetirementRecoveryEvidence,
  SampleRetirementOwnershipEvidence,
  SampleRetirementJob,
  SampleRetirementItem,
  SampleRetirementCheckpoint,
  AuditSubjectTombstone
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM SampleRetirementRecoveryEvidence)
     OR EXISTS (SELECT 1 FROM SampleRetirementOwnershipEvidence)
     OR EXISTS (SELECT 1 FROM SampleRetirementJob)
     OR EXISTS (SELECT 1 FROM SampleRetirementItem)
     OR EXISTS (SELECT 1 FROM SampleRetirementCheckpoint)
     OR EXISTS (SELECT 1 FROM AuditSubjectTombstone) THEN
    RAISE EXCEPTION
      'refusing migration 162 rollback while sample retirement evidence exists';
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS ApprovalRequest_subject_guard
  ON ApprovalRequest;
DROP FUNCTION IF EXISTS approval_request_subject_guard();

ALTER TABLE ApprovalRequest
  DROP CONSTRAINT approvalrequest_subject_type_check,
  ADD CONSTRAINT approvalrequest_subject_type_check CHECK (
    subject_type IN ('deployment_plan')
  );

ALTER TABLE ApprovalRequest
  DROP CONSTRAINT approvalrequest_invalidation_reason_check,
  ADD CONSTRAINT approvalrequest_invalidation_reason_check CHECK (
    invalidation_reason IN (
      'expired',
      'superseded',
      'plan_changed',
      'policy_changed',
      'subscriber_set_changed',
      'campaign_member_unapproved'
    )
  );

ALTER TABLE ApprovalRequest
  ADD CONSTRAINT approvalrequest_plan_fk
  FOREIGN KEY (subject_id, organization_id)
  REFERENCES DeploymentPlan(id, organization_id)
  ON UPDATE NO ACTION
  ON DELETE CASCADE
  DEFERRABLE INITIALLY IMMEDIATE;

DROP TRIGGER IF EXISTS AuditSubjectTombstone_no_truncate
  ON AuditSubjectTombstone;
DROP TRIGGER IF EXISTS AuditSubjectTombstone_append_only
  ON AuditSubjectTombstone;
DROP TRIGGER IF EXISTS SampleRetirementCheckpoint_no_truncate
  ON SampleRetirementCheckpoint;
DROP TRIGGER IF EXISTS SampleRetirementCheckpoint_append_only
  ON SampleRetirementCheckpoint;
DROP TRIGGER IF EXISTS SampleRetirementItem_no_truncate
  ON SampleRetirementItem;
DROP TRIGGER IF EXISTS SampleRetirementJob_no_truncate
  ON SampleRetirementJob;
DROP TRIGGER IF EXISTS SampleRetirementRecoveryEvidence_no_truncate
  ON SampleRetirementRecoveryEvidence;
DROP TRIGGER IF EXISTS SampleRetirementRecoveryEvidence_append_only
  ON SampleRetirementRecoveryEvidence;
DROP TRIGGER IF EXISTS SampleRetirementOwnershipEvidence_no_truncate
  ON SampleRetirementOwnershipEvidence;
DROP TRIGGER IF EXISTS SampleRetirementOwnershipEvidence_append_only
  ON SampleRetirementOwnershipEvidence;
DROP FUNCTION IF EXISTS sample_retirement_evidence_append_only_guard();

DROP TRIGGER IF EXISTS SampleRetirementItem_guard
  ON SampleRetirementItem;
DROP FUNCTION IF EXISTS sample_retirement_item_guard();

DROP TRIGGER IF EXISTS SampleRetirementJob_guard
  ON SampleRetirementJob;
DROP FUNCTION IF EXISTS sample_retirement_job_guard();
DROP TRIGGER IF EXISTS SampleRetirementJob_evidence_binding_guard
  ON SampleRetirementJob;
DROP FUNCTION IF EXISTS sample_retirement_job_evidence_binding_guard();

ALTER TABLE SampleRetirementItem
  DROP CONSTRAINT IF EXISTS sampleretirementitem_tombstone_fk;

DROP INDEX IF EXISTS AuditSubjectTombstone_job;
DROP INDEX IF EXISTS AuditSubjectTombstone_subject;
DROP TABLE IF EXISTS AuditSubjectTombstone;

DROP INDEX IF EXISTS SampleRetirementCheckpoint_job_sequence;
DROP TABLE IF EXISTS SampleRetirementCheckpoint;

DROP INDEX IF EXISTS SampleRetirementItem_pending;
DROP INDEX IF EXISTS SampleRetirementItem_job_order;
DROP TABLE IF EXISTS SampleRetirementItem;

DROP INDEX IF EXISTS SampleRetirementJob_resumable;
DROP INDEX IF EXISTS SampleRetirementJob_organization_history;
DROP TABLE IF EXISTS SampleRetirementJob;

ALTER TABLE ControlPlaneAuditEvent
  DROP CONSTRAINT controlplaneauditevent_correlation_required,
  ADD CONSTRAINT controlplaneauditevent_correlation_required CHECK (
    num_nonnulls(
      release_id,
      component_release_id,
      product_release_id,
      target_config_id,
      deployment_plan_id,
      deployment_policy_id,
      deployment_policy_version_id,
      approval_id,
      maintenance_calendar_id,
      deployment_freeze_id,
      admission_decision_id,
      emergency_override_id,
      campaign_draft_id,
      campaign_revision_id,
      campaign_run_id,
      campaign_wave_definition_id,
      campaign_wave_run_id,
      campaign_member_id,
      campaign_member_run_id,
      campaign_control_request_id,
      campaign_exclusion_id,
      campaign_prerequisite_evaluation_id,
      campaign_threshold_evaluation_id,
      execution_id,
      execution_attempt_id,
      adapter_revision_id,
      desired_state_id,
      observation_id,
      drift_case_id,
      reconciliation_id,
      deployment_target_id,
      environment_id,
      customer_organization_id,
      deployment_unit_id,
      component_id,
      task_id,
      step_run_id,
      audit_export_sink_id,
      audit_export_attempt_id
    ) > 0
  );

ALTER TABLE ControlPlaneAuditEvent
  DROP CONSTRAINT controlplaneauditevent_retirement_ownership_evidence_fk,
  DROP CONSTRAINT controlplaneauditevent_retirement_recovery_evidence_fk,
  DROP COLUMN sample_retirement_ownership_evidence_id,
  DROP COLUMN sample_retirement_recovery_evidence_id;

DROP TABLE IF EXISTS SampleRetirementOwnershipEvidence;
DROP TABLE IF EXISTS SampleRetirementRecoveryEvidence;
