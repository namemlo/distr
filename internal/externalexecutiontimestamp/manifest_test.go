package externalexecutiontimestamp_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func validDraftManifest(t *testing.T) types.ExternalExecutionTimestampManifest {
	t.Helper()
	rowID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	rawValues := []*string{
		stringPointer("2026-07-15T10:00:00.000000"),
		stringPointer("2026-07-15T10:05:00.000000"),
		nil,
		nil,
		stringPointer("2026-07-15T10:10:00.000000"),
	}
	columns := []string{
		"created_at", "updated_at", "started_at",
		"completed_at", "callback_deadline_at",
	}
	rawCells := make([]types.ExternalExecutionTimestampRawCell, 0, 5)
	decisions := make([]types.ExternalExecutionTimestampCellDecision, 0, 5)
	for index, column := range columns {
		raw := types.ExternalExecutionTimestampRawCell{
			SourceTable:   "externalexecution",
			SourceRowID:   rowID,
			SourceColumn:  column,
			ColumnOrdinal: uint8(index + 1),
			RawValue:      rawValues[index],
		}
		checksum, err := externalexecutiontimestamp.ComputeRawCellChecksum(raw)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		raw.RawCellChecksum = checksum
		decision := types.ExternalExecutionTimestampDecisionUnresolved
		if raw.RawValue == nil {
			decision = types.ExternalExecutionTimestampDecisionNull
		}
		rawCells = append(rawCells, raw)
		decisions = append(decisions, types.ExternalExecutionTimestampCellDecision{
			ExternalExecutionTimestampRawCell: raw,
			Decision:                          decision,
			ConversionExpressionVersion:       externalexecutiontimestamp.ConversionExpressionVersion,
		})
	}
	rawSet, err := externalexecutiontimestamp.ComputeRawSetChecksum(rawCells)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	identity, err := externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
		137, []uuid.UUID{rowID}, nil, 5, rawSet,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	manifest := types.ExternalExecutionTimestampManifest{
		ID:                          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		DatabaseIdentityChecksum:    identity,
		SourceSchemaVersion:         137,
		SnapshotStartedAt:           "2026-07-15T10:20:00.000000Z",
		SnapshotEndedAt:             "2026-07-15T10:20:01.000000Z",
		ExecutionCount:              1,
		RawCellCount:                5,
		PopulatedCellCount:          3,
		RawCellChecksum:             rawSet,
		ToolVersion:                 "distr-test",
		ConversionExpressionVersion: externalexecutiontimestamp.ConversionExpressionVersion,
		State:                       types.ExternalExecutionTimestampManifestStateDraft,
		Cells:                       decisions,
	}
	manifest.DecisionContentChecksum, err = externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return manifest
}

func approveManifest(
	t *testing.T,
	manifest types.ExternalExecutionTimestampManifest,
) types.ExternalExecutionTimestampManifest {
	t.Helper()
	manifest.State = types.ExternalExecutionTimestampManifestStateApproved
	manifest.EvidenceBundleReference = "evidence-bundle:timestamp-review-1"
	manifest.EvidenceBundleChecksum = "sha256:" + strings.Repeat("e", 64)
	manifest.AuthorIdentity = "operator@example.test"
	manifest.ReviewerIdentity = "reviewer@example.test"
	manifest.ApprovedAt = "2026-07-15T11:00:00.000000Z"
	manifest.TargetReleaseCommit = strings.Repeat("a", 40)
	manifest.TargetImageDigest = "sha256:" + strings.Repeat("b", 64)
	var err error
	manifest.DecisionContentChecksum, err = externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return manifest
}

