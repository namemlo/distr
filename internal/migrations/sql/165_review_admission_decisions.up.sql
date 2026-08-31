CREATE FUNCTION review_admission_append_only()
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

CREATE TABLE ReviewAdmissionDecision (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  deployment_plan_id UUID NOT NULL,
  plan_revision BIGINT NOT NULL CHECK (plan_revision > 0),
  plan_checksum TEXT NOT NULL CHECK (plan_checksum ~ '^sha256:[0-9a-f]{64}$'),
  review_material_checksum TEXT NOT NULL CHECK (
    review_material_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  observed_state_checksum TEXT NOT NULL CHECK (
    observed_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  decision TEXT NOT NULL CHECK (decision IN ('GO', 'NO_GO')),
  reason TEXT NOT NULL CHECK (
    reason = btrim(reason)
    AND length(reason) BETWEEN 1 AND 4096
    AND reason !~ E'[\r\n]'
  ),
  actor_useraccount_id UUID NOT NULL REFERENCES UserAccount(id) ON DELETE RESTRICT,
  expires_at TIMESTAMPTZ NOT NULL CHECK (
    expires_at > created_at
    AND expires_at <= created_at + INTERVAL '168 hours'
  ),
  supersedes_decision_id UUID,
  revokes_decision_id UUID,
  authorization_evidence TEXT NOT NULL CHECK (
    authorization_evidence ~ '^sha256:[0-9a-f]{64}$'
  ),
  canonical_checksum TEXT NOT NULL CHECK (
    canonical_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  idempotency_key TEXT NOT NULL CHECK (
    idempotency_key = btrim(idempotency_key)
    AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
  ),
  CONSTRAINT reviewadmissiondecision_id_org_unique UNIQUE (id, organization_id),
  CONSTRAINT reviewadmissiondecision_plan_fk
    FOREIGN KEY (deployment_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT reviewadmissiondecision_supersedes_fk
    FOREIGN KEY (supersedes_decision_id, organization_id)
    REFERENCES ReviewAdmissionDecision(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT reviewadmissiondecision_revokes_fk
    FOREIGN KEY (revokes_decision_id, organization_id)
    REFERENCES ReviewAdmissionDecision(id, organization_id)
    ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT reviewadmissiondecision_idempotency_unique
    UNIQUE (organization_id, deployment_plan_id, idempotency_key),
  CONSTRAINT reviewadmissiondecision_chain_shape CHECK (
    supersedes_decision_id IS DISTINCT FROM id
    AND revokes_decision_id IS DISTINCT FROM id
    AND (revokes_decision_id IS NULL OR decision = 'NO_GO')
  )
);

CREATE UNIQUE INDEX ReviewAdmissionDecision_one_superseder
  ON ReviewAdmissionDecision (organization_id, supersedes_decision_id)
  WHERE supersedes_decision_id IS NOT NULL;

CREATE UNIQUE INDEX ReviewAdmissionDecision_one_revoker
  ON ReviewAdmissionDecision (organization_id, revokes_decision_id)
  WHERE revokes_decision_id IS NOT NULL;

CREATE INDEX ReviewAdmissionDecision_plan_tip
  ON ReviewAdmissionDecision (
    organization_id, deployment_plan_id, created_at DESC, id DESC
  );

CREATE TRIGGER ReviewAdmissionDecision_append_only
BEFORE UPDATE OR DELETE ON ReviewAdmissionDecision
FOR EACH ROW EXECUTE FUNCTION review_admission_append_only();

CREATE TRIGGER ReviewAdmissionDecision_no_truncate
BEFORE TRUNCATE ON ReviewAdmissionDecision
FOR EACH STATEMENT EXECUTE FUNCTION review_admission_append_only();
