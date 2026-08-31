package protectedhistory

import (
	"fmt"
	"slices"
)

type ComparisonStatus string

const (
	ComparisonUnchanged     ComparisonStatus = "UNCHANGED"
	ComparisonAdditionsOnly ComparisonStatus = "ADDITIONS_ONLY"
	ComparisonRetirements   ComparisonStatus = "APPROVED_RETIREMENTS_ONLY"
	ComparisonViolation     ComparisonStatus = "VIOLATION"
)

type DifferenceType string

const (
	DifferenceAdded                  DifferenceType = "ADDED"
	DifferenceMissing                DifferenceType = "MISSING"
	DifferenceModified               DifferenceType = "MODIFIED"
	DifferenceUnusedRetirement       DifferenceType = "UNUSED_RETIREMENT_ALLOWANCE"
	DifferenceScopeMismatch          DifferenceType = "SCOPE_MISMATCH"
	DifferenceSourceSchemaRegression DifferenceType = "SOURCE_SCHEMA_REGRESSION"
	DifferenceSourceSchemaMismatch   DifferenceType = "SOURCE_SCHEMA_MISMATCH"
)

type Difference struct {
	Type         DifferenceType `json:"type"`
	Kind         string         `json:"kind,omitempty"`
	ID           string         `json:"id,omitempty"`
	BaselineHash string         `json:"baselineHash,omitempty"`
	CurrentHash  string         `json:"currentHash,omitempty"`
	Message      string         `json:"message,omitempty"`
}

type Comparison struct {
	Status                ComparisonStatus `json:"status"`
	BaselineArtifactID    string           `json:"baselineArtifactId"`
	CurrentArtifactID     string           `json:"currentArtifactId"`
	Additions             []Difference     `json:"additions"`
	ApprovedRetirements   []Difference     `json:"approvedRetirements"`
	Violations            []Difference     `json:"violations"`
	RetirementAllowlistID string           `json:"retirementAllowlistId,omitempty"`
}

func Compare(baseline, current Artifact) (*Comparison, error) {
	return compare(baseline, current, false, nil)
}

func CompareExact(
	baseline,
	current Artifact,
	retirementAuthorization *RetirementAuthorization,
) (*Comparison, error) {
	return compare(baseline, current, true, retirementAuthorization)
}

func compare(
	baseline,
	current Artifact,
	requireExact bool,
	retirementAuthorization *RetirementAuthorization,
) (*Comparison, error) {
	if err := Validate(baseline); err != nil {
		return nil, fmt.Errorf("baseline artifact: %w", err)
	}
	if err := Validate(current); err != nil {
		return nil, fmt.Errorf("current artifact: %w", err)
	}
	result := &Comparison{
		Status:              ComparisonUnchanged,
		BaselineArtifactID:  baseline.ArtifactID,
		CurrentArtifactID:   current.ArtifactID,
		Additions:           []Difference{},
		ApprovedRetirements: []Difference{},
		Violations:          []Difference{},
	}
	approvedByKey := map[string]RetirementAllowance{}
	if retirementAuthorization != nil {
		if !requireExact {
			return nil, fmt.Errorf("approved retirements require exact comparison")
		}
		if err := ValidateRetirementAuthorization(baseline, *retirementAuthorization); err != nil {
			return nil, fmt.Errorf("approved retirement authorization: %w", err)
		}
		result.RetirementAllowlistID = retirementAuthorization.Allowlist.AllowlistID
		for _, item := range retirementAuthorization.Allowlist.Items {
			approvedByKey[item.Kind+"\x00"+item.ID] = item
		}
	}
	if !scopeEqual(baseline.Scope, current.Scope) {
		result.Status = ComparisonViolation
		result.Violations = append(result.Violations, Difference{
			Type: DifferenceScopeMismatch, Message: "baseline and current scopes differ",
		})
		return result, nil
	}
	if requireExact && current.SourceSchemaVersion != baseline.SourceSchemaVersion {
		result.Violations = append(result.Violations, Difference{
			Type: DifferenceSourceSchemaMismatch,
			Message: fmt.Sprintf(
				"source schema differs: baseline %d, current %d",
				baseline.SourceSchemaVersion,
				current.SourceSchemaVersion,
			),
		})
	} else if current.SourceSchemaVersion < baseline.SourceSchemaVersion {
		result.Violations = append(result.Violations, Difference{
			Type: DifferenceSourceSchemaRegression,
			Message: fmt.Sprintf(
				"source schema regressed from %d to %d",
				baseline.SourceSchemaVersion,
				current.SourceSchemaVersion,
			),
		})
	}
	baselineByKey := make(map[string]Record, len(baseline.Records))
	for _, record := range baseline.Records {
		baselineByKey[recordKey(record)] = record
	}
	currentByKey := make(map[string]Record, len(current.Records))
	for _, record := range current.Records {
		currentByKey[recordKey(record)] = record
		baselineRecord, exists := baselineByKey[recordKey(record)]
		if !exists {
			difference := Difference{
				Type: DifferenceAdded, Kind: record.Kind, ID: record.ID, CurrentHash: record.Hash,
			}
			if requireExact {
				result.Violations = append(result.Violations, difference)
			} else {
				result.Additions = append(result.Additions, difference)
			}
			continue
		}
		if record.Hash != baselineRecord.Hash {
			result.Violations = append(result.Violations, Difference{
				Type: DifferenceModified, Kind: record.Kind, ID: record.ID,
				BaselineHash: baselineRecord.Hash, CurrentHash: record.Hash,
			})
		}
	}
	for _, record := range baseline.Records {
		if _, exists := currentByKey[recordKey(record)]; !exists {
			if allowance, approved := approvedByKey[recordKey(record)]; approved &&
				allowance.BaselineHash == record.Hash {
				result.ApprovedRetirements = append(result.ApprovedRetirements, Difference{
					Type: DifferenceMissing, Kind: record.Kind, ID: record.ID, BaselineHash: record.Hash,
				})
				delete(approvedByKey, recordKey(record))
				continue
			}
			result.Violations = append(result.Violations, Difference{
				Type: DifferenceMissing, Kind: record.Kind, ID: record.ID, BaselineHash: record.Hash,
			})
		}
	}
	for _, unused := range approvedByKey {
		result.Violations = append(result.Violations, Difference{
			Type: DifferenceUnusedRetirement, Kind: unused.Kind, ID: unused.ID,
			BaselineHash: unused.BaselineHash,
		})
	}
	slices.SortFunc(result.Additions, compareDifference)
	slices.SortFunc(result.ApprovedRetirements, compareDifference)
	slices.SortFunc(result.Violations, compareDifference)
	if len(result.Violations) > 0 {
		result.Status = ComparisonViolation
	} else if len(result.ApprovedRetirements) > 0 {
		result.Status = ComparisonRetirements
	} else if len(result.Additions) > 0 {
		result.Status = ComparisonAdditionsOnly
	}
	return result, nil
}

func recordKey(record Record) string {
	return record.Kind + "\x00" + record.ID
}

func compareDifference(left, right Difference) int {
	if left.Type != right.Type {
		if left.Type < right.Type {
			return -1
		}
		return 1
	}
	if left.Kind != right.Kind {
		if left.Kind < right.Kind {
			return -1
		}
		return 1
	}
	if left.ID < right.ID {
		return -1
	}
	if left.ID > right.ID {
		return 1
	}
	return 0
}
