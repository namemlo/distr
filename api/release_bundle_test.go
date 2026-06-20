package api

import (
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateUpdateReleaseBundleRequestValidateTrimsFields(t *testing.T) {
	g := NewWithT(t)
	applicationID := uuid.New()
	channelID := uuid.New()
	versionID := uuid.New()

	request := CreateUpdateReleaseBundleRequest{
		ApplicationID:  applicationID,
		ChannelID:      channelID,
		ReleaseNumber:  " 1.2.3 ",
		ReleaseNotes:   " release notes ",
		SourceRevision: " abc123 ",
		Components: []ReleaseBundleComponentRequest{
			{
				Key:                  " api ",
				Name:                 " API ",
				Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
				Version:              " 1.2.3 ",
				ApplicationVersionID: &versionID,
			},
		},
	}

	g.Expect(request.Validate()).To(Succeed())

	g.Expect(request.ReleaseNumber).To(Equal("1.2.3"))
	g.Expect(request.SourceRevision).To(Equal("abc123"))
	g.Expect(request.Components[0].Key).To(Equal("api"))
	g.Expect(request.Components[0].Name).To(Equal("API"))
	g.Expect(request.Components[0].Version).To(Equal("1.2.3"))
}

func TestCreateUpdateReleaseBundleRequestValidateRejectsInvalidPayloads(t *testing.T) {
	applicationID := uuid.New()
	channelID := uuid.New()
	versionID := uuid.New()
	childBundleID := uuid.New()

	validComponent := ReleaseBundleComponentRequest{
		Key:                  "api",
		Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
		Version:              "1.2.3",
		ApplicationVersionID: &versionID,
	}

	tests := []struct {
		name    string
		request CreateUpdateReleaseBundleRequest
	}{
		{
			name: "empty release number",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: " ",
				Components:    []ReleaseBundleComponentRequest{validComponent},
			},
		},
		{
			name: "missing application",
			request: CreateUpdateReleaseBundleRequest{
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components:    []ReleaseBundleComponentRequest{validComponent},
			},
		},
		{
			name: "missing channel",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ReleaseNumber: "1.2.3",
				Components:    []ReleaseBundleComponentRequest{validComponent},
			},
		},
		{
			name: "no components",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID:  applicationID,
				ChannelID:      channelID,
				ReleaseNumber:  "1.2.3",
				SourceRevision: "abc123",
			},
		},
		{
			name: "duplicate trimmed component keys",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					validComponent,
					{
						Key:                  " api ",
						Type:                 types.ReleaseBundleComponentTypeApplicationVersion,
						Version:              "1.2.4",
						ApplicationVersionID: &versionID,
					},
				},
			},
		},
		{
			name: "invalid component type",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:     "api",
						Type:    types.ReleaseBundleComponentType("unsupported"),
						Version: "1.2.3",
					},
				},
			},
		},
		{
			name: "application version component requires applicationVersionId",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:     "api",
						Type:    types.ReleaseBundleComponentTypeApplicationVersion,
						Version: "1.2.3",
					},
				},
			},
		},
		{
			name: "oci image component requires digest",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:        "api-image",
						Type:       types.ReleaseBundleComponentTypeOCIImage,
						Version:    "1.2.3",
						PackageRef: "registry.example/api",
					},
				},
			},
		},
		{
			name: "child bundle component requires childReleaseBundleId",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:     "platform",
						Type:    types.ReleaseBundleComponentTypeChildReleaseBundle,
						Version: "2026.06.20",
					},
				},
			},
		},
		{
			name: "external artifact requires checksum",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:        "manual",
						Type:       types.ReleaseBundleComponentTypeExternalArtifact,
						Version:    "1.2.3",
						PackageRef: "https://example.com/manual.zip",
					},
				},
			},
		},
		{
			name: "child bundle cannot also specify application version",
			request: CreateUpdateReleaseBundleRequest{
				ApplicationID: applicationID,
				ChannelID:     channelID,
				ReleaseNumber: "1.2.3",
				Components: []ReleaseBundleComponentRequest{
					{
						Key:                  "platform",
						Type:                 types.ReleaseBundleComponentTypeChildReleaseBundle,
						Version:              "2026.06.20",
						ChildReleaseBundleID: &childBundleID,
						ApplicationVersionID: &versionID,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			g.Expect(err).To(HaveOccurred())
		})
	}
}
