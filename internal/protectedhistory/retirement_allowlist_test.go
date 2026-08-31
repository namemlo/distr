package protectedhistory

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	testApprovalRequestID = "66666666-6666-4666-8666-666666666666"
	testRetirementJobID   = "77777777-7777-4777-8777-777777777777"
	testDecisionID        = "88888888-8888-4888-8888-888888888888"
	testOwnershipID       = "99999999-9999-4999-8999-999999999999"
	testRetirementItemID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
)

type retirementFixture struct {
	Baseline      *Artifact
	Current       *Artifact
	Authorization *RetirementAuthorization
	RawRecords    []RawRecord
}

func TestRetirementAuthorizationRequiresDatabaseBackedApprovalAndMembership(t *testing.T) {
	t.Parallel()
	fixture := mustRetirementFixture(t)
	if err := ValidateRetirementAuthorization(*fixture.Baseline, *fixture.Authorization); err != nil {
		t.Fatal(err)
	}

	result, err := CompareExact(*fixture.Baseline, *fixture.Current, fixture.Authorization)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ComparisonRetirements || len(result.ApprovedRetirements) != 1 {
		t.Fatalf("approved retirement was not accepted exactly: %#v", result)
	}

	selfAsserted := *fixture.Authorization
	selfAsserted.Approval = RetirementApprovalArtifact{}
	if err := ValidateRetirementAuthorization(*fixture.Baseline, selfAsserted); err == nil {
		t.Fatal("allowlist authorized itself without the approval artifact")
	}

	wrongMembership := *fixture.Authorization
	wrongMembership.Membership = fixture.Authorization.Membership
	wrongMembership.Membership.Items = append(
		[]SampleMembershipItem(nil), fixture.Authorization.Membership.Items...,
	)
	wrongMembership.Membership.Items[0].SubjectID = testPlanID
	if err := ValidateRetirementAuthorization(*fixture.Baseline, wrongMembership); err == nil {
		t.Fatal("unproven sample membership was accepted")
	}
}

func TestRetirementArtifactsAreCanonicalAndSealed(t *testing.T) {
	t.Parallel()
	fixture := mustRetirementFixture(t)

	allowlistPayload, err := MarshalApprovedRetirementAllowlist(fixture.Authorization.Allowlist)
	if err != nil {
		t.Fatal(err)
	}
	allowlist, err := ParseApprovedRetirementAllowlist(allowlistPayload)
	if err != nil || allowlist.AllowlistID != fixture.Authorization.Allowlist.AllowlistID {
		t.Fatalf("allowlist round trip failed: %v", err)
	}
	approvalPayload, err := MarshalRetirementApprovalArtifact(fixture.Authorization.Approval)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := ParseRetirementApprovalArtifact(approvalPayload)
	if err != nil || approval.ArtifactID != fixture.Authorization.Approval.ArtifactID {
		t.Fatalf("approval round trip failed: %v", err)
	}
	membershipPayload, err := MarshalSampleMembershipArtifact(fixture.Authorization.Membership)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := ParseSampleMembershipArtifact(membershipPayload)
	if err != nil || membership.ArtifactID != fixture.Authorization.Membership.ArtifactID {
		t.Fatalf("membership round trip failed: %v", err)
	}
}

func TestRetirementAllowlistRejectsBroadInputs(t *testing.T) {
	t.Parallel()
	fixture := mustRetirementFixture(t)
	broad := fixture.Authorization.Allowlist
	broad.Items = append([]RetirementAllowance(nil), broad.Items...)
	broad.Items[0].ID = "*"
	if err := ValidateApprovedRetirementAllowlist(broad); err == nil {
		t.Fatal("pattern retirement selector was accepted")
	}
}

