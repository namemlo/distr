LOCK TABLE
  DeploymentPlan,
  DeploymentPolicyVersion,
  ApprovalDecision,
  ApprovalRequirement,
  ApprovalRequest
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM ApprovalDecision)
     OR EXISTS (SELECT 1 FROM ApprovalRequirement)
     OR EXISTS (SELECT 1 FROM ApprovalRequest) THEN
    RAISE EXCEPTION
      'downgrade crossing 150 is forbidden while approval evidence exists';
  END IF;
END;
$$;

DROP TRIGGER ApprovalDecision_append_only ON ApprovalDecision;
DROP FUNCTION approval_decision_append_only();
DROP TABLE ApprovalDecision;

DROP TRIGGER ApprovalRequirement_append_only ON ApprovalRequirement;
DROP FUNCTION approval_requirement_append_only();
DROP TABLE ApprovalRequirement;

DROP TRIGGER ApprovalRequest_guard ON ApprovalRequest;
DROP FUNCTION approval_request_guard();
DROP TABLE ApprovalRequest;
