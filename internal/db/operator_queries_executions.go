package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Operator execution reads are deliberately single-statement projections.
// Keeping nested evidence in lateral aggregates makes the query count
// independent of page size and of the number of retained attempts.
const (
	operatorExecutionListQueryCount   = 1
	operatorExecutionDetailQueryCount = 1
)

const operatorExecutionCampaignSQL = `
  SELECT
    lineage.organization_id,
    lineage.task_id,
    revision.deployment_campaign_draft_id AS campaign_id,
    run.id AS campaign_run_id,
    run.campaign_revision_id
  FROM CampaignMemberTaskExecution AS lineage
  JOIN DeploymentCampaignMemberRun AS member
    ON member.id = lineage.campaign_member_run_id
   AND member.organization_id = lineage.organization_id
   AND member.campaign_run_id = lineage.campaign_run_id
  JOIN DeploymentCampaignRun AS run
    ON run.id = member.campaign_run_id
   AND run.organization_id = member.organization_id
  JOIN DeploymentCampaignRevision AS revision
    ON revision.id = run.campaign_revision_id
   AND revision.organization_id = run.organization_id`

const operatorExecutionVisibleScopeSQL = `
    AND (
      @organizationWide
      OR task.environment_id = ANY(@environmentIDs::uuid[])
      OR campaign.campaign_id = ANY(@campaignIDs::uuid[])
      OR EXISTS (
        SELECT 1
		FROM PendingDesiredRevision AS scoped_desired
		JOIN DeploymentUnit AS scoped_unit
		  ON scoped_unit.id = scoped_desired.deployment_unit_id
		 AND scoped_unit.organization_id = scoped_desired.organization_id
		JOIN DeploymentScope AS scoped_scope
		  ON scoped_scope.id = scoped_unit.deployment_scope_id
		 AND scoped_scope.organization_id = scoped_unit.organization_id
		JOIN ComponentInstance AS scoped_component
		  ON scoped_component.id = scoped_desired.component_instance_id
		 AND scoped_component.deployment_unit_id = scoped_desired.deployment_unit_id
		 AND scoped_component.organization_id = scoped_desired.organization_id
		LEFT JOIN DeploymentUnitSubscriber AS scoped_subscriber
		  ON scoped_subscriber.deployment_unit_id = scoped_unit.id
		 AND scoped_subscriber.organization_id = scoped_unit.organization_id
		 AND scoped_subscriber.retired_at IS NULL
		WHERE scoped_desired.organization_id = attempt.organization_id
		  AND scoped_desired.execution_attempt_id = attempt.id
		  AND (
			scoped_unit.id = ANY(@deploymentUnitIDs::uuid[])
			OR scoped_component.id = ANY(@componentIDs::uuid[])
			OR scoped_component.component_definition_id = ANY(@componentIDs::uuid[])
			OR scoped_scope.customer_organization_id = ANY(@customerIDs::uuid[])
			OR scoped_subscriber.customer_organization_id = ANY(@customerIDs::uuid[])
		  )
      )
    )`

