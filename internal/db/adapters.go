package db

import (
	"context"
	"encoding/json"
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
	return createAdapterImplementationWithAudit(
		ctx,
		implementation,
		DirectControlPlaneAuditAppendHook(),
	)
}

func createAdapterImplementationWithAudit(
	ctx context.Context,
	implementation types.AdapterImplementation,
	auditHook ControlPlaneAuditAppendHook,
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
		event, err := adapterImplementationPublishedAuditEvent(implementation)
		if err != nil {
			return err
		}
		return RecordControlPlaneAuditMutation(ctx, auditHook, event)
	})
	if err != nil {
		return nil, err
	}
	return &implementation, nil
}

func adapterImplementationPublishedAuditEvent(
	implementation types.AdapterImplementation,
) (types.ControlPlaneAuditEventInput, error) {
	capabilities := make([]map[string]string, 0, len(implementation.Capabilities))
	for _, capability := range implementation.Capabilities {
		capabilities = append(capabilities, map[string]string{
			"capability": capability.Capability,
			"version":    capability.Version,
		})
	}
	payload, err := json.Marshal(map[string]any{
		"adapterKey":   implementation.Key,
		"name":         implementation.Name,
		"version":      implementation.Version,
		"enabled":      implementation.Enabled,
		"capabilities": capabilities,
	})
	if err != nil {
		return types.ControlPlaneAuditEventInput{}, fmt.Errorf("could not encode adapter implementation audit: %w", err)
	}
	return types.ControlPlaneAuditEventInput{
		OrganizationID:    implementation.OrganizationID,
		EventType:         "adapter.implementation.published",
		Outcome:           "SUCCEEDED",
		AdapterRevisionID: &implementation.ID,
		Payload:           payload,
	}, nil
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
	if err := populateAdapterCapabilities(ctx, organizationID, values); err != nil {
		return nil, err
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
	if err := populateAdapterCapabilities(ctx, filter.OrganizationID, page.Items); err != nil {
		return nil, err
	}
	return page, nil
}

func populateAdapterCapabilities(
	ctx context.Context,
	organizationID uuid.UUID,
	implementations []types.AdapterImplementation,
) error {
	implementationIDs := make([]uuid.UUID, len(implementations))
	for index := range implementations {
		implementationIDs[index] = implementations[index].ID
	}
	capabilities, err := getAdapterCapabilitiesByImplementation(
		ctx,
		organizationID,
		implementationIDs,
	)
	if err != nil {
		return err
	}
	for index := range implementations {
		implementations[index].Capabilities = capabilities[implementations[index].ID]
	}
	return nil
}

func getAdapterCapabilitiesByImplementation(
	ctx context.Context,
	organizationID uuid.UUID,
	implementationIDs []uuid.UUID,
) (map[uuid.UUID][]types.AdapterCapability, error) {
	result := make(map[uuid.UUID][]types.AdapterCapability, len(implementationIDs))
	if len(implementationIDs) == 0 {
		return result, nil
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT id, adapter_implementation_id, organization_id, capability, version
		FROM AdapterCapability
		WHERE organization_id = @organizationID
		  AND adapter_implementation_id = ANY(@implementationIDs)
		ORDER BY adapter_implementation_id, capability, version, id`,
		pgx.NamedArgs{
			"organizationID": organizationID, "implementationIDs": implementationIDs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not batch query AdapterCapability: %w", err)
	}
	values, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.AdapterCapability])
	if err != nil {
		return nil, fmt.Errorf("could not collect AdapterCapability: %w", err)
	}
	for _, capability := range values {
		result[capability.AdapterImplementationID] = append(
			result[capability.AdapterImplementationID],
			capability,
		)
	}
	return result, nil
}

func CreateAdapterAssignment(
	ctx context.Context,
	assignment types.AdapterAssignment,
) (*types.AdapterAssignment, error) {
	return createAdapterAssignmentWithAudit(ctx, assignment, DirectControlPlaneAuditAppendHook())
}

func createAdapterAssignmentWithAudit(
	ctx context.Context,
	assignment types.AdapterAssignment,
	auditHook ControlPlaneAuditAppendHook,
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
			assignment.ScopeReference,
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
				scope_type, scope_reference, config_snapshot_id, config_checksum,
				key_id, public_key_fingerprint, signing_key_reference,
				signing_key_version_fingerprint, enabled
			) VALUES (
				@id, @organizationID, @implementationID,
				@scopeType, @scopeReference, @configSnapshotID, @configChecksum,
				@keyID, @publicKeyFingerprint, @signingKeyReference,
				@signingKeyVersionFingerprint, @enabled
			)
			RETURNING created_at, updated_at`,
			pgx.NamedArgs{
				"id": assignment.ID, "organizationID": assignment.OrganizationID,
				"implementationID":             assignment.AdapterImplementationID,
				"scopeType":                    assignment.ScopeType,
				"scopeReference":               assignment.ScopeReference,
				"configSnapshotID":             assignment.ConfigSnapshotID,
				"configChecksum":               assignment.ConfigChecksum,
				"keyID":                        assignment.KeyID,
				"publicKeyFingerprint":         assignment.PublicKeyFingerprint,
				"signingKeyReference":          assignment.SigningKeyReference,
				"signingKeyVersionFingerprint": assignment.SigningKeyVersionFingerprint,
				"enabled":                      assignment.Enabled,
			},
		).Scan(&assignment.CreatedAt, &assignment.UpdatedAt)
		if err := mapAdapterWriteError("create assignment", err); err != nil {
			return err
		}
		event, err := adapterAssignmentPublishedAuditEvent(assignment)
		if err != nil {
			return err
		}
		return RecordControlPlaneAuditMutation(ctx, auditHook, event)
	})
	if err != nil {
		return nil, err
	}
	return &assignment, nil
}

