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

const (
	agentCapabilityReportOutputExpr = `
		acr.id,
		acr.created_at,
		acr.updated_at,
		acr.organization_id,
		acr.deployment_target_id,
		acr.protocol_version,
		acr.agent_version,
		acr.supported_runtimes,
		acr.operating_system,
		acr.architecture,
		acr.available_tooling,
		acr.strategy_capabilities,
		acr.compatibility_warnings
	`
	agentActionCapabilityOutputExpr = `
		aac.id,
		aac.report_id,
		aac.organization_id,
		aac.deployment_target_id,
		aac.action_type,
		aac.versions
	`
)

func UpsertAgentCapabilityReport(
	ctx context.Context,
	report types.AgentCapabilityReport,
) (*types.AgentCapabilityReport, error) {
	if report.OrganizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if report.DeploymentTargetID == uuid.Nil {
		return nil, apierrors.NewBadRequest("deploymentTargetId is required")
	}
	var saved *types.AgentCapabilityReport
	err := RunTx(ctx, func(ctx context.Context) error {
		if err := ensureDeploymentTargetForCapabilityReport(
			ctx,
			report.DeploymentTargetID,
			report.OrganizationID,
		); err != nil {
			return err
		}
		upserted, err := upsertAgentCapabilityReport(ctx, report)
		if err != nil {
			return err
		}
		if err := replaceAgentActionCapabilities(ctx, *upserted, report.SupportedActions); err != nil {
			return err
		}
		saved, err = GetAgentCapabilityReportForDeploymentTarget(
			ctx,
			report.DeploymentTargetID,
			report.OrganizationID,
		)
		return err
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

func GetAgentCapabilityReportForDeploymentTarget(
	ctx context.Context,
	deploymentTargetID uuid.UUID,
	orgID uuid.UUID,
) (*types.AgentCapabilityReport, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+agentCapabilityReportOutputExpr+`
		FROM AgentCapabilityReport acr
		WHERE acr.deployment_target_id = @deploymentTargetId
			AND acr.organization_id = @organizationId`,
		pgx.NamedArgs{"deploymentTargetId": deploymentTargetID, "organizationId": orgID},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query AgentCapabilityReport: %w", err)
	}
	report, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.AgentCapabilityReport])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("could not collect AgentCapabilityReport: %w", err)
	}
	if err := hydrateAgentCapabilityReport(ctx, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func getAgentCapabilityReportsForDeploymentTargets(
	ctx context.Context,
	orgID uuid.UUID,
	deploymentTargetIDs []uuid.UUID,
) (map[uuid.UUID]types.AgentCapabilityReport, error) {
	reports := map[uuid.UUID]types.AgentCapabilityReport{}
	if len(deploymentTargetIDs) == 0 {
		return reports, nil
	}
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+agentCapabilityReportOutputExpr+`
		FROM AgentCapabilityReport acr
		WHERE acr.organization_id = @organizationId
			AND acr.deployment_target_id = ANY(@deploymentTargetIds)
		ORDER BY acr.deployment_target_id`,
		pgx.NamedArgs{"organizationId": orgID, "deploymentTargetIds": deploymentTargetIDs},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query AgentCapabilityReport by targets: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.AgentCapabilityReport])
	if err != nil {
		return nil, fmt.Errorf("could not collect AgentCapabilityReport by targets: %w", err)
	}
	for i := range items {
		if err := hydrateAgentCapabilityReport(ctx, &items[i]); err != nil {
			return nil, err
		}
		reports[items[i].DeploymentTargetID] = items[i]
	}
	return reports, nil
}

