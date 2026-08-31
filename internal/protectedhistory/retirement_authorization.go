package protectedhistory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	RetirementApprovalSchemaV1 = "distr.protected-history-retirement-approval/v1"
	SampleMembershipSchemaV1   = "distr.protected-history-sample-membership/v1"

	retirementApprovalDomain = "distr.protected-history-retirement-approval/v1"
	sampleMembershipDomain   = "distr.protected-history-sample-membership/v1"
)

type ProtectedRecordReference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

// RetirementApprovalArtifact binds the allowlist to approval records that
// were already present in the protected database snapshot.
type RetirementApprovalArtifact struct {
	Schema              string                     `json:"schema"`
	Purpose             string                     `json:"purpose"`
	BaselineArtifactID  string                     `json:"baselineArtifactId"`
	Scope               Scope                      `json:"scope"`
	PreviewChecksum     string                     `json:"previewChecksum"`
	ApprovalRequest     ProtectedRecordReference   `json:"approvalRequest"`
	SampleRetirementJob ProtectedRecordReference   `json:"sampleRetirementJob"`
	ApprovalDecisions   []ProtectedRecordReference `json:"approvalDecisions"`
	ArtifactID          string                     `json:"artifactId"`
}

type SampleMembershipItem struct {
	Kind              string                   `json:"kind"`
	ID                string                   `json:"id"`
	BaselineHash      string                   `json:"baselineHash"`
	SubjectType       string                   `json:"subjectType"`
	SubjectID         string                   `json:"subjectId"`
	OwnershipEvidence ProtectedRecordReference `json:"ownershipEvidence"`
	RetirementItem    ProtectedRecordReference `json:"retirementItem"`
}

// SampleMembershipArtifact proves each exception belongs to one exact subject
// admitted by the existing sample-domain retirement workflow.
type SampleMembershipArtifact struct {
	Schema             string                 `json:"schema"`
	Purpose            string                 `json:"purpose"`
	BaselineArtifactID string                 `json:"baselineArtifactId"`
	Scope              Scope                  `json:"scope"`
	PreviewChecksum    string                 `json:"previewChecksum"`
	Items              []SampleMembershipItem `json:"items"`
	ArtifactID         string                 `json:"artifactId"`
}

type RetirementAuthorization struct {
	Allowlist  ApprovedRetirementAllowlist
	Approval   RetirementApprovalArtifact
	Membership SampleMembershipArtifact
}

func BuildRetirementApprovalArtifact(
	baseline Artifact,
	previewChecksum string,
	request ProtectedRecordReference,
	job ProtectedRecordReference,
	decisions []ProtectedRecordReference,
) (*RetirementApprovalArtifact, error) {
	artifact := &RetirementApprovalArtifact{
		Schema:              RetirementApprovalSchemaV1,
		Purpose:             SampleDomainRetirement,
		BaselineArtifactID:  baseline.ArtifactID,
		Scope:               baseline.Scope,
		PreviewChecksum:     previewChecksum,
		ApprovalRequest:     request,
		SampleRetirementJob: job,
		ApprovalDecisions:   append([]ProtectedRecordReference(nil), decisions...),
	}
	if err := canonicalizeRetirementApproval(artifact); err != nil {
		return nil, err
	}
	artifact.ArtifactID = computeRetirementApprovalID(*artifact)
	if err := ValidateRetirementApprovalArtifact(*artifact); err != nil {
		return nil, err
	}
	return artifact, nil
}

func BuildSampleMembershipArtifact(
	baseline Artifact,
	previewChecksum string,
	items []SampleMembershipItem,
) (*SampleMembershipArtifact, error) {
	artifact := &SampleMembershipArtifact{
		Schema:             SampleMembershipSchemaV1,
		Purpose:            SampleDomainRetirement,
		BaselineArtifactID: baseline.ArtifactID,
		Scope:              baseline.Scope,
		PreviewChecksum:    previewChecksum,
		Items:              append([]SampleMembershipItem(nil), items...),
	}
	if err := canonicalizeSampleMembership(artifact); err != nil {
		return nil, err
	}
	artifact.ArtifactID = computeSampleMembershipID(*artifact)
	if err := ValidateSampleMembershipArtifact(*artifact); err != nil {
		return nil, err
	}
	return artifact, nil
}

