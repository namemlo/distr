package releasebundles

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

const componentReleaseCommit = "0123456789abcdef0123456789abcdef01234567"

func TestParseReleaseContractV2StrictlyDispatchesBySchema(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{
			name: "missing schema",
			mutate: func(contract map[string]any) {
				delete(contract, "schema")
			},
			wantErr: "schema",
		},
		{
			name: "unknown schema",
			mutate: func(contract map[string]any) {
				contract["schema"] = "distr.component-release/v99"
			},
			wantErr: "unsupported",
		},
		{
			name: "unknown v2 field",
			mutate: func(contract map[string]any) {
				contract["targetUrl"] = "https://target.example.invalid"
			},
			wantErr: "unknown field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			raw := validComponentReleaseJSON(t)
			var contract map[string]any
			g.Expect(json.Unmarshal(raw, &contract)).To(Succeed())
			tt.mutate(contract)
			raw, err := json.Marshal(contract)
			g.Expect(err).NotTo(HaveOccurred())

			_, _, err = ParseReleaseContract(raw)

			g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
		})
	}
}

func TestParseReleaseContractPreservesV1Shape(t *testing.T) {
	g := NewWithT(t)
	v1 := validReleaseContractForTest("sha256:" + strings.Repeat("a", 64))
	raw, err := json.Marshal(v1)
	g.Expect(err).NotTo(HaveOccurred())

	schema, parsed, err := ParseReleaseContract(raw)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(schema).To(Equal(types.ReleaseContractSchemaV1))
	g.Expect(parsed).To(Equal(v1))
	normalized, err := NormalizeReleaseContract(parsed)
	g.Expect(err).NotTo(HaveOccurred())
	expected, err := json.Marshal(NormalizedReleaseContract(&v1))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(normalized).To(MatchJSON(expected))
}

func TestValidateComponentReleaseContractV2RejectsInvalidIdentityAndTargetData(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*types.ComponentReleaseContractV2)
		wantRule string
	}{
		{
			name: "missing actual commit",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Source.Commit = ""
			},
			wantRule: "source.commit:commit",
		},
		{
			name: "invalid manifest digest",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Artifacts[0].Digest = "registry.example/component:latest"
			},
			wantRule: "artifacts.image.digest:sha256",
		},
		{
			name: "duplicate platform",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Artifacts[0].Platforms = append(
					contract.Artifacts[0].Platforms,
					contract.Artifacts[0].Platforms[0],
				)
			},
			wantRule: "artifacts.image.platforms.linux/amd64:unique",
		},
		{
			name: "version platform digest conflict",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				duplicate := contract.Artifacts[0].Platforms[0]
				duplicate.Digest = "sha256:" + strings.Repeat("f", 64)
				contract.Artifacts[0].Platforms = append(contract.Artifacts[0].Platforms, duplicate)
			},
			wantRule: "artifacts.image.platforms.linux/amd64:conflict",
		},
		{
			name: "secret looking evidence",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Evidence.Provenance = []string{"oci://evidence?token=secret"}
			},
			wantRule: "evidence.provenance:targetNeutral",
		},
		{
			name: "target path in migration declaration",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Migrations[0].Description = `run against C:\clients\customer-a`
			},
			wantRule: "migrations.schema-v2.description:targetNeutral",
		},
		{
			name: "secret in change summary",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Changes.Summary = "deploy with token=customer-secret"
			},
			wantRule: "changes.summary:targetNeutral",
		},
		{
			name: "empty capability range",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Requires[0].Range = ""
			},
			wantRule: "requires.identity.verify.range:required",
		},
		{
			name: "product requirement with target modes",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Requires[0].ResolutionStage = "product"
			},
			wantRule: "requires.identity.verify.allowedModes:forbidden",
		},
		{
			name: "target requirement without modes",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Requires[0].AllowedModes = nil
			},
			wantRule: "requires.identity.verify.allowedModes:required",
		},
		{
			name: "oci image with artifact media type",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Artifacts[0].MediaType = "application/vnd.oci.artifact.manifest.v1+json"
			},
			wantRule: "artifacts.image.mediaType:matchesType",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			contract := validComponentReleaseContract()
			tt.mutate(&contract)

			issues := ValidateComponentReleaseContractV2(contract)

			g.Expect(issueKeys(issues)).To(ContainElement(tt.wantRule))
		})
	}
}

