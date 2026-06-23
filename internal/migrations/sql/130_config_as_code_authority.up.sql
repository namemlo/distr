CREATE TABLE ConfigAsCodeAuthority (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  resource_kind TEXT NOT NULL CHECK (
    resource_kind IN (
      'DeploymentProcess',
      'Channel',
      'Lifecycle',
      'VariableSetDefinition',
      'StepTemplateReference',
      'Runbook'
    )
  ),
  resource_id UUID NOT NULL,
  authority TEXT NOT NULL CHECK (authority IN ('DATABASE_MANAGED', 'GIT_MANAGED')),
  repository_path TEXT NOT NULL DEFAULT '',
  source_revision TEXT NOT NULL DEFAULT '',
  document_checksum TEXT NOT NULL DEFAULT '',
  updated_by_useraccount_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL,
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  CONSTRAINT configascodeauthority_resource_unique UNIQUE (organization_id, resource_kind, resource_id),
  CONSTRAINT configascodeauthority_repository_path_relative CHECK (
    repository_path = ''
    OR (
      repository_path NOT LIKE '/%'
      AND repository_path !~ '^[A-Za-z]:'
      AND position(chr(92) in repository_path) = 0
      AND repository_path !~ '(^|/)\.\.(/|$)'
    )
  ),
  CONSTRAINT configascodeauthority_git_metadata CHECK (
    authority = 'DATABASE_MANAGED'
    OR (
      length(trim(repository_path)) > 0
      AND length(trim(document_checksum)) > 0
    )
  )
);

CREATE INDEX ConfigAsCodeAuthority_organization_kind
  ON ConfigAsCodeAuthority (organization_id, resource_kind, resource_id);

CREATE TABLE ConfigAsCodeAuthorityAuditEvent (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  resource_kind TEXT NOT NULL CHECK (
    resource_kind IN (
      'DeploymentProcess',
      'Channel',
      'Lifecycle',
      'VariableSetDefinition',
      'StepTemplateReference',
      'Runbook'
    )
  ),
  resource_id UUID NOT NULL,
  previous_authority TEXT NOT NULL CHECK (previous_authority IN ('DATABASE_MANAGED', 'GIT_MANAGED')),
  new_authority TEXT NOT NULL CHECK (new_authority IN ('DATABASE_MANAGED', 'GIT_MANAGED')),
  repository_path TEXT NOT NULL DEFAULT '',
  source_revision TEXT NOT NULL DEFAULT '',
  document_checksum TEXT NOT NULL DEFAULT '',
  actor_useraccount_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL
);

CREATE INDEX ConfigAsCodeAuthorityAuditEvent_organization_resource_created
  ON ConfigAsCodeAuthorityAuditEvent (organization_id, resource_kind, resource_id, created_at, id);