func ParseRetirementApprovalArtifact(payload []byte) (*RetirementApprovalArtifact, error) {
	var artifact RetirementApprovalArtifact
	if err := parseStrictArtifact(payload, &artifact); err != nil {
		return nil, fmt.Errorf("parse retirement approval artifact: %w", err)
	}
	if err := ValidateRetirementApprovalArtifact(artifact); err != nil {
		return nil, err
	}
	return &artifact, nil
}

func ParseSampleMembershipArtifact(payload []byte) (*SampleMembershipArtifact, error) {
	var artifact SampleMembershipArtifact
	if err := parseStrictArtifact(payload, &artifact); err != nil {
		return nil, fmt.Errorf("parse sample membership artifact: %w", err)
	}
	if err := ValidateSampleMembershipArtifact(artifact); err != nil {
		return nil, err
	}
	return &artifact, nil
}

func MarshalRetirementApprovalArtifact(artifact RetirementApprovalArtifact) ([]byte, error) {
	if err := ValidateRetirementApprovalArtifact(artifact); err != nil {
		return nil, err
	}
	return marshalArtifact(artifact)
}

func MarshalSampleMembershipArtifact(artifact SampleMembershipArtifact) ([]byte, error) {
	if err := ValidateSampleMembershipArtifact(artifact); err != nil {
		return nil, err
	}
	return marshalArtifact(artifact)
}

