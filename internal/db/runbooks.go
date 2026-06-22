package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/distr-sh/distr/internal/actionregistry"
	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/conditions"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/runbooks"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	runbookOutputExpr = `
	rb.id,
	rb.created_at,
	rb.updated_at,
	rb.organization_id,
	rb.application_id,
	rb.name,
	rb.description,
	rb.sort_order
`
	runbookRevisionOutputExpr = `
	rbr.id,
	rbr.created_at,
	rbr.updated_at,
	rbr.organization_id,
	rbr.runbook_id,
	rbr.revision_number,
	rbr.description
`
	runbookStepOutputExpr = `
	rbs.id,
	rbs.runbook_revision_id,
	rbs.key,
	rbs.name,
	rbs.action_type,
	rbs.step_template_version_id,
	rbs.execution_location,
	rbs.input_bindings,
	rbs.condition,
	rbs.failure_mode,
	rbs.timeout_seconds,
	rbs.retry_max_attempts,
	rbs.retry_interval_seconds,
	rbs.required_permissions,
	rbs.sort_order
`
	runbookSnapshotOutputExpr = `
	rbsn.id,
	rbsn.created_at,
	rbsn.published_at,
	rbsn.published_by_useraccount_id,
	rbsn.organization_id,
	rbsn.application_id,
	rbsn.runbook_id,
	rbsn.runbook_revision_id,
	rbsn.revision_number,
	rbsn.canonical_checksum,
	rbsn.canonical_payload
`
)

func CreateRunbook(ctx context.Context, runbook *types.Runbook) error {
	if err := normalizeRunbook(runbook); err != nil {
		return err
	}
	if err := ensureRunbookApplication(ctx, runbook.OrganizationID, runbook.ApplicationID); err != nil {
		return err
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`INSERT INTO Runbook AS rb (
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
		) RETURNING `+runbookOutputExpr,
		pgx.NamedArgs{
			"organizationId": runbook.OrganizationID,
			"applicationId":  runbook.ApplicationID,
			"name":           runbook.Name,
			"description":    runbook.Description,
			"sortOrder":      runbook.SortOrder,
		},
	)
	if err != nil {
		return mapRunbookWriteError("insert", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.Runbook])
	if err != nil {
		return mapRunbookWriteError("scan created", err)
	}
	*runbook = result
	return nil
}

func GetRunbooksByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]types.Runbook, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+runbookOutputExpr+`
		FROM Runbook rb
		WHERE rb.organization_id = @organizationId
		ORDER BY rb.application_id, rb.sort_order, rb.name, rb.id`,
		pgx.NamedArgs{"organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Runbook: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.Runbook])
	if err != nil {
		return nil, fmt.Errorf("could not collect Runbook: %w", err)
	}
	return result, nil
}

func GetRunbook(ctx context.Context, id, orgID uuid.UUID) (*types.Runbook, error) {
	return getRunbook(ctx, id, orgID, false)
}

func getRunbook(ctx context.Context, id, orgID uuid.UUID, forUpdate bool) (*types.Runbook, error) {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+runbookOutputExpr+`
		FROM Runbook rb
		WHERE rb.id = @id AND rb.organization_id = @organizationId`+lockClause,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Runbook: %w", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.Runbook])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect Runbook: %w", err)
	}
	return &result, nil
}

