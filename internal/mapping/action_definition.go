package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func ActionDefinitionToAPI(action types.ActionDefinition) api.ActionDefinition {
	return api.ActionDefinition{
		Type:         action.Type,
		Name:         action.Name,
		Description:  action.Description,
		InputSchema:  action.InputSchema,
		OutputSchema: action.OutputSchema,
	}
}