func resolveFirstCell(
	t *testing.T,
	manifest *types.ExternalExecutionTimestampManifest,
) {
	t.Helper()
	offset := int32(7 * 3600)
	converted, err := externalexecutiontimestamp.ConvertWallClock(
		*manifest.Cells[0].RawValue, offset,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	value := externalexecutiontimestamp.FormatInstant(converted)
	manifest.Cells[0].Decision = types.ExternalExecutionTimestampDecisionProven
	manifest.Cells[0].SourceZone = "Asia/Bangkok"
	manifest.Cells[0].SourceOffsetSeconds = &offset
	manifest.Cells[0].ConvertedValue = &value
	manifest.Cells[0].EvidenceReference = "log-record:execution-created"
	manifest.Cells[0].EvidenceChecksum = "sha256:" + strings.Repeat("c", 64)
	manifest.Cells[0].ApprovingIdentity = "reviewer@example.test"
}

func containsProblem(problems []error, fragment string) bool {
	return slices.ContainsFunc(problems, func(problem error) bool {
		return strings.Contains(problem.Error(), fragment)
	})
}

func supersedingManifest(
	t *testing.T,
	previous types.ExternalExecutionTimestampManifest,
) types.ExternalExecutionTimestampManifest {
	t.Helper()
	next := previous
	next.ID = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	previousID := previous.ID
	next.SupersedesManifestID = &previousID
	next.Cells = slices.Clone(previous.Cells)
	var err error
	next.DecisionContentChecksum, err = externalexecutiontimestamp.ComputeDecisionContentChecksum(next)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return next
}

func refreshManifestSnapshotChecksums(
	t *testing.T,
	manifest *types.ExternalExecutionTimestampManifest,
) {
	t.Helper()
	rawCells := make([]types.ExternalExecutionTimestampRawCell, 0, len(manifest.Cells))
	executionIDs := make(map[uuid.UUID]struct{})
	eventIDs := make(map[uuid.UUID]struct{})
	populatedCellCount := uint64(0)
	for index := range manifest.Cells {
		raw := &manifest.Cells[index].ExternalExecutionTimestampRawCell
		checksum, err := externalexecutiontimestamp.ComputeRawCellChecksum(*raw)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		raw.RawCellChecksum = checksum
		rawCells = append(rawCells, *raw)
		if raw.RawValue != nil {
			populatedCellCount++
		}
		switch strings.ToLower(raw.SourceTable) {
		case "externalexecution":
			executionIDs[raw.SourceRowID] = struct{}{}
		case "externalexecutionevent":
			eventIDs[raw.SourceRowID] = struct{}{}
		}
	}
	manifest.ExecutionCount = uint64(len(executionIDs))
	manifest.EventCount = uint64(len(eventIDs))
	manifest.RawCellCount = uint64(len(rawCells))
	manifest.PopulatedCellCount = populatedCellCount
	var err error
	manifest.RawCellChecksum, err = externalexecutiontimestamp.ComputeRawSetChecksum(rawCells)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	executionIDList := make([]uuid.UUID, 0, len(executionIDs))
	for id := range executionIDs {
		executionIDList = append(executionIDList, id)
	}
	eventIDList := make([]uuid.UUID, 0, len(eventIDs))
	for id := range eventIDs {
		eventIDList = append(eventIDList, id)
	}
	manifest.DatabaseIdentityChecksum, err = externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
		manifest.SourceSchemaVersion,
		executionIDList,
		eventIDList,
		manifest.RawCellCount,
		manifest.RawCellChecksum,
	)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	manifest.DecisionContentChecksum, err = externalexecutiontimestamp.ComputeDecisionContentChecksum(*manifest)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
}

func TestDecisionChecksumsKnownVectors(t *testing.T) {
	g := NewWithT(t)
	manifest := approveManifest(t, validDraftManifest(t))
	resolveFirstCell(t, &manifest)
	supersededID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	manifest.SupersedesManifestID = &supersededID

	cellChecksum, err := externalexecutiontimestamp.ComputeCellDecisionChecksum(manifest.Cells[0])
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cellChecksum).To(Equal(
		"sha256:6a4fec0f9618399745f122115ad7d0e651a97d920a93f40cfe90c088e65ec7f2",
	))

	manifestChecksum, err := externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(manifestChecksum).To(Equal(
		"sha256:6f49c3e26e2fd7e87cfd4d79540d267e52cb8a1029ab3fc318853cbba32e2715",
	))
}

func TestValidateManifestRequiresCompleteSnapshot(t *testing.T) {
	g := NewWithT(t)
	manifest := validDraftManifest(t)
	g.Expect(externalexecutiontimestamp.ValidateManifestDocument(manifest)).
		To(BeEmpty())

	missing := manifest
	missing.Cells = slices.Clone(manifest.Cells[:4])
	missing.DecisionContentChecksum, _ = externalexecutiontimestamp.ComputeDecisionContentChecksum(missing)
	g.Expect(containsProblem(
		externalexecutiontimestamp.ValidateManifestDocument(missing),
		"raw cell count",
	)).To(BeTrue())

	duplicate := manifest
	duplicate.Cells = append(slices.Clone(manifest.Cells), manifest.Cells[0])
	duplicate.DecisionContentChecksum, _ = externalexecutiontimestamp.ComputeDecisionContentChecksum(duplicate)
	g.Expect(containsProblem(
		externalexecutiontimestamp.ValidateManifestDocument(duplicate),
		"duplicate cell",
	)).To(BeTrue())
}

