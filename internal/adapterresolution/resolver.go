package adapterresolution

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/distr-sh/distr/internal/types"
)

var (
	adapterChecksumPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	adapterCapabilityPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
)

func ResolveStepAdapter(
	_ context.Context,
	request types.AdapterResolutionRequest,
) (*types.ResolvedStepAdapter, []types.ValidationIssue) {
	if issues := validateResolutionRequest(request); len(issues) > 0 {
		return nil, issues
	}

	scoped := make([]types.AdapterAssignment, 0)
	for _, source := range request.Assignments {
		assignment := source
		assignment.NormalizeKeyConfiguration()
		if assignment.OrganizationID == request.OrganizationID &&
			assignment.ScopeType == request.ScopeType &&
			assignment.ScopeID == request.ScopeID &&
			assignment.ConfigSnapshotID == request.TargetConfigSnapshotID {
			scoped = append(scoped, assignment)
		}
	}
	if len(scoped) == 0 {
		return nil, adapterIssue(
			"adapter_assignment_missing",
			"scope",
			"no adapter assignment matches the exact target scope and config snapshot",
		)
	}

	enabled := slices.DeleteFunc(scoped, func(assignment types.AdapterAssignment) bool {
		return !assignment.Enabled
	})
	if len(enabled) == 0 {
		return nil, adapterIssue(
			"adapter_assignment_disabled",
			"assignment",
			"the exact adapter assignment is disabled",
		)
	}

	checksumMatches := slices.DeleteFunc(enabled, func(assignment types.AdapterAssignment) bool {
		return assignment.ConfigChecksum != request.TargetConfigChecksum
	})
	if len(checksumMatches) == 0 {
		return nil, adapterIssue(
			"adapter_config_checksum_mismatch",
			"configChecksum",
			"adapter assignment config checksum does not match the immutable target config snapshot",
		)
	}

	type match struct {
		implementation types.AdapterImplementation
		assignment     types.AdapterAssignment
	}
	matches := make([]match, 0)
	for _, assignment := range checksumMatches {
		if !validKeyConfiguration(assignment.KeyConfiguration) {
			continue
		}
		for _, implementation := range request.Implementations {
			if implementation.ID != assignment.AdapterImplementationID ||
				implementation.OrganizationID != request.OrganizationID ||
				!implementation.Enabled ||
				!implementationProvides(
					implementation,
					request.RequiredCapability,
					request.RequiredCapabilityVersion,
				) {
				continue
			}
			matches = append(matches, match{implementation: implementation, assignment: assignment})
		}
	}

	switch len(matches) {
	case 0:
		return nil, adapterIssue(
			"adapter_implementation_missing",
			"requiredCapability",
			"no enabled adapter implementation provides the exact release capability and version",
		)
	case 1:
	default:
		return nil, adapterIssue(
			"adapter_implementation_ambiguous",
			"requiredCapability",
			"more than one adapter implementation provides the exact release capability and version",
		)
	}

	selected := matches[0]
	return &types.ResolvedStepAdapter{
		AdapterAssignmentID:     selected.assignment.ID,
		AdapterImplementationID: selected.implementation.ID,
		ImplementationVersion:   selected.implementation.Version,
		Capability:              request.RequiredCapability,
		CapabilityVersion:       request.RequiredCapabilityVersion,
		ScopeType:               request.ScopeType,
		ScopeID:                 request.ScopeID,
		ConfigSnapshotID:        request.TargetConfigSnapshotID,
		ConfigChecksum:          selected.assignment.ConfigChecksum,
		KeyConfiguration:        selected.assignment.KeyConfiguration,
	}, nil
}

