package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func DeploymentPlanDraftToAPI(draft types.PlanDraft) api.DeploymentPlanDraft {
	return api.DeploymentPlanDraft{
		ID:                            draft.ID,
		CreatedAt:                     draft.CreatedAt,
		UpdatedAt:                     draft.UpdatedAt,
		CreatedByUserAccountID:        draft.CreatedByUserAccountID,
		UpdatedByUserAccountID:        draft.UpdatedByUserAccountID,
		Revision:                      draft.Revision,
		ProductReleaseID:              draft.ProductReleaseID,
		DeploymentUnitID:              draft.DeploymentUnitID,
		EnvironmentAssignmentID:       draft.EnvironmentAssignmentID,
		TargetConfigSnapshotID:        draft.TargetConfigSnapshotID,
		ProtocolVersion:               draft.ProtocolVersion,
		SupersedesDeploymentPlanID:    draft.SupersedesDeploymentPlanID,
		SupersedeReason:               draft.SupersedeReason,
		PreviewChecksum:               draft.PreviewChecksum,
		PublishedDeploymentPlanID:     draft.PublishedDeploymentPlanID,
		PublishedDeploymentPlanStatus: draft.PublishedDeploymentPlanStatus,
	}
}

func DeploymentPlanDraftValidationToAPI(
	validation types.PlanDraftValidation,
) api.DeploymentPlanDraftValidation {
	return api.DeploymentPlanDraftValidation{
		Draft:           DeploymentPlanDraftToAPI(validation.Draft),
		Resolutions:     validation.Resolutions,
		Graph:           validation.Graph,
		Issues:          validation.Issues,
		PreviewChecksum: validation.PreviewChecksum,
	}
}
