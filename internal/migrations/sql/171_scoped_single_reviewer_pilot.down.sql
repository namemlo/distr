SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

LOCK TABLE ProtectedHistoryArtifact, ApprovalDecision
  IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM ProtectedHistoryArtifact
    WHERE governance_exception_key IS NOT NULL
       OR governance_exception_reference IS NOT NULL
  ) OR EXISTS (
    SELECT 1 FROM ApprovalDecision
    WHERE governance_exception_key IS NOT NULL
       OR governance_exception_reference IS NOT NULL
  ) THEN
    RAISE EXCEPTION
      'refusing migration 171 rollback while pilot governance exception evidence exists';
  END IF;

  IF EXISTS (
    SELECT 1 FROM ProtectedHistoryArtifact
    WHERE source_schema_version > 170
  ) THEN
    RAISE EXCEPTION
      'refusing migration 171 rollback while schema-171 protected-history evidence exists';
  END IF;
END;
$$;

ALTER TABLE ApprovalDecision
  DROP CONSTRAINT approvaldecision_governance_exception_check,
  DROP COLUMN governance_exception_key,
  DROP COLUMN governance_exception_reference;

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
  ) THEN
    RAISE EXCEPTION 'protected-history artifact audit binding is incomplete'
      USING ERRCODE = '23514';
  END IF;
  RETURN NULL;
END;
$$;

ALTER TABLE ProtectedHistoryArtifact
  DROP CONSTRAINT protectedhistoryartifact_source_schema_version_check,
  DROP CONSTRAINT protectedhistoryartifact_review_governance_check,
  DROP CONSTRAINT protectedhistoryartifact_request_checksum_matches,
  DROP CONSTRAINT protectedhistoryartifact_retention_checksum_matches;

DROP FUNCTION protected_history_request_checksum(
  UUID, UUID[], UUID[], UUID, UUID, TEXT, TEXT, TEXT
);
DROP FUNCTION protected_history_retention_checksum(
  UUID, UUID, TEXT, BIGINT, UUID[], UUID[], TEXT, TEXT, BIGINT,
  TEXT, TEXT, BIGINT, TEXT, TIMESTAMPTZ, UUID, UUID, TEXT, TEXT
);

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
  SELECT 'sha256:' || encode(sha256(
    protected_history_checksum_field(
      'distr.protected-history-retention/request/v1'
    )
    || protected_history_checksum_field(organization_id::TEXT)
    || protected_history_checksum_field(organization_id::TEXT)
    || protected_history_checksum_uuid_set(customer_organization_ids)
    || protected_history_checksum_uuid_set(deployment_target_ids)
    || protected_history_checksum_field(issuer_useraccount_id::TEXT)
    || protected_history_checksum_field(reviewer_useraccount_id::TEXT)
    || protected_history_checksum_field(idempotency_key)
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
  SELECT 'sha256:' || encode(sha256(
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
    || protected_history_checksum_field(reviewer_useraccount_id::TEXT)
  ), 'hex');
$$;

ALTER TABLE ProtectedHistoryArtifact
  DROP COLUMN governance_exception_key,
  DROP COLUMN governance_exception_reference,
  ADD CONSTRAINT protectedhistoryartifact_source_schema_version_check CHECK (
    source_schema_version BETWEEN 138 AND 170
  ),
  ADD CONSTRAINT protectedhistoryartifact_distinct_review_check CHECK (
    issuer_useraccount_id <> reviewer_useraccount_id
  ),
  ADD CONSTRAINT protectedhistoryartifact_request_checksum_matches CHECK (
    request_checksum = protected_history_request_checksum(
      organization_id,
      customer_organization_ids,
      deployment_target_ids,
      issuer_useraccount_id,
      reviewer_useraccount_id,
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
      reviewer_useraccount_id
    )
  );