func ValidateRetirementApprovalArtifact(artifact RetirementApprovalArtifact) error {
	if artifact.Schema != RetirementApprovalSchemaV1 || artifact.Purpose != SampleDomainRetirement {
		return errors.New("unsupported retirement approval artifact")
	}
	if err := validateAuthorizationEnvelope(
		artifact.BaselineArtifactID, artifact.Scope, artifact.PreviewChecksum,
	); err != nil {
		return err
	}
	for name, reference := range map[string]ProtectedRecordReference{
		"approval request":      artifact.ApprovalRequest,
		"sample retirement job": artifact.SampleRetirementJob,
	} {
		if err := validateRecordReference(reference); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if artifact.ApprovalRequest.Kind != "approvalrequest" {
		return errors.New("approval artifact must reference an approvalrequest record")
	}
	if artifact.SampleRetirementJob.Kind != "sampleretirementjob" {
		return errors.New("approval artifact must reference a sampleretirementjob record")
	}
	if len(artifact.ApprovalDecisions) == 0 {
		return errors.New("approval artifact requires at least one approval decision")
	}
	canonical := artifact
	if err := canonicalizeRetirementApproval(&canonical); err != nil {
		return err
	}
	if !slices.Equal(canonical.ApprovalDecisions, artifact.ApprovalDecisions) {
		return errors.New("approval decision references are not in strict canonical order")
	}
	if !checksumPattern.MatchString(artifact.ArtifactID) ||
		artifact.ArtifactID != computeRetirementApprovalID(artifact) {
		return errors.New("retirement approval artifact id mismatch")
	}
	return nil
}

func ValidateSampleMembershipArtifact(artifact SampleMembershipArtifact) error {
	if artifact.Schema != SampleMembershipSchemaV1 || artifact.Purpose != SampleDomainRetirement {
		return errors.New("unsupported sample membership artifact")
	}
	if err := validateAuthorizationEnvelope(
		artifact.BaselineArtifactID, artifact.Scope, artifact.PreviewChecksum,
	); err != nil {
		return err
	}
	if len(artifact.Items) == 0 {
		return errors.New("sample membership artifact requires at least one item")
	}
	canonical := artifact
	if err := canonicalizeSampleMembership(&canonical); err != nil {
		return err
	}
	if !slices.Equal(canonical.Items, artifact.Items) {
		return errors.New("sample membership items are not in strict canonical order")
	}
	if !checksumPattern.MatchString(artifact.ArtifactID) ||
		artifact.ArtifactID != computeSampleMembershipID(artifact) {
		return errors.New("sample membership artifact id mismatch")
	}
	return nil
}

func ValidateRetirementAuthorization(
	baseline Artifact,
	authorization RetirementAuthorization,
) error {
	if err := Validate(baseline); err != nil {
		return fmt.Errorf("baseline artifact: %w", err)
	}
	if baseline.SourceSchemaVersion < 162 {
		return errors.New("approved retirement requires schema 162 sample-retirement evidence")
	}
	if err := ValidateApprovedRetirementAllowlist(authorization.Allowlist); err != nil {
		return err
	}
	if err := ValidateRetirementApprovalArtifact(authorization.Approval); err != nil {
		return err
	}
	if err := ValidateSampleMembershipArtifact(authorization.Membership); err != nil {
		return err
	}
	allowlist := authorization.Allowlist
	approval := authorization.Approval
	membership := authorization.Membership
	if allowlist.ApprovalArtifactID != approval.ArtifactID ||
		allowlist.MembershipArtifactID != membership.ArtifactID {
		return errors.New("retirement allowlist artifact binding mismatch")
	}
	for _, envelope := range []struct {
		baselineID string
		scope      Scope
		preview    string
	}{
		{allowlist.BaselineArtifactID, allowlist.Scope, allowlist.PreviewChecksum},
		{approval.BaselineArtifactID, approval.Scope, approval.PreviewChecksum},
		{membership.BaselineArtifactID, membership.Scope, membership.PreviewChecksum},
	} {
		if envelope.baselineID != baseline.ArtifactID || !scopeEqual(envelope.scope, baseline.Scope) ||
			envelope.preview != allowlist.PreviewChecksum {
			return errors.New("retirement authorization is bound to a different baseline, scope, or preview")
		}
	}
	if err := validateApprovalAgainstBaseline(baseline, approval); err != nil {
		return err
	}
	return validateMembershipAgainstBaseline(baseline, approval, allowlist, membership)
}

func validateApprovalAgainstBaseline(
	baseline Artifact,
	approval RetirementApprovalArtifact,
) error {
	request, err := requireBaselineReference(baseline, approval.ApprovalRequest)
	if err != nil {
		return err
	}
	job, err := requireBaselineReference(baseline, approval.SampleRetirementJob)
	if err != nil {
		return err
	}
	requestPayload, err := recordPayload(request)
	if err != nil {
		return err
	}
	jobPayload, err := recordPayload(job)
	if err != nil {
		return err
	}
	if payloadString(requestPayload, "subjectType", "subject_type") != "sample_retirement" ||
		payloadString(requestPayload, "subjectId", "subject_id") != job.ID ||
		payloadString(requestPayload, "subjectChecksum", "subject_checksum") != approval.PreviewChecksum ||
		payloadString(requestPayload, "state") != "APPROVED" {
		return errors.New("approval request does not prove an approved sample-retirement preview")
	}
	jobState := payloadString(jobPayload, "state")
	if jobState != "APPLIED" && jobState != "VERIFIED" {
		return errors.New("sample retirement job is not applied or verified")
	}
	if payloadString(jobPayload, "previewChecksum", "preview_checksum") != approval.PreviewChecksum ||
		payloadString(jobPayload, "approvalId", "approval_id") != request.ID ||
		!checksumPattern.MatchString(payloadString(jobPayload, "approvalChecksum", "approval_checksum")) {
		return errors.New("sample retirement job approval binding is invalid")
	}
	for _, reference := range approval.ApprovalDecisions {
		decision, err := requireBaselineReference(baseline, reference)
		if err != nil {
			return err
		}
		payload, err := recordPayload(decision)
		if err != nil {
			return err
		}
		if payloadString(payload, "approvalRequestId", "approval_request_id") != request.ID ||
			payloadString(payload, "decision") != "APPROVE" {
			return errors.New("approval decision does not approve the bound request")
		}
	}
	return nil
}

func validateMembershipAgainstBaseline(
	baseline Artifact,
	approval RetirementApprovalArtifact,
	allowlist ApprovedRetirementAllowlist,
	membership SampleMembershipArtifact,
) error {
	allowances := make(map[string]RetirementAllowance, len(allowlist.Items))
	for _, item := range allowlist.Items {
		allowances[item.Kind+"\x00"+item.ID] = item
	}
	for _, item := range membership.Items {
		key := item.Kind + "\x00" + item.ID
		allowance, ok := allowances[key]
		if !ok || allowance.BaselineHash != item.BaselineHash {
			return fmt.Errorf("sample membership %s/%s is not an exact allowlist item", item.Kind, item.ID)
		}
		delete(allowances, key)
		protected, err := requireBaselineReference(baseline, ProtectedRecordReference{
			Kind: item.Kind, ID: item.ID, Hash: item.BaselineHash,
		})
		if err != nil {
			return err
		}
		ownership, err := requireBaselineReference(baseline, item.OwnershipEvidence)
		if err != nil {
			return err
		}
		retirementItem, err := requireBaselineReference(baseline, item.RetirementItem)
		if err != nil {
			return err
		}
		ownershipPayload, err := recordPayload(ownership)
		if err != nil {
			return err
		}
		retirementPayload, err := recordPayload(retirementItem)
		if err != nil {
			return err
		}
		marker := payloadString(ownershipPayload, "ownershipMarker", "ownership_marker")
		markerChecksum := payloadString(ownershipPayload, "ownershipChecksum", "ownership_checksum")
		if ownership.Kind != "sampleretirementownershipevidence" ||
			payloadString(ownershipPayload, "subjectType", "subject_type") != item.SubjectType ||
			payloadString(ownershipPayload, "subjectId", "subject_id") != item.SubjectID ||
			marker == "" || markerChecksum != checksumText(marker) ||
			payloadString(ownershipPayload, "sourceReference", "source_reference") == "" ||
			!checksumPattern.MatchString(payloadString(ownershipPayload, "sourceChecksum", "source_checksum")) {
			return errors.New("sample ownership evidence does not prove exact registered membership")
		}
		if retirementItem.Kind != "sampleretirementitem" ||
			payloadString(retirementPayload, "retirementJobId", "retirement_job_id") !=
				approval.SampleRetirementJob.ID ||
			payloadString(retirementPayload, "subjectType", "subject_type") != item.SubjectType ||
			payloadString(retirementPayload, "subjectId", "subject_id") != item.SubjectID ||
			payloadString(retirementPayload, "ownershipEvidenceId", "ownership_evidence_id") != ownership.ID ||
			payloadString(retirementPayload, "ownershipMarker", "ownership_marker") != marker ||
			payloadString(retirementPayload, "ownershipChecksum", "ownership_checksum") != markerChecksum ||
			payloadString(retirementPayload, "state") != "APPLIED" {
			return errors.New("sample retirement item does not bind an applied exact-ID membership")
		}
		if !recordDirectlyBelongsToSubject(protected, item.SubjectType, item.SubjectID) {
			return fmt.Errorf("protected record %s/%s is not directly bound to the sample subject", item.Kind, item.ID)
		}
	}
	if len(allowances) != 0 {
		return errors.New("sample membership artifact does not cover every retirement allowance")
	}
	return nil
}

func recordDirectlyBelongsToSubject(record Record, subjectType, subjectID string) bool {
	if (subjectType == "application" && record.Kind == "application" && record.ID == subjectID) ||
		(subjectType == "deployment_target" && record.Kind == "deploymenttarget" && record.ID == subjectID) {
		return true
	}
	payload, err := recordPayload(record)
	if err != nil {
		return false
	}
	keys := map[string][]string{
		"application":       {"applicationId", "application_id"},
		"deployment_target": {"deploymentTargetId", "deployment_target_id"},
		"environment":       {"environmentId", "environment_id"},
	}[subjectType]
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && strings.EqualFold(value, subjectID) {
			return true
		}
	}
	return false
}

