CREATE TABLE ApprovalRequest (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  subject_type TEXT NOT NULL CHECK (subject_type IN ('deployment_plan')),
  subject_id UUID NOT NULL,
  subject_revision BIGINT NOT NULL CHECK (subject_revision > 0),
  subject_checksum TEXT NOT NULL CHECK (
    subject_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  effective_policy_checksum TEXT NOT NULL CHECK (
    effective_policy_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  subscriber_set_checksum TEXT NOT NULL CHECK (
    subscriber_set_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  requester_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > created_at),
  state TEXT NOT NULL DEFAULT 'PENDING' CHECK (
    state IN (
      'PENDING',
      'APPROVED',
      'REJECTED',
      'EXPIRED',
      'SUPERSEDED',
      'INVALIDATED'
    )
  ),
  revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
  invalidation_reason TEXT CHECK (
    invalidation_reason IN (
      'expired',
      'superseded',
      'plan_changed',
      'policy_changed',
      'subscriber_set_changed',
      'campaign_member_unapproved'
    )
  ),
  invalidated_at TIMESTAMPTZ,
  resolved_at TIMESTAMPTZ,
  CONSTRAINT approvalrequest_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT approvalrequest_plan_fk
    FOREIGN KEY (subject_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT approvalrequest_resolution_shape_check CHECK (
    (
      state = 'PENDING'
      AND invalidation_reason IS NULL
      AND invalidated_at IS NULL
      AND resolved_at IS NULL
    )
    OR
    (
      state IN ('APPROVED', 'REJECTED')
      AND invalidation_reason IS NULL
      AND invalidated_at IS NULL
      AND resolved_at IS NOT NULL
    )
    OR
    (
      state IN ('EXPIRED', 'SUPERSEDED', 'INVALIDATED')
      AND invalidation_reason IS NOT NULL
      AND invalidated_at IS NOT NULL
      AND resolved_at IS NULL
    )
  )
);

CREATE UNIQUE INDEX ApprovalRequest_active_subject
  ON ApprovalRequest (organization_id, subject_type, subject_id)
  WHERE state IN ('PENDING', 'APPROVED');

CREATE INDEX ApprovalRequest_pending_work
  ON ApprovalRequest (organization_id, state, created_at DESC, id DESC);

CREATE FUNCTION approval_request_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF current_setting(
      'distr.approval_deletion_reason',
      true
    ) = 'ORGANIZATION_RETENTION' THEN
      RETURN OLD;
    END IF;
    RAISE EXCEPTION 'approval request history is append-only'
      USING ERRCODE = '23514';
  END IF;

  IF NEW.id IS DISTINCT FROM OLD.id
     OR NEW.created_at IS DISTINCT FROM OLD.created_at
     OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR NEW.subject_type IS DISTINCT FROM OLD.subject_type
     OR NEW.subject_id IS DISTINCT FROM OLD.subject_id
     OR NEW.subject_revision IS DISTINCT FROM OLD.subject_revision
     OR NEW.subject_checksum IS DISTINCT FROM OLD.subject_checksum
     OR NEW.effective_policy_checksum IS DISTINCT FROM
       OLD.effective_policy_checksum
     OR NEW.subscriber_set_checksum IS DISTINCT FROM
       OLD.subscriber_set_checksum
     OR NEW.requester_useraccount_id IS DISTINCT FROM
       OLD.requester_useraccount_id
     OR NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
    RAISE EXCEPTION 'approval request binding evidence is immutable'
      USING ERRCODE = '23514';
  END IF;

  IF NEW.revision <> OLD.revision + 1
     OR NEW.updated_at <= OLD.updated_at THEN
    RAISE EXCEPTION 'approval request updates require one optimistic revision'
      USING ERRCODE = '23514';
  END IF;

  IF OLD.state NOT IN ('PENDING', 'APPROVED')
     OR (
       OLD.state = 'APPROVED'
       AND NEW.state NOT IN ('EXPIRED', 'SUPERSEDED', 'INVALIDATED')
     )
     OR (
       OLD.state = 'PENDING'
       AND NEW.state NOT IN (
         'PENDING',
         'APPROVED',
         'REJECTED',
         'EXPIRED',
         'SUPERSEDED',
         'INVALIDATED'
       )
     ) THEN
    RAISE EXCEPTION 'approval request state transition is invalid'
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER ApprovalRequest_guard
BEFORE UPDATE OR DELETE ON ApprovalRequest
FOR EACH ROW EXECUTE FUNCTION approval_request_guard();

CREATE TABLE ApprovalRequirement (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  approval_request_id UUID NOT NULL,
  rule_key TEXT NOT NULL CHECK (
    rule_key ~ '^[a-z0-9]+([._-][a-z0-9]+)*$'
    AND length(rule_key) <= 128
  ),
  policy_version_id UUID NOT NULL,
  authority_kind TEXT NOT NULL CHECK (
    authority_kind IN ('owner', 'subscriber')
  ),
  authority_id UUID NOT NULL,
  principal_group_id UUID NOT NULL,
  quorum INTEGER NOT NULL CHECK (quorum BETWEEN 1 AND 100),
  separation_constraints TEXT[] NOT NULL DEFAULT '{}' CHECK (
    separation_constraints <@ ARRAY[
      'requester_cannot_approve',
      'publisher_cannot_approve',
      'executor_cannot_approve',
      'distinct_approvers'
    ]::TEXT[]
    AND array_position(separation_constraints, NULL) IS NULL
  ),
  sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
  CONSTRAINT approvalrequirement_id_request_organization_unique
    UNIQUE (id, approval_request_id, organization_id),
  CONSTRAINT approvalrequirement_request_fk
    FOREIGN KEY (approval_request_id, organization_id)
    REFERENCES ApprovalRequest(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT approvalrequirement_policy_version_fk
    FOREIGN KEY (policy_version_id, organization_id)
    REFERENCES DeploymentPolicyVersion(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT approvalrequirement_principal_group_fk
    FOREIGN KEY (principal_group_id, organization_id)
    REFERENCES PrincipalGroup(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT approvalrequirement_identity_unique
    UNIQUE (
      approval_request_id,
      policy_version_id,
      authority_kind,
      authority_id,
      rule_key
    ),
  CONSTRAINT approvalrequirement_order_unique
    UNIQUE (approval_request_id, sort_order)
);

CREATE INDEX ApprovalRequirement_request_order
  ON ApprovalRequirement (
    organization_id,
    approval_request_id,
    sort_order,
    id
  );

CREATE FUNCTION approval_requirement_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.approval_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'approval requirements are append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER ApprovalRequirement_append_only
BEFORE UPDATE OR DELETE ON ApprovalRequirement
FOR EACH ROW EXECUTE FUNCTION approval_requirement_append_only();

CREATE TABLE ApprovalDecision (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  approval_request_id UUID NOT NULL,
  approval_requirement_id UUID NOT NULL,
  actor_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  decision TEXT NOT NULL CHECK (decision IN ('APPROVE', 'REJECT')),
  comment TEXT NOT NULL CHECK (
    comment = btrim(comment) AND length(comment) BETWEEN 1 AND 4096
  ),
  request_revision BIGINT NOT NULL CHECK (request_revision > 0),
  idempotency_key TEXT NOT NULL CHECK (
    idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
  ),
  CONSTRAINT approvaldecision_request_fk
    FOREIGN KEY (approval_request_id, organization_id)
    REFERENCES ApprovalRequest(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT approvaldecision_requirement_fk
    FOREIGN KEY (
      approval_requirement_id,
      approval_request_id,
      organization_id
    )
    REFERENCES ApprovalRequirement(
      id,
      approval_request_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE CASCADE
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT approvaldecision_actor_idempotency_unique
    UNIQUE (approval_request_id, actor_useraccount_id, idempotency_key)
);

CREATE INDEX ApprovalDecision_request_order
  ON ApprovalDecision (
    organization_id,
    approval_request_id,
    created_at,
    id
  );

CREATE INDEX ApprovalDecision_requirement_actor
  ON ApprovalDecision (
    organization_id,
    approval_requirement_id,
    actor_useraccount_id
  );

CREATE FUNCTION approval_decision_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.approval_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'approval decisions are append-only'
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER ApprovalDecision_append_only
BEFORE UPDATE OR DELETE ON ApprovalDecision
FOR EACH ROW EXECUTE FUNCTION approval_decision_append_only();