func UpdateRunbook(ctx context.Context, runbook *types.Runbook) error {
	if err := normalizeRunbook(runbook); err != nil {
		return err
	}
	return RunTx(ctx, func(ctx context.Context) error {
		existing, err := getRunbook(ctx, runbook.ID, runbook.OrganizationID, true)
		if err != nil {
			return err
		}
		if err := ensureRunbookApplication(ctx, runbook.OrganizationID, runbook.ApplicationID); err != nil {
			return err
		}
		if existing.ApplicationID != runbook.ApplicationID {
			hasRevisions, err := runbookHasRevisions(ctx, runbook.ID)
			if err != nil {
				return err
			}
			if hasRevisions {
				return fmt.Errorf("could not update Runbook: %w", apierrors.ErrConflict)
			}
		}

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`UPDATE Runbook AS rb SET
				application_id = @applicationId,
				name = @name,
				description = @description,
				sort_order = @sortOrder,
				updated_at = now()
			WHERE rb.id = @id AND rb.organization_id = @organizationId
			RETURNING `+runbookOutputExpr,
			pgx.NamedArgs{
				"id":             runbook.ID,
				"organizationId": runbook.OrganizationID,
				"applicationId":  runbook.ApplicationID,
				"name":           runbook.Name,
				"description":    runbook.Description,
				"sortOrder":      runbook.SortOrder,
			},
		)
		if err != nil {
			return mapRunbookWriteError("update", err)
		}
		result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.Runbook])
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		} else if err != nil {
			return mapRunbookWriteError("scan updated", err)
		}
		*runbook = result
		return nil
	})
}

func DeleteRunbookWithID(ctx context.Context, id, organizationID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	cmd, err := db.Exec(ctx,
		`DELETE FROM Runbook WHERE id = @id AND organization_id = @organizationId`,
		pgx.NamedArgs{"id": id, "organizationId": organizationID},
	)
	if err != nil {
		return mapRunbookWriteError("delete", err)
	}
	if cmd.RowsAffected() == 0 {
		return apierrors.ErrNotFound
	}
	return nil
}

func CreateRunbookRevision(ctx context.Context, revision *types.RunbookRevision) error {
	if err := normalizeRunbookRevision(revision); err != nil {
		return err
	}
	return RunTx(ctx, func(ctx context.Context) error {
		runbook, err := getRunbook(ctx, revision.RunbookID, revision.OrganizationID, true)
		if err != nil {
			return err
		}
		number, err := nextRunbookRevisionNumber(ctx, runbook.ID)
		if err != nil {
			return err
		}
		revision.RevisionNumber = number

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`INSERT INTO RunbookRevision AS rbr (
				organization_id,
				runbook_id,
				revision_number,
				description
			) VALUES (
				@organizationId,
				@runbookId,
				@revisionNumber,
				@description
			) RETURNING `+runbookRevisionOutputExpr,
			pgx.NamedArgs{
				"organizationId": revision.OrganizationID,
				"runbookId":      revision.RunbookID,
				"revisionNumber": revision.RevisionNumber,
				"description":    revision.Description,
			},
		)
		if err != nil {
			return mapRunbookWriteError("insert revision", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.RunbookRevision])
		if err != nil {
			return mapRunbookWriteError("scan revision", err)
		}
		if err := insertRunbookSteps(ctx, created.ID, revision.Steps); err != nil {
			return err
		}
		loaded, err := getRunbookRevision(ctx, runbook.ID, created.ID, runbook.OrganizationID)
		if err != nil {
			return err
		}
		*revision = *loaded
		return nil
	})
}

func GetRunbookRevisions(ctx context.Context, runbookID uuid.UUID, orgID uuid.UUID) ([]types.RunbookRevision, error) {
	if _, err := getRunbook(ctx, runbookID, orgID, false); err != nil {
		return nil, err
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+runbookRevisionOutputExpr+`
		FROM RunbookRevision rbr
		WHERE rbr.runbook_id = @runbookId AND rbr.organization_id = @organizationId
		ORDER BY rbr.revision_number, rbr.id`,
		pgx.NamedArgs{"runbookId": runbookID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query RunbookRevision: %w", err)
	}
	revisions, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.RunbookRevision])
	if err != nil {
		return nil, fmt.Errorf("could not collect RunbookRevision: %w", err)
	}
	for i := range revisions {
		steps, err := getRunbookSteps(ctx, revisions[i].ID)
		if err != nil {
			return nil, err
		}
		revisions[i].Steps = steps
	}
	return revisions, nil
}

func GetRunbookRevision(ctx context.Context, runbookID, revisionID, orgID uuid.UUID) (*types.RunbookRevision, error) {
	return getRunbookRevision(ctx, runbookID, revisionID, orgID)
}