func TestNormalizeReleaseContractV2SortsOnlySetLikeCollections(t *testing.T) {
	g := NewWithT(t)
	first := validComponentReleaseContract()
	first.Artifacts[0].Platforms = append(first.Artifacts[0].Platforms, types.ComponentReleasePlatform{
		Platform: "linux/arm64",
		Digest:   "sha256:" + strings.Repeat("c", 64),
	})
	first.Provides = append(first.Provides, types.CapabilityDeclaration{Name: "health.http", Version: "1.0.0"})
	first.Evidence.SBOM = []string{"oci://evidence/sbom-b", "oci://evidence/sbom-a"}
	second := first
	second.Artifacts = append([]types.ComponentReleaseArtifact(nil), first.Artifacts...)
	second.Artifacts[0].Platforms = append(
		[]types.ComponentReleasePlatform(nil),
		first.Artifacts[0].Platforms[1],
		first.Artifacts[0].Platforms[0],
	)
	second.Provides = []types.CapabilityDeclaration{first.Provides[1], first.Provides[0]}
	second.Evidence.SBOM = []string{"oci://evidence/sbom-a", "oci://evidence/sbom-b"}

	firstBytes, err := NormalizeReleaseContract(first)
	g.Expect(err).NotTo(HaveOccurred())
	secondBytes, err := NormalizeReleaseContract(second)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(secondBytes).To(Equal(firstBytes))
}

func TestNormalizeReleaseContractV2TreatsNullOmittedAndEmptyCollectionsEqually(t *testing.T) {
	g := NewWithT(t)
	nilCollections := validComponentReleaseContract()
	nilCollections.Artifacts = nil
	nilCollections.Provides = nil
	nilCollections.Requires = nil
	nilCollections.Migrations = nil
	nilCollections.Changes.Commits = nil
	nilCollections.Evidence.Provenance = nil
	nilCollections.Evidence.SBOM = nil
	nilCollections.Evidence.Signatures = nil
	nilCollections.Evidence.Tests = nil

	emptyCollections := nilCollections
	emptyCollections.Artifacts = []types.ComponentReleaseArtifact{}
	emptyCollections.Provides = []types.CapabilityDeclaration{}
	emptyCollections.Requires = []types.CapabilityRequirement{}
	emptyCollections.Migrations = []types.MigrationDeclaration{}
	emptyCollections.Changes.Commits = []string{}
	emptyCollections.Evidence.Provenance = []string{}
	emptyCollections.Evidence.SBOM = []string{}
	emptyCollections.Evidence.Signatures = []string{}
	emptyCollections.Evidence.Tests = []string{}

	nilBytes, err := NormalizeReleaseContract(nilCollections)
	g.Expect(err).NotTo(HaveOccurred())
	emptyBytes, err := NormalizeReleaseContract(emptyCollections)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(nilBytes).To(Equal(emptyBytes))
	g.Expect(string(nilBytes)).To(ContainSubstring(`"artifacts":[]`))
	g.Expect(string(nilBytes)).To(ContainSubstring(`"provides":[]`))
	g.Expect(string(nilBytes)).To(ContainSubstring(`"requires":[]`))
	g.Expect(string(nilBytes)).To(ContainSubstring(`"migrations":[]`))
	g.Expect(string(nilBytes)).To(ContainSubstring(`"commits":[]`))
	g.Expect(string(nilBytes)).To(ContainSubstring(`"provenance":[]`))
	g.Expect(string(nilBytes)).To(ContainSubstring(`"sbom":[]`))
	g.Expect(string(nilBytes)).To(ContainSubstring(`"signatures":[]`))
	g.Expect(string(nilBytes)).To(ContainSubstring(`"tests":[]`))
}

func TestNormalizeReleaseContractV2TreatsNestedNullAndEmptyCollectionsEqually(t *testing.T) {
	g := NewWithT(t)
	nilCollections := validComponentReleaseContract()
	nilCollections.Artifacts[0].Platforms = nil
	nilCollections.Requires[0].AllowedModes = nil
	emptyCollections := nilCollections
	emptyCollections.Artifacts = append([]types.ComponentReleaseArtifact{}, nilCollections.Artifacts...)
	emptyCollections.Artifacts[0].Platforms = []types.ComponentReleasePlatform{}
	emptyCollections.Requires = append([]types.CapabilityRequirement{}, nilCollections.Requires...)
	emptyCollections.Requires[0].AllowedModes = []string{}

	nilBytes, err := NormalizeReleaseContract(nilCollections)
	g.Expect(err).NotTo(HaveOccurred())
	emptyBytes, err := NormalizeReleaseContract(emptyCollections)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(nilBytes).To(Equal(emptyBytes))
	g.Expect(string(nilBytes)).To(ContainSubstring(`"platforms":[]`))
	g.Expect(string(nilBytes)).To(ContainSubstring(`"allowedModes":[]`))
}

