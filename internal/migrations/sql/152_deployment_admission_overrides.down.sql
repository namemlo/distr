LOCK TABLE
  AdmissionEvaluation,
  EmergencyOverride
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM AdmissionEvaluation)
     OR EXISTS (SELECT 1 FROM EmergencyOverride) THEN
    RAISE EXCEPTION
      'downgrade crossing 152 is forbidden while admission or override rows exist';
  END IF;
END;
$$;

DROP TRIGGER AdmissionEvaluation_append_only
  ON AdmissionEvaluation;
DROP TABLE AdmissionEvaluation;

DROP TRIGGER EmergencyOverride_append_only
  ON EmergencyOverride;
DROP TABLE EmergencyOverride;

DROP FUNCTION admission_append_only();
