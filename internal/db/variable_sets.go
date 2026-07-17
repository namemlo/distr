package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/variableresolution"
	"github.com/distr-sh/distr/internal/variablescope"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	variableSetOutputExpr = `
	vs.id,
	vs.created_at,
	vs.updated_at,
	vs.organization_id,
	vs.name,
	vs.description,
	vs.sort_order
`

	variableOutputExpr = `
	v.id,
	v.created_at,
	v.updated_at,
	v.organization_id,
	v.variable_set_id,
	v.key,
	v.description,
	v.type,
	v.is_required,
	v.default_value,
	CASE
		WHEN v.secret_reference_id IS NOT NULL THEN v.secret_reference_id::text
		ELSE COALESCE(v.reference_id, '')
	END AS reference_id,
	COALESCE(s.key, v.reference_name, '') AS reference_name
`
)

func CreateVariableSet(ctx context.Context, variableSet *types.VariableSet) error {
	return RunTx(ctx, func(ctx context.Context) error {
		if err := normalizeVariableSet(variableSet); err != nil {
			return err
		}
		if err := ensureVariableSetApplications(ctx, variableSet.OrganizationID, variableSet.ApplicationIDs); err != nil {
			return err
		}
		if err := ensureVariableReferences(ctx, variableSet.OrganizationID, variableSet.Variables); err != nil {
			return err
		}
		if err := ensureVariableScopedValueReferences(ctx, variableSet.OrganizationID, variableSet.Variables); err != nil {
			return err
		}

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`INSERT INTO VariableSet AS vs (
				organization_id,
				name,
				description,
				sort_order
			) VALUES (
				@organizationId,
				@name,
				@description,
				@sortOrder
			) RETURNING `+variableSetOutputExpr,
			pgx.NamedArgs{
				"organizationId": variableSet.OrganizationID,
				"name":           variableSet.Name,
				"description":    variableSet.Description,
				"sortOrder":      variableSet.SortOrder,
			},
		)
		if err != nil {
			return mapVariableSetWriteError("insert", err)
		}
		result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.VariableSet])
		if err != nil {
			return mapVariableSetWriteError("scan created", err)
		}
		result.ApplicationIDs = variableSet.ApplicationIDs
		result.Variables = variableSet.Variables
		*variableSet = result

		if err := insertVariableSetApplications(
			ctx,
			variableSet.ID,
			variableSet.OrganizationID,
			variableSet.ApplicationIDs,
		); err != nil {
			return err
		}
		if err := insertVariables(ctx, variableSet.ID, variableSet.OrganizationID, variableSet.Variables); err != nil {
			return err
		}
		loaded, err := getVariableSet(ctx, variableSet.ID, variableSet.OrganizationID, false)
		if err != nil {
			return err
		}
		*variableSet = *loaded
		return nil
	})
}

func GetVariableSetsByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]types.VariableSet, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+variableSetOutputExpr+`
		FROM VariableSet vs
		WHERE vs.organization_id = @organizationId
		ORDER BY vs.sort_order, vs.name, vs.id`,
		pgx.NamedArgs{"organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query VariableSet: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.VariableSet])
	if err != nil {
		return nil, fmt.Errorf("could not collect VariableSet: %w", err)
	}
	for i := range result {
		if err := hydrateVariableSet(ctx, &result[i]); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func GetVariableSet(ctx context.Context, id, orgID uuid.UUID) (*types.VariableSet, error) {
	return getVariableSet(ctx, id, orgID, false)
}

func getVariableSet(ctx context.Context, id, orgID uuid.UUID, forUpdate bool) (*types.VariableSet, error) {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE"
	}

	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+variableSetOutputExpr+`
		FROM VariableSet vs
		WHERE vs.id = @id AND vs.organization_id = @organizationId`+lockClause,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query VariableSet: %w", err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.VariableSet])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect VariableSet: %w", err)
	}
	if err := hydrateVariableSet(ctx, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func UpdateVariableSet(ctx context.Context, variableSet *types.VariableSet) error {
	return RunTx(ctx, func(ctx context.Context) error {
		if _, err := getVariableSet(ctx, variableSet.ID, variableSet.OrganizationID, true); err != nil {
			return err
		}
		if err := EnsureConfigAsCodeDatabaseManagedForUpdate(
			ctx,
			variableSet.OrganizationID,
			types.ConfigAsCodeResourceKindVariableSetDefinition,
			variableSet.ID,
		); err != nil {
			return err
		}
		if err := normalizeVariableSet(variableSet); err != nil {
			return err
		}
		if err := ensureVariableSetApplications(ctx, variableSet.OrganizationID, variableSet.ApplicationIDs); err != nil {
			return err
		}
		if err := ensureVariableReferences(ctx, variableSet.OrganizationID, variableSet.Variables); err != nil {
			return err
		}
		if err := ensureVariableScopedValueReferences(ctx, variableSet.OrganizationID, variableSet.Variables); err != nil {
			return err
		}
		applicationIDs := variableSet.ApplicationIDs
		variables := variableSet.Variables

		db := internalctx.GetDb(ctx)
		rows, err := db.Query(ctx,
			`UPDATE VariableSet AS vs SET
				name = @name,
				description = @description,
				sort_order = @sortOrder,
				updated_at = now()
			WHERE vs.id = @id AND vs.organization_id = @organizationId
			RETURNING `+variableSetOutputExpr,
			pgx.NamedArgs{
				"id":             variableSet.ID,
				"organizationId": variableSet.OrganizationID,
				"name":           variableSet.Name,
				"description":    variableSet.Description,
				"sortOrder":      variableSet.SortOrder,
			},
		)
		if err != nil {
			return mapVariableSetWriteError("update", err)
		}
		result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.VariableSet])
		if errors.Is(err, pgx.ErrNoRows) {
			return apierrors.ErrNotFound
		} else if err != nil {
			return mapVariableSetWriteError("scan updated", err)
		}
		*variableSet = result

		if err := replaceVariableSetChildren(ctx, variableSet.ID, variableSet.OrganizationID); err != nil {
			return err
		}
		if err := insertVariableSetApplications(ctx, variableSet.ID, variableSet.OrganizationID, applicationIDs); err != nil {
			return err
		}
		if err := insertVariables(ctx, variableSet.ID, variableSet.OrganizationID, variables); err != nil {
			return err
		}
		loaded, err := getVariableSet(ctx, variableSet.ID, variableSet.OrganizationID, false)
		if err != nil {
			return err
		}
		*variableSet = *loaded
		return nil
	})
}

func DeleteVariableSetWithID(ctx context.Context, id, orgID uuid.UUID) error {
	return RunTx(ctx, func(ctx context.Context) error {
		if err := EnsureConfigAsCodeDatabaseManagedForUpdate(
			ctx,
			orgID,
			types.ConfigAsCodeResourceKindVariableSetDefinition,
			id,
		); err != nil {
			return err
		}
		if err := DeleteConfigAsCodeAuthorityForResource(
			ctx,
			orgID,
			types.ConfigAsCodeResourceKindVariableSetDefinition,
			id,
		); err != nil {
			return err
		}
		db := internalctx.GetDb(ctx)
		cmd, err := db.Exec(ctx,
			`DELETE FROM VariableSet WHERE id = @id AND organization_id = @organizationId`,
			pgx.NamedArgs{"id": id, "organizationId": orgID},
		)
		if err != nil {
			if isProtectedReferenceViolation(err) {
				return fmt.Errorf("could not delete VariableSet: %w", apierrors.ErrConflict)
			}
			return fmt.Errorf("could not delete VariableSet: %w", err)
		}
		if cmd.RowsAffected() == 0 {
			return apierrors.ErrNotFound
		}
		return nil
	})
}

