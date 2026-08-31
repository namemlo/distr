package protectedhistory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApprovedRetirementAllowlistIsCanonicalAndSealed(t *testing.T) {
	t.Parallel()
	baseline := mustBuild(t, []RawRecord{
		{Kind: "task", ID: testTaskID, Payload: json.RawMessage(`{"status":"SUCCEEDED"}`)},
		{Kind: "deploymentplan", ID: testPlanID, Payload: json.RawMessage(`{"status":"VALID"}`)},
	})
	allowlist, err := BuildApprovedRetirementAllowlist(
		*baseline,
		"sha256:"+strings.Repeat("a", 64),
		"77777777-7777-4777-8777-777777777777",
		"sha256:"+strings.Repeat("b", 64),
		[]RetirementAllowance{
			{Kind: "Task", ID: strings.ToUpper(testTaskID), BaselineHash: baseline.Records[1].Hash},
			{Kind: "deploymentplan", ID: testPlanID, BaselineHash: baseline.Records[0].Hash},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if allowlist.Items[0].Kind != "deploymentplan" || allowlist.Items[1].Kind != "task" {
		t.Fatalf("retirement items are not canonical: %#v", allowlist.Items)
	}
	payload, err := MarshalApprovedRetirementAllowlist(*allowlist)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseApprovedRetirementAllowlist(payload)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.AllowlistID != allowlist.AllowlistID {
		t.Fatalf("allowlist identity changed: %s != %s", parsed.AllowlistID, allowlist.AllowlistID)
	}
}

func TestApprovedRetirementAllowlistRejectsNonApprovedOrBroadInputs(t *testing.T) {
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

	notApproved := *allowlist
	notApproved.ApprovalState = "PENDING"
	if err := ValidateApprovedRetirementAllowlist(notApproved); err == nil {
		t.Fatal("non-approved retirement allowlist was accepted")
	}

	broad := *allowlist
	broad.Items = append([]RetirementAllowance(nil), allowlist.Items...)
	broad.Items[0].ID = "*"
	if err := ValidateApprovedRetirementAllowlist(broad); err == nil {
		t.Fatal("pattern retirement selector was accepted")
	}
}
