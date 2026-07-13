DROP INDEX IF EXISTS TaskLease_executor_active;

ALTER TABLE TaskLease
  DROP COLUMN executor_type;
