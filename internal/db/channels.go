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

const (
	DefaultChannelName        = "Stable"
	defaultChannelDescription = "Default channel"
	channelOutputExpr         = `
	c.id,
	c.created_at,
	c.updated_at,
	c.organization_id,
	c.application_id,
	c.lifecycle_id,
	c.name,
	c.description,
	c.sort_order,
	c.is_default
`
)

func CreateChannel(ctx context.Context, channel *types.Channel) error {
	return RunTx(ctx, func(ctx context.Context) error {
		if err := ensureChannelReferences(
			ctx,
			channel.OrganizationID,
			channel.ApplicationID,
			channel.LifecycleID,
		); err != nil {
			return err
		}
		if channel.IsDefault {
			if err := clearDefaultChannels(ctx, channel.OrganizationID, channel.ApplicationID, uuid.Nil); err != nil {
				return err
			}
		} else if exists, err := defaultChannelExists(
			ctx,
			channel.OrganizationID,
			channel.ApplicationID,
			uuid.Nil,
		); err != nil {
			return err
		} else if !exists {
			channel.IsDefault = true
		}

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`INSERT INTO Channel AS c (
				organization_id,
				application_id,
				lifecycle_id,
				name,
				description,
				sort_order,
				is_default
			) VALUES (
				@organizationId,
				@applicationId,
				@lifecycleId,
				@name,
				@description,
				@sortOrder,
				@isDefault
			) RETURNING `+channelOutputExpr,
			pgx.NamedArgs{
				"organizationId": channel.OrganizationID,
				"applicationId":  channel.ApplicationID,
				"lifecycleId":    channel.LifecycleID,
				"name":           channel.Name,
				"description":    channel.Description,
				"sortOrder":      channel.SortOrder,
				"isDefault":      channel.IsDefault,
			},
		)
		if err != nil {
			return mapChannelWriteError("insert", err)
		}
		result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.Channel])
		if err != nil {
			return mapChannelWriteError("scan created", err)
		}
		*channel = result
		return nil
	})
}

func GetChannelsByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]types.Channel, error) {
	if err := EnsureDefaultChannels(ctx, orgID); err != nil {
		return nil, err
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+channelOutputExpr+`
		FROM Channel c
		WHERE c.organization_id = @organizationId
		ORDER BY c.application_id, c.sort_order, c.name, c.id`,
		pgx.NamedArgs{"organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Channel: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.Channel])
	if err != nil {
		return nil, fmt.Errorf("could not collect Channel: %w", err)
	}
	return result, nil
}

func GetChannel(ctx context.Context, id, orgID uuid.UUID) (*types.Channel, error) {
	return getChannel(ctx, id, orgID, false)
}

func getChannel(ctx context.Context, id, orgID uuid.UUID, forUpdate bool) (*types.Channel, error) {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+channelOutputExpr+`
		FROM Channel c
		WHERE c.id = @id AND c.organization_id = @organizationId`+lockClause,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Channel: %w", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.Channel])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect Channel: %w", err)
	}
	return &result, nil
}

func UpdateChannel(ctx context.Context, channel *types.Channel) error {
	return RunTx(ctx, func(ctx context.Context) error {
		existing, err := getChannel(ctx, channel.ID, channel.OrganizationID, true)
		if err != nil {
			return err
		}
		if existing.IsDefault && !channel.IsDefault {
			return fmt.Errorf("could not update Channel: %w", apierrors.ErrConflict)
		}
		if existing.IsDefault && existing.ApplicationID != channel.ApplicationID {
			return fmt.Errorf("could not update Channel: %w", apierrors.ErrConflict)
		}
		if err := ensureChannelReferences(
			ctx,
			channel.OrganizationID,
			channel.ApplicationID,
			channel.LifecycleID,
		); err != nil {
			return err
		}
		if channel.IsDefault {
			if err := clearDefaultChannels(ctx, channel.OrganizationID, channel.ApplicationID, channel.ID); err != nil {
				return err
			}
		} else if exists, err := defaultChannelExists(
			ctx,
			channel.OrganizationID,
			channel.ApplicationID,
			channel.ID,
		); err != nil {
			return err
		} else if !exists {
			channel.IsDefault = true
		}

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`UPDATE Channel AS c SET
				application_id = @applicationId,
				lifecycle_id = @lifecycleId,
				name = @name,
				description = @description,
				sort_order = @sortOrder,
				is_default = @isDefault,
				updated_at = now()
			WHERE c.id = @id AND c.organization_id = @organizationId
			RETURNING `+channelOutputExpr,
			pgx.NamedArgs{
				"id":             channel.ID,
				"organizationId": channel.OrganizationID,
				"applicationId":  channel.ApplicationID,
				"lifecycleId":    channel.LifecycleID,
				"name":           channel.Name,
				"description":    channel.Description,
				"sortOrder":      channel.SortOrder,
				"isDefault":      channel.IsDefault,
			},
		)
		if err != nil {
			return mapChannelWriteError("update", err)
		}
		result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.Channel])
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		} else if err != nil {
			return mapChannelWriteError("scan updated", err)
		}
		*channel = result
		return nil
	})
}

