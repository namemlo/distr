CREATE FUNCTION deploymentcampaign_published_immutable()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE'
     AND current_setting(
       'distr.deployment_campaign_deletion_reason',
       true
     ) = 'ORGANIZATION_RETENTION'
     AND current_setting(
       'distr.deployment_campaign_deletion_operation_id',
       true
     ) ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
     AND pg_trigger_depth() > 1
     AND NOT EXISTS (
       SELECT 1
       FROM Organization
       WHERE id = OLD.organization_id
     ) THEN
    RETURN OLD;
  END IF;

  RAISE EXCEPTION '% rows are immutable', TG_TABLE_NAME
    USING ERRCODE = '23514';
END;
$$;

CREATE FUNCTION deploymentcampaign_published_no_truncate()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION '% rows cannot be truncated', TG_TABLE_NAME
    USING ERRCODE = '23514';
END;
$$;

ALTER TABLE AdmissionEvaluation
  ADD CONSTRAINT admissionevaluation_id_plan_organization_unique
  UNIQUE (id, deployment_plan_id, organization_id);

ALTER TABLE ApprovalRequest
  ADD CONSTRAINT approvalrequest_id_plan_organization_unique
  UNIQUE (id, subject_id, organization_id);

ALTER TABLE DeploymentPlan
  ADD CONSTRAINT deploymentplan_id_unit_organization_unique
  UNIQUE (id, deployment_unit_id, organization_id);

ALTER TABLE DeploymentPlanTargetComponent
  ADD CONSTRAINT deploymentplantargetcomponent_id_plan_organization_unique
  UNIQUE (id, deployment_plan_id, organization_id);

CREATE TABLE DeploymentCampaignDraft (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) BETWEEN 1 AND 200
  ),
  description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4000),
  membership JSONB NOT NULL CHECK (
    jsonb_typeof(membership) = 'object'
    AND pg_column_size(membership) <= 1048576
  ),
  waves JSONB NOT NULL CHECK (
    jsonb_typeof(waves) = 'array'
    AND jsonb_array_length(waves) BETWEEN 1 AND 100
    AND pg_column_size(waves) <= 1048576
  ),
  prerequisites JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (
    jsonb_typeof(prerequisites) = 'array'
    AND jsonb_array_length(prerequisites) <= 5000
    AND pg_column_size(prerequisites) <= 1048576
  ),
  risk_policy JSONB NOT NULL CHECK (
    jsonb_typeof(risk_policy) = 'object'
    AND pg_column_size(risk_policy) <= 65536
    AND jsonb_typeof(risk_policy -> 'maximumConcurrency') = 'number'
    AND (risk_policy ->> 'maximumConcurrency') ~ '^[0-9]+$'
    AND (risk_policy ->> 'maximumConcurrency')::integer BETWEEN 1 AND 1000
  ),
  revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
  last_published_revision_id UUID,
  created_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  updated_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  CONSTRAINT deploymentcampaigndraft_id_organization_unique
    UNIQUE (id, organization_id)
);

CREATE INDEX DeploymentCampaignDraft_page
  ON DeploymentCampaignDraft (organization_id, created_at DESC, id DESC);

CREATE TABLE DeploymentCampaignRevision (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  deployment_campaign_draft_id UUID NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  revision_number BIGINT NOT NULL CHECK (revision_number > 0),
  source_draft_revision BIGINT NOT NULL CHECK (source_draft_revision > 0),
  publication_key TEXT NOT NULL CHECK (
    publication_key = btrim(publication_key)
    AND length(publication_key) BETWEEN 1 AND 128
  ),
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) BETWEEN 1 AND 200
  ),
  description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4000),
  membership_tag_query TEXT NOT NULL DEFAULT '' CHECK (
    length(membership_tag_query) <= 1000
  ),
  risk_policy JSONB NOT NULL CHECK (
    jsonb_typeof(risk_policy) = 'object'
    AND pg_column_size(risk_policy) <= 65536
    AND jsonb_typeof(risk_policy -> 'maximumConcurrency') = 'number'
    AND (risk_policy ->> 'maximumConcurrency') ~ '^[0-9]+$'
    AND (risk_policy ->> 'maximumConcurrency')::integer BETWEEN 1 AND 1000
  ),
  canonical_payload BYTEA NOT NULL CHECK (
    octet_length(canonical_payload) BETWEEN 2 AND 1048576
  ),
  canonical_checksum TEXT NOT NULL CHECK (
    canonical_checksum ~ '^sha256:[0-9a-f]{64}$'
    AND canonical_checksum =
      'sha256:' || encode(sha256(canonical_payload), 'hex')
  ),
  published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  CONSTRAINT deploymentcampaignrevision_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT deploymentcampaignrevision_parent_identity_unique
    UNIQUE (id, organization_id, deployment_campaign_draft_id),
  CONSTRAINT deploymentcampaignrevision_draft_fk
    FOREIGN KEY (deployment_campaign_draft_id, organization_id)
    REFERENCES DeploymentCampaignDraft(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignrevision_number_unique
    UNIQUE (deployment_campaign_draft_id, revision_number),
  CONSTRAINT deploymentcampaignrevision_source_unique
    UNIQUE (deployment_campaign_draft_id, source_draft_revision),
  CONSTRAINT deploymentcampaignrevision_publication_key_unique
    UNIQUE (deployment_campaign_draft_id, publication_key)
);

