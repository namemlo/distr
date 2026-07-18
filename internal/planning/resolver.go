package planning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var planChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ResolveTargetRequirements resolves every symbolic target-stage requirement
// against already loaded, organization-scoped planning facts. The repository
// owns loading those facts; this pure function owns deterministic selection.
func ResolveTargetRequirements(
	_ context.Context,
	draft types.PlanDraft,
) ([]types.RequirementResolution, []types.ValidationIssue) {
	if draft.ResolutionInput == nil {
		return nil, []types.ValidationIssue{{
			Code: "resolution_input_required", Field: "resolutionInput",
			Message: "server-side resolution facts are required",
		}}
	}
	input := draft.ResolutionInput
	requirements := slices.Clone(input.Requirements)
	slices.SortFunc(requirements, func(a, b types.TargetRequirement) int {
		return strings.Compare(a.Key, b.Key)
	})
	candidates := slices.Clone(input.Candidates)
	slices.SortFunc(candidates, compareRequirementCandidates)

	resolutions := make([]types.RequirementResolution, 0, len(requirements))
	issues := make([]types.ValidationIssue, 0)
	for _, requirement := range requirements {
		matches := matchingTargetCandidates(requirement, candidates)
		switch len(matches) {
		case 0:
			issues = append(issues, types.ValidationIssue{
				Code:    "unresolved_requirement",
				Field:   "requirements." + requirement.Key,
				Message: fmt.Sprintf("target requirement %q has no exact allowed binding", requirement.Key),
			})
			continue
		case 1:
			// Continue below.
		default:
			issues = append(issues, types.ValidationIssue{
				Code:    "ambiguous_requirement",
				Field:   "requirements." + requirement.Key,
				Message: fmt.Sprintf("target requirement %q resolves to more than one binding", requirement.Key),
			})
			continue
		}

		candidate := matches[0]
		if issue := validateCandidateBinding(requirement, candidate, draft); issue != nil {
			issues = append(issues, *issue)
			continue
		}
		resolution := types.RequirementResolution{
			RequirementKey:            requirement.Key,
			ConsumerKey:               requirement.ConsumerKey,
			Capability:                requirement.Capability,
			VersionRange:              requirement.VersionRange,
			Mode:                      candidate.Mode,
			ProviderReleaseID:         cloneUUID(candidate.ProviderReleaseID),
			ObservationID:             cloneUUID(candidate.ObservationID),
			ProviderVersion:           strings.TrimSpace(candidate.ProviderVersion),
			ProviderPlatform:          strings.TrimSpace(candidate.ProviderPlatform),
			ProviderReleaseChecksum:   strings.TrimSpace(candidate.ProviderReleaseChecksum),
			ProvenanceBindingChecksum: strings.TrimSpace(candidate.ProvenanceBindingChecksum),
			ComponentInstanceID:       cloneUUID(candidate.ComponentInstanceID),
			SubscriberSetChecksum:     strings.TrimSpace(candidate.SubscriberSetChecksum),
			ExpectedStateVersion:      candidate.ExpectedStateVersion,
			ExpectedStateChecksum:     strings.TrimSpace(candidate.ExpectedStateChecksum),
			SortOrder:                 len(resolutions),
			V1Compatible:              candidate.V1Compatible,
		}
		if candidate.DeploymentUnitID != uuid.Nil {
			resolution.ProviderDeploymentUnitID = cloneUUID(&candidate.DeploymentUnitID)
		}
		checksum, err := canonicalChecksum(struct {
			RequirementKey            string                          `json:"requirementKey"`
			ConsumerKey               string                          `json:"consumerKey"`
			Capability                string                          `json:"capability"`
			VersionRange              string                          `json:"versionRange"`
			Mode                      types.RequirementResolutionMode `json:"mode"`
			ProviderReleaseID         *uuid.UUID                      `json:"providerReleaseId,omitempty"`
			ObservationID             *uuid.UUID                      `json:"observationId,omitempty"`
			ProviderVersion           string                          `json:"providerVersion"`
			ProviderPlatform          string                          `json:"providerPlatform"`
			ProviderReleaseChecksum   string                          `json:"providerReleaseChecksum,omitempty"`
			ProvenanceBindingChecksum string                          `json:"provenanceBindingChecksum,omitempty"`
			ProviderDeploymentUnitID  *uuid.UUID                      `json:"providerDeploymentUnitId,omitempty"`
			ComponentInstanceID       *uuid.UUID                      `json:"componentInstanceId,omitempty"`
			SubscriberSetChecksum     string                          `json:"subscriberSetChecksum,omitempty"`
			ExpectedStateVersion      int64                           `json:"expectedStateVersion"`
			ExpectedStateChecksum     string                          `json:"expectedStateChecksum"`
		}{
			RequirementKey: requirement.Key, ConsumerKey: requirement.ConsumerKey,
			Capability: requirement.Capability, VersionRange: requirement.VersionRange,
			Mode: candidate.Mode, ProviderReleaseID: resolution.ProviderReleaseID,
			ObservationID: resolution.ObservationID, ProviderVersion: resolution.ProviderVersion,
			ProviderPlatform:          resolution.ProviderPlatform,
			ProviderReleaseChecksum:   resolution.ProviderReleaseChecksum,
			ProvenanceBindingChecksum: resolution.ProvenanceBindingChecksum,
			ProviderDeploymentUnitID:  resolution.ProviderDeploymentUnitID,
			ComponentInstanceID:       resolution.ComponentInstanceID,
			SubscriberSetChecksum:     resolution.SubscriberSetChecksum,
			ExpectedStateVersion:      resolution.ExpectedStateVersion,
			ExpectedStateChecksum:     resolution.ExpectedStateChecksum,
		})
		if err != nil {
			issues = append(issues, types.ValidationIssue{
				Code: "binding_checksum_failed", Field: "requirements." + requirement.Key,
				Message: "could not canonicalize resolved requirement",
			})
			continue
		}
		resolution.BindingChecksum = checksum
		resolutions = append(resolutions, resolution)
	}
	sortValidationIssues(issues)
	return resolutions, issues
}

