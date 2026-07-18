package db

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/deploymentregistry"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	registryDefaultPageLimit = 50
	registryMaximumPageLimit = 100
	registryCursorVersion    = 1
)

const deploymentScopeOutputExpr = `
	s.id,
	s.created_at,
	s.updated_at,
	s.organization_id,
	s.customer_organization_id,
	s.key,
	s.name,
	s.description,
	s.delivery_model,
	s.management_state,
	s.retired_at
`

const targetEnvironmentAssignmentOutputExpr = `
	a.id,
	a.created_at,
	a.updated_at,
	a.organization_id,
	a.deployment_target_id,
	a.environment_id,
	a.active_from,
	a.active_until,
	a.policy_constraints
`

const deploymentUnitOutputExpr = `
	u.id,
	u.created_at,
	u.updated_at,
	u.organization_id,
	u.deployment_scope_id,
	u.target_environment_assignment_id,
	u.deployment_target_id,
	u.key,
	u.name,
	u.physical_identity,
	u.management_state,
	u.subscriber_set_checksum,
	u.retired_at
`

const deploymentUnitSubscriberOutputExpr = `
	us.id,
	us.created_at,
	us.organization_id,
	us.deployment_unit_id,
	us.customer_organization_id,
	us.retired_at
`

const componentDefinitionOutputExpr = `
	d.id,
	d.created_at,
	d.updated_at,
	d.organization_id,
	d.key,
	d.name,
	d.description,
	d.capability_scope,
	d.management_state,
	d.retired_at
`

const componentAliasOutputExpr = `
	ca.id,
	ca.created_at,
	ca.organization_id,
	ca.component_definition_id,
	ca.alias,
	ca.retired_at
`

const componentInstanceOutputExpr = `
	ci.id,
	ci.created_at,
	ci.updated_at,
	ci.organization_id,
	ci.deployment_unit_id,
	ci.component_definition_id,
	ci.physical_name,
	ci.config_namespace,
	ci.database_boundary,
	ci.health_adapter,
	ci.management_state,
	ci.retired_at
`

type registryCursor struct {
	Version   int       `json:"v"`
	CreatedAt time.Time `json:"createdAt"`
	ID        uuid.UUID `json:"id"`
}

func CreateDeploymentScope(ctx context.Context, scope *types.DeploymentScope) error {
	if scope == nil {
		return apierrors.NewBadRequest("deployment scope is required")
	}
	if err := validateDeploymentScopeForWrite(*scope); err != nil {
		return err
	}
	scope.Key = strings.TrimSpace(scope.Key)
	scope.Name = strings.TrimSpace(scope.Name)
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO DeploymentScope AS s (
			organization_id,
			customer_organization_id,
			key,
			name,
			description,
			delivery_model,
			management_state,
			retired_at
		) VALUES (
			@organizationID,
			@customerOrganizationID,
			@key,
			@name,
			@description,
			@deliveryModel,
			@managementState,
			@retiredAt
		)
		RETURNING `+deploymentScopeOutputExpr,
		pgx.NamedArgs{
			"organizationID":         scope.OrganizationID,
			"customerOrganizationID": scope.CustomerOrganizationID,
			"key":                    scope.Key,
			"name":                   scope.Name,
			"description":            scope.Description,
			"deliveryModel":          scope.DeliveryModel,
			"managementState":        scope.ManagementState,
			"retiredAt":              scope.RetiredAt,
		},
	)
	if err != nil {
		return mapDeploymentRegistryWriteError("create deployment scope", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentScope])
	if err != nil {
		return mapDeploymentRegistryWriteError("read created deployment scope", err)
	}
	*scope = result
	return nil
}

func CreateTargetEnvironmentAssignment(
	ctx context.Context,
	assignment *types.TargetEnvironmentAssignment,
) error {
	if assignment == nil {
		return apierrors.NewBadRequest("target environment assignment is required")
	}
	if assignment.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if assignment.DeploymentTargetID == uuid.Nil {
		return apierrors.NewBadRequest("deploymentTargetId is required")
	}
	if assignment.EnvironmentID == uuid.Nil {
		return apierrors.NewBadRequest("environmentId is required")
	}
	if assignment.ActiveFrom.IsZero() {
		return apierrors.NewBadRequest("activeFrom is required")
	}
	if assignment.ActiveUntil != nil && !assignment.ActiveUntil.After(assignment.ActiveFrom) {
		return apierrors.NewBadRequest("activeUntil must be after activeFrom")
	}
	if len(assignment.PolicyConstraints) == 0 {
		assignment.PolicyConstraints = json.RawMessage(`{}`)
	}
	if !json.Valid(assignment.PolicyConstraints) {
		return apierrors.NewBadRequest("policyConstraints must be valid JSON")
	}

	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO TargetEnvironmentAssignment AS a (
			organization_id,
			deployment_target_id,
			environment_id,
			active_from,
			active_until,
			policy_constraints
		) VALUES (
			@organizationID,
			@deploymentTargetID,
			@environmentID,
			@activeFrom,
			@activeUntil,
			@policyConstraints
		)
		RETURNING `+targetEnvironmentAssignmentOutputExpr,
		pgx.NamedArgs{
			"organizationID":     assignment.OrganizationID,
			"deploymentTargetID": assignment.DeploymentTargetID,
			"environmentID":      assignment.EnvironmentID,
			"activeFrom":         assignment.ActiveFrom,
			"activeUntil":        assignment.ActiveUntil,
			"policyConstraints":  assignment.PolicyConstraints,
		},
	)
	if err != nil {
		return mapDeploymentRegistryWriteError("create target environment assignment", err)
	}
	result, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.TargetEnvironmentAssignment],
	)
	if err != nil {
		return mapDeploymentRegistryWriteError("read created target environment assignment", err)
	}
	*assignment = result
	return nil
}

func CreateDeploymentUnit(ctx context.Context, unit *types.DeploymentUnit) error {
	return CreateDeploymentUnitWithSubscribers(ctx, unit, nil)
}

func CreateDeploymentUnitWithSubscribers(
	ctx context.Context,
	unit *types.DeploymentUnit,
	subscribers []types.DeploymentUnitSubscriber,
) error {
	if unit == nil {
		return apierrors.NewBadRequest("deployment unit is required")
	}
	err := RunTx(ctx, func(txCtx context.Context) error {
		if err := validateDeploymentUnitForWrite(*unit); err != nil {
			return err
		}
		scope, err := GetDeploymentScope(txCtx, unit.OrganizationID, unit.DeploymentScopeID)
		if err != nil {
			return err
		}
		if scope.DeliveryModel == types.DeliveryModelShared {
			if len(subscribers) == 0 {
				return apierrors.NewBadRequest(
					"shared deployment units require atomic subscriber initialization",
				)
			}
		} else if len(subscribers) != 0 {
			return apierrors.NewBadRequest("only shared deployment units may have subscribers")
		}

		seen := make(map[uuid.UUID]struct{}, len(subscribers))
		for _, subscriber := range subscribers {
			if subscriber.OrganizationID != unit.OrganizationID {
				return apierrors.NewBadRequest(
					"all subscribers must belong to the deployment unit organization",
				)
			}
			if subscriber.DeploymentUnitID != uuid.Nil {
				return apierrors.NewBadRequest(
					"subscriber deploymentUnitId is assigned during atomic initialization",
				)
			}
			if subscriber.CustomerOrganizationID == uuid.Nil {
				return apierrors.NewBadRequest("customerOrganizationId is required")
			}
			if subscriber.RetiredAt != nil {
				return apierrors.NewBadRequest("initial subscribers must be active")
			}
			if _, exists := seen[subscriber.CustomerOrganizationID]; exists {
				return apierrors.NewBadRequest("customerOrganizationIds must be unique")
			}
			seen[subscriber.CustomerOrganizationID] = struct{}{}
			if err := ensureRegistryCustomerExists(
				txCtx,
				unit.OrganizationID,
				subscriber.CustomerOrganizationID,
			); err != nil {
				return err
			}
		}
		expectedChecksum := deploymentregistry.SubscriberSetChecksum(subscribers)
		if unit.SubscriberSetChecksum != expectedChecksum {
			return apierrors.NewBadRequest(
				"subscriber set does not match the deployment unit subscriber-set checksum",
			)
		}
		if err := insertDeploymentUnit(txCtx, unit); err != nil {
			return err
		}
		for index := range subscribers {
			subscribers[index].DeploymentUnitID = unit.ID
			if err := insertDeploymentUnitSubscriber(txCtx, &subscribers[index]); err != nil {
				return err
			}
		}
		return sealDeploymentUnitSubscriberSet(txCtx, unit.OrganizationID, unit.ID)
	})
	if err != nil {
		if errors.Is(err, apierrors.ErrBadRequest) ||
			errors.Is(err, apierrors.ErrNotFound) ||
			errors.Is(err, apierrors.ErrAlreadyExists) ||
			errors.Is(err, apierrors.ErrConflict) {
			return err
		}
		return mapDeploymentRegistryWriteError("create deployment unit with subscribers", err)
	}
	return nil
}