func canonicalizeRetirementApproval(artifact *RetirementApprovalArtifact) error {
	var err error
	artifact.ApprovalRequest, err = canonicalRecordReference(artifact.ApprovalRequest)
	if err != nil {
		return err
	}
	artifact.SampleRetirementJob, err = canonicalRecordReference(artifact.SampleRetirementJob)
	if err != nil {
		return err
	}
	for index := range artifact.ApprovalDecisions {
		artifact.ApprovalDecisions[index], err = canonicalRecordReference(artifact.ApprovalDecisions[index])
		if err != nil {
			return err
		}
		if artifact.ApprovalDecisions[index].Kind != "approvaldecision" {
			return errors.New("approval decision reference has the wrong kind")
		}
	}
	slices.SortFunc(artifact.ApprovalDecisions, compareRecordReference)
	return rejectDuplicateReferences(artifact.ApprovalDecisions)
}

func canonicalizeSampleMembership(artifact *SampleMembershipArtifact) error {
	for index := range artifact.Items {
		item := &artifact.Items[index]
		item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
		if _, ok := allowedKinds[item.Kind]; !ok {
			return fmt.Errorf("sample membership kind %q is not protected", item.Kind)
		}
		id, err := canonicalUUID(item.Kind, item.ID)
		if err != nil {
			return err
		}
		item.ID = id
		if !checksumPattern.MatchString(item.BaselineHash) {
			return errors.New("sample membership baseline hash is invalid")
		}
		item.SubjectType = strings.ToLower(strings.TrimSpace(item.SubjectType))
		if item.SubjectType != "application" && item.SubjectType != "deployment_target" &&
			item.SubjectType != "environment" {
			return fmt.Errorf("unsupported sample membership subject type %q", item.SubjectType)
		}
		item.SubjectID, err = canonicalUUID(item.SubjectType, item.SubjectID)
		if err != nil {
			return err
		}
		item.OwnershipEvidence, err = canonicalRecordReference(item.OwnershipEvidence)
		if err != nil {
			return err
		}
		item.RetirementItem, err = canonicalRecordReference(item.RetirementItem)
		if err != nil {
			return err
		}
	}
	slices.SortFunc(artifact.Items, func(left, right SampleMembershipItem) int {
		if left.Kind != right.Kind {
			return strings.Compare(left.Kind, right.Kind)
		}
		return strings.Compare(left.ID, right.ID)
	})
	for index := 1; index < len(artifact.Items); index++ {
		if artifact.Items[index-1].Kind == artifact.Items[index].Kind &&
			artifact.Items[index-1].ID == artifact.Items[index].ID {
			return errors.New("duplicate sample membership item")
		}
	}
	return nil
}