// ValidatePlanDraft applies all publication blockers to a server-hydrated
// draft. It never trusts organization or placement facts supplied by an API.
func ValidatePlanDraft(ctx context.Context, draft types.PlanDraft) []types.ValidationIssue {
	issues := validatePlanDraftIdentity(draft)
	if draft.ResolutionInput == nil {
		issues = append(issues, types.ValidationIssue{
			Code: "resolution_input_required", Field: "resolutionInput",
			Message: "server-side resolution facts are required",
		})
		sortValidationIssues(issues)
		return issues
	}
	input := draft.ResolutionInput
	issues = append(issues, validatePlacement(draft, *input)...)
	issues = append(issues, validateTargetConfig(draft, *input)...)
	issues = append(issues, validateReleasePins(draft, *input)...)
	resolutions, resolutionIssues := ResolveTargetRequirements(ctx, draft)
	issues = append(issues, resolutionIssues...)
	if draft.ProtocolVersion == types.DeploymentPlanProtocolV1 {
		for _, resolution := range resolutions {
			if !resolution.V1Compatible {
				issues = append(issues, types.ValidationIssue{
					Code: "protocol_v1_incompatible", Field: "protocolVersion",
					Message: "protocol v1 cannot publish a plan containing v2-only requirement bindings",
				})
				break
			}
		}
	}
	sortValidationIssues(issues)
	return deduplicateValidationIssues(issues)
}

