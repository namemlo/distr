package releasebundles

import (
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestValidateReleaseContractRejectsDigestMismatchAndUnsafeConfigPath(t *testing.T) {
	g := NewWithT(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	contract := validReleaseContractForTest(digest)
	contract.Components[0].Image = "registry.example/loyalty-api@sha256:" + strings.Repeat("b", 64)
	contract.Config.ServiceConfigPath = "../secrets/appsettings.json"
	bundleComponents := []types.ReleaseBundleComponent{{
		Key: "loyalty-api", Type: types.ReleaseBundleComponentTypeOCIImage,
		PackageRef: "registry.example/loyalty-api", Digest: digest, Version: "1.2.3",
	}}

	result := ValidateReleaseContract(contract, bundleComponents)

	g.Expect(result.Valid).To(BeFalse())
	g.Expect(issueKeys(result.Errors)).To(ContainElement("releaseContract.components.loyalty-api.image:matchesBundle"))
	g.Expect(issueKeys(result.Errors)).To(ContainElement("releaseContract.config.serviceConfigPath:safePath"))
}

func issueKeys(issues []ValidationIssue) []string {
	keys := make([]string, 0, len(issues))
	for _, issue := range issues {
		keys = append(keys, issue.Field+":"+issue.Rule)
	}
	return keys
}

func TestValidateReleaseContractAcceptsAMD64AndARM64Components(t *testing.T) {
	g := NewWithT(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	contract := validReleaseContractForTest(digest)
	contract.Components = append(contract.Components, types.ReleaseContractComponent{
		Name: "loyalty-api-arm64", Version: "1.2.3",
		Image: "registry.example/loyalty-api@" + digest, Platform: "linux/arm64",
	})
	contract.Compatibility.AffectedComponents = append(
		contract.Compatibility.AffectedComponents, "loyalty-api-arm64",
	)
	bundleComponents := []types.ReleaseBundleComponent{
		{Key: "loyalty-api", Type: types.ReleaseBundleComponentTypeOCIImage, PackageRef: "registry.example/loyalty-api", Digest: digest, Version: "1.2.3"},
		{Key: "loyalty-api-arm64", Type: types.ReleaseBundleComponentTypeOCIImage, PackageRef: "registry.example/loyalty-api", Digest: digest, Version: "1.2.3"},
	}

	result := ValidateReleaseContract(contract, bundleComponents)

	g.Expect(result.Valid).To(BeTrue(), result.Errors)
}

func validReleaseContractForTest(digest string) types.ReleaseContract {
	checksum := "sha256:" + strings.Repeat("c", 64)
	return types.ReleaseContract{
		Schema: types.ReleaseContractSchemaV1,
		Source: types.ReleaseContractSource{
			Repository: "remittance-b2c-backend", Branch: "customization/emlo-remittance/dev",
			SourceCommit: "1111111111111111111111111111111111111111", BuiltCommit: "1111111111111111111111111111111111111111",
		},
		Build: types.ReleaseContractBuild{ExternalID: "42", ExternalURL: "https://ci.example/job/42"},
		Components: []types.ReleaseContractComponent{{
			Name: "loyalty-api", Version: "1.2.3",
			Image: "registry.example/loyalty-api@" + digest, Platform: "linux/amd64",
			Contracts: []string{"loyalty-api/v1"},
		}},
		Compatibility: types.ReleaseContractCompatibility{AffectedComponents: []string{"loyalty-api"}},
		Config: types.ReleaseContractConfig{
			RepositoryCommit: "2222222222222222222222222222222222222222",
			ComposePath:      "choice-tp_dev/1/docker-compose.yaml", ServiceConfigPath: "choice-tp_dev/1/rmt-loyalty-api/appsettings.Production.json",
			ComposeChecksum: checksum, ServiceConfigChecksum: checksum,
		},
		Changes: types.ReleaseContractChanges{Summary: "Deploy loyalty API"},
	}
}