func ResolveVariablesPreview(
	ctx context.Context,
	orgID uuid.UUID,
	variableSetIDs []uuid.UUID,
	scope types.VariableResolutionScope,
	promptedValues []types.VariablePromptedValue,
) ([]types.ResolvedVariable, error) {
	if len(variableSetIDs) == 0 {
		return nil, apierrors.ErrBadRequest
	}
	seen := map[uuid.UUID]struct{}{}
	var variables []types.Variable
	for _, variableSetID := range variableSetIDs {
		if variableSetID == uuid.Nil {
			return nil, apierrors.ErrBadRequest
		}
		if _, ok := seen[variableSetID]; ok {
			return nil, apierrors.ErrBadRequest
		}
		seen[variableSetID] = struct{}{}

		variableSet, err := GetVariableSet(ctx, variableSetID, orgID)
		if err != nil {
			return nil, err
		}
		variables = append(variables, variableSet.Variables...)
	}
	preparedPromptedValues, err := preparePromptedValues(ctx, orgID, variables, promptedValues)
	if err != nil {
		return nil, err
	}
	return variableresolution.Resolve(variableresolution.Request{
		Variables:      variables,
		Scope:          scope,
		PromptedValues: preparedPromptedValues,
	})
}

func hydrateVariableSet(ctx context.Context, variableSet *types.VariableSet) error {
	applicationIDs, err := getVariableSetApplicationIDs(ctx, variableSet.ID)
	if err != nil {
		return err
	}
	variables, err := getVariablesForSet(ctx, variableSet.ID)
	if err != nil {
		return err
	}
	variableSet.ApplicationIDs = applicationIDs
	variableSet.Variables = variables
	return nil
}

func getVariableSetApplicationIDs(ctx context.Context, variableSetID uuid.UUID) ([]uuid.UUID, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT application_id
		FROM VariableSetApplication
		WHERE variable_set_id = @variableSetId
		ORDER BY sort_order, application_id`,
		pgx.NamedArgs{"variableSetId": variableSetID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query VariableSetApplication: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("could not collect VariableSetApplication: %w", err)
	}
	return result, nil
}

func getVariablesForSet(ctx context.Context, variableSetID uuid.UUID) ([]types.Variable, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+variableOutputExpr+`
		FROM Variable v
		LEFT JOIN Secret s ON s.id = v.secret_reference_id
		WHERE v.variable_set_id = @variableSetId
		ORDER BY v.key, v.id`,
		pgx.NamedArgs{"variableSetId": variableSetID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query Variable: %w", err)
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.Variable])
	if err != nil {
		return nil, fmt.Errorf("could not collect Variable: %w", err)
	}
	for i := range result {
		if !hasVariableJSONValue(result[i].DefaultValue) {
			result[i].DefaultValue = nil
		}
		scopedValues, err := getScopedValuesForVariable(ctx, result[i].ID)
		if err != nil {
			return nil, err
		}
		result[i].ScopedValues = scopedValues
	}
	return result, nil
}

func getScopedValuesForVariable(ctx context.Context, variableID uuid.UUID) ([]types.VariableScopedValue, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			vsv.id,
			vsv.created_at,
			vsv.updated_at,
			vsv.organization_id,
			vsv.variable_set_id,
			vsv.variable_id,
			vsv.customer_organization_id,
			vsv.environment_id,
			vsv.channel_id,
			vsv.deployment_target_id,
			vsv.application_id,
			vsv.target_tag,
			vsv.process_step_key,
			vsv.sort_order,
			vsv.value,
			CASE
				WHEN vsv.secret_reference_id IS NOT NULL THEN vsv.secret_reference_id::text
				ELSE COALESCE(vsv.reference_id, '')
			END AS reference_id,
			COALESCE(s.key, vsv.reference_name, '') AS reference_name
		FROM VariableScopedValue vsv
		LEFT JOIN Secret s ON s.id = vsv.secret_reference_id
		WHERE vsv.variable_id = @variableId
		ORDER BY vsv.sort_order, vsv.id`,
		pgx.NamedArgs{"variableId": variableID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query VariableScopedValue: %w", err)
	}
	defer rows.Close()

	var result []types.VariableScopedValue
	for rows.Next() {
		var scopedValue types.VariableScopedValue
		var customerOrganizationID pgtype.UUID
		var environmentID pgtype.UUID
		var channelID pgtype.UUID
		var deploymentTargetID pgtype.UUID
		var applicationID pgtype.UUID
		var value json.RawMessage
		if err := rows.Scan(
			&scopedValue.ID,
			&scopedValue.CreatedAt,
			&scopedValue.UpdatedAt,
			&scopedValue.OrganizationID,
			&scopedValue.VariableSetID,
			&scopedValue.VariableID,
			&customerOrganizationID,
			&environmentID,
			&channelID,
			&deploymentTargetID,
			&applicationID,
			&scopedValue.Scope.TargetTag,
			&scopedValue.Scope.ProcessStepKey,
			&scopedValue.SortOrder,
			&value,
			&scopedValue.ReferenceID,
			&scopedValue.ReferenceName,
		); err != nil {
			return nil, fmt.Errorf("could not scan VariableScopedValue: %w", err)
		}
		scopedValue.Scope.CustomerOrganizationID = uuidFromPG(customerOrganizationID)
		scopedValue.Scope.EnvironmentID = uuidFromPG(environmentID)
		scopedValue.Scope.ChannelID = uuidFromPG(channelID)
		scopedValue.Scope.DeploymentTargetID = uuidFromPG(deploymentTargetID)
		scopedValue.Scope.ApplicationID = uuidFromPG(applicationID)
		if hasVariableJSONValue(value) {
			scopedValue.Value = value
		}
		result = append(result, scopedValue)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not iterate VariableScopedValue: %w", err)
	}
	return result, nil
}

