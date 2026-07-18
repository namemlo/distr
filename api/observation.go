package api

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

var observationChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type ObservationRequest struct {
	OrganizationID      uuid.UUID                `json:"organizationId"`
	ObserverID          uuid.UUID                `json:"observerId"`
	DeploymentUnitID    uuid.UUID                `json:"deploymentUnitId"`
	ComponentInstanceID uuid.UUID                `json:"componentInstanceId"`
	ComponentKey        string                   `json:"componentKey"`
	SourceSequence      int64                    `json:"sourceSequence"`
	CapturedAt          time.Time                `json:"capturedAt"`
	EvidenceChecksum    string                   `json:"evidenceChecksum"`
	EvidenceReference   string                   `json:"evidenceReference,omitempty"`
	ArtifactDigest      string                   `json:"artifactDigest"`
	ConfigChecksum      string                   `json:"configChecksum"`
	SchemaVersion       string                   `json:"schemaVersion"`
	CapabilityChecksum  string                   `json:"capabilityChecksum"`
	Platform            string                   `json:"platform"`
	TopologyChecksum    string                   `json:"topologyChecksum"`
	Health              types.ObservedHealth     `json:"health"`
	Outcome             types.ObservationOutcome `json:"outcome"`
}

func (r ObservationRequest) Validate() error {
	if r.OrganizationID == uuid.Nil || r.ObserverID == uuid.Nil ||
		r.DeploymentUnitID == uuid.Nil || r.ComponentInstanceID == uuid.Nil {
		return validation.NewValidationFailedError("observation identities are required")
	}
	if r.SourceSequence < 1 || strings.TrimSpace(r.ComponentKey) == "" ||
		r.CapturedAt.IsZero() || strings.TrimSpace(r.SchemaVersion) == "" ||
		strings.TrimSpace(r.Platform) == "" {
		return validation.NewValidationFailedError("observation state fields are required")
	}
	if len(r.ComponentKey) > 256 || len(r.SchemaVersion) > 256 ||
		len(r.Platform) > 128 || len(r.EvidenceReference) > 2048 {
		return validation.NewValidationFailedError("observation fields exceed their maximum length")
	}
	for _, checksum := range []string{
		r.EvidenceChecksum, r.ArtifactDigest, r.ConfigChecksum,
		r.CapabilityChecksum, r.TopologyChecksum,
	} {
		if !observationChecksumPattern.MatchString(checksum) {
			return validation.NewValidationFailedError(
				"observation checksums must be lowercase sha256",
			)
		}
	}
	if !slices.Contains([]types.ObservedHealth{
		types.ObservedHealthUnknown,
		types.ObservedHealthHealthy,
		types.ObservedHealthUnhealthy,
	}, r.Health) {
		return validation.NewValidationFailedError("health is invalid")
	}
	if !slices.Contains([]types.ObservationOutcome{
		types.ObservationOutcomeComplete,
		types.ObservationOutcomePartial,
		types.ObservationOutcomeUnknown,
		types.ObservationOutcomeCancelled,
		types.ObservationOutcomeFailed,
	}, r.Outcome) {
		return validation.NewValidationFailedError("outcome is invalid")
	}
	return nil
}

func (r ObservationRequest) ToEnvelope(
	credentialFingerprint string,
) types.ObservationEnvelope {
	return types.ObservationEnvelope{
		OrganizationID: r.OrganizationID, ObserverID: r.ObserverID,
		DeploymentUnitID: r.DeploymentUnitID, ComponentInstanceID: r.ComponentInstanceID,
		ComponentKey: strings.TrimSpace(r.ComponentKey), SourceSequence: r.SourceSequence,
		CapturedAt: r.CapturedAt.UTC(), CredentialFingerprint: credentialFingerprint,
		EvidenceChecksum:  r.EvidenceChecksum,
		EvidenceReference: strings.TrimSpace(r.EvidenceReference),
		ArtifactDigest:    r.ArtifactDigest, ConfigChecksum: r.ConfigChecksum,
		SchemaVersion:      strings.TrimSpace(r.SchemaVersion),
		CapabilityChecksum: r.CapabilityChecksum, Platform: strings.TrimSpace(r.Platform),
		TopologyChecksum: r.TopologyChecksum, Health: r.Health, Outcome: r.Outcome,
	}
}

