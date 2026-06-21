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
	}
}