func validatePlanDraftIdentity(draft types.PlanDraft) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	requiredUUIDs := []struct {
		field string
		value uuid.UUID
	}{
		{"organizationId", draft.OrganizationID},
		{"productReleaseId", draft.ProductReleaseID},
		{"deploymentUnitId", draft.DeploymentUnitID},
		{"environmentAssignmentId", draft.EnvironmentAssignmentID},
		{"targetConfigSnapshotId", draft.TargetConfigSnapshotID},
	}
	for _, required := range requiredUUIDs {
		if required.value == uuid.Nil {
			issues = append(issues, types.ValidationIssue{
				Code: "required", Field: required.field,
				Message: required.field + " is required",
			})
		}
	}
	if draft.Revision < 1 {
		issues = append(issues, types.ValidationIssue{
			Code: "invalid_revision", Field: "revision",
			Message: "draft revision must be positive",
		})
	}
	if draft.ProtocolVersion != types.DeploymentPlanProtocolV1 &&
		draft.ProtocolVersion != types.DeploymentPlanProtocolV2 {
		issues = append(issues, types.ValidationIssue{
			Code: "unsupported_protocol", Field: "protocolVersion",
			Message: "protocolVersion must be v1 or v2",
		})
	}
	if draft.SupersedesDeploymentPlanID == nil && strings.TrimSpace(draft.SupersedeReason) != "" {
		issues = append(issues, types.ValidationIssue{
			Code: "supersede_plan_required", Field: "supersedeReason",
			Message: "supersede reason requires a superseded deployment plan",
		})
	}
	if draft.SupersedesDeploymentPlanID != nil && strings.TrimSpace(draft.SupersedeReason) == "" {
		issues = append(issues, types.ValidationIssue{
			Code: "supersede_reason_required", Field: "supersedeReason",
			Message: "a superseding plan requires a reason",
		})
	}
	return issues
}

func validatePlacement(
	draft types.PlanDraft,
	input types.PlanResolutionInput,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	if input.ProductReleaseID != draft.ProductReleaseID ||
		!input.ProductPublished ||
		!planChecksumPattern.MatchString(input.ProductChecksum) {
		issues = append(issues, types.ValidationIssue{
			Code: "product_release_mismatch", Field: "productReleaseId",
			Message: "product release must be published and checksum-pinned",
		})
	}
	activeAssignments := make([]types.TargetEnvironmentAssignment, 0)
	for _, assignment := range input.ActiveAssignments {
		if assignment.OrganizationID != draft.OrganizationID ||
			assignment.DeploymentTargetID != input.Assignment.DeploymentTargetID ||
			assignment.ActiveFrom.After(input.EffectiveAt) ||
			(assignment.ActiveUntil != nil && !assignment.ActiveUntil.After(input.EffectiveAt)) {
			continue
		}
		activeAssignments = append(activeAssignments, assignment)
	}
	switch len(activeAssignments) {
	case 0:
		issues = append(issues, types.ValidationIssue{
			Code: "environment_assignment_inactive", Field: "environmentAssignmentId",
			Message: "the selected environment assignment is not active",
		})
	case 1:
	default:
		issues = append(issues, types.ValidationIssue{
			Code: "ambiguous_environment_assignment", Field: "environmentAssignmentId",
			Message: "the selected target has more than one active environment assignment",
		})
	}
	if input.Assignment.ID != draft.EnvironmentAssignmentID ||
		input.Assignment.OrganizationID != draft.OrganizationID {
		issues = append(issues, types.ValidationIssue{
			Code: "foreign_environment_assignment", Field: "environmentAssignmentId",
			Message: "environment assignment does not belong to the draft organization",
		})
	}
	activeUnits := make([]types.DeploymentUnit, 0)
	for _, unit := range input.ActiveUnits {
		if unit.OrganizationID == draft.OrganizationID &&
			unit.ID == draft.DeploymentUnitID &&
			unit.TargetEnvironmentAssignmentID == draft.EnvironmentAssignmentID &&
			unit.RetiredAt == nil {
			activeUnits = append(activeUnits, unit)
		}
	}
	switch len(activeUnits) {
	case 0:
		issues = append(issues, types.ValidationIssue{
			Code: "deployment_unit_inactive", Field: "deploymentUnitId",
			Message: "the selected deployment unit is not active for the assignment",
		})
	case 1:
	default:
		issues = append(issues, types.ValidationIssue{
			Code: "ambiguous_deployment_unit", Field: "deploymentUnitId",
			Message: "the selected placement resolves to more than one deployment unit",
		})
	}
	if input.Unit.ID != draft.DeploymentUnitID ||
		input.Unit.OrganizationID != draft.OrganizationID ||
		input.Unit.TargetEnvironmentAssignmentID != draft.EnvironmentAssignmentID ||
		input.Unit.DeploymentTargetID != input.Assignment.DeploymentTargetID {
		issues = append(issues, types.ValidationIssue{
			Code: "deployment_unit_assignment_mismatch", Field: "deploymentUnitId",
			Message: "deployment unit, target, and environment assignment must match exactly",
		})
	}
	if input.Unit.RetiredAt != nil ||
		(input.Unit.ManagementState != types.RegistryManagementStateManaged &&
			input.Unit.ManagementState != types.RegistryManagementStateLegacyCutover) {
		issues = append(issues, types.ValidationIssue{
			Code: "deployment_unit_not_plannable", Field: "deploymentUnitId",
			Message: "deployment unit management state is not plannable",
		})
	}
	return issues
}

