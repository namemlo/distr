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
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
		if err := normalizeVariableSet(variableSet); err != nil {
			return err
		}
		if err := ensureVariableSetApplications(ctx, variableSet.OrganizationID, variableSet.ApplicationIDs); err != nil {
			return err
		}
		if err := ensureVariableReferences(ctx, variableSet.OrganizationID, variableSet.Variables); err != nil {
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
	db := internalctx.GetDb(ctx)
	cmd, err := db.Exec(ctx,
		`DELETE FROM VariableSet WHERE id = @id AND organization_id = @organizationId`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == pgerrcode.ForeignKeyViolation {
			return fmt.Errorf("could not delete VariableSet: %w", apierrors.ErrConflict)
		}
		return fmt.Errorf("could not delete VariableSet: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return apierrors.ErrNotFound
	}
	return nil
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

	rows := make([]types.Variable, len(variables))
	copy(rows, variables)
	for i := range rows {
		rows[i].ID = uuid.New()
		rows[i].OrganizationID = orgID
		rows[i].VariableSetID = variableSetID
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
		pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
			variable := rows[i]
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
	if isReferenceVariableType(variable.Type) {
		return normalizeReferenceVariable(variable)
	}
	return normalizeValueVariable(variable)
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
