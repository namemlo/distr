LOCK TABLE
  MaintenanceWindowRule,
  MaintenanceCalendarVersion,
  MaintenanceCalendar,
  DeploymentFreezeRevision,
  DeploymentFreeze
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM MaintenanceCalendar)
     OR EXISTS (SELECT 1 FROM MaintenanceCalendarVersion)
     OR EXISTS (SELECT 1 FROM MaintenanceWindowRule)
     OR EXISTS (SELECT 1 FROM DeploymentFreeze)
     OR EXISTS (SELECT 1 FROM DeploymentFreezeRevision) THEN
    RAISE EXCEPTION
      'downgrade crossing 151 is forbidden while calendar or freeze rows exist';
  END IF;
END;
$$;

DROP TRIGGER DeploymentFreezeRevision_immutable
  ON DeploymentFreezeRevision;

ALTER TABLE DeploymentFreeze
  DROP CONSTRAINT deploymentfreeze_last_published_fk;
DROP TABLE DeploymentFreezeRevision;
DROP TABLE DeploymentFreeze;

DROP TRIGGER MaintenanceWindowRule_immutable
  ON MaintenanceWindowRule;
DROP TRIGGER MaintenanceCalendarVersion_immutable
  ON MaintenanceCalendarVersion;

ALTER TABLE MaintenanceCalendar
  DROP CONSTRAINT maintenancecalendar_last_published_fk;
DROP TABLE MaintenanceWindowRule;
DROP TABLE MaintenanceCalendarVersion;
DROP TABLE MaintenanceCalendar;

DROP FUNCTION maintenance_calendar_published_immutable();
DROP FUNCTION maintenance_calendar_weekdays_valid(INTEGER[]);