func adapterAssignmentPublishedAuditEvent(
	assignment types.AdapterAssignment,
) (types.ControlPlaneAuditEventInput, error) {
	assignment.NormalizeKeyConfiguration()
	payload, err := json.Marshal(map[string]any{
		"adapterImplementationId":      assignment.AdapterImplementationID,
		"scopeType":                    assignment.ScopeType,
		"scopeReference":               assignment.ScopeReference,
		"keyId":                        assignment.KeyConfiguration.KeyID,
		"publicKeyFingerprint":         assignment.KeyConfiguration.PublicKeyFingerprint,
		"signingKeyVersionFingerprint": assignment.KeyConfiguration.SigningKeyVersionFingerprint,
		"enabled":                      assignment.Enabled,
	})
	if err != nil {
		return types.ControlPlaneAuditEventInput{}, fmt.Errorf("could not encode adapter assignment audit: %w", err)
	}
	return types.ControlPlaneAuditEventInput{
		OrganizationID:       assignment.OrganizationID,
		EventType:            "adapter.assignment.published",
		Outcome:              "SUCCEEDED",
		AdapterRevisionID:    &assignment.ID,
		TargetConfigID:       &assignment.ConfigSnapshotID,
		TargetConfigChecksum: assignment.ConfigChecksum,
		Payload:              payload,
	}, nil
}

