package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/configascode"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const configAsCodeAuthorityOutputExpr = `
caca.id,
caca.organization_id,
caca.resource_kind,
caca.resource_id,
caca.authority,
caca.repository_path,
caca.source_revision,
caca.document_checksum,
caca.updated_by_useraccount_id,
caca.updated_at
`

var configAsCodeResourceTables = map[types.ConfigAsCodeResourceKind]string{
	types.ConfigAsCodeResourceKindDeploymentProcess:     "DeploymentProcess",
	types.ConfigAsCodeResourceKindChannel:               "Channel",
	types.ConfigAsCodeResourceKindLifecycle:             "Lifecycle",
	types.ConfigAsCodeResourceKindVariableSetDefinition: "VariableSet",
	types.ConfigAsCodeResourceKindStepTemplateReference: "StepTemplate",
	types.ConfigAsCodeResourceKindRunbook:               "Runbook",
}

func GetConfigAsCodeAuthorities(ctx context.Context, organizationID uuid.UUID) ([]types.ConfigAsCodeAuthority, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+configAsCodeAuthorityOutputExpr+`
		FROM ConfigAsCodeAuthority caca
		WHERE caca.organization_id = @organizationId
		ORDER BY caca.resource_kind, caca.repository_path, caca.resource_id`,
		pgx.NamedArgs{"organizationId": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ConfigAsCodeAuthority: %w", err)
	}
	authorities, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ConfigAsCodeAuthority])
	if err != nil {
		return nil, fmt.Errorf("could not collect ConfigAsCodeAuthority: %w", err)
	}
	return authorities, nil
}

func GetConfigAsCodeAuthority(
	ctx context.Context,
	organizationID uuid.UUID,
	resourceKind types.ConfigAsCodeResourceKind,
	resourceID uuid.UUID,
) (*types.ConfigAsCodeAuthority, error) {
	if err := ensureConfigAsCodeResourceExists(ctx, organizationID, resourceKind, resourceID, false); err != nil {
		return nil, err
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+configAsCodeAuthorityOutputExpr+`
		FROM ConfigAsCodeAuthority caca
		WHERE caca.organization_id = @organizationId
			AND caca.resource_kind = @resourceKind
			AND caca.resource_id = @resourceId`,
		pgx.NamedArgs{
			"organizationId": organizationID,
			"resourceKind":   resourceKind,
			"resourceId":     resourceID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ConfigAsCodeAuthority: %w", err)
	}
	authority, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ConfigAsCodeAuthority])
	if errors.Is(err, pgx.ErrNoRows) {
		return &types.ConfigAsCodeAuthority{
			OrganizationID: organizationID,
			ResourceKind:   resourceKind,
			ResourceID:     resourceID,
			Authority:      types.ConfigAsCodeAuthorityDatabaseManaged,
		}, nil
	} else if err != nil {
		return nil, fmt.Errorf("could not collect ConfigAsCodeAuthority: %w", err)
	}
	return &authority, nil
}

func UpsertConfigAsCodeAuthority(ctx context.Context, authority *types.ConfigAsCodeAuthority) error {
	if err := normalizeConfigAsCodeAuthority(authority); err != nil {
		return err
	}
	return RunTx(ctx, func(ctx context.Context) error {
		if err := ensureConfigAsCodeResourceExists(
			ctx,
			authority.OrganizationID,
			authority.ResourceKind,
			authority.ResourceID,
			true,
		); err != nil {
			return err
		}

		previous, err := getConfigAsCodeAuthorityForAudit(
			ctx,
			authority.OrganizationID,
			authority.ResourceKind,
			authority.ResourceID,
		)
		if err != nil {
			return err
		}

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`INSERT INTO ConfigAsCodeAuthority AS caca (
				organization_id,
				resource_kind,
				resource_id,
				authority,
				repository_path,
				source_revision,
				document_checksum,
				updated_by_useraccount_id,
				updated_at
			) VALUES (
				@organizationId,
				@resourceKind,
				@resourceId,
				@authority,
				@repositoryPath,
				@sourceRevision,
				@documentChecksum,
				@updatedByUserAccountId,
				now()
			)
			ON CONFLICT (organization_id, resource_kind, resource_id) DO UPDATE SET
				authority = EXCLUDED.authority,
				repository_path = EXCLUDED.repository_path,
				source_revision = EXCLUDED.source_revision,
				document_checksum = EXCLUDED.document_checksum,
				updated_by_useraccount_id = EXCLUDED.updated_by_useraccount_id,
				updated_at = now()
			RETURNING `+configAsCodeAuthorityOutputExpr,
			pgx.NamedArgs{
				"organizationId":         authority.OrganizationID,
				"resourceKind":           authority.ResourceKind,
				"resourceId":             authority.ResourceID,
				"authority":              authority.Authority,
				"repositoryPath":         authority.RepositoryPath,
				"sourceRevision":         authority.SourceRevision,
				"documentChecksum":       authority.DocumentChecksum,
				"updatedByUserAccountId": authority.UpdatedByUserID,
			},
		)
		if err != nil {
			return mapConfigAsCodeAuthorityWriteError("upsert", err)
		}
		result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ConfigAsCodeAuthority])
		if err != nil {
			return mapConfigAsCodeAuthorityWriteError("scan upserted", err)
		}
		*authority = result
		if configAsCodeAuthorityChanged(previous, result) {
			if err := insertConfigAsCodeAuthorityAuditEvent(ctx, previous, result); err != nil {
				return err
			}
		}
		return nil
	})
}

func EnsureConfigAsCodeDatabaseManagedForUpdate(
	ctx context.Context,
	organizationID uuid.UUID,
	resourceKind types.ConfigAsCodeResourceKind,
	resourceID uuid.UUID,
) error {
	if err := ensureConfigAsCodeResourceExists(ctx, organizationID, resourceKind, resourceID, true); err != nil {
		return err
	}

	db := internalctx.GetDb(ctx)
	var authority types.ConfigAsCodeAuthorityValue
	err := db.QueryRow(ctx,
		`SELECT caca.authority
		FROM ConfigAsCodeAuthority caca
		WHERE caca.organization_id = @organizationId
			AND caca.resource_kind = @resourceKind
			AND caca.resource_id = @resourceId
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationId": organizationID,
			"resourceKind":   resourceKind,
			"resourceId":     resourceID,
		},
	).Scan(&authority)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("could not query ConfigAsCodeAuthority for update: %w", err)
	}
	if authority == types.ConfigAsCodeAuthorityGitManaged {
		return fmt.Errorf("resource is managed by Git: %w", apierrors.ErrConflict)
	}
	return nil
}