func replaceVariableSetChildren(ctx context.Context, variableSetID, orgID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	if _, err := db.Exec(ctx,
		`DELETE FROM Variable WHERE variable_set_id = @variableSetId AND organization_id = @organizationId`,
		pgx.NamedArgs{"variableSetId": variableSetID, "organizationId": orgID},
	); err != nil {
		return fmt.Errorf("could not replace Variable rows: %w", err)
	}
	if _, err := db.Exec(ctx,
		`DELETE FROM VariableSetApplication
		WHERE variable_set_id = @variableSetId AND organization_id = @organizationId`,
		pgx.NamedArgs{"variableSetId": variableSetID, "organizationId": orgID},
	); err != nil {
		return fmt.Errorf("could not replace VariableSetApplication rows: %w", err)
	}
	return nil
}

func insertVariableSetApplications(
	ctx context.Context,
	variableSetID uuid.UUID,
	orgID uuid.UUID,
	applicationIDs []uuid.UUID,
) error {
	if len(applicationIDs) == 0 {
		return nil
	}
	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"variablesetapplication"},
		[]string{"variable_set_id", "organization_id", "application_id", "sort_order"},
		pgx.CopyFromSlice(len(applicationIDs), func(i int) ([]any, error) {
			return []any{variableSetID, orgID, applicationIDs[i], i}, nil
		}),
	)
	if err != nil {
		return mapVariableSetWriteError("insert application links", err)
	}
	return nil
}

func insertVariables(ctx context.Context, variableSetID, orgID uuid.UUID, variables []types.Variable) error {
	if len(variables) == 0 {
		return nil
	}

	for i := range variables {
		variables[i].ID = uuid.New()
		variables[i].OrganizationID = orgID
		variables[i].VariableSetID = variableSetID
	}

	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"variable"},
		[]string{
			"id",
			"organization_id",
			"variable_set_id",
			"key",
			"description",
			"type",
			"is_required",
			"default_value",
			"secret_reference_id",
			"reference_id",
			"reference_name",
		},
		pgx.CopyFromSlice(len(variables), func(i int) ([]any, error) {
			variable := variables[i]
			secretReferenceID, referenceID, err := variableReferenceColumns(variable)
			if err != nil {
				return nil, err
			}
			return []any{
				variable.ID,
				variable.OrganizationID,
				variable.VariableSetID,
				variable.Key,
				variable.Description,
				variable.Type,
				variable.IsRequired,
				variableDefaultValue(variable.DefaultValue),
				secretReferenceID,
				referenceID,
				variable.ReferenceName,
			}, nil
		}),
	)
	if err != nil {
		return mapVariableSetWriteError("insert variables", err)
	}
	if err := insertScopedValues(ctx, orgID, variableSetID, variables); err != nil {
		return err
	}
	return nil
}

