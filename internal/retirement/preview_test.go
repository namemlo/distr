package retirement_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/retirement"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

var (
	organizationID  = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	requesterID     = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	subjectID       = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	secondSubjectID = uuid.MustParse("44444444-4444-4444-8444-444444444444")
)

func TestPreviewSampleRetirementRejectsNonExactSelection(t *testing.T) {
	base := validRequest()
	olderThan := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*types.SampleRetirementRequest)
		want   string
	}{
		{
			name: "empty allowlist cannot act as age-only cleanup",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Items = nil
			},
			want: "exact UUID allowlist",
		},
		{
			name: "wildcard selector",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Selector.Wildcard = "*"
			},
			want: "wildcard",
		},
		{
			name: "name pattern selector",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Selector.NamePattern = "demo-*"
			},
			want: "name pattern",
		},
		{
			name: "age-only selector",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Selector.OlderThan = &olderThan
			},
			want: "age",
		},
		{
			name: "nil subject UUID",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Items[0].SubjectID = uuid.Nil
			},
			want: "exact UUID",
		},
		{
			name: "duplicate subject UUID",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Items = append(request.Items, request.Items[0])
			},
			want: "duplicate",
		},
		{
			name: "wildcard ownership marker",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Items[0].OwnershipMarker = "tutorial-*"
			},
			want: "ownership marker",
		},
		{
			name: "unknown subject type",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Items[0].SubjectType = "release*"
			},
			want: "subject type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			request := base
			request.Items = append([]types.SampleRetirementSubject(nil), base.Items...)
			test.mutate(&request)
			store := newPreviewStore(request)

			preview, err := retirement.PreviewSampleRetirement(
				context.Background(),
				store,
				request,
			)

			g.Expect(err).To(MatchError(ContainSubstring(test.want)))
			g.Expect(preview).To(BeNil())
			g.Expect(store.inspectCalls).To(Equal(0))
			g.Expect(store.saved).To(BeNil())
		})
	}
}

func TestPreviewSampleRetirementRequiresImmutableBackupRestoreAndOwnershipProof(t *testing.T) {
	base := validRequest()
	tests := []struct {
		name   string
		mutate func(*types.SampleRetirementRequest)
		want   string
	}{
		{
			name: "organization",
			mutate: func(request *types.SampleRetirementRequest) {
				request.OrganizationID = uuid.Nil
			},
			want: "organization",
		},
		{
			name: "requester",
			mutate: func(request *types.SampleRetirementRequest) {
				request.RequestedByUserAccountID = uuid.Nil
			},
			want: "requester",
		},
		{
			name: "backup reference",
			mutate: func(request *types.SampleRetirementRequest) {
				request.BackupReference = ""
			},
			want: "backup reference",
		},
		{
			name: "backup checksum",
			mutate: func(request *types.SampleRetirementRequest) {
				request.BackupChecksum = "latest"
			},
			want: "backup checksum",
		},
		{
			name: "restore proof reference",
			mutate: func(request *types.SampleRetirementRequest) {
				request.RestoreProofReference = ""
			},
			want: "restore proof reference",
		},
		{
			name: "restore proof checksum",
			mutate: func(request *types.SampleRetirementRequest) {
				request.RestoreProofChecksum = "sha256:ABC"
			},
			want: "restore proof checksum",
		},
		{
			name: "ownership marker",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Items[0].OwnershipMarker = ""
			},
			want: "ownership marker",
		},
		{
			name: "ownership checksum",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Items[0].OwnershipChecksum = ""
			},
			want: "ownership checksum",
		},
		{
			name: "ownership checksum does not bind the exact marker",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Items[0].OwnershipChecksum = checksum("e")
			},
			want: "ownership checksum",
		},
		{
			name: "expected checksum",
			mutate: func(request *types.SampleRetirementRequest) {
				request.Items[0].ExpectedChecksum = ""
			},
			want: "expected checksum",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			request := base
			request.Items = append([]types.SampleRetirementSubject(nil), base.Items...)
			test.mutate(&request)
			store := newPreviewStore(request)

			preview, err := retirement.PreviewSampleRetirement(
				context.Background(),
				store,
				request,
			)

			g.Expect(err).To(MatchError(ContainSubstring(test.want)))
			g.Expect(preview).To(BeNil())
			g.Expect(store.inspectCalls).To(Equal(0))
			g.Expect(store.saved).To(BeNil())
		})
	}
}

func TestPreviewSampleRetirementAcceptsVersionedEvidenceReferences(t *testing.T) {
	g := NewWithT(t)
	request := validRequest()
	request.BackupReference = "s3://evidence/backup.dump?versionId=abc%2Fdef"
	request.RestoreProofReference = "https://evidence.example/restore/42?digest=sha256%3Aabc"
	store := newPreviewStore(request)

	preview, err := retirement.PreviewSampleRetirement(
		context.Background(),
		store,
		request,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.Job.BackupReference).To(Equal(request.BackupReference))
	g.Expect(preview.Job.RestoreProofReference).To(Equal(request.RestoreProofReference))
}

