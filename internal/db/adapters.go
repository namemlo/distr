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

func CreateAdapterImplementation(
	ctx context.Context,
	implementation types.AdapterImplementation,
) (*types.AdapterImplementation, error) {
	if implementation.OrganizationID == uuid.Nil || len(implementation.Capabilities) == 0 {
		return nil, apierrors.NewBadRequest("adapter implementation identity and capabilities are required")
	}
	implementation.ID = uuid.New()
	err := RunTx(ctx, func(ctx context.Context) error {
		err := internalctx.GetDb(ctx).QueryRow(ctx, `
			INSERT INTO AdapterImplementation (
				id, organization_id, adapter_key, name, version, enabled
			) VALUES (
				@id, @organizationID, @key, @name, @version, @enabled
			)
			RETURNING created_at`,
			pgx.NamedArgs{
				"id": implementation.ID, "organizationID": implementation.OrganizationID,
				"key": implementation.Key, "name": implementation.Name,
				"version": implementation.Version, "enabled": implementation.Enabled,
			},
		).Scan(&implementation.CreatedAt)
		if err != nil {
			return mapAdapterWriteError("create implementation", err)
		}
		for index := range implementation.Capabilities {
			implementation.Capabilities[index].ID = uuid.New()
			implementation.Capabilities[index].AdapterImplementationID = implementation.ID
			implementation.Capabilities[index].OrganizationID = implementation.OrganizationID
		}
		_, err = internalctx.GetDb(ctx).CopyFrom(
			ctx,
			pgx.Identifier{"adaptercapability"},
			[]string{
				"id", "adapter_implementation_id", "organization_id", "capability", "version",
			},
			pgx.CopyFromSlice(len(implementation.Capabilities), func(index int) ([]any, error) {
				capability := implementation.Capabilities[index]
				return []any{
					capability.ID, capability.AdapterImplementationID, capability.OrganizationID,
					capability.Capability, capability.Version,
				}, nil
			}),
		)
		if err != nil {
			return mapAdapterWriteError("create capabilities", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &implementation, nil
}

func ListAdapterImplementations(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.AdapterImplementation, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT id, created_at, organization_id, adapter_key, name, version, enabled
		FROM AdapterImplementation
		WHERE organization_id = @organizationID
		ORDER BY adapter_key, version, id`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list AdapterImplementation: %w", err)
	}
	values, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.AdapterImplementation])
	if err != nil {
		return nil, fmt.Errorf("could not collect AdapterImplementation: %w", err)
	}
	for index := range values {
		values[index].Capabilities, err = getAdapterCapabilities(
			ctx,
			organizationID,
			values[index].ID,
		)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func ListAdapterImplementationsPage(
	ctx context.Context,
	filter types.RegistryListFilter,
) (*types.Page[types.AdapterImplementation], error) {
	limit, cursor, err := normalizeDeploymentRegistryListFilter(filter)
	if err != nil {
		return nil, err
	}
	var cursorCreatedAt any
	var cursorID any
	if cursor != nil {
		cursorCreatedAt, cursorID = cursor.CreatedAt, cursor.ID
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT id, created_at, organization_id, adapter_key, name, version, enabled
		FROM AdapterImplementation
		WHERE organization_id = @organizationID
		  AND (
		    @cursorCreatedAt::timestamptz IS NULL
		    OR (created_at, id) < (@cursorCreatedAt::timestamptz, @cursorID::uuid)
		  )
		ORDER BY created_at DESC, id DESC
		LIMIT @limit`,
		pgx.NamedArgs{
			"organizationID":  filter.OrganizationID,
			"cursorCreatedAt": cursorCreatedAt,
			"cursorID":        cursorID,
			"limit":           limit + 1,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list AdapterImplementation page: %w", err)
	}
	values, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.AdapterImplementation])
	if err != nil {
		return nil, fmt.Errorf("could not collect AdapterImplementation page: %w", err)
	}
	page := &types.Page[types.AdapterImplementation]{Items: values}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = encodeRegistryCursor(registryCursor{
			Version: registryCursorVersion, CreatedAt: last.CreatedAt, ID: last.ID,
		})
		if err != nil {
			return nil, err
		}
	}
	for index := range page.Items {
		page.Items[index].Capabilities, err = getAdapterCapabilities(
			ctx,
			filter.OrganizationID,
			page.Items[index].ID,
		)
		if err != nil {
			return nil, err
		}
	}
	return page, nil
}

func getAdapterCapabilities(
	ctx context.Context,
	organizationID uuid.UUID,
	implementationID uuid.UUID,
) ([]types.AdapterCapability, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT id, adapter_implementation_id, organization_id, capability, version
		FROM AdapterCapability
		WHERE organization_id = @organizationID
		  AND adapter_implementation_id = @implementationID
		ORDER BY capability, version, id`,
		pgx.NamedArgs{
			"organizationID": organizationID, "implementationID": implementationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query AdapterCapability: %w", err)
	}
	values, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.AdapterCapability])
	if err != nil {
		return nil, fmt.Errorf("could not collect AdapterCapability: %w", err)
	}
	return values, nil
}

func CreateAdapterAssignment(
	ctx context.Context,
	assignment types.AdapterAssignment,
) (*types.AdapterAssignment, error) {
	if assignment.OrganizationID == uuid.Nil || assignment.AdapterImplementationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("adapter assignment identity is required")
	}
	assignment.NormalizeKeyConfiguration()
	assignment.ID = uuid.New()
	err := RunTx(ctx, func(ctx context.Context) error {
		if err := requireAdapterScope(
			ctx,
			assignment.OrganizationID,
			assignment.ScopeType,
			assignment.ScopeID,
		); err != nil {
			return err
		}
		if err := requireTargetConfigSnapshot(
			ctx,
			assignment.OrganizationID,
			assignment.ConfigSnapshotID,
			assignment.ConfigChecksum,
		); err != nil {
			return err
		}
		err := internalctx.GetDb(ctx).QueryRow(ctx, `
			INSERT INTO AdapterAssignment (
				id, organization_id, adapter_implementation_id,
				scope_type, scope_id, config_snapshot_id, config_checksum,
				key_id, public_key_fingerprint, signing_key_reference,
				signing_key_version_fingerprint, enabled
			) VALUES (
				@id, @organizationID, @implementationID,
				@scopeType, @scopeID, @configSnapshotID, @configChecksum,
				@keyID, @publicKeyFingerprint, @signingKeyReference,
				@signingKeyVersionFingerprint, @enabled
			)
			RETURNING created_at, updated_at`,
			pgx.NamedArgs{
				"id": assignment.ID, "organizationID": assignment.OrganizationID,
				"implementationID": assignment.AdapterImplementationID,
				"scopeType":        assignment.ScopeType, "scopeID": assignment.ScopeID,
				"configSnapshotID":             assignment.ConfigSnapshotID,
				"configChecksum":               assignment.ConfigChecksum,
				"keyID":                        assignment.KeyID,
				"publicKeyFingerprint":         assignment.PublicKeyFingerprint,
				"signingKeyReference":          assignment.SigningKeyReference,
				"signingKeyVersionFingerprint": assignment.SigningKeyVersionFingerprint,
				"enabled":                      assignment.Enabled,
			},
		).Scan(&assignment.CreatedAt, &assignment.UpdatedAt)
		return mapAdapterWriteError("create assignment", err)
	})
	if err != nil {
		return nil, err
	}
	return &assignment, nil
}

func requireAdapterScope(
	ctx context.Context,
	organizationID uuid.UUID,
	scopeType types.AdapterScopeType,
	scopeID uuid.UUID,
) error {
	var query string
	switch scopeType {
	case types.AdapterScopeDeploymentTarget:
		query = `
			SELECT EXISTS (
				SELECT 1 FROM DeploymentTarget
				WHERE id = @scopeID AND organization_id = @organizationID
			)`
	case types.AdapterScopeDeploymentUnit:
		query = `
			SELECT EXISTS (
				SELECT 1 FROM DeploymentUnit
				WHERE id = @scopeID AND organization_id = @organizationID
			)`
	case types.AdapterScopeComponentInstance:
		query = `
			SELECT EXISTS (
				SELECT 1 FROM ComponentInstance
				WHERE id = @scopeID AND organization_id = @organizationID
			)`
	case types.AdapterScopeDatabaseResource, types.AdapterScopeObserverRegistration:
		return apierrors.NewBadRequest("adapter scope type requires its allocated predecessor schema")
	default:
		return apierrors.NewBadRequest("adapter scope type is invalid")
	}
	var exists bool
	if err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		query,
		pgx.NamedArgs{"scopeID": scopeID, "organizationID": organizationID},
	).Scan(&exists); err != nil {
		return fmt.Errorf("could not validate adapter scope: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func requireTargetConfigSnapshot(
	ctx context.Context,
	organizationID uuid.UUID,
	configSnapshotID uuid.UUID,
	checksum string,
) error {
	var exists bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM TargetConfigSnapshot
			WHERE id = @id
			  AND organization_id = @organizationID
			  AND canonical_checksum = @checksum
		)`,
		pgx.NamedArgs{
			"id": configSnapshotID, "organizationID": organizationID, "checksum": checksum,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate adapter TargetConfigSnapshot: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func ListAdapterAssignments(
	ctx context.Context,
	organizationID uuid.UUID,
) ([]types.AdapterAssignment, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
			id, created_at, updated_at, organization_id,
			adapter_implementation_id, scope_type, scope_id,
			config_snapshot_id, config_checksum, key_id,
			public_key_fingerprint, signing_key_reference,
			signing_key_version_fingerprint, enabled
		FROM AdapterAssignment
		WHERE organization_id = @organizationID
		ORDER BY scope_type, scope_id, adapter_implementation_id, id`,
		pgx.NamedArgs{"organizationID": organizationID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list AdapterAssignment: %w", err)
	}
	values, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.AdapterAssignment])
	if err != nil {
		return nil, fmt.Errorf("could not collect AdapterAssignment: %w", err)
	}
	for index := range values {
		values[index].NormalizeKeyConfiguration()
	}
	return values, nil
}

func ListAdapterAssignmentsPage(
	ctx context.Context,
	filter types.RegistryListFilter,
) (*types.Page[types.AdapterAssignment], error) {
	limit, cursor, err := normalizeDeploymentRegistryListFilter(filter)
	if err != nil {
		return nil, err
	}
	var cursorCreatedAt any
	var cursorID any
	if cursor != nil {
		cursorCreatedAt, cursorID = cursor.CreatedAt, cursor.ID
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
			id, created_at, updated_at, organization_id,
			adapter_implementation_id, scope_type, scope_id,
			config_snapshot_id, config_checksum, key_id,
			public_key_fingerprint, signing_key_reference,
			signing_key_version_fingerprint, enabled
		FROM AdapterAssignment
		WHERE organization_id = @organizationID
		  AND (
		    @cursorCreatedAt::timestamptz IS NULL
		    OR (created_at, id) < (@cursorCreatedAt::timestamptz, @cursorID::uuid)
		  )
		ORDER BY created_at DESC, id DESC
		LIMIT @limit`,
		pgx.NamedArgs{
			"organizationID":  filter.OrganizationID,
			"cursorCreatedAt": cursorCreatedAt,
			"cursorID":        cursorID,
			"limit":           limit + 1,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list AdapterAssignment page: %w", err)
	}
	values, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.AdapterAssignment])
	if err != nil {
		return nil, fmt.Errorf("could not collect AdapterAssignment page: %w", err)
	}
	page := &types.Page[types.AdapterAssignment]{Items: values}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = encodeRegistryCursor(registryCursor{
			Version: registryCursorVersion, CreatedAt: last.CreatedAt, ID: last.ID,
		})
		if err != nil {
			return nil, err
		}
	}
	for index := range page.Items {
		page.Items[index].NormalizeKeyConfiguration()
	}
	return page, nil
}