func validateAuthorizationEnvelope(baselineID string, scope Scope, preview string) error {
	if !checksumPattern.MatchString(baselineID) || !checksumPattern.MatchString(preview) {
		return errors.New("authorization envelope checksum is invalid")
	}
	canonical, err := CanonicalScope(scope)
	if err != nil {
		return err
	}
	if !scopeEqual(canonical, scope) {
		return errors.New("authorization scope is not canonical")
	}
	return nil
}

func validateRecordReference(reference ProtectedRecordReference) error {
	canonical, err := canonicalRecordReference(reference)
	if err != nil {
		return err
	}
	if canonical != reference {
		return errors.New("record reference is not canonical")
	}
	return nil
}

func canonicalRecordReference(reference ProtectedRecordReference) (ProtectedRecordReference, error) {
	reference.Kind = strings.ToLower(strings.TrimSpace(reference.Kind))
	if _, ok := allowedKinds[reference.Kind]; !ok {
		return ProtectedRecordReference{}, fmt.Errorf("record reference kind %q is not protected", reference.Kind)
	}
	id, err := canonicalUUID(reference.Kind, reference.ID)
	if err != nil {
		return ProtectedRecordReference{}, err
	}
	reference.ID = id
	if !checksumPattern.MatchString(reference.Hash) {
		return ProtectedRecordReference{}, errors.New("record reference hash is invalid")
	}
	return reference, nil
}

