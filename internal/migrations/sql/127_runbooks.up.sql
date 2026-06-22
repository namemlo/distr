ALTER TABLE Task
  ADD COLUMN task_type TEXT NOT NULL DEFAULT 'deployment'
    CHECK (task_type IN ('deployment', 'runbook'));

CREATE TABLE Runbook (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  application_id UUID NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  CONSTRAINT runbook_organization_application_name_unique UNIQUE (organization_id, application_id, name),
  CONSTRAINT runbook_id_organization_unique UNIQUE (id, organization_id),
  CONSTRAINT runbook_id_application_organization_unique UNIQUE (id, application_id, organization_id),
  CONSTRAINT runbook_application_organization_fk FOREIGN KEY (application_id, organization_id)
    REFERENCES Application(id, organization_id) ON DELETE CASCADE
);

CREATE INDEX Runbook_organization_application_sort_name
  ON Runbook (organization_id, application_id, sort_order, name);

CREATE TABLE RunbookRevision (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  runbook_id UUID NOT NULL,
  revision_number INTEGER NOT NULL CHECK (revision_number > 0),
  description TEXT NOT NULL DEFAULT '',
  CONSTRAINT runbookrevision_runbook_number_unique UNIQUE (runbook_id, revision_number),
  CONSTRAINT runbookrevision_id_runbook_unique UNIQUE (id, runbook_id),
  CONSTRAINT runbookrevision_id_runbook_organization_unique UNIQUE (id, runbook_id, organization_id),
  CONSTRAINT runbookrevision_runbook_organization_fk FOREIGN KEY (runbook_id, organization_id)
    REFERENCES Runbook(id, organization_id) ON DELETE CASCADE
);

CREATE INDEX RunbookRevision_runbook_number
  ON RunbookRevision (runbook_id, revision_number);

CREATE TABLE RunbookStep (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  runbook_revision_id UUID NOT NULL REFERENCES RunbookRevision(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  name TEXT NOT NULL,
  action_type TEXT NOT NULL,
  step_template_version_id UUID,
  execution_location TEXT NOT NULL,
  input_bindings JSONB NOT NULL DEFAULT '{}',
  condition TEXT NOT NULL DEFAULT '',
  failure_mode TEXT NOT NULL DEFAULT 'fail',
  timeout_seconds INTEGER NOT NULL DEFAULT 0 CHECK (timeout_seconds >= 0),
  retry_max_attempts INTEGER NOT NULL DEFAULT 0 CHECK (retry_max_attempts >= 0),
  retry_interval_seconds INTEGER NOT NULL DEFAULT 0 CHECK (retry_interval_seconds >= 0),
  required_permissions TEXT[] NOT NULL DEFAULT '{}',
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  CONSTRAINT runbookstep_revision_key_unique UNIQUE (runbook_revision_id, key),
  CONSTRAINT runbookstep_revision_sort_order_unique UNIQUE (runbook_revision_id, sort_order)
);

CREATE INDEX RunbookStep_revision_sort_key
  ON RunbookStep (runbook_revision_id, sort_order, key);

CREATE TABLE RunbookStepDependency (
  runbook_revision_id UUID NOT NULL,
  step_key TEXT NOT NULL,
  depends_on_step_key TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
  PRIMARY KEY (runbook_revision_id, step_key, depends_on_step_key),
  CONSTRAINT runbookstepdependency_no_self CHECK (step_key <> depends_on_step_key),
  CONSTRAINT runbookstepdependency_step_fk FOREIGN KEY (runbook_revision_id, step_key)
    REFERENCES RunbookStep(runbook_revision_id, key) ON DELETE CASCADE,
  CONSTRAINT runbookstepdependency_depends_on_fk FOREIGN KEY (runbook_revision_id, depends_on_step_key)
    REFERENCES RunbookStep(runbook_revision_id, key) ON DELETE CASCADE
);

CREATE INDEX RunbookStepDependency_revision_step_sort
  ON RunbookStepDependency (runbook_revision_id, step_key, sort_order);

CREATE TABLE RunbookSnapshot (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  published_at TIMESTAMP NOT NULL DEFAULT now(),
  published_by_useraccount_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL,
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  application_id UUID NOT NULL,
  runbook_id UUID NOT NULL,
  runbook_revision_id UUID NOT NULL,
  revision_number INTEGER NOT NULL CHECK (revision_number > 0),
  canonical_checksum TEXT NOT NULL,
  canonical_payload BYTEA NOT NULL,
  CONSTRAINT runbooksnapshot_revision_unique UNIQUE (runbook_revision_id),
  CONSTRAINT runbooksnapshot_id_application_organization_unique UNIQUE (id, application_id, organization_id),
  CONSTRAINT runbooksnapshot_runbook_application_organization_fk
    FOREIGN KEY (runbook_id, application_id, organization_id)
    REFERENCES Runbook(id, application_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT,
  CONSTRAINT runbooksnapshot_revision_runbook_organization_fk
    FOREIGN KEY (runbook_revision_id, runbook_id, organization_id)
    REFERENCES RunbookRevision(id, runbook_id, organization_id)
    ON UPDATE RESTRICT
    ON DELETE RESTRICT
);

CREATE INDEX RunbookSnapshot_organization_application_created
  ON RunbookSnapshot (organization_id, application_id, created_at, id);