func TestPreviewSampleRetirementRejectsStaleOrChangedOwnershipFacts(t *testing.T) {
	base := validRequest()
	tests := []struct {
		name   string
		mutate func(*previewStore)
		want   string
	}{
		{
			name: "missing current subject",
			mutate: func(store *previewStore) {
				store.current = nil
			},
			want: "exact allowlist",
		},
		{
			name: "unexpected current subject",
			mutate: func(store *previewStore) {
				extra := store.current[0]
				extra.Subject.SubjectID = secondSubjectID
				store.current = append(store.current, extra)
			},
			want: "exact allowlist",
		},
		{
			name: "stale subject checksum",
			mutate: func(store *previewStore) {
				store.current[0].CurrentChecksum = checksum("e")
			},
			want: "stale",
		},
		{
			name: "changed ownership marker",
			mutate: func(store *previewStore) {
				store.current[0].OwnershipMarker = "different-owner"
			},
			want: "ownership marker",
		},
		{
			name: "changed ownership checksum",
			mutate: func(store *previewStore) {
				store.current[0].OwnershipChecksum = checksum("f")
			},
			want: "ownership checksum",
		},
		{
			name: "duplicate current subject",
			mutate: func(store *previewStore) {
				store.current = append(store.current, store.current[0])
			},
			want: "duplicate current",
		},
		{
			name: "cross organization subject",
			mutate: func(store *previewStore) {
				store.current[0].OrganizationID = secondSubjectID
			},
			want: "cross-organization",
		},
		{
			name: "mutable subject",
			mutate: func(store *previewStore) {
				store.current[0].Immutable = false
			},
			want: "mutable",
		},
		{
			name: "invalid before count",
			mutate: func(store *previewStore) {
				store.current[0].BeforeCount = 2
			},
			want: "before count",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			store := newPreviewStore(base)
			test.mutate(store)

			preview, err := retirement.PreviewSampleRetirement(
				context.Background(),
				store,
				base,
			)

			g.Expect(err).To(MatchError(ContainSubstring(test.want)))
			g.Expect(preview).To(BeNil())
			g.Expect(store.saved).To(BeNil())
		})
	}
}

func TestPreviewSampleRetirementRejectsProtectedAndCrossOrganizationReferences(t *testing.T) {
	base := validRequest()
	otherOrganizationID := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	tests := []struct {
		name   string
		mutate func(*previewStore)
		want   string
	}{
		{
			name: "protected reference flag",
			mutate: func(store *previewStore) {
				store.reports[0].References = []types.RetirementReference{{
					SourceType:     "deployment_plan",
					SourceID:       secondSubjectID,
					Relationship:   "previous_known_good",
					OrganizationID: organizationID,
					Protected:      true,
				}}
			},
			want: "protected reverse reference",
		},
		{
			name: "protected reference count",
			mutate: func(store *previewStore) {
				store.reports[0].ProtectedReferenceCount = 1
			},
			want: "protected reverse reference",
		},
		{
			name: "cross organization reference",
			mutate: func(store *previewStore) {
				store.reports[0].References = []types.RetirementReference{{
					SourceType:     "application",
					SourceID:       secondSubjectID,
					Relationship:   "owns",
					OrganizationID: otherOrganizationID,
				}}
			},
			want: "cross-organization",
		},
		{
			name: "cross organization count",
			mutate: func(store *previewStore) {
				store.reports[0].CrossOrganizationReferenceCount = 1
			},
			want: "cross-organization",
		},
		{
			name: "repository says subject is not retirable",
			mutate: func(store *previewStore) {
				store.reports[0].Retirable = false
				store.reports[0].BlockingReasons = []string{"release history"}
			},
			want: "not retirable",
		},
		{
			name: "missing reference report",
			mutate: func(store *previewStore) {
				store.reports = nil
			},
			want: "reference report",
		},
		{
			name: "report for a different subject",
			mutate: func(store *previewStore) {
				store.reports[0].Subject.SubjectID = secondSubjectID
			},
			want: "reference report",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			store := newPreviewStore(base)
			test.mutate(store)

			preview, err := retirement.PreviewSampleRetirement(
				context.Background(),
				store,
				base,
			)

			g.Expect(err).To(MatchError(ContainSubstring(test.want)))
			g.Expect(preview).To(BeNil())
			g.Expect(store.saved).To(BeNil())
		})
	}
}

