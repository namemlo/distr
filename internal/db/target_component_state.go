package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const targetComponentStateOutputExpr = `
	tcs.id,
	tcs.created_at,
	tcs.updated_at,
	tcs.organization_id,
	tcs.deployment_target_id,
	tcs.application_id,
	tcs.component,
	tcs.state_version,
	tcs.state_checksum,
	tcs.release_bundle_id,
	tcs.version,
	tcs.image,
	tcs.platform,
	tcs.contracts,
	tcs.config_reference,
	tcs.config_checksum,
	tcs.health,
	tcs.observed_at
`

func GetTargetComponentState(
	ctx context.Context,
	orgID, deploymentTargetID, applicationID uuid.UUID,
	component string,
) (*types.TargetComponentState, error) {
	database := internalctx.GetDb(ctx)
	rows, err := database.Query(ctx,
		`SELECT `+targetComponentStateOutputExpr+`
		FROM TargetComponentState tcs
		WHERE tcs.organization_id = @organizationId
			AND tcs.deployment_target_id = @deploymentTargetId
			AND tcs.application_id = @applicationId
			AND tcs.component = @component`,
		pgx.NamedArgs{
			"organizationId": orgID, "deploymentTargetId": deploymentTargetID,
			"applicationId": applicationID, "component": component,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query TargetComponentState: %w", err)
	}
	state, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.TargetComponentState])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect TargetComponentState: %w", err)
	}
	return &state, nil
}
