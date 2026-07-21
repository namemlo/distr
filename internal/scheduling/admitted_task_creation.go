package scheduling

import (
	"context"
	"fmt"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type AdmittedTaskCreationDependencies struct {
	LoadPlanSnapshot func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (types.AdmissionPlanSnapshot, error)
	AdmitDeploymentPlan func(
		context.Context,
		types.AdmitDeploymentPlanRequest,
	) (*types.AdmissionEvaluation, error)
	CreateTasks func(
		context.Context,
		types.CreateTasksForDeploymentPlanRequest,
	) ([]types.Task, error)
}

func CreateTasksForAdmittedV2Plan(
	ctx context.Context,
	request types.CreateTasksForAdmittedV2PlanRequest,
	dependencies AdmittedTaskCreationDependencies,
) ([]types.Task, error) {
	if request.OrganizationID == uuid.Nil ||
		request.DeploymentPlanID == uuid.Nil ||
		request.ExecutionOccurrenceID == uuid.Nil ||
		request.ActorUserAccountID == uuid.Nil {
		return nil, apierrors.NewBadRequest(
			"organizationId, deploymentPlanId, executionOccurrenceId, and actorUserAccountId are required",
		)
	}
	if !admissionIdempotencyKeyValid(request.SchedulerIdempotencyKey) {
		return nil, apierrors.NewBadRequest("schedulerIdempotencyKey is invalid")
	}
	if dependencies.LoadPlanSnapshot == nil ||
		dependencies.AdmitDeploymentPlan == nil ||
		dependencies.CreateTasks == nil {
		return nil, errorsNewAdmittedTaskDependencies()
	}
	snapshot, err := dependencies.LoadPlanSnapshot(
		ctx,
		request.DeploymentPlanID,
		request.OrganizationID,
	)
	if err != nil {
		return nil, err
	}
	if snapshot.PlanSchema != types.AdmissionRequiredPlanSchemaV2 ||
		snapshot.ProtocolVersion != types.AdmissionRequiredProtocolV2 {
		return nil, apierrors.NewConflict(
			"task admission requires frozen plan_schema v2 and protocol_version v2",
		)
	}
	evaluation, err := dependencies.AdmitDeploymentPlan(
		ctx,
		types.AdmitDeploymentPlanRequest{
			OrganizationID:          request.OrganizationID,
			DeploymentPlanID:        request.DeploymentPlanID,
			ActorUserAccountID:      request.ActorUserAccountID,
			SchedulerIdempotencyKey: request.SchedulerIdempotencyKey,
			Campaign:                request.Campaign,
			Authorize:               request.Authorize,
		},
	)
	if err != nil {
		return nil, err
	}
	if evaluation == nil || evaluation.Decision != types.AdmissionDecisionAdmit {
		decision := types.AdmissionDecisionBlock
		if evaluation != nil {
			decision = evaluation.Decision
		}
		return nil, apierrors.NewConflict(
			fmt.Sprintf("deployment plan admission decision is %s", decision),
		)
	}
	return dependencies.CreateTasks(
		ctx,
		types.CreateTasksForDeploymentPlanRequest{
			OrganizationID:        request.OrganizationID,
			DeploymentPlanID:      request.DeploymentPlanID,
			ExecutionOccurrenceID: request.ExecutionOccurrenceID,
			ActorUserAccountID:    request.ActorUserAccountID,
			ConcurrencyPolicy:     request.ConcurrencyPolicy,
			AdditionalResources:   request.AdditionalResources,
		},
	)
}

func errorsNewAdmittedTaskDependencies() error {
	return fmt.Errorf("admitted v2 task creation dependencies are incomplete")
}

func admissionIdempotencyKeyValid(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' ||
			index > 0 && (character == '.' || character == '_' ||
				character == ':' || character == '-') {
			continue
		}
		return false
	}
	return true
}