const operatorExecutionListSQL = `
WITH campaign AS (` + operatorExecutionCampaignSQL + `
),
filtered AS (
  SELECT
    attempt.id,
    attempt.created_at,
    attempt.organization_id,
    attempt.execution_id,
    attempt.attempt_number,
    attempt.task_id,
    attempt.step_run_id,
    attempt.step_key,
    attempt.status,
    attempt.deployment_target_id,
    attempt.plan_checksum,
    attempt.artifact_digest,
    attempt.config_checksum,
    attempt.runtime_manifest_checksum,
    attempt.desired_service_config_checksum,
    attempt.expected_current_config_checksum,
    attempt.expected_current_service_config_checksum,
    attempt.adapter_revision,
    attempt.completed_at,
    attempt.cancellable,
    task.deployment_plan_id,
    task.environment_id,
    task.protocol_version,
    campaign.campaign_id
  FROM ExecutionAttempt AS attempt
  JOIN Task AS task
    ON task.id = attempt.task_id
   AND task.organization_id = attempt.organization_id
  JOIN StepRun AS step
    ON step.id = attempt.step_run_id
   AND step.task_id = attempt.task_id
   AND step.organization_id = attempt.organization_id
  LEFT JOIN campaign
    ON campaign.task_id = attempt.task_id
   AND campaign.organization_id = attempt.organization_id
  WHERE attempt.organization_id = @organizationID
    AND (@status = '' OR attempt.status = @status)
    AND (@campaignID::uuid IS NULL OR campaign.campaign_id = @campaignID)
    AND (@deploymentPlanID::uuid IS NULL OR task.deployment_plan_id = @deploymentPlanID)
    AND (@deploymentTargetID::uuid IS NULL OR attempt.deployment_target_id = @deploymentTargetID)
    AND (@fromTime::timestamptz IS NULL OR attempt.created_at >= @fromTime)
    AND (@toTime::timestamptz IS NULL OR attempt.created_at < @toTime)
    AND (
      @cursorCreatedAt::timestamptz IS NULL
      OR (attempt.created_at, attempt.id) < (@cursorCreatedAt, @cursorID)
    )
` + operatorExecutionVisibleScopeSQL + `
  ORDER BY attempt.created_at DESC, attempt.id DESC
  LIMIT @limitPlusOne
)
SELECT
  filtered.id,
  filtered.created_at,
  filtered.campaign_id,
  filtered.deployment_plan_id,
  filtered.deployment_target_id,
  filtered.task_id,
  filtered.step_run_id,
  filtered.step_key,
  filtered.attempt_number,
  filtered.protocol_version,
  filtered.status,
  filtered.plan_checksum,
  filtered.artifact_digest,
  filtered.config_checksum,
  filtered.runtime_manifest_checksum,
  filtered.desired_service_config_checksum,
  filtered.expected_current_config_checksum,
  filtered.expected_current_service_config_checksum,
  filtered.adapter_revision,
  filtered.completed_at,
  filtered.cancellable,
  CASE
    WHEN reconciliation.outcome IS NOT NULL THEN reconciliation.outcome
    WHEN filtered.status = 'UNKNOWN' THEN 'UNKNOWN'
    WHEN control.status_query_status IN ('PENDING', 'EXPIRED') THEN control.status_query_status
    ELSE 'NONE'
  END AS reconciliation,
  CASE
    WHEN observation.id IS NULL THEN 'UNKNOWN'
    WHEN observation.fresh_until < @decisionAt THEN 'STALE'
    ELSE observation.outcome
  END AS observation
FROM filtered
LEFT JOIN ExecutionIntent AS intent
  ON intent.execution_attempt_id = filtered.id
 AND intent.organization_id = filtered.organization_id
LEFT JOIN DeploymentPlanStepAdapter AS adapter
  ON adapter.deployment_plan_id = filtered.deployment_plan_id
 AND adapter.step_key = filtered.step_key
 AND adapter.organization_id = filtered.organization_id
LEFT JOIN DeploymentPlan AS plan
  ON plan.id = filtered.deployment_plan_id
 AND plan.organization_id = filtered.organization_id
LEFT JOIN LATERAL (
  SELECT
    cancel.id AS cancel_request_id,
    cancel.status AS cancel_status,
    status_query.id AS status_query_id,
    status_query.status AS status_query_status
  FROM (SELECT 1) AS singleton
  LEFT JOIN LATERAL (
    SELECT request.id, request.status
    FROM ExecutionCancelRequest AS request
    WHERE request.organization_id = filtered.organization_id
      AND request.execution_id = filtered.execution_id
      AND request.execution_attempt_id = filtered.id
    ORDER BY request.created_at DESC, request.id DESC
    LIMIT 1
  ) AS cancel ON TRUE
  LEFT JOIN LATERAL (
    SELECT query.id, query.status
    FROM ExecutionStatusQuery AS query
    WHERE query.organization_id = filtered.organization_id
      AND query.execution_id = filtered.execution_id
      AND query.execution_attempt_id = filtered.id
    ORDER BY query.created_at DESC, query.id DESC
    LIMIT 1
  ) AS status_query ON TRUE
) AS control ON TRUE
LEFT JOIN LATERAL (
  SELECT event.outcome, event.evidence_checksum
  FROM ExecutionReconciliationEvent AS event
  WHERE event.organization_id = filtered.organization_id
    AND event.execution_id = filtered.execution_id
    AND event.execution_attempt_id = filtered.id
  ORDER BY event.created_at DESC, event.id DESC
  LIMIT 1
) AS reconciliation ON TRUE
LEFT JOIN LATERAL (
  SELECT revision.status, revision.verified_observation_id,
    revision.terminal_observation_id
  FROM PendingDesiredRevision AS revision
  WHERE revision.organization_id = filtered.organization_id
    AND revision.execution_id = filtered.execution_id
    AND revision.execution_attempt_id = filtered.id
  ORDER BY revision.created_at DESC, revision.id DESC
  LIMIT 1
) AS desired ON TRUE
LEFT JOIN ObservedComponentState AS observation
  ON observation.id = COALESCE(
    desired.verified_observation_id,
    desired.terminal_observation_id
  )
 AND observation.organization_id = filtered.organization_id
ORDER BY filtered.created_at DESC, filtered.id DESC`

