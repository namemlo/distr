package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/deploymentcompat"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const deploymentCompatibilityMetadataSelect = `
	id,
	created_at,
	organization_id,
	legacy_deployment_id,
	legacy_deployment_revision_id,
	deployment_target_id,
	application_id,
	application_version_id,
	synthetic_release_id,
	source,
	canonical_checksum,
	canonical_payload,
	process_snapshot_available,
	variable_snapshot_available,
	channel_available,
	environment_available,
	task_logs_available,
	redeploy_plan_available
`

type legacyDeploymentCompatibilityCandidate struct {
	Deployment             types.Deployment
	Revision               types.DeploymentRevision
	OrganizationID         uuid.UUID
	ApplicationID          uuid.UUID
	ApplicationName        string
	ApplicationVersionName string
	AlreadyPresent         bool
}

func GetDeploymentCompatibilityByRevision(
	ctx context.Context,
	orgID uuid.UUID,
	revisionID uuid.UUID,
) (*types.DeploymentCompatibilityMetadata, error) {
	db := internalctx.GetDb(ctx)
	row := db.QueryRow(
		ctx,
		`SELECT `+deploymentCompatibilityMetadataSelect+`
		FROM DeploymentCompatibilityMetadata
		WHERE organization_id = @organizationId
			AND legacy_deployment_revision_id = @legacyDeploymentRevisionId`,
		pgx.NamedArgs{
			"organizationId":             orgID,
			"legacyDeploymentRevisionId": revisionID,
		},
	)
	metadata, err := scanDeploymentCompatibilityMetadata(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not get deployment compatibility metadata: %w", err)
	}
	return metadata, nil
}

func BackfillLegacyDeploymentCompatibility(
	ctx context.Context,
	request types.DeploymentCompatibilityBackfillRequest,
) (*types.DeploymentCompatibilityBackfillReport, error) {
	if request.OrganizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if request.Cursor != nil && request.Cursor.LegacyDeploymentRevisionID == uuid.Nil {
		return nil, apierrors.NewBadRequest("cursor legacyDeploymentRevisionId is required")
	}
	report := &types.DeploymentCompatibilityBackfillReport{}
	cursor := request.Cursor
	for {
		batchRequest := request
		batchRequest.Cursor = cursor
		candidates, err := listLegacyDeploymentCompatibilityCandidates(ctx, batchRequest)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			break
		}
		batchReport := &types.DeploymentCompatibilityBackfillReport{}
		if request.Apply {
			err = RunTx(ctx, func(txCtx context.Context) error {
				processLegacyDeploymentCompatibilityCandidates(txCtx, candidates, batchRequest, batchReport)
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else {
			processLegacyDeploymentCompatibilityCandidates(ctx, candidates, batchRequest, batchReport)
		}
		mergeDeploymentCompatibilityBackfillReport(report, batchReport)
		if batchReport.LastCursor == nil {
			break
		}
		cursor = batchReport.LastCursor
	}
	return report, nil
}

func mergeDeploymentCompatibilityBackfillReport(
	total *types.DeploymentCompatibilityBackfillReport,
	batch *types.DeploymentCompatibilityBackfillReport,
) {
	total.Scanned += batch.Scanned
	total.Eligible += batch.Eligible
	total.Projected += batch.Projected
	total.AlreadyPresent += batch.AlreadyPresent
	total.Skipped += batch.Skipped
	total.Failed += batch.Failed
	if batch.LastCursor != nil {
		total.LastCursor = batch.LastCursor
	}
}

func listLegacyDeploymentCompatibilityCandidates(
	ctx context.Context,
	request types.DeploymentCompatibilityBackfillRequest,
) ([]legacyDeploymentCompatibilityCandidate, error) {
	batchSize := request.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}
	if batchSize > 5000 {
		batchSize = 5000
	}
	var cursorCreatedAt any
	var cursorRevisionID any
	if request.Cursor != nil {
		cursorCreatedAt = request.Cursor.CreatedAt
		cursorRevisionID = request.Cursor.LegacyDeploymentRevisionID
	}
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(
		ctx,
		`SELECT
			dt.organization_id,
			d.id,
			d.created_at,
			d.deployment_target_id,
			d.release_name,
			d.application_entitlement_id,
			d.docker_type,
			dr.id,
			dr.created_at,
			dr.deployment_id,
			dr.application_version_id,
			dr.values_hash,
			dr.force_restart,
			dr.ignore_revision_skew,
			a.id,
			a.name,
			av.name,
			dcm.id IS NOT NULL AS already_present
		FROM DeploymentRevision dr
		JOIN Deployment d ON d.id = dr.deployment_id
		JOIN DeploymentTarget dt ON dt.id = d.deployment_target_id
		JOIN ApplicationVersion av ON av.id = dr.application_version_id
		JOIN Application a ON a.id = av.application_id
			AND a.organization_id = dt.organization_id
		LEFT JOIN DeploymentCompatibilityMetadata dcm
			ON dcm.organization_id = dt.organization_id
			AND dcm.legacy_deployment_revision_id = dr.id
		WHERE dt.organization_id = @organizationId
			AND (
				@cursorCreatedAt::timestamp IS NULL
				OR (dr.created_at, dr.id) > (@cursorCreatedAt::timestamp, @cursorRevisionId::uuid)
			)
		ORDER BY dr.created_at, dr.id
		LIMIT @batchSize`,
		pgx.NamedArgs{
			"organizationId":   request.OrganizationID,
			"cursorCreatedAt":  cursorCreatedAt,
			"cursorRevisionId": cursorRevisionID,
			"batchSize":        batchSize,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query legacy deployment compatibility candidates: %w", err)
	}
	defer rows.Close()

	candidates := []legacyDeploymentCompatibilityCandidate{}
	for rows.Next() {
		var candidate legacyDeploymentCompatibilityCandidate
		if err := rows.Scan(
			&candidate.OrganizationID,
			&candidate.Deployment.ID,
			&candidate.Deployment.CreatedAt,
			&candidate.Deployment.DeploymentTargetID,
			&candidate.Deployment.ReleaseName,
			&candidate.Deployment.ApplicationEntitlementID,
			&candidate.Deployment.DockerType,
			&candidate.Revision.ID,
			&candidate.Revision.CreatedAt,
			&candidate.Revision.DeploymentID,
			&candidate.Revision.ApplicationVersionID,
			&candidate.Revision.ValuesHash,
			&candidate.Revision.ForceRestart,
			&candidate.Revision.IgnoreRevisionSkew,
			&candidate.ApplicationID,
			&candidate.ApplicationName,
			&candidate.ApplicationVersionName,
			&candidate.AlreadyPresent,
		); err != nil {
			return nil, fmt.Errorf("could not scan legacy deployment compatibility candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("could not collect legacy deployment compatibility candidates: %w", rows.Err())
	}
	return candidates, nil
}

func processLegacyDeploymentCompatibilityCandidates(
	ctx context.Context,
	candidates []legacyDeploymentCompatibilityCandidate,
	request types.DeploymentCompatibilityBackfillRequest,
	report *types.DeploymentCompatibilityBackfillReport,
) {
	for _, candidate := range candidates {
		report.Scanned++
		report.LastCursor = &types.DeploymentCompatibilityCursor{
			CreatedAt:                  candidate.Revision.CreatedAt,
			LegacyDeploymentRevisionID: candidate.Revision.ID,
		}
		if candidate.AlreadyPresent {
			report.Eligible++
			report.AlreadyPresent++
			continue
		}
		projection, err := deploymentcompat.ProjectLegacyDeployment(
			candidate.Deployment,
			candidate.Revision,
			deploymentcompat.ProjectionContext{
				OrganizationID:         candidate.OrganizationID,
				ApplicationID:          candidate.ApplicationID,
				ApplicationName:        candidate.ApplicationName,
				ApplicationVersionName: candidate.ApplicationVersionName,
			},
		)
		if err != nil {
			report.Skipped++
			report.Failed++
			continue
		}
		report.Eligible++
		if !request.Apply {
			report.Projected++
			continue
		}
		inserted, err := insertDeploymentCompatibilityProjection(ctx, projection)
		if err != nil {
			report.Failed++
			continue
		}
		if inserted {
			report.Projected++
		} else {
			report.AlreadyPresent++
		}
	}
}

func insertDeploymentCompatibilityProjection(
	ctx context.Context,
	projection types.LegacyDeploymentProjection,
) (bool, error) {
	db := internalctx.GetDb(ctx)
	cmd, err := db.Exec(
		ctx,
		`INSERT INTO DeploymentCompatibilityMetadata (
			organization_id,
			legacy_deployment_id,
			legacy_deployment_revision_id,
			deployment_target_id,
			application_id,
			application_version_id,
			synthetic_release_id,
			source,
			canonical_checksum,
			canonical_payload,
			process_snapshot_available,
			variable_snapshot_available,
			channel_available,
			environment_available,
			task_logs_available,
			redeploy_plan_available
		) VALUES (
			@organizationId,
			@legacyDeploymentId,
			@legacyDeploymentRevisionId,
			@deploymentTargetId,
			@applicationId,
			@applicationVersionId,
			@syntheticReleaseId,
			@source,
			@canonicalChecksum,
			@canonicalPayload,
			@processSnapshotAvailable,
			@variableSnapshotAvailable,
			@channelAvailable,
			@environmentAvailable,
			@taskLogsAvailable,
			@redeployPlanAvailable
		)
		ON CONFLICT (organization_id, legacy_deployment_revision_id) DO NOTHING`,
		pgx.NamedArgs{
			"organizationId":             projection.OrganizationID,
			"legacyDeploymentId":         projection.LegacyDeploymentID,
			"legacyDeploymentRevisionId": projection.LegacyDeploymentRevisionID,
			"deploymentTargetId":         projection.DeploymentTargetID,
			"applicationId":              projection.ApplicationID,
			"applicationVersionId":       projection.ApplicationVersionID,
			"syntheticReleaseId":         projection.SyntheticReleaseID,
			"source":                     projection.Source,
			"canonicalChecksum":          projection.CanonicalChecksum,
			"canonicalPayload":           projection.CanonicalPayload,
			"processSnapshotAvailable":   projection.Availability.ProcessSnapshot,
			"variableSnapshotAvailable":  projection.Availability.VariableSnapshot,
			"channelAvailable":           projection.Availability.Channel,
			"environmentAvailable":       projection.Availability.Environment,
			"taskLogsAvailable":          projection.Availability.TaskLogs,
			"redeployPlanAvailable":      projection.Availability.RedeployPlan,
		},
	)
	if err != nil {
		return false, fmt.Errorf("could not insert deployment compatibility metadata: %w", err)
	}
	return cmd.RowsAffected() > 0, nil
}

type deploymentCompatibilityMetadataScanner interface {
	Scan(dest ...any) error
}

func scanDeploymentCompatibilityMetadata(
	row deploymentCompatibilityMetadataScanner,
) (*types.DeploymentCompatibilityMetadata, error) {
	var metadata types.DeploymentCompatibilityMetadata
	if err := row.Scan(
		&metadata.ID,
		&metadata.CreatedAt,
		&metadata.OrganizationID,
		&metadata.LegacyDeploymentID,
		&metadata.LegacyDeploymentRevisionID,
		&metadata.DeploymentTargetID,
		&metadata.ApplicationID,
		&metadata.ApplicationVersionID,
		&metadata.SyntheticReleaseID,
		&metadata.Source,
		&metadata.CanonicalChecksum,
		&metadata.CanonicalPayload,
		&metadata.Availability.ProcessSnapshot,
		&metadata.Availability.VariableSnapshot,
		&metadata.Availability.Channel,
		&metadata.Availability.Environment,
		&metadata.Availability.TaskLogs,
		&metadata.Availability.RedeployPlan,
	); err != nil {
		return nil, err
	}
	return &metadata, nil
}
