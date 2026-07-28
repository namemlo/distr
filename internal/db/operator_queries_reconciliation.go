package db

import (
	"context"
	"fmt"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const operatorReconciliationListSQL = `
WITH filtered AS (
	SELECT
		drift.id,
		drift.created_at,
		drift.id AS drift_case_id,
		desired.execution_id,
		desired.deployment_plan_id,
		assignment.environment_id,
		unit.deployment_target_id,
		desired.component_key AS component,
		array_to_string(drift.classes, ',') AS drift,
		drift.status,
		CASE
			WHEN observation.outcome = 'PARTIAL' THEN 'PARTIAL'
			WHEN observation.outcome = 'UNKNOWN' THEN 'UNKNOWN'
			WHEN observation.fresh_until <= @evaluatedAt THEN 'STALE'
			ELSE 'CURRENT'
		END AS outcome,
		observation.captured_at AS observed_at,
		observation.evidence_checksum,
		count(*) OVER() AS total_count
	FROM DriftCase drift
	JOIN ActiveDesiredRevision desired
	  ON desired.id = drift.active_desired_revision_id
	 AND desired.organization_id = drift.organization_id
	JOIN ObservedComponentState observation
	  ON observation.id = drift.observation_id
	 AND observation.organization_id = drift.organization_id
	JOIN DeploymentUnit unit
	  ON unit.id = drift.deployment_unit_id
	 AND unit.organization_id = drift.organization_id
	JOIN TargetEnvironmentAssignment assignment
	  ON assignment.id = unit.target_environment_assignment_id
	 AND assignment.organization_id = unit.organization_id
	JOIN DeploymentScope deployment_scope
	  ON deployment_scope.id = unit.deployment_scope_id
	 AND deployment_scope.organization_id = unit.organization_id
	JOIN ComponentInstance component
	  ON component.id = drift.component_instance_id
	 AND component.deployment_unit_id = drift.deployment_unit_id
	 AND component.organization_id = drift.organization_id
	WHERE drift.organization_id = @organizationId
	  AND (@status::text IS NULL OR drift.status = @status)
	  AND (@drift::text IS NULL OR @drift = ANY(drift.classes))
	  AND (@environmentId::uuid IS NULL OR assignment.environment_id = @environmentId)
	  AND (
		@deploymentTargetId::uuid IS NULL
		OR unit.deployment_target_id = @deploymentTargetId
	  )
	  AND (
		@organizationWide
		OR deployment_scope.customer_organization_id = ANY(@authorizedCustomerIds::uuid[])
		OR EXISTS (
			SELECT 1
			FROM DeploymentUnitSubscriber subscriber
			WHERE subscriber.organization_id = drift.organization_id
			  AND subscriber.deployment_unit_id = drift.deployment_unit_id
			  AND subscriber.retired_at IS NULL
			  AND subscriber.customer_organization_id = ANY(@authorizedCustomerIds::uuid[])
		)
		OR assignment.environment_id = ANY(@authorizedEnvironmentIds::uuid[])
		OR drift.deployment_unit_id = ANY(@authorizedDeploymentUnitIds::uuid[])
		OR component.component_definition_id = ANY(@authorizedComponentIds::uuid[])
	  )
)
SELECT *
FROM filtered drift
WHERE @afterCreatedAt::timestamptz IS NULL
   OR drift.created_at < @afterCreatedAt
   OR (drift.created_at = @afterCreatedAt AND drift.id < @afterId)
ORDER BY created_at DESC, id DESC
LIMIT @limit`

const operatorReconciliationDetailSQL = `
SELECT
	drift.id,
	drift.created_at,
	drift.id AS drift_case_id,
	desired.execution_id,
	desired.deployment_plan_id,
	assignment.environment_id,
	unit.deployment_target_id,
	desired.component_key AS component,
	array_to_string(drift.classes, ',') AS drift,
	drift.status,
	CASE
		WHEN observation.outcome = 'PARTIAL' THEN 'PARTIAL'
		WHEN observation.outcome = 'UNKNOWN' THEN 'UNKNOWN'
		WHEN observation.fresh_until <= @evaluatedAt THEN 'STALE'
		ELSE 'CURRENT'
	END AS outcome,
	observation.captured_at AS observed_at,
	observation.evidence_checksum,
	desired.id,
	desired.created_at,
	desired.artifact_digest,
	desired.config_checksum,
	observation.id,
	observation.created_at,
	observation.artifact_digest,
	observation.config_checksum,
	observation.outcome,
	observation.health,
	latest_action.id,
	latest_action.created_at,
	latest_action.action
FROM DriftCase drift
JOIN ActiveDesiredRevision desired
  ON desired.id = drift.active_desired_revision_id
 AND desired.organization_id = drift.organization_id
JOIN ObservedComponentState observation
  ON observation.id = drift.observation_id
 AND observation.organization_id = drift.organization_id
JOIN DeploymentUnit unit
  ON unit.id = drift.deployment_unit_id
 AND unit.organization_id = drift.organization_id
JOIN TargetEnvironmentAssignment assignment
  ON assignment.id = unit.target_environment_assignment_id
 AND assignment.organization_id = unit.organization_id
JOIN DeploymentScope deployment_scope
  ON deployment_scope.id = unit.deployment_scope_id
 AND deployment_scope.organization_id = unit.organization_id
JOIN ComponentInstance component
  ON component.id = drift.component_instance_id
 AND component.deployment_unit_id = drift.deployment_unit_id
 AND component.organization_id = drift.organization_id
LEFT JOIN LATERAL (
	SELECT action.id, action.created_at, action.action
	FROM ReconciliationAction action
	WHERE action.organization_id = drift.organization_id
	  AND action.drift_case_id = drift.id
	ORDER BY action.created_at DESC, action.id DESC
	LIMIT 1
) latest_action ON true
WHERE drift.organization_id = @organizationId
  AND drift.id = @reconciliationId
  AND (
	@organizationWide
	OR deployment_scope.customer_organization_id = ANY(@authorizedCustomerIds::uuid[])
	OR EXISTS (
		SELECT 1
		FROM DeploymentUnitSubscriber subscriber
		WHERE subscriber.organization_id = drift.organization_id
		  AND subscriber.deployment_unit_id = drift.deployment_unit_id
		  AND subscriber.retired_at IS NULL
		  AND subscriber.customer_organization_id = ANY(@authorizedCustomerIds::uuid[])
	)
	OR assignment.environment_id = ANY(@authorizedEnvironmentIds::uuid[])
	OR drift.deployment_unit_id = ANY(@authorizedDeploymentUnitIds::uuid[])
	OR component.component_definition_id = ANY(@authorizedComponentIds::uuid[])
  )`

type operatorSQLScopes struct {
	organizationWide  bool
	customerIDs       []uuid.UUID
	environmentIDs    []uuid.UUID
	deploymentUnitIDs []uuid.UUID
	componentIDs      []uuid.UUID
	campaignIDs       []uuid.UUID
}

func ListOperatorReconciliationRows(
	ctx context.Context,
	filter types.ReconciliationFilter,
	afterCreatedAt *time.Time,
	afterID *uuid.UUID,
	limit int,
) ([]types.OperatorReconciliationRow, *int64, error) {
	scopes, err := operatorSQLScopesFromFilter(filter.OperatorScopeFilter)
	if err != nil {
		return nil, nil, err
	}
	if limit < 1 || limit > types.OperatorMaximumPageLimit+1 ||
		(afterCreatedAt == nil) != (afterID == nil) {
		return nil, nil, apierrors.ErrBadRequest
	}

	rows, err := internalctx.GetDb(ctx).Query(ctx, operatorReconciliationListSQL, pgx.NamedArgs{
		"organizationId":              filter.OrganizationID,
		"evaluatedAt":                 filter.DecisionAt,
		"status":                      nullableOperatorString(filter.Status),
		"drift":                       nullableOperatorString(filter.Drift),
		"environmentId":               filter.EnvironmentID,
		"deploymentTargetId":          filter.DeploymentTargetID,
		"organizationWide":            scopes.organizationWide,
		"authorizedCustomerIds":       scopes.customerIDs,
		"authorizedEnvironmentIds":    scopes.environmentIDs,
		"authorizedDeploymentUnitIds": scopes.deploymentUnitIDs,
		"authorizedComponentIds":      scopes.componentIDs,
		"afterCreatedAt":              afterCreatedAt,
		"afterId":                     afterID,
		"limit":                       limit,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("query operator reconciliation rows: %w", err)
	}
	defer rows.Close()

	items := make([]types.OperatorReconciliationRow, 0, limit)
	var total *int64
	for rows.Next() {
		var item types.OperatorReconciliationRow
		var rowTotal int64
		if err := rows.Scan(
			&item.ID,
			&item.CreatedAt,
			&item.DriftCaseID,
			&item.ExecutionID,
			&item.DeploymentPlanID,
			&item.EnvironmentID,
			&item.DeploymentTargetID,
			&item.Component,
			&item.Drift,
			&item.Status,
			&item.Outcome,
			&item.ObservedAt,
			&item.EvidenceChecksum,
			&rowTotal,
		); err != nil {
			return nil, nil, fmt.Errorf("scan operator reconciliation row: %w", err)
		}
		if total == nil {
			total = new(rowTotal)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate operator reconciliation rows: %w", err)
	}
	return items, total, nil
}

func GetOperatorReconciliationDetail(
	ctx context.Context,
	filter types.OperatorScopeFilter,
	reconciliationID uuid.UUID,
) (*types.OperatorReconciliationDetail, error) {
	scopes, err := operatorSQLScopesFromFilter(filter)
	if err != nil {
		return nil, err
	}
	if reconciliationID == uuid.Nil {
		return nil, apierrors.ErrBadRequest
	}

	var detail types.OperatorReconciliationDetail
	var desiredID uuid.UUID
	var desiredCreatedAt time.Time
	var desiredArtifactDigest string
	var desiredConfigChecksum string
	var observationID uuid.UUID
	var observationCreatedAt time.Time
	var observedArtifactDigest string
	var observedConfigChecksum string
	var observationOutcome string
	var observationHealth string
	var actionID *uuid.UUID
	var actionCreatedAt *time.Time
	var action *string
	err = internalctx.GetDb(ctx).QueryRow(ctx, operatorReconciliationDetailSQL, pgx.NamedArgs{
		"organizationId":              filter.OrganizationID,
		"reconciliationId":            reconciliationID,
		"evaluatedAt":                 filter.DecisionAt,
		"organizationWide":            scopes.organizationWide,
		"authorizedCustomerIds":       scopes.customerIDs,
		"authorizedEnvironmentIds":    scopes.environmentIDs,
		"authorizedDeploymentUnitIds": scopes.deploymentUnitIDs,
		"authorizedComponentIds":      scopes.componentIDs,
	}).Scan(
		&detail.Reconciliation.ID,
		&detail.Reconciliation.CreatedAt,
		&detail.Reconciliation.DriftCaseID,
		&detail.Reconciliation.ExecutionID,
		&detail.Reconciliation.DeploymentPlanID,
		&detail.Reconciliation.EnvironmentID,
		&detail.Reconciliation.DeploymentTargetID,
		&detail.Reconciliation.Component,
		&detail.Reconciliation.Drift,
		&detail.Reconciliation.Status,
		&detail.Reconciliation.Outcome,
		&detail.Reconciliation.ObservedAt,
		&detail.Reconciliation.EvidenceChecksum,
		&desiredID,
		&desiredCreatedAt,
		&desiredArtifactDigest,
		&desiredConfigChecksum,
		&observationID,
		&observationCreatedAt,
		&observedArtifactDigest,
		&observedConfigChecksum,
		&observationOutcome,
		&observationHealth,
		&actionID,
		&actionCreatedAt,
		&action,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apierrors.ErrNotFound
		}
		return nil, fmt.Errorf("query operator reconciliation detail: %w", err)
	}

	detail.DesiredState = &types.OperatorPlanFact{
		ID: &desiredID, Key: detail.Reconciliation.Component, Kind: "desired_state",
		Status: "ACTIVE", Expected: desiredArtifactDigest, Checksum: desiredConfigChecksum,
	}
	detail.Observation = &types.OperatorPlanFact{
		ID: &observationID, Key: detail.Reconciliation.Component, Kind: "observation",
		Status:   observationOutcome + "/" + observationHealth,
		Expected: desiredArtifactDigest, Actual: observedArtifactDigest,
		Checksum: detail.Reconciliation.EvidenceChecksum,
	}
	if actionID != nil && action != nil {
		detail.Decision = &types.OperatorPlanFact{
			ID: actionID, Key: "latest", Kind: "reconciliation", Status: *action,
		}
	}
	detail.Evidence = []types.OperatorEvidenceRef{
		{
			ID: observationID, Kind: "observation", Label: "Independent observation",
			Href:     "/api/v1/control-plane/audit?subjectType=observation&subjectId=" + observationID.String(),
			Checksum: detail.Reconciliation.EvidenceChecksum, CreatedAt: observationCreatedAt,
		},
	}
	_ = desiredCreatedAt
	_ = observedConfigChecksum
	_ = actionCreatedAt
	return &detail, nil
}

func operatorSQLScopesFromFilter(filter types.OperatorScopeFilter) (operatorSQLScopes, error) {
	if err := validateCanonicalOperatorScopeFilter(filter); err != nil {
		return operatorSQLScopes{}, err
	}
	result := operatorSQLScopes{
		organizationWide:  filter.OrganizationWide,
		customerIDs:       filter.CustomerIDs,
		environmentIDs:    filter.EnvironmentIDs,
		deploymentUnitIDs: filter.DeploymentUnitIDs,
		componentIDs:      filter.ComponentIDs,
		campaignIDs:       filter.CampaignIDs,
	}
	return result, nil
}

func nullableOperatorString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