func PublishRunbookRevision(
	ctx context.Context,
	runbookID uuid.UUID,
	revisionID uuid.UUID,
	organizationID uuid.UUID,
	actorUserAccountID uuid.UUID,
) (*types.RunbookSnapshot, error) {
	var snapshot *types.RunbookSnapshot
	err := RunTx(ctx, func(ctx context.Context) error {
		runbook, revision, err := getRunbookRevisionForSnapshot(ctx, organizationID, runbookID, revisionID)
		if err != nil {
			return err
		}
		payload, checksum, err := runbooks.Canonicalize(*runbook, *revision)
		if err != nil {
			return fmt.Errorf("could not canonicalize RunbookSnapshot: %w", err)
		}

		var publishedBy *uuid.UUID
		if actorUserAccountID != uuid.Nil {
			publishedBy = &actorUserAccountID
		}
		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`INSERT INTO RunbookSnapshot AS rbsn (
				published_by_useraccount_id,
				organization_id,
				application_id,
				runbook_id,
				runbook_revision_id,
				revision_number,
				canonical_checksum,
				canonical_payload
			) VALUES (
				@publishedByUserAccountId,
				@organizationId,
				@applicationId,
				@runbookId,
				@runbookRevisionId,
				@revisionNumber,
				@canonicalChecksum,
				@canonicalPayload
			)
			ON CONFLICT (runbook_revision_id) DO NOTHING
			RETURNING `+runbookSnapshotOutputExpr,
			pgx.NamedArgs{
				"publishedByUserAccountId": publishedBy,
				"organizationId":           organizationID,
				"applicationId":            runbook.ApplicationID,
				"runbookId":                runbook.ID,
				"runbookRevisionId":        revision.ID,
				"revisionNumber":           revision.RevisionNumber,
				"canonicalChecksum":        checksum,
				"canonicalPayload":         payload,
			},
		)
		if err != nil {
			return mapRunbookWriteError("insert snapshot", err)
		}
		created, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.RunbookSnapshot])
		if errors.Is(err, pgx.ErrNoRows) {
			snapshot, err = getRunbookSnapshotByRevision(ctx, revision.ID, organizationID)
			return err
		} else if err != nil {
			return mapRunbookWriteError("scan snapshot", err)
		}
		created.Revision = *revision
		snapshot = &created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func getRunbookRevision(ctx context.Context, runbookID, revisionID, orgID uuid.UUID) (*types.RunbookRevision, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+runbookRevisionOutputExpr+`
		FROM RunbookRevision rbr
		WHERE rbr.id = @id
			AND rbr.runbook_id = @runbookId
			AND rbr.organization_id = @organizationId`,
		pgx.NamedArgs{"id": revisionID, "runbookId": runbookID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query RunbookRevision: %w", err)
	}
	revision, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.RunbookRevision])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect RunbookRevision: %w", err)
	}
	revision.Steps, err = getRunbookSteps(ctx, revision.ID)
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

