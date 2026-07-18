package types

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AdapterScopeType string

var adapterDatabaseResourcePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)

const (
	AdapterScopeDeploymentTarget     AdapterScopeType = "deployment_target"
	AdapterScopeDeploymentUnit       AdapterScopeType = "deployment_unit"
	AdapterScopeComponentInstance    AdapterScopeType = "component_instance"
	AdapterScopeDatabaseResource     AdapterScopeType = "database_resource"
	AdapterScopeObserverRegistration AdapterScopeType = "observer_registration"
)

func (s AdapterScopeType) IsValid() bool {
	switch s {
	case AdapterScopeDeploymentTarget,
		AdapterScopeDeploymentUnit,
		AdapterScopeComponentInstance,
		AdapterScopeDatabaseResource,
		AdapterScopeObserverRegistration:
		return true
	default:
		return false
	}
}

func (s AdapterScopeType) IsValidReference(reference string) bool {
	reference = strings.TrimSpace(reference)
	if s == AdapterScopeDatabaseResource {
		return adapterDatabaseResourcePattern.MatchString(reference)
	}
	if !s.IsValid() {
		return false
	}
	_, err := uuid.Parse(reference)
	return err == nil
}

// AdapterKeyConfiguration is non-secret configuration for protocol signing.
// The private key remains in the referenced secret provider; plans retain only
// the opaque reference and non-reversible key/version fingerprints.
type AdapterKeyConfiguration struct {
	KeyID                        string `json:"keyId"`
	PublicKeyFingerprint         string `json:"publicKeyFingerprint"`
	SigningKeyReference          string `json:"signingKeyReference"`
	SigningKeyVersionFingerprint string `json:"signingKeyVersionFingerprint"`
}

type AdapterCapability struct {
	ID                      uuid.UUID `db:"id" json:"id,omitempty"`
	AdapterImplementationID uuid.UUID `db:"adapter_implementation_id" json:"adapterImplementationId,omitempty"`
	OrganizationID          uuid.UUID `db:"organization_id" json:"organizationId,omitempty"`
	Capability              string    `db:"capability" json:"capability"`
	Version                 string    `db:"version" json:"version"`
}

type AdapterImplementation struct {
	ID             uuid.UUID           `db:"id" json:"id"`
	CreatedAt      time.Time           `db:"created_at" json:"createdAt"`
	OrganizationID uuid.UUID           `db:"organization_id" json:"organizationId"`
	Key            string              `db:"adapter_key" json:"key"`
	Name           string              `db:"name" json:"name"`
	Version        string              `db:"version" json:"version"`
	Enabled        bool                `db:"enabled" json:"enabled"`
	Capabilities   []AdapterCapability `db:"-" json:"capabilities"`
}

type AdapterAssignment struct {
	ID                           uuid.UUID               `db:"id" json:"id"`
	CreatedAt                    time.Time               `db:"created_at" json:"createdAt"`
	UpdatedAt                    time.Time               `db:"updated_at" json:"updatedAt"`
	OrganizationID               uuid.UUID               `db:"organization_id" json:"organizationId"`
	AdapterImplementationID      uuid.UUID               `db:"adapter_implementation_id" json:"adapterImplementationId"`
	ScopeType                    AdapterScopeType        `db:"scope_type" json:"scopeType"`
	ScopeReference               string                  `db:"scope_reference" json:"scopeReference"`
	ConfigSnapshotID             uuid.UUID               `db:"config_snapshot_id" json:"configSnapshotId"`
	ConfigChecksum               string                  `db:"config_checksum" json:"configChecksum"`
	KeyConfiguration             AdapterKeyConfiguration `db:"-" json:"keyConfiguration"`
	KeyID                        string                  `db:"key_id" json:"-"`
	PublicKeyFingerprint         string                  `db:"public_key_fingerprint" json:"-"`
	SigningKeyReference          string                  `db:"signing_key_reference" json:"-"`
	SigningKeyVersionFingerprint string                  `db:"signing_key_version_fingerprint" json:"-"`
	Enabled                      bool                    `db:"enabled" json:"enabled"`
}

func (a *AdapterAssignment) NormalizeKeyConfiguration() {
	if a.KeyConfiguration.KeyID == "" {
		a.KeyConfiguration = AdapterKeyConfiguration{
			KeyID:                        a.KeyID,
			PublicKeyFingerprint:         a.PublicKeyFingerprint,
			SigningKeyReference:          a.SigningKeyReference,
			SigningKeyVersionFingerprint: a.SigningKeyVersionFingerprint,
		}
	}
	a.KeyID = a.KeyConfiguration.KeyID
	a.PublicKeyFingerprint = a.KeyConfiguration.PublicKeyFingerprint
	a.SigningKeyReference = a.KeyConfiguration.SigningKeyReference
	a.SigningKeyVersionFingerprint = a.KeyConfiguration.SigningKeyVersionFingerprint
}

