package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/distr-sh/distr/internal/actionregistry"
	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	deploymentProcessOutputExpr = `
	dp.id,
	dp.created_at,
	dp.updated_at,
	dp.organization_id,
	dp.application_id,
	dp.name,
	dp.description,
	dp.sort_order
`
	deploymentProcessRevisionOutputExpr = `
	dpr.id,
	dpr.created_at,
	dpr.updated_at,
	dpr.organization_id,
	dpr.deployment_process_id,
	dpr.revision_number,
	dpr.description
`
	deploymentProcessStepOutputExpr = `
	dps.id,
	dps.deployment_process_revision_id,
	dps.key,
	dps.name,
	dps.action_type,
	dps.step_template_version_id,
	dps.execution_location,
	dps.input_bindings,
	dps.condition,
	dps.target_tags,
	dps.failure_mode,
	dps.timeout_seconds,
	dps.retry_max_attempts,
	dps.retry_interval_seconds,
	dps.required_permissions,
	dps.sort_order
`
)

func CreateDeploymentProcess(ctx context.Context, process *types.DeploymentProcess) error {
	if err := normalizeDeploymentProcess(process); err != nil {
		return err
	}
	if err := ensureDeploymentProcessApplication(ctx, process.OrganizationID, process.ApplicationID); err != nil {
		return err
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`INSERT INTO DeploymentProcess AS dp (
			organization_id,
			application_id,
			name,
			description,
			sort_order
		) VALUES (
			@organizationId,
			@applicationId,
			@name,
			@description,
			@sortOrder
		) RETURNING `+deploymentProcessOutputExpr,
		pgx.NamedArgs{
			"organizationId": process.OrganizationID,
			"applicationId":  process.ApplicationID,
			"name":           process.Name,
			"description":    process.Description,
			"sortOrder":      process.SortOrder,
		},
	)
	if err != nil {
		return mapDeploymentProcessWriteError("insert", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentProcess])
	if err != nil {
		return mapDeploymentProcessWriteError("scan created", err)
	}
	*process = result
	return nil
}

func GetDeploymentProcessesByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]types.DeploymentProcess, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+deploymentProcessOutputExpr+`
		FROM DeploymentProcess dp
		WHERE dp.organization_id = @organizationId
		ORDER BY dp.application_id, dp.sort_order, dp.name, dp.id`,
		pgx.NamedArgs{"organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentProcess: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentProcess])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentProcess: %w", err)
	}
	return result, nil
}

func GetDeploymentProcess(ctx context.Context, id, orgID uuid.UUID) (*types.DeploymentProcess, error) {
	return getDeploymentProcess(ctx, id, orgID, false)
}

func getDeploymentProcess(ctx context.Context, id, orgID uuid.UUID, forUpdate bool) (*types.DeploymentProcess, error) {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+deploymentProcessOutputExpr+`
		FROM DeploymentProcess dp
		WHERE dp.id = @id AND dp.organization_id = @organizationId`+lockClause,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentProcess: %w", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentProcess])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentProcess: %w", err)
	}
	return &result, nil
}

func UpdateDeploymentProcess(ctx context.Context, process *types.DeploymentProcess) error {
	if err := normalizeDeploymentProcess(process); err != nil {
		return err
	}
	return RunTx(ctx, func(ctx context.Context) error {
		existing, err := getDeploymentProcess(ctx, process.ID, process.OrganizationID, true)
		if err != nil {
			return err
		}
		if err := EnsureConfigAsCodeDatabaseManagedForUpdate(
			ctx,
			process.OrganizationID,
			types.ConfigAsCodeResourceKindDeploymentProcess,
			process.ID,
		); err != nil {
			return err
		}
		if err := ensureDeploymentProcessApplication(ctx, process.OrganizationID, process.ApplicationID); err != nil {
			return err
		}
		if existing.ApplicationID != process.ApplicationID {
			hasRevisions, err := deploymentProcessHasRevisions(ctx, process.ID)
			if err != nil {
				return err
			}
			if hasRevisions {
				return fmt.Errorf("could not update DeploymentProcess: %w", apierrors.ErrConflict)
			}
		}

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`UPDATE DeploymentProcess AS dp SET
				application_id = @applicationId,
				name = @name,
				description = @description,
				sort_order = @sortOrder,
				updated_at = now()
			WHERE dp.id = @id AND dp.organization_id = @organizationId
			RETURNING `+deploymentProcessOutputExpr,
			pgx.NamedArgs{
				"id":             process.ID,
				"organizationId": process.OrganizationID,
				"applicationId":  process.ApplicationID,
				"name":           process.Name,
				"description":    process.Description,
				"sortOrder":      process.SortOrder,
			},
		)
		if err != nil {
			return mapDeploymentProcessWriteError("update", err)
		}
		result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentProcess])
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		} else if err != nil {
			return mapDeploymentProcessWriteError("scan updated", err)
		}
		*process = result
		return nil
	})
}