func TestValidateManifestDecisionMatrix(t *testing.T) {
	g := NewWithT(t)
	manifest := validDraftManifest(t)
	converted := "2026-07-15T03:00:00.000000Z"
	manifest.Cells[0].ConvertedValue = &converted
	manifest.DecisionContentChecksum, _ = externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	g.Expect(containsProblem(
		externalexecutiontimestamp.ValidateManifestDocument(manifest),
		"UNRESOLVED",
	)).To(BeTrue())

	manifest = validDraftManifest(t)
	manifest.Cells[0].Decision = types.ExternalExecutionTimestampDecisionProven
	manifest.DecisionContentChecksum, _ = externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	g.Expect(containsProblem(
		externalexecutiontimestamp.ValidateManifestDocument(manifest),
		"PROVEN",
	)).To(BeTrue())

	manifest = validDraftManifest(t)
	resolveFirstCell(t, &manifest)
	wrong := "2026-07-15T04:00:00.000000Z"
	manifest.Cells[0].ConvertedValue = &wrong
	manifest.DecisionContentChecksum, _ = externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	g.Expect(containsProblem(
		externalexecutiontimestamp.ValidateManifestDocument(manifest),
		"does not reproduce",
	)).To(BeTrue())
}

func TestApprovedManifestRequiresIndependentReviewAndReleaseIdentity(t *testing.T) {
	g := NewWithT(t)
	manifest := approveManifest(t, validDraftManifest(t))
	g.Expect(externalexecutiontimestamp.ValidateManifestDocument(manifest)).
		To(BeEmpty())

	manifest.ReviewerIdentity = manifest.AuthorIdentity
	manifest.DecisionContentChecksum, _ = externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	g.Expect(containsProblem(
		externalexecutiontimestamp.ValidateManifestDocument(manifest),
		"must differ",
	)).To(BeTrue())
}

func TestValidateManifestRequiresSchema137(t *testing.T) {
	root := validDraftManifest(t)
	for name, manifest := range map[string]types.ExternalExecutionTimestampManifest{
		"root":        root,
		"superseding": supersedingManifest(t, root),
	} {
		t.Run(name, func(t *testing.T) {
			manifest.SourceSchemaVersion = 138
			refreshManifestSnapshotChecksums(t, &manifest)
			NewWithT(t).Expect(containsProblem(
				externalexecutiontimestamp.ValidateManifestDocument(manifest),
				"manifest source schema version must be 137",
			)).To(BeTrue())
		})
	}
}

func TestDecisionChecksumExcludesOnlyLifecycleStateAndApprovalInstant(t *testing.T) {
	g := NewWithT(t)
	manifest := approveManifest(t, validDraftManifest(t))
	first, err := externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	g.Expect(err).NotTo(HaveOccurred())
	manifest.State = types.ExternalExecutionTimestampManifestStateApplied
	manifest.ApprovedAt = "2026-07-15T11:01:00.000000Z"
	second, err := externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).To(Equal(first))
	manifest.TargetReleaseCommit = strings.Repeat("c", 40)
	third, err := externalexecutiontimestamp.ComputeDecisionContentChecksum(manifest)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(third).NotTo(Equal(first))
}

func TestValidateSupersessionPreservesResolvedCells(t *testing.T) {
	g := NewWithT(t)
	previous := approveManifest(t, validDraftManifest(t))
	resolveFirstCell(t, &previous)
	previous.DecisionContentChecksum, _ = externalexecutiontimestamp.ComputeDecisionContentChecksum(previous)
	next := previous
	next.ID = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	next.SupersedesManifestID = &previous.ID
	next.Cells = slices.Clone(previous.Cells)
	next.DecisionContentChecksum, _ = externalexecutiontimestamp.ComputeDecisionContentChecksum(next)
	g.Expect(externalexecutiontimestamp.ValidateSupersession(previous, next)).
		To(BeEmpty())

	wrong := "2026-07-15T04:00:00.000000Z"
	next.Cells[0].ConvertedValue = &wrong
	next.DecisionContentChecksum, _ = externalexecutiontimestamp.ComputeDecisionContentChecksum(next)
	g.Expect(containsProblem(
		externalexecutiontimestamp.ValidateSupersession(previous, next),
		"resolved instant cannot change",
	)).To(BeTrue())
}

