package api

import (
	"net/url"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type CreateUpdateReleaseBundleRequest struct {
	ApplicationID               uuid.UUID                       `json:"applicationId"`
	ChannelID                   uuid.UUID                       `json:"channelId"`
	DeploymentProcessRevisionID *uuid.UUID                      `json:"deploymentProcessRevisionId,omitempty"`
	ReleaseNumber               string                          `json:"releaseNumber"`
	ReleaseNotes                string                          `json:"releaseNotes"`
	SourceRevision              string                          `json:"sourceRevision"`
	SourceMetadata              *ReleaseBundleSourceMetadata    `json:"sourceMetadata,omitempty"`
	ReleaseContract             *types.ReleaseContract          `json:"releaseContract,omitempty"`
	Components                  []ReleaseBundleComponentRequest `json:"components"`
}

func (r *CreateUpdateReleaseBundleRequest) Validate() error {
	r.ReleaseNumber = strings.TrimSpace(r.ReleaseNumber)
	r.SourceRevision = strings.TrimSpace(r.SourceRevision)
	if r.SourceMetadata != nil {
		r.SourceMetadata.trim()
		if r.SourceMetadata.isZero() {
			r.SourceMetadata = nil
		} else if err := r.SourceMetadata.validate(); err != nil {
			return err
		}
	}
	if r.ReleaseNumber == "" {
		return validation.NewValidationFailedError("releaseNumber is required")
	}
	if r.ApplicationID == uuid.Nil {
		return validation.NewValidationFailedError("applicationId is required")
	}
	if r.ChannelID == uuid.Nil {
		return validation.NewValidationFailedError("channelId is required")
	}
	if r.DeploymentProcessRevisionID != nil && *r.DeploymentProcessRevisionID == uuid.Nil {
		return validation.NewValidationFailedError("deploymentProcessRevisionId must not be empty")
	}
	if len(r.Components) == 0 {
		return validation.NewValidationFailedError("at least one component is required")
	}
	if r.ReleaseContract != nil &&
		r.ReleaseContract.ComponentV2 != nil &&
		len(r.Components) > releasebundles.MaxComponentReleaseProjectionItems {
		return validation.NewValidationFailedError("components contains too many entries")
	}

	seenKeys := map[string]struct{}{}
	typedComponents := make([]types.ReleaseBundleComponent, 0, len(r.Components))
	for i := range r.Components {
		component := &r.Components[i]
		component.trim()
		if component.Key == "" {
			return validation.NewValidationFailedError("component key is required")
		}
		if _, ok := seenKeys[component.Key]; ok {
			return validation.NewValidationFailedError("component keys must be unique")
		}
		seenKeys[component.Key] = struct{}{}
		if err := component.validate(); err != nil {
			return err
		}
		typedComponents = append(typedComponents, types.ReleaseBundleComponent{
			Key: component.Key, Name: component.Name, Type: component.Type, Version: component.Version,
			ApplicationVersionID: component.ApplicationVersionID, PackageRef: component.PackageRef,
			Digest: component.Digest, Checksum: component.Checksum, ChildReleaseBundleID: component.ChildReleaseBundleID,
		})
	}
	if r.ReleaseContract != nil {
		if r.ReleaseContract.ComponentV2 != nil {
			issues := releasebundles.ValidateComponentReleaseContractV2(*r.ReleaseContract.ComponentV2)
			if len(issues) > 0 {
				return validation.NewValidationFailedError(issues[0].Message)
			}
		}
		r.ReleaseContract = releasebundles.NormalizedReleaseContract(r.ReleaseContract)
		if r.ReleaseContract.ComponentV2 != nil {
			bundle := types.ReleaseBundle{
				Kind:                  types.ReleaseBundleKindComponent,
				ReleaseContractSchema: types.ReleaseContractSchemaV2,
				ReleaseContract:       r.ReleaseContract,
				Components:            typedComponents,
				SourceRevision:        r.SourceRevision,
			}
			if r.SourceMetadata != nil {
				bundle.SourceRepository = r.SourceMetadata.Repository
				bundle.SourceBranch = r.SourceMetadata.Branch
				bundle.SourceTag = r.SourceMetadata.Tag
				bundle.CIProvider = r.SourceMetadata.CIProvider
				bundle.CIRunID = r.SourceMetadata.CIRunID
				bundle.CIRunURL = r.SourceMetadata.CIRunURL
			}
			if issues := releasebundles.BindComponentReleaseSourceProjection(&bundle); len(issues) > 0 {
				return validation.NewValidationFailedError(issues[0].Message)
			}
			r.SourceRevision = bundle.SourceRevision
			if r.SourceMetadata == nil {
				r.SourceMetadata = &ReleaseBundleSourceMetadata{}
			}
			r.SourceMetadata.Repository = bundle.SourceRepository
			r.SourceMetadata.Branch = bundle.SourceBranch
			r.SourceMetadata.Tag = bundle.SourceTag
			r.SourceMetadata.CIProvider = bundle.CIProvider
			r.SourceMetadata.CIRunID = bundle.CIRunID
			r.SourceMetadata.CIRunURL = bundle.CIRunURL
			result := releasebundles.ValidateBundleContent(bundle)
			if !result.Valid {
				return validation.NewValidationFailedError(result.Errors[0].Message)
			}
		} else {
			result := releasebundles.ValidateReleaseContractV1(*r.ReleaseContract, typedComponents)
			if !result.Valid {
				return validation.NewValidationFailedError(result.Errors[0].Message)
			}
		}
	}
	return nil
}

type ReleaseBundleSourceMetadata struct {
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	Tag        string `json:"tag"`
	CIProvider string `json:"ciProvider"`
	CIRunID    string `json:"ciRunId"`
	CIRunURL   string `json:"ciRunUrl"`
}

func (m *ReleaseBundleSourceMetadata) trim() {
	m.Repository = strings.TrimSpace(m.Repository)
	m.Branch = strings.TrimSpace(m.Branch)
	m.Tag = strings.TrimSpace(m.Tag)
	m.CIProvider = strings.TrimSpace(m.CIProvider)
	m.CIRunID = strings.TrimSpace(m.CIRunID)
	m.CIRunURL = strings.TrimSpace(m.CIRunURL)
}

func (m ReleaseBundleSourceMetadata) isZero() bool {
	return m.Repository == "" &&
		m.Branch == "" &&
		m.Tag == "" &&
		m.CIProvider == "" &&
		m.CIRunID == "" &&
		m.CIRunURL == ""
}

func (m ReleaseBundleSourceMetadata) validate() error {
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{name: "sourceMetadata.repository", value: m.Repository, limit: 512},
		{name: "sourceMetadata.branch", value: m.Branch, limit: 512},
		{name: "sourceMetadata.tag", value: m.Tag, limit: 512},
		{name: "sourceMetadata.ciProvider", value: m.CIProvider, limit: 512},
		{name: "sourceMetadata.ciRunId", value: m.CIRunID, limit: 512},
		{name: "sourceMetadata.ciRunUrl", value: m.CIRunURL, limit: 2048},
	} {
		if len(field.value) > field.limit {
			return validation.NewValidationFailedError(field.name + " is too long")
		}
		if containsUnsafeSourceMetadata(field.value) {
			return validation.NewValidationFailedError(field.name + " must not contain secrets or authorization data")
		}
	}
	return nil
}