func insertDeploymentUnit(ctx context.Context, unit *types.DeploymentUnit) error {
	if unit == nil {
		return apierrors.NewBadRequest("deployment unit is required")
	}
	if err := validateDeploymentUnitForWrite(*unit); err != nil {
		return err
	}
	unit.Key = strings.TrimSpace(unit.Key)
	unit.Name = strings.TrimSpace(unit.Name)
	unit.PhysicalIdentity = strings.TrimSpace(unit.PhysicalIdentity)
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO DeploymentUnit AS u (
			organization_id,
			deployment_scope_id,
			target_environment_assignment_id,
			deployment_target_id,
			key,
			name,
			physical_identity,
			management_state,
			subscriber_set_checksum,
			retired_at
		) VALUES (
			@organizationID,
			@deploymentScopeID,
			@targetEnvironmentAssignmentID,
			@deploymentTargetID,
			@key,
			@name,
			@physicalIdentity,
			@managementState,
			@subscriberSetChecksum,
			@retiredAt
		)
		RETURNING `+deploymentUnitOutputExpr,
		pgx.NamedArgs{
			"organizationID":                unit.OrganizationID,
			"deploymentScopeID":             unit.DeploymentScopeID,
			"targetEnvironmentAssignmentID": unit.TargetEnvironmentAssignmentID,
			"deploymentTargetID":            unit.DeploymentTargetID,
			"key":                           unit.Key,
			"name":                          unit.Name,
			"physicalIdentity":              unit.PhysicalIdentity,
			"managementState":               unit.ManagementState,
			"subscriberSetChecksum":         unit.SubscriberSetChecksum,
			"retiredAt":                     unit.RetiredAt,
		},
	)
	if err != nil {
		return mapDeploymentRegistryWriteError("create deployment unit", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.DeploymentUnit])
	if err != nil {
		return mapDeploymentRegistryWriteError("read created deployment unit", err)
	}
	*unit = result
	return nil
}

func sealDeploymentUnitSubscriberSet(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentUnitID uuid.UUID,
) error {
	commandTag, err := internalctx.GetDb(ctx).Exec(ctx, `
		UPDATE DeploymentUnit
		SET subscriber_set_sealed_at = now()
		WHERE id = @deploymentUnitID
		  AND organization_id = @organizationID
		  AND subscriber_set_sealed_at IS NULL`,
		pgx.NamedArgs{
			"deploymentUnitID": deploymentUnitID,
			"organizationID":   organizationID,
		},
	)
	if err != nil {
		return mapDeploymentRegistryWriteError("seal deployment unit subscriber set", err)
	}
	if commandTag.RowsAffected() != 1 {
		return apierrors.NewConflict("deployment unit subscriber set is already sealed")
	}
	return nil
}

func CreateDeploymentUnitSubscriber(
	ctx context.Context,
	subscriber *types.DeploymentUnitSubscriber,
) error {
	if subscriber == nil {
		return apierrors.NewBadRequest("deployment unit subscriber is required")
	}
	subscribers := []types.DeploymentUnitSubscriber{*subscriber}
	if err := CreateDeploymentUnitSubscribers(ctx, subscribers); err != nil {
		return err
	}
	*subscriber = subscribers[0]
	return nil
}

func CreateDeploymentUnitSubscribers(
	ctx context.Context,
	subscribers []types.DeploymentUnitSubscriber,
) error {
	if len(subscribers) == 0 {
		return apierrors.NewBadRequest("at least one deployment unit subscriber is required")
	}
	organizationID := subscribers[0].OrganizationID
	deploymentUnitID := subscribers[0].DeploymentUnitID
	if organizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if deploymentUnitID == uuid.Nil {
		return apierrors.NewBadRequest("deploymentUnitId is required")
	}
	seen := make(map[uuid.UUID]struct{}, len(subscribers))
	for _, subscriber := range subscribers {
		if subscriber.OrganizationID != organizationID ||
			subscriber.DeploymentUnitID != deploymentUnitID {
			return apierrors.NewBadRequest(
				"all subscribers must belong to the same organization and deployment unit",
			)
		}
		if subscriber.CustomerOrganizationID == uuid.Nil {
			return apierrors.NewBadRequest("customerOrganizationId is required")
		}
		if _, exists := seen[subscriber.CustomerOrganizationID]; exists {
			return apierrors.NewBadRequest("customerOrganizationIds must be unique")
		}
		seen[subscriber.CustomerOrganizationID] = struct{}{}
		if err := ensureRegistryCustomerExists(
			ctx,
			organizationID,
			subscriber.CustomerOrganizationID,
		); err != nil {
			return err
		}
	}
	if _, err := GetDeploymentUnit(ctx, organizationID, deploymentUnitID); err != nil {
		return err
	}
	return apierrors.NewConflict(
		"deployment unit subscribers are initialized atomically with the unit and are immutable",
	)
}

func insertDeploymentUnitSubscriber(
	ctx context.Context,
	subscriber *types.DeploymentUnitSubscriber,
) error {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO DeploymentUnitSubscriber AS us (
			organization_id,
			deployment_unit_id,
			customer_organization_id,
			retired_at
		) VALUES (
			@organizationID,
			@deploymentUnitID,
			@customerOrganizationID,
			@retiredAt
		)
		RETURNING `+deploymentUnitSubscriberOutputExpr,
		pgx.NamedArgs{
			"organizationID":         subscriber.OrganizationID,
			"deploymentUnitID":       subscriber.DeploymentUnitID,
			"customerOrganizationID": subscriber.CustomerOrganizationID,
			"retiredAt":              subscriber.RetiredAt,
		},
	)
	if err != nil {
		return mapDeploymentRegistryWriteError("create deployment unit subscriber", err)
	}
	result, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.DeploymentUnitSubscriber],
	)
	if err != nil {
		return mapDeploymentRegistryWriteError("read created deployment unit subscriber", err)
	}
	*subscriber = result
	return nil
}

func CreateComponentDefinition(
	ctx context.Context,
	definition *types.ComponentDefinition,
) error {
	if definition == nil {
		return apierrors.NewBadRequest("component definition is required")
	}
	if err := validateComponentDefinitionForWrite(*definition); err != nil {
		return err
	}
	definition.Key = strings.TrimSpace(definition.Key)
	definition.Name = strings.TrimSpace(definition.Name)
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO ComponentDefinition AS d (
			organization_id,
			key,
			name,
			description,
			capability_scope,
			management_state,
			retired_at
		) VALUES (
			@organizationID,
			@key,
			@name,
			@description,
			@capabilityScope,
			@managementState,
			@retiredAt
		)
		RETURNING `+componentDefinitionOutputExpr,
		pgx.NamedArgs{
			"organizationID":  definition.OrganizationID,
			"key":             definition.Key,
			"name":            definition.Name,
			"description":     definition.Description,
			"capabilityScope": definition.CapabilityScope,
			"managementState": definition.ManagementState,
			"retiredAt":       definition.RetiredAt,
		},
	)
	if err != nil {
		return mapDeploymentRegistryWriteError("create component definition", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ComponentDefinition])
	if err != nil {
		return mapDeploymentRegistryWriteError("read created component definition", err)
	}
	*definition = result
	return nil
}

func CreateComponentAlias(ctx context.Context, alias *types.ComponentAlias) error {
	if alias == nil {
		return apierrors.NewBadRequest("component alias is required")
	}
	if alias.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if alias.ComponentDefinitionID == uuid.Nil {
		return apierrors.NewBadRequest("componentDefinitionId is required")
	}
	alias.Alias = strings.ToLower(strings.TrimSpace(alias.Alias))
	if alias.Alias == "" {
		return apierrors.NewBadRequest("alias is required")
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO ComponentAlias AS ca (
			organization_id,
			component_definition_id,
			alias,
			retired_at
		) VALUES (
			@organizationID,
			@componentDefinitionID,
			@alias,
			@retiredAt
		)
		RETURNING `+componentAliasOutputExpr,
		pgx.NamedArgs{
			"organizationID":        alias.OrganizationID,
			"componentDefinitionID": alias.ComponentDefinitionID,
			"alias":                 alias.Alias,
			"retiredAt":             alias.RetiredAt,
		},
	)
	if err != nil {
		return mapDeploymentRegistryWriteError("create component alias", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ComponentAlias])
	if err != nil {
		return mapDeploymentRegistryWriteError("read created component alias", err)
	}
	*alias = result
	return nil
}

func CreateComponentInstance(ctx context.Context, instance *types.ComponentInstance) error {
	return RunTx(ctx, func(txCtx context.Context) error {
		return createComponentInstance(txCtx, instance)
	})
}