func TestValidateSupersessionPreservesOriginalRawSnapshot(t *testing.T) {
	previous := approveManifest(t, validDraftManifest(t))
	testCases := []struct {
		name   string
		mutate func(*types.ExternalExecutionTimestampManifest)
	}{
		{
			name: "unresolved raw cell",
			mutate: func(next *types.ExternalExecutionTimestampManifest) {
				value := "2026-07-15T10:00:01.000000"
				next.Cells[0].RawValue = &value
			},
		},
		{
			name: "null raw cell",
			mutate: func(next *types.ExternalExecutionTimestampManifest) {
				value := "2026-07-15T10:06:00.000000"
				next.Cells[2].RawValue = &value
				offset := int32(0)
				converted, err := externalexecutiontimestamp.ConvertWallClock(value, offset)
				NewWithT(t).Expect(err).NotTo(HaveOccurred())
				convertedValue := externalexecutiontimestamp.FormatInstant(converted)
				next.Cells[2].Decision = types.ExternalExecutionTimestampDecisionProven
				next.Cells[2].SourceOffsetSeconds = &offset
				next.Cells[2].ConvertedValue = &convertedValue
				next.Cells[2].EvidenceReference = "paired-shadow:started-at"
				next.Cells[2].EvidenceChecksum = "sha256:" + strings.Repeat("d", 64)
				next.Cells[2].ApprovingIdentity = "reviewer@example.test"
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			next := supersedingManifest(t, previous)
			testCase.mutate(&next)
			refreshManifestSnapshotChecksums(t, &next)
			g := NewWithT(t)
			g.Expect(externalexecutiontimestamp.ValidateManifestDocument(next)).To(BeEmpty())
			g.Expect(containsProblem(
				externalexecutiontimestamp.ValidateSupersession(previous, next),
				"raw value and checksum must remain unchanged",
			)).To(BeTrue())
		})
	}
}

func TestValidateSupersessionPreservesDatasetAndSnapshotIdentity(t *testing.T) {
	previous := approveManifest(t, validDraftManifest(t))
	testCases := []struct {
		name   string
		mutate func(*types.ExternalExecutionTimestampManifest)
	}{
		{
			name: "snapshot interval",
			mutate: func(next *types.ExternalExecutionTimestampManifest) {
				next.SnapshotStartedAt = "2026-07-15T10:19:59.000000Z"
			},
		},
		{
			name: "cell key set",
			mutate: func(next *types.ExternalExecutionTimestampManifest) {
				extraID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
				for _, cell := range slices.Clone(next.Cells) {
					cell.SourceRowID = extraID
					next.Cells = append(next.Cells, cell)
				}
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			next := supersedingManifest(t, previous)
			testCase.mutate(&next)
			refreshManifestSnapshotChecksums(t, &next)
			g := NewWithT(t)
			g.Expect(externalexecutiontimestamp.ValidateManifestDocument(next)).To(BeEmpty())
			g.Expect(externalexecutiontimestamp.ValidateSupersession(previous, next)).
				NotTo(BeEmpty())
		})
	}
}

func TestValidateSupersessionPreservesResolvedDecision(t *testing.T) {
	previous := approveManifest(t, validDraftManifest(t))
	resolveFirstCell(t, &previous)
	refreshManifestSnapshotChecksums(t, &previous)
	next := supersedingManifest(t, previous)
	next.Cells[0].Decision = types.ExternalExecutionTimestampDecisionAttested
	refreshManifestSnapshotChecksums(t, &next)
	g := NewWithT(t)
	g.Expect(externalexecutiontimestamp.ValidateManifestDocument(next)).To(BeEmpty())
	g.Expect(externalexecutiontimestamp.ValidateSupersession(previous, next)).
		NotTo(BeEmpty())
}

func TestValidateSupersessionPreservesResolvedCellEvidence(t *testing.T) {
	previous := approveManifest(t, validDraftManifest(t))
	resolveFirstCell(t, &previous)
	refreshManifestSnapshotChecksums(t, &previous)
	next := supersedingManifest(t, previous)
	next.Cells[0].EvidenceReference = "replacement-evidence"
	refreshManifestSnapshotChecksums(t, &next)
	g := NewWithT(t)
	g.Expect(externalexecutiontimestamp.ValidateManifestDocument(next)).To(BeEmpty())
	g.Expect(externalexecutiontimestamp.ValidateSupersession(previous, next)).
		NotTo(BeEmpty())
}

func TestValidateSupersessionAllowsUnchangedRawPromotion(t *testing.T) {
	previous := approveManifest(t, validDraftManifest(t))
	next := supersedingManifest(t, previous)
	resolveFirstCell(t, &next)
	refreshManifestSnapshotChecksums(t, &next)
	g := NewWithT(t)
	g.Expect(externalexecutiontimestamp.ValidateManifestDocument(next)).To(BeEmpty())
	g.Expect(externalexecutiontimestamp.ValidateSupersession(previous, next)).To(BeEmpty())
}

func TestValidateSupersessionAllowsNewToolVersion(t *testing.T) {
	previous := approveManifest(t, validDraftManifest(t))
	next := supersedingManifest(t, previous)
	next.ToolVersion = "distr-test-v2"
	refreshManifestSnapshotChecksums(t, &next)
	g := NewWithT(t)
	g.Expect(externalexecutiontimestamp.ValidateManifestDocument(next)).To(BeEmpty())
	g.Expect(externalexecutiontimestamp.ValidateSupersession(previous, next)).To(BeEmpty())
}
