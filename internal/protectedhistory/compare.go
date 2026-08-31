package protectedhistory

import (
	"fmt"
	"slices"
)

type ComparisonStatus string

const (
	ComparisonUnchanged     ComparisonStatus = "UNCHANGED"
	ComparisonAdditionsOnly ComparisonStatus = "ADDITIONS_ONLY"
	ComparisonViolation     ComparisonStatus = "VIOLATION"
)

type DifferenceType string

const (
	DifferenceAdded                  DifferenceType = "ADDED"
	DifferenceMissing                DifferenceType = "MISSING"
	DifferenceModified               DifferenceType = "MODIFIED"
	DifferenceScopeMismatch          DifferenceType = "SCOPE_MISMATCH"
	DifferenceSourceSchemaRegression DifferenceType = "SOURCE_SCHEMA_REGRESSION"
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
	Status             ComparisonStatus `json:"status"`
	BaselineArtifactID string           `json:"baselineArtifactId"`
	CurrentArtifactID  string           `json:"currentArtifactId"`
	Additions          []Difference     `json:"additions"`
	Violations         []Difference     `json:"violations"`
}

func Compare(baseline, current Artifact) (*Comparison, error) {
	if err := Validate(baseline); err != nil {
		return nil, fmt.Errorf("baseline artifact: %w", err)
	}
	if err := Validate(current); err != nil {
		return nil, fmt.Errorf("current artifact: %w", err)
	}
	result := &Comparison{
		Status:             ComparisonUnchanged,
		BaselineArtifactID: baseline.ArtifactID,
		CurrentArtifactID:  current.ArtifactID,
		Additions:          []Difference{},
		Violations:         []Difference{},
	}
	if !scopeEqual(baseline.Scope, current.Scope) {
		result.Status = ComparisonViolation
		result.Violations = append(result.Violations, Difference{
			Type: DifferenceScopeMismatch, Message: "baseline and current scopes differ",
		})
		return result, nil
	}
	if current.SourceSchemaVersion < baseline.SourceSchemaVersion {
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
			result.Additions = append(result.Additions, Difference{
				Type: DifferenceAdded, Kind: record.Kind, ID: record.ID, CurrentHash: record.Hash,
			})
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
			result.Violations = append(result.Violations, Difference{
				Type: DifferenceMissing, Kind: record.Kind, ID: record.ID, BaselineHash: record.Hash,
			})
		}
	}
	slices.SortFunc(result.Additions, compareDifference)
	slices.SortFunc(result.Violations, compareDifference)
	if len(result.Violations) > 0 {
		result.Status = ComparisonViolation
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