func TestValidateComponentReleaseContractV2RejectsCredentialBearingTextAndReferences(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*types.ComponentReleaseContractV2)
		wantRule string
	}{
		{
			name: "URL userinfo",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Evidence.Provenance = []string{"https://user:pass@evidence.example/provenance"}
			},
			wantRule: "evidence.provenance:targetNeutral",
		},
		{
			name: "PEM private key",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Changes.Summary = "-----BEGIN PRIVATE KEY-----"
			},
			wantRule: "changes.summary:targetNeutral",
		},
		{
			name: "password assignment",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Changes.Summary = "password: release-secret"
			},
			wantRule: "changes.summary:targetNeutral",
		},
		{
			name: "client secret assignment",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Migrations[0].Description = "client_secret: release-secret"
			},
			wantRule: "migrations.schema-v2.description:targetNeutral",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			contract := validComponentReleaseContract()
			tt.mutate(&contract)

			issues := ValidateComponentReleaseContractV2(contract)

			g.Expect(issueKeys(issues)).To(ContainElement(tt.wantRule))
		})
	}
}

func TestValidateComponentReleaseContractV2AcceptsCredentialFreeImmutableReferences(t *testing.T) {
	g := NewWithT(t)
	contract := validComponentReleaseContract()
	contract.Evidence.Provenance = []string{
		"https://evidence.example/provenance/sha256/" + strings.Repeat("a", 64),
		"oci://evidence/provenance@sha256:" + strings.Repeat("b", 64),
	}
	contract.Changes.Summary = "Rotate credential provider metadata without embedding credentials"

	issues := ValidateComponentReleaseContractV2(contract)

	g.Expect(issueKeys(issues)).NotTo(ContainElement("evidence.provenance:targetNeutral"))
	g.Expect(issueKeys(issues)).NotTo(ContainElement("changes.summary:targetNeutral"))
}

func TestValidateComponentReleaseContractV2BoundsEveryCollection(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*types.ComponentReleaseContractV2)
		wantRule string
	}{
		{
			name: "artifacts",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Artifacts = make([]types.ComponentReleaseArtifact, maxReleaseContractItems+1)
			},
			wantRule: "artifacts:limit",
		},
		{
			name: "platforms",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Artifacts[0].Platforms = make(
					[]types.ComponentReleasePlatform,
					maxComponentReleasePlatforms+1,
				)
			},
			wantRule: "artifacts.image.platforms:limit",
		},
		{
			name: "provides",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Provides = make([]types.CapabilityDeclaration, maxReleaseContractItems+1)
			},
			wantRule: "provides:limit",
		},
		{
			name: "requires",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Requires = make([]types.CapabilityRequirement, maxReleaseContractItems+1)
			},
			wantRule: "requires:limit",
		},
		{
			name: "allowed modes",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Requires[0].AllowedModes = make([]string, maxComponentReleaseAllowedModes+1)
			},
			wantRule: "requires.identity.verify.allowedModes:limit",
		},
		{
			name: "migrations",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Migrations = make([]types.MigrationDeclaration, maxReleaseContractItems+1)
			},
			wantRule: "migrations:limit",
		},
		{
			name: "commits",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Changes.Commits = make([]string, maxReleaseContractItems+1)
			},
			wantRule: "changes.commits:limit",
		},
		{
			name: "provenance evidence",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Evidence.Provenance = make([]string, maxReleaseContractItems+1)
			},
			wantRule: "evidence.provenance:limit",
		},
		{
			name: "SBOM evidence",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Evidence.SBOM = make([]string, maxReleaseContractItems+1)
			},
			wantRule: "evidence.sbom:limit",
		},
		{
			name: "signature evidence",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Evidence.Signatures = make([]string, maxReleaseContractItems+1)
			},
			wantRule: "evidence.signatures:limit",
		},
		{
			name: "test evidence",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Evidence.Tests = make([]string, maxReleaseContractItems+1)
			},
			wantRule: "evidence.tests:limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			contract := validComponentReleaseContract()
			tt.mutate(&contract)

			issues := ValidateComponentReleaseContractV2(contract)

			g.Expect(issueKeys(issues)).To(ContainElement(tt.wantRule))
		})
	}
}

