CREATE FUNCTION maintenance_calendar_weekdays_valid(requested_weekdays INTEGER[])
RETURNS BOOLEAN
LANGUAGE sql
IMMUTABLE
STRICT
AS $$
  SELECT
    cardinality(requested_weekdays) BETWEEN 1 AND 7
    AND requested_weekdays <@ ARRAY[0, 1, 2, 3, 4, 5, 6]
    AND cardinality(requested_weekdays) = (
      SELECT count(DISTINCT weekday)
      FROM unnest(requested_weekdays) AS weekday
    )
    AND requested_weekdays = ARRAY(
      SELECT weekday
      FROM unnest(requested_weekdays) AS weekday
      ORDER BY weekday
    );
$$;

CREATE FUNCTION maintenance_calendar_published_immutable()
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

  RAISE EXCEPTION '% rows are immutable', TG_TABLE_NAME
    USING ERRCODE = '23514';
END;
$$;

CREATE TABLE MaintenanceCalendar (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) BETWEEN 1 AND 200
  ),
  description TEXT NOT NULL DEFAULT '' CHECK (
    length(description) <= 4000
  ),
  draft_iana_zone TEXT NOT NULL CHECK (
    draft_iana_zone = btrim(draft_iana_zone)
    AND length(draft_iana_zone) BETWEEN 1 AND 128
  ),
  draft_rule_version TEXT NOT NULL CHECK (
    draft_rule_version = btrim(draft_rule_version)
    AND length(draft_rule_version) BETWEEN 1 AND 128
  ),
  draft_rules JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (
    jsonb_typeof(draft_rules) = 'array'
    AND pg_column_size(draft_rules) <= 1048576
  ),
  draft_revision BIGINT NOT NULL DEFAULT 1 CHECK (draft_revision > 0),
  last_published_version_id UUID,
  created_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  updated_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  CONSTRAINT maintenancecalendar_id_organization_unique
    UNIQUE (id, organization_id)
);

CREATE INDEX MaintenanceCalendar_page
  ON MaintenanceCalendar (organization_id, created_at DESC, id DESC);

CREATE TABLE MaintenanceCalendarVersion (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  maintenance_calendar_id UUID NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  version_number BIGINT NOT NULL CHECK (version_number > 0),
  source_draft_revision BIGINT NOT NULL CHECK (source_draft_revision > 0),
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) BETWEEN 1 AND 200
  ),
  description TEXT NOT NULL DEFAULT '' CHECK (
    length(description) <= 4000
  ),
  iana_zone TEXT NOT NULL CHECK (
    iana_zone = btrim(iana_zone) AND length(iana_zone) BETWEEN 1 AND 128
  ),
  rule_version TEXT NOT NULL CHECK (
    rule_version = btrim(rule_version)
    AND length(rule_version) BETWEEN 1 AND 128
  ),
  canonical_payload BYTEA NOT NULL CHECK (
    octet_length(canonical_payload) BETWEEN 2 AND 1048576
  ),
  checksum TEXT NOT NULL CHECK (
    checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  CONSTRAINT maintenancecalendarversion_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT maintenancecalendarversion_parent_identity_unique
    UNIQUE (id, organization_id, maintenance_calendar_id),
  CONSTRAINT maintenancecalendarversion_calendar_fk
    FOREIGN KEY (maintenance_calendar_id, organization_id)
    REFERENCES MaintenanceCalendar(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT maintenancecalendarversion_number_unique
    UNIQUE (maintenance_calendar_id, version_number),
  CONSTRAINT maintenancecalendarversion_draft_unique
    UNIQUE (maintenance_calendar_id, source_draft_revision)
);

CREATE INDEX MaintenanceCalendarVersion_page
  ON MaintenanceCalendarVersion (
    organization_id,
    maintenance_calendar_id,
    published_at DESC,
    id DESC
  );

ALTER TABLE MaintenanceCalendar
  ADD CONSTRAINT maintenancecalendar_last_published_fk
  FOREIGN KEY (last_published_version_id, organization_id, id)
  REFERENCES MaintenanceCalendarVersion(
    id,
    organization_id,
    maintenance_calendar_id
  )
  ON UPDATE NO ACTION
  ON DELETE NO ACTION
  DEFERRABLE INITIALLY IMMEDIATE;

CREATE TABLE MaintenanceWindowRule (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  logical_rule_id UUID NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  calendar_version_id UUID NOT NULL,
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) BETWEEN 1 AND 200
  ),
  weekdays INTEGER[] NOT NULL CHECK (
    maintenance_calendar_weekdays_valid(weekdays)
  ),
  start_minute INTEGER NOT NULL CHECK (
    start_minute BETWEEN 0 AND 1439
  ),
  end_minute INTEGER NOT NULL CHECK (
    end_minute BETWEEN 0 AND 1440
  ),
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  CONSTRAINT maintenancewindowrule_interval_check CHECK (
    start_minute <> end_minute
  ),
  CONSTRAINT maintenancewindowrule_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT maintenancewindowrule_name_unique
    UNIQUE (calendar_version_id, name),
  CONSTRAINT maintenancewindowrule_logical_unique
    UNIQUE (calendar_version_id, logical_rule_id),
  CONSTRAINT maintenancewindowrule_version_fk
    FOREIGN KEY (calendar_version_id, organization_id)
    REFERENCES MaintenanceCalendarVersion(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX MaintenanceWindowRule_version_order
  ON MaintenanceWindowRule (
    organization_id,
    calendar_version_id,
    sort_order,
    name,
    id
  );

CREATE TRIGGER MaintenanceCalendarVersion_immutable
BEFORE UPDATE OR DELETE ON MaintenanceCalendarVersion
FOR EACH ROW EXECUTE FUNCTION maintenance_calendar_published_immutable();

CREATE TRIGGER MaintenanceWindowRule_immutable
BEFORE UPDATE OR DELETE ON MaintenanceWindowRule
FOR EACH ROW EXECUTE FUNCTION maintenance_calendar_published_immutable();

CREATE TABLE DeploymentFreeze (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) BETWEEN 1 AND 200
  ),
  draft_start_at TIMESTAMPTZ NOT NULL,
  draft_end_at TIMESTAMPTZ NOT NULL,
  draft_iana_zone TEXT NOT NULL CHECK (
    draft_iana_zone = btrim(draft_iana_zone)
    AND length(draft_iana_zone) BETWEEN 1 AND 128
  ),
  draft_rule_version TEXT NOT NULL CHECK (
    draft_rule_version = btrim(draft_rule_version)
    AND length(draft_rule_version) BETWEEN 1 AND 128
  ),
  draft_scope_kind TEXT NOT NULL CHECK (
    draft_scope_kind IN (
      'organization',
      'customer',
      'environment',
      'deployment_unit',
      'component',
      'campaign'
    )
  ),
  draft_scope_id UUID NOT NULL,
  draft_priority INTEGER NOT NULL DEFAULT 0 CHECK (draft_priority >= 0),
  draft_reason TEXT NOT NULL CHECK (
    draft_reason = btrim(draft_reason)
    AND length(draft_reason) BETWEEN 1 AND 4000
  ),
  draft_revision BIGINT NOT NULL DEFAULT 1 CHECK (draft_revision > 0),
  last_published_revision_id UUID,
  created_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  updated_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  CONSTRAINT deploymentfreeze_interval_check CHECK (
    draft_end_at > draft_start_at
  ),
  CONSTRAINT deploymentfreeze_id_organization_unique
    UNIQUE (id, organization_id)
);

