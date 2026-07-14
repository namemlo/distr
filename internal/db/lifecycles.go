package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const lifecycleOutputExpr = `
	l.id,
	l.created_at,
	l.updated_at,
	l.organization_id,
	l.name,
	l.description,
	l.sort_order
`

const lifecyclePhaseOutputExpr = `
	lp.id,
	lp.lifecycle_id,
	lp.name,
	lp.description,
	lp.sort_order,
	lp.optional,
	lp.automatic_promotion,
	lp.minimum_successful_deployments,
	lp.approval_policy_id,
	lp.retention_policy_id
`

func CreateLifecycle(ctx context.Context, lifecycle *types.Lifecycle) error {
	return RunTx(ctx, func(ctx context.Context) error {
		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`INSERT INTO Lifecycle AS l (
				organization_id,
				name,
				description,
				sort_order
			) VALUES (
				@organizationId,
				@name,
				@description,
				@sortOrder
			) RETURNING `+lifecycleOutputExpr,
			pgx.NamedArgs{
				"organizationId": lifecycle.OrganizationID,
				"name":           lifecycle.Name,
				"description":    lifecycle.Description,
				"sortOrder":      lifecycle.SortOrder,
			},
		)
		if err != nil {
			return mapLifecycleWriteError("insert", err)
		}
		result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.Lifecycle])
		if err != nil {
			return mapLifecycleWriteError("scan created", err)
		}
		result.Phases = lifecycle.Phases
		if err := replaceLifecyclePhases(ctx, result.ID, result.OrganizationID, result.Phases); err != nil {
			return err
		}
		result.Phases, err = getLifecyclePhases(ctx, result.ID)
		if err != nil {
			return err
		}
		*lifecycle = result
		return nil
	})
}

func GetLifecyclesByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]types.Lifecycle, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+lifecycleOutputExpr+`
		FROM Lifecycle l
		WHERE l.organization_id = @organizationId
		ORDER BY l.sort_order, l.name`,
		pgx.NamedArgs{"organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Lifecycle: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.Lifecycle])
	if err != nil {
		return nil, fmt.Errorf("could not collect Lifecycle: %w", err)
	}
	for i := range result {
		result[i].Phases, err = getLifecyclePhases(ctx, result[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func GetLifecycle(ctx context.Context, id, orgID uuid.UUID) (*types.Lifecycle, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+lifecycleOutputExpr+`
		FROM Lifecycle l
		WHERE l.id = @id AND l.organization_id = @organizationId`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Lifecycle: %w", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.Lifecycle])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect Lifecycle: %w", err)
	}
	result.Phases, err = getLifecyclePhases(ctx, result.ID)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func UpdateLifecycle(ctx context.Context, lifecycle *types.Lifecycle) error {
	return RunTx(ctx, func(ctx context.Context) error {
		if err := EnsureConfigAsCodeDatabaseManagedForUpdate(
			ctx,
			lifecycle.OrganizationID,
			types.ConfigAsCodeResourceKindLifecycle,
			lifecycle.ID,
		); err != nil {
			return err
		}
		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`UPDATE Lifecycle AS l SET
				name = @name,
				description = @description,
				sort_order = @sortOrder,
				updated_at = now()
			WHERE l.id = @id AND l.organization_id = @organizationId
			RETURNING `+lifecycleOutputExpr,
			pgx.NamedArgs{
				"id":             lifecycle.ID,
				"organizationId": lifecycle.OrganizationID,
				"name":           lifecycle.Name,
				"description":    lifecycle.Description,
				"sortOrder":      lifecycle.SortOrder,
			},
		)
		if err != nil {
			return mapLifecycleWriteError("update", err)
		}
		result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.Lifecycle])
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		} else if err != nil {
			return mapLifecycleWriteError("scan updated", err)
		}
		result.Phases = lifecycle.Phases
		if err := replaceLifecyclePhases(ctx, result.ID, result.OrganizationID, result.Phases); err != nil {
			return err
		}
		result.Phases, err = getLifecyclePhases(ctx, result.ID)
		if err != nil {
			return err
		}
		*lifecycle = result
		return nil
	})
}

func ReplaceLifecyclePhases(
	ctx context.Context,
	lifecycleID uuid.UUID,
	orgID uuid.UUID,
	phases []types.LifecyclePhase,
) (*types.Lifecycle, error) {
	err := RunTx(ctx, func(ctx context.Context) error {
		if _, err := GetLifecycle(ctx, lifecycleID, orgID); err != nil {
			return err
		}
		if err := EnsureConfigAsCodeDatabaseManagedForUpdate(
			ctx,
			orgID,
			types.ConfigAsCodeResourceKindLifecycle,
			lifecycleID,
		); err != nil {
			return err
		}
		return replaceLifecyclePhases(ctx, lifecycleID, orgID, phases)
	})
	if err != nil {
		return nil, err
	}
	return GetLifecycle(ctx, lifecycleID, orgID)
}