type CreateObserverRegistrationRequest struct {
	DeploymentUnitID      uuid.UUID  `json:"deploymentUnitId"`
	ComponentInstanceID   *uuid.UUID `json:"componentInstanceId,omitempty"`
	ObserverKey           string     `json:"observerKey"`
	AdapterImplementation string     `json:"adapterImplementation"`
	AdapterVersion        string     `json:"adapterVersion"`
	Credential            string     `json:"credential"`
	MaxFreshnessSeconds   int64      `json:"maxFreshnessSeconds"`
	MaxClockSkewSeconds   int64      `json:"maxClockSkewSeconds"`
	Measurements          []string   `json:"measurements"`
}

func (r CreateObserverRegistrationRequest) Validate() error {
	if r.DeploymentUnitID == uuid.Nil ||
		(r.ComponentInstanceID != nil && *r.ComponentInstanceID == uuid.Nil) {
		return validation.NewValidationFailedError("observer scope is required")
	}
	for _, value := range []string{
		r.ObserverKey, r.AdapterImplementation, r.AdapterVersion,
	} {
		if strings.TrimSpace(value) == "" || len(value) > 256 {
			return validation.NewValidationFailedError("observer identity fields are invalid")
		}
	}
	if len(r.Credential) < 32 || len(r.Credential) > 512 ||
		strings.ContainsAny(r.Credential, " \t\r\n") {
		return validation.NewValidationFailedError("credential must be a 32-512 character token")
	}
	if r.MaxFreshnessSeconds < 1 || r.MaxFreshnessSeconds > 86400 ||
		r.MaxClockSkewSeconds < 0 || r.MaxClockSkewSeconds > 300 {
		return validation.NewValidationFailedError("observer freshness policy is invalid")
	}
	if len(r.Measurements) == 0 || len(r.Measurements) > 32 {
		return validation.NewValidationFailedError("observer measurements are required")
	}
	for _, measurement := range r.Measurements {
		if strings.TrimSpace(measurement) == "" || len(measurement) > 128 {
			return validation.NewValidationFailedError("observer measurement is invalid")
		}
	}
	return nil
}

func ObserverCredentialFingerprint(credential string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(credential)))
}

type ObserverRegistration struct {
	ID                    uuid.UUID  `json:"id"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
	DeploymentUnitID      uuid.UUID  `json:"deploymentUnitId"`
	ComponentInstanceID   *uuid.UUID `json:"componentInstanceId,omitempty"`
	ObserverKey           string     `json:"observerKey"`
	AdapterImplementation string     `json:"adapterImplementation"`
	AdapterVersion        string     `json:"adapterVersion"`
	Enabled               bool       `json:"enabled"`
	MaxFreshnessSeconds   int64      `json:"maxFreshnessSeconds"`
	MaxClockSkewSeconds   int64      `json:"maxClockSkewSeconds"`
	Measurements          []string   `json:"measurements"`
}

type ObservedComponentState struct {
	ID                  uuid.UUID                    `json:"id"`
	CreatedAt           time.Time                    `json:"createdAt"`
	ObserverID          uuid.UUID                    `json:"observerId"`
	DeploymentUnitID    uuid.UUID                    `json:"deploymentUnitId"`
	ComponentInstanceID uuid.UUID                    `json:"componentInstanceId"`
	ComponentKey        string                       `json:"componentKey"`
	SourceSequence      int64                        `json:"sourceSequence"`
	CapturedAt          time.Time                    `json:"capturedAt"`
	ReceivedAt          time.Time                    `json:"receivedAt"`
	EvidenceChecksum    string                       `json:"evidenceChecksum"`
	EvidenceReference   string                       `json:"evidenceReference,omitempty"`
	ArtifactDigest      string                       `json:"artifactDigest"`
	ConfigChecksum      string                       `json:"configChecksum"`
	SchemaVersion       string                       `json:"schemaVersion"`
	CapabilityChecksum  string                       `json:"capabilityChecksum"`
	Platform            string                       `json:"platform"`
	TopologyChecksum    string                       `json:"topologyChecksum"`
	Health              types.ObservedHealth         `json:"health"`
	Outcome             types.ObservationOutcome     `json:"outcome"`
	Disposition         types.ObservationDisposition `json:"disposition"`
	Trusted             bool                         `json:"trusted"`
	Current             bool                         `json:"current"`
	StateChecksum       string                       `json:"stateChecksum"`
}
