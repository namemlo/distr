SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

LOCK TABLE ControlPlaneAuditEvent, Organization_UserAccount,
  CustomerOrganization, DeploymentTarget
  IN SHARE ROW EXCLUSIVE MODE;

CREATE FUNCTION protected_history_checksum_field(value TEXT)
RETURNS BYTEA
LANGUAGE sql
IMMUTABLE
STRICT
AS $$
  SELECT int8send(octet_length(convert_to(value, 'UTF8'))::BIGINT)
    || convert_to(value, 'UTF8');
$$;

CREATE FUNCTION protected_history_checksum_uuid_set(values UUID[])
RETURNS BYTEA
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
  value UUID;
  payload BYTEA := protected_history_checksum_field(cardinality(values)::TEXT);
BEGIN
  FOREACH value IN ARRAY values LOOP
    payload := payload || protected_history_checksum_field(value::TEXT);
  END LOOP;
  RETURN payload;
END;
$$;

CREATE FUNCTION protected_history_rfc3339_microseconds(value TIMESTAMPTZ)
RETURNS TEXT
LANGUAGE plpgsql
STABLE
STRICT
AS $$
DECLARE
  fraction TEXT := rtrim(to_char(value AT TIME ZONE 'UTC', 'US'), '0');
  result TEXT := to_char(
    value AT TIME ZONE 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS'
  );
BEGIN
  IF fraction <> '' THEN
    result := result || '.' || fraction;
  END IF;
  RETURN result || 'Z';
END;
$$;

CREATE FUNCTION protected_history_request_checksum(
  organization_id UUID,
  customer_organization_ids UUID[],
  deployment_target_ids UUID[],
  issuer_useraccount_id UUID,
  reviewer_useraccount_id UUID,
  idempotency_key TEXT
)
RETURNS TEXT
LANGUAGE sql
IMMUTABLE
STRICT
AS $$
  SELECT 'sha256:' || encode(digest(
    protected_history_checksum_field(
      'distr.protected-history-retention/request/v1'
    )
    || protected_history_checksum_field(organization_id::TEXT)
    || protected_history_checksum_field(organization_id::TEXT)
    || protected_history_checksum_uuid_set(customer_organization_ids)
    || protected_history_checksum_uuid_set(deployment_target_ids)
    || protected_history_checksum_field(issuer_useraccount_id::TEXT)
    || protected_history_checksum_field(reviewer_useraccount_id::TEXT)
    || protected_history_checksum_field(idempotency_key),
    'sha256'
  ), 'hex');
$$;

CREATE FUNCTION protected_history_retention_checksum(
  id UUID,
  organization_id UUID,
  schema_name TEXT,
  source_schema_version BIGINT,
  customer_organization_ids UUID[],
  deployment_target_ids UUID[],
  artifact_id TEXT,
  records_root TEXT,
  record_count BIGINT,
  object_reference TEXT,
  media_type TEXT,
  byte_length BIGINT,
  content_checksum TEXT,
  captured_at TIMESTAMPTZ,
  issuer_useraccount_id UUID,
  reviewer_useraccount_id UUID
)
RETURNS TEXT
LANGUAGE sql
STABLE
STRICT
AS $$
  SELECT 'sha256:' || encode(digest(
    protected_history_checksum_field(
      'distr.protected-history-retention/material/v1'
    )
    || protected_history_checksum_field(schema_name)
    || protected_history_checksum_field(id::TEXT)
    || protected_history_checksum_field(organization_id::TEXT)
    || protected_history_checksum_field(source_schema_version::TEXT)
    || protected_history_checksum_field(organization_id::TEXT)
    || protected_history_checksum_uuid_set(customer_organization_ids)
    || protected_history_checksum_uuid_set(deployment_target_ids)
    || protected_history_checksum_field(artifact_id)
    || protected_history_checksum_field(records_root)
    || protected_history_checksum_field(record_count::TEXT)
    || protected_history_checksum_field(object_reference)
    || protected_history_checksum_field(media_type)
    || protected_history_checksum_field(byte_length::TEXT)
    || protected_history_checksum_field(content_checksum)
    || protected_history_checksum_field(
      protected_history_rfc3339_microseconds(captured_at)
    )
    || protected_history_checksum_field(issuer_useraccount_id::TEXT)
    || protected_history_checksum_field(reviewer_useraccount_id::TEXT),
    'sha256'
  ), 'hex');
