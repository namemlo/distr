package protectedhistory

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/pilotexception"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestBuildRetentionLabelsApprovedSingleReviewerPilot(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	artifact := mustBuild(t, nil)
	payload, err := Marshal(*artifact)
	g.Expect(err).NotTo(HaveOccurred())
	checksum := ContentChecksum(payload)
	actorID := uuid.MustParse("66666666-6666-4666-8666-666666666666")

	retained, err := BuildRetention(RetentionInput{
		ID: uuid.New(), Artifact: *artifact,
		ObjectReference: immutableProtectedHistoryReference(checksum),
		MediaType:       ArtifactMediaTypeV1, ByteLength: int64(len(payload)),
		ContentChecksum:     checksum,
		CapturedAt:          time.Date(2026, time.September, 3, 7, 8, 9, 0, time.UTC),
		IssuerUserAccountID: actorID, ReviewerUserAccountID: actorID,
		GovernanceException: &pilotexception.Evidence{
			Key:               pilotexception.Key,
			ApprovalReference: "owner-approval:adopter-dev-20260903",
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(retained.GovernanceExceptionKey).To(Equal(pilotexception.Key))
	g.Expect(retained.GovernanceExceptionReference).To(Equal(
		"owner-approval:adopter-dev-20260903",
	))
	g.Expect(retained.RetentionChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
}

func TestBuildRetentionSealsArtifactObjectReviewAndCaptureIdentity(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	artifact := mustBuild(t, nil)
	payload, err := Marshal(*artifact)
	g.Expect(err).NotTo(HaveOccurred())
	contentChecksum := ContentChecksum(payload)
	capturedAt := time.Date(2026, time.September, 1, 7, 8, 9, 123456000, time.UTC)
	issuerID := uuid.MustParse("66666666-6666-4666-8666-666666666666")
	reviewerID := uuid.MustParse("77777777-7777-4777-8777-777777777777")

	retained, err := BuildRetention(RetentionInput{
		ID:                    uuid.MustParse("88888888-8888-4888-8888-888888888888"),
		Artifact:              *artifact,
		ObjectReference:       immutableProtectedHistoryReference(contentChecksum),
		MediaType:             ArtifactMediaTypeV1,
		ByteLength:            int64(len(payload)),
		ContentChecksum:       contentChecksum,
		CapturedAt:            capturedAt,
		IssuerUserAccountID:   issuerID,
		ReviewerUserAccountID: reviewerID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(retained.Schema).To(Equal(RetentionSchemaV1))
	g.Expect(retained.ArtifactID).To(Equal(artifact.ArtifactID))
	g.Expect(retained.RecordsRoot).To(Equal(artifact.RecordsRoot))
	g.Expect(retained.Scope).To(Equal(artifact.Scope))
	g.Expect(retained.CapturedAt).To(Equal(capturedAt))
	g.Expect(retained.RetentionChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(ValidateRetention(*retained)).To(Succeed())

	rebuilt, err := BuildRetention(RetentionInput{
		ID: retained.ID, Artifact: *artifact,
		ObjectReference: retained.ObjectReference, MediaType: retained.MediaType,
		ByteLength: retained.ByteLength, ContentChecksum: retained.ContentChecksum,
		CapturedAt: retained.CapturedAt, IssuerUserAccountID: issuerID,
		ReviewerUserAccountID: reviewerID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rebuilt.RetentionChecksum).To(Equal(retained.RetentionChecksum))
}

func TestRetentionChecksumChangesForEveryMaterialIdentity(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	artifact := mustBuild(t, nil)
	payload, err := Marshal(*artifact)
	g.Expect(err).NotTo(HaveOccurred())
	checksum := ContentChecksum(payload)
	base := RetentionInput{
		ID: uuid.New(), Artifact: *artifact,
		ObjectReference: immutableProtectedHistoryReference(checksum),
		MediaType:       ArtifactMediaTypeV1, ByteLength: int64(len(payload)),
		ContentChecksum:       checksum,
		CapturedAt:            time.Date(2026, time.September, 1, 7, 8, 9, 0, time.UTC),
		IssuerUserAccountID:   uuid.MustParse("66666666-6666-4666-8666-666666666666"),
		ReviewerUserAccountID: uuid.MustParse("77777777-7777-4777-8777-777777777777"),
	}
	baseline, err := BuildRetention(base)
	g.Expect(err).NotTo(HaveOccurred())

	mutations := map[string]func(*RetentionInput){
		"artifact identity": func(input *RetentionInput) {
			input.Artifact.ArtifactID = checksumOfString("different-artifact")
		},
		"object reference": func(input *RetentionInput) {
			input.ObjectReference = strings.Replace(input.ObjectReference, "history.json", "copy.json", 1)
		},
		"media type":  func(input *RetentionInput) { input.MediaType = "application/json" },
		"byte length": func(input *RetentionInput) { input.ByteLength++ },
		"content checksum": func(input *RetentionInput) {
			input.ContentChecksum = checksumOfString("different-content")
			input.ObjectReference = immutableProtectedHistoryReference(input.ContentChecksum)
		},
		"capture time": func(input *RetentionInput) { input.CapturedAt = input.CapturedAt.Add(time.Second) },
		"issuer":       func(input *RetentionInput) { input.IssuerUserAccountID = uuid.New() },
		"reviewer":     func(input *RetentionInput) { input.ReviewerUserAccountID = uuid.New() },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			retained, buildErr := BuildRetention(candidate)
			if name == "artifact identity" || name == "media type" || name == "byte length" ||
				name == "content checksum" {
				NewWithT(t).Expect(buildErr).To(HaveOccurred())
				return
			}
			NewWithT(t).Expect(buildErr).NotTo(HaveOccurred())
			NewWithT(t).Expect(retained.RetentionChecksum).NotTo(Equal(baseline.RetentionChecksum))
		})
	}
}

func TestBuildRetentionRejectsUnreviewedOrMutableMaterial(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	artifact := mustBuild(t, nil)
	payload, err := Marshal(*artifact)
	g.Expect(err).NotTo(HaveOccurred())
	checksum := ContentChecksum(payload)
	base := RetentionInput{
		ID: uuid.New(), Artifact: *artifact,
		ObjectReference: immutableProtectedHistoryReference(checksum),
		MediaType:       ArtifactMediaTypeV1, ByteLength: int64(len(payload)),
		ContentChecksum:       checksum,
		CapturedAt:            time.Date(2026, time.September, 1, 7, 8, 9, 0, time.UTC),
		IssuerUserAccountID:   uuid.MustParse("66666666-6666-4666-8666-666666666666"),
		ReviewerUserAccountID: uuid.MustParse("77777777-7777-4777-8777-777777777777"),
	}

	tests := map[string]func(*RetentionInput){
		"same issuer and reviewer": func(input *RetentionInput) {
			input.ReviewerUserAccountID = input.IssuerUserAccountID
		},
		"mutable object reference": func(input *RetentionInput) {
			input.ObjectReference = "s3://history/latest/history.json"
		},
		"wrong media type": func(input *RetentionInput) { input.MediaType = "application/json" },
		"empty artifact":   func(input *RetentionInput) { input.ByteLength = 0 },
		"non UTC capture": func(input *RetentionInput) {
			input.CapturedAt = input.CapturedAt.In(time.FixedZone("offset", 3600))
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			_, buildErr := BuildRetention(candidate)
			NewWithT(t).Expect(buildErr).To(HaveOccurred())
		})
	}
}

func TestBindRetentionAuditSealsAppendOnlyAuditIdentity(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	artifact := mustBuild(t, nil)
	payload, err := Marshal(*artifact)
	g.Expect(err).NotTo(HaveOccurred())
	checksum := ContentChecksum(payload)
	retained, err := BuildRetention(RetentionInput{
		ID: uuid.New(), Artifact: *artifact,
		ObjectReference: immutableProtectedHistoryReference(checksum),
		MediaType:       ArtifactMediaTypeV1, ByteLength: int64(len(payload)),
		ContentChecksum:       checksum,
		CapturedAt:            time.Date(2026, time.September, 1, 7, 8, 9, 0, time.UTC),
		IssuerUserAccountID:   uuid.MustParse("66666666-6666-4666-8666-666666666666"),
		ReviewerUserAccountID: uuid.MustParse("77777777-7777-4777-8777-777777777777"),
	})
	g.Expect(err).NotTo(HaveOccurred())
	eventID := uuid.New()
	g.Expect(BindRetentionAudit(retained, eventID, 42)).To(Succeed())
	g.Expect(retained.AuditEventID).To(Equal(eventID))
	g.Expect(retained.AuditEventSequence).To(Equal(int64(42)))
	g.Expect(retained.AuditBindingChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))
	g.Expect(ValidateRetention(*retained)).To(Succeed())

	tampered := *retained
	tampered.AuditEventSequence++
	g.Expect(ValidateRetention(tampered)).To(MatchError(ContainSubstring("audit binding checksum")))
}

func immutableProtectedHistoryReference(checksum string) string {
	return "s3://history/_immutable/sha256/" + strings.TrimPrefix(checksum, "sha256:") + "/history.json"
}

func checksumOfString(value string) string {
	return ContentChecksum([]byte(value))
}