func insertScopedValues(
	ctx context.Context,
	orgID uuid.UUID,
	variableSetID uuid.UUID,
	variables []types.Variable,
) error {
	type scopedValueRow struct {
		value        types.VariableScopedValue
		variableType types.VariableType
	}
	rows := make([]scopedValueRow, 0)
	for i := range variables {
		for j := range variables[i].ScopedValues {
			scopedValue := variables[i].ScopedValues[j]
			scopedValue.ID = uuid.New()
			scopedValue.OrganizationID = orgID
			scopedValue.VariableSetID = variableSetID
			scopedValue.VariableID = variables[i].ID
			rows = append(rows, scopedValueRow{value: scopedValue, variableType: variables[i].Type})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	db := internalctx.GetDb(ctx)
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"variablescopedvalue"},
		[]string{
			"id",
			"organization_id",
			"variable_set_id",
			"variable_id",
			"customer_organization_id",
			"environment_id",
			"channel_id",
			"deployment_target_id",
			"application_id",
			"target_tag",
			"process_step_key",
			"sort_order",
			"value",
			"secret_reference_id",
			"reference_id",
			"reference_name",
		},
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			scopedValue := rows[i].value
			secretReferenceID, referenceID, err := scopedValueReferenceColumns(scopedValue, rows[i].variableType)
			if err != nil {
				return nil, err
			}
			return []any{
				scopedValue.ID,
				scopedValue.OrganizationID,
				scopedValue.VariableSetID,
				scopedValue.VariableID,
				scopedValue.Scope.CustomerOrganizationID,
				scopedValue.Scope.EnvironmentID,
				scopedValue.Scope.ChannelID,
				scopedValue.Scope.DeploymentTargetID,
				scopedValue.Scope.ApplicationID,
				scopedValue.Scope.TargetTag,
				scopedValue.Scope.ProcessStepKey,
				scopedValue.SortOrder,
				variableDefaultValue(scopedValue.Value),
				secretReferenceID,
				referenceID,
				scopedValue.ReferenceName,
			}, nil
		}),
	)
	if err != nil {
		return mapVariableSetWriteError("insert scoped values", err)
	}
	return nil
}

func normalizeVariableSet(variableSet *types.VariableSet) error {
	variableSet.Name = strings.TrimSpace(variableSet.Name)
	if variableSet.Name == "" {
		return fmt.Errorf("could not normalize VariableSet: %w", apierrors.ErrBadRequest)
	}
	if variableSet.SortOrder < 0 {
		return fmt.Errorf("could not normalize VariableSet: %w", apierrors.ErrBadRequest)
	}

	applicationIDs := map[uuid.UUID]struct{}{}
	for _, applicationID := range variableSet.ApplicationIDs {
		if applicationID == uuid.Nil {
			return fmt.Errorf("could not normalize VariableSet: %w", apierrors.ErrBadRequest)
		}
		if _, ok := applicationIDs[applicationID]; ok {
			return fmt.Errorf("could not normalize VariableSet: %w", apierrors.ErrBadRequest)
		}
		applicationIDs[applicationID] = struct{}{}
	}

	keys := map[string]struct{}{}
	for i := range variableSet.Variables {
		if err := normalizeVariable(&variableSet.Variables[i]); err != nil {
			return err
		}
		if _, ok := keys[variableSet.Variables[i].Key]; ok {
			return fmt.Errorf("could not normalize VariableSet: %w", apierrors.ErrBadRequest)
		}
		keys[variableSet.Variables[i].Key] = struct{}{}
	}
	return nil
}

func normalizeVariable(variable *types.Variable) error {
	variable.Key = strings.TrimSpace(variable.Key)
	variable.ReferenceID = strings.TrimSpace(variable.ReferenceID)
	variable.ReferenceName = strings.TrimSpace(variable.ReferenceName)
	if variable.Key == "" || !isKnownVariableType(variable.Type) {
		return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
	}
	var err error
	if isReferenceVariableType(variable.Type) {
		err = normalizeReferenceVariable(variable)
	} else {
		err = normalizeValueVariable(variable)
	}
	if err != nil {
		return err
	}
	return normalizeScopedValues(variable)
}

