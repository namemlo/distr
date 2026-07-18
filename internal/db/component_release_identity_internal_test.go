package db

import (
	"slices"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestComponentReleaseArtifactIdentityCoversCompleteArtifactSet(t *testing.T) {
	base := []types.ComponentReleaseArtifact{{
		Key:       "image",
		Type:      "oci-image",
		MediaType: "application/vnd.oci.image.index.v1+json",
		Digest:    "sha256:" + strings.Repeat("a", 64),
		Platforms: []types.ComponentReleasePlatform{
			{Platform: "linux/amd64", Digest: "sha256:" + strings.Repeat("b", 64)},
			{Platform: "linux/arm64", Digest: "sha256:" + strings.Repeat("c", 64)},
		},
	}}
	expected := componentReleaseArtifactIdentity(base)

	reordered := cloneComponentReleaseArtifacts(base)
	slices.Reverse(reordered[0].Platforms)
	NewWithT(t).Expect(componentReleaseArtifactIdentity(reordered)).To(Equal(expected))

	tests := []struct {
		name   string
		mutate func([]types.ComponentReleaseArtifact) []types.ComponentReleaseArtifact
	}{
		{
			name: "artifact key",
			mutate: func(artifacts []types.ComponentReleaseArtifact) []types.ComponentReleaseArtifact {
				artifacts[0].Key = "other"
				return artifacts
			},
		},
		{
			name: "artifact type",
			mutate: func(artifacts []types.ComponentReleaseArtifact) []types.ComponentReleaseArtifact {
				artifacts[0].Type = "oci-artifact"
				return artifacts
			},
		},
		{
			name: "media type",
			mutate: func(artifacts []types.ComponentReleaseArtifact) []types.ComponentReleaseArtifact {
				artifacts[0].MediaType = "application/vnd.oci.image.manifest.v1+json"
				return artifacts
			},
		},
		{
			name: "manifest digest",
			mutate: func(artifacts []types.ComponentReleaseArtifact) []types.ComponentReleaseArtifact {
				artifacts[0].Digest = "sha256:" + strings.Repeat("d", 64)
				return artifacts
			},
		},
		{
			name: "platform digest",
			mutate: func(artifacts []types.ComponentReleaseArtifact) []types.ComponentReleaseArtifact {
				artifacts[0].Platforms[0].Digest = "sha256:" + strings.Repeat("e", 64)
				return artifacts
			},
		},
		{
			name: "platform set",
			mutate: func(artifacts []types.ComponentReleaseArtifact) []types.ComponentReleaseArtifact {
				artifacts[0].Platforms = artifacts[0].Platforms[:1]
				return artifacts
			},
		},
		{
			name: "artifact set",
			mutate: func(artifacts []types.ComponentReleaseArtifact) []types.ComponentReleaseArtifact {
				artifacts = append(artifacts, types.ComponentReleaseArtifact{
					Key:       "chart",
					Type:      "helm-chart",
					MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
					Digest:    "sha256:" + strings.Repeat("f", 64),
					Platforms: []types.ComponentReleasePlatform{{
						Platform: "linux/amd64",
						Digest:   "sha256:" + strings.Repeat("1", 64),
					}},
				})
				return artifacts
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			changed := tt.mutate(cloneComponentReleaseArtifacts(base))

			g.Expect(componentReleaseArtifactIdentity(changed)).NotTo(Equal(expected))
		})
	}
}

func cloneComponentReleaseArtifacts(
	artifacts []types.ComponentReleaseArtifact,
) []types.ComponentReleaseArtifact {
	result := slices.Clone(artifacts)
	for i := range result {
		result[i].Platforms = slices.Clone(result[i].Platforms)
	}
	return result
}
