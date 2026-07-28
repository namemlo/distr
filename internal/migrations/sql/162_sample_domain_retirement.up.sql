CREATE TABLE SampleRetirementRecoveryEvidence (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE NO ACTION,
  evidence_kind TEXT NOT NULL CHECK (
    evidence_kind IN ('backup', 'restore_proof')
  ),
  reference TEXT NOT NULL CHECK (
    reference = btrim(reference)
    AND length(reference) BETWEEN 1 AND 1024
    AND reference !~ E'[\\r\\n]'
  ),
  checksum TEXT NOT NULL CHECK (
    checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  source_kind TEXT NOT NULL CHECK (
    source_kind = btrim(source_kind)
    AND source_kind ~ '^[a-z][a-z0-9._-]{0,127}$'
  ),
  source_id UUID NOT NULL,
  source_checksum TEXT NOT NULL CHECK (
    source_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  verified_at TIMESTAMPTZ NOT NULL,
  verified_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  CONSTRAINT sampleretirementrecoveryevidence_id_org_unique
    UNIQUE (id, organization_id),
  CONSTRAINT sampleretirementrecoveryevidence_exact_unique
    UNIQUE (
      organization_id,
      evidence_kind,
      reference,
      checksum
    ),
  CONSTRAINT sampleretirementrecoveryevidence_time_check
    CHECK (verified_at <= created_at)
);

CREATE TABLE SampleRetirementOwnershipEvidence (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE NO ACTION,
  subject_type TEXT NOT NULL CHECK (
    subject_type IN ('application', 'deployment_target', 'environment')
  ),
  subject_id UUID NOT NULL,
  ownership_marker TEXT NOT NULL CHECK (
    ownership_marker = btrim(ownership_marker)
    AND length(ownership_marker) BETWEEN 1 AND 256
    AND ownership_marker !~ E'[\\r\\n]'
  ),
  ownership_checksum TEXT NOT NULL CHECK (
    ownership_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  source_reference TEXT NOT NULL CHECK (
    source_reference = btrim(source_reference)
    AND length(source_reference) BETWEEN 1 AND 1024
    AND source_reference !~ E'[\\r\\n]'
  ),
  source_checksum TEXT NOT NULL CHECK (
    source_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  recorded_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  CONSTRAINT sampleretirementownershipevidence_id_org_unique
    UNIQUE (id, organization_id),
  CONSTRAINT sampleretirementownershipevidence_id_org_subject_unique
    UNIQUE (id, organization_id, subject_type, subject_id),
  CONSTRAINT sampleretirementownershipevidence_exact_binding_unique
    UNIQUE (
      id,
      organization_id,
      subject_type,
      subject_id,
      ownership_marker,
      ownership_checksum
    ),
  CONSTRAINT sampleretirementownershipevidence_subject_unique
    UNIQUE (organization_id, subject_type, subject_id)
);

ALTER TABLE ControlPlaneAuditEvent
  ADD COLUMN sample_retirement_ownership_evidence_id UUID,
  ADD COLUMN sample_retirement_recovery_evidence_id UUID,
  ADD CONSTRAINT controlplaneauditevent_retirement_ownership_evidence_fk
    FOREIGN KEY (
      sample_retirement_ownership_evidence_id,
      organization_id
    )
    REFERENCES SampleRetirementOwnershipEvidence(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  ADD CONSTRAINT controlplaneauditevent_retirement_recovery_evidence_fk
    FOREIGN KEY (
      sample_retirement_recovery_evidence_id,
      organization_id
    )
    REFERENCES SampleRetirementRecoveryEvidence(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION;

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
      sample_retirement_recovery_evidence_id
    ) > 0
  );

CREATE TABLE SampleRetirementJob (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  requested_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  state TEXT NOT NULL DEFAULT 'PREVIEWED' CHECK (
    state IN (
      'PREVIEWED',
      'APPLYING',
      'APPLIED',
      'VERIFIED',
      'FAILED'
    )
  ),
  backup_evidence_id UUID NOT NULL,
  backup_reference TEXT NOT NULL CHECK (
    backup_reference = btrim(backup_reference)
    AND length(backup_reference) BETWEEN 1 AND 1024
    AND backup_reference !~ E'[\\r\\n]'
  ),
  backup_checksum TEXT NOT NULL CHECK (
    backup_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  restore_proof_evidence_id UUID NOT NULL,
  restore_proof_reference TEXT NOT NULL CHECK (
    restore_proof_reference = btrim(restore_proof_reference)
    AND length(restore_proof_reference) BETWEEN 1 AND 1024
    AND restore_proof_reference !~ E'[\\r\\n]'
  ),
  restore_proof_checksum TEXT NOT NULL CHECK (
    restore_proof_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  approval_id TEXT CHECK (
    approval_id IS NULL
    OR (
      approval_id = btrim(approval_id)
      AND approval_id ~
        '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    )
  ),
  approval_checksum TEXT CHECK (
    approval_checksum IS NULL
    OR approval_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  allowlist_checksum TEXT NOT NULL CHECK (
    allowlist_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  preview_checksum TEXT NOT NULL CHECK (
    preview_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  requested_item_count INTEGER NOT NULL CHECK (
    requested_item_count BETWEEN 1 AND 1000
  ),
  previewed_item_count INTEGER NOT NULL CHECK (
    previewed_item_count BETWEEN 0 AND requested_item_count
  ),
  applied_item_count INTEGER NOT NULL DEFAULT 0 CHECK (
    applied_item_count >= 0
  ),
  skipped_item_count INTEGER NOT NULL DEFAULT 0 CHECK (
    skipped_item_count >= 0
  ),
  tombstone_count INTEGER NOT NULL DEFAULT 0 CHECK (
    tombstone_count >= 0
  ),
  failed_item_count INTEGER NOT NULL DEFAULT 0 CHECK (
    failed_item_count >= 0
  ),
  last_checkpoint_sequence BIGINT NOT NULL DEFAULT 0 CHECK (
    last_checkpoint_sequence >= 0
  ),
  completed_at TIMESTAMPTZ,
  verified_at TIMESTAMPTZ,
  version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
  CONSTRAINT sampleretirementjob_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT sampleretirementjob_backup_evidence_fk
    FOREIGN KEY (backup_evidence_id, organization_id)
    REFERENCES SampleRetirementRecoveryEvidence(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT sampleretirementjob_restore_proof_evidence_fk
    FOREIGN KEY (restore_proof_evidence_id, organization_id)
    REFERENCES SampleRetirementRecoveryEvidence(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION,
  CONSTRAINT sampleretirementjob_counts_check CHECK (
    applied_item_count + skipped_item_count + failed_item_count
      <= requested_item_count
    AND tombstone_count = applied_item_count
  ),
  CONSTRAINT sampleretirementjob_approval_shape_check CHECK (
    (
      state = 'PREVIEWED'
      AND approval_id IS NULL
      AND approval_checksum IS NULL
    )
    OR (
      state IN ('APPLYING', 'APPLIED', 'VERIFIED')
      AND approval_id IS NOT NULL
      AND approval_checksum IS NOT NULL
    )
    OR (
      state = 'FAILED'
      AND (
        (
          approval_id IS NULL
          AND approval_checksum IS NULL
        )
        OR (
          approval_id IS NOT NULL
          AND approval_checksum IS NOT NULL
        )
      )
    )
  ),
  CONSTRAINT sampleretirementjob_completion_check CHECK (
    (
      state IN ('PREVIEWED', 'APPLYING')
      AND completed_at IS NULL
      AND verified_at IS NULL
    )
    OR (
      state IN ('APPLIED', 'FAILED')
      AND completed_at IS NOT NULL
      AND verified_at IS NULL
    )
    OR (
      state = 'VERIFIED'
      AND completed_at IS NOT NULL
      AND verified_at IS NOT NULL
      AND verified_at >= completed_at
    )
  )
);

CREATE FUNCTION sample_retirement_job_evidence_binding_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM SampleRetirementRecoveryEvidence evidence
    WHERE evidence.id = NEW.backup_evidence_id
      AND evidence.organization_id = NEW.organization_id
      AND evidence.evidence_kind = 'backup'
      AND evidence.reference = NEW.backup_reference
      AND evidence.checksum = NEW.backup_checksum
  ) THEN
    RAISE EXCEPTION 'sample retirement backup evidence binding is invalid'
      USING ERRCODE = '23514';
  END IF;
  IF NOT EXISTS (
    SELECT 1
    FROM SampleRetirementRecoveryEvidence evidence
    WHERE evidence.id = NEW.restore_proof_evidence_id
      AND evidence.organization_id = NEW.organization_id
      AND evidence.evidence_kind = 'restore_proof'
      AND evidence.reference = NEW.restore_proof_reference
      AND evidence.checksum = NEW.restore_proof_checksum
  ) THEN
    RAISE EXCEPTION 'sample retirement restore proof binding is invalid'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER SampleRetirementJob_evidence_binding_guard
BEFORE INSERT OR UPDATE ON SampleRetirementJob
FOR EACH ROW
EXECUTE FUNCTION sample_retirement_job_evidence_binding_guard();

CREATE INDEX SampleRetirementJob_organization_history
  ON SampleRetirementJob (
    organization_id,
    created_at DESC,
    id DESC
  );

CREATE INDEX SampleRetirementJob_resumable
  ON SampleRetirementJob (
    organization_id,
    state,
    updated_at,
    id
  )
  WHERE state IN ('PREVIEWED', 'APPLYING');

ALTER TABLE ApprovalRequest
  DROP CONSTRAINT approvalrequest_plan_fk;

ALTER TABLE ApprovalRequest
  DROP CONSTRAINT approvalrequest_subject_type_check,
  ADD CONSTRAINT approvalrequest_subject_type_check CHECK (
    subject_type IN ('deployment_plan', 'sample_retirement')
  );

ALTER TABLE ApprovalRequest
  DROP CONSTRAINT approvalrequest_invalidation_reason_check,
  ADD CONSTRAINT approvalrequest_invalidation_reason_check CHECK (
    invalidation_reason IN (
      'expired',
      'superseded',
      'plan_changed',
      'policy_changed',
      'subscriber_set_changed',
      'campaign_member_unapproved',
      'sample_retirement_changed'
    )
  );

CREATE FUNCTION approval_request_subject_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.subject_type = 'deployment_plan' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM DeploymentPlan plan
      WHERE plan.id = NEW.subject_id
        AND plan.organization_id = NEW.organization_id
    ) THEN
      RAISE EXCEPTION 'approval deployment plan subject does not exist'
        USING ERRCODE = '23503';
    END IF;
  ELSIF NEW.subject_type = 'sample_retirement' THEN
    IF TG_OP = 'INSERT' AND NOT EXISTS (
      SELECT 1
      FROM SampleRetirementJob job
      WHERE job.id = NEW.subject_id
        AND job.organization_id = NEW.organization_id
        AND job.state = 'PREVIEWED'
        AND job.version = NEW.subject_revision
        AND job.preview_checksum = NEW.subject_checksum
    ) THEN
      RAISE EXCEPTION
        'approval sample retirement subject does not match frozen preview'
        USING ERRCODE = '23503';
    END IF;
    IF TG_OP = 'UPDATE' AND NOT EXISTS (
      SELECT 1
      FROM SampleRetirementJob job
      WHERE job.id = NEW.subject_id
        AND job.organization_id = NEW.organization_id
        AND job.preview_checksum = NEW.subject_checksum
    ) THEN
      RAISE EXCEPTION
        'approval sample retirement subject no longer exists'
        USING ERRCODE = '23503';
    END IF;
  ELSE
    RAISE EXCEPTION 'approval subject type is unsupported'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER ApprovalRequest_subject_guard
BEFORE INSERT OR UPDATE ON ApprovalRequest
FOR EACH ROW EXECUTE FUNCTION approval_request_subject_guard();

CREATE TABLE SampleRetirementItem (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  retirement_job_id UUID NOT NULL,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
  subject_type TEXT NOT NULL CHECK (
    subject_type IN ('application', 'deployment_target', 'environment')
  ),
  subject_id UUID NOT NULL,
  ownership_evidence_id UUID NOT NULL,
  ownership_marker TEXT NOT NULL CHECK (
    ownership_marker = btrim(ownership_marker)
    AND length(ownership_marker) BETWEEN 1 AND 256
    AND ownership_marker !~ E'[\\r\\n]'
  ),
  ownership_checksum TEXT NOT NULL CHECK (
    ownership_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  expected_checksum TEXT NOT NULL CHECK (
    expected_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  before_count INTEGER NOT NULL CHECK (before_count = 1),
  reference_report_checksum TEXT NOT NULL CHECK (
    reference_report_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  state TEXT NOT NULL DEFAULT 'PENDING' CHECK (
    state IN ('PENDING', 'APPLIED', 'SKIPPED', 'FAILED')
  ),
  applied_at TIMESTAMPTZ,
  tombstone_id UUID,
  error_code TEXT NOT NULL DEFAULT '' CHECK (
    length(error_code) <= 128
    AND error_code !~ E'[\\r\\n]'
  ),
  version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
  CONSTRAINT sampleretirementitem_id_job_organization_unique
    UNIQUE (id, retirement_job_id, organization_id),
  CONSTRAINT sampleretirementitem_job_fk
    FOREIGN KEY (retirement_job_id, organization_id)
    REFERENCES SampleRetirementJob(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT sampleretirementitem_ownership_evidence_fk
    FOREIGN KEY (
      ownership_evidence_id,
      organization_id,
      subject_type,
      subject_id,
      ownership_marker,
      ownership_checksum
    )
    REFERENCES SampleRetirementOwnershipEvidence(
      id,
      organization_id,
      subject_type,
      subject_id,
      ownership_marker,
      ownership_checksum
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT sampleretirementitem_ordinal_unique
    UNIQUE (retirement_job_id, ordinal),
  CONSTRAINT sampleretirementitem_subject_unique
    UNIQUE (retirement_job_id, subject_type, subject_id),
  CONSTRAINT sampleretirementitem_result_shape_check CHECK (
    (
      state = 'PENDING'
      AND applied_at IS NULL
      AND tombstone_id IS NULL
      AND error_code = ''
    )
    OR (
      state = 'APPLIED'
      AND applied_at IS NOT NULL
      AND tombstone_id IS NOT NULL
      AND error_code = ''
    )
    OR (
      state = 'SKIPPED'
      AND applied_at IS NOT NULL
      AND tombstone_id IS NULL
      AND error_code = ''
    )
    OR (
      state = 'FAILED'
      AND applied_at IS NULL
      AND tombstone_id IS NULL
      AND length(error_code) > 0
    )
  )
);

CREATE INDEX SampleRetirementItem_job_order
  ON SampleRetirementItem (
    organization_id,
    retirement_job_id,
    ordinal,
    id
  );

CREATE INDEX SampleRetirementItem_pending
  ON SampleRetirementItem (
    organization_id,
    retirement_job_id,
    ordinal,
    id
  )
  WHERE state = 'PENDING';

CREATE TABLE SampleRetirementCheckpoint (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL,
  retirement_job_id UUID NOT NULL,
  sequence BIGINT NOT NULL CHECK (sequence > 0),
  last_completed_ordinal INTEGER NOT NULL CHECK (
    last_completed_ordinal >= 0
  ),
  applied_item_count INTEGER NOT NULL CHECK (applied_item_count >= 0),
  skipped_item_count INTEGER NOT NULL CHECK (skipped_item_count >= 0),
  tombstone_count INTEGER NOT NULL CHECK (tombstone_count >= 0),
  checkpoint_checksum TEXT NOT NULL CHECK (
    checkpoint_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT sampleretirementcheckpoint_job_fk
    FOREIGN KEY (retirement_job_id, organization_id)
    REFERENCES SampleRetirementJob(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT sampleretirementcheckpoint_sequence_unique
    UNIQUE (retirement_job_id, sequence),
  CONSTRAINT sampleretirementcheckpoint_position_unique
    UNIQUE (retirement_job_id, last_completed_ordinal),
  CONSTRAINT sampleretirementcheckpoint_checksum_unique
    UNIQUE (retirement_job_id, checkpoint_checksum),
  CONSTRAINT sampleretirementcheckpoint_counts_check CHECK (
    tombstone_count = applied_item_count
    AND applied_item_count + skipped_item_count
      <= last_completed_ordinal + 1
  )
);

CREATE INDEX SampleRetirementCheckpoint_job_sequence
  ON SampleRetirementCheckpoint (
    organization_id,
    retirement_job_id,
    sequence DESC,
    id DESC
  );

CREATE TABLE AuditSubjectTombstone (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  retired_at TIMESTAMPTZ NOT NULL,
  organization_id UUID NOT NULL,
  retirement_job_id UUID NOT NULL,
  retirement_item_id UUID NOT NULL,
  subject_type TEXT NOT NULL CHECK (
    subject_type IN ('application', 'deployment_target', 'environment')
  ),
  subject_id UUID NOT NULL,
  ownership_marker TEXT NOT NULL CHECK (
    ownership_marker = btrim(ownership_marker)
    AND length(ownership_marker) BETWEEN 1 AND 256
    AND ownership_marker !~ E'[\\r\\n]'
  ),
  ownership_checksum TEXT NOT NULL CHECK (
    ownership_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  subject_checksum TEXT NOT NULL CHECK (
    subject_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  first_audit_event_id UUID,
  audit_event_count INTEGER NOT NULL CHECK (audit_event_count >= 0),
  retired_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  lineage_checksum TEXT NOT NULL CHECK (
    lineage_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT auditsubjecttombstone_id_item_organization_unique
    UNIQUE (id, retirement_item_id, organization_id),
  CONSTRAINT auditsubjecttombstone_subject_unique
    UNIQUE (organization_id, subject_type, subject_id),
  CONSTRAINT auditsubjecttombstone_lineage_unique
    UNIQUE (organization_id, lineage_checksum),
  CONSTRAINT auditsubjecttombstone_job_fk
    FOREIGN KEY (retirement_job_id, organization_id)
    REFERENCES SampleRetirementJob(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT auditsubjecttombstone_item_fk
    FOREIGN KEY (
      retirement_item_id,
      retirement_job_id,
      organization_id
    )
    REFERENCES SampleRetirementItem(
      id,
      retirement_job_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT auditsubjecttombstone_first_audit_event_fk
    FOREIGN KEY (first_audit_event_id, organization_id)
    REFERENCES ControlPlaneAuditEvent(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT auditsubjecttombstone_audit_shape_check CHECK (
    (
      audit_event_count = 0
      AND first_audit_event_id IS NULL
    )
    OR (
      audit_event_count > 0
      AND first_audit_event_id IS NOT NULL
    )
  ),
  CONSTRAINT auditsubjecttombstone_retired_at_check CHECK (
    retired_at >= created_at
  )
);

CREATE INDEX AuditSubjectTombstone_subject
  ON AuditSubjectTombstone (
    organization_id,
    subject_type,
    subject_id,
    retired_at DESC
  );

CREATE INDEX AuditSubjectTombstone_job
  ON AuditSubjectTombstone (
    organization_id,
    retirement_job_id,
    retired_at,
    id
  );

ALTER TABLE SampleRetirementItem
  ADD CONSTRAINT sampleretirementitem_tombstone_fk
  FOREIGN KEY (tombstone_id, id, organization_id)
  REFERENCES AuditSubjectTombstone(
    id,
    retirement_item_id,
    organization_id
  )
  ON UPDATE NO ACTION
  ON DELETE NO ACTION
  DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION sample_retirement_job_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'sample retirement jobs are retained as evidence'
      USING ERRCODE = '23514';
  END IF;

  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.requested_by_useraccount_id IS DISTINCT FROM
       OLD.requested_by_useraccount_id
     OR NEW.backup_evidence_id IS DISTINCT FROM OLD.backup_evidence_id
     OR NEW.backup_reference IS DISTINCT FROM OLD.backup_reference
     OR NEW.backup_checksum IS DISTINCT FROM OLD.backup_checksum
     OR NEW.restore_proof_reference IS DISTINCT FROM
       OLD.restore_proof_reference
     OR NEW.restore_proof_evidence_id IS DISTINCT FROM
       OLD.restore_proof_evidence_id
     OR NEW.restore_proof_checksum IS DISTINCT FROM
       OLD.restore_proof_checksum
     OR NEW.allowlist_checksum IS DISTINCT FROM OLD.allowlist_checksum
     OR NEW.preview_checksum IS DISTINCT FROM OLD.preview_checksum
     OR NEW.requested_item_count IS DISTINCT FROM
       OLD.requested_item_count
     OR NEW.previewed_item_count IS DISTINCT FROM
       OLD.previewed_item_count THEN
    RAISE EXCEPTION 'sample retirement proof and allowlist are immutable'
      USING ERRCODE = '23514';
  END IF;

  IF NEW.approval_id IS DISTINCT FROM OLD.approval_id
     OR NEW.approval_checksum IS DISTINCT FROM OLD.approval_checksum THEN
    IF NOT (
      OLD.state = 'PREVIEWED'
      AND NEW.state = 'APPLYING'
      AND OLD.approval_id IS NULL
      AND OLD.approval_checksum IS NULL
      AND NEW.approval_id IS NOT NULL
      AND NEW.approval_checksum IS NOT NULL
    ) THEN
      RAISE EXCEPTION
        'sample retirement approval can only bind when apply begins'
        USING ERRCODE = '23514';
    END IF;
  END IF;

  IF NEW.applied_item_count < OLD.applied_item_count
     OR NEW.skipped_item_count < OLD.skipped_item_count
     OR NEW.tombstone_count < OLD.tombstone_count
     OR NEW.failed_item_count < OLD.failed_item_count
     OR NEW.last_checkpoint_sequence < OLD.last_checkpoint_sequence THEN
    RAISE EXCEPTION 'sample retirement progress cannot move backwards'
      USING ERRCODE = '23514';
  END IF;

  IF NOT (
    (OLD.state = 'PREVIEWED' AND NEW.state IN (
      'APPLYING',
      'FAILED'
    ))
    OR (OLD.state = 'APPLYING' AND NEW.state IN (
      'APPLYING',
      'APPLIED',
      'FAILED'
    ))
    OR (OLD.state = 'APPLIED' AND NEW.state = 'VERIFIED')
  ) THEN
    RAISE EXCEPTION 'sample retirement job state transition is invalid'
      USING ERRCODE = '23514';
  END IF;

  IF NEW.version <> OLD.version + 1
     OR NEW.updated_at <= OLD.updated_at THEN
    RAISE EXCEPTION
      'sample retirement job updates require one optimistic revision'
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER SampleRetirementJob_guard
BEFORE UPDATE OR DELETE ON SampleRetirementJob
FOR EACH ROW EXECUTE FUNCTION sample_retirement_job_guard();

CREATE FUNCTION sample_retirement_item_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'sample retirement allowlist items are retained as evidence'
      USING ERRCODE = '23514';
  END IF;

  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.retirement_job_id IS DISTINCT FROM OLD.retirement_job_id
     OR NEW.ordinal IS DISTINCT FROM OLD.ordinal
     OR NEW.subject_type IS DISTINCT FROM OLD.subject_type
     OR NEW.subject_id IS DISTINCT FROM OLD.subject_id
     OR NEW.ownership_evidence_id IS DISTINCT FROM
       OLD.ownership_evidence_id
     OR NEW.ownership_marker IS DISTINCT FROM OLD.ownership_marker
     OR NEW.ownership_checksum IS DISTINCT FROM OLD.ownership_checksum
     OR NEW.expected_checksum IS DISTINCT FROM OLD.expected_checksum
     OR NEW.before_count IS DISTINCT FROM OLD.before_count
     OR NEW.reference_report_checksum IS DISTINCT FROM
       OLD.reference_report_checksum THEN
    RAISE EXCEPTION 'sample retirement allowlist binding is immutable'
      USING ERRCODE = '23514';
  END IF;

  IF NOT (
    (OLD.state = 'PENDING' AND NEW.state IN (
      'APPLIED',
      'SKIPPED',
      'FAILED'
    ))
  ) THEN
    RAISE EXCEPTION 'sample retirement item state transition is invalid'
      USING ERRCODE = '23514';
  END IF;

  IF NEW.version <> OLD.version + 1
     OR NEW.updated_at <= OLD.updated_at THEN
    RAISE EXCEPTION
      'sample retirement item updates require one optimistic revision'
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER SampleRetirementItem_guard
BEFORE UPDATE OR DELETE ON SampleRetirementItem
FOR EACH ROW EXECUTE FUNCTION sample_retirement_item_guard();

CREATE FUNCTION sample_retirement_evidence_append_only_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION '% rows are append-only evidence', TG_TABLE_NAME
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER SampleRetirementJob_no_truncate
BEFORE TRUNCATE ON SampleRetirementJob
FOR EACH STATEMENT
EXECUTE FUNCTION sample_retirement_evidence_append_only_guard();

CREATE TRIGGER SampleRetirementItem_no_truncate
BEFORE TRUNCATE ON SampleRetirementItem
FOR EACH STATEMENT
EXECUTE FUNCTION sample_retirement_evidence_append_only_guard();

CREATE TRIGGER SampleRetirementRecoveryEvidence_append_only
BEFORE UPDATE OR DELETE ON SampleRetirementRecoveryEvidence
FOR EACH ROW EXECUTE FUNCTION sample_retirement_evidence_append_only_guard();

CREATE TRIGGER SampleRetirementRecoveryEvidence_no_truncate
BEFORE TRUNCATE ON SampleRetirementRecoveryEvidence
FOR EACH STATEMENT
EXECUTE FUNCTION sample_retirement_evidence_append_only_guard();

CREATE TRIGGER SampleRetirementOwnershipEvidence_append_only
BEFORE UPDATE OR DELETE ON SampleRetirementOwnershipEvidence
FOR EACH ROW EXECUTE FUNCTION sample_retirement_evidence_append_only_guard();

CREATE TRIGGER SampleRetirementOwnershipEvidence_no_truncate
BEFORE TRUNCATE ON SampleRetirementOwnershipEvidence
FOR EACH STATEMENT
EXECUTE FUNCTION sample_retirement_evidence_append_only_guard();

CREATE TRIGGER SampleRetirementCheckpoint_append_only
BEFORE UPDATE OR DELETE ON SampleRetirementCheckpoint
FOR EACH ROW EXECUTE FUNCTION sample_retirement_evidence_append_only_guard();

CREATE TRIGGER SampleRetirementCheckpoint_no_truncate
BEFORE TRUNCATE ON SampleRetirementCheckpoint
FOR EACH STATEMENT
EXECUTE FUNCTION sample_retirement_evidence_append_only_guard();

CREATE TRIGGER AuditSubjectTombstone_append_only
BEFORE UPDATE OR DELETE ON AuditSubjectTombstone
FOR EACH ROW EXECUTE FUNCTION sample_retirement_evidence_append_only_guard();

CREATE TRIGGER AuditSubjectTombstone_no_truncate
BEFORE TRUNCATE ON AuditSubjectTombstone
FOR EACH STATEMENT
EXECUTE FUNCTION sample_retirement_evidence_append_only_guard();