func TestPreviewSampleRetirementBuildsDeterministicImmutableChecksum(t *testing.T) {
	g := NewWithT(t)
	firstSubject := validSubject()
	secondSubject := validSubject()
	secondSubject.SubjectType = types.SampleRetirementSubjectDeploymentTarget
	secondSubject.SubjectID = secondSubjectID
	secondSubject.OwnershipMarker = "tutorial-target"
	secondSubject.OwnershipChecksum = textChecksum("tutorial-target")
	secondSubject.ExpectedChecksum = checksum("f")

	firstRequest := validRequest()
	firstRequest.Items = []types.SampleRetirementSubject{secondSubject, firstSubject}
	firstStore := newPreviewStore(firstRequest)
	firstStore.current = []types.SampleRetirementCandidate{
		validCandidate(firstSubject),
		validCandidate(secondSubject),
	}
	firstStore.reports = []types.ReferenceReport{
		validReport(secondSubject),
		validReport(firstSubject),
	}
	firstInputOrder := append([]types.SampleRetirementSubject(nil), firstRequest.Items...)

	first, err := retirement.PreviewSampleRetirement(
		context.Background(),
		firstStore,
		firstRequest,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first.PreviewChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(first.Job.AllowlistChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(first.PreviewChecksum).To(Equal(first.Job.PreviewChecksum))
	g.Expect(first.Job.State).To(Equal(types.SampleRetirementJobPreviewed))
	g.Expect(first.Job.ApprovalID).To(BeNil())
	g.Expect(first.Job.ApprovalChecksum).To(BeNil())
	g.Expect(first.RequestedCount).To(Equal(2))
	g.Expect(first.RetirableCount).To(Equal(2))
	g.Expect(first.BlockedCount).To(Equal(0))
	g.Expect(first.AuditEventCount).To(Equal(4))
	g.Expect(first.Items).To(HaveLen(2))
	g.Expect(first.Items[0].SubjectID).To(Equal(subjectID))
	g.Expect(first.Items[0].Ordinal).To(Equal(1))
	g.Expect(first.Items[1].SubjectID).To(Equal(secondSubjectID))
	g.Expect(first.Items[1].Ordinal).To(Equal(2))
	g.Expect(firstRequest.Items).To(Equal(firstInputOrder))
	g.Expect(firstStore.saved).NotTo(BeNil())
	g.Expect(firstStore.saved).NotTo(BeIdenticalTo(first))

	secondRequest := firstRequest
	secondRequest.Items = []types.SampleRetirementSubject{firstSubject, secondSubject}
	secondStore := newPreviewStore(secondRequest)
	secondStore.current = []types.SampleRetirementCandidate{
		validCandidate(secondSubject),
		validCandidate(firstSubject),
	}
	secondStore.reports = []types.ReferenceReport{
		validReport(firstSubject),
		validReport(secondSubject),
	}
	secondStore.reports[0].References = nil
	secondStore.reports[0].BlockingReasons = nil
	secondStore.reports[1].References = nil
	secondStore.reports[1].BlockingReasons = nil
	second, err := retirement.PreviewSampleRetirement(
		context.Background(),
		secondStore,
		secondRequest,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second.PreviewChecksum).To(Equal(first.PreviewChecksum))
	g.Expect(second.Job.AllowlistChecksum).To(Equal(first.Job.AllowlistChecksum))

	mutations := []struct {
		name   string
		mutate func(*types.SampleRetirementRequest)
	}{
		{"backup reference", func(request *types.SampleRetirementRequest) {
			request.BackupReference = "backup://immutable/other"
		}},
		{"backup checksum", func(request *types.SampleRetirementRequest) {
			request.BackupChecksum = checksum("9")
		}},
		{"restore proof reference", func(request *types.SampleRetirementRequest) {
			request.RestoreProofReference = "restore-proof://isolated/other"
		}},
		{"restore proof checksum", func(request *types.SampleRetirementRequest) {
			request.RestoreProofChecksum = checksum("8")
		}},
		{"requester", func(request *types.SampleRetirementRequest) {
			request.RequestedByUserAccountID = secondSubjectID
		}},
		{"ownership", func(request *types.SampleRetirementRequest) {
			request.Items[0].OwnershipMarker = "tutorial-app-other"
			request.Items[0].OwnershipChecksum = textChecksum("tutorial-app-other")
		}},
	}
	for _, mutation := range mutations {
		t.Run("checksum binds "+mutation.name, func(t *testing.T) {
			mutated := secondRequest
			mutated.Items = append(
				[]types.SampleRetirementSubject(nil),
				secondRequest.Items...,
			)
			mutation.mutate(&mutated)
			store := newPreviewStore(mutated)

			preview, previewErr := retirement.PreviewSampleRetirement(
				context.Background(),
				store,
				mutated,
			)

			NewWithT(t).Expect(previewErr).NotTo(HaveOccurred())
			NewWithT(t).Expect(preview.PreviewChecksum).
				NotTo(Equal(first.PreviewChecksum))
		})
	}
}

func TestPreviewSampleRetirementDoesNotPersistDependencyFailures(t *testing.T) {
	base := validRequest()
	tests := []struct {
		name   string
		mutate func(*previewStore)
		want   string
	}{
		{
			name: "inspection",
			mutate: func(store *previewStore) {
				store.inspectErr = errors.New("inspect unavailable")
			},
			want: "inspect unavailable",
		},
		{
			name: "reverse reference verification",
			mutate: func(store *previewStore) {
				store.referencesErr = errors.New("reference unavailable")
			},
			want: "reference unavailable",
		},
		{
			name: "persistence",
			mutate: func(store *previewStore) {
				store.saveErr = errors.New("save unavailable")
			},
			want: "save unavailable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			store := newPreviewStore(base)
			test.mutate(store)

			preview, err := retirement.PreviewSampleRetirement(
				context.Background(),
				store,
				base,
			)

			g.Expect(err).To(MatchError(ContainSubstring(test.want)))
			g.Expect(preview).To(BeNil())
			if test.name != "persistence" {
				g.Expect(store.saved).To(BeNil())
			}
		})
	}
}

type previewStore struct {
	current       []types.SampleRetirementCandidate
	reports       []types.ReferenceReport
	inspectErr    error
	referencesErr error
	saveErr       error
	inspectCalls  int
	saved         *types.SampleRetirementPreview
}

func newPreviewStore(request types.SampleRetirementRequest) *previewStore {
	current := make([]types.SampleRetirementCandidate, 0, len(request.Items))
	reports := make([]types.ReferenceReport, 0, len(current))
	for _, subject := range request.Items {
		current = append(current, validCandidate(subject))
		reports = append(reports, validReport(subject))
	}
	return &previewStore{current: current, reports: reports}
}

func (store *previewStore) InspectSampleRetirementSubjects(
	_ context.Context,
	_ uuid.UUID,
	_ []types.SampleRetirementSubject,
) ([]types.SampleRetirementCandidate, error) {
	store.inspectCalls++
	return append([]types.SampleRetirementCandidate(nil), store.current...), store.inspectErr
}

func (store *previewStore) VerifyRetirementReverseReferences(
	_ context.Context,
	_ uuid.UUID,
	_ []types.SampleRetirementSubject,
) ([]types.ReferenceReport, error) {
	return append([]types.ReferenceReport(nil), store.reports...), store.referencesErr
}

func (store *previewStore) SaveSampleRetirementPreview(
	_ context.Context,
	preview *types.SampleRetirementPreview,
) error {
	if store.saveErr != nil {
		return store.saveErr
	}
	copy := *preview
	copy.Items = append([]types.SampleRetirementItem(nil), preview.Items...)
	copy.ReferenceReports = append(
		[]types.ReferenceReport(nil),
		preview.ReferenceReports...,
	)
	store.saved = &copy
	return nil
}

func validRequest() types.SampleRetirementRequest {
	return types.SampleRetirementRequest{
		OrganizationID:           organizationID,
		RequestedByUserAccountID: requesterID,
		BackupReference:          "backup://immutable/2026-07-28",
		BackupChecksum:           checksum("a"),
		RestoreProofReference:    "restore-proof://isolated/2026-07-28",
		RestoreProofChecksum:     checksum("b"),
		Items:                    []types.SampleRetirementSubject{validSubject()},
	}
}

func validSubject() types.SampleRetirementSubject {
	return types.SampleRetirementSubject{
		SubjectType:       types.SampleRetirementSubjectApplication,
		SubjectID:         subjectID,
		OwnershipMarker:   "tutorial-app",
		OwnershipChecksum: textChecksum("tutorial-app"),
		ExpectedChecksum:  checksum("d"),
	}
}

func validReport(subject types.SampleRetirementSubject) types.ReferenceReport {
	return types.ReferenceReport{
		Subject:               subject,
		SubjectOrganizationID: organizationID,
		CurrentChecksum:       subject.ExpectedChecksum,
		BeforeCount:           1,
		Immutable:             true,
		Retirable:             true,
		AuditEventCount:       2,
		References:            []types.RetirementReference{},
		BlockingReasons:       []string{},
	}
}

func validCandidate(subject types.SampleRetirementSubject) types.SampleRetirementCandidate {
	return types.SampleRetirementCandidate{
		Subject:           subject,
		OrganizationID:    organizationID,
		CurrentChecksum:   subject.ExpectedChecksum,
		OwnershipMarker:   subject.OwnershipMarker,
		OwnershipChecksum: subject.OwnershipChecksum,
		BeforeCount:       1,
		Immutable:         true,
	}
}

func checksum(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func textChecksum(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