func TestValidateComponentReleaseContractV2BoundsFreeTextAndReferences(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*types.ComponentReleaseContractV2)
		wantRule string
	}{
		{
			name: "source repository",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Source.Repository = strings.Repeat("a", maxComponentReleaseSourceFieldLength+1)
			},
			wantRule: "source.repository:limit",
		},
		{
			name: "source requested ref",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Source.RequestedRef = strings.Repeat("a", maxComponentReleaseSourceFieldLength+1)
			},
			wantRule: "source.requestedRef:limit",
		},
		{
			name: "build id",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Build.ID = strings.Repeat("a", maxComponentReleaseBuildFieldLength+1)
			},
			wantRule: "build.id:limit",
		},
		{
			name: "build builder",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Build.Builder = strings.Repeat("a", maxComponentReleaseBuildFieldLength+1)
			},
			wantRule: "build.builder:limit",
		},
		{
			name: "change summary",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Changes.Summary = strings.Repeat("a", maxComponentReleaseSummaryLength+1)
			},
			wantRule: "changes.summary:limit",
		},
		{
			name: "migration description",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Migrations[0].Description = strings.Repeat(
					"a",
					maxComponentReleaseDescriptionLength+1,
				)
			},
			wantRule: "migrations.schema-v2.description:limit",
		},
		{
			name: "evidence reference",
			mutate: func(contract *types.ComponentReleaseContractV2) {
				contract.Evidence.Provenance = []string{
					strings.Repeat("a", maxComponentReleaseReferenceLength+1),
				}
			},
			wantRule: "evidence.provenance:limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			contract := validComponentReleaseContract()
			tt.mutate(&contract)

			issues := ValidateComponentReleaseContractV2(contract)

			g.Expect(issueKeys(issues)).To(ContainElement(tt.wantRule))
		})
	}
}

func TestValidateComponentReleaseContractV2BoundsCanonicalPayload(t *testing.T) {
	g := NewWithT(t)
	contract := validComponentReleaseContract()
	reference := strings.Repeat("a", maxComponentReleaseReferenceLength)
	contract.Evidence.Provenance = make([]string, maxReleaseContractItems)
	for i := range contract.Evidence.Provenance {
		contract.Evidence.Provenance[i] = fmt.Sprintf("%03d-%s", i, reference[4:])
	}

	issues := ValidateComponentReleaseContractV2(contract)

	g.Expect(issueKeys(issues)).To(ContainElement("payload:limit"))
}

func validComponentReleaseJSON(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(validComponentReleaseContract())
	NewWithT(t).Expect(err).NotTo(HaveOccurred())
	return raw
}

func validComponentReleaseContract() types.ComponentReleaseContractV2 {
	return types.ComponentReleaseContractV2{
		Schema:       types.ReleaseContractSchemaV2,
		ComponentKey: "payments.api",
		Version:      "2.4.0",
		Source: types.ComponentReleaseSource{
			Repository:   "source/payments-api",
			RequestedRef: "refs/tags/v2.4.0",
			Commit:       componentReleaseCommit,
		},
		Build: types.ComponentReleaseBuild{
			ID:      "build-42",
			Builder: "generic-ci",
		},
		Artifacts: []types.ComponentReleaseArtifact{{
			Key:       "image",
			Type:      "oci-image",
			MediaType: "application/vnd.oci.image.index.v1+json",
			Digest:    "sha256:" + strings.Repeat("a", 64),
			Platforms: []types.ComponentReleasePlatform{{
				Platform: "linux/amd64",
				Digest:   "sha256:" + strings.Repeat("b", 64),
			}},
		}},
		Provides: []types.CapabilityDeclaration{{
			Name:    "payments.api",
			Version: "2.4.0",
		}},
		Requires: []types.CapabilityRequirement{{
			Name:            "identity.verify",
			Range:           ">=1.5.0 <3.0.0",
			ResolutionStage: "target",
			AllowedModes:    []string{"included", "pinned-existing"},
		}},
		Migrations: []types.MigrationDeclaration{{
			Key:           "schema-v2",
			Type:          "database",
			Order:         10,
			Compatibility: "forward-compatible",
			FailurePolicy: "stop",
			Description:   "Apply the symbolic account schema upgrade",
		}},
		Changes: types.ComponentReleaseChanges{
			Summary: "Add account settlement support",
			Commits: []string{componentReleaseCommit},
		},
		Evidence: types.ComponentReleaseEvidenceReferences{
			Provenance: []string{"oci://evidence/provenance@sha256:" + strings.Repeat("d", 64)},
			SBOM:       []string{"oci://evidence/sbom@sha256:" + strings.Repeat("e", 64)},
		},
	}
}
