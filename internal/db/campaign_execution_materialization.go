package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const loadPendingCampaignDispatchTasksSQL = `
SELECT ` + taskOutputExpr + `
FROM Task AS t
JOIN CampaignMemberTaskExecution AS lineage
  ON lineage.task_id = t.id
 AND lineage.organization_id = t.organization_id
 AND lineage.deployment_plan_id = t.deployment_plan_id
 AND lineage.deployment_target_id = t.deployment_target_id
JOIN DeploymentCampaignMemberRun AS member_run
  ON member_run.id = lineage.campaign_member_run_id
 AND member_run.organization_id = lineage.organization_id
 AND member_run.campaign_run_id = lineage.campaign_run_id
 AND member_run.deployment_plan_id = lineage.deployment_plan_id
 AND t.execution_occurrence_id = member_run.id
JOIN DeploymentCampaignRun AS run
  ON run.id = lineage.campaign_run_id
 AND run.organization_id = lineage.organization_id
WHERE run.id = @run_id
  AND run.organization_id = lineage.organization_id
  AND run.state = 'RUNNING'
  AND run.fencing_token = @fencing_token
  AND run.lease_expires_at > clock_timestamp()
  AND member_run.status IN ('ADMITTED', 'RUNNING')
  AND t.status IN ('QUEUED', 'RUNNING')
  AND NOT EXISTS (
    SELECT 1
    FROM ExecutionAttempt AS attempt
    WHERE attempt.organization_id = t.organization_id
      AND attempt.task_id = t.id
  )
ORDER BY member_run.wave_order, member_run.member_order, t.queue_order, t.id`

func campaignTaskCreationRequest(
	candidate types.CampaignMemberCandidate,
	admission types.CampaignMemberAdmission,
	authorizer types.AdmissionAuthorizer,
) (types.CreateTasksForAdmittedV2PlanRequest, error) {
	if candidate.OrganizationID == uuid.Nil || candidate.ActorUserAccountID == uuid.Nil ||
		candidate.MemberRunID == uuid.Nil || candidate.PlanID == uuid.Nil ||
		candidate.CampaignEvidence.ID == uuid.Nil ||
		candidate.CampaignEvidence.Revision <= 0 ||
		strings.TrimSpace(candidate.CampaignEvidence.Checksum) == "" ||
		admission.RunID == uuid.Nil || admission.MemberRunID != candidate.MemberRunID ||
		admission.PlanID != candidate.PlanID || authorizer == nil {
		return types.CreateTasksForAdmittedV2PlanRequest{}, apierrors.NewConflict(
			"campaign task materialization requires immutable tenant evidence and authorization",
		)
	}
	evidence := candidate.CampaignEvidence
	return types.CreateTasksForAdmittedV2PlanRequest{
		OrganizationID:          candidate.OrganizationID,
		DeploymentPlanID:        candidate.PlanID,
		ExecutionOccurrenceID:   candidate.MemberRunID,
		ActorUserAccountID:      candidate.ActorUserAccountID,
		SchedulerIdempotencyKey: fmt.Sprintf("campaign:%s:member:%s", admission.RunID, candidate.MemberRunID),
		ConcurrencyPolicy:       types.TaskConcurrencyPolicyQueue,
		Campaign:                &evidence,
		Authorize:               authorizer,
	}, nil
}

func campaignTaskBindings(
	candidate types.CampaignMemberCandidate,
	admission types.CampaignMemberAdmission,
	tasks []types.Task,
) ([]CampaignMemberTaskExecutionBinding, error) {
	if len(tasks) == 0 {
		return nil, apierrors.NewConflict("campaign admission produced no tasks")
	}
	bindings := make([]CampaignMemberTaskExecutionBinding, 0, len(tasks))
	for _, task := range tasks {
		if task.ID == uuid.Nil || task.OrganizationID != candidate.OrganizationID ||
			task.DeploymentPlanID != candidate.PlanID ||
			task.ExecutionOccurrenceID != candidate.MemberRunID ||
			task.DeploymentTargetID == uuid.Nil {
			return nil, apierrors.NewConflict(
				"campaign task does not match exact member execution lineage",
			)
		}
		bindingKey := strings.Join([]string{
			candidate.OrganizationID.String(), admission.RunID.String(),
			candidate.MemberRunID.String(), task.ID.String(),
		}, ":")
		bindings = append(bindings, CampaignMemberTaskExecutionBinding{
			ID:                  uuid.NewSHA1(uuid.NameSpaceOID, []byte(bindingKey)),
			OrganizationID:      candidate.OrganizationID,
			CampaignRunID:       admission.RunID,
			CampaignMemberRunID: candidate.MemberRunID,
			DeploymentPlanID:    candidate.PlanID,
			TaskID:              task.ID,
			DeploymentTargetID:  task.DeploymentTargetID,
		})
	}
	return bindings, nil
}

func materializeAdmittedCampaignTasks(
	ctx context.Context,
	candidate types.CampaignMemberCandidate,
	admission types.CampaignMemberAdmission,
	authorizer types.AdmissionAuthorizer,
) ([]types.Task, error) {
	request, err := campaignTaskCreationRequest(candidate, admission, authorizer)
	if err != nil {
		return nil, err
	}
	tasks, err := CreateTasksForAdmittedV2Plan(ctx, request)
	if err != nil {
		return nil, err
	}
	bindings, err := campaignTaskBindings(candidate, admission, tasks)
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		if err := BindCampaignMemberTaskExecution(ctx, binding); err != nil {
			return nil, err
		}
	}
	return tasks, nil
}

func (CampaignRepository) LoadPendingCampaignDispatchTasks(
	ctx context.Context,
	runID uuid.UUID,
	fencingToken int64,
) ([]types.Task, error) {
	rows, err := internalctx.GetDb(ctx).Query(
		ctx,
		loadPendingCampaignDispatchTasksSQL,
		pgx.NamedArgs{"run_id": runID, "fencing_token": fencingToken},
	)
	if err != nil {
		return nil, fmt.Errorf("load pending campaign dispatch tasks: %w", err)
	}
	tasks, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.Task])
	if err != nil {
		return nil, fmt.Errorf("collect pending campaign dispatch tasks: %w", err)
	}
	for index := range tasks {
		if err := hydrateTask(ctx, &tasks[index]); err != nil {
			return nil, err
		}
	}
	return tasks, nil
}
