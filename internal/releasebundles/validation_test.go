package releasebundles

import (
	"strings"
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
				Digest:     "sha256:" + strings.Repeat("a", 64),
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

func TestValidateComponentReleaseRequiresExactArtifactComponentBijection(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	tests := []struct {
		name       string
		components []types.ReleaseBundleComponent
		wantField  string
	}{
		{
			name: "outer component without artifact",
			components: []types.ReleaseBundleComponent{
				componentReleaseBundleComponent("image", types.ReleaseBundleComponentTypeOCIImage, digest),
				componentReleaseBundleComponent("extra", types.ReleaseBundleComponentTypeOCIArtifact, digest),
			},
			wantField: "components.extra",
		},
		{
			name:       "artifact without outer component",
			components: []types.ReleaseBundleComponent{},
			wantField:  "releaseContract.artifacts.image",
		},
		{
			name: "artifact type does not exactly map to outer component type",
			components: []types.ReleaseBundleComponent{
				componentReleaseBundleComponent("image", types.ReleaseBundleComponentTypeOCIArtifact, digest),
			},
			wantField: "releaseContract.artifacts.image.type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			contract := validComponentReleaseContract()
			bundle := types.ReleaseBundle{
				Kind:                  types.ReleaseBundleKindComponent,
				ReleaseContractSchema: types.ReleaseContractSchemaV2,
				ReleaseContract: &types.ReleaseContract{
					Schema:      types.ReleaseContractSchemaV2,
					ComponentV2: &contract,
				},
				Components: tt.components,
			}

			result := ValidateBundleContent(bundle)

			g.Expect(result.Valid).To(BeFalse())
			g.Expect(result.Errors).To(ContainElement(WithTransform(
				func(issue ValidationIssue) string { return issue.Field },
				Equal(tt.wantField),
			)))
		})
	}
}

func componentReleaseBundleComponent(
	key string,
	componentType types.ReleaseBundleComponentType,
	digest string,
) types.ReleaseBundleComponent {
	return types.ReleaseBundleComponent{
		Key:        key,
		Type:       componentType,
		Version:    "2.4.0",
		PackageRef: "registry.example/payments-api",
		Digest:     digest,
	}
}

func TestValidateComponentReleaseAcceptsDifferentDigestsForDifferentArtifactsOnSamePlatform(t *testing.T) {
	g := NewWithT(t)
	contract := validComponentReleaseContract()
	artifactDigest := "sha256:" + strings.Repeat("c", 64)
	contract.Artifacts = append(contract.Artifacts, types.ComponentReleaseArtifact{
		Key:       "metadata",
		Type:      "oci-artifact",
		MediaType: "application/vnd.oci.artifact.manifest.v1+json",
		Digest:    artifactDigest,
		Platforms: []types.ComponentReleasePlatform{{
			Platform: "linux/amd64",
			Digest:   "sha256:" + strings.Repeat("d", 64),
		}},
	})
	bundle := types.ReleaseBundle{
		Kind:                  types.ReleaseBundleKindComponent,
		ReleaseContractSchema: types.ReleaseContractSchemaV2,
		ReleaseContract: &types.ReleaseContract{
			Schema:      types.ReleaseContractSchemaV2,
			ComponentV2: &contract,
		},
		Components: []types.ReleaseBundleComponent{
			componentReleaseBundleComponent(
				"image",
				types.ReleaseBundleComponentTypeOCIImage,
				contract.Artifacts[0].Digest,
			),
			componentReleaseBundleComponent(
				"metadata",
				types.ReleaseBundleComponentTypeOCIArtifact,
				artifactDigest,
			),
		},
	}

	result := ValidateBundleContent(bundle)

	g.Expect(result.Valid).To(BeTrue(), "validation errors: %v", result.Errors)
}