func DeleteDeploymentProcessWithID(ctx context.Context, id, organizationID uuid.UUID) error {
	return RunTx(ctx, func(ctx context.Context) error {
		if err := EnsureConfigAsCodeDatabaseManagedForUpdate(
			ctx,
			organizationID,
			types.ConfigAsCodeResourceKindDeploymentProcess,
			id,
		); err != nil {
			return err
		}
		if err := DeleteConfigAsCodeAuthorityForResource(
			ctx,
			organizationID,
			types.ConfigAsCodeResourceKindDeploymentProcess,
			id,
		); err != nil {
			return err
		}
		db := internalctx.GetDb(ctx)
		cmd, err := db.Exec(ctx,
			`DELETE FROM DeploymentProcess WHERE id = @id AND organization_id = @organizationId`,
			pgx.NamedArgs{"id": id, "organizationId": organizationID},
		)
		if err != nil {
			return mapDeploymentProcessWriteError("delete", err)
		}
		if cmd.RowsAffected() == 0 {
			return apierrors.ErrNotFound
		}
		return nil
	})
}

func CreateDeploymentProcessRevision(ctx context.Context, revision *types.DeploymentProcessRevision) error {
	if err := normalizeDeploymentProcessRevision(revision); err != nil {
		return err
	}
	return RunTx(ctx, func(ctx context.Context) error {
		process, err := getDeploymentProcess(ctx, revision.DeploymentProcessID, revision.OrganizationID, true)
		if err != nil {
			return err
		}
		if err := EnsureConfigAsCodeDatabaseManagedForUpdate(
			ctx,
			revision.OrganizationID,
			types.ConfigAsCodeResourceKindDeploymentProcess,
			revision.DeploymentProcessID,
		); err != nil {
			return err
		}
		if err := ensureDeploymentProcessStepReferences(
			ctx,
			process.OrganizationID,
			process.ApplicationID,
			revision.Steps,
		); err != nil {
			return err
		}

		number, err := nextDeploymentProcessRevisionNumber(ctx, process.ID)
		if err != nil {
			return err
		}
		revision.RevisionNumber = number

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`INSERT INTO DeploymentProcessRevision AS dpr (
				organization_id,
				deployment_process_id,
				revision_number,
				description
			) VALUES (
				@organizationId,
				@deploymentProcessId,
				@revisionNumber,
				@description
			) RETURNING `+deploymentProcessRevisionOutputExpr,
			pgx.NamedArgs{
				"organizationId":      revision.OrganizationID,
				"deploymentProcessId": revision.DeploymentProcessID,
				"revisionNumber":      revision.RevisionNumber,
				"description":         revision.Description,
			},
		)
		if err != nil {
			return mapDeploymentProcessWriteError("insert revision", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentProcessRevision])
		if err != nil {
			return mapDeploymentProcessWriteError("scan revision", err)
		}
		if err := insertDeploymentProcessSteps(
			ctx,
			created.ID,
			process.OrganizationID,
			process.ApplicationID,
			revision.Steps,
		); err != nil {
			return err
		}
		loaded, err := getDeploymentProcessRevision(ctx, process.ID, created.ID, process.OrganizationID)
		if err != nil {
			return err
		}
		*revision = *loaded
		return nil
	})
}