func createComponentInstance(ctx context.Context, instance *types.ComponentInstance) error {
	if instance == nil {
		return apierrors.NewBadRequest("component instance is required")
	}
	if err := validateComponentInstanceForWrite(*instance); err != nil {
		return err
	}
	instance.PhysicalName = strings.TrimSpace(instance.PhysicalName)
	instance.RenamedFrom = strings.TrimSpace(instance.RenamedFrom)
	renamedFrom := instance.RenamedFrom
	var renameAlias *types.ComponentAlias
	if renamedFrom != "" {
		if renamedFrom == instance.PhysicalName {
			return apierrors.NewBadRequest(
				"a renamed component must change its physical name",
			)
		}
		var err error
		renameAlias, err = lockActiveComponentAlias(
			ctx,
			instance.OrganizationID,
			instance.ComponentDefinitionID,
			renamedFrom,
		)
		if err != nil {
			return err
		}
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		INSERT INTO ComponentInstance AS ci (
			organization_id,
			deployment_unit_id,
			component_definition_id,
			physical_name,
			config_namespace,
			database_boundary,
			health_adapter,
			management_state,
			retired_at
		) VALUES (
			@organizationID,
			@deploymentUnitID,
			@componentDefinitionID,
			@physicalName,
			@configNamespace,
			@databaseBoundary,
			@healthAdapter,
			@managementState,
			@retiredAt
		)
		RETURNING `+componentInstanceOutputExpr,
		pgx.NamedArgs{
			"organizationID":        instance.OrganizationID,
			"deploymentUnitID":      instance.DeploymentUnitID,
			"componentDefinitionID": instance.ComponentDefinitionID,
			"physicalName":          instance.PhysicalName,
			"configNamespace":       instance.ConfigNamespace,
			"databaseBoundary":      instance.DatabaseBoundary,
			"healthAdapter":         instance.HealthAdapter,
			"managementState":       instance.ManagementState,
			"retiredAt":             instance.RetiredAt,
		},
	)
	if err != nil {
		return mapDeploymentRegistryWriteError("create component instance", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.ComponentInstance])
	if err != nil {
		return mapDeploymentRegistryWriteError("read created component instance", err)
	}
	*instance = result
	if renameAlias != nil {
		if err := createComponentInstanceRename(
			ctx,
			instance.OrganizationID,
			instance.ID,
			renameAlias.ID,
			renamedFrom,
			instance.PhysicalName,
		); err != nil {
			return err
		}
	}
	return nil
}

func UpdateDeploymentScope(ctx context.Context, scope *types.DeploymentScope) error {
	if scope == nil {
		return apierrors.NewBadRequest("deployment scope is required")
	}
	if scope.ID == uuid.Nil {
		return apierrors.NewBadRequest("id is required")
	}
	if err := validateDeploymentScopeForWrite(*scope); err != nil {
		return err
	}
	scope.Name = strings.TrimSpace(scope.Name)
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		UPDATE DeploymentScope AS s
		SET name = @name,
		    description = @description,
		    management_state = @managementState,
		    retired_at = @retiredAt,
		    updated_at = now()
		WHERE s.id = @id
		  AND s.organization_id = @organizationID
		RETURNING `+deploymentScopeOutputExpr,
		pgx.NamedArgs{
			"id":              scope.ID,
			"organizationID":  scope.OrganizationID,
			"name":            scope.Name,
			"description":     scope.Description,
			"managementState": scope.ManagementState,
			"retiredAt":       scope.RetiredAt,
		},
	)
	return assignDeploymentRegistryWriteResult(scope, rows, err, "update deployment scope")
}

func UpdateTargetEnvironmentAssignment(
	ctx context.Context,
	assignment *types.TargetEnvironmentAssignment,
) error {
	return RunTx(ctx, func(txCtx context.Context) error {
		return updateTargetEnvironmentAssignment(txCtx, assignment)
	})
}

func updateTargetEnvironmentAssignment(
	ctx context.Context,
	assignment *types.TargetEnvironmentAssignment,
) error {
	if assignment == nil {
		return apierrors.NewBadRequest("target environment assignment is required")
	}
	if assignment.ID == uuid.Nil || assignment.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("id and organizationId are required")
	}
	current, err := GetTargetEnvironmentAssignment(ctx, assignment.OrganizationID, assignment.ID)
	if err != nil {
		return err
	}
	if assignment.ActiveUntil != nil && !assignment.ActiveUntil.After(current.ActiveFrom) {
		return apierrors.NewBadRequest("activeUntil must be after activeFrom")
	}
	if len(assignment.PolicyConstraints) == 0 {
		assignment.PolicyConstraints = json.RawMessage(`{}`)
	}
	if !json.Valid(assignment.PolicyConstraints) {
		return apierrors.NewBadRequest("policyConstraints must be valid JSON")
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		UPDATE TargetEnvironmentAssignment AS a
		SET active_until = @activeUntil,
		    policy_constraints = @policyConstraints,
		    updated_at = now()
		WHERE a.id = @id
		  AND a.organization_id = @organizationID
		RETURNING `+targetEnvironmentAssignmentOutputExpr,
		pgx.NamedArgs{
			"id":                assignment.ID,
			"organizationID":    assignment.OrganizationID,
			"activeUntil":       assignment.ActiveUntil,
			"policyConstraints": assignment.PolicyConstraints,
		},
	)
	return assignDeploymentRegistryWriteResult(
		assignment,
		rows,
		err,
		"update target environment assignment",
	)
}

func UpdateDeploymentUnit(ctx context.Context, unit *types.DeploymentUnit) error {
	if unit == nil {
		return apierrors.NewBadRequest("deployment unit is required")
	}
	if unit.ID == uuid.Nil {
		return apierrors.NewBadRequest("id is required")
	}
	if err := validateDeploymentUnitForWrite(*unit); err != nil {
		return err
	}
	unit.Name = strings.TrimSpace(unit.Name)
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		UPDATE DeploymentUnit AS u
		SET name = @name,
		    management_state = @managementState,
		    retired_at = @retiredAt,
		    updated_at = now()
		WHERE u.id = @id
		  AND u.organization_id = @organizationID
		RETURNING `+deploymentUnitOutputExpr,
		pgx.NamedArgs{
			"id":              unit.ID,
			"organizationID":  unit.OrganizationID,
			"name":            unit.Name,
			"managementState": unit.ManagementState,
			"retiredAt":       unit.RetiredAt,
		},
	)
	return assignDeploymentRegistryWriteResult(unit, rows, err, "update deployment unit")
}

func UpdateDeploymentUnitSubscriber(
	ctx context.Context,
	subscriber *types.DeploymentUnitSubscriber,
) error {
	if subscriber == nil {
		return apierrors.NewBadRequest("deployment unit subscriber is required")
	}
	if subscriber.ID == uuid.Nil || subscriber.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("id and organizationId are required")
	}
	current, err := GetDeploymentUnitSubscriber(ctx, subscriber.OrganizationID, subscriber.ID)
	if err != nil {
		return err
	}
	if !registryTimePointersEqual(subscriber.RetiredAt, current.RetiredAt) {
		return apierrors.NewConflict(
			"deployment unit subscriber set is immutable; retire the unit and create a new identity",
		)
	}
	*subscriber = *current
	return nil
}

func UpdateComponentDefinition(
	ctx context.Context,
	definition *types.ComponentDefinition,
) error {
	if definition == nil {
		return apierrors.NewBadRequest("component definition is required")
	}
	if definition.ID == uuid.Nil {
		return apierrors.NewBadRequest("id is required")
	}
	if err := validateComponentDefinitionForWrite(*definition); err != nil {
		return err
	}
	definition.Name = strings.TrimSpace(definition.Name)
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		UPDATE ComponentDefinition AS d
		SET name = @name,
		    description = @description,
		    capability_scope = @capabilityScope,
		    management_state = @managementState,
		    retired_at = @retiredAt,
		    updated_at = now()
		WHERE d.id = @id
		  AND d.organization_id = @organizationID
		RETURNING `+componentDefinitionOutputExpr,
		pgx.NamedArgs{
			"id":              definition.ID,
			"organizationID":  definition.OrganizationID,
			"name":            definition.Name,
			"description":     definition.Description,
			"capabilityScope": definition.CapabilityScope,
			"managementState": definition.ManagementState,
			"retiredAt":       definition.RetiredAt,
		},
	)
	return assignDeploymentRegistryWriteResult(
		definition,
		rows,
		err,
		"update component definition",
	)
}

func UpdateComponentAlias(ctx context.Context, alias *types.ComponentAlias) error {
	return RunTx(ctx, func(txCtx context.Context) error {
		return updateComponentAlias(txCtx, alias)
	})
}

func updateComponentAlias(ctx context.Context, alias *types.ComponentAlias) error {
	if alias == nil {
		return apierrors.NewBadRequest("component alias is required")
	}
	if alias.ID == uuid.Nil || alias.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("id and organizationId are required")
	}
	current, err := lockComponentAlias(ctx, alias.OrganizationID, alias.ID)
	if err != nil {
		return err
	}
	if alias.RetiredAt != nil &&
		!registryTimePointersEqual(alias.RetiredAt, current.RetiredAt) {
		used, err := componentAliasHasRenameHistory(
			ctx,
			alias.OrganizationID,
			alias.ID,
		)
		if err != nil {
			return err
		}
		if used {
			return apierrors.ErrConflict
		}
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		UPDATE ComponentAlias AS ca
		SET retired_at = @retiredAt
		WHERE ca.id = @id
		  AND ca.organization_id = @organizationID
		RETURNING `+componentAliasOutputExpr,
		pgx.NamedArgs{
			"id":             alias.ID,
			"organizationID": alias.OrganizationID,
			"retiredAt":      alias.RetiredAt,
		},
	)
	return assignDeploymentRegistryWriteResult(alias, rows, err, "update component alias")
}

func UpdateComponentInstance(ctx context.Context, instance *types.ComponentInstance) error {
	return RunTx(ctx, func(txCtx context.Context) error {
		return updateComponentInstance(txCtx, instance)
	})
}