type AdapterResolutionRequest struct {
	OrganizationID            uuid.UUID               `json:"organizationId"`
	StepKey                   string                  `json:"stepKey"`
	RequiredCapability        string                  `json:"requiredCapability"`
	RequiredCapabilityVersion string                  `json:"requiredCapabilityVersion"`
	ScopeType                 AdapterScopeType        `json:"scopeType"`
	ScopeReference            string                  `json:"scopeReference"`
	TargetConfigSnapshotID    uuid.UUID               `json:"targetConfigSnapshotId"`
	TargetConfigChecksum      string                  `json:"targetConfigChecksum"`
	Implementations           []AdapterImplementation `json:"implementations"`
	Assignments               []AdapterAssignment     `json:"assignments"`
}

type StepAdapterRequirement struct {
	StepKey           string           `json:"stepKey"`
	Capability        string           `json:"capability"`
	CapabilityVersion string           `json:"capabilityVersion"`
	ScopeType         AdapterScopeType `json:"scopeType"`
	ScopeReference    string           `json:"scopeReference"`
}

type AdapterRequirement struct {
	StepKind   string `json:"stepKind"`
	Capability string `json:"capability"`
	Version    string `json:"version"`
}

type AdapterStepScopeBinding struct {
	StepKey        string           `json:"stepKey"`
	ScopeType      AdapterScopeType `json:"scopeType"`
	ScopeReference string           `json:"scopeReference"`
}

type ResolvedStepAdapter struct {
	AdapterAssignmentID     uuid.UUID               `json:"adapterAssignmentId"`
	AdapterImplementationID uuid.UUID               `json:"adapterImplementationId"`
	ImplementationVersion   string                  `json:"implementationVersion"`
	Capability              string                  `json:"capability"`
	CapabilityVersion       string                  `json:"capabilityVersion"`
	ScopeType               AdapterScopeType        `json:"scopeType"`
	ScopeReference          string                  `json:"scopeReference"`
	ConfigSnapshotID        uuid.UUID               `json:"configSnapshotId"`
	ConfigChecksum          string                  `json:"configChecksum"`
	KeyConfiguration        AdapterKeyConfiguration `json:"keyConfiguration"`
}

type ResolvedPlanStepAdapter struct {
	StepKey string `json:"stepKey"`
	ResolvedStepAdapter
}

type DeploymentPlanStepAdapter struct {
	ID                           uuid.UUID               `db:"id" json:"id"`
	DeploymentPlanID             uuid.UUID               `db:"deployment_plan_id" json:"deploymentPlanId"`
	DeploymentPlanStepID         uuid.UUID               `db:"deployment_plan_step_id" json:"deploymentPlanStepId"`
	OrganizationID               uuid.UUID               `db:"organization_id" json:"organizationId"`
	StepKey                      string                  `db:"step_key" json:"stepKey"`
	AdapterAssignmentID          uuid.UUID               `db:"adapter_assignment_id" json:"adapterAssignmentId"`
	AdapterImplementationID      uuid.UUID               `db:"adapter_implementation_id" json:"adapterImplementationId"`
	ImplementationVersion        string                  `db:"implementation_version" json:"implementationVersion"`
	Capability                   string                  `db:"capability" json:"capability"`
	CapabilityVersion            string                  `db:"capability_version" json:"capabilityVersion"`
	ScopeType                    AdapterScopeType        `db:"scope_type" json:"scopeType"`
	ScopeReference               string                  `db:"scope_reference" json:"scopeReference"`
	ConfigSnapshotID             uuid.UUID               `db:"config_snapshot_id" json:"configSnapshotId"`
	ConfigChecksum               string                  `db:"config_checksum" json:"configChecksum"`
	KeyConfiguration             AdapterKeyConfiguration `db:"-" json:"keyConfiguration"`
	KeyID                        string                  `db:"key_id" json:"-"`
	PublicKeyFingerprint         string                  `db:"public_key_fingerprint" json:"-"`
	SigningKeyReference          string                  `db:"signing_key_reference" json:"-"`
	SigningKeyVersionFingerprint string                  `db:"signing_key_version_fingerprint" json:"-"`
	SortOrder                    int                     `db:"sort_order" json:"sortOrder"`
}

func (a *DeploymentPlanStepAdapter) NormalizeKeyConfiguration() {
	if a.KeyConfiguration.KeyID == "" {
		a.KeyConfiguration = AdapterKeyConfiguration{
			KeyID:                        a.KeyID,
			PublicKeyFingerprint:         a.PublicKeyFingerprint,
			SigningKeyReference:          a.SigningKeyReference,
			SigningKeyVersionFingerprint: a.SigningKeyVersionFingerprint,
		}
	}
	a.KeyID = a.KeyConfiguration.KeyID
	a.PublicKeyFingerprint = a.KeyConfiguration.PublicKeyFingerprint
	a.SigningKeyReference = a.KeyConfiguration.SigningKeyReference
	a.SigningKeyVersionFingerprint = a.KeyConfiguration.SigningKeyVersionFingerprint
}

type AdapterRuntimeState struct {
	AdapterAssignmentID       uuid.UUID
	AdapterImplementationID   uuid.UUID
	ImplementationVersion     string
	Capability                string
	CapabilityVersion         string
	ScopeType                 AdapterScopeType
	ScopeReference            string
	ConfigSnapshotID          uuid.UUID
	AssignmentConfigChecksum  string
	SnapshotCanonicalChecksum string
	KeyConfiguration          AdapterKeyConfiguration
	Enabled                   bool
}