func GetDeploymentProcessRevisions(
	ctx context.Context,
	deploymentProcessID uuid.UUID,
	orgID uuid.UUID,
) ([]types.DeploymentProcessRevision, error) {
	if _, err := getDeploymentProcess(ctx, deploymentProcessID, orgID, false); err != nil {
		return nil, err
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+deploymentProcessRevisionOutputExpr+`
		FROM DeploymentProcessRevision dpr
		WHERE dpr.deployment_process_id = @deploymentProcessId AND dpr.organization_id = @organizationId
		ORDER BY dpr.revision_number, dpr.id`,
		pgx.NamedArgs{"deploymentProcessId": deploymentProcessID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentProcessRevision: %w", err)
	}
	revisions, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentProcessRevision])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentProcessRevision: %w", err)
	}
	for i := range revisions {
		steps, err := getDeploymentProcessSteps(ctx, revisions[i].ID)
		if err != nil {
			return nil, err
		}
		revisions[i].Steps = steps
	}
	return revisions, nil
}

func GetDeploymentProcessRevision(
	ctx context.Context,
	deploymentProcessID uuid.UUID,
	revisionID uuid.UUID,
	orgID uuid.UUID,
) (*types.DeploymentProcessRevision, error) {
	return getDeploymentProcessRevision(ctx, deploymentProcessID, revisionID, orgID)
}

func getDeploymentProcessRevision(
	ctx context.Context,
	deploymentProcessID uuid.UUID,
	revisionID uuid.UUID,
	orgID uuid.UUID,
) (*types.DeploymentProcessRevision, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+deploymentProcessRevisionOutputExpr+`
		FROM DeploymentProcessRevision dpr
		WHERE dpr.id = @id
			AND dpr.deployment_process_id = @deploymentProcessId
			AND dpr.organization_id = @organizationId`,
		pgx.NamedArgs{
			"id":                  revisionID,
			"deploymentProcessId": deploymentProcessID,
			"organizationId":      orgID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentProcessRevision: %w", err)
	}
	revision, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentProcessRevision])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentProcessRevision: %w", err)
	}
	revision.Steps, err = getDeploymentProcessSteps(ctx, revision.ID)
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

func normalizeDeploymentProcess(process *types.DeploymentProcess) error {
	process.Name = strings.TrimSpace(process.Name)
	if process.Name == "" {
		return apierrors.NewBadRequest("name is required")
	}
	if process.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if process.ApplicationID == uuid.Nil {
		return apierrors.NewBadRequest("applicationId is required")
	}
	if process.SortOrder < 0 {
		return apierrors.NewBadRequest("sortOrder must be non-negative")
	}
	return nil
}