func updateComponentInstance(ctx context.Context, instance *types.ComponentInstance) error {
	if instance == nil {
		return apierrors.NewBadRequest("component instance is required")
	}
	if instance.ID == uuid.Nil {
		return apierrors.NewBadRequest("id is required")
	}
	if err := validateComponentInstanceForWrite(*instance); err != nil {
		return err
	}
	current, err := lockComponentInstance(ctx, instance.OrganizationID, instance.ID)
	if err != nil {
		return err
	}
	instance.PhysicalName = strings.TrimSpace(instance.PhysicalName)
	instance.RenamedFrom = strings.TrimSpace(instance.RenamedFrom)
	var renameAlias *types.ComponentAlias
	if instance.PhysicalName != current.PhysicalName {
		if instance.RenamedFrom != current.PhysicalName {
			return apierrors.NewBadRequest(
				"a renamed component requires renamedFrom to match its current physical name",
			)
		}
		renameAlias, err = lockActiveComponentAlias(
			ctx,
			instance.OrganizationID,
			current.ComponentDefinitionID,
			current.PhysicalName,
		)
		if err != nil {
			return err
		}
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		UPDATE ComponentInstance AS ci
		SET physical_name = @physicalName,
		    config_namespace = @configNamespace,
		    database_boundary = @databaseBoundary,
		    health_adapter = @healthAdapter,
		    management_state = @managementState,
		    retired_at = @retiredAt,
		    updated_at = now()
		WHERE ci.id = @id
		  AND ci.organization_id = @organizationID
		RETURNING `+componentInstanceOutputExpr,
		pgx.NamedArgs{
			"id":               instance.ID,
			"organizationID":   instance.OrganizationID,
			"physicalName":     instance.PhysicalName,
			"configNamespace":  instance.ConfigNamespace,
			"databaseBoundary": instance.DatabaseBoundary,
			"healthAdapter":    instance.HealthAdapter,
			"managementState":  instance.ManagementState,
			"retiredAt":        instance.RetiredAt,
		},
	)
	if err := assignDeploymentRegistryWriteResult(
		instance,
		rows,
		err,
		"update component instance",
	); err != nil {
		return err
	}
	if renameAlias == nil {
		return nil
	}
	return createComponentInstanceRename(
		ctx,
		instance.OrganizationID,
		instance.ID,
		renameAlias.ID,
		current.PhysicalName,
		instance.PhysicalName,
	)
}

func DeleteDeploymentScope(ctx context.Context, organizationID uuid.UUID, id uuid.UUID) error {
	return deleteDeploymentRegistryEntity(ctx, organizationID, id, "DeploymentScope")
}

func DeleteTargetEnvironmentAssignment(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) error {
	return deleteDeploymentRegistryEntity(ctx, organizationID, id, "TargetEnvironmentAssignment")
}

func DeleteDeploymentUnit(ctx context.Context, organizationID uuid.UUID, id uuid.UUID) error {
	return deleteDeploymentRegistryEntity(ctx, organizationID, id, "DeploymentUnit")
}

func DeleteDeploymentUnitSubscriber(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) error {
	if _, err := GetDeploymentUnitSubscriber(ctx, organizationID, id); err != nil {
		return err
	}
	return apierrors.NewConflict(
		"deployment unit subscriber set is immutable; retire the unit and create a new identity",
	)
}

func DeleteComponentDefinition(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) error {
	return deleteDeploymentRegistryEntity(ctx, organizationID, id, "ComponentDefinition")
}

func DeleteComponentAlias(ctx context.Context, organizationID uuid.UUID, id uuid.UUID) error {
	return RunTx(ctx, func(txCtx context.Context) error {
		if _, err := lockComponentAlias(txCtx, organizationID, id); err != nil {
			return err
		}
		used, err := componentAliasHasRenameHistory(txCtx, organizationID, id)
		if err != nil {
			return err
		}
		if used {
			return apierrors.ErrConflict
		}
		return deleteDeploymentRegistryEntity(txCtx, organizationID, id, "ComponentAlias")
	})
}

func DeleteComponentInstance(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) error {
	return deleteDeploymentRegistryEntity(ctx, organizationID, id, "ComponentInstance")
}

func assignDeploymentRegistryWriteResult[T any](
	target *T,
	rows pgx.Rows,
	err error,
	action string,
) error {
	if err != nil {
		return mapDeploymentRegistryWriteError(action, err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[T])
	if errors.Is(err, pgx.ErrNoRows) {
		return apierrors.ErrNotFound
	}
	if err != nil {
		return mapDeploymentRegistryWriteError(action, err)
	}
	*target = result
	return nil
}

func deleteDeploymentRegistryEntity(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
	table string,
) error {
	if organizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if id == uuid.Nil {
		return apierrors.NewBadRequest("id is required")
	}
	tag, err := internalctx.GetDb(ctx).Exec(ctx,
		"DELETE FROM "+table+
			" WHERE id = @id AND organization_id = @organizationID",
		pgx.NamedArgs{
			"id":             id,
			"organizationID": organizationID,
		},
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation {
			return apierrors.ErrConflict
		}
		return fmt.Errorf("could not delete deployment registry resource: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apierrors.ErrNotFound
	}
	return nil
}

func registryTimePointersEqual(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func GetDeploymentScope(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) (*types.DeploymentScope, error) {
	return getDeploymentRegistryEntity[types.DeploymentScope](
		ctx,
		organizationID,
		id,
		"DeploymentScope",
		"s",
		deploymentScopeOutputExpr,
	)
}

func ListDeploymentScopes(
	ctx context.Context,
	filter types.RegistryListFilter,
) (types.Page[types.DeploymentScope], error) {
	return listDeploymentRegistryEntities(
		ctx,
		filter,
		"DeploymentScope",
		"s",
		deploymentScopeOutputExpr,
		func(value types.DeploymentScope) (time.Time, uuid.UUID) {
			return value.CreatedAt, value.ID
		},
	)
}

func GetTargetEnvironmentAssignment(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) (*types.TargetEnvironmentAssignment, error) {
	return getDeploymentRegistryEntity[types.TargetEnvironmentAssignment](
		ctx,
		organizationID,
		id,
		"TargetEnvironmentAssignment",
		"a",
		targetEnvironmentAssignmentOutputExpr,
	)
}

func ListTargetEnvironmentAssignments(
	ctx context.Context,
	filter types.RegistryListFilter,
) (types.Page[types.TargetEnvironmentAssignment], error) {
	return listDeploymentRegistryEntities(
		ctx,
		filter,
		"TargetEnvironmentAssignment",
		"a",
		targetEnvironmentAssignmentOutputExpr,
		func(value types.TargetEnvironmentAssignment) (time.Time, uuid.UUID) {
			return value.CreatedAt, value.ID
		},
	)
}

func GetDeploymentUnit(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) (*types.DeploymentUnit, error) {
	return getDeploymentRegistryEntity[types.DeploymentUnit](
		ctx,
		organizationID,
		id,
		"DeploymentUnit",
		"u",
		deploymentUnitOutputExpr,
	)
}

func ListDeploymentUnits(
	ctx context.Context,
	filter types.RegistryListFilter,
) (types.Page[types.DeploymentUnit], error) {
	return listDeploymentRegistryEntities(
		ctx,
		filter,
		"DeploymentUnit",
		"u",
		deploymentUnitOutputExpr,
		func(value types.DeploymentUnit) (time.Time, uuid.UUID) {
			return value.CreatedAt, value.ID
		},
	)
}

func GetDeploymentUnitSubscriber(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) (*types.DeploymentUnitSubscriber, error) {
	return getDeploymentRegistryEntity[types.DeploymentUnitSubscriber](
		ctx,
		organizationID,
		id,
		"DeploymentUnitSubscriber",
		"us",
		deploymentUnitSubscriberOutputExpr,
	)
}

func ListDeploymentUnitSubscribers(
	ctx context.Context,
	filter types.RegistryListFilter,
) (types.Page[types.DeploymentUnitSubscriber], error) {
	return listDeploymentRegistryEntities(
		ctx,
		filter,
		"DeploymentUnitSubscriber",
		"us",
		deploymentUnitSubscriberOutputExpr,
		func(value types.DeploymentUnitSubscriber) (time.Time, uuid.UUID) {
			return value.CreatedAt, value.ID
		},
	)
}

func GetComponentDefinition(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) (*types.ComponentDefinition, error) {
	return getDeploymentRegistryEntity[types.ComponentDefinition](
		ctx,
		organizationID,
		id,
		"ComponentDefinition",
		"d",
		componentDefinitionOutputExpr,
	)
}

func ListComponentDefinitions(
	ctx context.Context,
	filter types.RegistryListFilter,
) (types.Page[types.ComponentDefinition], error) {
	return listDeploymentRegistryEntities(
		ctx,
		filter,
		"ComponentDefinition",
		"d",
		componentDefinitionOutputExpr,
		func(value types.ComponentDefinition) (time.Time, uuid.UUID) {
			return value.CreatedAt, value.ID
		},
	)
}

func GetComponentAlias(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) (*types.ComponentAlias, error) {
	return getDeploymentRegistryEntity[types.ComponentAlias](
		ctx,
		organizationID,
		id,
		"ComponentAlias",
		"ca",
		componentAliasOutputExpr,
	)
}

func ListComponentAliases(
	ctx context.Context,
	filter types.RegistryListFilter,
) (types.Page[types.ComponentAlias], error) {
	return listDeploymentRegistryEntities(
		ctx,
		filter,
		"ComponentAlias",
		"ca",
		componentAliasOutputExpr,
		func(value types.ComponentAlias) (time.Time, uuid.UUID) {
			return value.CreatedAt, value.ID
		},
	)
}

func GetComponentInstance(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) (*types.ComponentInstance, error) {
	return getDeploymentRegistryEntity[types.ComponentInstance](
		ctx,
		organizationID,
		id,
		"ComponentInstance",
		"ci",
		componentInstanceOutputExpr,
	)
}

func ListComponentInstances(
	ctx context.Context,
	filter types.RegistryListFilter,
) (types.Page[types.ComponentInstance], error) {
	return listDeploymentRegistryEntities(
		ctx,
		filter,
		"ComponentInstance",
		"ci",
		componentInstanceOutputExpr,
		func(value types.ComponentInstance) (time.Time, uuid.UUID) {
			return value.CreatedAt, value.ID
		},
	)
}

func getDeploymentRegistryEntity[T any](
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
	table string,
	alias string,
	outputExpression string,
) (*T, error) {
	if organizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if id == uuid.Nil {
		return nil, apierrors.NewBadRequest("id is required")
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx,
		"SELECT "+outputExpression+
			" FROM "+table+" "+alias+
			" WHERE "+alias+".id = @id"+
			" AND "+alias+".organization_id = @organizationID",
		pgx.NamedArgs{
			"id":             id,
			"organizationID": organizationID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not get deployment registry resource: %w", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[T])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect deployment registry resource: %w", err)
	}
	return &result, nil
}

func listDeploymentRegistryEntities[T any](
	ctx context.Context,
	filter types.RegistryListFilter,
	table string,
	alias string,
	outputExpression string,
	key func(T) (time.Time, uuid.UUID),
) (types.Page[T], error) {
	page := types.Page[T]{Items: []T{}}
	limit, cursor, err := normalizeDeploymentRegistryListFilter(filter)
	if err != nil {
		return page, err
	}
	var cursorCreatedAt any
	var cursorID any
	if cursor != nil {
		cursorCreatedAt = cursor.CreatedAt
		cursorID = cursor.ID
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx,
		"SELECT "+outputExpression+
			" FROM "+table+" "+alias+
			" WHERE "+alias+".organization_id = @organizationID"+
			" AND ("+
			" @cursorCreatedAt::timestamptz IS NULL"+
			" OR ("+alias+".created_at, "+alias+".id) <"+
			" (@cursorCreatedAt::timestamptz, @cursorID::uuid)"+
			" )"+
			" ORDER BY "+alias+".created_at DESC, "+alias+".id DESC"+
			" LIMIT @fetchLimit",
		pgx.NamedArgs{
			"organizationID":  filter.OrganizationID,
			"cursorCreatedAt": cursorCreatedAt,
			"cursorID":        cursorID,
			"fetchLimit":      limit + 1,
		},
	)
	if err != nil {
		return page, fmt.Errorf("could not list deployment registry resources: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[T])
	if err != nil {
		return page, fmt.Errorf("could not collect deployment registry resources: %w", err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	page.Items = items
	if hasMore && len(items) > 0 {
		createdAt, id := key(items[len(items)-1])
		page.NextCursor, err = encodeRegistryCursor(registryCursor{
			Version:   registryCursorVersion,
			CreatedAt: createdAt,
			ID:        id,
		})
		if err != nil {
			return page, err
		}
	}
	return page, nil
}

func normalizeDeploymentRegistryListFilter(
	filter types.RegistryListFilter,
) (int, *registryCursor, error) {
	if filter.OrganizationID == uuid.Nil {
		return 0, nil, apierrors.NewBadRequest("organizationId is required")
	}
	limit := filter.Limit
	if limit == 0 {
		limit = registryDefaultPageLimit
	}
	if limit < 1 || limit > registryMaximumPageLimit {
		return 0, nil, apierrors.NewBadRequest("limit must be between 1 and 100")
	}
	cursor, err := decodeRegistryCursor(filter.Cursor)
	if err != nil {
		return 0, nil, err
	}
	return limit, cursor, nil
}

func GetDeploymentRegistryPlacement(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentUnitID uuid.UUID,
) (*types.DeploymentRegistryPlacement, error) {
	if organizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if deploymentUnitID == uuid.Nil {
		return nil, apierrors.NewBadRequest("deploymentUnitId is required")
	}
	var placement *types.DeploymentRegistryPlacement
	err := RunReadOnlyTxRR(ctx, func(txCtx context.Context) error {
		var err error
		placement, err = getDeploymentRegistryPlacement(
			txCtx,
			organizationID,
			deploymentUnitID,
		)
		return err
	})
	return placement, err
}

func getDeploymentRegistryPlacement(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentUnitID uuid.UUID,
) (*types.DeploymentRegistryPlacement, error) {
	row := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT `+deploymentScopeOutputExpr+`,
		       `+targetEnvironmentAssignmentOutputExpr+`,
		       `+deploymentUnitOutputExpr+`
		FROM DeploymentUnit u
		JOIN DeploymentScope s
		  ON s.id = u.deployment_scope_id
		 AND s.organization_id = u.organization_id
		JOIN TargetEnvironmentAssignment a
		  ON a.id = u.target_environment_assignment_id
		 AND a.deployment_target_id = u.deployment_target_id
		 AND a.organization_id = u.organization_id
		WHERE u.id = @deploymentUnitID
		  AND u.organization_id = @organizationID`,
		pgx.NamedArgs{
			"organizationID":   organizationID,
			"deploymentUnitID": deploymentUnitID,
		},
	)
	placement := &types.DeploymentRegistryPlacement{EffectiveAt: time.Now().UTC()}
	err := scanDeploymentRegistryPlacementRoot(row, placement)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not read deployment registry placement: %w", err)
	}
	if err := loadDeploymentRegistryPlacementRelations(ctx, organizationID, placement); err != nil {
		return nil, err
	}
	return placement, nil
}