$$;

CREATE FUNCTION protected_history_audit_binding_checksum(
  id UUID,
  retention_checksum TEXT,
  audit_event_id UUID,
  audit_event_sequence BIGINT
)
RETURNS TEXT
LANGUAGE sql
IMMUTABLE
STRICT
AS $$
  SELECT 'sha256:' || encode(digest(
    protected_history_checksum_field(
      'distr.protected-history-retention/audit-binding/v1'
    )
    || protected_history_checksum_field(id::TEXT)
    || protected_history_checksum_field(retention_checksum)
    || protected_history_checksum_field(audit_event_id::TEXT)
    || protected_history_checksum_field(audit_event_sequence::TEXT),
    'sha256'
  ), 'hex');
$$;

ALTER TABLE ControlPlaneAuditEvent
  ADD COLUMN protected_history_artifact_id UUID,
  ADD CONSTRAINT controlplaneauditevent_exact_identity_unique
    UNIQUE (id, organization_id, sequence);

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
      sample_retirement_recovery_evidence_id,
      protected_history_artifact_id
    ) > 0
  );

CREATE TABLE ProtectedHistoryArtifact (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  schema TEXT NOT NULL CHECK (
    schema = 'distr.protected-history-retention/v1'
  ),
  source_schema_version BIGINT NOT NULL CHECK (
    source_schema_version BETWEEN 138 AND 170
  ),
  customer_organization_ids UUID[] NOT NULL DEFAULT '{}',
  deployment_target_ids UUID[] NOT NULL DEFAULT '{}',
  artifact_id TEXT NOT NULL CHECK (
    artifact_id ~ '^sha256:[0-9a-f]{64}$'
  ),
  records_root TEXT NOT NULL CHECK (
    records_root ~ '^sha256:[0-9a-f]{64}$'
  ),
  record_count BIGINT NOT NULL CHECK (record_count > 0),
  object_reference TEXT NOT NULL CHECK (
    object_reference = btrim(object_reference)
    AND octet_length(object_reference) BETWEEN 1 AND 2048
    AND object_reference ~
      '^s3://[^/[:space:]]+/_immutable/sha256/[0-9a-f]{64}/[^?#[:space:]]+$'
    AND position(E'\\' IN object_reference) = 0
    AND position('/../' IN object_reference) = 0
  ),
  media_type TEXT NOT NULL CHECK (
    media_type = 'application/vnd.distr.protected-history.v1+json'
  ),
  byte_length BIGINT NOT NULL CHECK (
    byte_length BETWEEN 1 AND 268435456
  ),
  content_checksum TEXT NOT NULL CHECK (
    content_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  captured_at TIMESTAMPTZ NOT NULL CHECK (
    captured_at = date_trunc('microseconds', captured_at)
  ),
  issuer_useraccount_id UUID NOT NULL,
  reviewer_useraccount_id UUID NOT NULL,
  retention_checksum TEXT NOT NULL CHECK (
    retention_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  audit_event_id UUID NOT NULL,
  audit_event_sequence BIGINT NOT NULL CHECK (audit_event_sequence > 0),
  audit_binding_checksum TEXT NOT NULL CHECK (
    audit_binding_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  idempotency_key TEXT NOT NULL CHECK (
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
  ),
  request_checksum TEXT NOT NULL CHECK (
    request_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT protectedhistoryartifact_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT protectedhistoryartifact_idempotency_unique
    UNIQUE (organization_id, idempotency_key),
  CONSTRAINT protectedhistoryartifact_audit_event_unique
    UNIQUE (audit_event_id, organization_id),
  CONSTRAINT protectedhistoryartifact_scope_required CHECK (
    cardinality(customer_organization_ids)
      + cardinality(deployment_target_ids) > 0
  ),
  CONSTRAINT protectedhistoryartifact_distinct_review_check CHECK (
    issuer_useraccount_id <> reviewer_useraccount_id
  ),
  CONSTRAINT protectedhistoryartifact_object_checksum_check CHECK (
    content_checksum = 'sha256:' || split_part(object_reference, '/', 6)
  ),
  CONSTRAINT protectedhistoryartifact_capture_time_check CHECK (
    captured_at <= created_at
  ),
  CONSTRAINT protectedhistoryartifact_request_checksum_check CHECK (
    request_checksum = protected_history_request_checksum(
      organization_id,
      customer_organization_ids,
      deployment_target_ids,
      issuer_useraccount_id,
      reviewer_useraccount_id,
      idempotency_key
    )
  ),
  CONSTRAINT protectedhistoryartifact_retention_checksum_check CHECK (
    retention_checksum = protected_history_retention_checksum(
      id,
      organization_id,
      schema,
      source_schema_version,
      customer_organization_ids,
      deployment_target_ids,
      artifact_id,
      records_root,
      record_count,
      object_reference,
      media_type,
      byte_length,
      content_checksum,
      captured_at,
      issuer_useraccount_id,
      reviewer_useraccount_id
    )
  ),
  CONSTRAINT protectedhistoryartifact_audit_binding_checksum_check CHECK (
    audit_binding_checksum = protected_history_audit_binding_checksum(
      id,
      retention_checksum,
      audit_event_id,
      audit_event_sequence
    )
  ),
  CONSTRAINT protectedhistoryartifact_organization_fk
    FOREIGN KEY (organization_id)
    REFERENCES Organization(id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT protectedhistoryartifact_issuer_organization_fk
    FOREIGN KEY (organization_id, issuer_useraccount_id)
    REFERENCES Organization_UserAccount(organization_id, user_account_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT protectedhistoryartifact_reviewer_organization_fk
    FOREIGN KEY (organization_id, reviewer_useraccount_id)
    REFERENCES Organization_UserAccount(organization_id, user_account_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT protectedhistoryartifact_audit_event_fk
    FOREIGN KEY (audit_event_id, organization_id, audit_event_sequence)
    REFERENCES ControlPlaneAuditEvent(id, organization_id, sequence)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY DEFERRED
);

ALTER TABLE ControlPlaneAuditEvent
  ADD CONSTRAINT controlplaneauditevent_protected_history_artifact_fk
    FOREIGN KEY (protected_history_artifact_id, organization_id)
    REFERENCES ProtectedHistoryArtifact(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX ProtectedHistoryArtifact_organization_created
  ON ProtectedHistoryArtifact (organization_id, created_at DESC, id DESC);

CREATE INDEX ProtectedHistoryArtifact_content_checksum
  ON ProtectedHistoryArtifact (organization_id, content_checksum, id);

CREATE INDEX ControlPlaneAuditEvent_protected_history_artifact
  ON ControlPlaneAuditEvent (
    organization_id,
    protected_history_artifact_id,
    sequence
  )
  WHERE protected_history_artifact_id IS NOT NULL;

CREATE FUNCTION protected_history_artifact_validate_scope()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF EXISTS (
       SELECT id
       FROM unnest(NEW.customer_organization_ids) id
       GROUP BY id
       HAVING count(*) > 1
     )
     OR array_position(NEW.customer_organization_ids, NULL) IS NOT NULL
     OR NEW.customer_organization_ids IS DISTINCT FROM ARRAY(
       SELECT id
       FROM unnest(NEW.customer_organization_ids) id
       ORDER BY id
     )
     OR EXISTS (
       SELECT id
       FROM unnest(NEW.deployment_target_ids) id
       GROUP BY id
       HAVING count(*) > 1
     )
     OR array_position(NEW.deployment_target_ids, NULL) IS NOT NULL
     OR NEW.deployment_target_ids IS DISTINCT FROM ARRAY(
       SELECT id
       FROM unnest(NEW.deployment_target_ids) id
       ORDER BY id
     ) THEN
    RAISE EXCEPTION 'protected-history scope arrays must be sorted and unique'
      USING ERRCODE = '23514';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM unnest(NEW.customer_organization_ids) requested(id)
    LEFT JOIN CustomerOrganization customer
      ON customer.id = requested.id
     AND customer.organization_id = NEW.organization_id
    WHERE customer.id IS NULL
  ) THEN
    RAISE EXCEPTION 'protected-history customer scope is outside its organization'
      USING ERRCODE = '23503';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM unnest(NEW.deployment_target_ids) requested(id)
    LEFT JOIN DeploymentTarget target
      ON target.id = requested.id
     AND target.organization_id = NEW.organization_id
    WHERE target.id IS NULL
  ) THEN
    RAISE EXCEPTION 'protected-history target scope is outside its organization'
      USING ERRCODE = '23503';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER ProtectedHistoryArtifact_validate_scope
BEFORE INSERT ON ProtectedHistoryArtifact
FOR EACH ROW EXECUTE FUNCTION protected_history_artifact_validate_scope();

CREATE FUNCTION protected_history_artifact_append_only_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'protected-history artifact records are append-only'
    USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER ProtectedHistoryArtifact_append_only
BEFORE UPDATE OR DELETE ON ProtectedHistoryArtifact
FOR EACH ROW EXECUTE FUNCTION protected_history_artifact_append_only_guard();

CREATE TRIGGER ProtectedHistoryArtifact_no_truncate
BEFORE TRUNCATE ON ProtectedHistoryArtifact
FOR EACH STATEMENT EXECUTE FUNCTION protected_history_artifact_append_only_guard();

CREATE FUNCTION protected_history_artifact_audit_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM ControlPlaneAuditEvent event
    WHERE event.id = NEW.audit_event_id
      AND event.organization_id = NEW.organization_id
      AND event.sequence = NEW.audit_event_sequence
      AND event.protected_history_artifact_id = NEW.id
      AND event.event_type = 'protected_history.retained'
      AND event.actor_id = NEW.issuer_useraccount_id
      AND event.outcome = 'SUCCEEDED'
      AND event.artifact_digest = NEW.content_checksum
      AND event.payload_redacted = false
      AND event.payload_truncated = false
      AND event.payload ->> 'retentionChecksum' = NEW.retention_checksum
      AND event.payload ->> 'requestChecksum' = NEW.request_checksum
      AND event.payload ->> 'artifactId' = NEW.artifact_id
      AND event.payload ->> 'recordsRoot' = NEW.records_root
      AND event.payload ->> 'objectReference' = NEW.object_reference
      AND event.payload ->> 'mediaType' = NEW.media_type
      AND event.payload ->> 'byteLength' = NEW.byte_length::TEXT
      AND event.payload ->> 'contentChecksum' = NEW.content_checksum
      AND event.payload ->> 'capturedAt'
        = protected_history_rfc3339_microseconds(NEW.captured_at)
      AND event.payload ->> 'issuerUserAccountId'
        = NEW.issuer_useraccount_id::TEXT
      AND event.payload ->> 'reviewerUserAccountId'
        = NEW.reviewer_useraccount_id::TEXT
  ) THEN
    RAISE EXCEPTION 'protected-history artifact audit binding is incomplete'
      USING ERRCODE = '23514';
  END IF;
  RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER ProtectedHistoryArtifact_audit_guard
AFTER INSERT ON ProtectedHistoryArtifact
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION protected_history_artifact_audit_guard();
