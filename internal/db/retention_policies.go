package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const retentionPolicyOutputExpr = `
	rp.id,
	rp.created_at,
	rp.updated_at,
	rp.organization_id,
	rp.name,
	rp.description,
	rp.keep_last_successful_releases,
	rp.failed_task_retention_days,
	rp.production_failed_task_retention_days,
	rp.step_log_retention_days,
	rp.protect_currently_deployed_releases,
	rp.protect_retention_protected_releases,
	rp.minimum_audit_retention_days
`

const retentionCleanupJobOutputExpr = `
	rcj.id,
	rcj.created_at,
	rcj.updated_at,
	rcj.organization_id,
	rcj.retention_policy_id,
	rcj.actor_user_account_id,
	rcj.dry_run,
	rcj.status,
	rcj.release_candidate_count,
	rcj.failed_task_candidate_count,
	rcj.step_log_candidate_count,
	rcj.safety_block_count,
	rcj.plan,
	rcj.message
`

type retentionReleaseCandidateRow struct {
	releaseBundleID    uuid.UUID
	applicationID      uuid.UUID
	releaseNumber      string
	status             types.ReleaseBundleStatus
	publishedAt        *time.Time
	retentionProtected bool
	lastSuccessfulAt   *time.Time
	successfulRank     int
	currentlyDeployed  bool
}

func CreateRetentionPolicy(
	ctx context.Context,
	request types.CreateRetentionPolicyRequest,
) (*types.RetentionPolicy, error) {
	if err := validateCreateRetentionPolicyRequest(&request); err != nil {
		return nil, err
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`INSERT INTO RetentionPolicy AS rp (
			organization_id,
			name,
			description,
			keep_last_successful_releases,
			failed_task_retention_days,
			production_failed_task_retention_days,
			step_log_retention_days,
			protect_currently_deployed_releases,
			protect_retention_protected_releases,
			minimum_audit_retention_days
		) VALUES (
			@organizationId,
			@name,
			@description,
			@keepLastSuccessfulReleases,
			@failedTaskRetentionDays,
			@productionFailedTaskRetentionDays,
			@stepLogRetentionDays,
			@protectCurrentlyDeployedReleases,
			@protectRetentionProtectedReleases,
			@minimumAuditRetentionDays
		) RETURNING `+retentionPolicyOutputExpr,
		pgx.NamedArgs{
			"organizationId":                    request.OrganizationID,
			"name":                              request.Name,
			"description":                       request.Description,
			"keepLastSuccessfulReleases":        request.KeepLastSuccessfulReleases,
			"failedTaskRetentionDays":           request.FailedTaskRetentionDays,
			"productionFailedTaskRetentionDays": request.ProductionFailedTaskRetentionDays,
			"stepLogRetentionDays":              request.StepLogRetentionDays,
			"protectCurrentlyDeployedReleases":  request.ProtectCurrentlyDeployedReleases,
			"protectRetentionProtectedReleases": request.ProtectRetentionProtectedReleases,
			"minimumAuditRetentionDays":         request.MinimumAuditRetentionDays,
		},
	)
	if err != nil {
		return nil, mapRetentionPolicyWriteError("insert", err)
	}
	policy, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.RetentionPolicy])
	if err != nil {
		return nil, mapRetentionPolicyWriteError("scan created", err)
	}
	return &policy, nil
}

func GetRetentionPoliciesByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]types.RetentionPolicy, error) {
	if orgID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+retentionPolicyOutputExpr+`
		FROM RetentionPolicy rp
		WHERE rp.organization_id = @organizationId
		ORDER BY rp.created_at DESC, rp.id`,
		pgx.NamedArgs{"organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query RetentionPolicy: %w", err)
	}
	policies, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.RetentionPolicy])
	if err != nil {
		return nil, fmt.Errorf("could not collect RetentionPolicy: %w", err)
	}
	return policies, nil
}