func DeleteLifecycleWithID(ctx context.Context, id, organizationID uuid.UUID) error {
	return RunTx(ctx, func(ctx context.Context) error {
		if err := EnsureConfigAsCodeDatabaseManagedForUpdate(
			ctx,
			organizationID,
			types.ConfigAsCodeResourceKindLifecycle,
			id,
		); err != nil {
			return err
		}
		if err := DeleteConfigAsCodeAuthorityForResource(
			ctx,
			organizationID,
			types.ConfigAsCodeResourceKindLifecycle,
			id,
		); err != nil {
			return err
		}
		db := internalctx.GetDb(ctx)
		cmd, err := db.Exec(ctx,
			`DELETE FROM Lifecycle WHERE id = @id AND organization_id = @organizationId`,
			pgx.NamedArgs{"id": id, "organizationId": organizationID},
		)
		if err != nil {
			if isProtectedReferenceViolation(err) {
				return fmt.Errorf("%w: %w", apierrors.ErrConflict, err)
			}
			return fmt.Errorf("could not delete Lifecycle: %w", err)
		}
		if cmd.RowsAffected() == 0 {
			return apierrors.ErrNotFound
		}
		return nil
	})
}

func getLifecyclePhases(ctx context.Context, lifecycleID uuid.UUID) ([]types.LifecyclePhase, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+lifecyclePhaseOutputExpr+`
		FROM LifecyclePhase lp
		WHERE lp.lifecycle_id = @lifecycleId
		ORDER BY lp.sort_order, lp.name`,
		pgx.NamedArgs{"lifecycleId": lifecycleID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query LifecyclePhase: %w", err)
	}
	phases, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.LifecyclePhase])
	if err != nil {
		return nil, fmt.Errorf("could not collect LifecyclePhase: %w", err)
	}
	for i := range phases {
		phases[i].EnvironmentIDs, err = getLifecyclePhaseEnvironmentIDs(ctx, phases[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return phases, nil
}

func replaceLifecyclePhases(ctx context.Context, lifecycleID, orgID uuid.UUID, phases []types.LifecyclePhase) error {
	db := internalctx.GetDb(ctx)
	if _, err := db.Exec(
		ctx,
		`DELETE FROM LifecyclePhase WHERE lifecycle_id = @lifecycleId`,
		pgx.NamedArgs{"lifecycleId": lifecycleID},
	); err != nil {
		return fmt.Errorf("could not delete LifecyclePhase: %w", err)
	}
	for i := range phases {
		phases[i].LifecycleID = lifecycleID
		rows, err := db.Query(ctx,
			`INSERT INTO LifecyclePhase AS lp (
				lifecycle_id,
				name,
				description,
				sort_order,
				optional,
				automatic_promotion,
				minimum_successful_deployments,
				approval_policy_id,
				retention_policy_id
			) VALUES (
				@lifecycleId,
				@name,
				@description,
				@sortOrder,
				@optional,
				@automaticPromotion,
				@minimumSuccessfulDeployments,
				@approvalPolicyId,
				@retentionPolicyId
			) RETURNING `+lifecyclePhaseOutputExpr,
			pgx.NamedArgs{
				"lifecycleId":                  lifecycleID,
				"name":                         phases[i].Name,
				"description":                  phases[i].Description,
				"sortOrder":                    phases[i].SortOrder,
				"optional":                     phases[i].Optional,
				"automaticPromotion":           phases[i].AutomaticPromotion,
				"minimumSuccessfulDeployments": phases[i].MinimumSuccessfulDeployments,
				"approvalPolicyId":             phases[i].ApprovalPolicyID,
				"retentionPolicyId":            phases[i].RetentionPolicyID,
			},
		)
		if err != nil {
			return mapLifecycleWriteError("insert phase", err)
		}
		phase, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.LifecyclePhase])
		if err != nil {
			return mapLifecycleWriteError("scan phase", err)
		}
		if err := replaceLifecyclePhaseEnvironments(ctx, phase.ID, orgID, phases[i].EnvironmentIDs); err != nil {
			return err
		}
	}
	return nil
}

func replaceLifecyclePhaseEnvironments(
	ctx context.Context,
	phaseID uuid.UUID,
	orgID uuid.UUID,
	environmentIDs []uuid.UUID,
) error {
	db := internalctx.GetDb(ctx)
	for i, environmentID := range environmentIDs {
		cmd, err := db.Exec(ctx,
			`INSERT INTO LifecyclePhaseEnvironment (
				lifecycle_phase_id,
				environment_id,
				sort_order
			)
			SELECT @phaseId, e.id, @sortOrder
			FROM Environment e
			WHERE e.id = @environmentId AND e.organization_id = @organizationId`,
			pgx.NamedArgs{
				"phaseId":        phaseID,
				"environmentId":  environmentID,
				"organizationId": orgID,
				"sortOrder":      i,
			},
		)
		if err != nil {
			return mapLifecycleWriteError("insert phase environment", err)
		}
		if cmd.RowsAffected() == 0 {
			return apierrors.ErrNotFound
		}
	}
	return nil
}

func getLifecyclePhaseEnvironmentIDs(ctx context.Context, phaseID uuid.UUID) ([]uuid.UUID, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT environment_id
		FROM LifecyclePhaseEnvironment
		WHERE lifecycle_phase_id = @phaseId
		ORDER BY sort_order`,
		pgx.NamedArgs{"phaseId": phaseID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query LifecyclePhaseEnvironment: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("could not collect LifecyclePhaseEnvironment: %w", err)
	}
	return ids, nil
}

func mapLifecycleWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == pgerrcode.UniqueViolation {
		return fmt.Errorf("could not %s Lifecycle: %w", action, apierrors.ErrAlreadyExists)
	}
	return fmt.Errorf("could not %s Lifecycle: %w", action, err)
}