func normalizeDeploymentProcessRevision(revision *types.DeploymentProcessRevision) error {
	revision.Description = strings.TrimSpace(revision.Description)
	if revision.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if revision.DeploymentProcessID == uuid.Nil {
		return apierrors.NewBadRequest("deploymentProcessId is required")
	}
	if len(revision.Steps) == 0 {
		return apierrors.NewBadRequest("at least one step is required")
	}

	keys := map[string]struct{}{}
	sortOrders := map[int]struct{}{}
	for i := range revision.Steps {
		step := &revision.Steps[i]
		step.Key = strings.TrimSpace(step.Key)
		step.Name = strings.TrimSpace(step.Name)
		step.ActionType = strings.TrimSpace(step.ActionType)
		step.ExecutionLocation = strings.TrimSpace(step.ExecutionLocation)
		step.Condition = strings.TrimSpace(step.Condition)
		step.FailureMode = strings.TrimSpace(step.FailureMode)
		if step.Key == "" {
			return apierrors.NewBadRequest("step key is required")
		}
		if step.Name == "" {
			return apierrors.NewBadRequest("step name is required")
		}
		if step.ActionType == "" {
			return apierrors.NewBadRequest("step actionType is required")
		}
		if step.ExecutionLocation == "" {
			return apierrors.NewBadRequest("step executionLocation is required")
		}
		if step.SortOrder < 0 {
			return apierrors.NewBadRequest("step sortOrder must be non-negative")
		}
		if step.TimeoutSeconds < 0 {
			return apierrors.NewBadRequest("step timeoutSeconds must be non-negative")
		}
		if step.RetryMaxAttempts < 0 {
			return apierrors.NewBadRequest("step retry_max_attempts must be non-negative")
		}
		if step.RetryIntervalSeconds < 0 {
			return apierrors.NewBadRequest("step retry_interval_seconds must be non-negative")
		}
		if step.FailureMode == "" {
			step.FailureMode = "fail"
		}
		if step.InputBindings == nil {
			step.InputBindings = map[string]any{}
		}
		if err := normalizeDeploymentProcessStringList(&step.TargetTags, "step targetTags"); err != nil {
			return err
		}
		if err := normalizeDeploymentProcessStringList(&step.RequiredPermissions, "step requiredPermissions"); err != nil {
			return err
		}
		if err := normalizeDeploymentProcessUUIDList(step.ChannelIDs, "step channelIds"); err != nil {
			return err
		}
		if err := normalizeDeploymentProcessUUIDList(step.EnvironmentIDs, "step environmentIds"); err != nil {
			return err
		}
		for j := range step.Dependencies {
			step.Dependencies[j] = strings.TrimSpace(step.Dependencies[j])
		}

		if _, ok := keys[step.Key]; ok {
			return apierrors.NewBadRequest("step keys must be unique")
		}
		keys[step.Key] = struct{}{}
		if _, ok := sortOrders[step.SortOrder]; ok {
			return apierrors.NewBadRequest("step sortOrder values must be unique")
		}
		sortOrders[step.SortOrder] = struct{}{}
	}
	if err := actionregistry.DefaultRegistry().ValidateSteps(revision.Steps); err != nil {
		return err
	}
	return validateDeploymentProcessStepGraph(revision.Steps, keys)
}

func validateDeploymentProcessStepGraph(steps []types.DeploymentProcessStep, keys map[string]struct{}) error {
	graph := make(map[string][]string, len(steps))
	for _, step := range steps {
		seen := map[string]struct{}{}
		for _, dependency := range step.Dependencies {
			if dependency == "" {
				return apierrors.NewBadRequest("step dependency is required")
			}
			if dependency == step.Key {
				return apierrors.NewBadRequest("step cannot depend on itself")
			}
			if _, ok := seen[dependency]; ok {
				return apierrors.NewBadRequest("step dependencies must be unique")
			}
			seen[dependency] = struct{}{}
			if _, ok := keys[dependency]; !ok {
				return apierrors.NewBadRequest(fmt.Sprintf("step dependency %q does not exist", dependency))
			}
			graph[step.Key] = append(graph[step.Key], dependency)
		}
		if _, ok := graph[step.Key]; !ok {
			graph[step.Key] = nil
		}
	}

	visiting := map[string]struct{}{}
	visited := map[string]struct{}{}
	var visit func(string) bool
	visit = func(key string) bool {
		if _, ok := visited[key]; ok {
			return false
		}
		if _, ok := visiting[key]; ok {
			return true
		}
		visiting[key] = struct{}{}
		for _, dependency := range graph[key] {
			if visit(dependency) {
				return true
			}
		}
		delete(visiting, key)
		visited[key] = struct{}{}
		return false
	}
	for key := range graph {
		if visit(key) {
			return apierrors.NewBadRequest("step dependencies must not contain cycles")
		}
	}
	return nil
}

func normalizeDeploymentProcessStringList(values *[]string, field string) error {
	if len(*values) == 0 {
		*values = []string{}
		return nil
	}
	for i, value := range *values {
		value = strings.TrimSpace(value)
		if value == "" {
			return apierrors.NewBadRequest(field + " must not contain empty values")
		}
		(*values)[i] = value
	}
	return nil
}

