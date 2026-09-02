SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE ProtectedHistoryArtifact
  ADD COLUMN governance_exception_key TEXT,
  ADD COLUMN governance_exception_reference TEXT,
  DROP CONSTRAINT protectedhistoryartifact_source_schema_version_check,
  DROP CONSTRAINT protectedhistoryartifact_distinct_review_check,
  DROP CONSTRAINT protectedhistoryartifact_request_checksum_matches,
  DROP CONSTRAINT protectedhistoryartifact_retention_checksum_matches;

ALTER TABLE ProtectedHistoryArtifact
  ADD CONSTRAINT protectedhistoryartifact_source_schema_version_check CHECK (
    source_schema_version BETWEEN 138 AND 171
  );

DROP FUNCTION protected_history_request_checksum(
  UUID, UUID[], UUID[], UUID, UUID, TEXT
);
DROP FUNCTION protected_history_retention_checksum(
  UUID, UUID, TEXT, BIGINT, UUID[], UUID[], TEXT, TEXT, BIGINT,
  TEXT, TEXT, BIGINT, TEXT, TIMESTAMPTZ, UUID, UUID
);

CREATE FUNCTION protected_history_request_checksum(
  organization_id UUID,
  customer_organization_ids UUID[],
  deployment_target_ids UUID[],
  issuer_useraccount_id UUID,
  reviewer_useraccount_id UUID,
  governance_exception_key TEXT,
  governance_exception_reference TEXT,
  idempotency_key TEXT
)
RETURNS TEXT
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
  payload BYTEA;
BEGIN
  payload := protected_history_checksum_field(
      'distr.protected-history-retention/request/v1'
    )
    || protected_history_checksum_field(organization_id::TEXT)
    || protected_history_checksum_field(organization_id::TEXT)
    || protected_history_checksum_uuid_set(customer_organization_ids)
    || protected_history_checksum_uuid_set(deployment_target_ids)
    || protected_history_checksum_field(issuer_useraccount_id::TEXT)
    || protected_history_checksum_field(reviewer_useraccount_id::TEXT);
  IF governance_exception_key IS NOT NULL THEN
    payload := payload
      || protected_history_checksum_field(governance_exception_key)
      || protected_history_checksum_field(governance_exception_reference);
  END IF;
  payload := payload || protected_history_checksum_field(idempotency_key);
  RETURN 'sha256:' || encode(sha256(payload), 'hex');
END;
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
  reviewer_useraccount_id UUID,
  governance_exception_key TEXT,
  governance_exception_reference TEXT
)
RETURNS TEXT
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
  payload BYTEA;
BEGIN
  payload := protected_history_checksum_field(
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
    || protected_history_checksum_field(reviewer_useraccount_id::TEXT);
  IF governance_exception_key IS NOT NULL THEN
    payload := payload
      || protected_history_checksum_field(governance_exception_key)
      || protected_history_checksum_field(governance_exception_reference);
  END IF;
  RETURN 'sha256:' || encode(sha256(payload), 'hex');
END;
$$;

ALTER TABLE ProtectedHistoryArtifact
  ADD CONSTRAINT protectedhistoryartifact_review_governance_check CHECK (
    (
      issuer_useraccount_id <> reviewer_useraccount_id
      AND governance_exception_key IS NULL
      AND governance_exception_reference IS NULL
    )
    OR
    (
      issuer_useraccount_id = reviewer_useraccount_id
      AND governance_exception_key IS NOT NULL
      AND governance_exception_reference IS NOT NULL
      AND governance_exception_key = 'scoped-single-reviewer-pilot'
      AND governance_exception_reference ~
        '^[A-Za-z0-9][A-Za-z0-9._:/-]{7,255}$'
    )
  ),
  ADD CONSTRAINT protectedhistoryartifact_request_checksum_matches CHECK (
    request_checksum = protected_history_request_checksum(
      organization_id,
      customer_organization_ids,
      deployment_target_ids,
      issuer_useraccount_id,
      reviewer_useraccount_id,
      governance_exception_key,
      governance_exception_reference,
      idempotency_key
    )
  ),
  ADD CONSTRAINT protectedhistoryartifact_retention_checksum_matches CHECK (
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
      reviewer_useraccount_id,
      governance_exception_key,
      governance_exception_reference
    )
  );

CREATE OR REPLACE FUNCTION protected_history_artifact_audit_guard()
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
      AND event.payload ->> 'governanceExceptionKey'
        IS NOT DISTINCT FROM NEW.governance_exception_key
      AND event.payload ->> 'governanceExceptionReference'
        IS NOT DISTINCT FROM NEW.governance_exception_reference
  ) THEN
    RAISE EXCEPTION 'protected-history artifact audit binding is incomplete'
      USING ERRCODE = '23514';
  END IF;
  RETURN NULL;
END;
$$;

ALTER TABLE ApprovalDecision
  ADD COLUMN governance_exception_key TEXT,
  ADD COLUMN governance_exception_reference TEXT,
  ADD CONSTRAINT approvaldecision_governance_exception_check CHECK (
    (
      governance_exception_key IS NULL
      AND governance_exception_reference IS NULL
    )
    OR
    (
      decision = 'APPROVE'
      AND governance_exception_key IS NOT NULL
      AND governance_exception_reference IS NOT NULL
      AND governance_exception_key = 'scoped-single-reviewer-pilot'
      AND governance_exception_reference ~
        '^[A-Za-z0-9][A-Za-z0-9._:/-]{7,255}$'
    )
  );