const operatorExecutionDetailSQL = `
WITH campaign AS (` + operatorExecutionCampaignSQL + `
),
attempt AS (
  SELECT
    candidate.*,
    task.protocol_version,
	task.status AS task_status,
    task.deployment_plan_id,
    task.environment_id,
    step.name AS step_name,
    step.action_type,
    step.sort_order,
	campaign.campaign_id,
    plan.previous_state_source_plan_id,
    fence.generation AS fence_generation,
    fence.resource_key AS fence_resource_key,
	fence.lease_expires_at AS fence_lease_expires_at,
	fence.released_at AS fence_released_at,
    COALESCE(external.idempotency_key, '') AS idempotency_key
  FROM ExecutionAttempt AS candidate
  JOIN Task AS task
    ON task.id = candidate.task_id
   AND task.organization_id = candidate.organization_id
  JOIN StepRun AS step
    ON step.id = candidate.step_run_id
   AND step.task_id = candidate.task_id
   AND step.organization_id = candidate.organization_id
  JOIN DeploymentPlan AS plan
    ON plan.id = task.deployment_plan_id
   AND plan.organization_id = task.organization_id
  JOIN ExecutionFence AS fence
    ON fence.execution_attempt_id = candidate.id
   AND fence.organization_id = candidate.organization_id
  LEFT JOIN ExternalExecution AS external
    ON external.id = candidate.execution_id
   AND external.organization_id = candidate.organization_id
  LEFT JOIN campaign
    ON campaign.task_id = candidate.task_id
   AND campaign.organization_id = candidate.organization_id
  WHERE candidate.organization_id = @organizationID
    AND candidate.id = @executionID
    AND (
      @organizationWide
      OR task.environment_id = ANY(@environmentIDs::uuid[])
      OR campaign.campaign_id = ANY(@campaignIDs::uuid[])
      OR EXISTS (
		SELECT 1
		FROM PendingDesiredRevision AS scoped_desired
		JOIN DeploymentUnit AS scoped_unit
		  ON scoped_unit.id = scoped_desired.deployment_unit_id
		 AND scoped_unit.organization_id = scoped_desired.organization_id
		JOIN DeploymentScope AS scoped_scope
		  ON scoped_scope.id = scoped_unit.deployment_scope_id
		 AND scoped_scope.organization_id = scoped_unit.organization_id
		JOIN ComponentInstance AS scoped_component
		  ON scoped_component.id = scoped_desired.component_instance_id
		 AND scoped_component.deployment_unit_id = scoped_desired.deployment_unit_id
		 AND scoped_component.organization_id = scoped_desired.organization_id
		LEFT JOIN DeploymentUnitSubscriber AS scoped_subscriber
		  ON scoped_subscriber.deployment_unit_id = scoped_unit.id
		 AND scoped_subscriber.organization_id = scoped_unit.organization_id
		 AND scoped_subscriber.retired_at IS NULL
		WHERE scoped_desired.organization_id = candidate.organization_id
		  AND scoped_desired.execution_attempt_id = candidate.id
		  AND (
			scoped_unit.id = ANY(@deploymentUnitIDs::uuid[])
			OR scoped_component.id = ANY(@componentIDs::uuid[])
			OR scoped_component.component_definition_id = ANY(@componentIDs::uuid[])
			OR scoped_scope.customer_organization_id = ANY(@customerIDs::uuid[])
			OR scoped_subscriber.customer_organization_id = ANY(@customerIDs::uuid[])
		  )
      )
    )
),
detail AS (
  SELECT jsonb_build_object(
    'execution', jsonb_build_object(
      'id', attempt.id,
      'createdAt', attempt.created_at,
      'campaignId', attempt.campaign_id,
      'deploymentPlanId', attempt.deployment_plan_id,
      'deploymentTargetId', attempt.deployment_target_id,
      'taskId', attempt.task_id,
      'stepRunId', attempt.step_run_id,
      'stepKey', attempt.step_key,
      'attemptNumber', attempt.attempt_number,
      'protocolVersion', attempt.protocol_version,
      'status', attempt.status,
      'planChecksum', attempt.plan_checksum,
      'artifactDigest', attempt.artifact_digest,
      'configChecksum', attempt.config_checksum,
      'runtimeManifestChecksum', attempt.runtime_manifest_checksum,
      'desiredServiceConfigChecksum', attempt.desired_service_config_checksum,
      'expectedCurrentConfigChecksum', attempt.expected_current_config_checksum,
      'expectedCurrentServiceConfigChecksum', attempt.expected_current_service_config_checksum,
      'adapterRevision', attempt.adapter_revision,
      'completedAt', attempt.completed_at,
      'cancellable', attempt.cancellable,
      'reconciliation', COALESCE(reconciliation.outcome, CASE WHEN attempt.status = 'UNKNOWN' THEN 'UNKNOWN' ELSE 'NONE' END),
      'observation', CASE
        WHEN observation.id IS NULL THEN 'UNKNOWN'
        WHEN observation.fresh_until < @decisionAt THEN 'STALE'
        ELSE observation.outcome
      END,
      'fenceGeneration', attempt.fence_generation,
      'fenceResourceKey', attempt.fence_resource_key,
      'idempotencyKey', attempt.idempotency_key
    ),
    'intent', CASE WHEN intent.id IS NULL THEN NULL ELSE jsonb_build_object(
      'id', intent.id, 'key', 'intent', 'kind', 'signed-intent',
      'status', attempt.status, 'checksum', intent.checksum,
      'keyId', intent.key_id, 'blocking', false, 'order', 0
    ) END,
    'adapter', CASE WHEN adapter.id IS NULL THEN NULL ELSE jsonb_build_object(
      'id', adapter.id, 'key', adapter.step_key, 'kind', 'adapter',
      'status', adapter.implementation_version,
      'expected', adapter.capability || '@' || adapter.capability_version,
      'actual', adapter.scope_type || ':' || adapter.scope_reference,
      'checksum', adapter.config_checksum, 'blocking', false, 'order', adapter.sort_order
    ) END,
    'cancellation', cancellation.fact,
    'reconciliation', reconciliation.fact,
    'previousState', CASE WHEN attempt.previous_state_source_plan_id IS NULL THEN NULL ELSE jsonb_build_object(
      'id', attempt.previous_state_source_plan_id, 'key', 'previous-state',
      'kind', 'deployment-plan', 'status', 'retained',
      'message', 'new plan preserves previous-state lineage',
      'blocking', false, 'order', 0
    ) END,
    'tasks', jsonb_build_array(jsonb_build_object(
      'id', attempt.task_id, 'key', attempt.task_id::text, 'kind', 'task',
      'status', attempt.status, 'blocking', false, 'order', 0
    )),
    'steps', jsonb_build_array(jsonb_build_object(
      'id', attempt.step_run_id, 'key', attempt.step_key, 'kind', attempt.action_type,
      'status', attempt.status, 'message', attempt.step_name,
      'blocking', false, 'order', attempt.sort_order
    )),
    'attempts', attempts.items,
	'locks', locks.items,
	'leases', leases.items,
	'coordination', jsonb_build_object(
	  'inFlight', attempt.status IN ('PENDING', 'CLAIMED', 'RUNNING'),
	  'activeLockCount', locks.active_count,
	  'unreleasedLockCount', locks.unreleased_count,
	  'activeLeaseCount', leases.active_count,
	  'unreleasedLeaseCount', leases.unreleased_count,
	  'fenceStatus', CASE
		WHEN attempt.fence_released_at IS NOT NULL THEN 'RELEASED'
		WHEN attempt.fence_lease_expires_at <= @decisionAt THEN 'EXPIRED'
		ELSE 'ACTIVE'
	  END,
	  'fenceGeneration', attempt.fence_generation,
	  'fenceLeaseExpiresAt', attempt.fence_lease_expires_at,
	  'fenceReleasedAt', attempt.fence_released_at,
	  'timedOut', attempt.status = 'TIMED_OUT',
	  'reconciliationRequired', attempt.status IN ('UNKNOWN', 'TIMED_OUT')
		AND COALESCE(reconciliation.outcome, 'UNKNOWN') = 'UNKNOWN',
	  'zeroLockClosure', attempt.status IN ('SUCCEEDED', 'FAILED', 'CANCELED', 'TIMED_OUT', 'FENCED', 'UNKNOWN')
		AND locks.unreleased_count = 0
		AND leases.unreleased_count = 0
		AND attempt.fence_released_at IS NOT NULL
	),
    'observations', observations.items,
    'evidence', evidence.items
  ) AS payload
  FROM attempt
  LEFT JOIN ExecutionIntent AS intent
    ON intent.execution_attempt_id = attempt.id
   AND intent.organization_id = attempt.organization_id
  LEFT JOIN DeploymentPlanStepAdapter AS adapter
    ON adapter.deployment_plan_id = attempt.deployment_plan_id
   AND adapter.step_key = attempt.step_key
   AND adapter.organization_id = attempt.organization_id
  LEFT JOIN LATERAL (
    SELECT jsonb_build_object(
      'id', control.id, 'key', 'cancel', 'kind', 'cancel-request',
      'status', control.status, 'message', control.reason,
      'blocking', control.status = 'REQUESTED', 'order', 0
    ) AS fact
    FROM ExecutionCancelRequest AS control
    WHERE control.organization_id = attempt.organization_id
      AND control.execution_id = attempt.execution_id
      AND control.execution_attempt_id = attempt.id
    ORDER BY control.created_at DESC, control.id DESC
    LIMIT 1
  ) AS cancellation ON TRUE
  LEFT JOIN LATERAL (
    SELECT event.outcome, jsonb_build_object(
      'id', event.id, 'key', 'reconciliation', 'kind', 'status-evidence',
      'status', event.outcome, 'checksum', event.evidence_checksum,
      'message', event.retry_disposition, 'blocking', event.outcome = 'UNKNOWN',
      'order', 0
    ) AS fact
    FROM ExecutionReconciliationEvent AS event
    WHERE event.organization_id = attempt.organization_id
      AND event.execution_id = attempt.execution_id
      AND event.execution_attempt_id = attempt.id
    ORDER BY event.created_at DESC, event.id DESC
    LIMIT 1
  ) AS reconciliation ON TRUE
  LEFT JOIN LATERAL (
    SELECT observed.*
    FROM PendingDesiredRevision AS desired
    JOIN ObservedComponentState AS observed
      ON observed.id = COALESCE(desired.verified_observation_id, desired.terminal_observation_id)
     AND observed.organization_id = desired.organization_id
    WHERE desired.organization_id = attempt.organization_id
      AND desired.execution_id = attempt.execution_id
      AND desired.execution_attempt_id = attempt.id
    ORDER BY desired.created_at DESC, desired.id DESC
    LIMIT 1
  ) AS observation ON TRUE
  LEFT JOIN LATERAL (
    SELECT COALESCE(jsonb_agg(jsonb_build_object(
      'id', retry.id, 'stepKey', retry.step_key,
      'status', retry.status, 'attemptNumber', retry.attempt_number,
      'planChecksum', retry.plan_checksum,
      'artifactDigest', retry.artifact_digest,
      'configChecksum', retry.config_checksum,
      'runtimeManifestChecksum', retry.runtime_manifest_checksum,
      'desiredServiceConfigChecksum', retry.desired_service_config_checksum,
      'expectedCurrentConfigChecksum', retry.expected_current_config_checksum,
      'expectedCurrentServiceConfigChecksum', retry.expected_current_service_config_checksum,
      'fenceGeneration', fence.generation,
      'fenceResourceKey', fence.resource_key,
      'idempotencyKey', external.idempotency_key,
      'message', retry.failure_reason,
      'blocking', retry.status IN ('UNKNOWN', 'FENCED')
    ) ORDER BY retry.attempt_number, retry.created_at, retry.id), '[]'::jsonb) AS items
    FROM ExecutionAttempt AS retry
    JOIN ExecutionFence AS fence
      ON fence.execution_attempt_id = retry.id
     AND fence.organization_id = retry.organization_id
    LEFT JOIN ExternalExecution AS external
      ON external.id = retry.execution_id
     AND external.organization_id = retry.organization_id
    WHERE retry.organization_id = attempt.organization_id
      AND retry.execution_id = attempt.execution_id
      AND retry.step_key = attempt.step_key
  ) AS attempts ON TRUE
	LEFT JOIN LATERAL (
	  SELECT
		COALESCE(jsonb_agg(jsonb_build_object(
		  'id', lockrow.id,
		  'resourceType', lockrow.resource_type,
		  'resourceKey', lockrow.resource_key,
		  'concurrencyPolicy', lockrow.concurrency_policy,
		  'status', CASE
			WHEN lockrow.released_at IS NOT NULL THEN 'RELEASED'
			WHEN lockrow.acquired_at IS NOT NULL THEN 'ACQUIRED'
			WHEN EXISTS (
			  SELECT 1
			  FROM TaskResourceLock AS competing
			  JOIN Task AS competing_task
				ON competing_task.id = competing.task_id
			   AND competing_task.organization_id = competing.organization_id
			  WHERE competing.organization_id = lockrow.organization_id
				AND competing.resource_type = lockrow.resource_type
				AND competing.resource_key = lockrow.resource_key
				AND competing.task_id <> lockrow.task_id
				AND competing.acquired_at IS NOT NULL
				AND competing.released_at IS NULL
				AND competing_task.status = 'RUNNING'
			) THEN 'CONFLICTED'
			ELSE 'WAITING'
		  END,
		  'createdAt', lockrow.created_at,
		  'acquiredAt', lockrow.acquired_at,
		  'releasedAt', lockrow.released_at,
		  'currentConflict', lockrow.released_at IS NULL AND EXISTS (
			SELECT 1
			FROM TaskResourceLock AS competing
			JOIN Task AS competing_task
			  ON competing_task.id = competing.task_id
			 AND competing_task.organization_id = competing.organization_id
			WHERE competing.organization_id = lockrow.organization_id
			  AND competing.resource_type = lockrow.resource_type
			  AND competing.resource_key = lockrow.resource_key
			  AND competing.task_id <> lockrow.task_id
			  AND competing.acquired_at IS NOT NULL
			  AND competing.released_at IS NULL
			  AND competing_task.status = 'RUNNING'
		  ),
		  'releaseReason', CASE
			WHEN lockrow.released_at IS NULL THEN ''
			WHEN attempt.status IN ('SUCCEEDED', 'FAILED', 'CANCELED', 'TIMED_OUT', 'FENCED', 'UNKNOWN')
			  THEN 'derived from terminal execution attempt: ' || attempt.status
			WHEN attempt.task_status IN ('SUCCEEDED', 'FAILED', 'CANCELED')
			  THEN 'derived from terminal task: ' || attempt.task_status
			ELSE 'retained release; no terminal reason is recorded'
		  END
		) ORDER BY lockrow.created_at, lockrow.id), '[]'::jsonb) AS items,
		count(*) FILTER (WHERE lockrow.acquired_at IS NOT NULL AND lockrow.released_at IS NULL)::int AS active_count,
		count(*) FILTER (WHERE lockrow.released_at IS NULL)::int AS unreleased_count
	  FROM TaskResourceLock AS lockrow
	  WHERE lockrow.organization_id = attempt.organization_id
		AND lockrow.task_id = attempt.task_id
	) AS locks ON TRUE
	LEFT JOIN LATERAL (
	  SELECT
		COALESCE(jsonb_agg(jsonb_build_object(
		  'id', lease.id,
		  'executorType', lease.executor_type,
		  'attempt', lease.attempt,
		  'status', CASE
			WHEN lease.released_at IS NOT NULL THEN 'RELEASED'
			WHEN lease.expires_at <= @decisionAt THEN 'EXPIRED'
			ELSE 'ACTIVE'
		  END,
		  'leasedAt', lease.leased_at,
		  'expiresAt', lease.expires_at,
		  'heartbeatAt', lease.heartbeat_at,
		  'releasedAt', lease.released_at,
		  'releaseReason', CASE
			WHEN lease.released_at IS NULL THEN ''
			WHEN lease.expires_at <= lease.released_at THEN 'derived from lease expiry or reclaim'
			WHEN attempt.task_status IN ('SUCCEEDED', 'FAILED', 'CANCELED')
			  THEN 'derived from terminal task: ' || attempt.task_status
			ELSE 'retained release; no terminal reason is recorded'
		  END
		) ORDER BY lease.attempt, lease.leased_at, lease.id), '[]'::jsonb) AS items,
		count(*) FILTER (WHERE lease.released_at IS NULL AND lease.expires_at > @decisionAt)::int AS active_count,
		count(*) FILTER (WHERE lease.released_at IS NULL)::int AS unreleased_count
	  FROM TaskLease AS lease
	  WHERE lease.organization_id = attempt.organization_id
		AND lease.task_id = attempt.task_id
	) AS leases ON TRUE
  LEFT JOIN LATERAL (
    SELECT COALESCE(jsonb_agg(jsonb_build_object(
      'id', observed.id, 'key', desired.component_key, 'kind', 'independent-observation',
	  'status', CASE
		WHEN observed.id IS NULL THEN desired.status
		WHEN observed.fresh_until < @decisionAt THEN 'STALE'
		ELSE observed.outcome
	  END,
      'expected', desired.artifact_digest, 'actual', observed.artifact_digest,
      'checksum', observed.evidence_checksum,
      'message', observed.evidence_reference,
      'artifactDigest', observed.artifact_digest,
      'configChecksum', observed.config_checksum,
      'platform', observed.platform,
      'schemaVersion', observed.schema_version,
      'capabilityChecksum', observed.capability_checksum,
      'health', observed.health,
      'blocking', observed.outcome IN ('PARTIAL', 'UNKNOWN') OR observed.fresh_until < @decisionAt,
      'order', desired.revision
    ) ORDER BY desired.created_at, desired.id), '[]'::jsonb) AS items
    FROM ExecutionAttempt AS related
    JOIN PendingDesiredRevision AS desired
      ON desired.execution_attempt_id = related.id
     AND desired.execution_id = related.execution_id
     AND desired.organization_id = related.organization_id
    LEFT JOIN ObservedComponentState AS observed
      ON observed.id = COALESCE(desired.verified_observation_id, desired.terminal_observation_id)
     AND observed.organization_id = desired.organization_id
    WHERE related.organization_id = attempt.organization_id
      AND related.execution_id = attempt.execution_id
      AND related.step_key = attempt.step_key
  ) AS observations ON TRUE
  LEFT JOIN LATERAL (
    SELECT COALESCE(jsonb_agg(item ORDER BY created_at, id), '[]'::jsonb) AS items
    FROM (
      SELECT intent.created_at, intent.id, jsonb_build_object(
        'id', intent.id, 'kind', 'intent', 'label', 'signed execution intent',
        'href', '/api/v1/control-plane/executions/' || attempt.id || '/evidence/intent/' || intent.id,
        'checksum', intent.checksum, 'createdAt', intent.created_at
      ) AS item
      WHERE intent.id IS NOT NULL
      UNION ALL
	  SELECT runtime.created_at, runtime.id, jsonb_build_object(
		'id', runtime.id, 'kind', 'executor-runtime-evidence',
		'label', runtime.health_status, 'href', runtime.evidence_reference,
		'checksum', runtime.canonical_checksum,
		'preExecutionConfigChecksum', runtime.pre_execution_config_checksum,
		'preExecutionServiceConfigChecksum', runtime.pre_execution_service_config_checksum,
		'resultConfigChecksum', runtime.result_config_checksum,
		'resultServiceConfigChecksum', runtime.result_service_config_checksum,
		'createdAt', runtime.created_at
	  ) AS item
	  FROM ExecutionRuntimeEvidence AS runtime
	  WHERE runtime.organization_id = attempt.organization_id
		AND runtime.execution_id = attempt.execution_id
		AND runtime.execution_attempt_id = attempt.id
	  UNION ALL
	  SELECT event.created_at, event.id, jsonb_build_object(
        'id', event.id, 'kind', 'execution-event', 'label', event.status,
        'href', '/api/v1/control-plane/executions/' || attempt.id || '/evidence/events/' || event.id,
        'checksum', event.payload_checksum, 'createdAt', event.created_at
      ) AS item
	  FROM (
		SELECT event.*
		FROM ExecutionEvent AS event
		WHERE event.organization_id = attempt.organization_id
		  AND event.execution_id = attempt.execution_id
		  AND event.execution_attempt_id = attempt.id
		ORDER BY event.event_sequence, event.id
	  ) AS event
      UNION ALL
      SELECT event.created_at, event.id, jsonb_build_object(
        'id', event.id, 'kind', 'reconciliation', 'label', event.outcome,
        'href', '/api/v1/control-plane/executions/' || attempt.id || '/evidence/reconciliation/' || event.id,
        'checksum', event.evidence_checksum, 'createdAt', event.created_at
      ) AS item
      FROM ExecutionReconciliationEvent AS event
      WHERE event.organization_id = attempt.organization_id
        AND event.execution_id = attempt.execution_id
        AND event.execution_attempt_id = attempt.id
      UNION ALL
      SELECT observed.created_at, observed.id, jsonb_build_object(
        'id', observed.id, 'kind', 'observation', 'label', observed.outcome,
        'href', observed.evidence_reference, 'checksum', observed.evidence_checksum,
        'createdAt', observed.created_at
      ) AS item
      FROM PendingDesiredRevision AS desired
      JOIN ObservedComponentState AS observed
        ON observed.id = COALESCE(desired.verified_observation_id, desired.terminal_observation_id)
       AND observed.organization_id = desired.organization_id
      WHERE desired.organization_id = attempt.organization_id
        AND desired.execution_id = attempt.execution_id
        AND desired.execution_attempt_id = attempt.id
    ) AS evidence
  ) AS evidence ON TRUE
)
SELECT payload FROM detail`