func normalizeDeploymentProcessUUIDList(values []uuid.UUID, field string) error {
	seen := map[uuid.UUID]struct{}{}
	for _, value := range values {
		if value == uuid.Nil {
			return apierrors.NewBadRequest(field + " must not contain empty IDs")
		}
		if _, ok := seen[value]; ok {
			return apierrors.NewBadRequest(field + " must be unique")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func ensureDeploymentProcessApplication(ctx context.Context, orgID, applicationID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM Application
			WHERE id = @applicationId AND organization_id = @organizationId
		)`,
		pgx.NamedArgs{"organizationId": orgID, "applicationId": applicationID},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate DeploymentProcess application reference: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func ensureDeploymentProcessStepReferences(
	ctx context.Context,
	orgID uuid.UUID,
	applicationID uuid.UUID,
	steps []types.DeploymentProcessStep,
) error {
	for _, step := range steps {
		for _, channelID := range step.ChannelIDs {
			if err := ensureDeploymentProcessStepChannel(ctx, orgID, applicationID, channelID); err != nil {
				return err
			}
		}
		for _, environmentID := range step.EnvironmentIDs {
			if err := ensureDeploymentProcessStepEnvironment(ctx, orgID, environmentID); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureDeploymentProcessStepChannel(ctx context.Context, orgID, applicationID, channelID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM Channel
			WHERE id = @channelId
				AND organization_id = @organizationId
				AND application_id = @applicationId
		)`,
		pgx.NamedArgs{
			"organizationId": orgID,
			"applicationId":  applicationID,
			"channelId":      channelID,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate DeploymentProcess step channel reference: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func ensureDeploymentProcessStepEnvironment(ctx context.Context, orgID, environmentID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM Environment
			WHERE id = @environmentId AND organization_id = @organizationId
		)`,
		pgx.NamedArgs{"organizationId": orgID, "environmentId": environmentID},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate DeploymentProcess step environment reference: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func nextDeploymentProcessRevisionNumber(ctx context.Context, deploymentProcessID uuid.UUID) (int, error) {
	db := internalctx.GetDb(ctx)
	var number int
	err := db.QueryRow(ctx,
		`SELECT COALESCE(MAX(revision_number), 0) + 1
		FROM DeploymentProcessRevision
		WHERE deployment_process_id = @deploymentProcessId`,
		pgx.NamedArgs{"deploymentProcessId": deploymentProcessID},
	).Scan(&number)
	if err != nil {
		return 0, fmt.Errorf("could not calculate DeploymentProcess revision number: %w", err)
	}
	return number, nil
}

func deploymentProcessHasRevisions(ctx context.Context, deploymentProcessID uuid.UUID) (bool, error) {
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM DeploymentProcessRevision
			WHERE deployment_process_id = @deploymentProcessId
		)`,
		pgx.NamedArgs{"deploymentProcessId": deploymentProcessID},
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("could not query DeploymentProcess revisions: %w", err)
	}
	return exists, nil
}

func insertDeploymentProcessSteps(
	ctx context.Context,
	deploymentProcessRevisionID uuid.UUID,
	orgID uuid.UUID,
	applicationID uuid.UUID,
	steps []types.DeploymentProcessStep,
) error {
	rows := make([]types.DeploymentProcessStep, len(steps))
	for i, step := range steps {
		step.ID = uuid.New()
		step.DeploymentProcessRevisionID = deploymentProcessRevisionID
		rows[i] = step
	}

	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"deploymentprocessstep"},
		[]string{
			"id",
			"deployment_process_revision_id",
			"key",
			"name",
			"action_type",
			"step_template_version_id",
			"execution_location",
			"input_bindings",
			"condition",
			"target_tags",
			"failure_mode",
			"timeout_seconds",
			"retry_max_attempts",
			"retry_interval_seconds",
			"required_permissions",
			"sort_order",
		},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			step := rows[i]
			return []any{
				step.ID,
				step.DeploymentProcessRevisionID,
				step.Key,
				step.Name,
				step.ActionType,
				step.StepTemplateVersionID,
				step.ExecutionLocation,
				step.InputBindings,
				step.Condition,
				stringSliceOrEmpty(step.TargetTags),
				step.FailureMode,
				step.TimeoutSeconds,
				step.RetryMaxAttempts,
				step.RetryIntervalSeconds,
				stringSliceOrEmpty(step.RequiredPermissions),
				step.SortOrder,
			}, nil
		}),
	)
	if err != nil {
		return mapDeploymentProcessWriteError("insert steps", err)
	}
	if err := insertDeploymentProcessStepDependencies(ctx, deploymentProcessRevisionID, rows); err != nil {
		return err
	}
	if err := insertDeploymentProcessStepChannels(ctx, orgID, applicationID, rows); err != nil {
		return err
	}
	if err := insertDeploymentProcessStepEnvironments(ctx, rows); err != nil {
		return err
	}
	return nil
}

