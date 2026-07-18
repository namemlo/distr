package api

import (
	"regexp"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

var (
	adapterAPIKeyPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	adapterAPIChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type AdapterCapabilityRequest struct {
	Capability string `json:"capability"`
	Version    string `json:"version"`
}

type CreateAdapterImplementationRequest struct {
	Key          string                     `json:"key"`
	Name         string                     `json:"name"`
	Version      string                     `json:"version"`
	Capabilities []AdapterCapabilityRequest `json:"capabilities"`
	Enabled      bool                       `json:"enabled"`
}

func (r *CreateAdapterImplementationRequest) Validate() error {
	r.Key = strings.TrimSpace(r.Key)
	r.Name = strings.TrimSpace(r.Name)
	r.Version = strings.TrimSpace(r.Version)
	if !adapterAPIKeyPattern.MatchString(r.Key) {
		return validation.NewValidationFailedError("key must be a lowercase stable key")
	}
	if r.Name == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if _, err := semver.StrictNewVersion(r.Version); err != nil {
		return validation.NewValidationFailedError("version must be a strict semantic version")
	}
	if len(r.Capabilities) == 0 {
		return validation.NewValidationFailedError("at least one capability is required")
	}
	seen := map[string]struct{}{}
	for i := range r.Capabilities {
		capability := &r.Capabilities[i]
		capability.Capability = strings.TrimSpace(capability.Capability)
		capability.Version = strings.TrimSpace(capability.Version)
		if !adapterAPIKeyPattern.MatchString(capability.Capability) {
			return validation.NewValidationFailedError("capability must be a lowercase stable key")
		}
		if _, err := semver.StrictNewVersion(capability.Version); err != nil {
			return validation.NewValidationFailedError("capability version must be a strict semantic version")
		}
		key := capability.Capability + "\x00" + capability.Version
		if _, duplicate := seen[key]; duplicate {
			return validation.NewValidationFailedError("capability and version pairs must be unique")
		}
		seen[key] = struct{}{}
	}
	return nil
}

type AdapterKeyConfigurationRequest struct {
	KeyID                        string `json:"keyId"`
	PublicKeyFingerprint         string `json:"publicKeyFingerprint"`
	SigningKeyReference          string `json:"signingKeyReference"`
	SigningKeyVersionFingerprint string `json:"signingKeyVersionFingerprint"`
}

type CreateAdapterAssignmentRequest struct {
	AdapterImplementationID uuid.UUID                      `json:"adapterImplementationId"`
	ScopeType               types.AdapterScopeType         `json:"scopeType"`
	ScopeID                 uuid.UUID                      `json:"scopeId"`
	ConfigSnapshotID        uuid.UUID                      `json:"configSnapshotId"`
	ConfigChecksum          string                         `json:"configChecksum"`
	KeyConfiguration        AdapterKeyConfigurationRequest `json:"keyConfiguration"`
	Enabled                 bool                           `json:"enabled"`
}

func (r *CreateAdapterAssignmentRequest) Validate() error {
	if r.AdapterImplementationID == uuid.Nil {
		return validation.NewValidationFailedError("adapterImplementationId is required")
	}
	if !r.ScopeType.IsValid() {
		return validation.NewValidationFailedError("scopeType is invalid")
	}
	if r.ScopeID == uuid.Nil {
		return validation.NewValidationFailedError("scopeId is required")
	}
	if r.ConfigSnapshotID == uuid.Nil {
		return validation.NewValidationFailedError("configSnapshotId is required")
	}
	r.ConfigChecksum = strings.TrimSpace(r.ConfigChecksum)
	if !adapterAPIChecksumPattern.MatchString(r.ConfigChecksum) {
		return validation.NewValidationFailedError("configChecksum must be canonical lowercase sha256")
	}
	key := &r.KeyConfiguration
	key.KeyID = strings.TrimSpace(key.KeyID)
	key.PublicKeyFingerprint = strings.TrimSpace(key.PublicKeyFingerprint)
	key.SigningKeyReference = strings.TrimSpace(key.SigningKeyReference)
	key.SigningKeyVersionFingerprint = strings.TrimSpace(key.SigningKeyVersionFingerprint)
	if key.KeyID == "" {
		return validation.NewValidationFailedError("keyConfiguration.keyId is required")
	}
	if !adapterAPIChecksumPattern.MatchString(key.PublicKeyFingerprint) {
		return validation.NewValidationFailedError(
			"keyConfiguration.publicKeyFingerprint must be canonical lowercase sha256",
		)
	}
	if !strings.HasPrefix(key.SigningKeyReference, "secret-provider://") ||
		strings.Contains(key.SigningKeyReference, "PRIVATE KEY") {
		return validation.NewValidationFailedError(
			"keyConfiguration.signingKeyReference must be an opaque secret-provider reference",
		)
	}
	if !adapterAPIChecksumPattern.MatchString(key.SigningKeyVersionFingerprint) {
		return validation.NewValidationFailedError(
			"keyConfiguration.signingKeyVersionFingerprint must be canonical lowercase sha256",
		)
	}
	return nil
}

type AdapterCapability struct {
	Capability string `json:"capability"`
	Version    string `json:"version"`
}

type AdapterImplementation struct {
	ID           uuid.UUID           `json:"id"`
	CreatedAt    time.Time           `json:"createdAt"`
	Key          string              `json:"key"`
	Name         string              `json:"name"`
	Version      string              `json:"version"`
	Capabilities []AdapterCapability `json:"capabilities"`
	Enabled      bool                `json:"enabled"`
}

type AdapterImplementationPage struct {
	Items      []AdapterImplementation `json:"items"`
	NextCursor string                  `json:"nextCursor,omitempty"`
}

type AdapterKeyConfiguration struct {
	KeyID                        string `json:"keyId"`
	PublicKeyFingerprint         string `json:"publicKeyFingerprint"`
	SigningKeyReference          string `json:"signingKeyReference"`
	SigningKeyVersionFingerprint string `json:"signingKeyVersionFingerprint"`
}

type AdapterAssignment struct {
	ID                      uuid.UUID               `json:"id"`
	CreatedAt               time.Time               `json:"createdAt"`
	UpdatedAt               time.Time               `json:"updatedAt"`
	AdapterImplementationID uuid.UUID               `json:"adapterImplementationId"`
	ScopeType               types.AdapterScopeType  `json:"scopeType"`
	ScopeID                 uuid.UUID               `json:"scopeId"`
	ConfigSnapshotID        uuid.UUID               `json:"configSnapshotId"`
	ConfigChecksum          string                  `json:"configChecksum"`
	KeyConfiguration        AdapterKeyConfiguration `json:"keyConfiguration"`
	Enabled                 bool                    `json:"enabled"`
}

type AdapterAssignmentPage struct {
	Items      []AdapterAssignment `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

type DeploymentPlanStepAdapter struct {
	StepKey                 string                  `json:"stepKey"`
	AdapterAssignmentID     uuid.UUID               `json:"adapterAssignmentId"`
	AdapterImplementationID uuid.UUID               `json:"adapterImplementationId"`
	ImplementationVersion   string                  `json:"implementationVersion"`
	Capability              string                  `json:"capability"`
	CapabilityVersion       string                  `json:"capabilityVersion"`
	ScopeType               types.AdapterScopeType  `json:"scopeType"`
	ScopeID                 uuid.UUID               `json:"scopeId"`
	ConfigSnapshotID        uuid.UUID               `json:"configSnapshotId"`
	ConfigChecksum          string                  `json:"configChecksum"`
	KeyConfiguration        AdapterKeyConfiguration `json:"keyConfiguration"`
	SortOrder               int                     `json:"sortOrder"`
}