func GetDeploymentPlanStepAdapters(
	ctx context.Context,
	deploymentPlanID uuid.UUID,
	organizationID uuid.UUID,
) ([]types.DeploymentPlanStepAdapter, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT
			id, deployment_plan_id, deployment_plan_step_id, organization_id,
			step_key, adapter_assignment_id, adapter_implementation_id,
			implementation_version, capability, capability_version,
			scope_type, scope_id, config_snapshot_id, config_checksum,
			key_id, public_key_fingerprint, signing_key_reference,
			signing_key_version_fingerprint, sort_order
		FROM DeploymentPlanStepAdapter
		WHERE deployment_plan_id = @deploymentPlanID
		  AND organization_id = @organizationID
		ORDER BY sort_order, step_key`,
		pgx.NamedArgs{
			"deploymentPlanID": deploymentPlanID, "organizationID": organizationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query DeploymentPlanStepAdapter: %w", err)
	}
	values, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentPlanStepAdapter])
	if err != nil {
		return nil, fmt.Errorf("could not collect DeploymentPlanStepAdapter: %w", err)
	}
	for index := range values {
		values[index].NormalizeKeyConfiguration()
	}
	return values, nil
}

func GetAdapterRuntimeState(
	ctx context.Context,
	assignmentID uuid.UUID,
	organizationID uuid.UUID,
	capability string,
	capabilityVersion string,
) (*types.AdapterRuntimeState, error) {
	var state types.AdapterRuntimeState
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
			assignment.id,
			implementation.id,
			implementation.version,
			capability.capability,
			capability.version,
			assignment.scope_type,
			assignment.scope_id,
			assignment.config_snapshot_id,
			config.canonical_checksum,
			assignment.key_id,
			assignment.public_key_fingerprint,
			assignment.signing_key_reference,
			assignment.signing_key_version_fingerprint,
			(assignment.enabled AND implementation.enabled)
		FROM AdapterAssignment assignment
		JOIN AdapterImplementation implementation
		  ON implementation.id = assignment.adapter_implementation_id
		 AND implementation.organization_id = assignment.organization_id
		JOIN AdapterCapability capability
		  ON capability.adapter_implementation_id = implementation.id
		 AND capability.organization_id = implementation.organization_id
		JOIN TargetConfigSnapshot config
		  ON config.id = assignment.config_snapshot_id
		 AND config.organization_id = assignment.organization_id
		WHERE assignment.id = @assignmentID
		  AND assignment.organization_id = @organizationID
		  AND capability.capability = @capability
		  AND capability.version = @capabilityVersion`,
		pgx.NamedArgs{
			"assignmentID": assignmentID, "organizationID": organizationID,
			"capability": capability, "capabilityVersion": capabilityVersion,
		},
	).Scan(
		&state.AdapterAssignmentID,
		&state.AdapterImplementationID,
		&state.ImplementationVersion,
		&state.Capability,
		&state.CapabilityVersion,
		&state.ScopeType,
		&state.ScopeID,
		&state.ConfigSnapshotID,
		&state.ConfigChecksum,
		&state.KeyConfiguration.KeyID,
		&state.KeyConfiguration.PublicKeyFingerprint,
		&state.KeyConfiguration.SigningKeyReference,
		&state.KeyConfiguration.SigningKeyVersionFingerprint,
		&state.Enabled,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not query current adapter runtime state: %w", err)
	}
	return &state, nil
}

