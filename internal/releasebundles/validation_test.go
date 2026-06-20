package releasebundles

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestValidateBundleContentRejectsChecksumAndComponentProblems(t *testing.T) {
	g := NewWithT(t)
	bundle := types.ReleaseBundle{
		ReleaseNumber:     "1.2.3",
		CanonicalChecksum: "sha256:not-the-real-checksum",
		CanonicalPayload:  []byte("{}"),
		Components: []types.ReleaseBundleComponent{
			{
				Key:        "image",
				Type:       types.ReleaseBundleComponentTypeOCIImage,
				Version:    "1.2.3",
				PackageRef: "registry.example/api",
			},
			{
				Key:        "manual",
				Type:       types.ReleaseBundleComponentTypeExternalArtifact,
				Version:    "1.2.3",
				PackageRef: "https://example.com/manual.zip",
			},
		},
	}

	result := ValidateBundleContent(bundle)

	g.Expect(result.Valid).To(BeFalse())
	g.Expect(result.Errors).To(ContainElements(
		ValidationIssue{
			Field:   "canonicalChecksum",
			Rule:    "sha256",
			Message: "canonical payload does not match checksum",
		},
		ValidationIssue{
			Field:   "components.image.digest",
			Rule:    "sha256",
			Message: "OCI component digest must be a sha256 digest",
		},
		ValidationIssue{
			Field:   "components.manual.checksum",
			Rule:    "required",
			Message: "external artifact checksum is required",
		},
	))
}

func TestValidateBundleContentAcceptsCompleteBundle(t *testing.T) {
	g := NewWithT(t)
	bundle := types.ReleaseBundle{
		ReleaseNumber: "1.2.3",
		Components: []types.ReleaseBundleComponent{
			{
				Key:        "image",
				Type:       types.ReleaseBundleComponentTypeOCIImage,
				Version:    "1.2.3",
				PackageRef: "registry.example/api",
				Digest:     "sha256:abcdef",
			},
		},
	}
	payload, checksum, err := Canonicalize(bundle)
	g.Expect(err).NotTo(HaveOccurred())
	bundle.CanonicalPayload = payload
	bundle.CanonicalChecksum = checksum

	result := ValidateBundleContent(bundle)

	g.Expect(result.Valid).To(BeTrue())
	g.Expect(result.Errors).To(BeEmpty())
	g.Expect(result.Warnings).To(BeEmpty())
}