func normalizeValueVariable(variable *types.Variable) error {
	if variable.ReferenceID != "" || variable.ReferenceName != "" {
		return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
	}
	if !hasVariableJSONValue(variable.DefaultValue) {
		if variable.IsRequired {
			variable.DefaultValue = nil
			return nil
		}
		return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
	}
	value, err := decodeVariableJSONValue(variable.DefaultValue)
	if err != nil {
		return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
	}
	switch variable.Type {
	case types.VariableTypeString:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
		}
	case types.VariableTypeNumber:
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
		}
		if _, err := number.Float64(); err != nil {
			return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
		}
	case types.VariableTypeBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
		}
	case types.VariableTypeJSON:
		if value == nil {
			return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
		}
	}
	return nil
}

func normalizeReferenceVariable(variable *types.Variable) error {
	variable.DefaultValue = nil
	if variable.ReferenceID == "" {
		if variable.IsRequired {
			return nil
		}
		return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
	}
	id, err := uuid.Parse(variable.ReferenceID)
	if err != nil || id == uuid.Nil {
		return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
	}
	if variable.Type != types.VariableTypeSecretReference && variable.ReferenceName == "" {
		return fmt.Errorf("could not normalize Variable: %w", apierrors.ErrBadRequest)
	}
	return nil
}

func normalizeScopedValues(variable *types.Variable) error {
	scopes := map[string]struct{}{}
	for i := range variable.ScopedValues {
		if err := normalizeScopedValue(variable.Type, &variable.ScopedValues[i]); err != nil {
			return err
		}
		key := variablescope.Key(variable.ScopedValues[i].Scope)
		if _, ok := scopes[key]; ok {
			return fmt.Errorf("could not normalize VariableScopedValue: %w", apierrors.ErrBadRequest)
		}
		scopes[key] = struct{}{}
	}
	return nil
}

func normalizeScopedValue(variableType types.VariableType, scopedValue *types.VariableScopedValue) error {
	scopedValue.Scope.TargetTag = strings.TrimSpace(scopedValue.Scope.TargetTag)
	scopedValue.Scope.ProcessStepKey = strings.TrimSpace(scopedValue.Scope.ProcessStepKey)
	scopedValue.ReferenceID = strings.TrimSpace(scopedValue.ReferenceID)
	scopedValue.ReferenceName = strings.TrimSpace(scopedValue.ReferenceName)
	if scopedValue.SortOrder < 0 || !variablescope.Supported(scopedValue.Scope) {
		return fmt.Errorf("could not normalize VariableScopedValue: %w", apierrors.ErrBadRequest)
	}
	if isReferenceVariableType(variableType) {
		if hasVariableJSONValue(scopedValue.Value) {
			return fmt.Errorf("could not normalize VariableScopedValue: %w", apierrors.ErrBadRequest)
		}
		scopedValue.Value = nil
		if scopedValue.ReferenceID == "" {
			return fmt.Errorf("could not normalize VariableScopedValue: %w", apierrors.ErrBadRequest)
		}
		id, err := uuid.Parse(scopedValue.ReferenceID)
		if err != nil || id == uuid.Nil {
			return fmt.Errorf("could not normalize VariableScopedValue: %w", apierrors.ErrBadRequest)
		}
		if variableType != types.VariableTypeSecretReference && scopedValue.ReferenceName == "" {
			return fmt.Errorf("could not normalize VariableScopedValue: %w", apierrors.ErrBadRequest)
		}
		return nil
	}
	if scopedValue.ReferenceID != "" || scopedValue.ReferenceName != "" {
		return fmt.Errorf("could not normalize VariableScopedValue: %w", apierrors.ErrBadRequest)
	}
	if !hasVariableJSONValue(scopedValue.Value) {
		return fmt.Errorf("could not normalize VariableScopedValue: %w", apierrors.ErrBadRequest)
	}
	return validateValueForVariableType(variableType, scopedValue.Value)
}

func ensureVariableSetApplications(ctx context.Context, orgID uuid.UUID, applicationIDs []uuid.UUID) error {
	if len(applicationIDs) == 0 {
		return nil
	}
	db := internalctx.GetDb(ctx)
	var count int
	if err := db.QueryRow(ctx,
		`SELECT count(*)
		FROM Application
		WHERE organization_id = @organizationId AND id = ANY(@applicationIds)`,
		pgx.NamedArgs{"organizationId": orgID, "applicationIds": applicationIDs},
	).Scan(&count); err != nil {
		return fmt.Errorf("could not validate VariableSet applications: %w", err)
	}
	if count != len(applicationIDs) {
		return apierrors.ErrNotFound
	}
	return nil
}

