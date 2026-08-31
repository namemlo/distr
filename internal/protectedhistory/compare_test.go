package protectedhistory

import (
	"encoding/json"
	"testing"
)

func TestCompareClassifiesExactAdditions(t *testing.T) {
	t.Parallel()
	baseline := mustBuild(t, []RawRecord{{
		Kind: "deploymentplan", ID: testPlanID, Payload: json.RawMessage(`{"status":"VALID"}`),
	}})
	current := mustBuild(t, []RawRecord{
		{Kind: "deploymentplan", ID: testPlanID, Payload: json.RawMessage(`{"status":"VALID"}`)},
		{Kind: "task", ID: testTaskID, Payload: json.RawMessage(`{"status":"SUCCEEDED"}`)},
	})
	result, err := Compare(*baseline, *current)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ComparisonAdditionsOnly || len(result.Additions) != 1 || len(result.Violations) != 0 {
		t.Fatalf("unexpected comparison: %#v", result)
	}
	if result.Additions[0].Kind != "task" || result.Additions[0].ID != testTaskID {
		t.Fatalf("addition was not classified exactly: %#v", result.Additions)
	}
}

func TestCompareFailsClosedForMissingAndModifiedBaselineRecords(t *testing.T) {
	t.Parallel()
	baseline := mustBuild(t, []RawRecord{
		{Kind: "deploymentplan", ID: testPlanID, Payload: json.RawMessage(`{"status":"VALID"}`)},
		{Kind: "task", ID: testTaskID, Payload: json.RawMessage(`{"status":"SUCCEEDED"}`)},
	})
	current := mustBuild(t, []RawRecord{{
		Kind: "deploymentplan", ID: testPlanID, Payload: json.RawMessage(`{"status":"INVALID"}`),
	}})
	result, err := Compare(*baseline, *current)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ComparisonViolation || len(result.Violations) != 2 {
		t.Fatalf("unexpected comparison: %#v", result)
	}
	seen := map[DifferenceType]bool{}
	for _, violation := range result.Violations {
		seen[violation.Type] = true
	}
	if !seen[DifferenceMissing] || !seen[DifferenceModified] {
		t.Fatalf("expected missing and modified violations: %#v", result.Violations)
	}
}

func TestCompareFailsClosedForScopeMismatchAndSchemaRegression(t *testing.T) {
	t.Parallel()
	baseline := mustBuild(t, nil)
	differentScope, err := Build(Scope{
		OrganizationID:      testOrganizationID,
		DeploymentTargetIDs: []string{"66666666-6666-4666-8666-666666666666"},
	}, 138, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Compare(*baseline, *differentScope)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ComparisonViolation || result.Violations[0].Type != DifferenceScopeMismatch {
		t.Fatalf("scope mismatch did not fail closed: %#v", result)
	}

	newer, err := Build(testScope(), 139, nil)
	if err != nil {
		t.Fatal(err)
	}
	older := mustBuild(t, nil)
	result, err = Compare(*newer, *older)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ComparisonViolation || result.Violations[0].Type != DifferenceSourceSchemaRegression {
		t.Fatalf("schema regression did not fail closed: %#v", result)
	}
}
