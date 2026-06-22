package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func StepTemplateToAPI(template types.StepTemplate) api.StepTemplate {
	return api.StepTemplate{
		ID:                       template.ID,
		CreatedAt:                template.CreatedAt,
		UpdatedAt:                template.UpdatedAt,
		SourceType:               string(template.SourceType),
		SourceRef:                template.SourceRef,
		Name:                     template.Name,
		Description:              template.Description,
		Category:                 template.Category,
		InstalledAt:              template.InstalledAt,
		InstalledByUserAccountID: template.InstalledByUserAccountID,
		Versions:                 List(template.Versions, StepTemplateVersionToAPI),
	}
}

func StepTemplateVersionToAPI(version types.StepTemplateVersion) api.StepTemplateVersion {
	return api.StepTemplateVersion{
		ID:                        version.ID,
		CreatedAt:                 version.CreatedAt,
		StepTemplateID:            version.StepTemplateID,
		Version:                   version.Version,
		ActionType:                version.ActionType,
		ExecutionLocation:         version.ExecutionLocation,
		InputSchema:               version.InputSchema,
		OutputSchema:              version.OutputSchema,
		DefaultInputBindings:      version.DefaultInputBindings,
		MinimumAgentVersion:       version.MinimumAgentVersion,
		CompatibleActionVersion:   version.CompatibleActionVersion,
		RuntimeCompatibilityNotes: version.RuntimeCompatibilityNotes,
		Deprecated:                version.Deprecated,
	}
}
