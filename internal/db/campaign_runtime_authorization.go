package db

import (
	"context"
	"errors"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const campaignRevisionRuntimeAuthorizationTargetQuery = `
SELECT
  revision.deployment_campaign_draft_id,
  array_agg(DISTINCT plan.environment_id ORDER BY plan.environment_id)
FROM DeploymentCampaignRevision AS revision
JOIN DeploymentCampaignMember AS member
  ON member.campaign_revision_id = revision.id
 AND member.organization_id = revision.organization_id
JOIN DeploymentPlan AS plan
  ON plan.id = member.deployment_plan_id
 AND plan.organization_id = member.organization_id
 AND plan.deployment_unit_id = member.deployment_unit_id
WHERE revision.id = @campaign_revision_id
  AND revision.organization_id = @organization_id
GROUP BY revision.deployment_campaign_draft_id`

const campaignRunRuntimeAuthorizationTargetQuery = `
SELECT
  revision.deployment_campaign_draft_id,
  array_agg(DISTINCT plan.environment_id ORDER BY plan.environment_id)
FROM DeploymentCampaignRun AS run
JOIN DeploymentCampaignRevision AS revision
  ON run.campaign_revision_id = revision.id
 AND run.organization_id = revision.organization_id
JOIN DeploymentCampaignMember AS member
  ON member.campaign_revision_id = revision.id
 AND member.organization_id = revision.organization_id
JOIN DeploymentPlan AS plan
  ON plan.id = member.deployment_plan_id
 AND plan.organization_id = member.organization_id
 AND plan.deployment_unit_id = member.deployment_unit_id
WHERE run.id = @campaign_run_id
  AND run.organization_id = @organization_id
GROUP BY revision.deployment_campaign_draft_id`

func (CampaignRepository) ResolveCampaignRevisionRuntimeAuthorizationTarget(
	ctx context.Context,
	organizationID uuid.UUID,
	revisionID uuid.UUID,
) (types.CampaignRuntimeAuthorizationTarget, error) {
	return resolveCampaignRuntimeAuthorizationTarget(
		ctx,
		campaignRevisionRuntimeAuthorizationTargetQuery,
		pgx.NamedArgs{
			"organization_id":      organizationID,
			"campaign_revision_id": revisionID,
		},
	)
}

func (CampaignRepository) ResolveCampaignRunRuntimeAuthorizationTarget(
	ctx context.Context,
	organizationID uuid.UUID,
	runID uuid.UUID,
) (types.CampaignRuntimeAuthorizationTarget, error) {
	return resolveCampaignRuntimeAuthorizationTarget(
		ctx,
		campaignRunRuntimeAuthorizationTargetQuery,
		pgx.NamedArgs{
			"organization_id": organizationID,
			"campaign_run_id": runID,
		},
	)
}

func resolveCampaignRuntimeAuthorizationTarget(
	ctx context.Context,
	query string,
	args pgx.NamedArgs,
) (types.CampaignRuntimeAuthorizationTarget, error) {
	var target types.CampaignRuntimeAuthorizationTarget
	err := internalctx.GetDb(ctx).QueryRow(ctx, query, args).Scan(
		&target.CampaignDraftID,
		&target.EnvironmentIDs,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.CampaignRuntimeAuthorizationTarget{}, apierrors.ErrNotFound
	}
	if err != nil {
		return types.CampaignRuntimeAuthorizationTarget{}, err
	}
	if target.CampaignDraftID == uuid.Nil || len(target.EnvironmentIDs) == 0 {
		return types.CampaignRuntimeAuthorizationTarget{}, apierrors.ErrNotFound
	}
	return target, nil
}