func ensureVariableReferences(ctx context.Context, orgID uuid.UUID, variables []types.Variable) error {
	for i := range variables {
		if variables[i].Type != types.VariableTypeSecretReference || variables[i].ReferenceID == "" {
			continue
		}
		key, err := ensureVariableSecretReference(ctx, orgID, variables[i].ReferenceID)
		if err != nil {
			return err
		}
		variables[i].ReferenceName = key
	}
	return nil
}

func ensureVariableScopedValueReferences(ctx context.Context, orgID uuid.UUID, variables []types.Variable) error {
	for i := range variables {
		for j := range variables[i].ScopedValues {
			scopedValue := &variables[i].ScopedValues[j]
			if err := ensureVariableScopeReferences(ctx, orgID, scopedValue.Scope); err != nil {
				return err
			}
			if variables[i].Type == types.VariableTypeSecretReference {
				key, err := ensureVariableSecretReference(ctx, orgID, scopedValue.ReferenceID)
				if err != nil {
					return err
				}
				scopedValue.ReferenceName = key
			}
		}
	}
	return nil
}

func ensureVariableScopeReferences(ctx context.Context, orgID uuid.UUID, scope types.VariableScope) error {
	if scope.ApplicationID != nil {
		if err := ensureScopedReference(ctx, orgID, *scope.ApplicationID, "Application"); err != nil {
			return err
		}
	}
	if scope.ChannelID != nil {
		if err := ensureScopedReference(ctx, orgID, *scope.ChannelID, "Channel"); err != nil {
			return err
		}
	}
	if scope.EnvironmentID != nil {
		if err := ensureScopedReference(ctx, orgID, *scope.EnvironmentID, "Environment"); err != nil {
			return err
		}
	}
	if scope.DeploymentTargetID != nil {
		if err := ensureScopedReference(ctx, orgID, *scope.DeploymentTargetID, "DeploymentTarget"); err != nil {
			return err
		}
	}
	if scope.CustomerOrganizationID != nil {
		if err := ensureScopedReference(ctx, orgID, *scope.CustomerOrganizationID, "CustomerOrganization"); err != nil {
			return err
		}
	}
	return nil
}

