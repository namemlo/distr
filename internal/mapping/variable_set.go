package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func VariableSetToAPI(variableSet types.VariableSet) api.VariableSet {
	return api.VariableSet{
		ID:             variableSet.ID,
		CreatedAt:      variableSet.CreatedAt,
		UpdatedAt:      variableSet.UpdatedAt,
		Name:           variableSet.Name,
		Description:    variableSet.Description,
		SortOrder:      variableSet.SortOrder,
		ApplicationIDs: variableSet.ApplicationIDs,
		Variables:      List(variableSet.Variables, VariableToAPI),
	}
}

func VariableToAPI(variable types.Variable) api.Variable {
	return api.Variable{
		ID:            variable.ID,
		CreatedAt:     variable.CreatedAt,
		UpdatedAt:     variable.UpdatedAt,
		Key:           variable.Key,
		Description:   variable.Description,
		Type:          api.VariableType(variable.Type),
		IsRequired:    variable.IsRequired,
		DefaultValue:  variable.DefaultValue,
		ReferenceID:   variable.ReferenceID,
		ReferenceName: variable.ReferenceName,
		ScopedValues:  List(variable.ScopedValues, VariableScopedValueToAPI),
	}
}

func VariableScopedValueToAPI(scopedValue types.VariableScopedValue) api.VariableScopedValue {
	return api.VariableScopedValue{
		ID:            scopedValue.ID,
		CreatedAt:     scopedValue.CreatedAt,
		UpdatedAt:     scopedValue.UpdatedAt,
		Scope:         VariableScopeToAPI(scopedValue.Scope),
		SortOrder:     scopedValue.SortOrder,
		Value:         scopedValue.Value,
		ReferenceID:   scopedValue.ReferenceID,
		ReferenceName: scopedValue.ReferenceName,
	}
}

func VariableScopeToAPI(scope types.VariableScope) api.VariableScope {
	return api.VariableScope{
		CustomerOrganizationID: scope.CustomerOrganizationID,
		EnvironmentID:          scope.EnvironmentID,
		ChannelID:              scope.ChannelID,
		DeploymentTargetID:     scope.DeploymentTargetID,
		ApplicationID:          scope.ApplicationID,
		TargetTag:              scope.TargetTag,
		ProcessStepKey:         scope.ProcessStepKey,
	}
}

func ResolvedVariableToAPI(variable types.ResolvedVariable) api.ResolvedVariable {
	return api.ResolvedVariable{
		VariableSetID: variable.VariableSetID,
		VariableID:    variable.VariableID,
		Key:           variable.Key,
		Type:          api.VariableType(variable.Type),
		IsRequired:    variable.IsRequired,
		Status:        string(variable.Status),
		Source:        string(variable.Source),
		Value:         variable.Value,
		ReferenceID:   variable.ReferenceID,
		ReferenceName: variable.ReferenceName,
		Redacted:      variable.Redacted,
		Trace:         List(variable.Trace, VariableResolutionTraceEntryToAPI),
	}
}

func VariableResolutionTraceEntryToAPI(entry types.VariableResolutionTraceEntry) api.VariableResolutionTraceEntry {
	return api.VariableResolutionTraceEntry{
		Source:   string(entry.Source),
		Scope:    VariableScopeToAPI(entry.Scope),
		Selected: entry.Selected,
		Reason:   entry.Reason,
	}
}