func requireAdapterScope(
	ctx context.Context,
	organizationID uuid.UUID,
	scopeType types.AdapterScopeType,
	scopeReference string,
) error {
	if !scopeType.IsValidReference(scopeReference) {
		return apierrors.NewBadRequest("adapter scope reference is invalid for scope type")
	}
	var query string
	var scopeID uuid.UUID
	switch scopeType {
	case types.AdapterScopeDeploymentTarget:
		scopeID = uuid.MustParse(scopeReference)
		query = `
			SELECT EXISTS (
				SELECT 1 FROM DeploymentTarget
				WHERE id = @scopeID AND organization_id = @organizationID
			)`
	case types.AdapterScopeDeploymentUnit:
		scopeID = uuid.MustParse(scopeReference)
		query = `
			SELECT EXISTS (
				SELECT 1 FROM DeploymentUnit
				WHERE id = @scopeID AND organization_id = @organizationID
			)`
	case types.AdapterScopeComponentInstance:
		scopeID = uuid.MustParse(scopeReference)
		query = `
			SELECT EXISTS (
				SELECT 1 FROM ComponentInstance
				WHERE id = @scopeID AND organization_id = @organizationID
			)`
	case types.AdapterScopeDatabaseResource:
		return validateDatabaseResourceReference(scopeReference)
	case types.AdapterScopeObserverRegistration:
		scopeID = uuid.MustParse(scopeReference)
		query = `
			SELECT EXISTS (
				SELECT 1 FROM ObserverRegistration
				WHERE id = @scopeID AND organization_id = @organizationID
			)`
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

func validateDatabaseResourceReference(reference string) error {
	if !types.AdapterScopeDatabaseResource.IsValidReference(reference) {
		return apierrors.NewBadRequest("database resource scope reference is invalid")
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
			adapter_implementation_id, scope_type, scope_reference,
			config_snapshot_id, config_checksum, key_id,
			public_key_fingerprint, signing_key_reference,
			signing_key_version_fingerprint, enabled
		FROM AdapterAssignment
		WHERE organization_id = @organizationID
		ORDER BY scope_type, scope_reference, adapter_implementation_id, id`,
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
			adapter_implementation_id, scope_type, scope_reference,
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
			scope_type, scope_reference, config_snapshot_id, config_checksum,
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
			assignment.scope_reference,
			assignment.config_snapshot_id,
			assignment.config_checksum,
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
		&state.ScopeReference,
		&state.ConfigSnapshotID,
		&state.AssignmentConfigChecksum,
		&state.SnapshotCanonicalChecksum,
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
	return insertDeploymentPlanStepAdaptersWithAudit(
		ctx,
		plan,
		DirectControlPlaneAuditAppendHook(),
	)
}

func insertDeploymentPlanStepAdaptersWithAudit(
	ctx context.Context,
	plan types.DeploymentPlan,
	auditHook ControlPlaneAuditAppendHook,
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
			"scope_type", "scope_reference", "config_snapshot_id", "config_checksum",
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
				value.ScopeType, value.ScopeReference,
				value.ConfigSnapshotID, value.ConfigChecksum,
				value.KeyID, value.PublicKeyFingerprint, value.SigningKeyReference,
				value.SigningKeyVersionFingerprint, value.SortOrder,
			}, nil
		}),
	)
	if err != nil {
		return mapAdapterWriteError("freeze plan step adapters", err)
	}
	return recordFrozenAdapterSelectionAudit(ctx, auditHook, plan)
}

func recordFrozenAdapterSelectionAudit(
	ctx context.Context,
	auditHook ControlPlaneAuditAppendHook,
	plan types.DeploymentPlan,
) error {
	for index := range plan.StepAdapters {
		frozen := plan.StepAdapters[index]
		frozen.NormalizeKeyConfiguration()
		payload, err := json.Marshal(map[string]any{
			"stepKey":                      frozen.StepKey,
			"adapterAssignmentId":          frozen.AdapterAssignmentID,
			"adapterImplementationId":      frozen.AdapterImplementationID,
			"implementationVersion":        frozen.ImplementationVersion,
			"capability":                   frozen.Capability,
			"capabilityVersion":            frozen.CapabilityVersion,
			"scopeType":                    frozen.ScopeType,
			"scopeReference":               frozen.ScopeReference,
			"keyId":                        frozen.KeyConfiguration.KeyID,
			"publicKeyFingerprint":         frozen.KeyConfiguration.PublicKeyFingerprint,
			"signingKeyVersionFingerprint": frozen.KeyConfiguration.SigningKeyVersionFingerprint,
		})
		if err != nil {
			return fmt.Errorf("could not encode frozen adapter selection audit: %w", err)
		}
		event := types.ControlPlaneAuditEventInput{
			OrganizationID:         plan.OrganizationID,
			EventType:              "adapter.revision.selected",
			ActorID:                plan.PublishedByUserAccountID,
			Outcome:                "SUCCEEDED",
			DeploymentPlanID:       &plan.ID,
			AdapterRevisionID:      &frozen.ID,
			TargetConfigID:         &frozen.ConfigSnapshotID,
			TargetConfigChecksum:   frozen.ConfigChecksum,
			DeploymentPlanChecksum: plan.CanonicalChecksum,
			Payload:                payload,
		}
		if err := RecordControlPlaneAuditMutation(ctx, auditHook, event); err != nil {
			return err
		}
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
