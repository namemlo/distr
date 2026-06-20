package mapping

import (
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestReleaseBundleToAPI(t *testing.T) {
	g := NewWithT(t)
	bundleID := uuid.New()
	componentID := uuid.New()
	applicationID := uuid.New()
	channelID := uuid.New()
	versionID := uuid.New()

	response := ReleaseBundleToAPI(types.ReleaseBundle{
		ID:                bundleID,
		ApplicationID:     applicationID,
		ChannelID:         channelID,
		ReleaseNumber:     "2026.06.20",
		ReleaseNotes:      "Initial release",
		SourceRevision:    "abc123",
		Status:            types.ReleaseBundleStatusDraft,
		CanonicalChecksum: "sha256:abc",
		Components: []types.ReleaseBundleComponent{
			{
				ID:                   componentID,
				ReleaseBundleID:      bundleID,
				Key:                  "api",
				Name:                 "API",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	})

	g.Expect(response).To(Equal(api.ReleaseBundle{
		ID:                bundleID,
		ApplicationID:     applicationID,
		ChannelID:         channelID,
		ReleaseNumber:     "2026.06.20",
		ReleaseNotes:      "Initial release",
		SourceRevision:    "abc123",
		Status:            types.ReleaseBundleStatusDraft,
		CanonicalChecksum: "sha256:abc",
		Components: []api.ReleaseBundleComponent{
			{
				ID:                   componentID,
				ReleaseBundleID:      bundleID,
				Key:                  "api",
				Name:                 "API",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              "1.2.3",
				ApplicationVersionID: &versionID,
			},
		},
	}))
}