func ensureDeploymentTargetForCapabilityReport(ctx context.Context, targetID, orgID uuid.UUID) error {
	db := internalctx.GetDb(ctx)
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM DeploymentTarget dt
			JOIN Organization o ON o.id = dt.organization_id AND o.deleted_at IS NULL
			WHERE dt.id = @deploymentTargetId AND dt.organization_id = @organizationId
		)`,
		pgx.NamedArgs{"deploymentTargetId": targetID, "organizationId": orgID},
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("could not validate AgentCapabilityReport target: %w", err)
	}
	if !exists {
		return apierrors.ErrNotFound
	}
	return nil
}

func upsertAgentCapabilityReport(
	ctx context.Context,
	report types.AgentCapabilityReport,
) (*types.AgentCapabilityReport, error) {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`INSERT INTO AgentCapabilityReport AS acr (
			organization_id,
			deployment_target_id,
			protocol_version,
			agent_version,
			supported_runtimes,
			operating_system,
			architecture,
			available_tooling,
			strategy_capabilities,
			compatibility_warnings
		) VALUES (
			@organizationId,
			@deploymentTargetId,
			@protocolVersion,
			@agentVersion,
			@supportedRuntimes,
			@operatingSystem,
			@architecture,
			@availableTooling,
			@strategyCapabilities,
			@compatibilityWarnings
		)
		ON CONFLICT (deployment_target_id) DO UPDATE SET
			protocol_version = EXCLUDED.protocol_version,
			agent_version = EXCLUDED.agent_version,
			supported_runtimes = EXCLUDED.supported_runtimes,
			operating_system = EXCLUDED.operating_system,
			architecture = EXCLUDED.architecture,
			available_tooling = EXCLUDED.available_tooling,
			strategy_capabilities = EXCLUDED.strategy_capabilities,
			compatibility_warnings = EXCLUDED.compatibility_warnings,
			updated_at = now()
		RETURNING `+agentCapabilityReportOutputExpr,
		pgx.NamedArgs{
			"organizationId":        report.OrganizationID,
			"deploymentTargetId":    report.DeploymentTargetID,
			"protocolVersion":       report.ProtocolVersion,
			"agentVersion":          report.AgentVersion,
			"supportedRuntimes":     stringSliceOrEmpty(report.SupportedRuntimes),
			"operatingSystem":       report.OperatingSystem,
			"architecture":          report.Architecture,
			"availableTooling":      stringSliceOrEmpty(report.AvailableTooling),
			"strategyCapabilities":  stringSliceOrEmpty(report.StrategyCapabilities),
			"compatibilityWarnings": stringSliceOrEmpty(report.CompatibilityWarnings),
		},
	)
	if err != nil {
		return nil, mapAgentCapabilityWriteError("upsert report", err)
	}
	upserted, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[types.AgentCapabilityReport])
	if err != nil {
		return nil, mapAgentCapabilityWriteError("scan report", err)
	}
	return &upserted, nil
}

func replaceAgentActionCapabilities(
	ctx context.Context,
	report types.AgentCapabilityReport,
	actions []types.AgentActionCapability,
) error {
	db := internalctx.GetDb(ctx)
	if _, err := db.Exec(ctx,
		`DELETE FROM AgentActionCapability
		WHERE report_id = @reportId AND organization_id = @organizationId`,
		pgx.NamedArgs{"reportId": report.ID, "organizationId": report.OrganizationID},
	); err != nil {
		return mapAgentCapabilityWriteError("delete actions", err)
	}
	if len(actions) == 0 {
		return nil
	}
	_, err := db.CopyFrom(
		ctx,
		pgx.Identifier{"agentactioncapability"},
		[]string{
			"report_id",
			"organization_id",
			"deployment_target_id",
			"action_type",
			"versions",
		},
		pgx.CopyFromSlice(len(actions), func(i int) ([]any, error) {
			action := actions[i]
			return []any{
				report.ID,
				report.OrganizationID,
				report.DeploymentTargetID,
				action.ActionType,
				stringSliceOrEmpty(action.Versions),
			}, nil
		}),
	)
	if err != nil {
		return mapAgentCapabilityWriteError("insert actions", err)
	}
	return nil
}

func hydrateAgentCapabilityReport(ctx context.Context, report *types.AgentCapabilityReport) error {
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT `+agentActionCapabilityOutputExpr+`
		FROM AgentActionCapability aac
		WHERE aac.report_id = @reportId AND aac.organization_id = @organizationId
		ORDER BY aac.action_type, aac.id`,
		pgx.NamedArgs{"reportId": report.ID, "organizationId": report.OrganizationID},
	)
	if err != nil {
		return fmt.Errorf("could not query AgentActionCapability: %w", err)
	}
	actions, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.AgentActionCapability])
	if err != nil {
		return fmt.Errorf("could not collect AgentActionCapability: %w", err)
	}
	report.SupportedActions = actions
	return nil
}

func agentCapabilitySupportsAction(
	report types.AgentCapabilityReport,
	actionType string,
	actionVersion string,
) bool {
	for _, action := range report.SupportedActions {
		if action.ActionType != actionType {
			continue
		}
		for _, version := range action.Versions {
			if version == actionVersion {
				return true
			}
		}
	}
	return false
}

func mapAgentCapabilityWriteError(action string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.ForeignKeyViolation:
			return fmt.Errorf("could not %s AgentCapabilityReport: %w", action, apierrors.ErrNotFound)
		case pgerrcode.UniqueViolation:
			return fmt.Errorf("could not %s AgentCapabilityReport: %w", action, apierrors.ErrConflict)
		case pgerrcode.CheckViolation:
			return fmt.Errorf("could not %s AgentCapabilityReport: %w", action, apierrors.ErrBadRequest)
		}
	}
	return fmt.Errorf("could not %s AgentCapabilityReport: %w", action, err)
}
