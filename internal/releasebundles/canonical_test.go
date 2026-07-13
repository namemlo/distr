package releasebundles

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCanonicalizeIsStableForComponentOrder(t *testing.T) {
	g := NewWithT(t)
	applicationID := uuid.New()
	channelID := uuid.New()
	apiVersionID := uuid.New()
	webVersionID := uuid.New()

	first := types.ReleaseBundle{
		ApplicationID:  applicationID,
		ChannelID:      channelID,
		ReleaseNumber:  "2026.06.20",
		ReleaseNotes:   "Initial release",
		SourceRevision: "abc123",
		Components: []types.ReleaseBundleComponent{
			{
				Key:                  "web",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.4",
				ApplicationVersionID: &webVersionID,
			},
			{
				Key:                  "api",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &apiVersionID,
			},
		},
	}
	second := first
	second.Components = []types.ReleaseBundleComponent{first.Components[1], first.Components[0]}

	firstPayload, firstChecksum, err := Canonicalize(first)
	g.Expect(err).NotTo(HaveOccurred())
	secondPayload, secondChecksum, err := Canonicalize(second)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(firstPayload).To(Equal(secondPayload))
	g.Expect(firstChecksum).To(Equal(secondChecksum))
	g.Expect(firstChecksum).To(HavePrefix("sha256:"))
}

