ALTER TABLE ExecutionAttempt
  DROP CONSTRAINT executionattempt_status_check,
  ADD CONSTRAINT executionattempt_status_check CHECK (
    status IN (
      'PENDING', 'CLAIMED', 'RUNNING', 'SUCCEEDED', 'FAILED',
      'CANCELED', 'TIMED_OUT', 'FENCED', 'UNKNOWN'
    )
  );

ALTER TABLE ExecutionAttempt
  ADD CONSTRAINT executionattempt_id_org_execution_unique
    UNIQUE (id, organization_id, execution_id);

CREATE TABLE ExecutionCancelRequest (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  execution_id UUID NOT NULL,
  execution_attempt_id UUID NOT NULL,
  requested_by UUID NOT NULL,
  idempotency_key TEXT NOT NULL CHECK (
    length(btrim(idempotency_key)) BETWEEN 1 AND 128
    AND idempotency_key !~ E'[\r\n]'
  ),
  reason TEXT NOT NULL CHECK (
    length(btrim(reason)) BETWEEN 1 AND 2048
    AND reason !~ E'[\r\n]'
  ),
  status TEXT NOT NULL DEFAULT 'REQUESTED' CHECK (
    status IN ('REQUESTED', 'ACKNOWLEDGED', 'REJECTED')
  ),
  acknowledged_at TIMESTAMPTZ,
  acknowledged_by TEXT NOT NULL DEFAULT '' CHECK (
    length(acknowledged_by) <= 128
    AND acknowledged_by !~ E'[\r\n]'
  ),
  CONSTRAINT executioncancelrequest_attempt_fk
    FOREIGN KEY (
      execution_attempt_id,
      organization_id,
      execution_id
    )
    REFERENCES ExecutionAttempt(
      id,
      organization_id,
      execution_id
    )
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT executioncancelrequest_idempotency_unique
    UNIQUE (organization_id, execution_id, idempotency_key),
  CONSTRAINT executioncancelrequest_ack_check CHECK (
    (
      status = 'REQUESTED'
      AND acknowledged_at IS NULL
      AND acknowledged_by = ''
    )
    OR (
      status IN ('ACKNOWLEDGED', 'REJECTED')
      AND acknowledged_at IS NOT NULL
      AND length(btrim(acknowledged_by)) > 0
    )
  )
);

CREATE TABLE ExecutionStatusQuery (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  execution_id UUID NOT NULL,
  execution_attempt_id UUID NOT NULL,
  requested_by UUID NOT NULL,
  idempotency_key TEXT NOT NULL CHECK (
    length(btrim(idempotency_key)) BETWEEN 1 AND 128
    AND idempotency_key !~ E'[\r\n]'
  ),
  reason TEXT NOT NULL CHECK (
    length(btrim(reason)) BETWEEN 1 AND 2048
    AND reason !~ E'[\r\n]'
  ),
  status TEXT NOT NULL DEFAULT 'PENDING' CHECK (
    status IN ('PENDING', 'REPORTED', 'EXPIRED')
  ),
  expires_at TIMESTAMPTZ NOT NULL,
  reported_at TIMESTAMPTZ,
  CONSTRAINT executionstatusquery_attempt_fk
    FOREIGN KEY (
      execution_attempt_id,
      organization_id,
      execution_id
    )
    REFERENCES ExecutionAttempt(
      id,
      organization_id,
      execution_id
    )
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT executionstatusquery_idempotency_unique
    UNIQUE (organization_id, execution_id, idempotency_key),
  CONSTRAINT executionstatusquery_interval_check CHECK (
    expires_at > created_at
  ),
  CONSTRAINT executionstatusquery_report_check CHECK (
    (
      status = 'REPORTED'
      AND reported_at IS NOT NULL
    )
    OR (
      status IN ('PENDING', 'EXPIRED')
      AND reported_at IS NULL
    )
  )
);

ALTER TABLE ExecutionStatusQuery
  ADD CONSTRAINT executionstatusquery_id_org_execution_unique
    UNIQUE (id, organization_id, execution_id);

CREATE TABLE ExecutionReconciliationEvent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  execution_id UUID NOT NULL,
  execution_attempt_id UUID NOT NULL,
  status_query_id UUID NOT NULL,
  event_identity UUID NOT NULL,
  outcome TEXT NOT NULL CHECK (
    outcome IN ('PROVEN_SUCCEEDED', 'PROVEN_FAILED', 'UNKNOWN')
  ),
  evidence_checksum TEXT NOT NULL CHECK (
    evidence_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  observed_at TIMESTAMPTZ NOT NULL,
  operation_incomplete BOOLEAN NOT NULL,
  retry_requested BOOLEAN NOT NULL,
  retry_disposition TEXT NOT NULL CHECK (
    retry_disposition IN ('ALLOWED', 'FORBIDDEN', 'NOT_REQUESTED')
  ),
  CONSTRAINT executionreconciliationevent_attempt_fk
    FOREIGN KEY (
      execution_attempt_id,
      organization_id,
      execution_id
    )
    REFERENCES ExecutionAttempt(
      id,
      organization_id,
      execution_id
    )
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT executionreconciliationevent_query_fk
    FOREIGN KEY (
      status_query_id,
      organization_id,
      execution_id
    )
    REFERENCES ExecutionStatusQuery(
      id,
      organization_id,
      execution_id
    )
    ON UPDATE NO ACTION
    ON DELETE CASCADE,
  CONSTRAINT executionreconciliationevent_identity_unique
    UNIQUE (organization_id, event_identity)
);

CREATE INDEX ExecutionCancelRequest_execution_status
  ON ExecutionCancelRequest (
    organization_id,
    execution_id,
    status,
    created_at,
    id
  );

CREATE INDEX ExecutionStatusQuery_execution_status
  ON ExecutionStatusQuery (
    organization_id,
    execution_id,
    status,
    created_at,
    id
  );

CREATE INDEX ExecutionReconciliationEvent_execution_created
  ON ExecutionReconciliationEvent (
    organization_id,
    execution_id,
    created_at,
    id
  );

CREATE FUNCTION execution_reconciliation_append_only_guard()
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
  RAISE EXCEPTION 'execution reconciliation events are append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER ExecutionReconciliationEvent_append_only
BEFORE UPDATE OR DELETE ON ExecutionReconciliationEvent
FOR EACH ROW EXECUTE FUNCTION execution_reconciliation_append_only_guard();

CREATE TRIGGER ExecutionReconciliationEvent_no_truncate
BEFORE TRUNCATE ON ExecutionReconciliationEvent
FOR EACH STATEMENT EXECUTE FUNCTION execution_reconciliation_append_only_guard();