func validateTargetConfig(
	draft types.PlanDraft,
	input types.PlanResolutionInput,
) []types.ValidationIssue {
	config := input.Config
	issues := make([]types.ValidationIssue, 0)
	if config.ID != draft.TargetConfigSnapshotID {
		issues = append(issues, types.ValidationIssue{
			Code: "target_config_mismatch", Field: "targetConfigSnapshotId",
			Message: "target config snapshot identity does not match the draft",
		})
	}
	if config.OrganizationID != draft.OrganizationID {
		issues = append(issues, types.ValidationIssue{
			Code: "foreign_target_config", Field: "targetConfigSnapshotId",
			Message: "target config snapshot belongs to another organization",
		})
	}
	if config.DeploymentUnitID != draft.DeploymentUnitID {
		issues = append(issues, types.ValidationIssue{
			Code: "target_config_unit_mismatch", Field: "targetConfigSnapshotId",
			Message: "target config snapshot belongs to another deployment unit",
		})
	}
	if config.EnvironmentAssignmentID != draft.EnvironmentAssignmentID ||
		config.EnvironmentID != input.Assignment.EnvironmentID {
		issues = append(issues, types.ValidationIssue{
			Code: "target_config_environment_mismatch", Field: "targetConfigSnapshotId",
			Message: "target config snapshot belongs to another environment assignment",
		})
	}
	if string(input.TargetPlatform) != strings.TrimSpace(config.TargetPlatform) {
		issues = append(issues, types.ValidationIssue{
			Code: "deployment_target_platform_mismatch", Field: "targetConfigSnapshotId",
			Message: "target config platform does not match the assigned deployment target",
		})
	}
	if !planChecksumPattern.MatchString(config.CanonicalChecksum) {
		issues = append(issues, types.ValidationIssue{
			Code: "target_config_checksum_invalid", Field: "targetConfigSnapshotId",
			Message: "target config snapshot checksum is not canonical sha256",
		})
	}
	for _, platform := range normalizedStrings(input.RequiredPlatforms) {
		if platform != strings.TrimSpace(config.TargetPlatform) {
			issues = append(issues, types.ValidationIssue{
				Code: "target_platform_mismatch", Field: "targetConfigSnapshotId",
				Message: "target config platform does not satisfy the product release",
			})
			break
		}
	}
	for _, fact := range input.Config.VerificationFacts {
		if !fact.Verified ||
			!planChecksumPattern.MatchString(fact.Checksum) ||
			(fact.ObservedChecksum != "" && fact.ObservedChecksum != fact.Checksum) {
			issues = append(issues, types.ValidationIssue{
				Code: "target_config_unverified", Field: "targetConfigSnapshotId",
				Message: "every target config object must have an exact verified checksum",
			})
			break
		}
	}
	if len(input.Config.VerificationFacts) == 0 {
		issues = append(issues, types.ValidationIssue{
			Code: "target_config_unverified", Field: "targetConfigSnapshotId",
			Message: "target config snapshot has no object verification facts",
		})
	}
	instanceByID := make(map[uuid.UUID]types.ComponentInstance, len(input.ComponentInstances))
	for _, instance := range input.ComponentInstances {
		instanceByID[instance.ID] = instance
	}
	seenComponentKeys := make(map[string]struct{}, len(input.Config.ComponentBindings))
	for _, binding := range input.Config.ComponentBindings {
		componentKey := strings.TrimSpace(binding.ComponentKey)
		if _, duplicate := seenComponentKeys[componentKey]; duplicate {
			issues = append(issues, types.ValidationIssue{
				Code: "ambiguous_component_instance", Field: "targetConfigSnapshotId",
				Message: "target config must bind each component key to exactly one physical instance",
			})
			break
		}
		seenComponentKeys[componentKey] = struct{}{}
		instance, ok := instanceByID[binding.ComponentInstanceID]
		if !ok ||
			instance.OrganizationID != draft.OrganizationID ||
			instance.DeploymentUnitID != draft.DeploymentUnitID ||
			instance.RetiredAt != nil ||
			strings.TrimSpace(instance.PhysicalName) != strings.TrimSpace(binding.PhysicalName) {
			issues = append(issues, types.ValidationIssue{
				Code: "component_instance_mismatch", Field: "targetConfigSnapshotId",
				Message: "target config component binding does not match the physical registry instance",
			})
			break
		}
	}
	return issues
}

