package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/processsnapshots"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const processSnapshotOutputExpr = `
	ps.id,
	ps.created_at,
	ps.organization_id,
	ps.application_id,
	ps.deployment_process_id,
	ps.deployment_process_revision_id,
	ps.revision_number,
	ps.canonical_checksum,
	ps.canonical_payload
`

func GetProcessSnapshot(ctx context.Context, id, organizationID uuid.UUID) (*types.ProcessSnapshot, error) {
	return getProcessSnapshot(ctx, id, organizationID)
}

func GetProcessSnapshotForReleaseBundle(
	ctx context.Context,
	releaseBundleID uuid.UUID,
	organizationID uuid.UUID,
) (*types.ProcessSnapshot, error) {
	bundle, err := getReleaseBundle(ctx, releaseBundleID, organizationID, false)
	if err != nil {
		return nil, err
	}
	if bundle.ProcessSnapshotID == nil {
		return nil, apierrors.ErrNotFound
	}
	return getProcessSnapshot(ctx, *bundle.ProcessSnapshotID, organizationID)
}

func ensureProcessSnapshotForRevision(
	ctx context.Context,
	organizationID uuid.UUID,
	applicationID uuid.UUID,
	revisionID uuid.UUID,
) (*types.ProcessSnapshot, error) {
	process, revision, err := getDeploymentProcessRevisionForSnapshot(ctx, organizationID, applicationID, revisionID)
	if err != nil {
		return nil, err
	}
	payload, checksum, err := processsnapshots.Canonicalize(*process, *revision)
	if err != nil {
		return nil, fmt.Errorf("could not canonicalize ProcessSnapshot: %w", err)
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(
		ctx,
		`INSERT INTO ProcessSnapshot AS ps (
			organization_id,
			application_id,
			deployment_process_id,
			deployment_process_revision_id,
			revision_number,
			canonical_checksum,
			canonical_payload
		) VALUES (
			@organizationId,
			@applicationId,
			@deploymentProcessId,
			@deploymentProcessRevisionId,
			@revisionNumber,
			@canonicalChecksum,
			@canonicalPayload
		)
		ON CONFLICT (deployment_process_revision_id) DO NOTHING
		RETURNING `+processSnapshotOutputExpr,
		pgx.NamedArgs{
			"organizationId":              organizationID,
			"applicationId":               applicationID,
			"deploymentProcessId":         process.ID,
			"deploymentProcessRevisionId": revision.ID,
			"revisionNumber":              revision.RevisionNumber,
			"canonicalChecksum":           checksum,
			"canonicalPayload":            payload,
		},
	)
	if err != nil {
		return nil, mapReleaseBundleWriteError("insert process snapshot", err)
	}
	snapshot, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ProcessSnapshot])
	if errors.Is(err, pgx.ErrNoRows) {
		return getProcessSnapshotByRevision(ctx, revision.ID, organizationID)
	} else if err != nil {
		return nil, mapReleaseBundleWriteError("scan process snapshot", err)
	}
	snapshot.Revision = *revision
	return &snapshot, nil
}

func getDeploymentProcessRevisionForSnapshot(
	ctx context.Context,
	organizationID uuid.UUID,
	applicationID uuid.UUID,
	revisionID uuid.UUID,
) (*types.DeploymentProcess, *types.DeploymentProcessRevision, error) {
	db := internalctx.GetDb(ctx)
	var processID uuid.UUID
	err := db.QueryRow(
		ctx,
		`SELECT dp.id
		FROM DeploymentProcessRevision dpr
		JOIN DeploymentProcess dp ON dp.id = dpr.deployment_process_id
			AND dp.organization_id = dpr.organization_id
		WHERE dpr.id = @revisionId
			AND dpr.organization_id = @organizationId
			AND dp.application_id = @applicationId
		FOR KEY SHARE OF dpr, dp`,
		pgx.NamedArgs{
			"revisionId":     revisionID,
			"organizationId": organizationID,
			"applicationId":  applicationID,
		},
	).Scan(&processID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, nil, fmt.Errorf("could not query DeploymentProcess revision for ProcessSnapshot: %w", err)
	}

	process, err := getDeploymentProcess(ctx, processID, organizationID, false)
	if err != nil {
		return nil, nil, err
	}
	revision, err := getDeploymentProcessRevision(ctx, process.ID, revisionID, organizationID)
	if err != nil {
		return nil, nil, err
	}
	return process, revision, nil
}

func getProcessSnapshot(ctx context.Context, id, organizationID uuid.UUID) (*types.ProcessSnapshot, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(
		ctx,
		`SELECT `+processSnapshotOutputExpr+`
		FROM ProcessSnapshot ps
		WHERE ps.id = @id AND ps.organization_id = @organizationId`,
		pgx.NamedArgs{"id": id, "organizationId": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ProcessSnapshot: %w", err)
	}
	snapshot, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ProcessSnapshot])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect ProcessSnapshot: %w", err)
	}
	if err := hydrateProcessSnapshotRevision(&snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func getProcessSnapshotByRevision(
	ctx context.Context,
	revisionID uuid.UUID,
	organizationID uuid.UUID,
) (*types.ProcessSnapshot, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(
		ctx,
		`SELECT `+processSnapshotOutputExpr+`
		FROM ProcessSnapshot ps
		WHERE ps.deployment_process_revision_id = @revisionId
			AND ps.organization_id = @organizationId`,
		pgx.NamedArgs{"revisionId": revisionID, "organizationId": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query ProcessSnapshot by revision: %w", err)
	}
	snapshot, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ProcessSnapshot])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect ProcessSnapshot by revision: %w", err)
	}
	if err := hydrateProcessSnapshotRevision(&snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func hydrateProcessSnapshotRevision(snapshot *types.ProcessSnapshot) error {
	revision, err := processsnapshots.DecodeRevision(snapshot.CanonicalPayload)
	if err != nil {
		return fmt.Errorf("could not decode ProcessSnapshot payload: %w", err)
	}
	revision.OrganizationID = snapshot.OrganizationID
	revision.ID = snapshot.DeploymentProcessRevisionID
	revision.DeploymentProcessID = snapshot.DeploymentProcessID
	revision.RevisionNumber = snapshot.RevisionNumber
	for i := range revision.Steps {
		revision.Steps[i].DeploymentProcessRevisionID = snapshot.DeploymentProcessRevisionID
	}
	snapshot.Revision = revision
	return nil
}