func getRunbookRevisionForSnapshot(
	ctx context.Context,
	organizationID uuid.UUID,
	runbookID uuid.UUID,
	revisionID uuid.UUID,
) (*types.Runbook, *types.RunbookRevision, error) {
	db := internalctx.GetDb(ctx)
	var foundRevisionID uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT rbr.id
		FROM RunbookRevision rbr
		JOIN Runbook rb ON rb.id = rbr.runbook_id
			AND rb.organization_id = rbr.organization_id
		WHERE rbr.id = @revisionId
			AND rbr.runbook_id = @runbookId
			AND rbr.organization_id = @organizationId
		FOR KEY SHARE OF rbr, rb`,
		pgx.NamedArgs{
			"revisionId":     revisionID,
			"runbookId":      runbookID,
			"organizationId": organizationID,
		},
	).Scan(&foundRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, nil, fmt.Errorf("could not query Runbook revision for RunbookSnapshot: %w", err)
	}

	runbook, err := getRunbook(ctx, runbookID, organizationID, false)
	if err != nil {
		return nil, nil, err
	}
	revision, err := getRunbookRevision(ctx, runbook.ID, foundRevisionID, organizationID)
	if err != nil {
		return nil, nil, err
	}
	return runbook, revision, nil
}

func getRunbookSnapshotByRevision(
	ctx context.Context,
	revisionID uuid.UUID,
	organizationID uuid.UUID,
) (*types.RunbookSnapshot, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+runbookSnapshotOutputExpr+`
		FROM RunbookSnapshot rbsn
		WHERE rbsn.runbook_revision_id = @revisionId
			AND rbsn.organization_id = @organizationId`,
		pgx.NamedArgs{"revisionId": revisionID, "organizationId": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query RunbookSnapshot by revision: %w", err)
	}
	snapshot, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.RunbookSnapshot])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect RunbookSnapshot: %w", err)
	}
	if err := hydrateRunbookSnapshotRevision(&snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func hydrateRunbookSnapshotRevision(snapshot *types.RunbookSnapshot) error {
	revision, err := runbooks.DecodeRevision(snapshot.CanonicalPayload)
	if err != nil {
		return fmt.Errorf("could not decode RunbookSnapshot payload: %w", err)
	}
	revision.OrganizationID = snapshot.OrganizationID
	revision.ID = snapshot.RunbookRevisionID
	revision.RunbookID = snapshot.RunbookID
	revision.RevisionNumber = snapshot.RevisionNumber
	for i := range revision.Steps {
		revision.Steps[i].RunbookRevisionID = snapshot.RunbookRevisionID
	}
	snapshot.Revision = revision
	return nil
}

func normalizeRunbook(runbook *types.Runbook) error {
	runbook.Name = strings.TrimSpace(runbook.Name)
	runbook.Description = strings.TrimSpace(runbook.Description)
	if runbook.Name == "" {
		return apierrors.NewBadRequest("name is required")
	}
	if runbook.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if runbook.ApplicationID == uuid.Nil {
		return apierrors.NewBadRequest("applicationId is required")
	}
	if runbook.SortOrder < 0 {
		return apierrors.NewBadRequest("sortOrder must be non-negative")
	}
	return nil
}

func normalizeRunbookRevision(revision *types.RunbookRevision) error {
	revision.Description = strings.TrimSpace(revision.Description)
	if revision.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if revision.RunbookID == uuid.Nil {
		return apierrors.NewBadRequest("runbookId is required")
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
		if err := normalizeRunbookStringList(&step.RequiredPermissions, "step requiredPermissions"); err != nil {
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
		if err := conditions.Validate(step.Condition); err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("step %q condition is invalid: %s", step.Key, err.Error()))
		}
	}
	if err := actionregistry.DefaultRegistry().ValidateRunbookSteps(revision.Steps); err != nil {
		return err
	}
	return validateRunbookStepGraph(revision.Steps, keys)
}

func validateRunbookStepGraph(steps []types.RunbookStep, keys map[string]struct{}) error {
	graph := make(map[string][]string, len(steps))
	for _, step := range steps {
		dependencies := make([]string, 0, len(step.Dependencies))
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
			dependencies = append(dependencies, dependency)
		}
		refs, err := conditions.OutputReferences(step.Condition)
		if err != nil {
			return apierrors.NewBadRequest(fmt.Sprintf("step %q condition is invalid: %s", step.Key, err.Error()))
		}
		for _, ref := range refs {
			if _, ok := keys[ref.StepKey]; !ok {
				return apierrors.NewBadRequest(fmt.Sprintf("step condition output reference %q does not exist", ref.StepKey))
			}
			if _, ok := seen[ref.StepKey]; ok {
				continue
			}
			seen[ref.StepKey] = struct{}{}
			dependencies = append(dependencies, ref.StepKey)
		}
		graph[step.Key] = dependencies
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

func normalizeRunbookStringList(values *[]string, field string) error {
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

func ensureRunbookApplication(ctx context.Context, orgID, applicationID uuid.UUID) error {
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
		return fmt.Errorf("could not validate Runbook application reference: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func nextRunbookRevisionNumber(ctx context.Context, runbookID uuid.UUID) (int, error) {
	db := internalctx.GetDb(ctx)
	var number int
	err := db.QueryRow(ctx,
		`SELECT COALESCE(MAX(revision_number), 0) + 1
		FROM RunbookRevision
		WHERE runbook_id = @runbookId`,
		pgx.NamedArgs{"runbookId": runbookID},
	).Scan(&number)
	if err != nil {
		return 0, fmt.Errorf("could not calculate Runbook revision number: %w", err)
	}
	return number, nil
}

func runbookHasRevisions(ctx context.Context, runbookID uuid.UUID) (bool, error) {
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM RunbookRevision
			WHERE runbook_id = @runbookId
		)`,
		pgx.NamedArgs{"runbookId": runbookID},
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("could not query Runbook revisions: %w", err)
	}
	return exists, nil
}