CREATE INDEX DeploymentCampaignRevision_page
  ON DeploymentCampaignRevision (
    organization_id,
    deployment_campaign_draft_id,
    published_at DESC,
    id DESC
  );

ALTER TABLE DeploymentCampaignDraft
  ADD CONSTRAINT deploymentcampaigndraft_last_published_fk
  FOREIGN KEY (last_published_revision_id, organization_id, id)
  REFERENCES DeploymentCampaignRevision(
    id,
    organization_id,
    deployment_campaign_draft_id
  )
  ON UPDATE NO ACTION
  ON DELETE NO ACTION
  DEFERRABLE INITIALLY IMMEDIATE;

CREATE TABLE DeploymentCampaignWave (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  campaign_revision_id UUID NOT NULL,
  wave_order INTEGER NOT NULL CHECK (wave_order > 0),
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) BETWEEN 1 AND 200
  ),
  bake_seconds INTEGER NOT NULL CHECK (
    bake_seconds BETWEEN 0 AND 31536000
  ),
  maximum_concurrency INTEGER NOT NULL CHECK (
    maximum_concurrency BETWEEN 1 AND 1000
  ),
  CONSTRAINT deploymentcampaignwave_revision_fk
    FOREIGN KEY (campaign_revision_id, organization_id)
    REFERENCES DeploymentCampaignRevision(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignwave_order_unique
    UNIQUE (campaign_revision_id, wave_order, organization_id)
);

