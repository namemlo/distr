SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

LOCK TABLE BaselineAdoption, BaselineAdoptionComponent,
  ActiveDesiredRevision, ObservedComponentState,
  DeploymentPlan IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM BaselineAdoption)
     OR EXISTS (SELECT 1 FROM BaselineAdoptionComponent)
     OR EXISTS (
       SELECT 1 FROM ActiveDesiredRevision
       WHERE source_kind = 'BASELINE_ADOPTION'
     ) OR EXISTS (
       SELECT 1 FROM ObservedComponentState
       WHERE health_evidence_kind IS NOT NULL
          OR health_evidence_use IS NOT NULL
          OR health_policy_checksum IS NOT NULL
     ) THEN
    RAISE EXCEPTION
      'refusing migration 166 rollback: native baseline or observation health evidence exists'
      USING ERRCODE = '23514';
  END IF;
END;
$$;

DROP TRIGGER ObservedComponentState_health_evidence_immutable
  ON ObservedComponentState;
DROP FUNCTION observed_component_health_evidence_guard();

DROP TRIGGER BaselineAdoptionComponent_no_truncate
  ON BaselineAdoptionComponent;
DROP TRIGGER BaselineAdoptionComponent_append_only
  ON BaselineAdoptionComponent;
DROP TRIGGER BaselineAdoption_no_truncate ON BaselineAdoption;
DROP TRIGGER BaselineAdoption_append_only ON BaselineAdoption;
DROP TRIGGER BaselineAdoption_commit_guard ON BaselineAdoption;
DROP FUNCTION baseline_adoption_commit_guard();

DROP TRIGGER DeploymentPlan_baseline_adoption_status_guard
  ON DeploymentPlan;
DROP FUNCTION baseline_adoption_plan_status_guard();
DROP TRIGGER ExternalExecution_baseline_adoption_exclusion
  ON ExternalExecution;
DROP TRIGGER Task_baseline_adoption_exclusion ON Task;
DROP FUNCTION baseline_adoption_execution_exclusion_guard();
DROP TRIGGER BaselineAdoption_insert_guard ON BaselineAdoption;
DROP FUNCTION baseline_adoption_insert_guard();

ALTER TABLE BaselineAdoptionComponent
  DROP CONSTRAINT baselineadoptioncomponent_active_fk;

ALTER TABLE ActiveDesiredRevision
  DROP CONSTRAINT activedesiredrevision_baseline_component_fk,
  DROP CONSTRAINT activedesiredrevision_source_shape_check,
  DROP CONSTRAINT activedesiredrevision_baseline_component_unique,
  DROP COLUMN health_evidence_use,
  DROP COLUMN health_evidence_kind,
  DROP COLUMN baseline_adoption_component_id,
  DROP COLUMN source_kind,
  ALTER COLUMN execution_id SET NOT NULL,
  ALTER COLUMN pending_revision_id SET NOT NULL;

DROP TABLE BaselineAdoptionComponent;
DROP TABLE BaselineAdoption;

ALTER TABLE ObservedComponentState
  DROP CONSTRAINT observedcomponentstate_health_evidence_shape,
  DROP COLUMN health_policy_checksum,
  DROP COLUMN health_evidence_use,
  DROP COLUMN health_evidence_kind;