type deploymentRegistryRowScanner interface {
	Scan(destinations ...any) error
}

func scanDeploymentRegistryPlacementRoot(
	row deploymentRegistryRowScanner,
	placement *types.DeploymentRegistryPlacement,
) error {
	return row.Scan(
		&placement.Scope.ID,
		&placement.Scope.CreatedAt,
		&placement.Scope.UpdatedAt,
		&placement.Scope.OrganizationID,
		&placement.Scope.CustomerOrganizationID,
		&placement.Scope.Key,
		&placement.Scope.Name,
		&placement.Scope.Description,
		&placement.Scope.DeliveryModel,
		&placement.Scope.ManagementState,
		&placement.Scope.RetiredAt,
		&placement.Assignment.ID,
		&placement.Assignment.CreatedAt,
		&placement.Assignment.UpdatedAt,
		&placement.Assignment.OrganizationID,
		&placement.Assignment.DeploymentTargetID,
		&placement.Assignment.EnvironmentID,
		&placement.Assignment.ActiveFrom,
		&placement.Assignment.ActiveUntil,
		&placement.Assignment.PolicyConstraints,
		&placement.Unit.ID,
		&placement.Unit.CreatedAt,
		&placement.Unit.UpdatedAt,
		&placement.Unit.OrganizationID,
		&placement.Unit.DeploymentScopeID,
		&placement.Unit.TargetEnvironmentAssignmentID,
		&placement.Unit.DeploymentTargetID,
		&placement.Unit.Key,
		&placement.Unit.Name,
		&placement.Unit.PhysicalIdentity,
		&placement.Unit.ManagementState,
		&placement.Unit.SubscriberSetChecksum,
		&placement.Unit.RetiredAt,
	)
}