func insertDeploymentProcessStepDependencies(
	ctx context.Context,
	deploymentProcessRevisionID uuid.UUID,
	steps []types.DeploymentProcessStep,
) error {
	count := 0
	for _, step := range steps {
		count += len(step.Dependencies)
	}
	if count == 0 {
		return nil
	}

	type row struct {
		StepKey      string
		DependsOnKey string
		SortOrder    int
	}
	rows := make([]row, 0, count)
	for _, step := range steps {
		for i, dependency := range step.Dependencies {
			rows = append(rows, row{
				StepKey:      step.Key,
				DependsOnKey: dependency,
				SortOrder:    i,
			})
		}
	}

	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"deploymentprocessstepdependency"},
		[]string{"deployment_process_revision_id", "step_key", "depends_on_step_key", "sort_order"},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			row := rows[i]
			return []any{deploymentProcessRevisionID, row.StepKey, row.DependsOnKey, row.SortOrder}, nil
		}),
	)
	if err != nil {
		return mapDeploymentProcessWriteError("insert step dependencies", err)
	}
	return nil
}

func insertDeploymentProcessStepChannels(
	ctx context.Context,
	orgID uuid.UUID,
	applicationID uuid.UUID,
	steps []types.DeploymentProcessStep,
) error {
	count := 0
	for _, step := range steps {
		count += len(step.ChannelIDs)
	}
	if count == 0 {
		return nil
	}

	type row struct {
		StepID    uuid.UUID
		ChannelID uuid.UUID
		SortOrder int
	}
	rows := make([]row, 0, count)
	for _, step := range steps {
		for i, channelID := range step.ChannelIDs {
			rows = append(rows, row{
				StepID:    step.ID,
				ChannelID: channelID,
				SortOrder: i,
			})
		}
	}

	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"deploymentprocessstepchannel"},
		[]string{
			"deployment_process_step_id",
			"organization_id",
			"application_id",
			"channel_id",
			"sort_order",
		},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			row := rows[i]
			return []any{row.StepID, orgID, applicationID, row.ChannelID, row.SortOrder}, nil
		}),
	)
	if err != nil {
		return mapDeploymentProcessStepReferenceWriteError("insert step channels", err)
	}
	return nil
}

func insertDeploymentProcessStepEnvironments(ctx context.Context, steps []types.DeploymentProcessStep) error {
	rows := deploymentProcessStepUUIDRelations(steps, func(step types.DeploymentProcessStep) []uuid.UUID {
		return step.EnvironmentIDs
	})
	return insertDeploymentProcessStepUUIDRelations(
		ctx,
		"deploymentprocessstepenvironment",
		"environment_id",
		"insert step environments",
		rows,
	)
}

type deploymentProcessStepUUIDRelation struct {
	StepID    uuid.UUID
	RelatedID uuid.UUID
	SortOrder int
}

func deploymentProcessStepUUIDRelations(
	steps []types.DeploymentProcessStep,
	ids func(types.DeploymentProcessStep) []uuid.UUID,
) []deploymentProcessStepUUIDRelation {
	count := 0
	for _, step := range steps {
		count += len(ids(step))
	}
	rows := make([]deploymentProcessStepUUIDRelation, 0, count)
	for _, step := range steps {
		for i, id := range ids(step) {
			rows = append(rows, deploymentProcessStepUUIDRelation{
				StepID:    step.ID,
				RelatedID: id,
				SortOrder: i,
			})
		}
	}
	return rows
}