func containsUnsafeSourceMetadata(value string) bool {
	if containsSourceMetadataControl(value) {
		return true
	}
	if parsed, err := url.Parse(strings.TrimSpace(value)); err == nil && parsed.Scheme != "" && parsed.User != nil {
		return true
	}
	normalized := strings.ToLower(value)
	for _, marker := range []string{
		"authorization:",
		"bearer ",
		"accesstoken ",
		"access_token=",
		"api_key=",
		"password=",
		"secret=",
		"token=",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func containsSourceMetadataControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

type ReleaseBundleComponentRequest struct {
	Key                  string                           `json:"key"`
	Name                 string                           `json:"name"`
	Type                 types.ReleaseBundleComponentType `json:"type"`
	Version              string                           `json:"version"`
	ApplicationVersionID *uuid.UUID                       `json:"applicationVersionId,omitempty"`
	PackageRef           string                           `json:"packageRef"`
	Digest               string                           `json:"digest"`
	Checksum             string                           `json:"checksum"`
	ChildReleaseBundleID *uuid.UUID                       `json:"childReleaseBundleId,omitempty"`
}

func (r *ReleaseBundleComponentRequest) trim() {
	r.Key = strings.TrimSpace(r.Key)
	r.Name = strings.TrimSpace(r.Name)
	r.Version = strings.TrimSpace(r.Version)
	r.PackageRef = strings.TrimSpace(r.PackageRef)
	r.Digest = strings.TrimSpace(r.Digest)
	r.Checksum = strings.TrimSpace(r.Checksum)
}

func (r ReleaseBundleComponentRequest) validate() error {
	if !r.Type.IsValid() {
		return validation.NewValidationFailedError("component type is invalid")
	}
	if r.Version == "" {
		return validation.NewValidationFailedError("component version is required")
	}
	switch r.Type {
	case types.ReleaseBundleComponentTypeApplicationVersion:
		if r.ApplicationVersionID == nil || *r.ApplicationVersionID == uuid.Nil {
			return validation.NewValidationFailedError("component applicationVersionId is required")
		}
		if r.ChildReleaseBundleID != nil {
			return validation.NewValidationFailedError("application version component cannot reference a child release bundle")
		}
	case types.ReleaseBundleComponentTypeOCIImage, types.ReleaseBundleComponentTypeOCIArtifact:
		if r.PackageRef == "" {
			return validation.NewValidationFailedError("component packageRef is required")
		}
		if !releasebundles.IsSHA256Digest(r.Digest) {
			return validation.NewValidationFailedError("component digest must be a sha256 digest")
		}
		if r.ApplicationVersionID != nil || r.ChildReleaseBundleID != nil {
			return validation.NewValidationFailedError("oci component cannot reference application versions or child bundles")
		}
	case types.ReleaseBundleComponentTypeHelmChart:
		if r.PackageRef == "" {
			return validation.NewValidationFailedError("component packageRef is required")
		}
		if r.ApplicationVersionID != nil || r.ChildReleaseBundleID != nil {
			return validation.NewValidationFailedError(
				"helm chart component cannot reference application versions or child bundles",
			)
		}
	case types.ReleaseBundleComponentTypeChildReleaseBundle:
		if r.ChildReleaseBundleID == nil || *r.ChildReleaseBundleID == uuid.Nil {
			return validation.NewValidationFailedError("component childReleaseBundleId is required")
		}
		if r.ApplicationVersionID != nil {
			return validation.NewValidationFailedError("child release bundle component cannot reference an application version")
		}
	case types.ReleaseBundleComponentTypeExternalArtifact:
		if r.PackageRef == "" {
			return validation.NewValidationFailedError("component packageRef is required")
		}
		if r.Checksum == "" {
			return validation.NewValidationFailedError("component checksum is required")
		}
		if r.ApplicationVersionID != nil || r.ChildReleaseBundleID != nil {
			return validation.NewValidationFailedError(
				"external artifact component cannot reference application versions or child bundles",
			)
		}
	}
	return nil
}

type ReleaseBundle struct {
	ID                       uuid.UUID                    `json:"id"`
	CreatedAt                time.Time                    `json:"createdAt"`
	UpdatedAt                time.Time                    `json:"updatedAt"`
	ApplicationID            uuid.UUID                    `json:"applicationId"`
	ChannelID                uuid.UUID                    `json:"channelId"`
	ProcessSnapshotID        *uuid.UUID                   `json:"processSnapshotId,omitempty"`
	VariableSnapshotID       *uuid.UUID                   `json:"variableSnapshotId,omitempty"`
	ReleaseNumber            string                       `json:"releaseNumber"`
	ReleaseNotes             string                       `json:"releaseNotes"`
	SourceRevision           string                       `json:"sourceRevision"`
	SourceMetadata           *ReleaseBundleSourceMetadata `json:"sourceMetadata,omitempty"`
	ReleaseContract          *types.ReleaseContract       `json:"releaseContract,omitempty"`
	Kind                     types.ReleaseBundleKind      `json:"kind"`
	ReleaseContractSchema    string                       `json:"releaseContractSchema"`
	Status                   types.ReleaseBundleStatus    `json:"status"`
	PublishedByUserAccountID *uuid.UUID                   `json:"publishedByUserAccountId,omitempty"`
	PublishedAt              *time.Time                   `json:"publishedAt,omitempty"`
	CanonicalChecksum        string                       `json:"canonicalChecksum"`
	Components               []ReleaseBundleComponent     `json:"components"`
}

type ProcessSnapshot struct {
	ID                          uuid.UUID                 `json:"id"`
	CreatedAt                   time.Time                 `json:"createdAt"`
	ApplicationID               uuid.UUID                 `json:"applicationId"`
	DeploymentProcessID         uuid.UUID                 `json:"deploymentProcessId"`
	DeploymentProcessRevisionID uuid.UUID                 `json:"deploymentProcessRevisionId"`
	RevisionNumber              int                       `json:"revisionNumber"`
	CanonicalChecksum           string                    `json:"canonicalChecksum"`
	Revision                    DeploymentProcessRevision `json:"revision"`
}

type ReleaseBundleComponent struct {
	ID                   uuid.UUID                        `json:"id"`
	ReleaseBundleID      uuid.UUID                        `json:"releaseBundleId"`
	Key                  string                           `json:"key"`
	Name                 string                           `json:"name"`
	Type                 types.ReleaseBundleComponentType `json:"type"`
	Version              string                           `json:"version"`
	ApplicationVersionID *uuid.UUID                       `json:"applicationVersionId,omitempty"`
	PackageRef           string                           `json:"packageRef"`
	Digest               string                           `json:"digest"`
	Checksum             string                           `json:"checksum"`
	ChildReleaseBundleID *uuid.UUID                       `json:"childReleaseBundleId,omitempty"`
}

type ReleaseBundleValidationResponse struct {
	Valid    bool                           `json:"valid"`
	Errors   []ReleaseBundleValidationIssue `json:"errors"`
	Warnings []ReleaseBundleValidationIssue `json:"warnings"`
}

type ReleaseBundleValidationIssue struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

type ReleaseBundleEligibilityResponse struct {
	ReleaseBundleID uuid.UUID                        `json:"releaseBundleId"`
	ApplicationID   uuid.UUID                        `json:"applicationId"`
	ChannelID       uuid.UUID                        `json:"channelId"`
	LifecycleID     uuid.UUID                        `json:"lifecycleId"`
	EnvironmentID   uuid.UUID                        `json:"environmentId"`
	EngineReady     bool                             `json:"engineReady"`
	Eligible        bool                             `json:"eligible"`
	TargetPhase     *ReleaseBundleEligibilityPhase   `json:"targetPhase,omitempty"`
	Phases          []ReleaseBundleEligibilityPhase  `json:"phases"`
	Reasons         []ReleaseBundleEligibilityReason `json:"reasons"`
}

type ReleaseBundleEligibilityPhase struct {
	ID                           uuid.UUID   `json:"id"`
	Name                         string      `json:"name"`
	SortOrder                    int         `json:"sortOrder"`
	EnvironmentIDs               []uuid.UUID `json:"environmentIds"`
	Optional                     bool        `json:"optional"`
	AutomaticPromotion           bool        `json:"automaticPromotion"`
	MinimumSuccessfulDeployments int         `json:"minimumSuccessfulDeployments"`
	ApprovalPolicyID             *uuid.UUID  `json:"approvalPolicyId,omitempty"`
	RetentionPolicyID            *uuid.UUID  `json:"retentionPolicyId,omitempty"`
	MatchesEnvironment           bool        `json:"matchesEnvironment"`
	RequiredBeforeTarget         bool        `json:"requiredBeforeTarget"`
	BlocksEligibility            bool        `json:"blocksEligibility"`
}

type ReleaseBundleEligibilityReason struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

const ErrorCodeIdempotencyKeyReusedWithDifferentRequest = "idempotency_key_reused_with_different_request"

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