func ListDeploymentRegistryPlacements(
	ctx context.Context,
	filter types.RegistryListFilter,
) (types.Page[types.DeploymentRegistryPlacement], error) {
	page := types.Page[types.DeploymentRegistryPlacement]{
		Items: []types.DeploymentRegistryPlacement{},
	}
	if filter.OrganizationID == uuid.Nil {
		return page, apierrors.NewBadRequest("organizationId is required")
	}
	limit := filter.Limit
	if limit == 0 {
		limit = registryDefaultPageLimit
	}
	if limit < 1 || limit > registryMaximumPageLimit {
		return page, apierrors.NewBadRequest("limit must be between 1 and 100")
	}
	cursor, err := decodeRegistryCursor(filter.Cursor)
	if err != nil {
		return page, err
	}

	err = RunReadOnlyTxRR(ctx, func(txCtx context.Context) error {
		var cursorCreatedAt any
		var cursorID any
		if cursor != nil {
			cursorCreatedAt = cursor.CreatedAt
			cursorID = cursor.ID
		}
		rows, err := internalctx.GetDb(txCtx).Query(txCtx, `
			SELECT `+deploymentScopeOutputExpr+`,
			       `+targetEnvironmentAssignmentOutputExpr+`,
			       `+deploymentUnitOutputExpr+`
			FROM DeploymentUnit u
			JOIN DeploymentScope s
			  ON s.id = u.deployment_scope_id
			 AND s.organization_id = u.organization_id
			JOIN TargetEnvironmentAssignment a
			  ON a.id = u.target_environment_assignment_id
			 AND a.deployment_target_id = u.deployment_target_id
			 AND a.organization_id = u.organization_id
			WHERE u.organization_id = @organizationID
			  AND (
			    @cursorCreatedAt::timestamptz IS NULL
			    OR (u.created_at, u.id) <
			       (@cursorCreatedAt::timestamptz, @cursorID::uuid)
			  )
			ORDER BY u.created_at DESC, u.id DESC
			LIMIT @fetchLimit`,
			pgx.NamedArgs{
				"organizationID":  filter.OrganizationID,
				"cursorCreatedAt": cursorCreatedAt,
				"cursorID":        cursorID,
				"fetchLimit":      limit + 1,
			},
		)
		if err != nil {
			return fmt.Errorf("could not list deployment registry placement roots: %w", err)
		}
		placements := make([]types.DeploymentRegistryPlacement, 0, limit+1)
		effectiveAt := time.Now().UTC()
		for rows.Next() {
			placement := types.DeploymentRegistryPlacement{EffectiveAt: effectiveAt}
			if err := scanDeploymentRegistryPlacementRoot(rows, &placement); err != nil {
				rows.Close()
				return fmt.Errorf("could not scan deployment registry placement root: %w", err)
			}
			placements = append(placements, placement)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("could not collect deployment registry placement roots: %w", err)
		}
		hasMore := len(placements) > limit
		if hasMore {
			placements = placements[:limit]
		}
		if err := loadDeploymentRegistryPlacementPageRelations(
			txCtx,
			filter.OrganizationID,
			placements,
		); err != nil {
			return err
		}
		page.Items = placements
		if hasMore && len(placements) > 0 {
			last := placements[len(placements)-1].Unit
			page.NextCursor, err = encodeRegistryCursor(registryCursor{
				Version:   registryCursorVersion,
				CreatedAt: last.CreatedAt,
				ID:        last.ID,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return page, err
}

type deploymentRegistryScopeTarget struct {
	scopeID  uuid.UUID
	targetID uuid.UUID
}

func loadDeploymentRegistryPlacementPageRelations(
	ctx context.Context,
	organizationID uuid.UUID,
	placements []types.DeploymentRegistryPlacement,
) error {
	if len(placements) == 0 {
		return nil
	}
	unitIDs := make([]uuid.UUID, 0, len(placements))
	targetIDs := make([]uuid.UUID, 0, len(placements))
	scopeIDs := make([]uuid.UUID, 0, len(placements))
	scopeTargetIDs := make([]uuid.UUID, 0, len(placements))
	seenUnits := make(map[uuid.UUID]struct{}, len(placements))
	seenTargets := make(map[uuid.UUID]struct{}, len(placements))
	seenScopeTargets := make(map[deploymentRegistryScopeTarget]struct{}, len(placements))
	for _, placement := range placements {
		if _, seen := seenUnits[placement.Unit.ID]; !seen {
			seenUnits[placement.Unit.ID] = struct{}{}
			unitIDs = append(unitIDs, placement.Unit.ID)
		}
		if _, seen := seenTargets[placement.Unit.DeploymentTargetID]; !seen {
			seenTargets[placement.Unit.DeploymentTargetID] = struct{}{}
			targetIDs = append(targetIDs, placement.Unit.DeploymentTargetID)
		}
		pair := deploymentRegistryScopeTarget{
			scopeID:  placement.Unit.DeploymentScopeID,
			targetID: placement.Unit.DeploymentTargetID,
		}
		if _, seen := seenScopeTargets[pair]; !seen {
			seenScopeTargets[pair] = struct{}{}
			scopeIDs = append(scopeIDs, pair.scopeID)
			scopeTargetIDs = append(scopeTargetIDs, pair.targetID)
		}
	}

	database := internalctx.GetDb(ctx)
	assignmentsByTarget := make(map[uuid.UUID][]types.TargetEnvironmentAssignment)
	rows, err := database.Query(ctx, `
		SELECT `+targetEnvironmentAssignmentOutputExpr+`
		FROM TargetEnvironmentAssignment a
		WHERE a.organization_id = @organizationID
		  AND a.deployment_target_id = ANY(@deploymentTargetIDs)
		ORDER BY a.deployment_target_id, a.active_from, a.id`,
		pgx.NamedArgs{
			"organizationID":      organizationID,
			"deploymentTargetIDs": targetIDs,
		},
	)
	if err != nil {
		return fmt.Errorf("could not batch list target environment assignments: %w", err)
	}
	assignments, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.TargetEnvironmentAssignment],
	)
	if err != nil {
		return fmt.Errorf("could not batch collect target environment assignments: %w", err)
	}
	for _, assignment := range assignments {
		assignmentsByTarget[assignment.DeploymentTargetID] = append(
			assignmentsByTarget[assignment.DeploymentTargetID],
			assignment,
		)
	}

	unitsByScopeTarget := make(map[deploymentRegistryScopeTarget][]types.DeploymentUnit)
	rows, err = database.Query(ctx, `
		SELECT `+deploymentUnitOutputExpr+`
		FROM DeploymentUnit u
		WHERE u.organization_id = @organizationID
		  AND (u.deployment_scope_id, u.deployment_target_id) IN (
		    SELECT requested.scope_id, requested.target_id
		    FROM unnest(
		      @deploymentScopeIDs::uuid[],
		      @scopeDeploymentTargetIDs::uuid[]
		    ) AS requested(scope_id, target_id)
		  )
		ORDER BY
		  u.deployment_scope_id,
		  u.deployment_target_id,
		  u.created_at,
		  u.id`,
		pgx.NamedArgs{
			"organizationID":           organizationID,
			"deploymentScopeIDs":       scopeIDs,
			"scopeDeploymentTargetIDs": scopeTargetIDs,
		},
	)
	if err != nil {
		return fmt.Errorf("could not batch list deployment units: %w", err)
	}
	units, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentUnit])
	if err != nil {
		return fmt.Errorf("could not batch collect deployment units: %w", err)
	}
	for _, unit := range units {
		pair := deploymentRegistryScopeTarget{
			scopeID:  unit.DeploymentScopeID,
			targetID: unit.DeploymentTargetID,
		}
		unitsByScopeTarget[pair] = append(unitsByScopeTarget[pair], unit)
	}

	subscribersByUnit := make(map[uuid.UUID][]types.DeploymentUnitSubscriber)
	rows, err = database.Query(ctx, `
		SELECT `+deploymentUnitSubscriberOutputExpr+`
		FROM DeploymentUnitSubscriber us
		WHERE us.organization_id = @organizationID
		  AND us.deployment_unit_id = ANY(@deploymentUnitIDs)
		ORDER BY
		  us.deployment_unit_id,
		  us.customer_organization_id,
		  us.id`,
		pgx.NamedArgs{
			"organizationID":    organizationID,
			"deploymentUnitIDs": unitIDs,
		},
	)
	if err != nil {
		return fmt.Errorf("could not batch list deployment unit subscribers: %w", err)
	}
	subscribers, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.DeploymentUnitSubscriber],
	)
	if err != nil {
		return fmt.Errorf("could not batch collect deployment unit subscribers: %w", err)
	}
	for _, subscriber := range subscribers {
		subscribersByUnit[subscriber.DeploymentUnitID] = append(
			subscribersByUnit[subscriber.DeploymentUnitID],
			subscriber,
		)
	}

	instancesByUnit := make(map[uuid.UUID][]types.ComponentInstance)
	rows, err = database.Query(ctx, `
		SELECT `+componentInstanceOutputExpr+`
		FROM ComponentInstance ci
		WHERE ci.organization_id = @organizationID
		  AND ci.deployment_unit_id = ANY(@deploymentUnitIDs)
		ORDER BY ci.deployment_unit_id, ci.physical_name, ci.id`,
		pgx.NamedArgs{
			"organizationID":    organizationID,
			"deploymentUnitIDs": unitIDs,
		},
	)
	if err != nil {
		return fmt.Errorf("could not batch list component instances: %w", err)
	}
	instances, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.ComponentInstance],
	)
	if err != nil {
		return fmt.Errorf("could not batch collect component instances: %w", err)
	}
	for _, instance := range instances {
		instancesByUnit[instance.DeploymentUnitID] = append(
			instancesByUnit[instance.DeploymentUnitID],
			instance,
		)
	}

	definitionsByUnit := make(map[uuid.UUID][]types.ComponentDefinition)
	rows, err = database.Query(ctx, `
		SELECT
		  used.deployment_unit_id,
		  `+componentDefinitionOutputExpr+`
		FROM (
		  SELECT DISTINCT
		    ci.deployment_unit_id,
		    ci.component_definition_id
		  FROM ComponentInstance ci
		  WHERE ci.organization_id = @organizationID
		    AND ci.deployment_unit_id = ANY(@deploymentUnitIDs)
		) used
		JOIN ComponentDefinition d
		  ON d.id = used.component_definition_id
		 AND d.organization_id = @organizationID
		ORDER BY used.deployment_unit_id, d.key, d.id`,
		pgx.NamedArgs{
			"organizationID":    organizationID,
			"deploymentUnitIDs": unitIDs,
		},
	)
	if err != nil {
		return fmt.Errorf("could not batch list component definitions: %w", err)
	}
	for rows.Next() {
		var unitID uuid.UUID
		var definition types.ComponentDefinition
		if err := rows.Scan(
			&unitID,
			&definition.ID,
			&definition.CreatedAt,
			&definition.UpdatedAt,
			&definition.OrganizationID,
			&definition.Key,
			&definition.Name,
			&definition.Description,
			&definition.CapabilityScope,
			&definition.ManagementState,
			&definition.RetiredAt,
		); err != nil {
			rows.Close()
			return fmt.Errorf("could not batch scan component definition: %w", err)
		}
		definitionsByUnit[unitID] = append(definitionsByUnit[unitID], definition)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("could not batch collect component definitions: %w", err)
	}

	aliasesByUnit := make(map[uuid.UUID][]types.ComponentAlias)
	rows, err = database.Query(ctx, `
		SELECT
		  used.deployment_unit_id,
		  `+componentAliasOutputExpr+`
		FROM (
		  SELECT DISTINCT
		    ci.deployment_unit_id,
		    ci.component_definition_id
		  FROM ComponentInstance ci
		  WHERE ci.organization_id = @organizationID
		    AND ci.deployment_unit_id = ANY(@deploymentUnitIDs)
		) used
		JOIN ComponentAlias ca
		  ON ca.component_definition_id = used.component_definition_id
		 AND ca.organization_id = @organizationID
		ORDER BY used.deployment_unit_id, ca.alias, ca.id`,
		pgx.NamedArgs{
			"organizationID":    organizationID,
			"deploymentUnitIDs": unitIDs,
		},
	)
	if err != nil {
		return fmt.Errorf("could not batch list component aliases: %w", err)
	}
	for rows.Next() {
		var unitID uuid.UUID
		var alias types.ComponentAlias
		if err := rows.Scan(
			&unitID,
			&alias.ID,
			&alias.CreatedAt,
			&alias.OrganizationID,
			&alias.ComponentDefinitionID,
			&alias.Alias,
			&alias.RetiredAt,
		); err != nil {
			rows.Close()
			return fmt.Errorf("could not batch scan component alias: %w", err)
		}
		aliasesByUnit[unitID] = append(aliasesByUnit[unitID], alias)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("could not batch collect component aliases: %w", err)
	}

	for index := range placements {
		placement := &placements[index]
		pair := deploymentRegistryScopeTarget{
			scopeID:  placement.Unit.DeploymentScopeID,
			targetID: placement.Unit.DeploymentTargetID,
		}
		placement.Assignments = append(
			[]types.TargetEnvironmentAssignment{},
			assignmentsByTarget[placement.Unit.DeploymentTargetID]...,
		)
		placement.Units = append(
			[]types.DeploymentUnit{},
			unitsByScopeTarget[pair]...,
		)
		placement.Subscribers = append(
			[]types.DeploymentUnitSubscriber{},
			subscribersByUnit[placement.Unit.ID]...,
		)
		placement.Instances = append(
			[]types.ComponentInstance{},
			instancesByUnit[placement.Unit.ID]...,
		)
		placement.Definitions = append(
			[]types.ComponentDefinition{},
			definitionsByUnit[placement.Unit.ID]...,
		)
		placement.Aliases = append(
			[]types.ComponentAlias{},
			aliasesByUnit[placement.Unit.ID]...,
		)
	}
	return nil
}