func mustRetirementFixture(t *testing.T) retirementFixture {
	t.Helper()
	preview := "sha256:" + strings.Repeat("a", 64)
	marker := "tutorial-target"
	markerChecksum := checksumText(marker)
	raw := []RawRecord{
		{Kind: "deploymenttarget", ID: testDeploymentTargetID, Payload: json.RawMessage(`{"organization_id":"` + testOrganizationID + `","customer_organization_id":"` + testCustomerOrganizationID + `"}`)},
		{Kind: "approvalrequest", ID: testApprovalRequestID, Payload: json.RawMessage(`{"subjectType":"sample_retirement","subjectId":"` + testRetirementJobID + `","subjectChecksum":"` + preview + `","state":"APPROVED"}`)},
		{Kind: "sampleretirementjob", ID: testRetirementJobID, Payload: json.RawMessage(`{"state":"VERIFIED","previewChecksum":"` + preview + `","approvalId":"` + testApprovalRequestID + `","approvalChecksum":"sha256:` + strings.Repeat("b", 64) + `"}`)},
		{Kind: "approvaldecision", ID: testDecisionID, Payload: json.RawMessage(`{"approvalRequestId":"` + testApprovalRequestID + `","decision":"APPROVE"}`)},
		{Kind: "sampleretirementownershipevidence", ID: testOwnershipID, Payload: json.RawMessage(`{"subjectType":"deployment_target","subjectId":"` + testDeploymentTargetID + `","ownershipMarker":"` + marker + `","ownershipChecksum":"` + markerChecksum + `","sourceReference":"evidence://inventory/demo","sourceChecksum":"sha256:` + strings.Repeat("c", 64) + `"}`)},
		{Kind: "sampleretirementitem", ID: testRetirementItemID, Payload: json.RawMessage(`{"retirementJobId":"` + testRetirementJobID + `","subjectType":"deployment_target","subjectId":"` + testDeploymentTargetID + `","ownershipEvidenceId":"` + testOwnershipID + `","ownershipMarker":"` + marker + `","ownershipChecksum":"` + markerChecksum + `","state":"APPLIED"}`)},
	}
	baseline, err := Build(testScope(), 166, raw)
	if err != nil {
		t.Fatal(err)
	}
	current, err := Build(testScope(), 166, raw[1:])
	if err != nil {
		t.Fatal(err)
	}
	reference := func(kind, id string) ProtectedRecordReference {
		for _, record := range baseline.Records {
			if record.Kind == kind && record.ID == id {
				return ProtectedRecordReference{Kind: kind, ID: id, Hash: record.Hash}
			}
		}
		t.Fatalf("missing fixture record %s/%s", kind, id)
		return ProtectedRecordReference{}
	}
	approval, err := BuildRetirementApprovalArtifact(
		*baseline,
		preview,
		reference("approvalrequest", testApprovalRequestID),
		reference("sampleretirementjob", testRetirementJobID),
		[]ProtectedRecordReference{reference("approvaldecision", testDecisionID)},
	)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := BuildSampleMembershipArtifact(
		*baseline,
		preview,
		[]SampleMembershipItem{{
			Kind: "deploymenttarget", ID: testDeploymentTargetID,
			BaselineHash:      reference("deploymenttarget", testDeploymentTargetID).Hash,
			SubjectType:       "deployment_target",
			SubjectID:         testDeploymentTargetID,
			OwnershipEvidence: reference("sampleretirementownershipevidence", testOwnershipID),
			RetirementItem:    reference("sampleretirementitem", testRetirementItemID),
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	allowlist, err := BuildApprovedRetirementAllowlist(
		*baseline,
		preview,
		approval.ArtifactID,
		membership.ArtifactID,
		[]RetirementAllowance{{
			Kind: "deploymenttarget", ID: testDeploymentTargetID,
			BaselineHash: reference("deploymenttarget", testDeploymentTargetID).Hash,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return retirementFixture{
		Baseline: baseline,
		Current:  current,
		Authorization: &RetirementAuthorization{
			Allowlist: *allowlist, Approval: *approval, Membership: *membership,
		},
		RawRecords: raw,
	}
}
