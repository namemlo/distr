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
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	stepTemplateOutputExpr = `
	st.id,
	st.created_at,
	st.updated_at,
	st.organization_id,
	st.source_type,
	st.source_ref,
	st.name,
	st.description,
	st.category,
	st.installed_at,
	st.installed_by_useraccount_id
`
	stepTemplateVersionOutputExpr = `
	stv.id,
	stv.created_at,
	stv.step_template_id,
	stv.organization_id,
	stv.version,
	stv.action_type,
	stv.execution_location,
	stv.input_schema,
	stv.output_schema,
	stv.default_input_bindings,
	stv.minimum_agent_version,
	stv.compatible_action_version,
	stv.runtime_compatibility_notes,
	stv.deprecated
`
)

func ImportStepTemplate(ctx context.Context, request types.StepTemplateImport) (*types.StepTemplate, error) {
	if err := normalizeStepTemplateImport(&request); err != nil {
		return nil, err
	}

	var templateID uuid.UUID
	err := RunTx(ctx, func(ctx context.Context) error {
		id, err := getStepTemplateIDBySource(ctx, request.OrganizationID, request.SourceType, request.SourceRef)
		if errors.Is(err, apierrors.ErrNotFound) {
			templateID, err = insertStepTemplate(ctx, request)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if err := EnsureConfigAsCodeDatabaseManagedForUpdate(
				ctx,
				request.OrganizationID,
				types.ConfigAsCodeResourceKindStepTemplateReference,
				id,
			); err != nil {
				return err
			}
			templateID = id
		}
		return insertStepTemplateVersion(ctx, templateID, request)
	})
	if err != nil {
		return nil, err
	}
	return GetStepTemplate(ctx, templateID, request.OrganizationID)
}

func GetStepTemplatesByOrganizationID(ctx context.Context, orgID uuid.UUID) ([]types.StepTemplate, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+stepTemplateOutputExpr+`
		FROM StepTemplate st
		WHERE st.organization_id = @organizationId
		ORDER BY st.category, st.name, st.source_ref, st.id`,
		pgx.NamedArgs{"organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query StepTemplate: %w", err)
	}
	templates, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.StepTemplate])
	if err != nil {
		return nil, fmt.Errorf("could not collect StepTemplate: %w", err)
	}
	for i := range templates {
		templates[i].Versions, err = getStepTemplateVersions(ctx, templates[i].ID, orgID)
		if err != nil {
			return nil, err
		}
	}
	return templates, nil
}

