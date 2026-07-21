package observation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
)

const campaignRuntimeExpectationSchemaV1 = "distr.campaign-runtime-expectation/v1"

var (
	ErrObserverMismatch        = errors.New("observer identity or scope mismatch")
	ErrUntrustedObservation    = errors.New("observation authentication failed")
	ErrStaleObservation        = errors.New("observation is stale")
	ErrConflictingReplay       = errors.New("observation sequence conflicts with retained evidence")
	ErrInvalidObservation      = errors.New("observation envelope is invalid")
	observationChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	runtimePlatformPattern     = regexp.MustCompile(
		`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$`,
	)
)

// RuntimeStateChecksum is the stable checksum shared with campaign runtime
// expectations. It intentionally excludes observer, sequence, timestamps,
// evidence transport, health, and outcome metadata.
func RuntimeStateChecksum(envelope types.ObservationEnvelope) (string, error) {
	artifactDigest := strings.TrimSpace(envelope.ArtifactDigest)
	if index := strings.LastIndex(artifactDigest, "@sha256:"); index >= 0 {
		artifactDigest = artifactDigest[index+1:]
	}
	componentKey := strings.TrimSpace(envelope.ComponentKey)
	if envelope.DeploymentUnitID == [16]byte{} ||
		envelope.ComponentInstanceID == [16]byte{} {
		return "", fmt.Errorf("canonical provider identity is required")
	}
	if componentKey == "" ||
		!observationChecksumPattern.MatchString(artifactDigest) ||
		!observationChecksumPattern.MatchString(envelope.ConfigChecksum) ||
		!runtimePlatformPattern.MatchString(envelope.Platform) {
		return "", fmt.Errorf("canonical runtime state is invalid")
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
		Schema:                      campaignRuntimeExpectationSchemaV1,
		ProviderDeploymentUnitID:    envelope.DeploymentUnitID.String(),
		ProviderComponentInstanceID: envelope.ComponentInstanceID.String(),
		ComponentKey:                componentKey, ArtifactDigest: artifactDigest,
		ConfigChecksum: envelope.ConfigChecksum, Platform: envelope.Platform,
	})
	if err != nil {
		return "", fmt.Errorf("canonicalize observed runtime state: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func SameObservationMaterial(
	envelope types.ObservationEnvelope,
	retained types.ObservedComponentState,
) bool {
	return envelope.OrganizationID == retained.OrganizationID &&
		envelope.ObserverID == retained.ObserverID &&
		envelope.DeploymentUnitID == retained.DeploymentUnitID &&
		envelope.ComponentInstanceID == retained.ComponentInstanceID &&
		strings.TrimSpace(envelope.ComponentKey) == retained.ComponentKey &&
		envelope.SourceSequence == retained.SourceSequence &&
		envelope.CapturedAt.Equal(retained.CapturedAt) &&
		envelope.EvidenceChecksum == retained.EvidenceChecksum &&
		envelope.EvidenceReference == retained.EvidenceReference &&
		envelope.ArtifactDigest == retained.ArtifactDigest &&
		envelope.ConfigChecksum == retained.ConfigChecksum &&
		envelope.SchemaVersion == retained.SchemaVersion &&
		envelope.CapabilityChecksum == retained.CapabilityChecksum &&
		envelope.Platform == retained.Platform &&
		envelope.TopologyChecksum == retained.TopologyChecksum &&
		envelope.Health == retained.Health &&
		envelope.Outcome == retained.Outcome
}

func EvaluateAdmission(
	registration types.ObserverRegistration,
	head *types.ComponentObservationHead,
	retained *types.ObservedComponentState,
	envelope types.ObservationEnvelope,
	receivedAt time.Time,
) (types.ObservationAdmissionDecision, error) {
	reject := func(reason string, retain bool) types.ObservationAdmissionDecision {
		return types.ObservationAdmissionDecision{
			Disposition:    types.ObservationDispositionRejected,
			RetainEvidence: retain, Reason: reason,
		}
	}
	if !registration.Enabled || envelope.ObserverID != registration.ID ||
		envelope.OrganizationID != registration.OrganizationID ||
		envelope.DeploymentUnitID != registration.DeploymentUnitID ||
		(registration.ComponentInstanceID != nil &&
			envelope.ComponentInstanceID != *registration.ComponentInstanceID) {
		return reject(ErrObserverMismatch.Error(), false), ErrObserverMismatch
	}
	if registration.CredentialFingerprint == "" ||
		envelope.CredentialFingerprint != registration.CredentialFingerprint {
		return reject(ErrUntrustedObservation.Error(), false), ErrUntrustedObservation
	}
	if envelope.SourceSequence < 1 || envelope.ComponentInstanceID == [16]byte{} ||
		strings.TrimSpace(envelope.ComponentKey) == "" ||
		!observationChecksumPattern.MatchString(envelope.EvidenceChecksum) {
		return reject(ErrInvalidObservation.Error(), false), ErrInvalidObservation
	}
	if envelope.CapturedAt.After(receivedAt.Add(registration.MaxClockSkew)) {
		decision := reject("observation captured time exceeds clock-skew policy", true)
		decision.Trusted = true
		return decision, ErrInvalidObservation
	}
	if receivedAt.Sub(envelope.CapturedAt) > registration.MaxFreshness {
		decision := reject(ErrStaleObservation.Error(), true)
		decision.Trusted = true
		return decision, ErrStaleObservation
	}
	if head != nil && head.ObserverID == envelope.ObserverID {
		switch {
		case envelope.SourceSequence == head.SourceSequence &&
			retained != nil &&
			SameObservationMaterial(envelope, *retained):
			return types.ObservationAdmissionDecision{
				Disposition: types.ObservationDispositionReplay,
				Trusted:     true, RetainEvidence: true,
			}, nil
		case envelope.SourceSequence == head.SourceSequence:
			return types.ObservationAdmissionDecision{
				Disposition: types.ObservationDispositionConflict,
				Trusted:     true, RetainEvidence: true, Quarantine: true,
				Reason: ErrConflictingReplay.Error(),
			}, ErrConflictingReplay
		case envelope.SourceSequence < head.SourceSequence:
			return types.ObservationAdmissionDecision{
				Disposition: types.ObservationDispositionOutOfOrder,
				Trusted:     true, RetainEvidence: true,
				Reason: "older source sequence retained without replacing current evidence",
			}, nil
		}
	}
	return types.ObservationAdmissionDecision{
		Disposition: types.ObservationDispositionAccepted,
		Trusted:     true, AdvanceHead: true, RetainEvidence: true,
	}, nil
}
