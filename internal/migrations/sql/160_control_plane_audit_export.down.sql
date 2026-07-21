LOCK TABLE
  AuditExportSink,
  AuditExportCheckpoint,
  AuditExportAttempt,
  ControlPlaneAuditEvent,
  ControlPlaneAuditSubject,
  ControlPlaneAuditEventSubject
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM ControlPlaneAuditEvent)
     OR EXISTS (SELECT 1 FROM ControlPlaneAuditSubject)
     OR EXISTS (SELECT 1 FROM ControlPlaneAuditEventSubject)
     OR EXISTS (SELECT 1 FROM AuditExportSink)
     OR EXISTS (SELECT 1 FROM AuditExportCheckpoint)
     OR EXISTS (SELECT 1 FROM AuditExportAttempt) THEN
    RAISE EXCEPTION
      'refusing migration 160 rollback while control-plane audit evidence exists';
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS ControlPlaneAuditEvent_no_truncate
  ON ControlPlaneAuditEvent;
DROP TRIGGER IF EXISTS ControlPlaneAuditEvent_append_only
  ON ControlPlaneAuditEvent;
DROP TRIGGER IF EXISTS ControlPlaneAuditEventSubject_no_truncate
  ON ControlPlaneAuditEventSubject;
DROP TRIGGER IF EXISTS ControlPlaneAuditEventSubject_append_only
  ON ControlPlaneAuditEventSubject;
DROP TRIGGER IF EXISTS ControlPlaneAuditSubject_no_truncate
  ON ControlPlaneAuditSubject;
DROP TRIGGER IF EXISTS ControlPlaneAuditSubject_append_only
  ON ControlPlaneAuditSubject;
DROP FUNCTION IF EXISTS control_plane_audit_append_only_guard();

DROP INDEX IF EXISTS AuditExportAttempt_sink_started;
DROP TABLE IF EXISTS AuditExportAttempt;
DROP TABLE IF EXISTS AuditExportCheckpoint;
DROP TABLE IF EXISTS AuditExportSink;

DROP INDEX IF EXISTS ControlPlaneAuditEvent_execution_sequence;
DROP INDEX IF EXISTS ControlPlaneAuditEvent_attempt_event_unique;
DROP INDEX IF EXISTS ControlPlaneAuditEvent_plan_sequence;
DROP INDEX IF EXISTS ControlPlaneAuditEvent_organization_created;
DROP INDEX IF EXISTS ControlPlaneAuditEventSubject_subject_event;
DROP TABLE IF EXISTS ControlPlaneAuditEventSubject;
DROP TABLE IF EXISTS ControlPlaneAuditSubject;
DROP TABLE IF EXISTS ControlPlaneAuditEvent;