func GetStepTemplate(ctx context.Context, id, orgID uuid.UUID) (*types.StepTemplate, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+stepTemplateOutputExpr+`
		FROM StepTemplate st
		WHERE st.id = @id AND st.organization_id = @organizationId`,
		pgx.NamedArgs{"id": id, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query StepTemplate: %w", err)
	}
	template, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.StepTemplate])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect StepTemplate: %w", err)
	}
	template.Versions, err = getStepTemplateVersions(ctx, template.ID, orgID)
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func getStepTemplateIDBySource(
	ctx context.Context,
	orgID uuid.UUID,
	sourceType types.StepTemplateSourceType,
	sourceRef string,
) (uuid.UUID, error) {
	db := internalctx.GetDb(ctx)
	var id uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT id
		FROM StepTemplate
		WHERE organization_id = @organizationId
			AND source_type = @sourceType
			AND source_ref = @sourceRef
		FOR UPDATE`,
		pgx.NamedArgs{"organizationId": orgID, "sourceType": sourceType, "sourceRef": sourceRef},
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apierrors.ErrNotFound
	} else if err != nil {
		return uuid.Nil, fmt.Errorf("could not query StepTemplate source: %w", err)
	}
	return id, nil
}

func insertStepTemplate(ctx context.Context, request types.StepTemplateImport) (uuid.UUID, error) {
	db := internalctx.GetDb(ctx)
	var id uuid.UUID
	err := db.QueryRow(ctx,
		`INSERT INTO StepTemplate (
			organization_id,
			source_type,
			source_ref,
			name,
			description,
			category,
			installed_by_useraccount_id
		) VALUES (
			@organizationId,
			@sourceType,
			@sourceRef,
			@name,
			@description,
			@category,
			@installedByUserAccountId
		)
		RETURNING id`,
		pgx.NamedArgs{
			"organizationId":           request.OrganizationID,
			"sourceType":               request.SourceType,
			"sourceRef":                request.SourceRef,
			"name":                     request.Name,
			"description":              request.Description,
			"category":                 request.Category,
			"installedByUserAccountId": request.InstalledByUserAccountID,
		},
	).Scan(&id)
	if err != nil {
		return uuid.Nil, mapStepTemplateWriteError("insert", err)
	}
	return id, nil
}

func insertStepTemplateVersion(ctx context.Context, templateID uuid.UUID, request types.StepTemplateImport) error {
	db := internalctx.GetDb(ctx)
	_, err := db.Exec(ctx,
		`INSERT INTO StepTemplateVersion (
			step_template_id,
			organization_id,
			version,
			action_type,
			execution_location,
			input_schema,
			output_schema,
			default_input_bindings,
			minimum_agent_version,
			compatible_action_version,
			runtime_compatibility_notes,
			deprecated
		) VALUES (
			@stepTemplateId,
			@organizationId,
			@version,
			@actionType,
			@executionLocation,
			@inputSchema,
			@outputSchema,
			@defaultInputBindings,
			@minimumAgentVersion,
			@compatibleActionVersion,
			@runtimeCompatibilityNotes,
			@deprecated
		)`,
		pgx.NamedArgs{
			"stepTemplateId":            templateID,
			"organizationId":            request.OrganizationID,
			"version":                   request.Version,
			"actionType":                request.ActionType,
			"executionLocation":         request.ExecutionLocation,
			"inputSchema":               request.InputSchema,
			"outputSchema":              request.OutputSchema,
			"defaultInputBindings":      request.DefaultInputBindings,
			"minimumAgentVersion":       request.MinimumAgentVersion,
			"compatibleActionVersion":   request.CompatibleActionVersion,
			"runtimeCompatibilityNotes": request.RuntimeCompatibilityNotes,
			"deprecated":                request.Deprecated,
		},
	)
	if err != nil {
		return mapStepTemplateWriteError("insert version", err)
	}
	return nil
}

func getStepTemplateVersions(ctx context.Context, templateID, orgID uuid.UUID) ([]types.StepTemplateVersion, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+stepTemplateVersionOutputExpr+`
		FROM StepTemplateVersion stv
		WHERE stv.step_template_id = @stepTemplateId AND stv.organization_id = @organizationId
		ORDER BY stv.version, stv.id`,
		pgx.NamedArgs{"stepTemplateId": templateID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query StepTemplateVersion: %w", err)
	}
	versions, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.StepTemplateVersion])
	if err != nil {
		return nil, fmt.Errorf("could not collect StepTemplateVersion: %w", err)
	}
	return versions, nil
}

func normalizeStepTemplateImport(request *types.StepTemplateImport) error {
	request.SourceRef = strings.TrimSpace(request.SourceRef)
	request.Name = strings.TrimSpace(request.Name)
	request.Description = strings.TrimSpace(request.Description)
	request.Category = strings.TrimSpace(request.Category)
	request.Version = strings.TrimSpace(request.Version)
	request.ActionType = strings.TrimSpace(request.ActionType)
	request.ExecutionLocation = strings.TrimSpace(request.ExecutionLocation)
	request.MinimumAgentVersion = strings.TrimSpace(request.MinimumAgentVersion)
	request.CompatibleActionVersion = strings.TrimSpace(request.CompatibleActionVersion)
	request.RuntimeCompatibilityNotes = strings.TrimSpace(request.RuntimeCompatibilityNotes)

	if request.OrganizationID == uuid.Nil {
		return apierrors.NewBadRequest("organizationId is required")
	}
	if !request.SourceType.IsValid() {
		return apierrors.NewBadRequest("sourceType must be builtin or oci")
	}
	if request.SourceRef == "" {
		return apierrors.NewBadRequest("sourceRef is required")
	}
	if request.Name == "" {
		return apierrors.NewBadRequest("name is required")
	}
	if request.Version == "" {
		return apierrors.NewBadRequest("version is required")
	}
	if request.ActionType == "" {
		return apierrors.NewBadRequest("actionType is required")
	}
	if request.ExecutionLocation == "" {
		return apierrors.NewBadRequest("executionLocation is required")
	}
	if request.DefaultInputBindings == nil {
		request.DefaultInputBindings = map[string]any{}
	}
	if err := normalizeStepTemplateSchema(&request.InputSchema, "input"); err != nil {
		return err
	}
	if err := normalizeStepTemplateSchema(&request.OutputSchema, "output"); err != nil {
		return err
	}
	if err := actionregistry.DefaultRegistry().ValidateInput(request.ActionType, request.DefaultInputBindings); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	return nil
}

func normalizeStepTemplateSchema(schema *map[string]any, label string) error {
	if *schema == nil {
		*schema = map[string]any{"type": "object", "additionalProperties": true}
	}
	if value, ok := (*schema)["type"].(string); !ok || value != "object" {
		return apierrors.NewBadRequest(label + " schema must be a JSON object schema")
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource(label+".schema.json", *schema); err != nil {
		return apierrors.NewBadRequest(label + " schema is invalid: " + err.Error())
	}
	if _, err := compiler.Compile(label + ".schema.json"); err != nil {
		return apierrors.NewBadRequest(label + " schema is invalid: " + err.Error())
	}
	return nil
}

func mapStepTemplateWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s StepTemplate: %w", action, apierrors.ErrAlreadyExists)
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s StepTemplate: %w", action, apierrors.ErrNotFound)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s StepTemplate: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s StepTemplate: %w", action, err)
}
