package releasebundles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/distr-sh/distr/internal/types"
)

type canonicalBundle struct {
	ApplicationID  string               `json:"applicationId"`
	ChannelID      string               `json:"channelId"`
	ReleaseNumber  string               `json:"releaseNumber"`
	ReleaseNotes   string               `json:"releaseNotes"`
	SourceRevision string               `json:"sourceRevision"`
	Components     []canonicalComponent `json:"components"`
}

type canonicalComponent struct {
	Key                  string `json:"key"`
	Name                 string `json:"name"`
	Type                 string `json:"type"`
	Version              string `json:"version"`
	ApplicationVersionID string `json:"applicationVersionId,omitempty"`
	PackageRef           string `json:"packageRef,omitempty"`
	Digest               string `json:"digest,omitempty"`
	Checksum             string `json:"checksum,omitempty"`
	ChildReleaseBundleID string `json:"childReleaseBundleId,omitempty"`
}

func Canonicalize(bundle types.ReleaseBundle) ([]byte, string, error) {
	components := slices.Clone(bundle.Components)
	slices.SortFunc(components, func(a, b types.ReleaseBundleComponent) int {
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
			return 1
		}
		return 0
	})

	canonical := canonicalBundle{
		ApplicationID:  bundle.ApplicationID.String(),
		ChannelID:      bundle.ChannelID.String(),
		ReleaseNumber:  bundle.ReleaseNumber,
		ReleaseNotes:   bundle.ReleaseNotes,
		SourceRevision: bundle.SourceRevision,
		Components:     make([]canonicalComponent, 0, len(components)),
	}
	for _, component := range components {
		canonical.Components = append(canonical.Components, canonicalizeComponent(component))
	}

	payload, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(payload)
	return payload, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func canonicalizeComponent(component types.ReleaseBundleComponent) canonicalComponent {
	result := canonicalComponent{
		Key:        component.Key,
		Name:       component.Name,
		Type:       string(component.Type),
		Version:    component.Version,
		PackageRef: component.PackageRef,
		Digest:     component.Digest,
		Checksum:   component.Checksum,
	}
	if component.ApplicationVersionID != nil {
		result.ApplicationVersionID = component.ApplicationVersionID.String()
	}
	if component.ChildReleaseBundleID != nil {
		result.ChildReleaseBundleID = component.ChildReleaseBundleID.String()
	}
	return result
}