func validateResolutionRequest(request types.AdapterResolutionRequest) []types.ValidationIssue {
	issues := make([]types.ValidationIssue, 0)
	requiredIDs := []struct {
		field string
		empty bool
	}{
		{"organizationId", request.OrganizationID.String() == "00000000-0000-0000-0000-000000000000"},
		{"scopeId", request.ScopeID.String() == "00000000-0000-0000-0000-000000000000"},
		{"targetConfigSnapshotId", request.TargetConfigSnapshotID.String() == "00000000-0000-0000-0000-000000000000"},
	}
	for _, required := range requiredIDs {
		if required.empty {
			issues = append(issues, types.ValidationIssue{
				Code: "required", Field: required.field, Message: required.field + " is required",
			})
		}
	}
	if strings.TrimSpace(request.StepKey) == "" {
		issues = append(issues, types.ValidationIssue{
			Code: "required", Field: "stepKey", Message: "stepKey is required",
		})
	}
	if !adapterCapabilityPattern.MatchString(request.RequiredCapability) {
		issues = append(issues, types.ValidationIssue{
			Code: "invalid_adapter_capability", Field: "requiredCapability",
			Message: "required adapter capability must be a stable lowercase key",
		})
	}
	if _, err := semver.StrictNewVersion(request.RequiredCapabilityVersion); err != nil {
		issues = append(issues, types.ValidationIssue{
			Code: "invalid_adapter_capability_version", Field: "requiredCapabilityVersion",
			Message: "required adapter capability version must be strict semantic version",
		})
	}
	if !request.ScopeType.IsValid() {
		issues = append(issues, types.ValidationIssue{
			Code: "invalid_adapter_scope", Field: "scopeType", Message: "adapter scope type is invalid",
		})
	}
	if !adapterChecksumPattern.MatchString(request.TargetConfigChecksum) {
		issues = append(issues, types.ValidationIssue{
			Code: "invalid_adapter_config_checksum", Field: "targetConfigChecksum",
			Message: "target config checksum must be canonical lowercase sha256",
		})
	}
	return issues
}

func implementationProvides(
	implementation types.AdapterImplementation,
	capability string,
	version string,
) bool {
	for _, provided := range implementation.Capabilities {
		if provided.Capability == capability && provided.Version == version {
			return true
		}
	}
	return false
}

func validKeyConfiguration(config types.AdapterKeyConfiguration) bool {
	return strings.TrimSpace(config.KeyID) != "" &&
		adapterChecksumPattern.MatchString(config.PublicKeyFingerprint) &&
		strings.TrimSpace(config.SigningKeyReference) != "" &&
		adapterChecksumPattern.MatchString(config.SigningKeyVersionFingerprint)
}

func adapterIssue(code, field, message string) []types.ValidationIssue {
	return []types.ValidationIssue{{Code: code, Field: field, Message: message}}
}

type runtimeStateContextKey struct{}

func WithRuntimeState(ctx context.Context, state types.AdapterRuntimeState) context.Context {
	return context.WithValue(ctx, runtimeStateContextKey{}, state)
}

func VerifyAdapterAtStart(
	ctx context.Context,
	frozen types.DeploymentPlanStepAdapter,
) error {
	current, ok := ctx.Value(runtimeStateContextKey{}).(types.AdapterRuntimeState)
	if !ok {
		return fmt.Errorf("current adapter runtime state is required")
	}
	if !current.Enabled {
		return fmt.Errorf("adapter assignment is disabled")
	}
	if current.ImplementationVersion != frozen.ImplementationVersion {
		return fmt.Errorf(
			"adapter implementation version changed from %q to %q",
			frozen.ImplementationVersion,
			current.ImplementationVersion,
		)
	}
	if current.AdapterAssignmentID != frozen.AdapterAssignmentID ||
		current.AdapterImplementationID != frozen.AdapterImplementationID {
		return fmt.Errorf("adapter identity changed after plan publication")
	}
	if current.Capability != frozen.Capability ||
		current.CapabilityVersion != frozen.CapabilityVersion {
		return fmt.Errorf("adapter capability changed after plan publication")
	}
	if current.ScopeType != frozen.ScopeType || current.ScopeID != frozen.ScopeID {
		return fmt.Errorf("adapter target scope changed after plan publication")
	}
	if current.ConfigSnapshotID != frozen.ConfigSnapshotID ||
		current.ConfigChecksum != frozen.ConfigChecksum {
		return fmt.Errorf("adapter config snapshot changed after plan publication")
	}
	if current.KeyConfiguration != frozen.KeyConfiguration {
		return fmt.Errorf("adapter signing-key fingerprint changed after plan publication")
	}
	return nil
}
