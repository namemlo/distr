package protectedhistory

import (
	"encoding/json"
	"strings"
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

func TestCompareExactRequiresArtifactEquality(t *testing.T) {
	t.Parallel()
	baseline := mustBuild(t, []RawRecord{{
		Kind: "deploymentplan", ID: testPlanID, Payload: json.RawMessage(`{"status":"VALID"}`),
	}})
	withAddition := mustBuild(t, []RawRecord{
		{Kind: "deploymentplan", ID: testPlanID, Payload: json.RawMessage(`{"status":"VALID"}`)},
		{Kind: "task", ID: testTaskID, Payload: json.RawMessage(`{"status":"SUCCEEDED"}`)},
	})
	result, err := CompareExact(*baseline, *withAddition, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ComparisonViolation || len(result.Violations) != 1 ||
		result.Violations[0].Type != DifferenceAdded {
		t.Fatalf("exact comparison accepted an addition: %#v", result)
	}

	result, err = CompareExact(*baseline, *baseline, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ComparisonUnchanged || result.BaselineArtifactID != result.CurrentArtifactID {
		t.Fatalf("equal artifacts did not compare unchanged: %#v", result)
	}

	newerSchema, err := Build(testScope(), 166, []RawRecord{{
		Kind: "deploymentplan", ID: testPlanID, Payload: json.RawMessage(`{"status":"VALID"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err = CompareExact(*baseline, *newerSchema, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ComparisonViolation || len(result.Violations) != 1 ||
		result.Violations[0].Type != DifferenceSourceSchemaMismatch {
		t.Fatalf("exact comparison accepted a schema change: %#v", result)
	}
}

func TestCompareExactPermitsOnlyEveryApprovedRetirement(t *testing.T) {
	t.Parallel()
	baseline := mustBuild(t, []RawRecord{
		{Kind: "deploymentplan", ID: testPlanID, Payload: json.RawMessage(`{"status":"VALID"}`)},
		{Kind: "task", ID: testTaskID, Payload: json.RawMessage(`{"status":"SUCCEEDED"}`)},
	})
	current := mustBuild(t, []RawRecord{{
		Kind: "deploymentplan", ID: testPlanID, Payload: json.RawMessage(`{"status":"VALID"}`),
	}})
	allowlist, err := BuildApprovedRetirementAllowlist(
		*baseline,
		"sha256:"+strings.Repeat("a", 64),
		"77777777-7777-4777-8777-777777777777",
		"sha256:"+strings.Repeat("b", 64),
		[]RetirementAllowance{{
			Kind: "task", ID: testTaskID, BaselineHash: baseline.Records[1].Hash,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompareExact(*baseline, *current, allowlist)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ComparisonRetirements || len(result.ApprovedRetirements) != 1 ||
		result.RetirementAllowlistID != allowlist.AllowlistID {
		t.Fatalf("approved retirement was not classified exactly: %#v", result)
	}

	tampered := *allowlist
	tampered.Items = append([]RetirementAllowance(nil), allowlist.Items...)
	tampered.Items[0].BaselineHash = "sha256:" + strings.Repeat("c", 64)
	if _, err := CompareExact(*baseline, *current, &tampered); err == nil {
		t.Fatal("tampered retirement allowlist was accepted")
	}
}

func TestCompareExactRejectsUnusedRetirementAllowance(t *testing.T) {
	t.Parallel()
	baseline := mustBuild(t, []RawRecord{{
		Kind: "task", ID: testTaskID, Payload: json.RawMessage(`{"status":"SUCCEEDED"}`),
	}})
	allowlist, err := BuildApprovedRetirementAllowlist(
		*baseline,
		"sha256:"+strings.Repeat("a", 64),
		"77777777-7777-4777-8777-777777777777",
		"sha256:"+strings.Repeat("b", 64),
		[]RetirementAllowance{{
			Kind: "task", ID: testTaskID, BaselineHash: baseline.Records[0].Hash,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompareExact(*baseline, *baseline, allowlist)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ComparisonViolation || len(result.Violations) != 1 ||
		result.Violations[0].Type != DifferenceUnusedRetirement {
		t.Fatalf("unused allowance did not fail closed: %#v", result)
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