CREATE INDEX DeploymentFreeze_page
  ON DeploymentFreeze (organization_id, created_at DESC, id DESC);

CREATE INDEX DeploymentFreeze_scope
  ON DeploymentFreeze (
    organization_id,
    draft_scope_kind,
    draft_scope_id,
    draft_start_at,
    draft_end_at,
    id
  );

CREATE TABLE DeploymentFreezeRevision (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  deployment_freeze_id UUID NOT NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  version_number BIGINT NOT NULL CHECK (version_number > 0),
  source_draft_revision BIGINT NOT NULL CHECK (source_draft_revision > 0),
  name TEXT NOT NULL CHECK (
    name = btrim(name) AND length(name) BETWEEN 1 AND 200
  ),
  start_at TIMESTAMPTZ NOT NULL,
  end_at TIMESTAMPTZ NOT NULL,
  iana_zone TEXT NOT NULL CHECK (
    iana_zone = btrim(iana_zone) AND length(iana_zone) BETWEEN 1 AND 128
  ),
  rule_version TEXT NOT NULL CHECK (
    rule_version = btrim(rule_version)
    AND length(rule_version) BETWEEN 1 AND 128
  ),
  scope_kind TEXT NOT NULL CHECK (
    scope_kind IN (
      'organization',
      'customer',
      'environment',
      'deployment_unit',
      'component',
      'campaign'
    )
  ),
  scope_id UUID NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0 CHECK (priority >= 0),
  reason TEXT NOT NULL CHECK (
    reason = btrim(reason) AND length(reason) BETWEEN 1 AND 4000
  ),
  canonical_payload BYTEA NOT NULL CHECK (
    octet_length(canonical_payload) BETWEEN 2 AND 1048576
  ),
  checksum TEXT NOT NULL CHECK (
    checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_by_useraccount_id UUID NOT NULL
    REFERENCES UserAccount(id) ON DELETE RESTRICT,
  CONSTRAINT deploymentfreezerevision_interval_check CHECK (
    end_at > start_at
  ),
  CONSTRAINT deploymentfreezerevision_id_organization_unique
    UNIQUE (id, organization_id),
  CONSTRAINT deploymentfreezerevision_parent_identity_unique
    UNIQUE (id, organization_id, deployment_freeze_id),
  CONSTRAINT deploymentfreezerevision_freeze_fk
    FOREIGN KEY (deployment_freeze_id, organization_id)
    REFERENCES DeploymentFreeze(id, organization_id)
    ON UPDATE NO ACTION
    ON DELETE NO ACTION
    DEFERRABLE INITIALLY IMMEDIATE,
  CONSTRAINT deploymentfreezerevision_number_unique
    UNIQUE (deployment_freeze_id, version_number),
  CONSTRAINT deploymentfreezerevision_draft_unique
    UNIQUE (deployment_freeze_id, source_draft_revision)
);

CREATE INDEX DeploymentFreezeRevision_page
  ON DeploymentFreezeRevision (
    organization_id,
    deployment_freeze_id,
    published_at DESC,
    id DESC
  );

CREATE INDEX DeploymentFreezeRevision_active_scope
  ON DeploymentFreezeRevision (
    organization_id,
    scope_kind,
    scope_id,
    start_at,
    end_at,
    priority DESC,
    id
  );

ALTER TABLE DeploymentFreeze
  ADD CONSTRAINT deploymentfreeze_last_published_fk
  FOREIGN KEY (last_published_revision_id, organization_id, id)
  REFERENCES DeploymentFreezeRevision(
    id,
    organization_id,
    deployment_freeze_id
  )
  ON UPDATE NO ACTION
  ON DELETE NO ACTION
  DEFERRABLE INITIALLY IMMEDIATE;

CREATE TRIGGER DeploymentFreezeRevision_immutable
BEFORE UPDATE OR DELETE ON DeploymentFreezeRevision
FOR EACH ROW EXECUTE FUNCTION maintenance_calendar_published_immutable();