func insertDeploymentProcessStepUUIDRelations(
	ctx context.Context,
	tableName string,
	relatedColumnName string,
	action string,
	rows []deploymentProcessStepUUIDRelation,
) error {
	if len(rows) == 0 {
		return nil
	}

	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{tableName},
		[]string{"deployment_process_step_id", relatedColumnName, "sort_order"},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			row := rows[i]
			return []any{row.StepID, row.RelatedID, row.SortOrder}, nil
		}),
	)
	if err != nil {
		return mapDeploymentProcessWriteError(action, err)
	}
	return nil
}

func getDeploymentProcessSteps(
	ctx context.Context,
	deploymentProcessRevisionID uuid.UUID,
) ([]types.DeploymentProcessStep, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+deploymentProcessStepOutputExpr+`
		FROM DeploymentProcessStep dps
		WHERE dps.deployment_process_revision_id = @deploymentProcessRevisionId
		ORDER BY dps.sort_order, dps.key, dps.id`,
		pgx.NamedArgs{"deploymentProcessRevisionId": deploymentProcessRevisionID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentProcessStep: %w", err)
	}
	steps, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentProcessStep])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentProcessStep: %w", err)
	}
	for i := range steps {
		steps[i].Dependencies, err = getDeploymentProcessStepDependencies(
			ctx,
			steps[i].DeploymentProcessRevisionID,
			steps[i].Key,
		)
		if err != nil {
			return nil, err
		}
		steps[i].ChannelIDs, err = getDeploymentProcessStepChannelIDs(ctx, steps[i].ID)
		if err != nil {
			return nil, err
		}
		steps[i].EnvironmentIDs, err = getDeploymentProcessStepEnvironmentIDs(ctx, steps[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return steps, nil
}

func getDeploymentProcessStepDependencies(
	ctx context.Context,
	deploymentProcessRevisionID uuid.UUID,
	stepKey string,
) ([]string, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT depends_on_step_key
		FROM DeploymentProcessStepDependency
		WHERE deployment_process_revision_id = @deploymentProcessRevisionId
			AND step_key = @stepKey
		ORDER BY sort_order, depends_on_step_key`,
		pgx.NamedArgs{"deploymentProcessRevisionId": deploymentProcessRevisionID, "stepKey": stepKey},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentProcessStepDependency: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentProcessStepDependency: %w", err)
	}
	return result, nil
}

func getDeploymentProcessStepChannelIDs(ctx context.Context, stepID uuid.UUID) ([]uuid.UUID, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT channel_id
		FROM DeploymentProcessStepChannel
		WHERE deployment_process_step_id = @deploymentProcessStepId
		ORDER BY sort_order, channel_id`,
		pgx.NamedArgs{"deploymentProcessStepId": stepID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentProcessStepChannel: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentProcessStepChannel: %w", err)
	}
	return result, nil
}

func getDeploymentProcessStepEnvironmentIDs(ctx context.Context, stepID uuid.UUID) ([]uuid.UUID, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT environment_id
		FROM DeploymentProcessStepEnvironment
		WHERE deployment_process_step_id = @deploymentProcessStepId
		ORDER BY sort_order, environment_id`,
		pgx.NamedArgs{"deploymentProcessStepId": stepID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentProcessStepEnvironment: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentProcessStepEnvironment: %w", err)
	}
	return result, nil
}

func mapDeploymentProcessWriteError(action string, err error) error {
	if isProtectedReferenceViolation(err) {
		return fmt.Errorf("could not %s DeploymentProcess: %w", action, apierrors.ErrConflict)
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s DeploymentProcess: %w", action, apierrors.ErrAlreadyExists)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s DeploymentProcess: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s DeploymentProcess: %w", action, err)
}

func mapDeploymentProcessStepReferenceWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == pgerrcode.ForeignKeyViolation {
		return fmt.Errorf("could not %s DeploymentProcess: %w", action, apierrors.ErrNotFound)
	}
	return mapDeploymentProcessWriteError(action, err)
}