func insertRunbookSteps(ctx context.Context, runbookRevisionID uuid.UUID, steps []types.RunbookStep) error {
	rows := make([]types.RunbookStep, len(steps))
	for i, step := range steps {
		step.ID = uuid.New()
		step.RunbookRevisionID = runbookRevisionID
		rows[i] = step
	}

	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"runbookstep"},
		[]string{
			"id",
			"runbook_revision_id",
			"key",
			"name",
			"action_type",
			"step_template_version_id",
			"execution_location",
			"input_bindings",
			"condition",
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
				step.RunbookRevisionID,
				step.Key,
				step.Name,
				step.ActionType,
				step.StepTemplateVersionID,
				step.ExecutionLocation,
				step.InputBindings,
				step.Condition,
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
		return mapRunbookWriteError("insert steps", err)
	}
	return insertRunbookStepDependencies(ctx, runbookRevisionID, rows)
}

func insertRunbookStepDependencies(
	ctx context.Context,
	runbookRevisionID uuid.UUID,
	steps []types.RunbookStep,
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
			rows = append(rows, row{StepKey: step.Key, DependsOnKey: dependency, SortOrder: i})
		}
	}

	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"runbookstepdependency"},
		[]string{"runbook_revision_id", "step_key", "depends_on_step_key", "sort_order"},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			row := rows[i]
			return []any{runbookRevisionID, row.StepKey, row.DependsOnKey, row.SortOrder}, nil
		}),
	)
	if err != nil {
		return mapRunbookWriteError("insert step dependencies", err)
	}
	return nil
}

func getRunbookSteps(ctx context.Context, runbookRevisionID uuid.UUID) ([]types.RunbookStep, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+runbookStepOutputExpr+`
		FROM RunbookStep rbs
		WHERE rbs.runbook_revision_id = @runbookRevisionId
		ORDER BY rbs.sort_order, rbs.key, rbs.id`,
		pgx.NamedArgs{"runbookRevisionId": runbookRevisionID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query RunbookStep: %w", err)
	}
	steps, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.RunbookStep])
	if err != nil {
		return nil, fmt.Errorf("could not collect RunbookStep: %w", err)
	}
	for i := range steps {
		steps[i].Dependencies, err = getRunbookStepDependencies(ctx, steps[i].RunbookRevisionID, steps[i].Key)
		if err != nil {
			return nil, err
		}
	}
	return steps, nil
}

func getRunbookStepDependencies(ctx context.Context, runbookRevisionID uuid.UUID, stepKey string) ([]string, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT depends_on_step_key
		FROM RunbookStepDependency
		WHERE runbook_revision_id = @runbookRevisionId
			AND step_key = @stepKey
		ORDER BY sort_order, depends_on_step_key`,
		pgx.NamedArgs{"runbookRevisionId": runbookRevisionID, "stepKey": stepKey},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query RunbookStepDependency: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("could not collect RunbookStepDependency: %w", err)
	}
	return result, nil
}

func mapRunbookWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s Runbook: %w", action, apierrors.ErrAlreadyExists)
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s Runbook: %w", action, apierrors.ErrNotFound)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s Runbook: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s Runbook: %w", action, err)
}
