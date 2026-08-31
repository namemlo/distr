package api

import (
	"regexp"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

const baselineAdoptionMaximumComponents = 256

var (
	baselineAdoptionChecksumPattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	baselineAdoptionComponentPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	baselineAdoptionCommitPattern      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	baselineAdoptionIdempotencyPattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`,
	)
	baselineAdoptionPlatformPattern = regexp.MustCompile(
		`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$`,
	)
)

type CreateBaselineAdoptionRequest struct {
	IdempotencyKey                 string                             `json:"idempotencyKey"`
	Reason                         string                             `json:"reason"`
	ExpectedPlanChecksum           string                             `json:"expectedPlanChecksum"`
	ExpectedProductReleaseChecksum string                             `json:"expectedProductReleaseChecksum"`
	ExpectedTargetConfigChecksum   string                             `json:"expectedTargetConfigChecksum"`
	Components                     []BaselineAdoptionComponentRequest `json:"components"`
}

type BaselineAdoptionComponentRequest struct {
	ComponentInstanceID             uuid.UUID `json:"componentInstanceId"`
	ComponentKey                    string    `json:"componentKey"`
	ComponentReleaseID              uuid.UUID `json:"componentReleaseId"`
	ComponentReleaseChecksum        string    `json:"componentReleaseChecksum"`
	SourceCommit                    string    `json:"sourceCommit"`
	BuildID                         string    `json:"buildId"`
	ProvenanceVerificationID        uuid.UUID `json:"provenanceVerificationId"`
	ProvenanceEvidenceDigest        string    `json:"provenanceEvidenceDigest"`
	ProvenancePolicyChecksum        string    `json:"provenancePolicyChecksum"`
	ArtifactDigest                  string    `json:"artifactDigest"`
	Platform                        string    `json:"platform"`
	ConfigChecksum                  string    `json:"configChecksum"`
	SchemaVersion                   string    `json:"schemaVersion"`
	CapabilityChecksum              string    `json:"capabilityChecksum"`
	TopologyChecksum                string    `json:"topologyChecksum"`
	ObservationID                   uuid.UUID `json:"observationId"`
	ObserverID                      uuid.UUID `json:"observerId"`
	ObservationEvidenceChecksum     string    `json:"observationEvidenceChecksum"`
	ObservationStateChecksum        string    `json:"observationStateChecksum"`
	ObservationRuntimeStateChecksum string    `json:"observationRuntimeStateChecksum"`
}

type BaselineAdoption = types.BaselineAdoption

func (request *CreateBaselineAdoptionRequest) Validate() error {
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.Reason = strings.TrimSpace(request.Reason)
	request.ExpectedPlanChecksum = strings.TrimSpace(request.ExpectedPlanChecksum)
	request.ExpectedProductReleaseChecksum = strings.TrimSpace(
		request.ExpectedProductReleaseChecksum,
	)
	request.ExpectedTargetConfigChecksum = strings.TrimSpace(
		request.ExpectedTargetConfigChecksum,
	)
	if !baselineAdoptionIdempotencyPattern.MatchString(request.IdempotencyKey) {
		return validation.NewValidationFailedError(
			"idempotencyKey must be 1-128 URL-safe characters",
		)
	}
	if request.Reason == "" || len(request.Reason) > 2048 ||
		strings.ContainsAny(request.Reason, "\r\n") {
		return validation.NewValidationFailedError("reason must be 1-2048 single-line characters")
	}
	if !baselineAdoptionChecksumPattern.MatchString(request.ExpectedPlanChecksum) {
		return validation.NewValidationFailedError("expectedPlanChecksum is invalid")
	}
	if !baselineAdoptionChecksumPattern.MatchString(request.ExpectedProductReleaseChecksum) {
		return validation.NewValidationFailedError("expectedProductReleaseChecksum is invalid")
	}
	if !baselineAdoptionChecksumPattern.MatchString(request.ExpectedTargetConfigChecksum) {
		return validation.NewValidationFailedError("expectedTargetConfigChecksum is invalid")
	}
	if len(request.Components) < 1 || len(request.Components) > baselineAdoptionMaximumComponents {
		return validation.NewValidationFailedError("components must contain between 1 and 256 items")
	}
	seenInstances := make(map[uuid.UUID]struct{}, len(request.Components))
	seenKeys := make(map[string]struct{}, len(request.Components))
	for index := range request.Components {
		component := &request.Components[index]
		component.ComponentKey = strings.TrimSpace(component.ComponentKey)
		component.SourceCommit = strings.TrimSpace(component.SourceCommit)
		component.BuildID = strings.TrimSpace(component.BuildID)
		component.Platform = strings.TrimSpace(component.Platform)
		component.SchemaVersion = strings.TrimSpace(component.SchemaVersion)
		if err := validateBaselineAdoptionComponent(*component, request.ExpectedTargetConfigChecksum); err != nil {
			return err
		}
		if _, exists := seenInstances[component.ComponentInstanceID]; exists {
			return validation.NewValidationFailedError("components contains duplicate componentInstanceId")
		}
		if _, exists := seenKeys[component.ComponentKey]; exists {
			return validation.NewValidationFailedError("components contains duplicate componentKey")
		}
		seenInstances[component.ComponentInstanceID] = struct{}{}
		seenKeys[component.ComponentKey] = struct{}{}
	}
	return nil
}

func validateBaselineAdoptionComponent(
	component BaselineAdoptionComponentRequest,
	targetConfigChecksum string,
) error {
	switch {
	case component.ComponentInstanceID == uuid.Nil:
		return validation.NewValidationFailedError("componentInstanceId is required")
	case !baselineAdoptionComponentPattern.MatchString(component.ComponentKey):
		return validation.NewValidationFailedError("componentKey is invalid")
	case component.ComponentReleaseID == uuid.Nil:
		return validation.NewValidationFailedError("componentReleaseId is required")
	case !baselineAdoptionChecksumPattern.MatchString(component.ComponentReleaseChecksum):
		return validation.NewValidationFailedError("componentReleaseChecksum is invalid")
	case !baselineAdoptionCommitPattern.MatchString(component.SourceCommit):
		return validation.NewValidationFailedError("sourceCommit is invalid")
	case component.BuildID == "" || len(component.BuildID) > 1024 || strings.ContainsAny(component.BuildID, "\r\n"):
		return validation.NewValidationFailedError("buildId is invalid")
	case component.ProvenanceVerificationID == uuid.Nil:
		return validation.NewValidationFailedError("provenanceVerificationId is required")
	case !baselineAdoptionChecksumPattern.MatchString(component.ProvenanceEvidenceDigest):
		return validation.NewValidationFailedError("provenanceEvidenceDigest is invalid")
	case !baselineAdoptionChecksumPattern.MatchString(component.ProvenancePolicyChecksum):
		return validation.NewValidationFailedError("provenancePolicyChecksum is invalid")
	case !baselineAdoptionChecksumPattern.MatchString(component.ArtifactDigest):
		return validation.NewValidationFailedError("artifactDigest is invalid")
	case !baselineAdoptionPlatformPattern.MatchString(component.Platform):
		return validation.NewValidationFailedError("platform is invalid")
	case component.ConfigChecksum != targetConfigChecksum:
		return validation.NewValidationFailedError("configChecksum must match expectedTargetConfigChecksum")
	case component.SchemaVersion == "" || len(component.SchemaVersion) > 256:
		return validation.NewValidationFailedError("schemaVersion is invalid")
	case component.CapabilityChecksum != component.ComponentReleaseChecksum:
		return validation.NewValidationFailedError("capabilityChecksum must match componentReleaseChecksum")
	case !baselineAdoptionChecksumPattern.MatchString(component.TopologyChecksum):
		return validation.NewValidationFailedError("topologyChecksum is invalid")
	case component.ObservationID == uuid.Nil:
		return validation.NewValidationFailedError("observationId is required")
	case component.ObserverID == uuid.Nil:
		return validation.NewValidationFailedError("observerId is required")
	}
	if !baselineAdoptionChecksumPattern.MatchString(component.ObservationEvidenceChecksum) {
		return validation.NewValidationFailedError("observationEvidenceChecksum is invalid")
	}
	if !baselineAdoptionChecksumPattern.MatchString(component.ObservationStateChecksum) {
		return validation.NewValidationFailedError("observationStateChecksum is invalid")
	}
	if !baselineAdoptionChecksumPattern.MatchString(component.ObservationRuntimeStateChecksum) {
		return validation.NewValidationFailedError("observationRuntimeStateChecksum is invalid")
	}
	return nil
}

func (request CreateBaselineAdoptionRequest) ToTypes(
	organizationID,
	deploymentPlanID,
	actorUserAccountID uuid.UUID,
) types.CreateBaselineAdoptionInput {
	components := make([]types.BaselineAdoptionComponentInput, len(request.Components))
	for index, component := range request.Components {
		components[index] = types.BaselineAdoptionComponentInput(component)
	}
	return types.CreateBaselineAdoptionInput{
		OrganizationID: organizationID, DeploymentPlanID: deploymentPlanID,
		ActorUserAccountID: actorUserAccountID, IdempotencyKey: request.IdempotencyKey,
		Reason: request.Reason, ExpectedPlanChecksum: request.ExpectedPlanChecksum,
		ExpectedProductReleaseChecksum: request.ExpectedProductReleaseChecksum,
		ExpectedTargetConfigChecksum:   request.ExpectedTargetConfigChecksum,
		Components:                     components,
	}
}
