ALTER TABLE Task
  ADD COLUMN actor_user_account_id UUID REFERENCES UserAccount(id) ON DELETE SET NULL;

CREATE INDEX Task_actor_user_account
  ON Task (organization_id, actor_user_account_id)
  WHERE actor_user_account_id IS NOT NULL;
