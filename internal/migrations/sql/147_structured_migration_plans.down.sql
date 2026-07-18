LOCK TABLE DeploymentPlanMigration, DeploymentPlanStep
  IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM DeploymentPlanMigration)
     OR EXISTS (
       SELECT 1
       FROM DeploymentPlanStep
       WHERE step_input_checksum <> ''
          OR retry_class <> ''
          OR cancellation_behavior <> ''
          OR observation_requirement <> ''
          OR target_lock_key <> ''
          OR database_lock_key <> ''
     ) THEN
    RAISE EXCEPTION
      'refusing migration 147 rollback while structured migration evidence exists';
  END IF;
END;
$$;

DROP TRIGGER IF EXISTS DeploymentPlanMigration_no_truncate
  ON DeploymentPlanMigration;
DROP TRIGGER IF EXISTS DeploymentPlanMigration_append_only
  ON DeploymentPlanMigration;

DROP INDEX IF EXISTS DeploymentPlanMigration_database_resource;
DROP INDEX IF EXISTS DeploymentPlanMigration_plan_order;
DROP TABLE IF EXISTS DeploymentPlanMigration;

ALTER TABLE DeploymentPlanStep
  DROP CONSTRAINT IF EXISTS deploymentplanstep_database_lock_key_check,
  DROP CONSTRAINT IF EXISTS deploymentplanstep_target_lock_key_check,
  DROP CONSTRAINT IF EXISTS deploymentplanstep_observation_requirement_check,
  DROP CONSTRAINT IF EXISTS deploymentplanstep_cancellation_behavior_check,
  DROP CONSTRAINT IF EXISTS deploymentplanstep_retry_class_check,
  DROP CONSTRAINT IF EXISTS deploymentplanstep_input_checksum_check,
  DROP COLUMN IF EXISTS database_lock_key,
  DROP COLUMN IF EXISTS target_lock_key,
  DROP COLUMN IF EXISTS observation_requirement,
  DROP COLUMN IF EXISTS cancellation_behavior,
  DROP COLUMN IF EXISTS retry_class,
  DROP COLUMN IF EXISTS step_input_checksum;
