package protectedhistory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	RetirementAllowlistSchemaV1 = "distr.protected-history-retirement-allowlist/v1"
	SampleDomainRetirement      = "sample_domain_retirement"

	retirementAllowlistDomain = "distr.protected-history-retirement-allowlist/v1"
)

type RetirementAllowance struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	BaselineHash string `json:"baselineHash"`
}

// ApprovedRetirementAllowlist names exact missing records but delegates
// authorization and sample ownership proof to two separately sealed artifacts.
type ApprovedRetirementAllowlist struct {
	Schema               string                `json:"schema"`
	Purpose              string                `json:"purpose"`
	BaselineArtifactID   string                `json:"baselineArtifactId"`
	Scope                Scope                 `json:"scope"`
	PreviewChecksum      string                `json:"previewChecksum"`
	ApprovalArtifactID   string                `json:"approvalArtifactId"`
	MembershipArtifactID string                `json:"membershipArtifactId"`
	Items                []RetirementAllowance `json:"items"`
	AllowlistID          string                `json:"allowlistId"`
}

func BuildApprovedRetirementAllowlist(
	baseline Artifact,
	previewChecksum string,
	approvalArtifactID string,
	membershipArtifactID string,
	items []RetirementAllowance,
) (*ApprovedRetirementAllowlist, error) {
	if err := Validate(baseline); err != nil {
		return nil, fmt.Errorf("baseline artifact: %w", err)
	}
	allowlist := &ApprovedRetirementAllowlist{
		Schema:               RetirementAllowlistSchemaV1,
		Purpose:              SampleDomainRetirement,
		BaselineArtifactID:   baseline.ArtifactID,
		Scope:                baseline.Scope,
		PreviewChecksum:      previewChecksum,
		ApprovalArtifactID:   approvalArtifactID,
		MembershipArtifactID: membershipArtifactID,
		Items:                append([]RetirementAllowance(nil), items...),
	}
	if err := canonicalizeRetirementAllowlist(allowlist); err != nil {
		return nil, err
	}
	allowlist.AllowlistID = computeRetirementAllowlistID(*allowlist)
	if err := ValidateApprovedRetirementAllowlist(*allowlist); err != nil {
		return nil, err
	}
	return allowlist, nil
}

func ParseApprovedRetirementAllowlist(payload []byte) (*ApprovedRetirementAllowlist, error) {
	if _, err := canonicalJSON(payload, true); err != nil {
		return nil, fmt.Errorf("parse approved retirement allowlist: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var allowlist ApprovedRetirementAllowlist
	if err := decoder.Decode(&allowlist); err != nil {
		return nil, fmt.Errorf("parse approved retirement allowlist: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("parse approved retirement allowlist: %w", err)
	}
	if err := ValidateApprovedRetirementAllowlist(allowlist); err != nil {
		return nil, err
	}
	return &allowlist, nil
}

func MarshalApprovedRetirementAllowlist(allowlist ApprovedRetirementAllowlist) ([]byte, error) {
	if err := ValidateApprovedRetirementAllowlist(allowlist); err != nil {
		return nil, err
	}
	payload, err := json.MarshalIndent(allowlist, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal approved retirement allowlist: %w", err)
	}
	return append(payload, '\n'), nil
}

func ValidateApprovedRetirementAllowlist(allowlist ApprovedRetirementAllowlist) error {
	if allowlist.Schema != RetirementAllowlistSchemaV1 {
		return fmt.Errorf("unsupported approved retirement allowlist schema %q", allowlist.Schema)
	}
	if allowlist.Purpose != SampleDomainRetirement {
		return fmt.Errorf("unsupported retirement purpose %q", allowlist.Purpose)
	}
	if !checksumPattern.MatchString(allowlist.BaselineArtifactID) {
		return errors.New("baseline artifact id must use lowercase sha256 format")
	}
	canonicalScope, err := CanonicalScope(allowlist.Scope)
	if err != nil {
		return fmt.Errorf("validate approved retirement scope: %w", err)
	}
	if !scopeEqual(canonicalScope, allowlist.Scope) {
		return errors.New("approved retirement scope is not in canonical order")
	}
	for name, value := range map[string]string{
		"preview checksum":       allowlist.PreviewChecksum,
		"approval artifact id":   allowlist.ApprovalArtifactID,
		"membership artifact id": allowlist.MembershipArtifactID,
	} {
		if !checksumPattern.MatchString(value) {
			return fmt.Errorf("%s must use lowercase sha256 format", name)
		}
	}
	if len(allowlist.Items) == 0 {
		return errors.New("approved retirement allowlist requires at least one exact item")
	}
	canonical := allowlist
	if err := canonicalizeRetirementAllowlist(&canonical); err != nil {
		return err
	}
	if !slices.Equal(canonical.Items, allowlist.Items) {
		return errors.New("approved retirement items are not in strict canonical order")
	}
	if !checksumPattern.MatchString(allowlist.AllowlistID) {
		return errors.New("allowlist id must use lowercase sha256 format")
	}
	if allowlist.AllowlistID != computeRetirementAllowlistID(allowlist) {
		return errors.New("approved retirement allowlist id mismatch")
	}
	return nil
}

func canonicalizeRetirementAllowlist(allowlist *ApprovedRetirementAllowlist) error {
	for index := range allowlist.Items {
		item := &allowlist.Items[index]
		item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
		if _, ok := allowedKinds[item.Kind]; !ok {
			return fmt.Errorf("retirement item kind %q is not protected", item.Kind)
		}
		id, err := canonicalUUID(item.Kind, item.ID)
		if err != nil {
			return err
		}
		item.ID = id
		if !checksumPattern.MatchString(item.BaselineHash) {
			return fmt.Errorf("retirement item %s/%s baseline hash is invalid", item.Kind, item.ID)
		}
	}
	slices.SortFunc(allowlist.Items, func(left, right RetirementAllowance) int {
		if comparison := strings.Compare(left.Kind, right.Kind); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.ID, right.ID)
	})
	for index := 1; index < len(allowlist.Items); index++ {
		if allowlist.Items[index-1].Kind == allowlist.Items[index].Kind &&
			allowlist.Items[index-1].ID == allowlist.Items[index].ID {
			return fmt.Errorf(
				"duplicate approved retirement item %s/%s",
				allowlist.Items[index].Kind,
				allowlist.Items[index].ID,
			)
		}
	}
	return nil
}

func computeRetirementAllowlistID(allowlist ApprovedRetirementAllowlist) string {
	var buffer bytes.Buffer
	writeField(&buffer, retirementAllowlistDomain)
	writeField(&buffer, allowlist.Schema)
	writeField(&buffer, allowlist.Purpose)
	writeField(&buffer, allowlist.BaselineArtifactID)
	writeField(&buffer, allowlist.Scope.OrganizationID)
	writeStringSet(&buffer, allowlist.Scope.CustomerOrganizationIDs)
	writeStringSet(&buffer, allowlist.Scope.DeploymentTargetIDs)
	writeField(&buffer, allowlist.PreviewChecksum)
	writeField(&buffer, allowlist.ApprovalArtifactID)
	writeField(&buffer, allowlist.MembershipArtifactID)
	for _, item := range allowlist.Items {
		writeField(&buffer, item.Kind)
		writeField(&buffer, item.ID)
		writeField(&buffer, item.BaselineHash)
	}
	return checksum(buffer.Bytes())
}
