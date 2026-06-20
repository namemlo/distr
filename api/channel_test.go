package api

import (
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateUpdateChannelRequestValidate(t *testing.T) {
	applicationID := uuid.New()
	lifecycleID := uuid.New()

	tests := []struct {
		name    string
		request CreateUpdateChannelRequest
		wantErr bool
	}{
		{
			name: "accepts complete channel settings",
			request: CreateUpdateChannelRequest{
				ApplicationID:               applicationID,
				LifecycleID:                 lifecycleID,
				Name:                        "Stable",
				Description:                 "Default production-ready channel",
				SortOrder:                   10,
				IsDefault:                   true,
				AllowedVersionRanges:        []string{">=1.0.0 <2.0.0"},
				AllowedPrereleasePatterns:   []string{"rc.*"},
				AllowedSourceBranchPatterns: []string{"main", "release/*"},
				AllowedSourceTagPatterns:    []string{"v*"},
			},
			wantErr: false,
		},
		{
			name: "rejects blank names",
			request: CreateUpdateChannelRequest{
				ApplicationID: applicationID,
				LifecycleID:   lifecycleID,
				Name:          "   ",
			},
			wantErr: true,
		},
		{
			name: "rejects missing application references",
			request: CreateUpdateChannelRequest{
				LifecycleID: lifecycleID,
				Name:        "Stable",
			},
			wantErr: true,
		},
		{
			name: "rejects missing lifecycle references",
			request: CreateUpdateChannelRequest{
				ApplicationID: applicationID,
				Name:          "Stable",
			},
			wantErr: true,
		},
		{
			name: "rejects negative sort order",
			request: CreateUpdateChannelRequest{
				ApplicationID: applicationID,
				LifecycleID:   lifecycleID,
				Name:          "Stable",
				SortOrder:     -1,
			},
			wantErr: true,
		},
		{
			name: "rejects invalid version ranges",
			request: CreateUpdateChannelRequest{
				ApplicationID:        applicationID,
				LifecycleID:          lifecycleID,
				Name:                 "Stable",
				AllowedVersionRanges: []string{">=>1.0.0"},
			},
			wantErr: true,
		},
		{
			name: "rejects invalid prerelease patterns",
			request: CreateUpdateChannelRequest{
				ApplicationID:             applicationID,
				LifecycleID:               lifecycleID,
				Name:                      "Stable",
				AllowedPrereleasePatterns: []string{"["},
			},
			wantErr: true,
		},
		{
			name: "rejects invalid branch globs",
			request: CreateUpdateChannelRequest{
				ApplicationID:               applicationID,
				LifecycleID:                 lifecycleID,
				Name:                        "Stable",
				AllowedSourceBranchPatterns: []string{"["},
			},
			wantErr: true,
		},
		{
			name: "rejects invalid tag globs",
			request: CreateUpdateChannelRequest{
				ApplicationID:            applicationID,
				LifecycleID:              lifecycleID,
				Name:                     "Stable",
				AllowedSourceTagPatterns: []string{"["},
			},
			wantErr: true,
		},
		{
			name: "rejects duplicate trimmed rule entries",
			request: CreateUpdateChannelRequest{
				ApplicationID:               applicationID,
				LifecycleID:                 lifecycleID,
				Name:                        "Stable",
				AllowedSourceBranchPatterns: []string{"release/*", " release/* "},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestValidateChannelVersionRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		request ValidateChannelVersionRequest
		wantErr bool
	}{
		{
			name: "accepts version with branch source",
			request: ValidateChannelVersionRequest{
				Version:      "1.2.3",
				SourceBranch: "release/2026.06",
			},
		},
		{
			name: "accepts version with tag source",
			request: ValidateChannelVersionRequest{
				Version:   "1.2.3",
				SourceTag: "v1.2.3",
			},
		},
		{
			name: "rejects missing version",
			request: ValidateChannelVersionRequest{
				Version: " ",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.request.Validate()

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}
