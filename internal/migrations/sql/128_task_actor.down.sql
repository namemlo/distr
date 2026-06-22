DROP INDEX IF EXISTS Task_actor_user_account;

ALTER TABLE Task
  DROP COLUMN actor_user_account_id;