func DeleteConfigAsCodeAuthorityForResource(
	ctx context.Context,
	organizationID uuid.UUID,
	resourceKind types.ConfigAsCodeResourceKind,
	resourceID uuid.UUID,
) error {
	db := internalctx.GetDb(ctx)
	if _, err := db.Exec(ctx,
		`DELETE FROM ConfigAsCodeAuthority
		WHERE organization_id = @organizationId
			AND resource_kind = @resourceKind
			AND resource_id = @resourceId`,
		pgx.NamedArgs{
			"organizationId": organizationID,
			"resourceKind":   resourceKind,
			"resourceId":     resourceID,
		},
	); err != nil {
		return mapConfigAsCodeAuthorityWriteError("delete", err)
	}
	return nil
}

func getConfigAsCodeAuthorityForAudit(
	ctx context.Context,
	organizationID uuid.UUID,
	resourceKind types.ConfigAsCodeResourceKind,
	resourceID uuid.UUID,
) (types.ConfigAsCodeAuthority, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+configAsCodeAuthorityOutputExpr+`
		FROM ConfigAsCodeAuthority caca
		WHERE caca.organization_id = @organizationId
			AND caca.resource_kind = @resourceKind
			AND caca.resource_id = @resourceId
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationId": organizationID,
			"resourceKind":   resourceKind,
			"resourceId":     resourceID,
		},
	)
	if err != nil {
		return types.ConfigAsCodeAuthority{}, fmt.Errorf("could not query ConfigAsCodeAuthority for audit: %w", err)
	}
	authority, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ConfigAsCodeAuthority])
	if errors.Is(err, pgx.ErrNoRows) {
		return types.ConfigAsCodeAuthority{
			OrganizationID: organizationID,
			ResourceKind:   resourceKind,
			ResourceID:     resourceID,
			Authority:      types.ConfigAsCodeAuthorityDatabaseManaged,
		}, nil
	} else if err != nil {
		return types.ConfigAsCodeAuthority{}, fmt.Errorf("could not collect ConfigAsCodeAuthority for audit: %w", err)
	}
	return authority, nil
}

func configAsCodeAuthorityChanged(
	previous types.ConfigAsCodeAuthority,
	next types.ConfigAsCodeAuthority,
) bool {
	return previous.Authority != next.Authority ||
		previous.RepositoryPath != next.RepositoryPath ||
		previous.SourceRevision != next.SourceRevision ||
		previous.DocumentChecksum != next.DocumentChecksum
}

func insertConfigAsCodeAuthorityAuditEvent(
	ctx context.Context,
	previous types.ConfigAsCodeAuthority,
	next types.ConfigAsCodeAuthority,
) error {
	db := internalctx.GetDb(ctx)
	if _, err := db.Exec(ctx,
		`INSERT INTO ConfigAsCodeAuthorityAuditEvent (
			organization_id,
			resource_kind,
			resource_id,
			previous_authority,
			new_authority,
			repository_path,
			source_revision,
			document_checksum,
			actor_useraccount_id
		) VALUES (
			@organizationId,
			@resourceKind,
			@resourceId,
			@previousAuthority,
			@newAuthority,
			@repositoryPath,
			@sourceRevision,
			@documentChecksum,
			@actorUserAccountId
		)`,
		pgx.NamedArgs{
			"organizationId":     next.OrganizationID,
			"resourceKind":       next.ResourceKind,
			"resourceId":         next.ResourceID,
			"previousAuthority":  previous.Authority,
			"newAuthority":       next.Authority,
			"repositoryPath":     next.RepositoryPath,
			"sourceRevision":     next.SourceRevision,
			"documentChecksum":   next.DocumentChecksum,
			"actorUserAccountId": next.UpdatedByUserID,
		},
	); err != nil {
		return mapConfigAsCodeAuthorityWriteError("insert audit event", err)
	}
	return nil
}

func ensureConfigAsCodeResourceExists(
	ctx context.Context,
	organizationID uuid.UUID,
	resourceKind types.ConfigAsCodeResourceKind,
	resourceID uuid.UUID,
	forUpdate bool,
) error {
	table, ok := configAsCodeResourceTables[resourceKind]
	if !ok {
		return apierrors.NewBadRequest("unsupported config-as-code resource kind")
	}
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}

	db := internalctx.GetDb(ctx)
	var foundID uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT id FROM `+table+`
		WHERE id = @resourceId AND organization_id = @organizationId`+lockClause,
		pgx.NamedArgs{
			"organizationId": organizationID,
			"resourceId":     resourceID,
		},
	).Scan(&foundID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierrors.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("could not query %s for config-as-code authority: %w", table, err)
	}
	return nil
}