func loadDeploymentRegistryPlacementRelations(
	ctx context.Context,
	organizationID uuid.UUID,
	placement *types.DeploymentRegistryPlacement,
) error {
	assignments, err := listTargetEnvironmentAssignmentsForTarget(
		ctx,
		organizationID,
		placement.Unit.DeploymentTargetID,
	)
	if err != nil {
		return err
	}
	placement.Assignments = assignments
	units, err := listDeploymentUnitsForScopeTarget(
		ctx,
		organizationID,
		placement.Unit.DeploymentScopeID,
		placement.Unit.DeploymentTargetID,
	)
	if err != nil {
		return err
	}
	placement.Units = units
	placement.Subscribers, err = getDeploymentUnitSubscribers(
		ctx,
		organizationID,
		placement.Unit.ID,
	)
	if err != nil {
		return err
	}
	placement.Instances, err = listComponentInstancesForUnit(
		ctx,
		organizationID,
		placement.Unit.ID,
	)
	if err != nil {
		return err
	}
	placement.Definitions, err = listComponentDefinitionsForUnit(
		ctx,
		organizationID,
		placement.Unit.ID,
	)
	if err != nil {
		return err
	}
	placement.Aliases, err = listComponentAliasesForUnit(
		ctx,
		organizationID,
		placement.Unit.ID,
	)
	return err
}

func listTargetEnvironmentAssignmentsForTarget(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentTargetID uuid.UUID,
) ([]types.TargetEnvironmentAssignment, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+targetEnvironmentAssignmentOutputExpr+`
		FROM TargetEnvironmentAssignment a
		WHERE a.organization_id = @organizationID
		  AND a.deployment_target_id = @deploymentTargetID
		ORDER BY a.active_from, a.id`,
		pgx.NamedArgs{
			"organizationID":     organizationID,
			"deploymentTargetID": deploymentTargetID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list target environment assignments: %w", err)
	}
	result, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.TargetEnvironmentAssignment],
	)
	if err != nil {
		return nil, fmt.Errorf("could not collect target environment assignments: %w", err)
	}
	return result, nil
}

func listDeploymentUnitsForScopeTarget(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentScopeID uuid.UUID,
	deploymentTargetID uuid.UUID,
) ([]types.DeploymentUnit, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentUnitOutputExpr+`
		FROM DeploymentUnit u
		WHERE u.organization_id = @organizationID
		  AND u.deployment_scope_id = @deploymentScopeID
		  AND u.deployment_target_id = @deploymentTargetID
		ORDER BY u.created_at, u.id`,
		pgx.NamedArgs{
			"organizationID":     organizationID,
			"deploymentScopeID":  deploymentScopeID,
			"deploymentTargetID": deploymentTargetID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list deployment units: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.DeploymentUnit])
	if err != nil {
		return nil, fmt.Errorf("could not collect deployment units: %w", err)
	}
	return result, nil
}

func getDeploymentUnitSubscribers(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentUnitID uuid.UUID,
) ([]types.DeploymentUnitSubscriber, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+deploymentUnitSubscriberOutputExpr+`
		FROM DeploymentUnitSubscriber us
		WHERE us.organization_id = @organizationID
		  AND us.deployment_unit_id = @deploymentUnitID
		ORDER BY us.customer_organization_id, us.id`,
		pgx.NamedArgs{
			"organizationID":   organizationID,
			"deploymentUnitID": deploymentUnitID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list deployment unit subscribers: %w", err)
	}
	result, err := pgx.CollectRows(
		rows,
		pgx.RowToStructByName[types.DeploymentUnitSubscriber],
	)
	if err != nil {
		return nil, fmt.Errorf("could not collect deployment unit subscribers: %w", err)
	}
	return result, nil
}

func listComponentInstancesForUnit(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentUnitID uuid.UUID,
) ([]types.ComponentInstance, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+componentInstanceOutputExpr+`
		FROM ComponentInstance ci
		WHERE ci.organization_id = @organizationID
		  AND ci.deployment_unit_id = @deploymentUnitID
		ORDER BY ci.physical_name, ci.id`,
		pgx.NamedArgs{
			"organizationID":   organizationID,
			"deploymentUnitID": deploymentUnitID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list component instances: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ComponentInstance])
	if err != nil {
		return nil, fmt.Errorf("could not collect component instances: %w", err)
	}
	return result, nil
}

func listComponentDefinitionsForUnit(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentUnitID uuid.UUID,
) ([]types.ComponentDefinition, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+componentDefinitionOutputExpr+`
		FROM ComponentDefinition d
		WHERE d.organization_id = @organizationID
		  AND EXISTS (
		    SELECT 1
		    FROM ComponentInstance ci
		    WHERE ci.organization_id = d.organization_id
		      AND ci.component_definition_id = d.id
		      AND ci.deployment_unit_id = @deploymentUnitID
		  )
		ORDER BY d.key, d.id`,
		pgx.NamedArgs{
			"organizationID":   organizationID,
			"deploymentUnitID": deploymentUnitID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list component definitions: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ComponentDefinition])
	if err != nil {
		return nil, fmt.Errorf("could not collect component definitions: %w", err)
	}
	return result, nil
}

func listComponentAliasesForUnit(
	ctx context.Context,
	organizationID uuid.UUID,
	deploymentUnitID uuid.UUID,
) ([]types.ComponentAlias, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+componentAliasOutputExpr+`
		FROM ComponentAlias ca
		WHERE ca.organization_id = @organizationID
		  AND EXISTS (
		    SELECT 1
		    FROM ComponentInstance ci
		    WHERE ci.organization_id = ca.organization_id
		      AND ci.component_definition_id = ca.component_definition_id
		      AND ci.deployment_unit_id = @deploymentUnitID
		  )
		ORDER BY ca.alias, ca.id`,
		pgx.NamedArgs{
			"organizationID":   organizationID,
			"deploymentUnitID": deploymentUnitID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not list component aliases: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.ComponentAlias])
	if err != nil {
		return nil, fmt.Errorf("could not collect component aliases: %w", err)
	}
	return result, nil
}

