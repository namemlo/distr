LOCK TABLE ReviewAdmissionDecision IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM ReviewAdmissionDecision) THEN
    RAISE EXCEPTION
      'refusing migration 165 rollback: review admission evidence exists';
  END IF;
END
$$;

DROP TRIGGER ReviewAdmissionDecision_no_truncate ON ReviewAdmissionDecision;
DROP TRIGGER ReviewAdmissionDecision_append_only ON ReviewAdmissionDecision;
DROP TABLE ReviewAdmissionDecision;
DROP FUNCTION review_admission_append_only();
