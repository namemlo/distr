CREATE TABLE RetentionPolicy (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  keep_last_successful_releases INTEGER NOT NULL DEFAULT 3 CHECK (keep_last_successful_releases >= 0),
  failed_task_retention_days INTEGER NOT NULL DEFAULT 30 CHECK (failed_task_retention_days >= 0),
  production_failed_task_retention_days INTEGER NOT NULL DEFAULT 90 CHECK (production_failed_task_retention_days >= 0),
  step_log_retention_days INTEGER NOT NULL DEFAULT 14 CHECK (step_log_retention_days >= 0),
  protect_currently_deployed_releases BOOLEAN NOT NULL DEFAULT true,
  protect_retention_protected_releases BOOLEAN NOT NULL DEFAULT true,
  minimum_audit_retention_days INTEGER NOT NULL DEFAULT 365 CHECK (minimum_audit_retention_days >= 0),
  CONSTRAINT retentionpolicy_organization_name_unique UNIQUE (organization_id, name)
);

ALTER TABLE ReleaseBundle
  ADD COLUMN retention_protected BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE RetentionCleanupJob (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at TIMESTAMP NOT NULL DEFAULT now(),
  updated_at TIMESTAMP NOT NULL DEFAULT now(),
  organization_id UUID NOT NULL REFERENCES Organization(id) ON DELETE CASCADE,
  retention_policy_id UUID NOT NULL REFERENCES RetentionPolicy(id) ON DELETE RESTRICT,
  actor_user_account_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL,
  dry_run BOOLEAN NOT NULL DEFAULT true,
  status TEXT NOT NULL CHECK (status IN ('PREVIEWED', 'REJECTED', 'COMPLETED', 'FAILED')),
  release_candidate_count INTEGER NOT NULL DEFAULT 0 CHECK (release_candidate_count >= 0),
  failed_task_candidate_count INTEGER NOT NULL DEFAULT 0 CHECK (failed_task_candidate_count >= 0),
  step_log_candidate_count INTEGER NOT NULL DEFAULT 0 CHECK (step_log_candidate_count >= 0),
  safety_block_count INTEGER NOT NULL DEFAULT 0 CHECK (safety_block_count >= 0),
  plan JSONB NOT NULL DEFAULT '{}'::jsonb,
  message TEXT NOT NULL DEFAULT ''
);

CREATE INDEX RetentionPolicy_organization_created
  ON RetentionPolicy (organization_id, created_at DESC, id);

CREATE INDEX RetentionCleanupJob_organization_created
  ON RetentionCleanupJob (organization_id, created_at DESC, id);

CREATE INDEX ReleaseBundle_retention_protected
  ON ReleaseBundle (organization_id, retention_protected, id)
  WHERE retention_protected = true;
