ALTER TABLE ExternalExecution
  ADD COLUMN expected_compose_reference TEXT NOT NULL DEFAULT '',
  ADD COLUMN expected_compose_checksum TEXT NOT NULL DEFAULT '' CHECK (
    expected_compose_checksum = '' OR expected_compose_checksum ~ '^sha256:[0-9a-f]{64}$'
  ),
  ADD CONSTRAINT externalexecution_compose_identity_complete CHECK (
    (expected_compose_reference = '') = (expected_compose_checksum = '')
  );