func TestCanonicalizeChangesWhenSemanticContentChanges(t *testing.T) {
	g := NewWithT(t)
	versionID := uuid.New()
	bundle := types.ReleaseBundle{
		ApplicationID: uuid.New(),
		ChannelID:     uuid.New(),
		ReleaseNumber: "2026.06.20",
		Components: []types.ReleaseBundleComponent{
			{
				Key:                  "api",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	}

	_, firstChecksum, err := Canonicalize(bundle)
	g.Expect(err).NotTo(HaveOccurred())
	bundle.Components[0].Version = "1.2.4"
	_, secondChecksum, err := Canonicalize(bundle)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(secondChecksum).NotTo(Equal(firstChecksum))
}

func TestCanonicalizeOmitsEmptySourceMetadataForCompatibility(t *testing.T) {
	g := NewWithT(t)
	applicationID := uuid.New()
	channelID := uuid.New()
	versionID := uuid.New()
	bundle := types.ReleaseBundle{
		ApplicationID:  applicationID,
		ChannelID:      channelID,
		ReleaseNumber:  "2026.06.20",
		ReleaseNotes:   "Initial release",
		SourceRevision: "abc123",
		Components: []types.ReleaseBundleComponent{
			{
				Key:                  "api",
				Name:                 "API",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	}

	payload, _, err := Canonicalize(bundle)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(string(payload)).NotTo(ContainSubstring("sourceMetadata"))
	expected := fmt.Sprintf(
		`{"applicationId":"%s",`+
			`"channelId":"%s",`+
			`"releaseNumber":"2026.06.20",`+
			`"releaseNotes":"Initial release",`+
			`"sourceRevision":"abc123",`+
			`"components":[{`+
			`"key":"api",`+
			`"name":"API",`+
			`"type":"application_version",`+
			`"version":"1.2.3",`+
			`"applicationVersionId":"%s"`+
			`}]}`,
		applicationID,
		channelID,
		versionID,
	)
	g.Expect(string(payload)).To(Equal(expected))
}

func TestCanonicalizeIncludesSourceMetadata(t *testing.T) {
	g := NewWithT(t)
	versionID := uuid.New()
	bundle := types.ReleaseBundle{
		ApplicationID:    uuid.New(),
		ChannelID:        uuid.New(),
		ReleaseNumber:    "2026.06.20",
		SourceRevision:   "abc123",
		SourceRepository: "https://example.invalid/org/project",
		SourceBranch:     "main",
		SourceTag:        "v1.2.3",
		CIProvider:       "generic-ci",
		CIRunID:          "run-123",
		CIRunURL:         "https://ci.example.invalid/runs/123",
		Components: []types.ReleaseBundleComponent{
			{
				Key:                  "api",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	}

	firstPayload, firstChecksum, err := Canonicalize(bundle)
	g.Expect(err).NotTo(HaveOccurred())
	bundle.CIRunID = "run-456"
	secondPayload, secondChecksum, err := Canonicalize(bundle)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(string(firstPayload)).To(ContainSubstring(`"sourceMetadata"`))
	g.Expect(string(firstPayload)).To(ContainSubstring(`"ciRunId":"run-123"`))
	g.Expect(string(secondPayload)).To(ContainSubstring(`"ciRunId":"run-456"`))
	g.Expect(secondChecksum).NotTo(Equal(firstChecksum))
}

func TestCanonicalizeReleaseContractIsStableForSetLikeOrder(t *testing.T) {
	g := NewWithT(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	checksum := "sha256:" + strings.Repeat("b", 64)
	bundle := types.ReleaseBundle{
		ApplicationID: uuid.New(),
		ChannelID:     uuid.New(),
		ReleaseNumber: "2026.07.13.1",
		ReleaseContract: &types.ReleaseContract{
			Schema: types.ReleaseContractSchemaV1,
			Source: types.ReleaseContractSource{
				Repository:   "remittance-b2c-backend",
				Branch:       "customization/emlo-remittance/dev",
				SourceCommit: "1111111111111111111111111111111111111111",
				BuiltCommit:  "1111111111111111111111111111111111111111",
			},
			Build: types.ReleaseContractBuild{ExternalID: "jenkins-42", ExternalURL: "https://ci.example/build/42"},
			Components: []types.ReleaseContractComponent{
				{Name: "transaction-api", Version: "0.0.7", Image: "registry.example/transaction-api@" + digest, Platform: "linux/amd64"},
				{Name: "loyalty-api", Version: "1.2.3", Image: "registry.example/loyalty-api@" + digest, Platform: "linux/arm64"},
			},
			Compatibility: types.ReleaseContractCompatibility{
				Requires: []types.ReleaseContractRequirement{
					{Component: "mc-api", Contract: "mc-api.http@5"},
					{Component: "identity-api", MinimumVersion: "0.0.5"},
				},
				AffectedComponents: []string{"transaction-api", "loyalty-api"},
			},
			Config: types.ReleaseContractConfig{
				RepositoryCommit:      "2222222222222222222222222222222222222222",
				ComposePath:           "choice-tp_dev/1/docker-compose.yaml",
				ServiceConfigPath:     "choice-tp_dev/1/rmt-loyalty-api/appsettings.Production.json",
				ComposeChecksum:       checksum,
				ServiceConfigChecksum: checksum,
				ImmutableObjects: []types.ReleaseContractConfigObject{
					{URI: "s3://config/loyalty", VersionID: "v2", Checksum: checksum},
					{URI: "s3://config/compose", VersionID: "v1", Checksum: checksum},
				},
			},
			Changes: types.ReleaseContractChanges{Summary: "Choice TP loyalty pilot", Commits: []string{"repo-b@222", "repo-a@111"}},
		},
		Components: []types.ReleaseBundleComponent{{
			Key: "loyalty-api", Type: types.ReleaseBundleComponentTypeOCIImage,
			Version: "2026.07.13.1", PackageRef: "registry.example/loyalty-api", Digest: digest,
		}},
	}

	_, firstChecksum, err := Canonicalize(bundle)
	g.Expect(err).NotTo(HaveOccurred())
	bundle.ReleaseContract.Components[0], bundle.ReleaseContract.Components[1] =
		bundle.ReleaseContract.Components[1], bundle.ReleaseContract.Components[0]
	bundle.ReleaseContract.Compatibility.Requires[0], bundle.ReleaseContract.Compatibility.Requires[1] =
		bundle.ReleaseContract.Compatibility.Requires[1], bundle.ReleaseContract.Compatibility.Requires[0]
	slices.Reverse(bundle.ReleaseContract.Compatibility.AffectedComponents)
	slices.Reverse(bundle.ReleaseContract.Config.ImmutableObjects)
	slices.Reverse(bundle.ReleaseContract.Changes.Commits)
	_, secondChecksum, err := Canonicalize(bundle)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(secondChecksum).To(Equal(firstChecksum))
}