func GetRetentionPolicy(ctx context.Context, id, orgID uuid.UUID) (*types.RetentionPolicy, error) {
	if orgID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if id == uuid.Nil {
		return nil, apierrors.NewBadRequest("retentionPolicyId is required")
	}
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+retentionPolicyOutputExpr+`
		FROM RetentionPolicy rp
		WHERE rp.id = @id AND rp.organization_id = @organizationId`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query RetentionPolicy: %w", err)
	}
	policy, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.RetentionPolicy])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect RetentionPolicy: %w", err)
	}
	return &policy, nil
}

func PreviewRetentionCleanup(
	ctx context.Context,
	request types.RetentionCleanupPreviewRequest,
) (*types.RetentionCleanupPreview, error) {
	if request.OrganizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if request.PolicyID == uuid.Nil {
		return nil, apierrors.NewBadRequest("retentionPolicyId is required")
	}
	if request.Now.IsZero() {
		request.Now = time.Now().UTC()
	}

	policy, err := GetRetentionPolicy(ctx, request.PolicyID, request.OrganizationID)
	if err != nil {
		return nil, err
	}
	releases, safetyBlocks, err := queryRetentionReleaseCandidates(ctx, *policy)
	if err != nil {
		return nil, err
	}
	failedTasks, err := queryRetentionFailedTaskCandidates(ctx, *policy, request.Now.UTC())
	if err != nil {
		return nil, err
	}
	stepLogs, err := queryRetentionStepLogCandidates(ctx, *policy, request.Now.UTC())
	if err != nil {
		return nil, err
	}

	return &types.RetentionCleanupPreview{
		Policy:               *policy,
		GeneratedAt:          request.Now.UTC(),
		ReleaseCandidates:    releases,
		FailedTaskCandidates: failedTasks,
		StepLogCandidates:    stepLogs,
		SafetyBlocks:         safetyBlocks,
	}, nil
}

