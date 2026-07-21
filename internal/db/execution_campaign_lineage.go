package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CampaignMemberTaskExecutionBinding is the exact immutable bridge from a
// campaign member-run to the task/execution created for that member. Campaign
// schedulers must persist it in the same transaction as task creation.
type CampaignMemberTaskExecutionBinding struct {
	ID                  uuid.UUID
	OrganizationID      uuid.UUID
	CampaignRunID       uuid.UUID
	CampaignMemberRunID uuid.UUID
	DeploymentPlanID    uuid.UUID
	TaskID              uuid.UUID
	DeploymentTargetID  uuid.UUID
}

func BindCampaignMemberTaskExecution(
	ctx context.Context,
	binding CampaignMemberTaskExecutionBinding,
) error {
	if err := validateCampaignMemberTaskExecutionBinding(binding); err != nil {
		return err
	}
	database := internalctx.GetDb(ctx)
	var insertedID uuid.UUID
	err := database.QueryRow(ctx, `
		INSERT INTO CampaignMemberTaskExecution (
			id, organization_id, campaign_run_id, campaign_member_run_id,
			deployment_plan_id, task_id, deployment_target_id
		) VALUES (
			@id, @organizationID, @campaignRunID, @campaignMemberRunID,
			@deploymentPlanID, @taskID, @deploymentTargetID
		)
		ON CONFLICT (organization_id, task_id) DO NOTHING
		RETURNING id
	`, pgx.NamedArgs{
		"id": binding.ID, "organizationID": binding.OrganizationID,
		"campaignRunID":       binding.CampaignRunID,
		"campaignMemberRunID": binding.CampaignMemberRunID,
		"deploymentPlanID":    binding.DeploymentPlanID, "taskID": binding.TaskID,
		"deploymentTargetID": binding.DeploymentTargetID,
	}).Scan(&insertedID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("bind campaign member task execution: %w", err)
	}

	var existing CampaignMemberTaskExecutionBinding
	err = database.QueryRow(ctx, `
		SELECT id, organization_id, campaign_run_id, campaign_member_run_id,
		       deployment_plan_id, task_id, deployment_target_id
		FROM CampaignMemberTaskExecution
		WHERE organization_id = @organizationID
		  AND task_id = @taskID
	`, pgx.NamedArgs{
		"organizationID": binding.OrganizationID, "taskID": binding.TaskID,
	}).Scan(
		&existing.ID, &existing.OrganizationID, &existing.CampaignRunID,
		&existing.CampaignMemberRunID, &existing.DeploymentPlanID,
		&existing.TaskID, &existing.DeploymentTargetID,
	)
	if err != nil {
		return fmt.Errorf("read campaign member task execution binding: %w", err)
	}
	if existing != binding {
		return apierrors.NewConflict("task is already bound to different campaign member lineage")
	}
	return nil
}

func validateCampaignMemberTaskExecutionBinding(
	binding CampaignMemberTaskExecutionBinding,
) error {
	if binding.ID == uuid.Nil || binding.OrganizationID == uuid.Nil ||
		binding.CampaignRunID == uuid.Nil || binding.CampaignMemberRunID == uuid.Nil ||
		binding.DeploymentPlanID == uuid.Nil || binding.TaskID == uuid.Nil ||
		binding.DeploymentTargetID == uuid.Nil {
		return apierrors.NewBadRequest("campaign member task execution binding is incomplete")
	}
	return nil
}
