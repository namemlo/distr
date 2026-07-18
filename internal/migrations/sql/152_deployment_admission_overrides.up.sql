CREATE FUNCTION admission_append_only()
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

  RAISE EXCEPTION '% rows are append-only', TG_TABLE_NAME
    USING ERRCODE = '23514';
END;
$$;

CREATE TABLE EmergencyOverride (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_plan_id UUID NOT NULL,
  plan_revision BIGINT NOT NULL CHECK (plan_revision > 0),
  plan_checksum TEXT NOT NULL CHECK (
    plan_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  effective_policy_checksum TEXT NOT NULL CHECK (
    effective_policy_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  accelerations JSONB NOT NULL CHECK (
    jsonb_typeof(accelerations) = 'array'
    AND jsonb_array_length(accelerations) BETWEEN 1 AND 256
    AND pg_column_size(accelerations) <= 262144
  ),
  reason TEXT NOT NULL CHECK (
    reason = btrim(reason)
    AND length(reason) BETWEEN 1 AND 4096
    AND reason !~ E'[\\r\\n]'
  ),
  actor_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  approval_evidence JSONB NOT NULL CHECK (
    jsonb_typeof(approval_evidence) = 'array'
    AND jsonb_array_length(approval_evidence) BETWEEN 1 AND 256
    AND pg_column_size(approval_evidence) <= 524288
  ),
  expires_at TIMESTAMPTZ NOT NULL CHECK (
    expires_at > created_at
    AND expires_at <= created_at + INTERVAL '24 hours'
  ),
  checksum TEXT NOT NULL CHECK (
    checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  idempotency_key TEXT NOT NULL CHECK (
    idempotency_key = btrim(idempotency_key)
    AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
  ),
  CONSTRAINT emergencyoverride_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT emergencyoverride_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT emergencyoverride_idempotency_unique
    UNIQUE (organization_id, deployment_plan_id, idempotency_key)
);

CREATE INDEX EmergencyOverride_plan_expiry
  ON EmergencyOverride (
    organization_id,
    deployment_plan_id,
    expires_at DESC,
    id DESC
  );

CREATE TRIGGER EmergencyOverride_append_only
BEFORE UPDATE OR DELETE ON EmergencyOverride
FOR EACH ROW EXECUTE FUNCTION admission_append_only();

CREATE TABLE AdmissionEvaluation (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_plan_id UUID NOT NULL,
  plan_revision BIGINT NOT NULL CHECK (plan_revision > 0),
  plan_checksum TEXT NOT NULL CHECK (
    plan_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  plan_schema TEXT NOT NULL CHECK (
    plan_schema = 'distr.target-deployment-plan/v2'
  ),
  protocol_version TEXT NOT NULL CHECK (protocol_version = 'v2'),
  campaign_id UUID,
  campaign_revision BIGINT CHECK (campaign_revision > 0),
  campaign_checksum TEXT NOT NULL DEFAULT '' CHECK (
    campaign_checksum = ''
    OR campaign_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  effective_policy_checksum TEXT NOT NULL CHECK (
    effective_policy_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  policy_version_ids UUID[] NOT NULL CHECK (
    cardinality(policy_version_ids) BETWEEN 1 AND 256
    AND array_position(policy_version_ids, NULL) IS NULL
  ),
  calendar_version_ids UUID[] NOT NULL DEFAULT '{}' CHECK (
    cardinality(calendar_version_ids) <= 256
    AND array_position(calendar_version_ids, NULL) IS NULL
  ),
  freeze_revision_ids UUID[] NOT NULL DEFAULT '{}' CHECK (
    cardinality(freeze_revision_ids) <= 256
    AND array_position(freeze_revision_ids, NULL) IS NULL
  ),
  approval_request_id UUID,
  approval_request_revision BIGINT CHECK (approval_request_revision > 0),
  emergency_override_id UUID,
  emergency_override_checksum TEXT NOT NULL DEFAULT '' CHECK (
    emergency_override_checksum = ''
    OR emergency_override_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  decision TEXT NOT NULL CHECK (decision IN ('ADMIT', 'WAIT', 'BLOCK')),
  reason_codes TEXT[] NOT NULL CHECK (
    cardinality(reason_codes) BETWEEN 1 AND 16
    AND array_position(reason_codes, NULL) IS NULL
    AND reason_codes <@ ARRAY[
      'admitted',
      'maintenance_window_closed',
      'deployment_freeze_active',
      'approval_missing',
      'approval_invalid',
      'mandatory_gate_failed',
      'emergency_acceleration'
    ]::TEXT[]
  ),
  evaluated_at TIMESTAMPTZ NOT NULL,
  temporal_evidence JSONB NOT NULL CHECK (
    jsonb_typeof(temporal_evidence) = 'object'
    AND pg_column_size(temporal_evidence) <= 1048576
  ),
  gate_evidence JSONB NOT NULL CHECK (
    jsonb_typeof(gate_evidence) = 'array'
    AND jsonb_array_length(gate_evidence) <= 256
    AND pg_column_size(gate_evidence) <= 524288
  ),
  material_checksum TEXT NOT NULL CHECK (
    material_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  decision_checksum TEXT NOT NULL CHECK (
    decision_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  scheduler_idempotency_key TEXT NOT NULL CHECK (
    scheduler_idempotency_key = btrim(scheduler_idempotency_key)
    AND scheduler_idempotency_key ~
      '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
  ),
  actor_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  CONSTRAINT admissionevaluation_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT admissionevaluation_approval_fk
    FOREIGN KEY (approval_request_id, organization_id)
    REFERENCES ApprovalRequest(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT admissionevaluation_override_fk
    FOREIGN KEY (emergency_override_id, organization_id)
    REFERENCES EmergencyOverride(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT admissionevaluation_campaign_shape_check CHECK (
    (
      campaign_id IS NULL
      AND campaign_revision IS NULL
      AND campaign_checksum = ''
    )
    OR (
      campaign_id IS NOT NULL
      AND campaign_revision IS NOT NULL
      AND campaign_checksum ~ '^sha256:[0-9a-f]{64}$'
    )
  ),
  CONSTRAINT admissionevaluation_approval_shape_check CHECK (
    (approval_request_id IS NULL AND approval_request_revision IS NULL)
    OR (
      approval_request_id IS NOT NULL
      AND approval_request_revision IS NOT NULL
    )
  ),
  CONSTRAINT admissionevaluation_override_shape_check CHECK (
    (emergency_override_id IS NULL AND emergency_override_checksum = '')
    OR (
      emergency_override_id IS NOT NULL
      AND emergency_override_checksum ~ '^sha256:[0-9a-f]{64}$'
    )
  )
);

CREATE UNIQUE INDEX AdmissionEvaluation_scheduler_idempotency
  ON AdmissionEvaluation (
    organization_id,
    deployment_plan_id,
    scheduler_idempotency_key
  );

CREATE INDEX AdmissionEvaluation_plan_history
  ON AdmissionEvaluation (
    organization_id,
    deployment_plan_id,
    created_at DESC,
    id DESC
  );

CREATE INDEX AdmissionEvaluation_admitted_material
  ON AdmissionEvaluation (
    organization_id,
    deployment_plan_id,
    material_checksum,
    created_at DESC
  )
  WHERE decision = 'ADMIT';

CREATE TRIGGER AdmissionEvaluation_append_only
BEFORE UPDATE OR DELETE ON AdmissionEvaluation
FOR EACH ROW EXECUTE FUNCTION admission_append_only();
