package externalexecutiontimestamp_test

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func stringPointer(value string) *string { return &value }

func TestCanonicalRawCellKnownVectorsAndSorting(t *testing.T) {
	g := NewWithT(t)
	rowID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	created := types.ExternalExecutionTimestampRawCell{
		SourceTable: "ExternalExecution", SourceRowID: rowID,
		SourceColumn: "CREATED_AT", ColumnOrdinal: 1,
		RawValue: stringPointer("2026-07-15T10:11:12.123456"),
	}
	started := types.ExternalExecutionTimestampRawCell{
		SourceTable: "externalexecution", SourceRowID: rowID,
		SourceColumn: "started_at", ColumnOrdinal: 3,
	}

	createdChecksum, err := externalexecutiontimestamp.ComputeRawCellChecksum(created)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(createdChecksum).To(Equal(
		"sha256:570790f803e3f137b10c20d677ff5a769ce570e5c4cb4631dfea9c860b875599",
	))
	nullChecksum, err := externalexecutiontimestamp.ComputeRawCellChecksum(started)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(nullChecksum).To(Equal(
		"sha256:971fac6746efa0eee7a3b0da1bbd3d41c63b4869de9c3a59df96e8f7389aa9be",
	))

	first, err := externalexecutiontimestamp.ComputeRawSetChecksum(
		[]types.ExternalExecutionTimestampRawCell{started, created},
	)
	g.Expect(err).NotTo(HaveOccurred())
	second, err := externalexecutiontimestamp.ComputeRawSetChecksum(
		[]types.ExternalExecutionTimestampRawCell{created, started},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first).To(Equal(second))
	g.Expect(first).To(Equal(
		"sha256:ec4f13a15923b3f14255d3f40692ee40e591f640f735605fddf62497a8f24fa0",
	))

	identity, err := externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
		137, []uuid.UUID{rowID}, nil, 2, first,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(identity).To(Equal(
		"sha256:896dbdbb325b56a0a47c6dbfcbed6a35b98e09550fb4e76aedcdd4946f532d00",
	))
}

func TestCanonicalRawSetRejectsDuplicatesAndInvalidCells(t *testing.T) {
	g := NewWithT(t)
	value := "2026-07-15T10:11:12.123456"
	cell := types.ExternalExecutionTimestampRawCell{
		SourceTable: "externalexecution", SourceRowID: uuid.New(),
		SourceColumn: "created_at", ColumnOrdinal: 1, RawValue: &value,
	}
	_, err := externalexecutiontimestamp.ComputeRawSetChecksum(
		[]types.ExternalExecutionTimestampRawCell{cell, cell},
	)
	g.Expect(err).To(MatchError(ContainSubstring("duplicate raw cell")))

	cell.ColumnOrdinal = 2
	_, err = externalexecutiontimestamp.ComputeRawCellChecksum(cell)
	g.Expect(err).To(MatchError(ContainSubstring("allowlist")))
}

func TestConvertWallClockUsesOnlyExplicitOffset(t *testing.T) {
	testCases := []struct {
		raw    string
		offset int32
		want   string
	}{
		{"2026-07-15T12:00:00.000000", 0, "2026-07-15T12:00:00.000000Z"},
		{"2026-07-15T12:00:00.000000", 7 * 3600, "2026-07-15T05:00:00.000000Z"},
		{"2026-07-15T12:00:00.000000", -5 * 3600, "2026-07-15T17:00:00.000000Z"},
		{"2026-07-15T12:00:00.000000", 5*3600 + 30*60, "2026-07-15T06:30:00.000000Z"},
		{"2026-11-01T01:30:00.000000", -4 * 3600, "2026-11-01T05:30:00.000000Z"},
		{"2026-11-01T01:30:00.000000", -5 * 3600, "2026-11-01T06:30:00.000000Z"},
	}
	for _, testCase := range testCases {
		instant, err := externalexecutiontimestamp.ConvertWallClock(
			testCase.raw, testCase.offset,
		)
		NewWithT(t).Expect(err).NotTo(HaveOccurred())
		NewWithT(t).Expect(externalexecutiontimestamp.FormatInstant(instant)).
			To(Equal(testCase.want))
	}
	_, err := externalexecutiontimestamp.ConvertWallClock(
		"2026-07-15T12:00:00.000000", 64801,
	)
	NewWithT(t).Expect(err).To(MatchError(ContainSubstring("offset seconds")))
}

func TestDatabaseIdentityRejectsDuplicateIDs(t *testing.T) {
	id := uuid.New()
	_, err := externalexecutiontimestamp.ComputeDatabaseIdentityChecksum(
		137, []uuid.UUID{id, id}, nil, 10, "sha256:"+strings.Repeat("a", 64),
	)
	NewWithT(t).Expect(err).To(MatchError(ContainSubstring("duplicate execution id")))
}
