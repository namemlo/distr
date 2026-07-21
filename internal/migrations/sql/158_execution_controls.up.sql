ALTER TABLE ExecutionAttempt
  DROP CONSTRAINT executionattempt_status_check,
  ADD CONSTRAINT executionattempt_status_check CHECK (
    status IN (
      'PENDING', 'CLAIMED', 'RUNNING', 'SUCCEEDED', 'FAILED',
      'CANCELED', 'TIMED_OUT', 'FENCED', 'UNKNOWN'
    )
  );

ALTER TABLE ExecutionAttempt
  DROP CONSTRAINT executionattempt_completion_check,
  ADD CONSTRAINT executionattempt_completion_check CHECK (
    (
      status IN ('PENDING', 'CLAIMED', 'RUNNING', 'UNKNOWN')
      AND completed_at IS NULL
    )
    OR (
      status IN ('SUCCEEDED', 'FAILED', 'CANCELED', 'TIMED_OUT', 'FENCED')
      AND completed_at IS NOT NULL
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
    UNIQUE (organization_id, execution_attempt_id, idempotency_key),
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

ALTER TABLE ExecutionCancelRequest
  ADD CONSTRAINT executioncancelrequest_id_org_execution_attempt_unique
    UNIQUE (id, organization_id, execution_id, execution_attempt_id);

ALTER TABLE DeploymentCampaignMemberRun
  ADD CONSTRAINT deploymentcampaignmemberrun_execution_lineage_unique
    UNIQUE (id, organization_id, campaign_run_id, deployment_plan_id);

CREATE TABLE CampaignMemberTaskExecution (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  campaign_run_id UUID NOT NULL,
  campaign_member_run_id UUID NOT NULL,
  deployment_plan_id UUID NOT NULL,
  task_id UUID NOT NULL,
  deployment_target_id UUID NOT NULL,
  CONSTRAINT campaignmembertaskexecution_member_fk
    FOREIGN KEY (
      campaign_member_run_id,
      organization_id,
      campaign_run_id,
      deployment_plan_id
    ) REFERENCES DeploymentCampaignMemberRun(
      id,
      organization_id,
      campaign_run_id,
      deployment_plan_id
    ) ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT campaignmembertaskexecution_task_fk
    FOREIGN KEY (task_id, deployment_plan_id, organization_id)
    REFERENCES Task(id, deployment_plan_id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT campaignmembertaskexecution_target_fk
    FOREIGN KEY (deployment_target_id, organization_id)
    REFERENCES DeploymentTarget(id, organization_id)
    ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT campaignmembertaskexecution_task_unique
    UNIQUE (organization_id, task_id),
  CONSTRAINT campaignmembertaskexecution_id_org_task_unique
    UNIQUE (id, organization_id, task_id)
);

CREATE TABLE ExecutionCampaignControlHandoff (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  execution_cancel_request_id UUID NOT NULL,
  execution_id UUID NOT NULL,
  execution_attempt_id UUID NOT NULL,
  campaign_member_task_execution_id UUID NOT NULL,
  task_id UUID NOT NULL,
  control_kind TEXT NOT NULL CHECK (control_kind = 'CANCEL_REQUESTED'),
  CONSTRAINT executioncampaigncontrolhandoff_cancel_fk
    FOREIGN KEY (
      execution_cancel_request_id,
      organization_id,
      execution_id,
      execution_attempt_id
    ) REFERENCES ExecutionCancelRequest(
      id,
      organization_id,
      execution_id,
      execution_attempt_id
    ) ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT executioncampaigncontrolhandoff_lineage_fk
    FOREIGN KEY (
      campaign_member_task_execution_id,
      organization_id,
      task_id
    ) REFERENCES CampaignMemberTaskExecution(id, organization_id, task_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT executioncampaigncontrolhandoff_cancel_unique
    UNIQUE (organization_id, execution_cancel_request_id)
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
  requested_ttl_seconds INTEGER NOT NULL CHECK (
    requested_ttl_seconds BETWEEN 30 AND 3600
  ),
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
    UNIQUE (organization_id, execution_attempt_id, idempotency_key),
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
    UNIQUE (id, organization_id, execution_id),
  ADD CONSTRAINT executionstatusquery_id_org_execution_attempt_unique
    UNIQUE (id, organization_id, execution_id, execution_attempt_id);

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
  evidence_payload BYTEA NOT NULL CHECK (
    octet_length(evidence_payload) BETWEEN 2 AND 1048576
  ),
  evidence_envelope_checksum TEXT NOT NULL CHECK (
    evidence_envelope_checksum ~ '^sha256:[0-9a-f]{64}$'
    AND evidence_envelope_checksum =
      'sha256:' || encode(sha256(evidence_payload), 'hex')
  ),
  evidence_key_id TEXT NOT NULL CHECK (
    evidence_key_id ~ '^sha256:[0-9a-f]{64}$'
  ),
  evidence_signature TEXT NOT NULL CHECK (
    length(evidence_signature) BETWEEN 80 AND 128
    AND evidence_signature !~ E'[\r\n]'
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
  CONSTRAINT executionreconciliationevent_query_attempt_fk
    FOREIGN KEY (
      status_query_id,
      organization_id,
      execution_id,
      execution_attempt_id
    )
    REFERENCES ExecutionStatusQuery(
      id,
      organization_id,
      execution_id,
      execution_attempt_id
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

CREATE TRIGGER CampaignMemberTaskExecution_append_only
BEFORE UPDATE OR DELETE ON CampaignMemberTaskExecution
FOR EACH ROW EXECUTE FUNCTION execution_reconciliation_append_only_guard();

CREATE TRIGGER CampaignMemberTaskExecution_no_truncate
BEFORE TRUNCATE ON CampaignMemberTaskExecution
FOR EACH STATEMENT EXECUTE FUNCTION execution_reconciliation_append_only_guard();

CREATE TRIGGER ExecutionCampaignControlHandoff_append_only
BEFORE UPDATE OR DELETE ON ExecutionCampaignControlHandoff
FOR EACH ROW EXECUTE FUNCTION execution_reconciliation_append_only_guard();

CREATE TRIGGER ExecutionCampaignControlHandoff_no_truncate
BEFORE TRUNCATE ON ExecutionCampaignControlHandoff
FOR EACH STATEMENT EXECUTE FUNCTION execution_reconciliation_append_only_guard();
