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
  target_config_id UUID,
  deployment_plan_id UUID,
  approval_id UUID,
  campaign_id UUID,
  wave_id UUID,
  execution_id UUID,
  adapter_revision_id UUID,
  observation_id UUID,
  reconciliation_id UUID,
  release_checksum TEXT NOT NULL DEFAULT '',
  target_config_checksum TEXT NOT NULL DEFAULT '',
  deployment_plan_checksum TEXT NOT NULL DEFAULT '',
  approval_checksum TEXT NOT NULL DEFAULT '',
  campaign_checksum TEXT NOT NULL DEFAULT '',
  execution_checksum TEXT NOT NULL DEFAULT '',
  observation_checksum TEXT NOT NULL DEFAULT '',
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
    OR target_config_id IS NOT NULL
    OR deployment_plan_id IS NOT NULL
    OR approval_id IS NOT NULL
    OR campaign_id IS NOT NULL
    OR wave_id IS NOT NULL
    OR execution_id IS NOT NULL
    OR adapter_revision_id IS NOT NULL
    OR observation_id IS NOT NULL
    OR reconciliation_id IS NOT NULL
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

CREATE TABLE AuditExportSink (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 128),
  kind TEXT NOT NULL CHECK (kind IN ('webhook', 'object_store', 'siem')),
  endpoint_reference TEXT NOT NULL CHECK (
    length(btrim(endpoint_reference)) BETWEEN 1 AND 1024
    AND endpoint_reference !~ E'[\\r\\n]'
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
