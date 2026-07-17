package externalexecutiontimestamp

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
)

func SealManifest(
	manifest types.ExternalExecutionTimestampManifest,
	options types.ExternalExecutionTimestampSealOptions,
	approvedAt time.Time,
) (types.ExternalExecutionTimestampManifest, error) {
	if manifest.State != types.ExternalExecutionTimestampManifestStateDraft {
		return manifest, errors.New("only DRAFT manifests can be sealed")
	}

	validated := cloneManifest(manifest)
	for index := range validated.Cells {
		cell := &validated.Cells[index]
		if cell.Decision != types.ExternalExecutionTimestampDecisionProven &&
			cell.Decision != types.ExternalExecutionTimestampDecisionAttested {
			continue
		}
		if cell.RawValue == nil || cell.SourceOffsetSeconds == nil ||
			cell.ConvertedValue == nil {
			continue
		}
		expected, err := ConvertWallClock(*cell.RawValue, *cell.SourceOffsetSeconds)
		if err != nil {
			return manifest, fmt.Errorf("cell %d conversion: %w", index, err)
		}
		actual, err := time.Parse(time.RFC3339Nano, *cell.ConvertedValue)
		if err != nil {
			return manifest, fmt.Errorf("cell %d converted value must be RFC3339", index)
		}
		if !actual.Equal(expected) {
			return manifest, fmt.Errorf(
				"cell %d converted value does not reproduce raw wall minus explicit offset",
				index,
			)
		}
		canonical := FormatInstant(expected)
		cell.ConvertedValue = &canonical
	}
	var err error
	validated.DecisionContentChecksum, err = ComputeDecisionContentChecksum(validated)
	if err != nil {
		return manifest, err
	}
	if problems := ValidateManifestDocument(validated); len(problems) != 0 {
		return manifest, errors.Join(problems...)
	}

	sealed := validated
	sealed.AuthorIdentity = strings.TrimSpace(options.AuthorIdentity)
	sealed.ReviewerIdentity = strings.TrimSpace(options.ReviewerIdentity)
	sealed.EvidenceBundleReference = strings.TrimSpace(options.EvidenceBundleReference)
	sealed.EvidenceBundleChecksum = strings.TrimSpace(options.EvidenceBundleChecksum)
	sealed.TargetReleaseCommit = strings.TrimSpace(options.TargetReleaseCommit)
	sealed.TargetImageDigest = strings.TrimSpace(options.TargetImageDigest)
	sealed.ApprovedAt = FormatInstant(approvedAt)
	sealed.State = types.ExternalExecutionTimestampManifestStateApproved
	sealed.DecisionContentChecksum, err = ComputeDecisionContentChecksum(sealed)
	if err != nil {
		return manifest, err
	}
	if problems := ValidateManifestDocument(sealed); len(problems) != 0 {
		return manifest, errors.Join(problems...)
	}
	return sealed, nil
}

func cloneManifest(
	manifest types.ExternalExecutionTimestampManifest,
) types.ExternalExecutionTimestampManifest {
	cloned := manifest
	cloned.SupersedesManifestID = clonePointer(manifest.SupersedesManifestID)
	if manifest.Cells == nil {
		return cloned
	}
	cloned.Cells = make([]types.ExternalExecutionTimestampCellDecision, len(manifest.Cells))
	copy(cloned.Cells, manifest.Cells)
	for index := range cloned.Cells {
		cloned.Cells[index].RawValue = clonePointer(manifest.Cells[index].RawValue)
		cloned.Cells[index].SourceOffsetSeconds = clonePointer(
			manifest.Cells[index].SourceOffsetSeconds,
		)
		cloned.Cells[index].ConvertedValue = clonePointer(
			manifest.Cells[index].ConvertedValue,
		)
	}
	return cloned
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
