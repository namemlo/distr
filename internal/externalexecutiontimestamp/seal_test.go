package externalexecutiontimestamp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/externalexecutiontimestamp"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func validSealOptions() types.ExternalExecutionTimestampSealOptions {
	return types.ExternalExecutionTimestampSealOptions{
		AuthorIdentity:          "release-author@example.invalid",
		ReviewerIdentity:        "release-reviewer@example.invalid",
		EvidenceBundleReference: "evidence:bundle-42",
		EvidenceBundleChecksum:  "sha256:" + strings.Repeat("a", 64),
		TargetReleaseCommit:     strings.Repeat("b", 40),
		TargetImageDigest:       "sha256:" + strings.Repeat("c", 64),
	}
}

func reviewedDraftForSeal(t *testing.T) types.ExternalExecutionTimestampManifest {
	t.Helper()
	manifest := validDraftManifest(t)
	resolveFirstCell(t, &manifest)
	return manifest
}

func TestSealManifestRecomputesCanonicalContentAndApproves(t *testing.T) {
	reviewedDraft := reviewedDraftForSeal(t)
	staleDecisionChecksum := reviewedDraft.DecisionContentChecksum
	g := NewWithT(t)

	sealed, err := externalexecutiontimestamp.SealManifest(
		reviewedDraft,
		validSealOptions(),
		time.Date(2026, 7, 15, 10, 4, 5, 123456000,
			time.FixedZone("Asia/Bangkok", 7*60*60)),
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(sealed.State).To(Equal(types.ExternalExecutionTimestampManifestStateApproved))
	g.Expect(sealed.ApprovedAt).To(Equal("2026-07-15T03:04:05.123456Z"))
	g.Expect(sealed.AuthorIdentity).To(Equal("release-author@example.invalid"))
	g.Expect(sealed.ReviewerIdentity).To(Equal("release-reviewer@example.invalid"))
	g.Expect(sealed.DecisionContentChecksum).NotTo(Equal(staleDecisionChecksum))
	g.Expect(externalexecutiontimestamp.ValidateManifestDocument(sealed)).To(BeEmpty())
	g.Expect(reviewedDraft.State).To(Equal(types.ExternalExecutionTimestampManifestStateDraft))
}

func TestSealManifestCanonicalizesEquivalentConvertedInstant(t *testing.T) {
	reviewedDraft := reviewedDraftForSeal(t)
	nonCanonical := "2026-07-15T10:00:00.000000+07:00"
	reviewedDraft.Cells[0].ConvertedValue = &nonCanonical

	sealed, err := externalexecutiontimestamp.SealManifest(
		reviewedDraft, validSealOptions(), time.Now(),
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(sealed.Cells[0].ConvertedValue).NotTo(BeNil())
	g.Expect(*sealed.Cells[0].ConvertedValue).To(Equal(
		"2026-07-15T03:00:00.000000Z",
	))
	g.Expect(reviewedDraft.Cells[0].ConvertedValue).NotTo(BeNil())
	g.Expect(*reviewedDraft.Cells[0].ConvertedValue).To(Equal(nonCanonical))
	g.Expect(externalexecutiontimestamp.ValidateManifestDocument(sealed)).To(BeEmpty())
}

func TestSealManifestDeepCopiesEveryPointerField(t *testing.T) {
	reviewedDraft := reviewedDraftForSeal(t)
	supersedesManifestID := uuid.MustParse("99999999-9999-4999-8999-999999999999")
	reviewedDraft.SupersedesManifestID = &supersedesManifestID
	originalSupersedesManifestID := supersedesManifestID
	originalRawValue := *reviewedDraft.Cells[0].RawValue
	originalOffset := *reviewedDraft.Cells[0].SourceOffsetSeconds
	originalConvertedValue := *reviewedDraft.Cells[0].ConvertedValue

	sealed, err := externalexecutiontimestamp.SealManifest(
		reviewedDraft, validSealOptions(), time.Now(),
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(sealed.SupersedesManifestID).NotTo(BeIdenticalTo(
		reviewedDraft.SupersedesManifestID,
	))
	for index := range reviewedDraft.Cells {
		if reviewedDraft.Cells[index].RawValue != nil {
			g.Expect(sealed.Cells[index].RawValue).NotTo(BeIdenticalTo(
				reviewedDraft.Cells[index].RawValue,
			))
		}
		if reviewedDraft.Cells[index].SourceOffsetSeconds != nil {
			g.Expect(sealed.Cells[index].SourceOffsetSeconds).NotTo(BeIdenticalTo(
				reviewedDraft.Cells[index].SourceOffsetSeconds,
			))
		}
		if reviewedDraft.Cells[index].ConvertedValue != nil {
			g.Expect(sealed.Cells[index].ConvertedValue).NotTo(BeIdenticalTo(
				reviewedDraft.Cells[index].ConvertedValue,
			))
		}
	}

	changedSupersedesID := uuid.MustParse("88888888-8888-4888-8888-888888888888")
	*reviewedDraft.SupersedesManifestID = changedSupersedesID
	*reviewedDraft.Cells[0].RawValue = "2030-01-01T00:00:00.000000"
	*reviewedDraft.Cells[0].SourceOffsetSeconds = 0
	*reviewedDraft.Cells[0].ConvertedValue = "2030-01-01T00:00:00.000000Z"
	g.Expect(*sealed.SupersedesManifestID).To(Equal(originalSupersedesManifestID))
	g.Expect(*sealed.Cells[0].RawValue).To(Equal(originalRawValue))
	g.Expect(*sealed.Cells[0].SourceOffsetSeconds).To(Equal(originalOffset))
	g.Expect(*sealed.Cells[0].ConvertedValue).To(Equal(originalConvertedValue))

	sealedSupersedesID := uuid.MustParse("77777777-7777-4777-8777-777777777777")
	*sealed.SupersedesManifestID = sealedSupersedesID
	*sealed.Cells[0].RawValue = "2040-01-01T00:00:00.000000"
	*sealed.Cells[0].SourceOffsetSeconds = -3600
	*sealed.Cells[0].ConvertedValue = "2040-01-01T01:00:00.000000Z"
	g.Expect(*reviewedDraft.SupersedesManifestID).To(Equal(changedSupersedesID))
	g.Expect(*reviewedDraft.Cells[0].RawValue).To(Equal(
		"2030-01-01T00:00:00.000000",
	))
	g.Expect(*reviewedDraft.Cells[0].SourceOffsetSeconds).To(Equal(int32(0)))
	g.Expect(*reviewedDraft.Cells[0].ConvertedValue).To(Equal(
		"2030-01-01T00:00:00.000000Z",
	))
}

func TestSealManifestPreservesNonNilEmptyCells(t *testing.T) {
	manifest := validDraftManifest(t)
	manifest.Cells = make([]types.ExternalExecutionTimestampCellDecision, 0)
	refreshManifestSnapshotChecksums(t, &manifest)

	sealed, err := externalexecutiontimestamp.SealManifest(
		manifest, validSealOptions(), time.Now(),
	)

	g := NewWithT(t)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(sealed.Cells).NotTo(BeNil())
	g.Expect(sealed.Cells).To(BeEmpty())
	g.Expect(manifest.Cells).NotTo(BeNil())
}

func TestSealManifestRejectsInvalidCanonicalDataAndConversions(t *testing.T) {
	wrongChecksum := "sha256:" + strings.Repeat("f", 64)
	tests := []struct {
		name   string
		mutate func(*types.ExternalExecutionTimestampManifest)
		want   string
	}{
		{
			name: "raw cell checksum",
			mutate: func(manifest *types.ExternalExecutionTimestampManifest) {
				manifest.Cells[0].RawCellChecksum = wrongChecksum
			},
			want: "raw cell checksum",
		},
		{
			name: "raw set checksum",
			mutate: func(manifest *types.ExternalExecutionTimestampManifest) {
				manifest.RawCellChecksum = wrongChecksum
			},
			want: "raw cell checksum",
		},
		{
			name: "database identity checksum",
			mutate: func(manifest *types.ExternalExecutionTimestampManifest) {
				manifest.DatabaseIdentityChecksum = wrongChecksum
			},
			want: "database identity checksum",
		},
		{
			name: "converted value",
			mutate: func(manifest *types.ExternalExecutionTimestampManifest) {
				manifest.Cells[0].ConvertedValue = stringPointer("2026-07-15T00:00:00.000000Z")
			},
			want: "converted value does not reproduce",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := reviewedDraftForSeal(t)
			test.mutate(&manifest)
			_, err := externalexecutiontimestamp.SealManifest(
				manifest, validSealOptions(), time.Now(),
			)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(test.want)))
		})
	}
}

