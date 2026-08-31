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

func TestEvidenceVerificationBindsVerificationModeAndKeyIdentityWithoutSchemaColumns(t *testing.T) {
	g := NewWithT(t)
	verification := boundedEvidenceVerificationFixture()

	g.Expect(verification.VerificationMode).To(Equal("keyless"))
	g.Expect(validateEvidenceVerification(verification)).To(Succeed())

	keyful := verification
	keyful.VerificationMode = "keyful"
	keyful.TrustRootID = "release-key-1"
	keyful.KeyID = "release-key-1"
	keyful.KeyFingerprint = "sha256:" + strings.Repeat("e", 64)
	keyful.SignerIssuer = keyfulEvidenceSignerIssuerPrefix + keyful.KeyID
	keyful.SignerIdentity = keyful.KeyFingerprint
	g.Expect(validateEvidenceVerification(keyful)).To(Succeed())
	g.Expect(sameEvidenceVerification(verification, keyful)).To(BeFalse())

	wrongKey := keyful
	wrongKey.KeyID = "release-key-2"
	g.Expect(validateEvidenceVerification(wrongKey)).To(HaveOccurred())

	wrongFingerprint := keyful
	wrongFingerprint.SignerIdentity = "sha256:" + strings.Repeat("f", 64)
	g.Expect(validateEvidenceVerification(wrongFingerprint)).To(HaveOccurred())
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
		VerificationMode:           "keyless",
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
