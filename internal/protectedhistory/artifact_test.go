package protectedhistory

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	testOrganizationID         = "11111111-1111-4111-8111-111111111111"
	testCustomerOrganizationID = "22222222-2222-4222-8222-222222222222"
	testDeploymentTargetID     = "33333333-3333-4333-8333-333333333333"
	testPlanID                 = "44444444-4444-4444-8444-444444444444"
	testTaskID                 = "55555555-5555-4555-8555-555555555555"
)

func TestBuildIsDeterministicAndSealsScope(t *testing.T) {
	t.Parallel()
	scope := Scope{
		OrganizationID:          strings.ToUpper(testOrganizationID),
		CustomerOrganizationIDs: []string{testCustomerOrganizationID},
		DeploymentTargetIDs:     []string{testDeploymentTargetID},
	}
	left, err := Build(scope, 138, []RawRecord{
		{Kind: "task", ID: testTaskID, Payload: json.RawMessage(`{"status":"SUCCEEDED","order":2}`)},
		{Kind: "deploymentplan", ID: testPlanID, Payload: json.RawMessage(`{"b":2,"a":1}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	right, err := Build(scope, 138, []RawRecord{
		{Kind: "DeploymentPlan", ID: strings.ToUpper(testPlanID), Payload: json.RawMessage(`{ "a": 1, "b": 2 }`)},
		{Kind: "task", ID: testTaskID, Payload: json.RawMessage(`{"order":2,"status":"SUCCEEDED"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if left.ArtifactID != right.ArtifactID || left.RecordsRoot != right.RecordsRoot {
		t.Fatalf("equivalent input produced different identities: %#v %#v", left, right)
	}
	if left.Records[0].Kind != "deploymentplan" || left.Records[0].ID != testPlanID {
		t.Fatalf("records were not canonically ordered: %#v", left.Records)
	}
	if string(left.Records[0].Payload) != `{"a":1,"b":2}` {
		t.Fatalf("payload was not canonicalized: %s", left.Records[0].Payload)
	}
	if left.Scope.OrganizationID != testOrganizationID {
		t.Fatalf("scope was not canonicalized: %#v", left.Scope)
	}
	if left.ArtifactID == "" || left.RecordsRoot == "" {
		t.Fatal("artifact was not sealed")
	}
	if err := Validate(*left); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRejectsDuplicateAndUnknownLogicalRecords(t *testing.T) {
	t.Parallel()
	scope := testScope()
	_, err := Build(scope, 138, []RawRecord{
		{Kind: "task", ID: testTaskID, Payload: json.RawMessage(`{"status":"SUCCEEDED"}`)},
		{Kind: "task", ID: testTaskID, Payload: json.RawMessage(`{"status":"SUCCEEDED"}`)},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate protected record") {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
	_, err = Build(scope, 138, []RawRecord{
		{Kind: "new-unreviewed-table", ID: testTaskID, Payload: json.RawMessage(`{}`)},
	})
	if err == nil || !strings.Contains(err.Error(), "not in the protected-history allowlist") {
		t.Fatalf("expected unknown kind rejection, got %v", err)
	}
}

func TestSchema170KindsDoNotChangeOlderArtifactValidation(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"protectedhistoryartifact", "controlplaneauditevent"} {
		if _, err := Build(testScope(), 169, []RawRecord{{
			Kind: kind, ID: testTaskID, Payload: json.RawMessage(`{}`),
		}}); err == nil || !strings.Contains(err.Error(), "requires source schema 170") {
			t.Fatalf("schema 169 accepted migration-170 kind %s: %v", kind, err)
		}
		if _, err := Build(testScope(), 170, []RawRecord{{
			Kind: kind, ID: testTaskID, Payload: json.RawMessage(`{}`),
		}}); err != nil {
			t.Fatalf("schema 170 rejected migration-170 kind %s: %v", kind, err)
		}
	}
}

func TestBuildRejectsAmbiguousOrMalformedPayloads(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{
		`{"status":"SUCCEEDED","status":"FAILED"}`,
		`[]`,
		`{"status":`,
	} {
		_, err := Build(testScope(), 138, []RawRecord{{
			Kind: "task", ID: testTaskID, Payload: json.RawMessage(payload),
		}})
		if err == nil {
			t.Fatalf("expected payload %q to be rejected", payload)
		}
	}
}

func TestParseRejectsUnknownFieldsAndTampering(t *testing.T) {
	t.Parallel()
	artifact := mustBuild(t, []RawRecord{{
		Kind: "task", ID: testTaskID, Payload: json.RawMessage(`{"status":"SUCCEEDED"}`),
	}})
	payload, err := Marshal(*artifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Parse(payload); err != nil {
		t.Fatal(err)
	}
	unknown := strings.Replace(string(payload), `"schema":`, `"unexpected":true,"schema":`, 1)
	if _, err = Parse([]byte(unknown)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field rejection, got %v", err)
	}
	tampered := strings.Replace(string(payload), `"SUCCEEDED"`, `"FAILED"`, 1)
	if _, err = Parse([]byte(tampered)); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected tamper rejection, got %v", err)
	}
}

func TestScopeRejectsEmptyDuplicateAndNilSelectors(t *testing.T) {
	t.Parallel()
	for _, scope := range []Scope{
		{OrganizationID: testOrganizationID},
		{
			OrganizationID:          testOrganizationID,
			CustomerOrganizationIDs: []string{testCustomerOrganizationID, testCustomerOrganizationID},
		},
		{
			OrganizationID:      testOrganizationID,
			DeploymentTargetIDs: []string{"00000000-0000-0000-0000-000000000000"},
		},
	} {
		if _, err := CanonicalScope(scope); err == nil {
			t.Fatalf("expected invalid scope to be rejected: %#v", scope)
		}
	}
}

func testScope() Scope {
	return Scope{
		OrganizationID:          testOrganizationID,
		CustomerOrganizationIDs: []string{testCustomerOrganizationID},
		DeploymentTargetIDs:     []string{testDeploymentTargetID},
	}
}

func mustBuild(t *testing.T, records []RawRecord) *Artifact {
	t.Helper()
	artifact, err := Build(testScope(), 138, records)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}