func CreateRetentionCleanupJob(
	ctx context.Context,
	request types.CreateRetentionCleanupJobRequest,
) (*types.RetentionCleanupJob, error) {
	if request.OrganizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if request.PolicyID == uuid.Nil {
		return nil, apierrors.NewBadRequest("retentionPolicyId is required")
	}
	if !request.DryRun {
		return nil, apierrors.NewBadRequest("retention cleanup apply is not enabled; run a dry-run preview first")
	}

	var job *types.RetentionCleanupJob
	err := RunTx(ctx, func(ctx context.Context) error {
		preview, err := PreviewRetentionCleanup(ctx, types.RetentionCleanupPreviewRequest{
			OrganizationID: request.OrganizationID,
			PolicyID:       request.PolicyID,
			Now:            request.Now,
		})
		if err != nil {
			return err
		}
		plan, err := json.Marshal(preview)
		if err != nil {
			return fmt.Errorf("could not encode retention cleanup preview: %w", err)
		}

		db := internalctx.GetDb(ctx)
		inserted, err := scanRetentionCleanupJob(db.QueryRow(ctx,
			`INSERT INTO RetentionCleanupJob AS rcj (
				organization_id,
				retention_policy_id,
				actor_user_account_id,
				dry_run,
				status,
				release_candidate_count,
				failed_task_candidate_count,
				step_log_candidate_count,
				safety_block_count,
				plan,
				message
			) VALUES (
				@organizationId,
				@retentionPolicyId,
				@actorUserAccountId,
				true,
				@status,
				@releaseCandidateCount,
				@failedTaskCandidateCount,
				@stepLogCandidateCount,
				@safetyBlockCount,
				@plan::jsonb,
				@message
			) RETURNING `+retentionCleanupJobOutputExpr,
			pgx.NamedArgs{
				"organizationId":           request.OrganizationID,
				"retentionPolicyId":        request.PolicyID,
				"actorUserAccountId":       uuidOrNil(request.ActorUserID),
				"status":                   types.RetentionCleanupJobStatusPreviewed,
				"releaseCandidateCount":    len(preview.ReleaseCandidates),
				"failedTaskCandidateCount": len(preview.FailedTaskCandidates),
				"stepLogCandidateCount":    len(preview.StepLogCandidates),
				"safetyBlockCount":         len(preview.SafetyBlocks),
				"plan":                     plan,
				"message":                  "Dry-run cleanup preview recorded. Apply is disabled until a preview is reviewed.",
			},
		))
		if err != nil {
			return mapRetentionPolicyWriteError("insert cleanup job", err)
		}
		job = inserted
		return nil
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

func validateCreateRetentionPolicyRequest(request *types.CreateRetentionPolicyRequest) error {
	request.Name = strings.TrimSpace(request.Name)
	request.Description = strings.TrimSpace(request.Description)
	if request.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if request.Name == "" {
		return apierrors.NewBadRequest("name is required")
	}
	if request.KeepLastSuccessfulReleases < 0 {
		return apierrors.NewBadRequest("keepLastSuccessfulReleases must be greater than or equal to 0")
	}
	if request.FailedTaskRetentionDays < 0 {
		return apierrors.NewBadRequest("failedTaskRetentionDays must be greater than or equal to 0")
	}
	if request.ProductionFailedTaskRetentionDays < 0 {
		return apierrors.NewBadRequest("productionFailedTaskRetentionDays must be greater than or equal to 0")
	}
	if request.StepLogRetentionDays < 0 {
		return apierrors.NewBadRequest("stepLogRetentionDays must be greater than or equal to 0")
	}
	if request.MinimumAuditRetentionDays < 0 {
		return apierrors.NewBadRequest("minimumAuditRetentionDays must be greater than or equal to 0")
	}
	return nil
}

func queryRetentionReleaseCandidates(
	ctx context.Context,
	policy types.RetentionPolicy,
) ([]types.RetentionReleaseCandidate, []types.RetentionSafetyBlock, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`WITH successful_release_tasks AS (
			SELECT DISTINCT ON (rb.id)
				rb.id AS release_bundle_id,
				COALESCE(t.completed_at, t.started_at, t.queued_at) AS last_successful_at,
				t.queue_order AS last_successful_queue_order,
				t.id AS last_successful_task_id
			FROM ReleaseBundle rb
			JOIN Task t
				ON t.release_bundle_id = rb.id
				AND t.organization_id = rb.organization_id
			WHERE rb.organization_id = @organizationId
				AND t.task_type = @taskType
				AND t.status = @succeededStatus
			ORDER BY
				rb.id,
				COALESCE(t.completed_at, t.started_at, t.queued_at) DESC,
				t.queue_order DESC,
				t.id DESC
		),
		successful_ranked AS (
			SELECT
				srt.release_bundle_id,
				srt.last_successful_at,
				dense_rank() OVER (
					PARTITION BY rb.application_id
					ORDER BY
						srt.last_successful_at DESC,
						srt.last_successful_queue_order DESC,
						srt.last_successful_task_id DESC
				) AS successful_rank
			FROM successful_release_tasks srt
			JOIN ReleaseBundle rb
				ON rb.id = srt.release_bundle_id
		),
		current_deployed AS (
			SELECT DISTINCT ON (t.application_id, t.environment_id, t.deployment_target_id)
				t.release_bundle_id
			FROM Task t
			WHERE t.organization_id = @organizationId
				AND t.task_type = @taskType
				AND t.status = @succeededStatus
			ORDER BY
				t.application_id,
				t.environment_id,
				t.deployment_target_id,
				COALESCE(t.completed_at, t.started_at, t.queued_at) DESC,
				t.queue_order DESC,
				t.id DESC
		)
		SELECT
			rb.id,
			rb.application_id,
			rb.release_number,
			rb.status,
			rb.published_at,
			rb.retention_protected,
			sr.last_successful_at,
			COALESCE(sr.successful_rank, 0),
			cd.release_bundle_id IS NOT NULL AS currently_deployed
		FROM ReleaseBundle rb
		LEFT JOIN successful_ranked sr
			ON sr.release_bundle_id = rb.id
		LEFT JOIN current_deployed cd
			ON cd.release_bundle_id = rb.id
		WHERE rb.organization_id = @organizationId
			AND rb.status IN (@publishedStatus, @blockedStatus, @archivedStatus)
		ORDER BY COALESCE(sr.last_successful_at, rb.published_at, rb.created_at), rb.id`,
		pgx.NamedArgs{
			"organizationId":  policy.OrganizationID,
			"taskType":        types.TaskTypeDeployment,
			"succeededStatus": types.TaskStatusSucceeded,
			"publishedStatus": types.ReleaseBundleStatusPublished,
			"blockedStatus":   types.ReleaseBundleStatusBlocked,
			"archivedStatus":  types.ReleaseBundleStatusArchived,
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("could not query retention release candidates: %w", err)
	}
	defer rows.Close()

	candidates := []types.RetentionReleaseCandidate{}
	safetyBlocks := []types.RetentionSafetyBlock{}
	for rows.Next() {
		var row retentionReleaseCandidateRow
		if err := rows.Scan(
			&row.releaseBundleID,
			&row.applicationID,
			&row.releaseNumber,
			&row.status,
			&row.publishedAt,
			&row.retentionProtected,
			&row.lastSuccessfulAt,
			&row.successfulRank,
			&row.currentlyDeployed,
		); err != nil {
			return nil, nil, fmt.Errorf("could not scan retention release candidate: %w", err)
		}
		if row.currentlyDeployed && policy.ProtectCurrentlyDeployedReleases {
			safetyBlocks = append(safetyBlocks, types.RetentionSafetyBlock{
				ResourceType: "release_bundle",
				ResourceID:   row.releaseBundleID,
				Reason:       types.RetentionSafetyCurrentlyDeployed,
				Message:      "release is currently deployed to at least one target",
			})
			continue
		}
		if row.retentionProtected && policy.ProtectRetentionProtectedReleases {
			safetyBlocks = append(safetyBlocks, types.RetentionSafetyBlock{
				ResourceType: "release_bundle",
				ResourceID:   row.releaseBundleID,
				Reason:       types.RetentionSafetyProtectedRelease,
				Message:      "release is marked retention protected",
			})
			continue
		}
		if row.successfulRank > 0 && row.successfulRank <= policy.KeepLastSuccessfulReleases {
			safetyBlocks = append(safetyBlocks, types.RetentionSafetyBlock{
				ResourceType: "release_bundle",
				ResourceID:   row.releaseBundleID,
				Reason:       types.RetentionSafetyRecentSuccessRank,
				Message:      "release is inside the successful release retention window",
			})
			continue
		}
		candidates = append(candidates, types.RetentionReleaseCandidate{
			ReleaseBundleID:  row.releaseBundleID,
			ApplicationID:    row.applicationID,
			ReleaseNumber:    row.releaseNumber,
			Status:           row.status,
			PublishedAt:      row.publishedAt,
			LastSuccessfulAt: row.lastSuccessfulAt,
			SuccessfulRank:   row.successfulRank,
			Reason:           "outside successful release retention window",
		})
	}
	if rows.Err() != nil {
		return nil, nil, fmt.Errorf("could not collect retention release candidates: %w", rows.Err())
	}
	return candidates, safetyBlocks, nil
}

func queryRetentionFailedTaskCandidates(
	ctx context.Context,
	policy types.RetentionPolicy,
	now time.Time,
) ([]types.RetentionTaskCandidate, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			t.id,
			t.release_bundle_id,
			t.application_id,
			t.environment_id,
			t.deployment_target_id,
			t.status,
			t.completed_at,
			e.is_production
		FROM Task t
		JOIN Environment e
			ON e.id = t.environment_id
			AND e.organization_id = t.organization_id
		WHERE t.organization_id = @organizationId
			AND t.task_type = @taskType
			AND t.status IN (@failedStatus, @canceledStatus)
			AND t.completed_at IS NOT NULL
		ORDER BY t.completed_at, t.id`,
		pgx.NamedArgs{
			"organizationId": policy.OrganizationID,
			"taskType":       types.TaskTypeDeployment,
			"failedStatus":   types.TaskStatusFailed,
			"canceledStatus": types.TaskStatusCanceled,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query retention failed task candidates: %w", err)
	}
	defer rows.Close()

	candidates := []types.RetentionTaskCandidate{}
	for rows.Next() {
		var candidate types.RetentionTaskCandidate
		var isProduction bool
		if err := rows.Scan(
			&candidate.TaskID,
			&candidate.ReleaseBundleID,
			&candidate.ApplicationID,
			&candidate.EnvironmentID,
			&candidate.DeploymentTargetID,
			&candidate.Status,
			&candidate.CompletedAt,
			&isProduction,
		); err != nil {
			return nil, fmt.Errorf("could not scan retention failed task candidate: %w", err)
		}
		candidate.RetentionDays = policy.FailedTaskRetentionDays
		if isProduction {
			candidate.RetentionDays = policy.ProductionFailedTaskRetentionDays
		}
		if candidate.CompletedAt.After(now.AddDate(0, 0, -candidate.RetentionDays)) {
			continue
		}
		candidate.Reason = "failed deployment task exceeded retention window"
		candidates = append(candidates, candidate)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("could not collect retention failed task candidates: %w", rows.Err())
	}
	return candidates, nil
}

func queryRetentionStepLogCandidates(
	ctx context.Context,
	policy types.RetentionPolicy,
	now time.Time,
) ([]types.RetentionStepLogCandidate, error) {
	cutoff := now.AddDate(0, 0, -policy.StepLogRetentionDays)
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			lc.task_id,
			count(*),
			min(lc.created_at),
			max(lc.created_at)
		FROM StepRunLogChunk lc
		JOIN Task t
			ON t.id = lc.task_id
			AND t.organization_id = lc.organization_id
		WHERE lc.organization_id = @organizationId
			AND t.task_type = @taskType
			AND lc.created_at <= @cutoff
		GROUP BY lc.task_id
		ORDER BY min(lc.created_at), lc.task_id`,
		pgx.NamedArgs{
			"organizationId": policy.OrganizationID,
			"taskType":       types.TaskTypeDeployment,
			"cutoff":         cutoff,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query retention step log candidates: %w", err)
	}
	defer rows.Close()

	candidates := []types.RetentionStepLogCandidate{}
	for rows.Next() {
		var candidate types.RetentionStepLogCandidate
		var chunkCount int64
		if err := rows.Scan(
			&candidate.TaskID,
			&chunkCount,
			&candidate.OldestAt,
			&candidate.NewestAt,
		); err != nil {
			return nil, fmt.Errorf("could not scan retention step log candidate: %w", err)
		}
		candidate.ChunkCount = int(chunkCount)
		candidate.Reason = "step logs exceeded retention window"
		candidates = append(candidates, candidate)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("could not collect retention step log candidates: %w", rows.Err())
	}
	return candidates, nil
}

func scanRetentionCleanupJob(row pgx.Row) (*types.RetentionCleanupJob, error) {
	var job types.RetentionCleanupJob
	var plan []byte
	if err := row.Scan(
		&job.ID,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.OrganizationID,
		&job.RetentionPolicyID,
		&job.ActorUserAccountID,
		&job.DryRun,
		&job.Status,
		&job.ReleaseCandidateCount,
		&job.FailedTaskCandidateCount,
		&job.StepLogCandidateCount,
		&job.SafetyBlockCount,
		&plan,
		&job.Message,
	); err != nil {
		return nil, err
	}
	if len(plan) > 0 {
		if err := json.Unmarshal(plan, &job.Plan); err != nil {
			return nil, fmt.Errorf("could not decode RetentionCleanupJob plan: %w", err)
		}
	}
	return &job, nil
}

func mapRetentionPolicyWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s RetentionPolicy: %w", action, apierrors.ErrAlreadyExists)
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s RetentionPolicy: %w", action, apierrors.ErrNotFound)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s RetentionPolicy: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s RetentionPolicy: %w", action, err)
}
