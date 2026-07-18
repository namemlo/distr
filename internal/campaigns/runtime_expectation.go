package campaigns

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var campaignRuntimePlatformPattern = regexp.MustCompile(
	`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$`,
)

func RuntimeExpectationChecksum(
	expectation types.CampaignRuntimeExpectation,
) (string, error) {
	artifactDigest := campaignArtifactDigest(expectation.ArtifactDigest)
	componentKey := strings.TrimSpace(expectation.ComponentKey)
	if expectation.ProviderDeploymentUnitID == uuid.Nil ||
		expectation.ProviderComponentInstanceID == uuid.Nil {
		return "", fmt.Errorf("canonical provider identity is required")
	}
	if !campaignChecksumPattern.MatchString(artifactDigest) {
		return "", fmt.Errorf("lowercase sha256 artifact digest is required")
	}
	if !campaignChecksumPattern.MatchString(expectation.ConfigChecksum) {
		return "", fmt.Errorf("lowercase sha256 config checksum is required")
	}
	if componentKey == "" {
		return "", fmt.Errorf("component key is required")
	}
	if !campaignRuntimePlatformPattern.MatchString(expectation.Platform) {
		return "", fmt.Errorf("runtime platform is invalid")
	}
	payload, err := json.Marshal(struct {
		Schema                      string `json:"schema"`
		ProviderDeploymentUnitID    string `json:"providerDeploymentUnitId"`
		ProviderComponentInstanceID string `json:"providerComponentInstanceId"`
		ComponentKey                string `json:"componentKey"`
		ArtifactDigest              string `json:"artifactDigest"`
		ConfigChecksum              string `json:"configChecksum"`
		Platform                    string `json:"platform"`
	}{
		Schema:                      types.CampaignRuntimeExpectationSchemaV1,
		ProviderDeploymentUnitID:    expectation.ProviderDeploymentUnitID.String(),
		ProviderComponentInstanceID: expectation.ProviderComponentInstanceID.String(),
		ComponentKey:                componentKey,
		ArtifactDigest:              artifactDigest,
		ConfigChecksum:              expectation.ConfigChecksum,
		Platform:                    expectation.Platform,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize campaign runtime expectation: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func campaignArtifactDigest(image string) string {
	image = strings.TrimSpace(image)
	if index := strings.LastIndex(image, "@sha256:"); index >= 0 {
		return image[index+1:]
	}
	return image
}
