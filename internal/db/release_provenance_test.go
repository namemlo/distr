package db

import (
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestEvidenceVerificationRequiresBoundedSourceCommitAndBuildID(t *testing.T) {
	g := NewWithT(t)
	verification := boundedEvidenceVerificationFixture()

	g.Expect(validateEvidenceVerification(verification)).To(Succeed())

	withoutSourceCommit := verification
	withoutSourceCommit.SourceCommit = ""
	g.Expect(validateEvidenceVerification(withoutSourceCommit)).To(HaveOccurred())

	withoutBuildID := verification
	withoutBuildID.BuildID = ""
	g.Expect(validateEvidenceVerification(withoutBuildID)).To(HaveOccurred())

	oversizedBuildID := verification
	oversizedBuildID.BuildID = strings.Repeat("x", 1025)
	g.Expect(validateEvidenceVerification(oversizedBuildID)).To(HaveOccurred())
}

func TestSameEvidenceVerificationBindsSourceCommitAndBuildID(t *testing.T) {
	g := NewWithT(t)
	verification := boundedEvidenceVerificationFixture()

	changedSourceCommit := verification
	changedSourceCommit.SourceCommit = strings.Repeat("f", 40)
	g.Expect(sameEvidenceVerification(verification, changedSourceCommit)).To(BeFalse())

	changedBuildID := verification
	changedBuildID.BuildID = "build-43"
	g.Expect(sameEvidenceVerification(verification, changedBuildID)).To(BeFalse())
}

func boundedEvidenceVerificationFixture() types.EvidenceVerification {
	return types.EvidenceVerification{
		OrganizationID:             uuid.New(),
		ReleaseBundleID:            uuid.New(),
		ArtifactKey:                "service",
		Platform:                   "linux/amd64",
		ArtifactDigest:             "sha256:" + strings.Repeat("a", 64),
		EvidenceReference:          "oci://evidence/service",
		EvidenceDigest:             "sha256:" + strings.Repeat("b", 64),
		PolicyChecksum:             "sha256:" + strings.Repeat("c", 64),
		TrustRootID:                "root-1",
		PredicateType:              "https://slsa.dev/provenance/v1",
		BuilderID:                  "https://builder.example.invalid/worker",
		BuildID:                    "build-42",
		SourceURI:                  "git+https://code.example.invalid/platform/service",
		SourceCommit:               "0123456789abcdef0123456789abcdef01234567",
		BuildType:                  "https://build.example.invalid/types/container/v1",
		ExternalParametersChecksum: "sha256:" + strings.Repeat("d", 64),
		SignerIssuer:               "https://issuer.example.invalid",
		SignerIdentity:             "repo:platform/service:ref:refs/heads/main",
		VerifiedAt:                 time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
	}
}