func DeleteChannelWithID(ctx context.Context, id, organizationID uuid.UUID) error {
	return RunTx(ctx, func(ctx context.Context) error {
		db := internalctx.GetDb(ctx)
		var isDefault bool
		err := db.QueryRow(ctx,
			`SELECT is_default
			FROM Channel
			WHERE id = @id AND organization_id = @organizationId
			FOR UPDATE`,
			pgx.NamedArgs{"id": id, "organizationId": organizationID},
		).Scan(&isDefault)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		} else if err != nil {
			return fmt.Errorf("could not lock Channel for delete: %w", err)
		}
		if isDefault {
			return fmt.Errorf("could not delete Channel: %w", apierrors.ErrConflict)
		}

		cmd, err := db.Exec(ctx,
			`DELETE FROM Channel WHERE id = @id AND organization_id = @organizationId`,
			pgx.NamedArgs{"id": id, "organizationId": organizationID},
		)
		if err != nil {
			var pgError *pgconn.PgError
			if errors.As(err, &pgError) && pgError.Code == pgerrcode.ForeignKeyViolation {
				return fmt.Errorf("%w: %w", apierrors.ErrConflict, err)
			}
			return fmt.Errorf("could not delete Channel: %w", err)
		}
		if cmd.RowsAffected() == 0 {
			return apierrors.ErrNotFound
		}
		return nil
	})
}

func EnsureDefaultChannels(ctx context.Context, orgID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`INSERT INTO Channel (
			organization_id,
			application_id,
			lifecycle_id,
			name,
			description,
			sort_order,
			is_default
		)
		SELECT
			@organizationId,
			a.id,
			l.id,
			@name,
			@description,
			0,
			true
		FROM Application a
		JOIN LATERAL (
			SELECT id
			FROM Lifecycle
			WHERE organization_id = @organizationId
			ORDER BY sort_order, name, id
			LIMIT 1
		) l ON true
		WHERE a.organization_id = @organizationId
			AND NOT EXISTS (
				SELECT 1
				FROM Channel c
				WHERE c.organization_id = @organizationId AND c.application_id = a.id
			)
		ON CONFLICT DO NOTHING`,
		pgx.NamedArgs{
			"organizationId": orgID,
			"name":           DefaultChannelName,
			"description":    defaultChannelDescription,
		},
	)
	if err != nil {
		return mapChannelWriteError("ensure default", err)
	}
	return nil
}

func ensureChannelReferences(
	ctx context.Context,
	orgID uuid.UUID,
	applicationID uuid.UUID,
	lifecycleID uuid.UUID,
) error {
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT
			EXISTS (
				SELECT 1
				FROM Application
				WHERE id = @applicationId AND organization_id = @organizationId
			)
			AND EXISTS (
				SELECT 1
				FROM Lifecycle
				WHERE id = @lifecycleId AND organization_id = @organizationId
			)`,
		pgx.NamedArgs{
			"organizationId": orgID,
			"applicationId":  applicationID,
			"lifecycleId":    lifecycleID,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate Channel references: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func clearDefaultChannels(ctx context.Context, orgID, applicationID, exceptID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`UPDATE Channel
		SET is_default = false, updated_at = now()
		WHERE organization_id = @organizationId
			AND application_id = @applicationId
			AND id <> @exceptId`,
		pgx.NamedArgs{
			"organizationId": orgID,
			"applicationId":  applicationID,
			"exceptId":       exceptID,
		},
	)
	if err != nil {
		return fmt.Errorf("could not clear default Channels: %w", err)
	}
	return nil
}

func defaultChannelExists(ctx context.Context, orgID, applicationID, exceptID uuid.UUID) (bool, error) {
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM Channel
			WHERE organization_id = @organizationId
				AND application_id = @applicationId
				AND is_default
				AND id <> @exceptId
		)`,
		pgx.NamedArgs{
			"organizationId": orgID,
			"applicationId":  applicationID,
			"exceptId":       exceptID,
		},
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("could not query default Channel: %w", err)
	}
	return exists, nil
}

func mapChannelWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == pgerrcode.UniqueViolation {
		return fmt.Errorf("could not %s Channel: %w", action, apierrors.ErrAlreadyExists)
	}
	return fmt.Errorf("could not %s Channel: %w", action, err)
}
