package variableresolution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/variablescope"
	"github.com/google/uuid"
)

type Request struct {
	Variables      []types.Variable
	Scope          types.VariableResolutionScope
	PromptedValues []types.VariablePromptedValue
}

type candidate struct {
	source        types.VariableResolutionSource
	rank          int
	sortOrder     int
	scope         types.VariableScope
	value         json.RawMessage
	referenceID   string
	referenceName string
}

func Resolve(request Request) ([]types.ResolvedVariable, error) {
	prompted, err := promptedValuesByKey(request.PromptedValues)
	if err != nil {
		return nil, err
	}

	result := make([]types.ResolvedVariable, 0, len(request.Variables))
	for _, variable := range request.Variables {
		result = append(result, resolveVariable(variable, request.Scope, prompted[strings.TrimSpace(variable.Key)]))
	}
	return result, nil
}

func promptedValuesByKey(values []types.VariablePromptedValue) (map[string]*types.VariablePromptedValue, error) {
	result := map[string]*types.VariablePromptedValue{}
	for i := range values {
		values[i].Key = strings.TrimSpace(values[i].Key)
		if values[i].Key == "" {
			return nil, fmt.Errorf("prompted variable key is required")
		}
		if _, ok := result[values[i].Key]; ok {
			return nil, fmt.Errorf("prompted variable keys must be unique")
		}
		result[values[i].Key] = &values[i]
	}
	return result, nil
}

func resolveVariable(
	variable types.Variable,
	scope types.VariableResolutionScope,
	promptedValue *types.VariablePromptedValue,
) types.ResolvedVariable {
	candidates := make([]candidate, 0, len(variable.ScopedValues)+2)
	if promptedValue != nil {
		candidates = append(candidates, candidate{
			source:        types.VariableResolutionSourcePrompted,
			rank:          1,
			value:         promptedValue.Value,
			referenceID:   promptedValue.ReferenceID,
			referenceName: promptedValue.ReferenceName,
		})
	}
	for _, scopedValue := range variable.ScopedValues {
		source, rank, ok := variablescope.Source(scopedValue.Scope)
		if !ok || !scopeMatches(scopedValue.Scope, scope) {
			continue
		}
		candidates = append(candidates, candidate{
			source:        source,
			rank:          rank,
			sortOrder:     scopedValue.SortOrder,
			scope:         scopedValue.Scope,
			value:         scopedValue.Value,
			referenceID:   scopedValue.ReferenceID,
			referenceName: scopedValue.ReferenceName,
		})
	}
	if hasResolutionDefault(variable) {
		candidates = append(candidates, candidate{
			source:        types.VariableResolutionSourceDefault,
			rank:          10,
			value:         variable.DefaultValue,
			referenceID:   variable.ReferenceID,
			referenceName: variable.ReferenceName,
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		if candidates[i].sortOrder != candidates[j].sortOrder {
			return candidates[i].sortOrder < candidates[j].sortOrder
		}
		return variablescope.Key(candidates[i].scope) < variablescope.Key(candidates[j].scope)
	})

	resolved := types.ResolvedVariable{
		VariableSetID: variable.VariableSetID,
		VariableID:    variable.ID,
		Key:           strings.TrimSpace(variable.Key),
		Type:          variable.Type,
		IsRequired:    variable.IsRequired,
	}
	if len(candidates) == 0 {
		resolved.Status = types.VariableResolutionStatusUnresolved
		resolved.Source = types.VariableResolutionSourceUnresolved
		resolved.Trace = []types.VariableResolutionTraceEntry{{
			Source:   types.VariableResolutionSourceUnresolved,
			Selected: true,
			Reason:   "no prompted, scoped, or default value matched",
		}}
		return resolved
	}

	selected := candidates[0]
	resolved.Status = types.VariableResolutionStatusResolved
	resolved.Source = selected.source
	resolved.ReferenceID = selected.referenceID
	resolved.ReferenceName = selected.referenceName
	if variable.Type == types.VariableTypeSecretReference {
		resolved.Redacted = true
	} else if !isReferenceVariableType(variable.Type) {
		resolved.Value = cloneRawMessage(selected.value)
	}
	resolved.Trace = traceFromCandidates(candidates)
	return resolved
}

func hasResolutionDefault(variable types.Variable) bool {
	if isReferenceVariableType(variable.Type) {
		return strings.TrimSpace(variable.ReferenceID) != ""
	}
	return hasRawJSONValue(variable.DefaultValue)
}

func scopeMatches(valueScope types.VariableScope, requestScope types.VariableResolutionScope) bool {
	if !uuidPtrMatches(valueScope.CustomerOrganizationID, requestScope.CustomerOrganizationID) {
		return false
	}
	if !uuidPtrMatches(valueScope.EnvironmentID, requestScope.EnvironmentID) {
		return false
	}
	if !uuidPtrMatches(valueScope.ChannelID, requestScope.ChannelID) {
		return false
	}
	if !uuidPtrMatches(valueScope.DeploymentTargetID, requestScope.DeploymentTargetID) {
		return false
	}
	if !uuidPtrMatches(valueScope.ApplicationID, requestScope.ApplicationID) {
		return false
	}
	if strings.TrimSpace(valueScope.ProcessStepKey) != "" &&
		strings.TrimSpace(valueScope.ProcessStepKey) != strings.TrimSpace(requestScope.ProcessStepKey) {
		return false
	}
	if tag := strings.TrimSpace(valueScope.TargetTag); tag != "" && !containsString(requestScope.TargetTags, tag) {
		return false
	}
	return true
}

func uuidPtrMatches(value, request *uuid.UUID) bool {
	if value == nil {
		return true
	}
	return request != nil && *value == *request
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == expected {
			return true
		}
	}
	return false
}

func traceFromCandidates(candidates []candidate) []types.VariableResolutionTraceEntry {
	trace := make([]types.VariableResolutionTraceEntry, 0, len(candidates))
	for i, candidate := range candidates {
		trace = append(trace, types.VariableResolutionTraceEntry{
			Source:   candidate.source,
			Scope:    candidate.scope,
			Selected: i == 0,
			Reason:   "matched resolution scope",
		})
	}
	return trace
}

func isReferenceVariableType(value types.VariableType) bool {
	switch value {
	case types.VariableTypeSecretReference, types.VariableTypeAccountReference, types.VariableTypeCertificateReference:
		return true
	default:
		return false
	}
}

func hasRawJSONValue(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	clone := make([]byte, len(value))
	copy(clone, value)
	return clone
}
