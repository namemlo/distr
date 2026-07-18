DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM ControlPlaneAuditEvent)
     OR EXISTS (SELECT 1 FROM AuditExportSink) THEN
    RAISE EXCEPTION
      'refusing migration 160 rollback while control-plane audit evidence exists';
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS ControlPlaneAuditEvent_no_truncate
  ON ControlPlaneAuditEvent;
DROP TRIGGER IF EXISTS ControlPlaneAuditEvent_append_only
  ON ControlPlaneAuditEvent;
DROP FUNCTION IF EXISTS control_plane_audit_append_only_guard();

DROP INDEX IF EXISTS AuditExportAttempt_sink_started;
DROP TABLE IF EXISTS AuditExportAttempt;
DROP TABLE IF EXISTS AuditExportCheckpoint;
DROP TABLE IF EXISTS AuditExportSink;

DROP INDEX IF EXISTS ControlPlaneAuditEvent_execution_sequence;
DROP INDEX IF EXISTS ControlPlaneAuditEvent_plan_sequence;
DROP INDEX IF EXISTS ControlPlaneAuditEvent_organization_created;
DROP TABLE IF EXISTS ControlPlaneAuditEvent;