func TestSealManifestRequiresDistinctCompleteApprovalEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*types.ExternalExecutionTimestampSealOptions)
		want   string
	}{
		{name: "author", mutate: func(options *types.ExternalExecutionTimestampSealOptions) {
			options.AuthorIdentity = " "
		}, want: "author identity is required"},
		{name: "reviewer", mutate: func(options *types.ExternalExecutionTimestampSealOptions) {
			options.ReviewerIdentity = ""
		}, want: "reviewer identity is required"},
		{name: "distinct identities", mutate: func(options *types.ExternalExecutionTimestampSealOptions) {
			options.ReviewerIdentity = options.AuthorIdentity
		}, want: "author and reviewer identities must differ"},
		{name: "evidence reference", mutate: func(options *types.ExternalExecutionTimestampSealOptions) {
			options.EvidenceBundleReference = ""
		}, want: "evidence bundle reference is required"},
		{name: "evidence checksum", mutate: func(options *types.ExternalExecutionTimestampSealOptions) {
			options.EvidenceBundleChecksum = ""
		}, want: "evidence bundle checksum"},
		{name: "target commit", mutate: func(options *types.ExternalExecutionTimestampSealOptions) {
			options.TargetReleaseCommit = ""
		}, want: "target release commit"},
		{name: "target image digest", mutate: func(options *types.ExternalExecutionTimestampSealOptions) {
			options.TargetImageDigest = ""
		}, want: "target image digest"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := validSealOptions()
			test.mutate(&options)
			_, err := externalexecutiontimestamp.SealManifest(
				reviewedDraftForSeal(t), options, time.Now(),
			)
			NewWithT(t).Expect(err).To(MatchError(ContainSubstring(test.want)))
		})
	}
}

func TestSealManifestRejectsNonDraftInput(t *testing.T) {
	manifest := reviewedDraftForSeal(t)
	manifest.State = types.ExternalExecutionTimestampManifestStateApproved
	_, err := externalexecutiontimestamp.SealManifest(
		manifest, validSealOptions(), time.Now(),
	)
	NewWithT(t).Expect(err).To(MatchError(ContainSubstring(
		"only DRAFT manifests can be sealed",
	)))
}
