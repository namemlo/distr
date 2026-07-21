package db

import (
	"context"
	"fmt"
	"strings"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxRunnableCampaignBatchSize = 100

const listRunnableCampaignRunIDsSQL = `
SELECT id
FROM DeploymentCampaignRun
WHERE state = 'RUNNING'
  AND (
    lease_expires_at IS NULL
    OR lease_expires_at <= clock_timestamp()
    OR lease_holder = @worker_id
  )
ORDER BY COALESCE(lease_expires_at, '-infinity'::timestamptz), id
LIMIT @limit`

func (CampaignRepository) ListRunnableCampaignRunIDs(
	ctx context.Context,
	workerID string,
	limit int,
) ([]uuid.UUID, error) {
	if err := validateRunnableCampaignBatch(workerID, limit); err != nil {
		return nil, err
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, listRunnableCampaignRunIDsSQL, pgx.NamedArgs{
		"worker_id": strings.TrimSpace(workerID),
		"limit":     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list runnable campaign runs: %w", err)
	}
	runIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("collect runnable campaign runs: %w", err)
	}
	return runIDs, nil
}

func validateRunnableCampaignBatch(workerID string, limit int) error {
	if strings.TrimSpace(workerID) == "" {
		return fmt.Errorf("campaign scheduler worker ID is required")
	}
	if limit < 1 || limit > maxRunnableCampaignBatchSize {
		return fmt.Errorf("campaign scheduler batch limit must be between 1 and %d", maxRunnableCampaignBatchSize)
	}
	return nil
}
