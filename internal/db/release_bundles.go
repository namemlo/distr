package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/releasebundles"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	releaseBundleOutputExpr = `
	rb.id,
	rb.created_at,
	rb.updated_at,
	rb.organization_id,
	rb.application_id,
	rb.channel_id,
	rb.release_number,
	rb.release_notes,
	rb.source_revision,
	rb.status,
	rb.canonical_checksum,
	rb.canonical_payload
`
	releaseBundleComponentOutputExpr = `
	rbc.id,
	rbc.release_bundle_id,
	rbc.key,
	rbc.name,
	rbc.component_type,
	rbc.version,
	rbc.application_version_id,
	rbc.package_ref,
	rbc.digest,
	rbc.checksum,
	rbc.child_release_bundle_id
`
)

func CreateReleaseBundle(ctx context.Context, bundle *types.ReleaseBundle) error {
	return RunTx(ctx, func(ctx context.Context) error {
		if bundle.Status == "" {
			bundle.Status = types.ReleaseBundleStatusDraft
		}
		if bundle.Status != types.ReleaseBundleStatusDraft {
			return fmt.Errorf("could not create ReleaseBundle: %w", apierrors.ErrConflict)
		}
		if err := ensureReleaseBundleReferences(ctx, *bundle); err != nil {
			return err
		}
		if err := setReleaseBundleCanonicalFields(bundle); err != nil {
			return err
		}

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`INSERT INTO ReleaseBundle AS rb (
				organization_id,
				application_id,
				channel_id,
				release_number,
				release_notes,
				source_revision,
				status,
				canonical_checksum,
				canonical_payload
			) VALUES (
				@organizationId,
				@applicationId,
				@channelId,
				@releaseNumber,
				@releaseNotes,
				@sourceRevision,
				@status,
				@canonicalChecksum,
				@canonicalPayload::jsonb
			) RETURNING `+releaseBundleOutputExpr,
			pgx.NamedArgs{
				"organizationId":    bundle.OrganizationID,
				"applicationId":     bundle.ApplicationID,
				"channelId":         bundle.ChannelID,
				"releaseNumber":     bundle.ReleaseNumber,
				"releaseNotes":      bundle.ReleaseNotes,
				"sourceRevision":    bundle.SourceRevision,
				"status":            bundle.Status,
				"canonicalChecksum": bundle.CanonicalChecksum,
				"canonicalPayload":  string(bundle.CanonicalPayload),
			},
		)
		if err != nil {
			return mapReleaseBundleWriteError("insert", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ReleaseBundle])
		if err != nil {
			return mapReleaseBundleWriteError("scan created", err)
		}
		if err := insertReleaseBundleComponents(ctx, created.ID, bundle.Components); err != nil {
			return err
		}
		loaded, err := getReleaseBundle(ctx, created.ID, bundle.OrganizationID, false)
		if err != nil {
			return err
		}
		*bundle = *loaded
		return nil
	})
}

func GetReleaseBundlesByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]types.ReleaseBundle, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+releaseBundleOutputExpr+`
		FROM ReleaseBundle rb
		WHERE rb.organization_id = @organizationId
		ORDER BY rb.application_id, rb.release_number, rb.id`,
		pgx.NamedArgs{"organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ReleaseBundle: %w", err)
	}
	bundles, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ReleaseBundle])
	if err != nil {
		return nil, fmt.Errorf("could not collect ReleaseBundle: %w", err)
	}
	for i := range bundles {
		components, err := getReleaseBundleComponents(ctx, bundles[i].ID)
		if err != nil {
			return nil, err
		}
		bundles[i].Components = components
	}
	return bundles, nil
}

func GetReleaseBundle(ctx context.Context, id, orgID uuid.UUID) (*types.ReleaseBundle, error) {
	return getReleaseBundle(ctx, id, orgID, false)
}

func getReleaseBundle(ctx context.Context, id, orgID uuid.UUID, forUpdate bool) (*types.ReleaseBundle, error) {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+releaseBundleOutputExpr+`
		FROM ReleaseBundle rb
		WHERE rb.id = @id AND rb.organization_id = @organizationId`+lockClause,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ReleaseBundle: %w", err)
	}
	bundle, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ReleaseBundle])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect ReleaseBundle: %w", err)
	}
	bundle.Components, err = getReleaseBundleComponents(ctx, bundle.ID)
	if err != nil {
		return nil, err
	}
	return &bundle, nil
}

func UpdateReleaseBundle(ctx context.Context, bundle *types.ReleaseBundle) error {
	return RunTx(ctx, func(ctx context.Context) error {
		existing, err := getReleaseBundle(ctx, bundle.ID, bundle.OrganizationID, true)
		if err != nil {
			return err
		}
		if existing.Status != types.ReleaseBundleStatusDraft {
			return fmt.Errorf("could not update ReleaseBundle: %w", apierrors.ErrConflict)
		}
		bundle.Status = existing.Status
		if err := ensureReleaseBundleReferences(ctx, *bundle); err != nil {
			return err
		}
		if err := setReleaseBundleCanonicalFields(bundle); err != nil {
			return err
		}

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`UPDATE ReleaseBundle AS rb SET
				application_id = @applicationId,
				channel_id = @channelId,
				release_number = @releaseNumber,
				release_notes = @releaseNotes,
				source_revision = @sourceRevision,
				canonical_checksum = @canonicalChecksum,
				canonical_payload = @canonicalPayload::jsonb,
				updated_at = now()
			WHERE rb.id = @id AND rb.organization_id = @organizationId
			RETURNING `+releaseBundleOutputExpr,
			pgx.NamedArgs{
				"id":                bundle.ID,
				"organizationId":    bundle.OrganizationID,
				"applicationId":     bundle.ApplicationID,
				"channelId":         bundle.ChannelID,
				"releaseNumber":     bundle.ReleaseNumber,
				"releaseNotes":      bundle.ReleaseNotes,
				"sourceRevision":    bundle.SourceRevision,
				"canonicalChecksum": bundle.CanonicalChecksum,
				"canonicalPayload":  string(bundle.CanonicalPayload),
			},
		)
		if err != nil {
			return mapReleaseBundleWriteError("update", err)
		}
		updated, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ReleaseBundle])
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		} else if err != nil {
			return mapReleaseBundleWriteError("scan updated", err)
		}
		if _, err := db.Exec(
			ctx,
			`DELETE FROM ReleaseBundleComponent WHERE release_bundle_id = @releaseBundleId`,
			pgx.NamedArgs{"releaseBundleId": bundle.ID},
		); err != nil {
			return fmt.Errorf("could not replace ReleaseBundle components: %w", err)
		}
		if err := insertReleaseBundleComponents(ctx, bundle.ID, bundle.Components); err != nil {
			return err
		}
		loaded, err := getReleaseBundle(ctx, updated.ID, bundle.OrganizationID, false)
		if err != nil {
			return err
		}
		*bundle = *loaded
		return nil
	})
}

func DeleteReleaseBundleWithID(ctx context.Context, id, organizationID uuid.UUID) error {
	return RunTx(ctx, func(ctx context.Context) error {
		bundle, err := getReleaseBundle(ctx, id, organizationID, true)
		if err != nil {
			return err
		}
		if bundle.Status != types.ReleaseBundleStatusDraft {
			return fmt.Errorf("could not delete ReleaseBundle: %w", apierrors.ErrConflict)
		}

		db := internalctx.GetDb(ctx)
		cmd, err := db.Exec(
			ctx,
			`DELETE FROM ReleaseBundle WHERE id = @id AND organization_id = @organizationId`,
			pgx.NamedArgs{"id": id, "organizationId": organizationID},
		)
		if err != nil {
			return mapReleaseBundleWriteError("delete", err)
		}
		if cmd.RowsAffected() == 0 {
			return apierrors.ErrNotFound
		}
		return nil
	})
}

func getReleaseBundleComponents(
	ctx context.Context,
	releaseBundleID uuid.UUID,
) ([]types.ReleaseBundleComponent, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+releaseBundleComponentOutputExpr+`
		FROM ReleaseBundleComponent rbc
		WHERE rbc.release_bundle_id = @releaseBundleId
		ORDER BY rbc.key, rbc.id`,
		pgx.NamedArgs{"releaseBundleId": releaseBundleID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ReleaseBundleComponent: %w", err)
	}
	components, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ReleaseBundleComponent])
	if err != nil {
		return nil, fmt.Errorf("could not collect ReleaseBundleComponent: %w", err)
	}
	return components, nil
}

func insertReleaseBundleComponents(
	ctx context.Context,
	releaseBundleID uuid.UUID,
	components []types.ReleaseBundleComponent,
) error {
	if len(components) == 0 {
		return nil
	}
	rows := make([]types.ReleaseBundleComponent, len(components))
	for i, component := range components {
		component.ID = uuid.New()
		component.ReleaseBundleID = releaseBundleID
		rows[i] = component
	}

	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"releasebundlecomponent"},
		[]string{
			"id",
			"release_bundle_id",
			"key",
			"name",
			"component_type",
			"version",
			"application_version_id",
			"package_ref",
			"digest",
			"checksum",
			"child_release_bundle_id",
		},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			component := rows[i]
			return []any{
				component.ID,
				component.ReleaseBundleID,
				component.Key,
				component.Name,
				component.Type,
				component.Version,
				component.ApplicationVersionID,
				component.PackageRef,
				component.Digest,
				component.Checksum,
				component.ChildReleaseBundleID,
			}, nil
		}),
	)
	if err != nil {
		return mapReleaseBundleWriteError("insert components", err)
	}
	return nil
}

func setReleaseBundleCanonicalFields(bundle *types.ReleaseBundle) error {
	payload, checksum, err := releasebundles.Canonicalize(*bundle)
	if err != nil {
		return fmt.Errorf("could not canonicalize ReleaseBundle: %w", err)
	}
	bundle.CanonicalPayload = payload
	bundle.CanonicalChecksum = checksum
	return nil
}

func ensureReleaseBundleReferences(ctx context.Context, bundle types.ReleaseBundle) error {
	if err := ensureReleaseBundleParentReferences(ctx, bundle); err != nil {
		return err
	}
	for _, component := range bundle.Components {
		if err := ensureReleaseBundleComponentReferences(ctx, bundle, component); err != nil {
			return err
		}
	}
	return nil
}

func ensureReleaseBundleParentReferences(ctx context.Context, bundle types.ReleaseBundle) error {
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
				FROM Channel
				WHERE id = @channelId
					AND organization_id = @organizationId
					AND application_id = @applicationId
			)`,
		pgx.NamedArgs{
			"organizationId": bundle.OrganizationID,
			"applicationId":  bundle.ApplicationID,
			"channelId":      bundle.ChannelID,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate ReleaseBundle references: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func ensureReleaseBundleComponentReferences(
	ctx context.Context,
	bundle types.ReleaseBundle,
	component types.ReleaseBundleComponent,
) error {
	switch component.Type {
	case types.ReleaseBundleComponentTypeApplicationVersion:
		return ensureReleaseBundleApplicationVersionReference(ctx, bundle, component)
	case types.ReleaseBundleComponentTypeChildReleaseBundle:
		return ensureReleaseBundleChildReference(ctx, bundle, component)
	default:
		return nil
	}
}

func ensureReleaseBundleApplicationVersionReference(
	ctx context.Context,
	bundle types.ReleaseBundle,
	component types.ReleaseBundleComponent,
) error {
	if component.ApplicationVersionID == nil {
		return apierrors.ErrNotFound
	}
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM ApplicationVersion av
			JOIN Application a ON a.id = av.application_id
			WHERE av.id = @applicationVersionId
				AND a.id = @applicationId
				AND a.organization_id = @organizationId
		)`,
		pgx.NamedArgs{
			"organizationId":       bundle.OrganizationID,
			"applicationId":        bundle.ApplicationID,
			"applicationVersionId": *component.ApplicationVersionID,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate ReleaseBundle component references: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func ensureReleaseBundleChildReference(
	ctx context.Context,
	bundle types.ReleaseBundle,
	component types.ReleaseBundleComponent,
) error {
	if component.ChildReleaseBundleID == nil || *component.ChildReleaseBundleID == bundle.ID {
		return apierrors.ErrNotFound
	}
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM ReleaseBundle
			WHERE id = @childReleaseBundleId
				AND organization_id = @organizationId
		)`,
		pgx.NamedArgs{
			"organizationId":       bundle.OrganizationID,
			"childReleaseBundleId": *component.ChildReleaseBundleID,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate ReleaseBundle child reference: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func mapReleaseBundleWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s ReleaseBundle: %w", action, apierrors.ErrAlreadyExists)
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s ReleaseBundle: %w", action, apierrors.ErrConflict)
		}
	}
	return fmt.Errorf("could not %s ReleaseBundle: %w", action, err)
}
