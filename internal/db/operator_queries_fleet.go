package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const operatorFleetSQL = `
WITH subscriber_customers AS (
  SELECT
    source.organization_id,
    source.deployment_unit_id,
    array_agg(customer.id ORDER BY customer.name, customer.id) AS customer_ids,
    string_agg(customer.name, ', ' ORDER BY customer.name, customer.id) AS customers,
    count(DISTINCT customer.id) AS subscriber_count
  FROM (
    SELECT
      unit.organization_id,
      unit.id AS deployment_unit_id,
      scope.customer_organization_id
    FROM DeploymentUnit unit
    JOIN DeploymentScope scope
      ON scope.id = unit.deployment_scope_id
     AND scope.organization_id = unit.organization_id
    WHERE unit.organization_id = @organizationID
      AND scope.customer_organization_id IS NOT NULL
    UNION
    SELECT
      subscriber.organization_id,
      subscriber.deployment_unit_id,
      subscriber.customer_organization_id
    FROM DeploymentUnitSubscriber subscriber
    WHERE subscriber.organization_id = @organizationID
      AND subscriber.retired_at IS NULL
  ) source
  JOIN CustomerOrganization customer
    ON customer.id = source.customer_organization_id
   AND customer.organization_id = source.organization_id
  GROUP BY source.organization_id, source.deployment_unit_id
),
effective_enrollment AS (
  SELECT DISTINCT ON (enrollment.organization_id, enrollment.scope_kind, enrollment.scope_id)
    enrollment.organization_id,
    enrollment.scope_kind,
    enrollment.scope_id,
    enrollment.enabled
  FROM ControlPlaneEnrollment enrollment
  WHERE enrollment.organization_id = @organizationID
    AND enrollment.effective_from <= @decisionAt
    AND (
      enrollment.effective_until IS NULL
      OR enrollment.effective_until > @decisionAt
    )
  ORDER BY
    enrollment.organization_id,
    enrollment.scope_kind,
    enrollment.scope_id,
    enrollment.revision DESC,
    enrollment.id DESC
),
current_observations AS (
  SELECT
    observation.organization_id,
    observation.deployment_unit_id,
    observation.component_instance_id,
    count(*) AS observation_count,
    count(DISTINCT observation.state_checksum) AS state_count,
    bool_or(observation.fresh_until < @decisionAt) AS stale,
    bool_or(observation.outcome = 'PARTIAL') AS partial,
    bool_or(observation.outcome = 'UNKNOWN') AS unknown,
    bool_or(observation.health = 'UNHEALTHY') AS unhealthy,
    bool_and(observation.health = 'HEALTHY') AS healthy,
    min(observation.artifact_digest) AS artifact_digest,
    min(observation.config_checksum) AS config_checksum,
    min(observation.schema_version) AS schema_version,
    min(observation.capability_checksum) AS capability_checksum,
    min(observation.platform) AS platform,
    min(observation.topology_checksum) AS topology_checksum
  FROM ObservedComponentState observation
  WHERE observation.organization_id = @organizationID
    AND observation.is_current
    AND observation.trusted
    AND observation.disposition = 'ACCEPTED'
  GROUP BY
    observation.organization_id,
    observation.deployment_unit_id,
    observation.component_instance_id
),
open_drift AS (
  SELECT DISTINCT ON (
    drift.organization_id,
    drift.deployment_unit_id,
    drift.component_instance_id
  )
    drift.organization_id,
    drift.deployment_unit_id,
    drift.component_instance_id,
    drift.status,
    drift.classes
  FROM DriftCase drift
  WHERE drift.organization_id = @organizationID
    AND drift.status IN ('OPEN', 'ASSIGNED', 'EXCEPTION')
  ORDER BY
    drift.organization_id,
    drift.deployment_unit_id,
    drift.component_instance_id,
    drift.updated_at DESC,
    drift.id DESC
),
latest_pending AS (
  SELECT DISTINCT ON (
    pending.organization_id,
    pending.deployment_unit_id,
    pending.component_instance_id
  )
    pending.organization_id,
    pending.deployment_unit_id,
    pending.component_instance_id,
    pending.execution_id,
    pending.status,
    pending.created_at,
    pending.id
  FROM PendingDesiredRevision pending
  WHERE pending.organization_id = @organizationID
  ORDER BY
    pending.organization_id,
    pending.deployment_unit_id,
    pending.component_instance_id,
    pending.created_at DESC,
    pending.id DESC
),
latest_attempt AS (
  SELECT DISTINCT ON (attempt.organization_id, attempt.execution_id)
    attempt.organization_id,
    attempt.execution_id,
    attempt.status
  FROM ExecutionAttempt attempt
  WHERE attempt.organization_id = @organizationID
  ORDER BY
    attempt.organization_id,
    attempt.execution_id,
    attempt.attempt_number DESC,
    attempt.created_at DESC,
    attempt.id DESC
),
release_lookup AS (
  SELECT DISTINCT ON (
    artifact.organization_id,
    artifact.component_key,
    artifact.platform,
    artifact.platform_digest
  )
    artifact.organization_id,
    artifact.component_key,
    artifact.platform,
    artifact.platform_digest,
    artifact.release_bundle_id,
    artifact.component_version
  FROM ComponentReleaseArtifact artifact
  WHERE artifact.organization_id = @organizationID
  ORDER BY
    artifact.organization_id,
    artifact.component_key,
    artifact.platform,
    artifact.platform_digest,
    artifact.release_bundle_id
),
authorized_registry AS (
  SELECT
    instance.id,
    instance.created_at,
    CASE
      WHEN cardinality(COALESCE(customers.customer_ids, ARRAY[]::uuid[])) = 1
      THEN customers.customer_ids[1]
    END AS customer_organization_id,
    COALESCE(customers.customers, '') AS customer,
    assignment.environment_id,
    environment.name AS environment,
    assignment.deployment_target_id,
    target.name AS target,
    unit.id AS deployment_unit_id,
    unit.name AS unit,
    definition.id AS component_id,
    definition.key AS component_key,
    definition.name AS component,
    desired.pending_revision_id,
    desired.active_revision_id,
    pending.execution_id AS pending_execution_id,
    pending.status AS pending_status,
    pending.artifact_digest AS pending_artifact_digest,
    pending.platform AS pending_platform,
    active.execution_id AS active_execution_id,
    active.artifact_digest AS active_artifact_digest,
    active.config_checksum AS active_config_checksum,
    active.schema_version AS active_schema_version,
    active.capability_checksum AS active_capability_checksum,
    active.platform AS active_platform,
    active.topology_checksum AS active_topology_checksum,
    active_release.release_bundle_id AS active_release_id,
    active_release.component_version AS active_release_version,
    pending_release.release_bundle_id AS pending_release_id,
    pending_release.component_version AS pending_release_version,
    observation.observation_count,
    observation.state_count,
    observation.stale AS observation_stale,
    observation.partial AS observation_partial,
    observation.unknown AS observation_unknown,
    observation.unhealthy AS observation_unhealthy,
    observation.healthy AS observation_healthy,
    observation.artifact_digest AS observed_artifact_digest,
    observation.config_checksum AS observed_config_checksum,
    observation.schema_version AS observed_schema_version,
    observation.capability_checksum AS observed_capability_checksum,
    observation.platform AS observed_platform,
    observation.topology_checksum AS observed_topology_checksum,
    drift.status AS drift_case_status,
    last_pending.execution_id AS last_execution_id,
    COALESCE(last_attempt.status::text, last_pending.status::text, '') AS last_execution,
    organization_enrollment.enabled AS organization_enrolled,
    environment_enrollment.enabled AS environment_enrolled
  FROM ComponentInstance instance
  JOIN DeploymentUnit unit
    ON unit.id = instance.deployment_unit_id
   AND unit.organization_id = instance.organization_id
  JOIN DeploymentScope scope
    ON scope.id = unit.deployment_scope_id
   AND scope.organization_id = unit.organization_id
  JOIN TargetEnvironmentAssignment assignment
    ON assignment.id = unit.target_environment_assignment_id
   AND assignment.organization_id = unit.organization_id
   AND assignment.deployment_target_id = unit.deployment_target_id
  JOIN Environment environment
    ON environment.id = assignment.environment_id
   AND environment.organization_id = assignment.organization_id
  JOIN DeploymentTarget target
    ON target.id = assignment.deployment_target_id
   AND target.organization_id = assignment.organization_id
  JOIN ComponentDefinition definition
    ON definition.id = instance.component_definition_id
   AND definition.organization_id = instance.organization_id
  LEFT JOIN subscriber_customers customers
    ON customers.organization_id = unit.organization_id
   AND customers.deployment_unit_id = unit.id
  LEFT JOIN ComponentDesiredStateHead desired
    ON desired.organization_id = instance.organization_id
   AND desired.deployment_unit_id = unit.id
   AND desired.component_instance_id = instance.id
  LEFT JOIN PendingDesiredRevision pending
    ON pending.id = desired.pending_revision_id
   AND pending.organization_id = desired.organization_id
  LEFT JOIN ActiveDesiredRevision active
    ON active.id = desired.active_revision_id
   AND active.organization_id = desired.organization_id
  LEFT JOIN release_lookup active_release
    ON active_release.organization_id = active.organization_id
   AND active_release.component_key = active.component_key
   AND active_release.platform = active.platform
   AND active_release.platform_digest = active.artifact_digest
  LEFT JOIN release_lookup pending_release
    ON pending_release.organization_id = pending.organization_id
   AND pending_release.component_key = pending.component_key
   AND pending_release.platform = pending.platform
   AND pending_release.platform_digest = pending.artifact_digest
  LEFT JOIN current_observations observation
    ON observation.organization_id = instance.organization_id
   AND observation.deployment_unit_id = unit.id
   AND observation.component_instance_id = instance.id
  LEFT JOIN open_drift drift
    ON drift.organization_id = instance.organization_id
   AND drift.deployment_unit_id = unit.id
   AND drift.component_instance_id = instance.id
  LEFT JOIN latest_pending last_pending
    ON last_pending.organization_id = instance.organization_id
   AND last_pending.deployment_unit_id = unit.id
   AND last_pending.component_instance_id = instance.id
  LEFT JOIN latest_attempt last_attempt
    ON last_attempt.organization_id = last_pending.organization_id
   AND last_attempt.execution_id = last_pending.execution_id
  LEFT JOIN effective_enrollment organization_enrollment
    ON organization_enrollment.organization_id = instance.organization_id
   AND organization_enrollment.scope_kind = 'organization'
   AND organization_enrollment.scope_id = instance.organization_id
  LEFT JOIN effective_enrollment environment_enrollment
    ON environment_enrollment.organization_id = instance.organization_id
   AND environment_enrollment.scope_kind = 'environment'
   AND environment_enrollment.scope_id = assignment.environment_id
  WHERE instance.organization_id = @organizationID
    AND instance.retired_at IS NULL
    AND unit.retired_at IS NULL
    AND scope.retired_at IS NULL
    AND assignment.active_from <= @decisionAt
    AND (assignment.active_until IS NULL OR assignment.active_until > @decisionAt)
    AND (
      @organizationWide
      OR scope.customer_organization_id = ANY(@customerScopeIDs::uuid[])
      OR EXISTS (
        SELECT 1
        FROM unnest(COALESCE(customers.customer_ids, ARRAY[]::uuid[])) customer_scope_id
        WHERE customer_scope_id = ANY(@customerScopeIDs::uuid[])
      )
      OR assignment.environment_id = ANY(@environmentScopeIDs::uuid[])
      OR unit.id = ANY(@deploymentUnitScopeIDs::uuid[])
      OR definition.id = ANY(@componentScopeIDs::uuid[])
    )
    AND (
      @customerOrganizationID::uuid IS NULL
      OR scope.customer_organization_id = @customerOrganizationID
      OR @customerOrganizationID = ANY(
        COALESCE(customers.customer_ids, ARRAY[]::uuid[])
      )
    )
    AND (
      @environmentID::uuid IS NULL
      OR assignment.environment_id = @environmentID
    )
    AND (
      @deploymentTargetID::uuid IS NULL
      OR assignment.deployment_target_id = @deploymentTargetID
    )
    AND (
      @deploymentUnitID::uuid IS NULL
      OR unit.id = @deploymentUnitID
    )
    AND (
      @component = ''
      OR lower(definition.key) LIKE '%' || @component || '%'
      OR lower(definition.name) LIKE '%' || @component || '%'
      OR lower(instance.physical_name) LIKE '%' || @component || '%'
    )
),
projected_fleet AS (
  SELECT
    registry.id,
    registry.created_at,
    registry.customer_organization_id,
    registry.customer,
    registry.environment_id,
    registry.environment,
    registry.deployment_target_id,
    registry.target,
    registry.deployment_unit_id,
    registry.unit,
    registry.component_id,
    registry.component,
    registry.active_release_id,
    COALESCE(registry.active_release_version, registry.active_artifact_digest, '')
      AS active_release,
    registry.pending_release_id,
    COALESCE(registry.pending_release_version, registry.pending_artifact_digest, '')
      AS pending_release,
    CASE
      WHEN registry.observation_count IS NULL THEN 'unknown'
      WHEN registry.state_count > 1 THEN 'conflict'
      WHEN registry.observation_stale THEN 'stale'
      WHEN registry.observation_partial THEN 'partial'
      WHEN registry.observation_unknown THEN 'unknown'
      WHEN registry.observation_unhealthy THEN 'unhealthy'
      WHEN registry.observation_healthy THEN 'healthy'
      ELSE 'unknown'
    END AS observed_state,
    CASE
      WHEN registry.drift_case_status IS NOT NULL THEN 'drifted'
      WHEN registry.active_revision_id IS NULL
        OR registry.observation_count IS NULL THEN 'unknown'
      WHEN registry.state_count > 1 THEN 'conflict'
      WHEN registry.observation_stale THEN 'stale'
      WHEN registry.observed_artifact_digest IS DISTINCT FROM registry.active_artifact_digest
        OR registry.observed_config_checksum IS DISTINCT FROM registry.active_config_checksum
        OR registry.observed_schema_version IS DISTINCT FROM registry.active_schema_version
        OR registry.observed_capability_checksum IS DISTINCT FROM registry.active_capability_checksum
        OR registry.observed_platform IS DISTINCT FROM registry.active_platform
        OR registry.observed_topology_checksum IS DISTINCT FROM registry.active_topology_checksum
      THEN 'drifted'
      ELSE 'in_sync'
    END AS drift,
    registry.last_execution_id,
    registry.last_execution,
    CASE
      WHEN registry.organization_enrolled IS NULL
        OR registry.environment_enrolled IS NULL THEN 'unknown'
      WHEN registry.organization_enrolled AND registry.environment_enrolled
        THEN 'enabled'
      ELSE 'disabled'
    END AS enrollment
  FROM authorized_registry registry
),
filtered_fleet AS (
  SELECT *
  FROM projected_fleet fleet
  WHERE (@observedState = '' OR fleet.observed_state = @observedState)
    AND (@drift = '' OR fleet.drift = @drift)
    AND (@enrollment = '' OR fleet.enrollment = @enrollment)
    AND (
      @search = ''
      OR lower(concat_ws(
        ' ', fleet.customer, fleet.environment, fleet.target,
        fleet.unit, fleet.component, fleet.active_release,
        fleet.pending_release, fleet.observed_state, fleet.drift,
        fleet.last_execution, fleet.enrollment
      )) LIKE '%' || @search || '%'
    )
),
paged_fleet AS (
  SELECT *
  FROM filtered_fleet
  WHERE (
    @cursorCreatedAt::timestamptz IS NULL
    OR (created_at, id) < (@cursorCreatedAt::timestamptz, @cursorID::uuid)
  )
  ORDER BY created_at DESC, id DESC
  LIMIT @fetchLimit
)
SELECT jsonb_build_object(
  'total', (SELECT count(*) FROM filtered_fleet),
  'items', COALESCE((
    SELECT jsonb_agg(
      jsonb_build_object(
        'id', fleet.id,
        'createdAt', fleet.created_at,
        'customerOrganizationId', fleet.customer_organization_id,
        'customer', fleet.customer,
        'environmentId', fleet.environment_id,
        'environment', fleet.environment,
        'deploymentTargetId', fleet.deployment_target_id,
        'target', fleet.target,
        'deploymentUnitId', fleet.deployment_unit_id,
        'unit', fleet.unit,
        'componentId', fleet.component_id,
        'component', fleet.component,
        'activeReleaseId', fleet.active_release_id,
        'activeRelease', fleet.active_release,
        'pendingReleaseId', fleet.pending_release_id,
        'pendingRelease', fleet.pending_release,
        'observedState', fleet.observed_state,
        'drift', fleet.drift,
        'lastExecutionId', fleet.last_execution_id,
        'lastExecution', fleet.last_execution,
        'enrollment', fleet.enrollment
      ) ORDER BY fleet.created_at DESC, fleet.id DESC
    )
    FROM paged_fleet fleet
  ), '[]'::jsonb)
)::text`

type OperatorFleetCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type OperatorFleetQuery struct {
	OrganizationID         uuid.UUID
	DecisionAt             time.Time
	OrganizationWide       bool
	CustomerScopeIDs       []uuid.UUID
	EnvironmentScopeIDs    []uuid.UUID
	DeploymentUnitScopeIDs []uuid.UUID
	ComponentScopeIDs      []uuid.UUID
	Filter                 types.FleetFilter
	Cursor                 *OperatorFleetCursor
	Limit                  int
}

type OperatorFleetResult struct {
	Items []types.FleetRow
	Total int64
}

type operatorFleetPayload struct {
	Items []types.FleetRow `json:"items"`
	Total int64            `json:"total"`
}

func ListOperatorFleetRows(
	ctx context.Context,
	query OperatorFleetQuery,
) (OperatorFleetResult, error) {
	result := OperatorFleetResult{Items: []types.FleetRow{}}
	if err := validateOperatorFleetQuery(query); err != nil {
		return result, err
	}
	var cursorCreatedAt any
	var cursorID any
	if query.Cursor != nil {
		cursorCreatedAt = query.Cursor.CreatedAt.UTC()
		cursorID = query.Cursor.ID
	}
	var payload string
	err := internalctx.GetDb(ctx).QueryRow(ctx, operatorFleetSQL, pgx.NamedArgs{
		"organizationID":         query.OrganizationID,
		"decisionAt":             query.DecisionAt.UTC(),
		"organizationWide":       query.OrganizationWide,
		"customerScopeIDs":       nonNilUUIDs(query.CustomerScopeIDs),
		"environmentScopeIDs":    nonNilUUIDs(query.EnvironmentScopeIDs),
		"deploymentUnitScopeIDs": nonNilUUIDs(query.DeploymentUnitScopeIDs),
		"componentScopeIDs":      nonNilUUIDs(query.ComponentScopeIDs),
		"customerOrganizationID": query.Filter.CustomerOrganizationID,
		"environmentID":          query.Filter.EnvironmentID,
		"deploymentTargetID":     query.Filter.DeploymentTargetID,
		"deploymentUnitID":       query.Filter.DeploymentUnitID,
		"component":              query.Filter.Component,
		"observedState":          query.Filter.ObservedState,
		"drift":                  query.Filter.Drift,
		"enrollment":             query.Filter.Enrollment,
		"search":                 query.Filter.Search,
		"cursorCreatedAt":        cursorCreatedAt,
		"cursorID":               cursorID,
		"fetchLimit":             query.Limit,
	}).Scan(&payload)
	if err != nil {
		return result, fmt.Errorf("could not query operator fleet: %w", err)
	}
	var decoded operatorFleetPayload
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return result, fmt.Errorf("could not decode operator fleet: %w", err)
	}
	if decoded.Items == nil {
		decoded.Items = []types.FleetRow{}
	}
	return OperatorFleetResult{Items: decoded.Items, Total: decoded.Total}, nil
}

func validateOperatorFleetQuery(query OperatorFleetQuery) error {
	if query.OrganizationID == uuid.Nil || query.DecisionAt.IsZero() ||
		query.Limit < 1 || query.Limit > types.OperatorMaximumPageLimit+1 {
		return apierrors.ErrBadRequest
	}
	if err := validateCanonicalOperatorScopeFilter(types.OperatorScopeFilter{
		OrganizationID: query.OrganizationID, DecisionAt: query.DecisionAt,
		OrganizationWide: query.OrganizationWide,
		CustomerIDs:      query.CustomerScopeIDs, EnvironmentIDs: query.EnvironmentScopeIDs,
		DeploymentUnitIDs: query.DeploymentUnitScopeIDs, ComponentIDs: query.ComponentScopeIDs,
		CampaignIDs: []uuid.UUID{},
	}); err != nil {
		return err
	}
	if query.Cursor != nil &&
		(query.Cursor.CreatedAt.IsZero() || query.Cursor.ID == uuid.Nil) {
		return apierrors.ErrBadRequest
	}
	return nil
}

func nonNilUUIDs(values []uuid.UUID) []uuid.UUID {
	if values == nil {
		return []uuid.UUID{}
	}
	return values
}