func ensureScopedReference(ctx context.Context, orgID, id uuid.UUID, table string) error {
	db := internalctx.GetDb(ctx)
	var exists bool
	if err := db.QueryRow(ctx,
		fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE id = @id AND organization_id = @organizationId)`, table),
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	).Scan(&exists); err != nil {
		return fmt.Errorf("could not validate Variable scoped reference: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func ensureVariableSecretReference(ctx context.Context, orgID uuid.UUID, referenceID string) (string, error) {
	id, err := uuid.Parse(referenceID)
	if err != nil || id == uuid.Nil {
		return "", fmt.Errorf("could not validate Variable secret reference: %w", apierrors.ErrBadRequest)
	}
	db := internalctx.GetDb(ctx)
	var key string
	err = db.QueryRow(ctx,
		`SELECT key
		FROM Secret
		WHERE id = @id
			AND organization_id = @organizationId
			AND customer_organization_id IS NULL`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apierrors.ErrNotFound
	} else if err != nil {
		return "", fmt.Errorf("could not validate Variable secret reference: %w", err)
	}
	return key, nil
}

func variableReferenceColumns(variable types.Variable) (*uuid.UUID, *string, error) {
	if variable.ReferenceID == "" {
		return nil, nil, nil
	}
	if variable.Type == types.VariableTypeSecretReference {
		id, err := uuid.Parse(variable.ReferenceID)
		if err != nil {
			return nil, nil, err
		}
		return &id, nil, nil
	}
	return nil, &variable.ReferenceID, nil
}

func scopedValueReferenceColumns(
	scopedValue types.VariableScopedValue,
	variableType types.VariableType,
) (*uuid.UUID, *string, error) {
	if scopedValue.ReferenceID == "" {
		return nil, nil, nil
	}
	if scopedValue.Value != nil {
		return nil, nil, fmt.Errorf("scoped value cannot have both value and reference")
	}
	if variableType == types.VariableTypeSecretReference {
		id, err := uuid.Parse(scopedValue.ReferenceID)
		if err != nil {
			return nil, nil, err
		}
		return &id, nil, nil
	}
	return nil, &scopedValue.ReferenceID, nil
}

func variableDefaultValue(value json.RawMessage) any {
	if !hasVariableJSONValue(value) {
		return nil
	}
	return value
}

func hasVariableJSONValue(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func decodeVariableJSONValue(value json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("defaultValue must contain one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return decoded, nil
}

func validateValueForVariableType(variableType types.VariableType, raw json.RawMessage) error {
	value, err := decodeVariableJSONValue(raw)
	if err != nil {
		return fmt.Errorf("could not normalize Variable value: %w", apierrors.ErrBadRequest)
	}
	switch variableType {
	case types.VariableTypeString:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("could not normalize Variable value: %w", apierrors.ErrBadRequest)
		}
	case types.VariableTypeNumber:
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("could not normalize Variable value: %w", apierrors.ErrBadRequest)
		}
		if _, err := number.Float64(); err != nil {
			return fmt.Errorf("could not normalize Variable value: %w", apierrors.ErrBadRequest)
		}
	case types.VariableTypeBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("could not normalize Variable value: %w", apierrors.ErrBadRequest)
		}
	case types.VariableTypeJSON:
		if value == nil {
			return fmt.Errorf("could not normalize Variable value: %w", apierrors.ErrBadRequest)
		}
	}
	return nil
}

func isKnownVariableType(value types.VariableType) bool {
	switch value {
	case types.VariableTypeString,
		types.VariableTypeNumber,
		types.VariableTypeBoolean,
		types.VariableTypeJSON,
		types.VariableTypeSecretReference,
		types.VariableTypeAccountReference,
		types.VariableTypeCertificateReference:
		return true
	default:
		return false
	}
}

func isReferenceVariableType(value types.VariableType) bool {
	switch value {
	case types.VariableTypeSecretReference, types.VariableTypeAccountReference, types.VariableTypeCertificateReference:
		return true
	default:
		return false
	}
}

func uuidFromPG(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := uuid.UUID(value.Bytes)
	return &id
}

func preparePromptedValues(
	ctx context.Context,
	orgID uuid.UUID,
	variables []types.Variable,
	promptedValues []types.VariablePromptedValue,
) ([]types.VariablePromptedValue, error) {
	variablesByKey := map[string]types.Variable{}
	for _, variable := range variables {
		variablesByKey[variable.Key] = variable
	}
	result := make([]types.VariablePromptedValue, len(promptedValues))
	copy(result, promptedValues)
	for i := range result {
		result[i].Key = strings.TrimSpace(result[i].Key)
		result[i].ReferenceID = strings.TrimSpace(result[i].ReferenceID)
		result[i].ReferenceName = strings.TrimSpace(result[i].ReferenceName)
		variable, ok := variablesByKey[result[i].Key]
		if !ok {
			return nil, apierrors.ErrBadRequest
		}
		if isReferenceVariableType(variable.Type) {
			if hasVariableJSONValue(result[i].Value) || result[i].ReferenceID == "" {
				return nil, apierrors.ErrBadRequest
			}
			if variable.Type == types.VariableTypeSecretReference {
				key, err := ensureVariableSecretReference(ctx, orgID, result[i].ReferenceID)
				if err != nil {
					return nil, err
				}
				result[i].ReferenceName = key
			} else if result[i].ReferenceName == "" {
				return nil, apierrors.ErrBadRequest
			}
			continue
		}
		if result[i].ReferenceID != "" || result[i].ReferenceName != "" || !hasVariableJSONValue(result[i].Value) {
			return nil, apierrors.ErrBadRequest
		}
		if err := validateValueForVariableType(variable.Type, result[i].Value); err != nil {
			return nil, apierrors.ErrBadRequest
		}
	}
	return result, nil
}

func mapVariableSetWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s VariableSet: %w", action, apierrors.ErrAlreadyExists)
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s VariableSet: %w", action, apierrors.ErrNotFound)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s VariableSet: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s VariableSet: %w", action, err)
}