// OperatorExecutionRepository adapts the database package to the bounded
// operatorqueries.ExecutionRepository interface without creating a package
// cycle or exposing the database handle to handlers.
type OperatorExecutionRepository struct{}

func (OperatorExecutionRepository) ListOperatorExecutions(
	ctx context.Context,
	filter types.ExecutionFilter,
	after *time.Time,
	afterID *uuid.UUID,
	limit int,
) ([]types.OperatorExecutionRow, error) {
	if err := validateOperatorExecutionRepositoryInput(filter.OperatorScopeFilter); err != nil {
		return nil, err
	}
	if limit < 1 || limit > types.OperatorMaximumPageLimit+1 ||
		(after == nil) != (afterID == nil) {
		return nil, apierrors.NewBadRequest("operator execution page is invalid")
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, operatorExecutionListSQL, pgx.NamedArgs{
		"organizationID":   filter.OrganizationID,
		"decisionAt":       filter.DecisionAt,
		"organizationWide": filter.OrganizationWide,
		"customerIDs":      filter.CustomerIDs, "environmentIDs": filter.EnvironmentIDs,
		"deploymentUnitIDs": filter.DeploymentUnitIDs, "componentIDs": filter.ComponentIDs,
		"campaignIDs": filter.CampaignIDs, "status": filter.Status,
		"campaignID": filter.CampaignID, "deploymentPlanID": filter.DeploymentPlanID,
		"deploymentTargetID": filter.DeploymentTargetID,
		"fromTime":           filter.From, "toTime": filter.To,
		"cursorCreatedAt": after, "cursorID": afterID, "limitPlusOne": limit,
	})
	if err != nil {
		return nil, fmt.Errorf("query operator executions: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[types.OperatorExecutionRow])
	if err != nil {
		return nil, fmt.Errorf("read operator executions: %w", err)
	}
	return items, nil
}

func (OperatorExecutionRepository) GetOperatorExecution(
	ctx context.Context,
	scope types.OperatorScopeFilter,
	executionID uuid.UUID,
) (*types.OperatorExecutionDetail, error) {
	if err := validateOperatorExecutionRepositoryInput(scope); err != nil {
		return nil, err
	}
	if executionID == uuid.Nil {
		return nil, apierrors.ErrNotFound
	}
	var payload []byte
	err := internalctx.GetDb(ctx).QueryRow(ctx, operatorExecutionDetailSQL, pgx.NamedArgs{
		"organizationID": scope.OrganizationID, "decisionAt": scope.DecisionAt,
		"organizationWide": scope.OrganizationWide,
		"customerIDs":      scope.CustomerIDs, "environmentIDs": scope.EnvironmentIDs,
		"deploymentUnitIDs": scope.DeploymentUnitIDs, "componentIDs": scope.ComponentIDs,
		"campaignIDs": scope.CampaignIDs, "executionID": executionID,
	}).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apierrors.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query operator execution detail: %w", err)
	}
	var detail types.OperatorExecutionDetail
	if err := json.Unmarshal(payload, &detail); err != nil {
		return nil, fmt.Errorf("decode operator execution detail: %w", err)
	}
	if detail.Execution.ID == uuid.Nil {
		return nil, apierrors.ErrNotFound
	}
	if detail.Tasks == nil {
		detail.Tasks = []types.OperatorPlanFact{}
	}
	if detail.Steps == nil {
		detail.Steps = []types.OperatorPlanFact{}
	}
	if detail.Attempts == nil {
		detail.Attempts = []types.OperatorExecutionAttemptFact{}
	}
	if detail.Locks == nil {
		detail.Locks = []types.OperatorExecutionLockFact{}
	}
	if detail.Leases == nil {
		detail.Leases = []types.OperatorExecutionLeaseFact{}
	}
	if detail.Observations == nil {
		detail.Observations = []types.OperatorPlanFact{}
	}
	if detail.Evidence == nil {
		detail.Evidence = []types.OperatorEvidenceRef{}
	}
	return &detail, nil
}

func validateOperatorExecutionRepositoryInput(scope types.OperatorScopeFilter) error {
	return validateCanonicalOperatorScopeFilter(scope)
}
