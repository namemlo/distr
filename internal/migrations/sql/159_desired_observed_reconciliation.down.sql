DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM PendingDesiredRevision)
     OR EXISTS (SELECT 1 FROM ActiveDesiredRevision)
     OR EXISTS (SELECT 1 FROM ExecutorReport)
     OR EXISTS (SELECT 1 FROM ObservedComponentState)
     OR EXISTS (SELECT 1 FROM DriftCase)
     OR EXISTS (SELECT 1 FROM ReconciliationAction) THEN
    RAISE EXCEPTION
      'refusing migration 159 rollback while desired, observed, or reconciliation evidence exists';
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS PendingDesiredRevision_no_truncate
  ON PendingDesiredRevision;
DROP TRIGGER IF EXISTS PendingDesiredRevision_guard
  ON PendingDesiredRevision;
DROP FUNCTION IF EXISTS pending_desired_revision_guard();

DROP TRIGGER IF EXISTS ObservedComponentState_retire_head_only
  ON ObservedComponentState;
DROP FUNCTION IF EXISTS observed_component_state_guard();

DROP TRIGGER IF EXISTS ReconciliationAction_no_truncate
  ON ReconciliationAction;
DROP TRIGGER IF EXISTS ReconciliationAction_append_only
  ON ReconciliationAction;
DROP TRIGGER IF EXISTS DriftCaseEvent_no_truncate ON DriftCaseEvent;
DROP TRIGGER IF EXISTS DriftCaseEvent_append_only ON DriftCaseEvent;
DROP TRIGGER IF EXISTS ObservedComponentState_no_truncate
  ON ObservedComponentState;
DROP TRIGGER IF EXISTS ObservedComponentState_append_only
  ON ObservedComponentState;
DROP TRIGGER IF EXISTS ExecutorReport_no_truncate ON ExecutorReport;
DROP TRIGGER IF EXISTS ExecutorReport_append_only ON ExecutorReport;
DROP TRIGGER IF EXISTS ActiveDesiredRevision_no_truncate
  ON ActiveDesiredRevision;
DROP TRIGGER IF EXISTS ActiveDesiredRevision_append_only
  ON ActiveDesiredRevision;
DROP FUNCTION IF EXISTS desired_observed_append_only_guard();

DROP INDEX IF EXISTS ReconciliationAction_case_history;
DROP TABLE IF EXISTS ReconciliationAction;

DROP INDEX IF EXISTS DriftCaseEvent_case_history;
DROP TABLE IF EXISTS DriftCaseEvent;

DROP INDEX IF EXISTS DriftCase_organization_status;
DROP INDEX IF EXISTS DriftCase_open_lineage_unique;
DROP TABLE IF EXISTS DriftCase;

DROP TABLE IF EXISTS ComponentObservationHead;

DO $$
BEGIN
  IF to_regclass('CampaignPrerequisiteEvaluation') IS NOT NULL THEN
    ALTER TABLE CampaignPrerequisiteEvaluation
      DROP CONSTRAINT IF EXISTS campaignprerequisiteevaluation_observation_fk;
  END IF;
END;
$$;
ALTER TABLE ActiveDesiredRevision
  DROP CONSTRAINT IF EXISTS activedesiredrevision_verified_observation_fk;
ALTER TABLE PendingDesiredRevision
  DROP CONSTRAINT IF EXISTS pendingdesiredrevision_terminal_observation_fk;
ALTER TABLE PendingDesiredRevision
  DROP CONSTRAINT IF EXISTS pendingdesiredrevision_verified_observation_fk;

DROP INDEX IF EXISTS ObservedComponentState_component_history;
DROP INDEX IF EXISTS ObservedComponentState_current_observer_component;
DROP TABLE IF EXISTS ObservedComponentState;

DROP INDEX IF EXISTS ObserverRegistration_enabled_scope;
DROP TABLE IF EXISTS ObserverRegistration;

DROP INDEX IF EXISTS ExecutorReport_pending;
DROP TABLE IF EXISTS ExecutorReport;

DROP TABLE IF EXISTS ComponentDesiredStateHead;

DROP INDEX IF EXISTS ActiveDesiredRevision_component_history;
DROP TABLE IF EXISTS ActiveDesiredRevision;

DROP INDEX IF EXISTS PendingDesiredRevision_component_status;
DROP TABLE IF EXISTS PendingDesiredRevision;

ALTER TABLE ComponentInstance
  DROP CONSTRAINT IF EXISTS componentinstance_id_unit_organization_unique;
