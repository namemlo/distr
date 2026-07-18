package releasebundles

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
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

func TestValidateComponentReleaseRejectsUnsafeOuterComponentProjection(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.ReleaseBundleComponent)
		wantIssue string
	}{
		{
			name: "key too long",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Key = strings.Repeat("k", 257)
			},
			wantIssue: "components[0].key:limit",
		},
		{
			name: "name too long",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Name = strings.Repeat("n", 257)
			},
			wantIssue: "components.image.name:limit",
		},
		{
			name: "name contains secret",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Name = "password=customer-secret"
			},
			wantIssue: "components.image.name:targetNeutral",
		},
		{
			name: "name contains embedded path",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Name = "copy /srv/customer/config"
			},
			wantIssue: "components.image.name:targetNeutral",
		},
		{
			name: "name is not normalized",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Name = " Payments API "
			},
			wantIssue: "components.image.name:normalized",
		},
		{
			name: "name contains control character",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Name = "Payments\x00API"
			},
			wantIssue: "components.image.name:targetNeutral",
		},
		{
			name: "type too long",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Type = types.ReleaseBundleComponentType(strings.Repeat("t", 257))
			},
			wantIssue: "components.image.type:limit",
		},
		{
			name: "version too long",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Version = strings.Repeat("v", 129)
			},
			wantIssue: "components.image.version:limit",
		},
		{
			name: "package reference too long",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.PackageRef = strings.Repeat("r", 2049)
			},
			wantIssue: "components.image.packageRef:limit",
		},
		{
			name: "package reference has credentials",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.PackageRef = "user:password@registry.example/payments-api"
			},
			wantIssue: "components.image.packageRef:targetNeutral",
		},
		{
			name: "package reference has mutable tag",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.PackageRef = "registry.example/payments-api:latest"
			},
			wantIssue: "components.image.packageRef:immutableReference",
		},
		{
			name: "package reference duplicates digest",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.PackageRef += "@" + component.Digest
			},
			wantIssue: "components.image.packageRef:immutableReference",
		},
		{
			name: "package reference is local path",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.PackageRef = "/srv/customer/payments-api"
			},
			wantIssue: "components.image.packageRef:immutableReference",
		},
		{
			name: "digest too long",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Digest = strings.Repeat("d", 257)
			},
			wantIssue: "components.image.digest:limit",
		},
		{
			name: "checksum too long",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Checksum = strings.Repeat("c", 257)
			},
			wantIssue: "components.image.checksum:limit",
		},
		{
			name: "key contains secret",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Key = "token=customer-secret"
			},
			wantIssue: "components[0].key:targetNeutral",
		},
		{
			name: "type contains secret",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Type = "client_secret=customer-secret"
			},
			wantIssue: "components.image.type:targetNeutral",
		},
		{
			name: "version contains secret",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Version = "password=customer-secret"
			},
			wantIssue: "components.image.version:targetNeutral",
		},
		{
			name: "digest contains secret",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Digest = "api_key=customer-secret"
			},
			wantIssue: "components.image.digest:targetNeutral",
		},
		{
			name: "checksum is irrelevant",
			mutate: func(component *types.ReleaseBundleComponent) {
				component.Checksum = "sha256:" + strings.Repeat("c", 64)
			},
			wantIssue: "components.image.checksum:forbidden",
		},
		{
			name: "application version reference is irrelevant",
			mutate: func(component *types.ReleaseBundleComponent) {
				id := uuid.New()
				component.ApplicationVersionID = &id
			},
			wantIssue: "components.image.applicationVersionId:forbidden",
		},
		{
			name: "child release reference is irrelevant",
			mutate: func(component *types.ReleaseBundleComponent) {
				id := uuid.New()
				component.ChildReleaseBundleID = &id
			},
			wantIssue: "components.image.childReleaseBundleId:forbidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			bundle := validComponentReleaseBundle()
			tt.mutate(&bundle.Components[0])

			result := ValidateBundleContent(bundle)

			g.Expect(issueKeys(result.Errors)).To(ContainElement(tt.wantIssue))
		})
	}
}

func TestValidateComponentReleaseBoundsOuterComponentCollectionBeforeCanonicalization(t *testing.T) {
	g := NewWithT(t)
	bundle := validComponentReleaseBundle()
	bundle.Components = make([]types.ReleaseBundleComponent, maxReleaseContractItems+1)
	bundle.CanonicalChecksum = "force-canonical-validation"
	bundle.CanonicalPayload = []byte("{}")

	result := ValidateBundleContent(bundle)

	g.Expect(issueKeys(result.Errors)).To(Equal([]string{"components:limit"}))
}

func TestValidateComponentReleaseAcceptsCanonicalCredentialFreePackageReference(t *testing.T) {
	g := NewWithT(t)
	bundle := validComponentReleaseBundle()
	bundle.Components[0].PackageRef = "registry.example:5000/team/payments-api"

	result := ValidateBundleContent(bundle)

	g.Expect(result.Valid).To(BeTrue(), "validation errors: %v", result.Errors)
}

func TestValidateComponentReleaseBindsOuterSourceProjection(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.ReleaseBundle)
		wantIssue string
	}{
		{
			name: "resolved commit mismatch",
			mutate: func(bundle *types.ReleaseBundle) {
				bundle.SourceRevision = strings.Repeat("f", 40)
			},
			wantIssue: "sourceRevision:matchesContract",
		},
		{
			name: "repository mismatch",
			mutate: func(bundle *types.ReleaseBundle) {
				bundle.SourceRepository = "source/other"
			},
			wantIssue: "sourceMetadata.repository:matchesContract",
		},
		{
			name: "requested tag projected as branch",
			mutate: func(bundle *types.ReleaseBundle) {
				bundle.SourceBranch = "v2.4.0"
				bundle.SourceTag = ""
			},
			wantIssue: "sourceMetadata.branch:matchesContract",
		},
		{
			name: "requested tag mismatch",
			mutate: func(bundle *types.ReleaseBundle) {
				bundle.SourceTag = "v9.9.9"
			},
			wantIssue: "sourceMetadata.tag:matchesContract",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			bundle := validComponentReleaseBundle()
			tt.mutate(&bundle)

			result := ValidateBundleContent(bundle)

			g.Expect(issueKeys(result.Errors)).To(ContainElement(tt.wantIssue))
		})
	}
}

func TestBindComponentReleaseSourceProjectionDoesNotMutateContradictoryInput(t *testing.T) {
	g := NewWithT(t)
	bundle := validComponentReleaseBundle()
	bundle.SourceRevision = strings.Repeat("f", 40)

	issues := BindComponentReleaseSourceProjection(&bundle)

	g.Expect(issueKeys(issues)).To(ContainElement("sourceRevision:matchesContract"))
	g.Expect(bundle.SourceRevision).To(Equal(strings.Repeat("f", 40)))
}

func validComponentReleaseBundle() types.ReleaseBundle {
	contract := validComponentReleaseContract()
	return types.ReleaseBundle{
		Kind:                  types.ReleaseBundleKindComponent,
		ReleaseContractSchema: types.ReleaseContractSchemaV2,
		ReleaseContract: &types.ReleaseContract{
			Schema:      types.ReleaseContractSchemaV2,
			ComponentV2: &contract,
		},
		SourceRevision:   contract.Source.Commit,
		SourceRepository: contract.Source.Repository,
		SourceTag:        "v2.4.0",
		Components: []types.ReleaseBundleComponent{
			componentReleaseBundleComponent(
				"image",
				types.ReleaseBundleComponentTypeOCIImage,
				contract.Artifacts[0].Digest,
			),
		},
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
