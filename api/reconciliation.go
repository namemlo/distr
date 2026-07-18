package api

import (
	"slices"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type ReconciliationDecisionRequest struct {
	Action               types.ReconciliationActionType `json:"action"`
	Reason               string                         `json:"reason"`
	DeploymentPlanID     *uuid.UUID                     `json:"deploymentPlanId,omitempty"`
	OutcomeObservationID *uuid.UUID                     `json:"outcomeObservationId,omitempty"`
	AcceptedUntil        *time.Time                     `json:"acceptedUntil,omitempty"`
}

func (r ReconciliationDecisionRequest) Validate(now time.Time) error {
	if !slices.Contains([]types.ReconciliationActionType{
		types.ReconciliationActionRestoreDesired,
		types.ReconciliationActionCreatePlan,
		types.ReconciliationActionAcceptDeviation,
		types.ReconciliationActionCloseWithEvidence,
	}, r.Action) {
		return validation.NewValidationFailedError("reconciliation action is invalid")
	}
	reason := strings.TrimSpace(r.Reason)
	if reason == "" || len(reason) > 2048 || strings.ContainsAny(r.Reason, "\r\n") {
		return validation.NewValidationFailedError("reconciliation reason is invalid")
	}
	if r.Action == types.ReconciliationActionCreatePlan &&
		(r.DeploymentPlanID == nil || *r.DeploymentPlanID == uuid.Nil) {
		return validation.NewValidationFailedError("deploymentPlanId is required")
	}
	if r.Action != types.ReconciliationActionCreatePlan &&
		r.DeploymentPlanID != nil {
		return validation.NewValidationFailedError(
			"deploymentPlanId is only valid for create-plan actions",
		)
	}
	if slices.Contains([]types.ReconciliationActionType{
		types.ReconciliationActionRestoreDesired,
		types.ReconciliationActionCloseWithEvidence,
	}, r.Action) &&
		(r.OutcomeObservationID == nil || *r.OutcomeObservationID == uuid.Nil) {
		return validation.NewValidationFailedError(
			"outcomeObservationId is required",
		)
	}
	if !slices.Contains([]types.ReconciliationActionType{
		types.ReconciliationActionRestoreDesired,
		types.ReconciliationActionCloseWithEvidence,
	}, r.Action) && r.OutcomeObservationID != nil {
		return validation.NewValidationFailedError(
			"outcomeObservationId is only valid for proven resolution",
		)
	}
	if r.Action == types.ReconciliationActionAcceptDeviation {
		if r.AcceptedUntil == nil || !r.AcceptedUntil.After(now) {
			return validation.NewValidationFailedError(
				"accepted deviation must have a future expiry",
			)
		}
	} else if r.AcceptedUntil != nil {
		return validation.NewValidationFailedError(
			"acceptedUntil is only valid for accepted deviations",
		)
	}
	return nil
}

type DriftCase struct {
	ID                      uuid.UUID             `json:"id"`
	CreatedAt               time.Time             `json:"createdAt"`
	UpdatedAt               time.Time             `json:"updatedAt"`
	ActiveDesiredRevisionID uuid.UUID             `json:"activeDesiredRevisionId"`
	ObservationID           uuid.UUID             `json:"observationId"`
	DeploymentUnitID        uuid.UUID             `json:"deploymentUnitId"`
	ComponentInstanceID     uuid.UUID             `json:"componentInstanceId"`
	Status                  types.DriftCaseStatus `json:"status"`
	Classes                 []types.DriftClass    `json:"classes"`
	Summary                 string                `json:"summary"`
	AssignedTo              *uuid.UUID            `json:"assignedTo,omitempty"`
	ResolvedAt              *time.Time            `json:"resolvedAt,omitempty"`
}

type ReconciliationAction struct {
	ID                   uuid.UUID                      `json:"id"`
	CreatedAt            time.Time                      `json:"createdAt"`
	DriftCaseID          uuid.UUID                      `json:"driftCaseId"`
	Action               types.ReconciliationActionType `json:"action"`
	Reason               string                         `json:"reason"`
	ActorID              uuid.UUID                      `json:"actorId"`
	DeploymentPlanID     *uuid.UUID                     `json:"deploymentPlanId,omitempty"`
	OutcomeObservationID *uuid.UUID                     `json:"outcomeObservationId,omitempty"`
	AcceptedUntil        *time.Time                     `json:"acceptedUntil,omitempty"`
}