func insertDeploymentPlanStepAdapters(
	ctx context.Context,
	plan types.DeploymentPlan,
) error {
	if len(plan.StepAdapters) == 0 {
		return nil
	}
	stepIDs := make(map[string]uuid.UUID, len(plan.Steps))
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT step_key, id
		FROM DeploymentPlanStep
		WHERE deployment_plan_id = @deploymentPlanID
		  AND organization_id = @organizationID`,
		pgx.NamedArgs{
			"deploymentPlanID": plan.ID, "organizationID": plan.OrganizationID,
		},
	)
	if err != nil {
		return fmt.Errorf("could not query plan steps for adapter freeze: %w", err)
	}
	for rows.Next() {
		var key string
		var id uuid.UUID
		if err := rows.Scan(&key, &id); err != nil {
			return fmt.Errorf("could not scan plan step for adapter freeze: %w", err)
		}
		stepIDs[key] = id
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("could not collect plan steps for adapter freeze: %w", err)
	}
	_, err = internalctx.GetDb(ctx).CopyFrom(
		ctx,
		pgx.Identifier{"deploymentplanstepadapter"},
		[]string{
			"id", "deployment_plan_id", "deployment_plan_step_id", "organization_id",
			"step_key", "adapter_assignment_id", "adapter_implementation_id",
			"implementation_version", "capability", "capability_version",
			"scope_type", "scope_id", "config_snapshot_id", "config_checksum",
			"key_id", "public_key_fingerprint", "signing_key_reference",
			"signing_key_version_fingerprint", "sort_order",
		},
		pgx.CopyFromSlice(len(plan.StepAdapters), func(index int) ([]any, error) {
			value := plan.StepAdapters[index]
			value.NormalizeKeyConfiguration()
			stepID, ok := stepIDs[value.StepKey]
			if !ok {
				return nil, fmt.Errorf("adapter references unknown plan step %q", value.StepKey)
			}
			return []any{
				value.ID, plan.ID, stepID, plan.OrganizationID, value.StepKey,
				value.AdapterAssignmentID, value.AdapterImplementationID,
				value.ImplementationVersion, value.Capability, value.CapabilityVersion,
				value.ScopeType, value.ScopeID, value.ConfigSnapshotID, value.ConfigChecksum,
				value.KeyID, value.PublicKeyFingerprint, value.SigningKeyReference,
				value.SigningKeyVersionFingerprint, value.SortOrder,
			}, nil
		}),
	)
	if err != nil {
		return mapAdapterWriteError("freeze plan step adapters", err)
	}
	return nil
}

func mapAdapterWriteError(action string, err error) error {
	if err == nil {
		return nil
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s: %w", action, apierrors.ErrAlreadyExists)
		case pgerrcode.ForeignKeyViolation, pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s: %w", action, err)
}