CREATE TABLE DeploymentCampaignMember (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  campaign_revision_id UUID NOT NULL,
  deployment_plan_id UUID NOT NULL,
  deployment_unit_id UUID NOT NULL,
  plan_checksum TEXT NOT NULL CHECK (
    plan_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  effective_policy_checksum TEXT NOT NULL CHECK (
    effective_policy_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  approval_request_id UUID NOT NULL,
  approval_request_revision BIGINT NOT NULL CHECK (
    approval_request_revision > 0
  ),
  approval_checksum TEXT NOT NULL CHECK (
    approval_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  calendar_version_ids UUID[] NOT NULL DEFAULT '{}' CHECK (
    cardinality(calendar_version_ids) <= 256
    AND array_position(calendar_version_ids, NULL) IS NULL
  ),
  calendar_checksums TEXT[] NOT NULL DEFAULT '{}' CHECK (
    cardinality(calendar_checksums) = cardinality(calendar_version_ids)
    AND array_position(calendar_checksums, NULL) IS NULL
    AND (
      cardinality(calendar_checksums) = 0
      OR array_to_string(calendar_checksums, ',') ~
        '^sha256:[0-9a-f]{64}(,sha256:[0-9a-f]{64})*$'
    )
  ),
  admission_evaluation_id UUID NOT NULL,
  admission_checksum TEXT NOT NULL CHECK (
    admission_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  wave_order INTEGER NOT NULL CHECK (wave_order > 0),
  member_order INTEGER NOT NULL CHECK (member_order > 0),
  CONSTRAINT deploymentcampaignmember_revision_fk
    FOREIGN KEY (campaign_revision_id, organization_id)
    REFERENCES DeploymentCampaignRevision(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignmember_plan_fk
    FOREIGN KEY (
      deployment_plan_id,
      deployment_unit_id,
      organization_id
    )
    REFERENCES DeploymentPlan(id, deployment_unit_id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignmember_approval_fk
    FOREIGN KEY (
      approval_request_id,
      deployment_plan_id,
      organization_id
    )
    REFERENCES ApprovalRequest(id, subject_id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignmember_admission_fk
    FOREIGN KEY (
      admission_evaluation_id,
      deployment_plan_id,
      organization_id
    )
    REFERENCES AdmissionEvaluation(id, deployment_plan_id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignmember_wave_fk
    FOREIGN KEY (campaign_revision_id, wave_order, organization_id)
    REFERENCES DeploymentCampaignWave(
      campaign_revision_id,
      wave_order,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignmember_plan_unique
    UNIQUE (campaign_revision_id, deployment_plan_id, organization_id),
  CONSTRAINT deploymentcampaignmember_unit_unique
    UNIQUE (campaign_revision_id, deployment_unit_id),
  CONSTRAINT deploymentcampaignmember_order_unique
    UNIQUE (campaign_revision_id, wave_order, member_order)
);

CREATE TABLE DeploymentCampaignPrerequisite (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  campaign_revision_id UUID NOT NULL,
  downstream_plan_id UUID NOT NULL,
  upstream_plan_id UUID NOT NULL,
  upstream_step_key TEXT NOT NULL CHECK (
    upstream_step_key = btrim(upstream_step_key)
    AND length(upstream_step_key) BETWEEN 1 AND 200
  ),
  provider_placement_id UUID NOT NULL,
  provider_deployment_unit_id UUID NOT NULL,
  provider_component_instance_id UUID NOT NULL,
  expected_runtime_state_checksum TEXT NOT NULL CHECK (
    expected_runtime_state_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  CONSTRAINT deploymentcampaignprerequisite_distinct_plans
    CHECK (downstream_plan_id <> upstream_plan_id),
  CONSTRAINT deploymentcampaignprerequisite_revision_fk
    FOREIGN KEY (campaign_revision_id, organization_id)
    REFERENCES DeploymentCampaignRevision(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignprerequisite_downstream_fk
    FOREIGN KEY (
      campaign_revision_id,
      downstream_plan_id,
      organization_id
    )
    REFERENCES DeploymentCampaignMember(
      campaign_revision_id,
      deployment_plan_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignprerequisite_upstream_fk
    FOREIGN KEY (
      campaign_revision_id,
      upstream_plan_id,
      organization_id
    )
    REFERENCES DeploymentCampaignMember(
      campaign_revision_id,
      deployment_plan_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignprerequisite_downstream_plan_fk
    FOREIGN KEY (downstream_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignprerequisite_upstream_plan_fk
    FOREIGN KEY (upstream_plan_id, organization_id)
    REFERENCES DeploymentPlan(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignprerequisite_upstream_step_fk
    FOREIGN KEY (
      upstream_plan_id,
      upstream_step_key,
      organization_id
    )
    REFERENCES DeploymentPlanStep(
      deployment_plan_id,
      step_key,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignprerequisite_provider_placement_fk
    FOREIGN KEY (
      provider_placement_id,
      upstream_plan_id,
      organization_id
    )
    REFERENCES DeploymentPlanTargetComponent(
      id,
      deployment_plan_id,
      organization_id
    )
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignprerequisite_provider_unit_fk
    FOREIGN KEY (provider_deployment_unit_id, organization_id)
    REFERENCES DeploymentUnit(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignprerequisite_provider_instance_fk
    FOREIGN KEY (
      provider_component_instance_id,
      provider_deployment_unit_id,
      organization_id
    )
    REFERENCES ComponentInstance(id, deployment_unit_id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentcampaignprerequisite_identity_unique
    UNIQUE (
      campaign_revision_id,
      downstream_plan_id,
      upstream_plan_id,
      upstream_step_key,
      provider_placement_id
    )
);

CREATE INDEX DeploymentCampaignMember_execution_order
  ON DeploymentCampaignMember (
    organization_id,
    campaign_revision_id,
    wave_order,
    member_order,
    deployment_plan_id
  );

CREATE INDEX DeploymentCampaignPrerequisite_downstream
  ON DeploymentCampaignPrerequisite (
    organization_id,
    campaign_revision_id,
    downstream_plan_id,
    upstream_plan_id,
    upstream_step_key
  );

CREATE TRIGGER DeploymentCampaignRevision_immutable
BEFORE UPDATE OR DELETE ON DeploymentCampaignRevision
FOR EACH ROW EXECUTE FUNCTION deploymentcampaign_published_immutable();

CREATE TRIGGER DeploymentCampaignWave_immutable
BEFORE UPDATE OR DELETE ON DeploymentCampaignWave
FOR EACH ROW EXECUTE FUNCTION deploymentcampaign_published_immutable();

CREATE TRIGGER DeploymentCampaignMember_immutable
BEFORE UPDATE OR DELETE ON DeploymentCampaignMember
FOR EACH ROW EXECUTE FUNCTION deploymentcampaign_published_immutable();

CREATE TRIGGER DeploymentCampaignPrerequisite_immutable
BEFORE UPDATE OR DELETE ON DeploymentCampaignPrerequisite
FOR EACH ROW EXECUTE FUNCTION deploymentcampaign_published_immutable();

CREATE TRIGGER DeploymentCampaignRevision_no_truncate
BEFORE TRUNCATE ON DeploymentCampaignRevision
FOR EACH STATEMENT EXECUTE FUNCTION deploymentcampaign_published_no_truncate();

CREATE TRIGGER DeploymentCampaignWave_no_truncate
BEFORE TRUNCATE ON DeploymentCampaignWave
FOR EACH STATEMENT EXECUTE FUNCTION deploymentcampaign_published_no_truncate();

CREATE TRIGGER DeploymentCampaignMember_no_truncate
BEFORE TRUNCATE ON DeploymentCampaignMember
FOR EACH STATEMENT EXECUTE FUNCTION deploymentcampaign_published_no_truncate();

CREATE TRIGGER DeploymentCampaignPrerequisite_no_truncate
BEFORE TRUNCATE ON DeploymentCampaignPrerequisite
FOR EACH STATEMENT EXECUTE FUNCTION deploymentcampaign_published_no_truncate();