func compareRecordReference(left, right ProtectedRecordReference) int {
	if left.Kind != right.Kind {
		return strings.Compare(left.Kind, right.Kind)
	}
	return strings.Compare(left.ID, right.ID)
}

func rejectDuplicateReferences(references []ProtectedRecordReference) error {
	for index := 1; index < len(references); index++ {
		if references[index-1].Kind == references[index].Kind && references[index-1].ID == references[index].ID {
			return errors.New("duplicate protected record reference")
		}
	}
	return nil
}

func requireBaselineReference(baseline Artifact, reference ProtectedRecordReference) (Record, error) {
	for _, record := range baseline.Records {
		if record.Kind == reference.Kind && record.ID == reference.ID {
			if record.Hash != reference.Hash {
				return Record{}, fmt.Errorf("baseline record %s/%s hash differs from authorization", record.Kind, record.ID)
			}
			return record, nil
		}
	}
	return Record{}, fmt.Errorf("authorization record %s/%s is absent from the baseline", reference.Kind, reference.ID)
}

func recordPayload(record Record) (map[string]any, error) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode %s/%s payload: %w", record.Kind, record.ID, err)
	}
	return payload, nil
}

func payloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}

func checksumText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func parseStrictArtifact(payload []byte, output any) error {
	if _, err := canonicalJSON(payload, true); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func marshalArtifact(artifact any) ([]byte, error) {
	payload, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

func computeRetirementApprovalID(artifact RetirementApprovalArtifact) string {
	var buffer bytes.Buffer
	writeField(&buffer, retirementApprovalDomain)
	writeField(&buffer, artifact.Schema)
	writeField(&buffer, artifact.Purpose)
	writeField(&buffer, artifact.BaselineArtifactID)
	writeField(&buffer, artifact.Scope.OrganizationID)
	writeStringSet(&buffer, artifact.Scope.CustomerOrganizationIDs)
	writeStringSet(&buffer, artifact.Scope.DeploymentTargetIDs)
	writeField(&buffer, artifact.PreviewChecksum)
	writeRecordReference(&buffer, artifact.ApprovalRequest)
	writeRecordReference(&buffer, artifact.SampleRetirementJob)
	for _, decision := range artifact.ApprovalDecisions {
		writeRecordReference(&buffer, decision)
	}
	return checksum(buffer.Bytes())
}

func computeSampleMembershipID(artifact SampleMembershipArtifact) string {
	var buffer bytes.Buffer
	writeField(&buffer, sampleMembershipDomain)
	writeField(&buffer, artifact.Schema)
	writeField(&buffer, artifact.Purpose)
	writeField(&buffer, artifact.BaselineArtifactID)
	writeField(&buffer, artifact.Scope.OrganizationID)
	writeStringSet(&buffer, artifact.Scope.CustomerOrganizationIDs)
	writeStringSet(&buffer, artifact.Scope.DeploymentTargetIDs)
	writeField(&buffer, artifact.PreviewChecksum)
	for _, item := range artifact.Items {
		writeField(&buffer, item.Kind)
		writeField(&buffer, item.ID)
		writeField(&buffer, item.BaselineHash)
		writeField(&buffer, item.SubjectType)
		writeField(&buffer, item.SubjectID)
		writeRecordReference(&buffer, item.OwnershipEvidence)
		writeRecordReference(&buffer, item.RetirementItem)
	}
	return checksum(buffer.Bytes())
}

func writeRecordReference(buffer *bytes.Buffer, reference ProtectedRecordReference) {
	writeField(buffer, reference.Kind)
	writeField(buffer, reference.ID)
	writeField(buffer, reference.Hash)
}
