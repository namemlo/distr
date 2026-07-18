ALTER TABLE ComponentInstance
  ADD CONSTRAINT componentinstance_id_unit_organization_unique
  UNIQUE (id, deployment_unit_id, organization_id);

CREATE TABLE PendingDesiredRevision (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  deployment_plan_id UUID NOT NULL,
  execution_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  component_instance_id UUID NOT NULL,
  component_key TEXT NOT NULL CHECK (
    length(btrim(component_key)) BETWEEN 1 AND 256
  ),
  revision BIGINT NOT NULL CHECK (revision > 0),
  artifact_digest TEXT NOT NULL CHECK (
    artifact_digest ~ '^sha256:[0-9a-f]{64}$'
  ),
  config_checksum TEXT NOT NULL CHECK (
    config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  schema_version TEXT NOT NULL CHECK (
    length(btrim(schema_version)) BETWEEN 1 AND 256
  ),
  capability_checksum TEXT NOT NULL CHECK (
    capability_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  platform TEXT NOT NULL CHECK (
    platform ~ '^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$'
  ),
  topology_checksum TEXT NOT NULL CHECK (
    topology_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  observation_deadline TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL CHECK (
    status IN (
      'PENDING', 'VERIFIED', 'PARTIAL', 'FAILED', 'CANCELLED',
      'UNKNOWN', 'TIMED_OUT', 'CONFLICT'
    )
  ),
  terminal_reason TEXT NOT NULL DEFAULT '' CHECK (
    length(terminal_reason) <= 2048
    AND terminal_reason !~ E'[\\r\\n]'
  ),
  verified_observation_id UUID,
  terminal_observation_id UUID,
  terminal_at TIMESTAMPTZ,
  CONSTRAINT pendingdesiredrevision_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT pendingdesiredrevision_execution_unique
    UNIQUE (id, execution_id, organization_id),
  CONSTRAINT pendingdesiredrevision_component_revision_unique
    UNIQUE (
      organization_id, deployment_unit_id, component_instance_id, revision
    ),
  CONSTRAINT pendingdesiredrevision_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id) ON DELETE CASCADE,
  CONSTRAINT pendingdesiredrevision_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT pendingdesiredrevision_unit_fk
    FOREIGN KEY (deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT pendingdesiredrevision_instance_placement_fk
    FOREIGN KEY (
      component_instance_id, deployment_unit_id, organization_id
    )
    REFERENCES ComponentInstance(id, deployment_unit_id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT pendingdesiredrevision_terminal_shape_check CHECK (
    (
      status = 'PENDING'
      AND terminal_at IS NULL
      AND verified_observation_id IS NULL
      AND terminal_observation_id IS NULL
    )
    OR (
      status = 'VERIFIED'
      AND terminal_at IS NOT NULL
      AND verified_observation_id IS NOT NULL
      AND terminal_observation_id IS NULL
    )
    OR (
      status = 'TIMED_OUT'
      AND terminal_at IS NOT NULL
      AND verified_observation_id IS NULL
      AND terminal_observation_id IS NULL
    )
    OR (
      status NOT IN ('PENDING', 'VERIFIED', 'TIMED_OUT')
      AND terminal_at IS NOT NULL
      AND verified_observation_id IS NULL
      AND terminal_observation_id IS NOT NULL
    )
  )
);

CREATE INDEX PendingDesiredRevision_component_status
  ON PendingDesiredRevision (
    organization_id, deployment_unit_id, component_instance_id,
    status, created_at DESC
  );

CREATE TABLE ActiveDesiredRevision (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  pending_revision_id UUID NOT NULL,
  deployment_plan_id UUID NOT NULL,
  execution_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  component_instance_id UUID NOT NULL,
  component_key TEXT NOT NULL CHECK (
    length(btrim(component_key)) BETWEEN 1 AND 256
  ),
  revision BIGINT NOT NULL CHECK (revision > 0),
  artifact_digest TEXT NOT NULL CHECK (
    artifact_digest ~ '^sha256:[0-9a-f]{64}$'
  ),
  config_checksum TEXT NOT NULL CHECK (
    config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  schema_version TEXT NOT NULL CHECK (
    length(btrim(schema_version)) BETWEEN 1 AND 256
  ),
  capability_checksum TEXT NOT NULL CHECK (
    capability_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  platform TEXT NOT NULL CHECK (
    platform ~ '^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$'
  ),
  topology_checksum TEXT NOT NULL CHECK (
    topology_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  verified_observation_id UUID NOT NULL,
  CONSTRAINT activedesiredrevision_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT activedesiredrevision_placement_unique
    UNIQUE (
      id, deployment_unit_id, component_instance_id, organization_id
    ),
  CONSTRAINT activedesiredrevision_pending_unique
    UNIQUE (pending_revision_id),
  CONSTRAINT activedesiredrevision_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id) ON DELETE CASCADE,
  CONSTRAINT activedesiredrevision_pending_fk
    FOREIGN KEY (pending_revision_id, organization_id)
    REFERENCES PendingDesiredRevision(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT activedesiredrevision_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT activedesiredrevision_unit_fk
    FOREIGN KEY (deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT activedesiredrevision_instance_placement_fk
    FOREIGN KEY (
      component_instance_id, deployment_unit_id, organization_id
    )
    REFERENCES ComponentInstance(id, deployment_unit_id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
);

CREATE INDEX ActiveDesiredRevision_component_history
  ON ActiveDesiredRevision (
    organization_id, deployment_unit_id, component_instance_id,
    revision DESC, created_at DESC
  );

CREATE TABLE ComponentDesiredStateHead (
  organization_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  component_instance_id UUID NOT NULL,
  component_key TEXT NOT NULL CHECK (
    length(btrim(component_key)) BETWEEN 1 AND 256
  ),
  pending_revision_id UUID,
  active_revision_id UUID,
  quarantined BOOLEAN NOT NULL DEFAULT FALSE,
  quarantine_reason TEXT NOT NULL DEFAULT '' CHECK (
    length(quarantine_reason) <= 2048
    AND quarantine_reason !~ E'[\\r\\n]'
  ),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (
    organization_id, deployment_unit_id, component_instance_id
  ),
  CONSTRAINT componentdesiredstatehead_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id) ON DELETE CASCADE,
  CONSTRAINT componentdesiredstatehead_unit_fk
    FOREIGN KEY (deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT componentdesiredstatehead_instance_placement_fk
    FOREIGN KEY (
      component_instance_id, deployment_unit_id, organization_id
    )
    REFERENCES ComponentInstance(id, deployment_unit_id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT componentdesiredstatehead_pending_fk
    FOREIGN KEY (pending_revision_id, organization_id)
    REFERENCES PendingDesiredRevision(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT componentdesiredstatehead_active_fk
    FOREIGN KEY (active_revision_id, organization_id)
    REFERENCES ActiveDesiredRevision(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT componentdesiredstatehead_quarantine_shape_check CHECK (
    (quarantined AND length(btrim(quarantine_reason)) > 0)
    OR (NOT quarantined AND quarantine_reason = '')
  )
);

CREATE TABLE ExecutorReport (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  pending_revision_id UUID NOT NULL,
  execution_id UUID NOT NULL,
  outcome TEXT NOT NULL CHECK (
    outcome IN ('SUCCEEDED', 'FAILED', 'CANCELLED', 'UNKNOWN')
  ),
  reported_state_checksum TEXT NOT NULL DEFAULT '' CHECK (
    reported_state_checksum = ''
    OR reported_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  evidence_reference TEXT NOT NULL DEFAULT '' CHECK (
    length(evidence_reference) <= 2048
  ),
  CONSTRAINT executorreport_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT executorreport_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id) ON DELETE CASCADE,
  CONSTRAINT executorreport_pending_execution_fk
    FOREIGN KEY (pending_revision_id, execution_id, organization_id)
    REFERENCES PendingDesiredRevision(id, execution_id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
);

CREATE INDEX ExecutorReport_pending
  ON ExecutorReport (organization_id, pending_revision_id, created_at DESC);

CREATE TABLE ObserverRegistration (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  component_instance_id UUID,
  observer_key TEXT NOT NULL CHECK (
    observer_key ~ '^[a-z0-9][a-z0-9._-]{0,255}$'
  ),
  adapter_implementation TEXT NOT NULL CHECK (
    length(btrim(adapter_implementation)) BETWEEN 1 AND 256
  ),
  adapter_version TEXT NOT NULL CHECK (
    length(btrim(adapter_version)) BETWEEN 1 AND 128
  ),
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  credential_fingerprint TEXT NOT NULL CHECK (
    credential_fingerprint ~ '^sha256:[0-9a-f]{64}$'
  ),
  max_freshness_seconds BIGINT NOT NULL CHECK (
    max_freshness_seconds BETWEEN 1 AND 86400
  ),
  max_clock_skew_seconds BIGINT NOT NULL CHECK (
    max_clock_skew_seconds BETWEEN 0 AND 300
  ),
  measurements TEXT[] NOT NULL CHECK (
    cardinality(measurements) BETWEEN 1 AND 32
  ),
  CONSTRAINT observerregistration_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT observerregistration_scope_key_unique
    UNIQUE (
      organization_id, deployment_unit_id, component_instance_id, observer_key
    ),
  CONSTRAINT observerregistration_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id) ON DELETE CASCADE,
  CONSTRAINT observerregistration_unit_fk
    FOREIGN KEY (deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT observerregistration_instance_placement_fk
    FOREIGN KEY (
      component_instance_id, deployment_unit_id, organization_id
    )
    REFERENCES ComponentInstance(id, deployment_unit_id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE
);

CREATE INDEX ObserverRegistration_enabled_scope
  ON ObserverRegistration (
    organization_id, deployment_unit_id, enabled, observer_key
  );

CREATE TABLE ObservedComponentState (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  observer_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  component_instance_id UUID NOT NULL,
  component_key TEXT NOT NULL CHECK (
    length(btrim(component_key)) BETWEEN 1 AND 256
  ),
  source_sequence BIGINT NOT NULL CHECK (source_sequence > 0),
  captured_at TIMESTAMPTZ NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  fresh_until TIMESTAMPTZ NOT NULL CHECK (fresh_until >= captured_at),
  evidence_checksum TEXT NOT NULL CHECK (
    evidence_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  evidence_reference TEXT NOT NULL DEFAULT '' CHECK (
    length(evidence_reference) <= 2048
  ),
  artifact_digest TEXT NOT NULL CHECK (
    artifact_digest ~ '^sha256:[0-9a-f]{64}$'
  ),
  config_checksum TEXT NOT NULL CHECK (
    config_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  schema_version TEXT NOT NULL CHECK (
    length(btrim(schema_version)) BETWEEN 1 AND 256
  ),
  capability_checksum TEXT NOT NULL CHECK (
    capability_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  platform TEXT NOT NULL CHECK (
    platform ~ '^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$'
  ),
  topology_checksum TEXT NOT NULL CHECK (
    topology_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  health TEXT NOT NULL CHECK (
    health IN ('UNKNOWN', 'HEALTHY', 'UNHEALTHY')
  ),
  outcome TEXT NOT NULL CHECK (
    outcome IN ('COMPLETE', 'PARTIAL', 'UNKNOWN', 'CANCELLED', 'FAILED')
  ),
  disposition TEXT NOT NULL CHECK (
    disposition IN (
      'ACCEPTED', 'REPLAY', 'OUT_OF_ORDER', 'CONFLICT', 'REJECTED'
    )
  ),
  trusted BOOLEAN NOT NULL,
  is_current BOOLEAN NOT NULL DEFAULT FALSE,
  state_checksum TEXT NOT NULL CHECK (
    state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  executor_outcome TEXT NOT NULL DEFAULT '' CHECK (
    executor_outcome IN ('', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'UNKNOWN')
  ),
  CONSTRAINT observedcomponentstate_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT observedcomponentstate_placement_unique
    UNIQUE (
      id, deployment_unit_id, component_instance_id, organization_id
    ),
  CONSTRAINT observedcomponentstate_replay_unique
    UNIQUE (
      organization_id, observer_id, deployment_unit_id,
      component_instance_id, source_sequence, state_checksum
    ),
  CONSTRAINT observedcomponentstate_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id) ON DELETE CASCADE,
  CONSTRAINT observedcomponentstate_observer_fk
    FOREIGN KEY (observer_id, organization_id)
    REFERENCES ObserverRegistration(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT observedcomponentstate_unit_fk
    FOREIGN KEY (deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT observedcomponentstate_instance_placement_fk
    FOREIGN KEY (
      component_instance_id, deployment_unit_id, organization_id
    )
    REFERENCES ComponentInstance(id, deployment_unit_id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT observedcomponentstate_current_shape_check CHECK (
    NOT is_current OR (trusted AND disposition = 'ACCEPTED')
  )
);

CREATE UNIQUE INDEX ObservedComponentState_current_observer_component
  ON ObservedComponentState (
    organization_id, observer_id, deployment_unit_id, component_instance_id
  )
  WHERE is_current;

CREATE INDEX ObservedComponentState_component_history
  ON ObservedComponentState (
    organization_id, deployment_unit_id, component_instance_id,
    captured_at DESC, id DESC
  );

ALTER TABLE PendingDesiredRevision
  ADD CONSTRAINT pendingdesiredrevision_verified_observation_fk
  FOREIGN KEY (verified_observation_id, organization_id)
  REFERENCES ObservedComponentState(id, organization_id)
  ON UPDATE NO ACTION ON DELETE NO ACTION;

ALTER TABLE PendingDesiredRevision
  ADD CONSTRAINT pendingdesiredrevision_terminal_observation_fk
  FOREIGN KEY (terminal_observation_id, organization_id)
  REFERENCES ObservedComponentState(id, organization_id)
  ON UPDATE NO ACTION ON DELETE NO ACTION;

ALTER TABLE ActiveDesiredRevision
  ADD CONSTRAINT activedesiredrevision_verified_observation_fk
  FOREIGN KEY (verified_observation_id, organization_id)
  REFERENCES ObservedComponentState(id, organization_id)
  ON UPDATE NO ACTION ON DELETE NO ACTION;

DO $$
BEGIN
  IF to_regclass('CampaignPrerequisiteEvaluation') IS NOT NULL THEN
    ALTER TABLE CampaignPrerequisiteEvaluation
      ADD CONSTRAINT campaignprerequisiteevaluation_observation_fk
      FOREIGN KEY (actual_observation_id, organization_id)
      REFERENCES ObservedComponentState(id, organization_id)
      ON UPDATE NO ACTION ON DELETE NO ACTION;
  END IF;
END;
$$;

CREATE TABLE ComponentObservationHead (
  organization_id UUID NOT NULL,
  observer_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  component_instance_id UUID NOT NULL,
  source_sequence BIGINT NOT NULL CHECK (source_sequence > 0),
  observation_id UUID NOT NULL,
  evidence_checksum TEXT NOT NULL CHECK (
    evidence_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  captured_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (
    organization_id, observer_id, deployment_unit_id, component_instance_id
  ),
  CONSTRAINT componentobservationhead_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id) ON DELETE CASCADE,
  CONSTRAINT componentobservationhead_observer_fk
    FOREIGN KEY (observer_id, organization_id)
    REFERENCES ObserverRegistration(id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT componentobservationhead_observation_fk
    FOREIGN KEY (observation_id, organization_id)
    REFERENCES ObservedComponentState(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
);

CREATE TABLE DriftCase (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  active_desired_revision_id UUID NOT NULL,
  observation_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  component_instance_id UUID NOT NULL,
  status TEXT NOT NULL CHECK (
    status IN ('OPEN', 'ASSIGNED', 'EXCEPTION', 'RESOLVED')
  ),
  classes TEXT[] NOT NULL CHECK (cardinality(classes) BETWEEN 1 AND 16),
  summary TEXT NOT NULL CHECK (
    length(btrim(summary)) BETWEEN 1 AND 2048
    AND summary !~ E'[\\r\\n]'
  ),
  assigned_to UUID,
  resolved_at TIMESTAMPTZ,
  CONSTRAINT driftcase_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT driftcase_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id) ON DELETE CASCADE,
  CONSTRAINT driftcase_active_placement_fk
    FOREIGN KEY (
      active_desired_revision_id, deployment_unit_id,
      component_instance_id, organization_id
    )
    REFERENCES ActiveDesiredRevision(
      id, deployment_unit_id, component_instance_id, organization_id
    )
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT driftcase_observation_placement_fk
    FOREIGN KEY (
      observation_id, deployment_unit_id,
      component_instance_id, organization_id
    )
    REFERENCES ObservedComponentState(
      id, deployment_unit_id, component_instance_id, organization_id
    )
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT driftcase_resolution_shape_check CHECK (
    (status = 'RESOLVED' AND resolved_at IS NOT NULL)
    OR (status <> 'RESOLVED' AND resolved_at IS NULL)
  )
);

CREATE UNIQUE INDEX DriftCase_open_lineage_unique
  ON DriftCase (
    organization_id, active_desired_revision_id, observation_id
  )
  WHERE status IN ('OPEN', 'ASSIGNED', 'EXCEPTION');

CREATE INDEX DriftCase_organization_status
  ON DriftCase (organization_id, status, updated_at DESC, id DESC);

CREATE TABLE DriftCaseEvent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  drift_case_id UUID NOT NULL,
  status TEXT NOT NULL CHECK (
    status IN ('OPEN', 'ASSIGNED', 'EXCEPTION', 'RESOLVED')
  ),
  actor_id UUID,
  reason TEXT NOT NULL CHECK (
    length(reason) <= 2048 AND reason !~ E'[\\r\\n]'
  ),
  CONSTRAINT driftcaseevent_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT driftcaseevent_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id) ON DELETE CASCADE,
  CONSTRAINT driftcaseevent_case_fk
    FOREIGN KEY (drift_case_id, organization_id)
    REFERENCES DriftCase(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION
);

CREATE INDEX DriftCaseEvent_case_history
  ON DriftCaseEvent (
    organization_id, drift_case_id, created_at, id
  );

CREATE TABLE ReconciliationAction (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  drift_case_id UUID NOT NULL,
  action TEXT NOT NULL CHECK (
    action IN (
      'RESTORE_DESIRED', 'CREATE_PLAN',
      'ACCEPT_DEVIATION', 'CLOSE_WITH_EVIDENCE'
    )
  ),
  reason TEXT NOT NULL CHECK (
    length(btrim(reason)) BETWEEN 1 AND 2048
    AND reason !~ E'[\\r\\n]'
  ),
  actor_id UUID NOT NULL,
  deployment_plan_id UUID,
  outcome_observation_id UUID,
  accepted_until TIMESTAMPTZ,
  CONSTRAINT reconciliationaction_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT reconciliationaction_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id) ON DELETE CASCADE,
  CONSTRAINT reconciliationaction_case_fk
    FOREIGN KEY (drift_case_id, organization_id)
    REFERENCES DriftCase(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT reconciliationaction_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT reconciliationaction_outcome_observation_fk
    FOREIGN KEY (outcome_observation_id, organization_id)
    REFERENCES ObservedComponentState(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT reconciliationaction_shape_check CHECK (
    (
      action = 'ACCEPT_DEVIATION'
      AND deployment_plan_id IS NULL
      AND outcome_observation_id IS NULL
      AND accepted_until IS NOT NULL
      AND accepted_until > created_at
    )
    OR (
      action = 'CREATE_PLAN'
      AND deployment_plan_id IS NOT NULL
      AND outcome_observation_id IS NULL
      AND accepted_until IS NULL
    )
    OR (
      action IN ('RESTORE_DESIRED', 'CLOSE_WITH_EVIDENCE')
      AND deployment_plan_id IS NULL
      AND outcome_observation_id IS NOT NULL
      AND accepted_until IS NULL
    )
  )
);

CREATE INDEX ReconciliationAction_case_history
  ON ReconciliationAction (
    organization_id, drift_case_id, created_at, id
  );

CREATE FUNCTION desired_observed_append_only_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.deployment_registry_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'desired and observed evidence is append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER ActiveDesiredRevision_append_only
BEFORE UPDATE OR DELETE ON ActiveDesiredRevision
FOR EACH ROW EXECUTE FUNCTION desired_observed_append_only_guard();
CREATE TRIGGER ActiveDesiredRevision_no_truncate
BEFORE TRUNCATE ON ActiveDesiredRevision
FOR EACH STATEMENT EXECUTE FUNCTION desired_observed_append_only_guard();

CREATE TRIGGER ExecutorReport_append_only
BEFORE UPDATE OR DELETE ON ExecutorReport
FOR EACH ROW EXECUTE FUNCTION desired_observed_append_only_guard();
CREATE TRIGGER ExecutorReport_no_truncate
BEFORE TRUNCATE ON ExecutorReport
FOR EACH STATEMENT EXECUTE FUNCTION desired_observed_append_only_guard();

CREATE TRIGGER ObservedComponentState_append_only
BEFORE DELETE ON ObservedComponentState
FOR EACH ROW EXECUTE FUNCTION desired_observed_append_only_guard();
CREATE TRIGGER ObservedComponentState_no_truncate
BEFORE TRUNCATE ON ObservedComponentState
FOR EACH STATEMENT EXECUTE FUNCTION desired_observed_append_only_guard();

CREATE TRIGGER DriftCaseEvent_append_only
BEFORE UPDATE OR DELETE ON DriftCaseEvent
FOR EACH ROW EXECUTE FUNCTION desired_observed_append_only_guard();
CREATE TRIGGER DriftCaseEvent_no_truncate
BEFORE TRUNCATE ON DriftCaseEvent
FOR EACH STATEMENT EXECUTE FUNCTION desired_observed_append_only_guard();

CREATE TRIGGER ReconciliationAction_append_only
BEFORE UPDATE OR DELETE ON ReconciliationAction
FOR EACH ROW EXECUTE FUNCTION desired_observed_append_only_guard();
CREATE TRIGGER ReconciliationAction_no_truncate
BEFORE TRUNCATE ON ReconciliationAction
FOR EACH STATEMENT EXECUTE FUNCTION desired_observed_append_only_guard();

CREATE FUNCTION observed_component_state_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF OLD.is_current
     AND NOT NEW.is_current
     AND NEW.id = OLD.id
     AND NEW.created_at = OLD.created_at
     AND NEW.organization_id = OLD.organization_id
     AND NEW.observer_id = OLD.observer_id
     AND NEW.deployment_unit_id = OLD.deployment_unit_id
     AND NEW.component_instance_id = OLD.component_instance_id
     AND NEW.component_key = OLD.component_key
     AND NEW.source_sequence = OLD.source_sequence
     AND NEW.captured_at = OLD.captured_at
     AND NEW.received_at = OLD.received_at
     AND NEW.fresh_until = OLD.fresh_until
     AND NEW.evidence_checksum = OLD.evidence_checksum
     AND NEW.evidence_reference = OLD.evidence_reference
     AND NEW.artifact_digest = OLD.artifact_digest
     AND NEW.config_checksum = OLD.config_checksum
     AND NEW.schema_version = OLD.schema_version
     AND NEW.capability_checksum = OLD.capability_checksum
     AND NEW.platform = OLD.platform
     AND NEW.topology_checksum = OLD.topology_checksum
     AND NEW.health = OLD.health
     AND NEW.outcome = OLD.outcome
     AND NEW.disposition = OLD.disposition
     AND NEW.trusted = OLD.trusted
     AND NEW.state_checksum = OLD.state_checksum
     AND NEW.executor_outcome = OLD.executor_outcome THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'observed state evidence is immutable'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER ObservedComponentState_retire_head_only
BEFORE UPDATE ON ObservedComponentState
FOR EACH ROW EXECUTE FUNCTION observed_component_state_guard();

CREATE FUNCTION pending_desired_revision_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF current_setting(
         'distr.deployment_registry_deletion_reason',
         true
       ) = 'ORGANIZATION_RETENTION' THEN
      RETURN OLD;
    END IF;
    RAISE EXCEPTION 'pending desired revision evidence is append-only'
      USING ERRCODE = '23514';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.deployment_plan_id IS DISTINCT FROM OLD.deployment_plan_id
     OR NEW.execution_id IS DISTINCT FROM OLD.execution_id
     OR NEW.deployment_unit_id IS DISTINCT FROM OLD.deployment_unit_id
     OR NEW.component_instance_id IS DISTINCT FROM OLD.component_instance_id
     OR NEW.component_key IS DISTINCT FROM OLD.component_key
     OR NEW.revision IS DISTINCT FROM OLD.revision
     OR NEW.artifact_digest IS DISTINCT FROM OLD.artifact_digest
     OR NEW.config_checksum IS DISTINCT FROM OLD.config_checksum
     OR NEW.schema_version IS DISTINCT FROM OLD.schema_version
     OR NEW.capability_checksum IS DISTINCT FROM OLD.capability_checksum
     OR NEW.platform IS DISTINCT FROM OLD.platform
     OR NEW.topology_checksum IS DISTINCT FROM OLD.topology_checksum
     OR NEW.observation_deadline IS DISTINCT FROM OLD.observation_deadline
     OR OLD.status <> 'PENDING'
     OR NEW.status = 'PENDING' THEN
    RAISE EXCEPTION 'pending desired revision intent and terminal outcome are immutable'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER PendingDesiredRevision_guard
BEFORE UPDATE OR DELETE ON PendingDesiredRevision
FOR EACH ROW EXECUTE FUNCTION pending_desired_revision_guard();
CREATE TRIGGER PendingDesiredRevision_no_truncate
BEFORE TRUNCATE ON PendingDesiredRevision
FOR EACH STATEMENT EXECUTE FUNCTION desired_observed_append_only_guard();
