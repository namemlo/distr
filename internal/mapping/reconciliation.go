package mapping

import (
	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

func DriftCaseToAPI(item types.DriftCase) api.DriftCase {
	return api.DriftCase{
		ID: item.ID, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		ActiveDesiredRevisionID: item.ActiveDesiredRevisionID,
		ObservationID:           item.ObservationID, DeploymentUnitID: item.DeploymentUnitID,
		ComponentInstanceID: item.ComponentInstanceID, Status: item.Status, Classes: item.Classes,
		Summary: item.Summary, AssignedTo: item.AssignedTo, ResolvedAt: item.ResolvedAt,
	}
}

func ReconciliationActionToAPI(
	item types.ReconciliationAction,
) api.ReconciliationAction {
	return api.ReconciliationAction{
		ID: item.ID, CreatedAt: item.CreatedAt, DriftCaseID: item.DriftCaseID,
		Action: item.Action, Reason: item.Reason, ActorID: item.ActorID,
		DeploymentPlanID:     item.DeploymentPlanID,
		OutcomeObservationID: item.OutcomeObservationID,
		AcceptedUntil:        item.AcceptedUntil,
	}
}