func validateReleasePins(
	_ types.PlanDraft,
	input types.PlanResolutionInput,
) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	configBindings := make(map[string]types.ConfigComponentBinding, len(input.Config.ComponentBindings))
	for _, binding := range input.Config.ComponentBindings {
		configBindings[strings.TrimSpace(binding.ComponentKey)] = binding
	}
	instances := make(map[uuid.UUID]types.ComponentInstance, len(input.ComponentInstances))
	for _, instance := range input.ComponentInstances {
		instances[instance.ID] = instance
	}
	seen := make(map[string]struct{}, len(input.ReleasePins))
	for _, pin := range input.ReleasePins {
		key := strings.TrimSpace(pin.ComponentKey)
		if _, duplicate := seen[key]; duplicate {
			issues = append(issues, types.ValidationIssue{
				Code: "ambiguous_component_release", Field: "productReleaseId",
				Message: "product release contains duplicate component pins",
			})
		}
		seen[key] = struct{}{}
		if pin.ComponentReleaseID == uuid.Nil || !planChecksumPattern.MatchString(pin.ReleaseChecksum) {
			issues = append(issues, types.ValidationIssue{
				Code: "component_release_mismatch", Field: "productReleaseId",
				Message: "component release pin is missing exact identity or checksum",
			})
		}
		if !pin.ProvenanceVerified ||
			!planChecksumPattern.MatchString(pin.ProvenanceBindingChecksum) ||
			!releaseProvenanceFactsMatchArtifacts(pin) ||
			!releaseProvenanceChecksumMatches(pin) {
			issues = append(issues, types.ValidationIssue{
				Code: "component_provenance_unverified", Field: "productReleaseId",
				Message: "component release provenance is not exactly bound to every selected artifact",
			})
		}
		if !slices.Contains(normalizedStrings(pin.Platforms), input.Config.TargetPlatform) {
			issues = append(issues, types.ValidationIssue{
				Code: "component_platform_mismatch", Field: "productReleaseId",
				Message: "component release has no artifact for the selected target platform",
			})
		}
		binding, ok := configBindings[key]
		if !ok {
			issues = append(issues, types.ValidationIssue{
				Code: "config_release_component_mismatch", Field: "targetConfigSnapshotId",
				Message: "target config has no physical binding for a pinned component release",
			})
			continue
		}
		instance, ok := instances[binding.ComponentInstanceID]
		if !ok ||
			(instance.ManagementState != types.RegistryManagementStateManaged &&
				instance.ManagementState != types.RegistryManagementStateLegacyCutover) {
			issues = append(issues, types.ValidationIssue{
				Code: "component_instance_not_deployable", Field: "targetConfigSnapshotId",
				Message: "pinned product components require managed or legacy-cutover physical instances",
			})
		}
	}
	return issues
}

