CREATE TABLE ControlPlaneAuditEvent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  sequence BIGINT NOT NULL CHECK (sequence > 0),
  event_type TEXT NOT NULL CHECK (
    event_type ~ '^[a-z][a-z0-9._-]{0,127}$'
  ),
  actor_id UUID,
  outcome TEXT NOT NULL CHECK (
    outcome ~ '^[A-Z][A-Z0-9_]{0,63}$'
  ),
  release_id UUID,
  component_release_id UUID,
  product_release_id UUID,
  target_config_id UUID,
  deployment_plan_id UUID,
  deployment_policy_id UUID,
  deployment_policy_version_id UUID,
  approval_id UUID,
  maintenance_calendar_id UUID,
  deployment_freeze_id UUID,
  admission_decision_id UUID,
  emergency_override_id UUID,
  campaign_id UUID,
  wave_id UUID,
  execution_id UUID,
  adapter_revision_id UUID,
  desired_state_id UUID,
  observation_id UUID,
  drift_case_id UUID,
  reconciliation_id UUID,
  deployment_target_id UUID,
  environment_id UUID,
  customer_organization_id UUID,
  deployment_unit_id UUID,
  component_id UUID,
  task_id UUID,
  step_run_id UUID,
  audit_export_sink_id UUID,
  audit_export_attempt_id UUID,
  release_checksum TEXT NOT NULL DEFAULT '',
  component_release_checksum TEXT NOT NULL DEFAULT '',
  product_release_checksum TEXT NOT NULL DEFAULT '',
  artifact_digest TEXT NOT NULL DEFAULT '',
  manifest_digest TEXT NOT NULL DEFAULT '',
  target_config_checksum TEXT NOT NULL DEFAULT '',
  deployment_plan_checksum TEXT NOT NULL DEFAULT '',
  policy_checksum TEXT NOT NULL DEFAULT '',
  approval_checksum TEXT NOT NULL DEFAULT '',
  calendar_checksum TEXT NOT NULL DEFAULT '',
  admission_checksum TEXT NOT NULL DEFAULT '',
  campaign_checksum TEXT NOT NULL DEFAULT '',
  execution_checksum TEXT NOT NULL DEFAULT '',
  desired_state_checksum TEXT NOT NULL DEFAULT '',
  observation_checksum TEXT NOT NULL DEFAULT '',
  drift_checksum TEXT NOT NULL DEFAULT '',
  reconciliation_checksum TEXT NOT NULL DEFAULT '',
  audit_export_config_checksum TEXT NOT NULL DEFAULT '',
  payload JSONB,
  payload_redacted BOOLEAN NOT NULL DEFAULT false,
  payload_truncated BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT controlplaneauditevent_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT controlplaneauditevent_sequence_unique
    UNIQUE (organization_id, sequence),
  CONSTRAINT controlplaneauditevent_payload_bound CHECK (
    payload IS NULL OR octet_length(payload::text) <= 32768
  ),
  CONSTRAINT controlplaneauditevent_correlation_required CHECK (
    release_id IS NOT NULL
    OR component_release_id IS NOT NULL
    OR product_release_id IS NOT NULL
    OR target_config_id IS NOT NULL
    OR deployment_plan_id IS NOT NULL
    OR deployment_policy_id IS NOT NULL
    OR deployment_policy_version_id IS NOT NULL
    OR approval_id IS NOT NULL
    OR maintenance_calendar_id IS NOT NULL
    OR deployment_freeze_id IS NOT NULL
    OR admission_decision_id IS NOT NULL
    OR emergency_override_id IS NOT NULL
    OR campaign_id IS NOT NULL
    OR wave_id IS NOT NULL
    OR execution_id IS NOT NULL
    OR adapter_revision_id IS NOT NULL
    OR desired_state_id IS NOT NULL
    OR observation_id IS NOT NULL
    OR drift_case_id IS NOT NULL
    OR reconciliation_id IS NOT NULL
    OR deployment_target_id IS NOT NULL
    OR environment_id IS NOT NULL
    OR customer_organization_id IS NOT NULL
    OR deployment_unit_id IS NOT NULL
    OR component_id IS NOT NULL
    OR task_id IS NOT NULL
    OR step_run_id IS NOT NULL
    OR audit_export_sink_id IS NOT NULL
    OR audit_export_attempt_id IS NOT NULL
  ),
  CONSTRAINT controlplaneauditevent_release_checksum_check CHECK (
    release_checksum = '' OR release_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT controlplaneauditevent_target_config_checksum_check CHECK (
    target_config_checksum = ''
    OR target_config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT controlplaneauditevent_plan_checksum_check CHECK (
    deployment_plan_checksum = ''
    OR deployment_plan_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT controlplaneauditevent_approval_checksum_check CHECK (
    approval_checksum = ''
    OR approval_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT controlplaneauditevent_campaign_checksum_check CHECK (
    campaign_checksum = ''
    OR campaign_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT controlplaneauditevent_execution_checksum_check CHECK (
    execution_checksum = ''
    OR execution_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT controlplaneauditevent_observation_checksum_check CHECK (
    observation_checksum = ''
    OR observation_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT controlplaneauditevent_extended_checksum_check CHECK (
    (component_release_checksum = '' OR component_release_checksum ~ '^sha256:[0-9a-f]{64}$')
    AND (product_release_checksum = '' OR product_release_checksum ~ '^sha256:[0-9a-f]{64}$')
    AND (artifact_digest = '' OR artifact_digest ~ '^sha256:[0-9a-f]{64}$')
    AND (manifest_digest = '' OR manifest_digest ~ '^sha256:[0-9a-f]{64}$')
    AND (policy_checksum = '' OR policy_checksum ~ '^sha256:[0-9a-f]{64}$')
    AND (calendar_checksum = '' OR calendar_checksum ~ '^sha256:[0-9a-f]{64}$')
    AND (admission_checksum = '' OR admission_checksum ~ '^sha256:[0-9a-f]{64}$')
    AND (desired_state_checksum = '' OR desired_state_checksum ~ '^sha256:[0-9a-f]{64}$')
    AND (drift_checksum = '' OR drift_checksum ~ '^sha256:[0-9a-f]{64}$')
    AND (reconciliation_checksum = '' OR reconciliation_checksum ~ '^sha256:[0-9a-f]{64}$')
    AND (audit_export_config_checksum = '' OR audit_export_config_checksum ~ '^sha256:[0-9a-f]{64}$')
  )
);

CREATE INDEX ControlPlaneAuditEvent_organization_created
  ON ControlPlaneAuditEvent (organization_id, created_at DESC, id DESC);

CREATE INDEX ControlPlaneAuditEvent_plan_sequence
  ON ControlPlaneAuditEvent (
    organization_id,
    deployment_plan_id,
    sequence,
    id
  )
  WHERE deployment_plan_id IS NOT NULL;

CREATE INDEX ControlPlaneAuditEvent_execution_sequence
  ON ControlPlaneAuditEvent (
    organization_id,
    execution_id,
    sequence,
    id
  )
  WHERE execution_id IS NOT NULL;

CREATE TABLE ControlPlaneAuditSubject (
  correlation_kind TEXT NOT NULL CHECK (
    correlation_kind ~ '^[a-z][a-z0-9_]{0,63}$'
  ),
  subject_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  first_event_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (correlation_kind, subject_id),
  CONSTRAINT controlplaneauditsubject_tenant_unique
    UNIQUE (correlation_kind, subject_id, organization_id),
  CONSTRAINT controlplaneauditsubject_event_fk
    FOREIGN KEY (first_event_id, organization_id)
    REFERENCES ControlPlaneAuditEvent(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
);

CREATE TABLE ControlPlaneAuditEventSubject (
  event_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  correlation_kind TEXT NOT NULL,
  subject_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (event_id, correlation_kind, subject_id),
  CONSTRAINT controlplaneauditeventsubject_event_fk
    FOREIGN KEY (event_id, organization_id)
    REFERENCES ControlPlaneAuditEvent(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT controlplaneauditeventsubject_subject_fk
    FOREIGN KEY (correlation_kind, subject_id, organization_id)
    REFERENCES ControlPlaneAuditSubject(
      correlation_kind,
      subject_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
);

CREATE INDEX ControlPlaneAuditEventSubject_subject_event
  ON ControlPlaneAuditEventSubject (
    organization_id,
    correlation_kind,
    subject_id,
    event_id
  );

CREATE TABLE AuditExportSink (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 128),
  kind TEXT NOT NULL CHECK (kind IN ('webhook', 'object_store', 'siem')),
  endpoint_reference TEXT NOT NULL CHECK (
    length(btrim(endpoint_reference)) BETWEEN 1 AND 1024
    AND endpoint_reference !~ E'[\\r\\n]'
    AND endpoint_reference LIKE 'secret:%'
    AND endpoint_reference !~ E'[?#@\\\\]'
    AND endpoint_reference !~ E'\\.\\.'
  ),
  config_checksum TEXT NOT NULL CHECK (
    config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  enabled BOOLEAN NOT NULL DEFAULT true,
  last_success_at TIMESTAMPTZ,
  last_failure_at TIMESTAMPTZ,
  consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (
    consecutive_failures >= 0
  ),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT auditexportsink_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT auditexportsink_name_unique
    UNIQUE (organization_id, name)
);

CREATE TABLE AuditExportCheckpoint (
  sink_id UUID PRIMARY KEY,
  organization_id UUID NOT NULL,
  last_sequence BIGINT NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
  last_event_id UUID,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT auditexportcheckpoint_sink_fk
    FOREIGN KEY (sink_id, organization_id)
    REFERENCES AuditExportSink(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT auditexportcheckpoint_event_fk
    FOREIGN KEY (last_event_id, organization_id)
    REFERENCES ControlPlaneAuditEvent(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE TABLE AuditExportAttempt (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  sink_id UUID NOT NULL,
  organization_id UUID NOT NULL,
  first_sequence BIGINT NOT NULL CHECK (first_sequence > 0),
  last_sequence BIGINT NOT NULL CHECK (last_sequence >= first_sequence),
  event_count INTEGER NOT NULL CHECK (event_count > 0),
  status TEXT NOT NULL CHECK (
    status IN ('RUNNING', 'SUCCEEDED', 'FAILED')
  ),
  idempotency_key TEXT NOT NULL CHECK (
    idempotency_key ~ '^sha256:[0-9a-f]{64}$'
  ),
  error_summary TEXT NOT NULL DEFAULT '' CHECK (
    length(error_summary) <= 2048
    AND error_summary !~ E'[\\r\\n]'
  ),
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  lease_expires_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ,
  CONSTRAINT auditexportattempt_sink_fk
    FOREIGN KEY (sink_id, organization_id)
    REFERENCES AuditExportSink(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT auditexportattempt_idempotency_unique
    UNIQUE (sink_id, idempotency_key),
  CONSTRAINT auditexportattempt_completion_check CHECK (
    (status = 'RUNNING' AND completed_at IS NULL)
    OR (status IN ('SUCCEEDED', 'FAILED') AND completed_at IS NOT NULL)
  ),
  CONSTRAINT auditexportattempt_lease_check CHECK (
    lease_expires_at > started_at
  )
);

CREATE INDEX AuditExportAttempt_sink_started
  ON AuditExportAttempt (
    organization_id,
    sink_id,
    started_at DESC,
    id DESC
  );

CREATE FUNCTION control_plane_audit_append_only_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'control-plane audit events are append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER ControlPlaneAuditEvent_append_only
BEFORE UPDATE OR DELETE ON ControlPlaneAuditEvent
FOR EACH ROW EXECUTE FUNCTION control_plane_audit_append_only_guard();

CREATE TRIGGER ControlPlaneAuditEvent_no_truncate
BEFORE TRUNCATE ON ControlPlaneAuditEvent
FOR EACH STATEMENT EXECUTE FUNCTION control_plane_audit_append_only_guard();

CREATE TRIGGER ControlPlaneAuditSubject_append_only
BEFORE UPDATE OR DELETE ON ControlPlaneAuditSubject
FOR EACH ROW EXECUTE FUNCTION control_plane_audit_append_only_guard();

CREATE TRIGGER ControlPlaneAuditSubject_no_truncate
BEFORE TRUNCATE ON ControlPlaneAuditSubject
FOR EACH STATEMENT EXECUTE FUNCTION control_plane_audit_append_only_guard();

CREATE TRIGGER ControlPlaneAuditEventSubject_append_only
BEFORE UPDATE OR DELETE ON ControlPlaneAuditEventSubject
FOR EACH ROW EXECUTE FUNCTION control_plane_audit_append_only_guard();

CREATE TRIGGER ControlPlaneAuditEventSubject_no_truncate
BEFORE TRUNCATE ON ControlPlaneAuditEventSubject
FOR EACH STATEMENT EXECUTE FUNCTION control_plane_audit_append_only_guard();
