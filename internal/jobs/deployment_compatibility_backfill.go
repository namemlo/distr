package jobs

import (
	"context"

	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
)

func RunDeploymentCompatibilityBackfill(
	ctx context.Context,
	request types.DeploymentCompatibilityBackfillRequest,
) (*types.DeploymentCompatibilityBackfillReport, error) {
	return db.BackfillLegacyDeploymentCompatibility(ctx, request)
}
