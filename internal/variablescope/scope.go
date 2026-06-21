package variablescope

import (
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const (
	scopeTenant = 1 << iota
	scopeEnvironment
	scopeChannel
	scopeTarget
	scopeApplication
	scopeTargetTag
	scopeStep
)

type shape struct {
	mask   int
	source types.VariableResolutionSource
	rank   int
}

var shapes = []shape{
	{
		mask:   scopeTenant | scopeEnvironment | scopeTarget | scopeChannel | scopeStep,
		source: types.VariableResolutionSourceExactTenantEnvironmentTargetChannelStep,
		rank:   2,
	},
	{
		mask:   scopeTenant | scopeEnvironment | scopeTarget,
		source: types.VariableResolutionSourceExactTenantEnvironmentTarget,
		rank:   3,
	},
	{
		mask:   scopeTenant | scopeEnvironment | scopeChannel,
		source: types.VariableResolutionSourceExactTenantEnvironmentChannel,
		rank:   4,
	},
	{
		mask:   scopeTenant | scopeEnvironment,
		source: types.VariableResolutionSourceExactTenantEnvironment,
		rank:   5,
	},
	{
		mask:   scopeEnvironment | scopeTargetTag,
		source: types.VariableResolutionSourceExactEnvironmentTargetTag,
		rank:   6,
	},
	{
		mask:   scopeEnvironment,
		source: types.VariableResolutionSourceExactEnvironment,
		rank:   7,
	},
	{
		mask:   scopeChannel,
		source: types.VariableResolutionSourceChannel,
		rank:   8,
	},
	{
		mask:   scopeApplication,
		source: types.VariableResolutionSourceApplication,
		rank:   9,
	},
}

func Supported(scope types.VariableScope) bool {
	_, _, ok := Source(scope)
	return ok
}

func Source(scope types.VariableScope) (types.VariableResolutionSource, int, bool) {
	mask := Mask(scope)
	for _, candidate := range shapes {
		if candidate.mask == mask {
			return candidate.source, candidate.rank, true
		}
	}
	return "", 0, false
}

func Mask(scope types.VariableScope) int {
	mask := 0
	if scope.CustomerOrganizationID != nil {
		mask |= scopeTenant
	}
	if scope.EnvironmentID != nil {
		mask |= scopeEnvironment
	}
	if scope.ChannelID != nil {
		mask |= scopeChannel
	}
	if scope.DeploymentTargetID != nil {
		mask |= scopeTarget
	}
	if scope.ApplicationID != nil {
		mask |= scopeApplication
	}
	if strings.TrimSpace(scope.TargetTag) != "" {
		mask |= scopeTargetTag
	}
	if strings.TrimSpace(scope.ProcessStepKey) != "" {
		mask |= scopeStep
	}
	return mask
}

func Key(scope types.VariableScope) string {
	parts := []string{
		uuidScopeKey(scope.CustomerOrganizationID),
		uuidScopeKey(scope.EnvironmentID),
		uuidScopeKey(scope.DeploymentTargetID),
		uuidScopeKey(scope.ChannelID),
		uuidScopeKey(scope.ApplicationID),
		strings.TrimSpace(scope.TargetTag),
		strings.TrimSpace(scope.ProcessStepKey),
	}
	return strings.Join(parts, "|")
}

func uuidScopeKey(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}