func matchingTargetCandidates(
	requirement types.TargetRequirement,
	candidates []types.RequirementProviderCandidate,
) []types.RequirementProviderCandidate {
	constraint, err := semver.NewConstraint(strings.TrimSpace(requirement.VersionRange))
	if err != nil {
		return nil
	}
	allowed := make(map[types.RequirementResolutionMode]struct{}, len(requirement.AllowedModes))
	for _, mode := range requirement.AllowedModes {
		allowed[mode] = struct{}{}
	}
	matches := make([]types.RequirementProviderCandidate, 0)
	for _, candidate := range candidates {
		if candidate.RequirementKey != requirement.Key {
			continue
		}
		if _, ok := allowed[candidate.Mode]; !ok {
			continue
		}
		if candidate.Mode != types.RequirementResolutionModeFeatureDisabled {
			version, versionErr := semver.StrictNewVersion(strings.TrimSpace(candidate.ProviderVersion))
			if versionErr != nil || !constraint.Check(version) {
				continue
			}
		}
		matches = append(matches, candidate)
	}
	return matches
}

func validateCandidateBinding(
	requirement types.TargetRequirement,
	candidate types.RequirementProviderCandidate,
	draft types.PlanDraft,
) *types.ValidationIssue {
	field := "requirements." + requirement.Key
	if candidate.ExpectedStateVersion != candidate.ObservedStateVersion ||
		candidate.ExpectedStateChecksum != candidate.ObservedStateChecksum ||
		!planChecksumPattern.MatchString(candidate.ExpectedStateChecksum) {
		return &types.ValidationIssue{
			Code: "stale_expected_state", Field: field,
			Message: "provider observed state no longer matches the exact expected state",
		}
	}
	if candidate.ProviderPlatform != draft.ResolutionInput.Config.TargetPlatform {
		return &types.ValidationIssue{
			Code: "provider_platform_mismatch", Field: field,
			Message: "provider platform does not match the target config platform",
		}
	}
	switch candidate.Mode {
	case types.RequirementResolutionModeIncluded:
		if candidate.ProviderReleaseID == nil ||
			candidate.ComponentInstanceID == nil ||
			candidate.DeploymentUnitID != draft.DeploymentUnitID ||
			!planChecksumPattern.MatchString(candidate.ProviderReleaseChecksum) ||
			!planChecksumPattern.MatchString(candidate.ProvenanceBindingChecksum) ||
			!candidate.ProvenanceVerified ||
			!containsExactReleasePin(
				draft.ResolutionInput.ReleasePins,
				*candidate.ProviderReleaseID,
				candidate.ProviderReleaseChecksum,
				candidate.ProvenanceBindingChecksum,
			) {
			return invalidBinding(field, "included provider is not an exact pinned component instance")
		}
	case types.RequirementResolutionModePinnedExisting:
		if candidate.ProviderReleaseID == nil ||
			candidate.ComponentInstanceID == nil ||
			candidate.ObservationID == nil ||
			candidate.DeploymentUnitID != draft.DeploymentUnitID ||
			!planChecksumPattern.MatchString(candidate.ProviderReleaseChecksum) ||
			!planChecksumPattern.MatchString(candidate.ProvenanceBindingChecksum) ||
			!candidate.ProvenanceVerified {
			return invalidBinding(field, "pinned existing provider requires exact release, instance, and observation")
		}
	case types.RequirementResolutionModeSharedProvider:
		if candidate.ProviderReleaseID == nil ||
			candidate.ObservationID == nil ||
			candidate.DeploymentUnitID == uuid.Nil ||
			candidate.DeploymentUnitID == draft.DeploymentUnitID ||
			!planChecksumPattern.MatchString(candidate.SubscriberSetChecksum) ||
			!planChecksumPattern.MatchString(candidate.ProviderReleaseChecksum) ||
			!planChecksumPattern.MatchString(candidate.ProvenanceBindingChecksum) ||
			!candidate.ProvenanceVerified {
			return invalidBinding(field, "shared provider requires exact shared unit, subscriber set, release, and observation")
		}
	case types.RequirementResolutionModeApprovedExternal:
		if candidate.ObservationID == nil ||
			(candidate.ProviderReleaseID != nil &&
				(!planChecksumPattern.MatchString(candidate.ProviderReleaseChecksum) ||
					!planChecksumPattern.MatchString(candidate.ProvenanceBindingChecksum))) ||
			!candidate.ProvenanceVerified {
			return invalidBinding(field, "approved external provider requires trusted approval evidence")
		}
	case types.RequirementResolutionModeFeatureDisabled:
		enabled, present := draft.ResolutionInput.Config.FeatureFlags[candidate.FeatureFlagKey]
		if candidate.FeatureFlagKey == "" ||
			candidate.FeatureFlagEnabled ||
			!present ||
			enabled {
			return invalidBinding(field, "feature-disabled binding requires a frozen disabled feature flag")
		}
	default:
		return invalidBinding(field, "unsupported requirement resolution mode")
	}
	return nil
}