func normalizeConfigAsCodeAuthority(authority *types.ConfigAsCodeAuthority) error {
	if authority.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if authority.ResourceID == uuid.Nil {
		return apierrors.NewBadRequest("resourceId is required")
	}
	if _, ok := configAsCodeResourceTables[authority.ResourceKind]; !ok {
		return apierrors.NewBadRequest("unsupported config-as-code resource kind")
	}
	if authority.Authority == "" {
		authority.Authority = types.ConfigAsCodeAuthorityDatabaseManaged
	}
	if authority.Authority != types.ConfigAsCodeAuthorityDatabaseManaged &&
		authority.Authority != types.ConfigAsCodeAuthorityGitManaged {
		return apierrors.NewBadRequest("unsupported config-as-code authority")
	}
	authority.RepositoryPath = strings.TrimSpace(authority.RepositoryPath)
	authority.SourceRevision = strings.TrimSpace(authority.SourceRevision)
	authority.DocumentChecksum = strings.TrimSpace(authority.DocumentChecksum)
	if authority.Authority == types.ConfigAsCodeAuthorityDatabaseManaged {
		authority.RepositoryPath = ""
		authority.SourceRevision = ""
		authority.DocumentChecksum = ""
		return nil
	}
	if authority.RepositoryPath == "" {
		return apierrors.NewBadRequest("repositoryPath is required for Git-managed resources")
	}
	if err := configascode.ValidateRepositoryPath(authority.RepositoryPath); err != nil {
		return apierrors.NewBadRequest("repositoryPath " + err.Error())
	}
	if authority.DocumentChecksum == "" {
		return apierrors.NewBadRequest("documentChecksum is required for Git-managed resources")
	}
	return nil
}

func mapConfigAsCodeAuthorityWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s ConfigAsCodeAuthority: %w", action, apierrors.ErrAlreadyExists)
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s ConfigAsCodeAuthority: %w", action, apierrors.ErrNotFound)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s ConfigAsCodeAuthority: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s ConfigAsCodeAuthority: %w", action, err)
}