func lockComponentInstance(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) (*types.ComponentInstance, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+componentInstanceOutputExpr+`
		FROM ComponentInstance ci
		WHERE ci.organization_id = @organizationID
		  AND ci.id = @id
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"id":             id,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not lock component instance: %w", err)
	}
	result, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ComponentInstance],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect locked component instance: %w", err)
	}
	return &result, nil
}

func lockComponentAlias(
	ctx context.Context,
	organizationID uuid.UUID,
	id uuid.UUID,
) (*types.ComponentAlias, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+componentAliasOutputExpr+`
		FROM ComponentAlias ca
		WHERE ca.organization_id = @organizationID
		  AND ca.id = @id
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationID": organizationID,
			"id":             id,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not lock component alias: %w", err)
	}
	result, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ComponentAlias],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect locked component alias: %w", err)
	}
	return &result, nil
}

func lockActiveComponentAlias(
	ctx context.Context,
	organizationID uuid.UUID,
	componentDefinitionID uuid.UUID,
	alias string,
) (*types.ComponentAlias, error) {
	rows, err := internalctx.GetDb(ctx).Query(ctx, `
		SELECT `+componentAliasOutputExpr+`
		FROM ComponentAlias ca
		WHERE ca.organization_id = @organizationID
		  AND ca.component_definition_id = @componentDefinitionID
		  AND ca.alias = lower(@alias)
		  AND ca.retired_at IS NULL
		FOR UPDATE`,
		pgx.NamedArgs{
			"organizationID":        organizationID,
			"componentDefinitionID": componentDefinitionID,
			"alias":                 strings.TrimSpace(alias),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not lock component alias for rename: %w", err)
	}
	result, err := pgx.CollectExactlyOneRow(
		rows,
		pgx.RowToStructByName[types.ComponentAlias],
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.NewBadRequest(
			"a renamed component requires an active alias or explicit retirement and new instance",
		)
	}
	if err != nil {
		return nil, fmt.Errorf("could not collect component alias for rename: %w", err)
	}
	return &result, nil
}

func componentAliasHasRenameHistory(
	ctx context.Context,
	organizationID uuid.UUID,
	componentAliasID uuid.UUID,
) (bool, error) {
	var used bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM ComponentInstanceRename history
		  WHERE history.organization_id = @organizationID
		    AND history.component_alias_id = @componentAliasID
		)`,
		pgx.NamedArgs{
			"organizationID":   organizationID,
			"componentAliasID": componentAliasID,
		},
	).Scan(&used)
	if err != nil {
		return false, fmt.Errorf("could not inspect component alias rename history: %w", err)
	}
	return used, nil
}

func createComponentInstanceRename(
	ctx context.Context,
	organizationID uuid.UUID,
	componentInstanceID uuid.UUID,
	componentAliasID uuid.UUID,
	fromPhysicalName string,
	toPhysicalName string,
) error {
	_, err := internalctx.GetDb(ctx).Exec(ctx, `
		INSERT INTO ComponentInstanceRename (
		  organization_id,
		  component_instance_id,
		  component_alias_id,
		  from_physical_name,
		  to_physical_name
		) VALUES (
		  @organizationID,
		  @componentInstanceID,
		  @componentAliasID,
		  @fromPhysicalName,
		  @toPhysicalName
		)`,
		pgx.NamedArgs{
			"organizationID":      organizationID,
			"componentInstanceID": componentInstanceID,
			"componentAliasID":    componentAliasID,
			"fromPhysicalName":    fromPhysicalName,
			"toPhysicalName":      toPhysicalName,
		},
	)
	if err != nil {
		return mapDeploymentRegistryWriteError("record component instance rename", err)
	}
	return nil
}

func ensureRegistryCustomerExists(
	ctx context.Context,
	organizationID uuid.UUID,
	customerOrganizationID uuid.UUID,
) error {
	var exists bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM CustomerOrganization
		  WHERE id = @customerOrganizationID
		    AND organization_id = @organizationID
		)`,
		pgx.NamedArgs{
			"organizationID":         organizationID,
			"customerOrganizationID": customerOrganizationID,
		},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate registry customer organization: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func validateDeploymentScopeForWrite(scope types.DeploymentScope) error {
	if scope.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if strings.TrimSpace(scope.Key) == "" {
		return apierrors.NewBadRequest("key is required")
	}
	if strings.TrimSpace(scope.Name) == "" {
		return apierrors.NewBadRequest("name is required")
	}
	if !scope.DeliveryModel.IsValid() {
		return apierrors.NewBadRequest("deliveryModel must be dedicated, shared, or external")
	}
	if !scope.ManagementState.IsValid() {
		return apierrors.NewBadRequest("managementState is invalid")
	}
	if scope.DeliveryModel == types.DeliveryModelDedicated &&
		scope.CustomerOrganizationID == nil {
		return apierrors.NewBadRequest("dedicated deployment scope requires customerOrganizationId")
	}
	if scope.DeliveryModel != types.DeliveryModelDedicated &&
		scope.CustomerOrganizationID != nil {
		return apierrors.NewBadRequest("shared and external deployment scopes cannot select a customer organization")
	}
	if (scope.ManagementState == types.RegistryManagementStateRetired) !=
		(scope.RetiredAt != nil) {
		return apierrors.NewBadRequest("retired management state and retiredAt must be set together")
	}
	return nil
}

func validateDeploymentUnitForWrite(unit types.DeploymentUnit) error {
	if unit.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if unit.DeploymentScopeID == uuid.Nil {
		return apierrors.NewBadRequest("deploymentScopeId is required")
	}
	if unit.TargetEnvironmentAssignmentID == uuid.Nil {
		return apierrors.NewBadRequest("targetEnvironmentAssignmentId is required")
	}
	if unit.DeploymentTargetID == uuid.Nil {
		return apierrors.NewBadRequest("deploymentTargetId is required")
	}
	if strings.TrimSpace(unit.Key) == "" {
		return apierrors.NewBadRequest("key is required")
	}
	if strings.TrimSpace(unit.Name) == "" {
		return apierrors.NewBadRequest("name is required")
	}
	if strings.TrimSpace(unit.PhysicalIdentity) == "" {
		return apierrors.NewBadRequest("physicalIdentity is required")
	}
	if !unit.ManagementState.IsValid() {
		return apierrors.NewBadRequest("managementState is invalid")
	}
	if !validSHA256Checksum(unit.SubscriberSetChecksum) {
		return apierrors.NewBadRequest("subscriberSetChecksum must be a sha256 checksum")
	}
	if (unit.ManagementState == types.RegistryManagementStateRetired) !=
		(unit.RetiredAt != nil) {
		return apierrors.NewBadRequest("retired management state and retiredAt must be set together")
	}
	return nil
}

func validateComponentDefinitionForWrite(definition types.ComponentDefinition) error {
	if definition.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if strings.TrimSpace(definition.Key) == "" {
		return apierrors.NewBadRequest("key is required")
	}
	if strings.TrimSpace(definition.Name) == "" {
		return apierrors.NewBadRequest("name is required")
	}
	if !definition.ManagementState.IsValid() {
		return apierrors.NewBadRequest("managementState is invalid")
	}
	if (definition.ManagementState == types.RegistryManagementStateRetired) !=
		(definition.RetiredAt != nil) {
		return apierrors.NewBadRequest("retired management state and retiredAt must be set together")
	}
	return nil
}

func validateComponentInstanceForWrite(instance types.ComponentInstance) error {
	if instance.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if instance.DeploymentUnitID == uuid.Nil {
		return apierrors.NewBadRequest("deploymentUnitId is required")
	}
	if instance.ComponentDefinitionID == uuid.Nil {
		return apierrors.NewBadRequest("componentDefinitionId is required")
	}
	if strings.TrimSpace(instance.PhysicalName) == "" {
		return apierrors.NewBadRequest("physicalName is required")
	}
	if !instance.ManagementState.IsValid() {
		return apierrors.NewBadRequest("managementState is invalid")
	}
	if (instance.ManagementState == types.RegistryManagementStateRetired) !=
		(instance.RetiredAt != nil) {
		return apierrors.NewBadRequest("retired management state and retiredAt must be set together")
	}
	return nil
}

func validSHA256Checksum(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range strings.TrimPrefix(value, "sha256:") {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func mapDeploymentRegistryWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		if pgError.ConstraintName == "componentalias_rename_history_guard" {
			return apierrors.ErrConflict
		}
		switch pgError.Code {
		case pgerrcode.ForeignKeyViolation:
			return apierrors.ErrNotFound
		case pgerrcode.UniqueViolation, pgerrcode.ExclusionViolation:
			return apierrors.ErrAlreadyExists
		case pgerrcode.CheckViolation, pgerrcode.NotNullViolation:
			return apierrors.NewBadRequest("deployment registry value violates its contract")
		}
	}
	return fmt.Errorf("could not %s: %w", action, err)
}

func encodeRegistryCursor(cursor registryCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("could not encode deployment registry cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeRegistryCursor(value string) (*registryCursor, error) {
	if value == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor registryCursor
	if err := decoder.Decode(&cursor); err != nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	if err := ensureRegistryCursorEOF(decoder); err != nil {
		return nil, err
	}
	if cursor.Version != registryCursorVersion ||
		cursor.CreatedAt.IsZero() ||
		cursor.ID == uuid.Nil {
		return nil, apierrors.NewBadRequest("cursor is invalid")
	}
	return &cursor, nil
}

func ensureRegistryCursorEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return apierrors.NewBadRequest("cursor is invalid")
	}
	return nil
}
