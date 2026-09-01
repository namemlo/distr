SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

LOCK TABLE ProtectedHistoryArtifact, ControlPlaneAuditEvent
  IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM ProtectedHistoryArtifact)
     OR EXISTS (
       SELECT 1
       FROM ControlPlaneAuditEvent
       WHERE protected_history_artifact_id IS NOT NULL
     ) THEN
    RAISE EXCEPTION
      'refusing migration 170 rollback while protected-history artifacts exist'
      USING ERRCODE = '23514';
  END IF;
END;
$$;

ALTER TABLE ControlPlaneAuditEvent
  DROP CONSTRAINT controlplaneauditevent_protected_history_artifact_fk;

DROP TRIGGER ProtectedHistoryArtifact_audit_guard
  ON ProtectedHistoryArtifact;
DROP FUNCTION protected_history_artifact_audit_guard();

DROP TRIGGER ProtectedHistoryArtifact_no_truncate
  ON ProtectedHistoryArtifact;
DROP TRIGGER ProtectedHistoryArtifact_append_only
  ON ProtectedHistoryArtifact;
DROP FUNCTION protected_history_artifact_append_only_guard();

DROP TRIGGER ProtectedHistoryArtifact_validate_scope
  ON ProtectedHistoryArtifact;
DROP FUNCTION protected_history_artifact_validate_scope();

DROP INDEX ControlPlaneAuditEvent_protected_history_artifact;
DROP INDEX ProtectedHistoryArtifact_content_checksum;
DROP INDEX ProtectedHistoryArtifact_organization_created;
DROP TABLE ProtectedHistoryArtifact;

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
      audit_export_attempt_id,
      sample_retirement_ownership_evidence_id,
      sample_retirement_recovery_evidence_id
    ) > 0
  ),
  DROP CONSTRAINT controlplaneauditevent_exact_identity_unique,
  DROP COLUMN protected_history_artifact_id;

DROP FUNCTION protected_history_audit_binding_checksum(
  UUID,
  TEXT,
  UUID,
  BIGINT
);
DROP FUNCTION protected_history_retention_checksum(
  UUID,
  UUID,
  TEXT,
  BIGINT,
  UUID[],
  UUID[],
  TEXT,
  TEXT,
  BIGINT,
  TEXT,
  TEXT,
  BIGINT,
  TEXT,
  TIMESTAMPTZ,
  UUID,
  UUID
);
DROP FUNCTION protected_history_request_checksum(
  UUID,
  UUID[],
  UUID[],
  UUID,
  UUID,
  TEXT
);
DROP FUNCTION protected_history_rfc3339_microseconds(TIMESTAMPTZ);
DROP FUNCTION protected_history_checksum_uuid_set(UUID[]);
DROP FUNCTION protected_history_checksum_field(TEXT);
