package releasebundles

import (
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