func invalidBinding(field, message string) *types.ValidationIssue {
	return &types.ValidationIssue{Code: "invalid_requirement_binding", Field: field, Message: message}
}

func containsExactReleasePin(
	pins []types.ComponentReleasePin,
	id uuid.UUID,
	releaseChecksum string,
	provenanceBindingChecksum string,
) bool {
	for _, pin := range pins {
		if pin.ComponentReleaseID == id &&
			pin.ReleaseChecksum == releaseChecksum &&
			pin.ProvenanceBindingChecksum == provenanceBindingChecksum {
			return true
		}
	}
	return false
}

func releaseProvenanceFactsMatchArtifacts(pin types.ComponentReleasePin) bool {
	if len(pin.Artifacts) == 0 || len(pin.ProvenanceFacts) != len(pin.Artifacts) {
		return false
	}
	facts := make(map[string]types.ComponentProvenanceFact, len(pin.ProvenanceFacts))
	for _, fact := range pin.ProvenanceFacts {
		key := fact.ArtifactKey + "\x00" + fact.Platform
		if fact.VerificationID == uuid.Nil ||
			!planChecksumPattern.MatchString(fact.ArtifactDigest) ||
			!planChecksumPattern.MatchString(fact.EvidenceDigest) ||
			!planChecksumPattern.MatchString(fact.PolicyChecksum) ||
			strings.TrimSpace(fact.TrustRootID) == "" {
			return false
		}
		if _, duplicate := facts[key]; duplicate {
			return false
		}
		facts[key] = fact
	}
	for _, artifact := range pin.Artifacts {
		key := artifact.Key + "\x00" + artifact.Platform
		fact, ok := facts[key]
		if !ok || fact.ArtifactDigest != artifact.PlatformDigest {
			return false
		}
	}
	return true
}

func releaseProvenanceChecksumMatches(pin types.ComponentReleasePin) bool {
	facts := slices.Clone(pin.ProvenanceFacts)
	slices.SortFunc(facts, func(a, b types.ComponentProvenanceFact) int {
		if cmp := strings.Compare(a.ArtifactKey, b.ArtifactKey); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.Platform, b.Platform); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.VerificationID.String(), b.VerificationID.String())
	})
	checksum, err := canonicalChecksum(facts)
	return err == nil && checksum == pin.ProvenanceBindingChecksum
}

func compareRequirementCandidates(a, b types.RequirementProviderCandidate) int {
	keysA := []string{
		a.RequirementKey, string(a.Mode), uuidString(a.ProviderReleaseID),
		uuidString(a.ObservationID), a.ProviderVersion, a.DeploymentUnitID.String(),
		uuidString(a.ComponentInstanceID),
	}
	keysB := []string{
		b.RequirementKey, string(b.Mode), uuidString(b.ProviderReleaseID),
		uuidString(b.ObservationID), b.ProviderVersion, b.DeploymentUnitID.String(),
		uuidString(b.ComponentInstanceID),
	}
	for index := range keysA {
		if cmp := strings.Compare(keysA[index], keysB[index]); cmp != 0 {
			return cmp
		}
	}
	return 0
}

func canonicalChecksum(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func normalizedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return slices.Compact(result)
}

func sortValidationIssues(issues []types.ValidationIssue) {
	slices.SortFunc(issues, func(a, b types.ValidationIssue) int {
		if cmp := strings.Compare(a.Field, b.Field); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.Code, b.Code); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Message, b.Message)
	})
}

func deduplicateValidationIssues(issues []types.ValidationIssue) []types.ValidationIssue {
	return slices.CompactFunc(issues, func(a, b types.ValidationIssue) bool {
		return a.Code == b.Code && a.Field == b.Field && a.Message == b.Message
	})
}

func cloneUUID(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func uuidString(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}
