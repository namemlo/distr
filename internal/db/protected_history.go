package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/protectedhistory"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ExportProtectedHistory reads one deterministic logical snapshot without taking write locks.
func ExportProtectedHistory(
	ctx context.Context,
	scope protectedhistory.Scope,
) (*protectedhistory.Artifact, error) {
	canonicalScope, err := protectedhistory.CanonicalScope(scope)
	if err != nil {
		return nil, err
	}
	args, err := protectedHistoryArgs(canonicalScope)
	if err != nil {
		return nil, err
	}
	var artifact *protectedhistory.Artifact
	err = RunReadOnlyTxRR(ctx, func(txCtx context.Context) error {
		version, err := protectedHistorySchemaVersion(txCtx)
		if err != nil {
			return err
		}
		if err := validateProtectedHistoryScope(txCtx, args); err != nil {
			return err
		}
		records, err := readProtectedHistoryRecords(txCtx, version, args)
		if err != nil {
			return err
		}
		artifact, err = protectedhistory.Build(canonicalScope, version, records)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("export protected history: %w", err)
	}
	return artifact, nil
}

func protectedHistoryArgs(scope protectedhistory.Scope) (pgx.NamedArgs, error) {
	organizationID, err := uuid.Parse(scope.OrganizationID)
	if err != nil {
		return nil, err
	}
	parseIDs := func(values []string) ([]uuid.UUID, error) {
		ids := make([]uuid.UUID, 0, len(values))
		for _, value := range values {
			id, err := uuid.Parse(value)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, nil
	}
	customerOrganizationIDs, err := parseIDs(scope.CustomerOrganizationIDs)
	if err != nil {
		return nil, err
	}
	deploymentTargetIDs, err := parseIDs(scope.DeploymentTargetIDs)
	if err != nil {
		return nil, err
	}
	return pgx.NamedArgs{
		"organizationId":          organizationID,
		"customerOrganizationIds": customerOrganizationIDs,
		"deploymentTargetIds":     deploymentTargetIDs,
	}, nil
}

func protectedHistorySchemaVersion(ctx context.Context) (uint64, error) {
	var version int64
	var dirty bool
	err := internalctx.GetDb(ctx).QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(
		&version, &dirty,
	)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	if dirty {
		return 0, fmt.Errorf("schema version %d is dirty", version)
	}
	if version < 138 {
		return 0, fmt.Errorf("schema version %d is unsupported; minimum is 138", version)
	}
	if version > 172 {
		return 0, fmt.Errorf("schema version %d is unsupported; maximum registered projection is 172", version)
	}
	return uint64(version), nil
}

func validateProtectedHistoryScope(ctx context.Context, args pgx.NamedArgs) error {
	var matchedCustomerOrganizations, requestedCustomerOrganizations int64
	var matchedDeploymentTargets, requestedDeploymentTargets int64
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM CustomerOrganization
   WHERE organization_id = @organizationId
     AND id = ANY(@customerOrganizationIds::uuid[])),
  cardinality(@customerOrganizationIds::uuid[]),
  (SELECT count(*) FROM DeploymentTarget
   WHERE organization_id = @organizationId
     AND id = ANY(@deploymentTargetIds::uuid[])),
  cardinality(@deploymentTargetIds::uuid[])
`, args).Scan(
		&matchedCustomerOrganizations,
		&requestedCustomerOrganizations,
		&matchedDeploymentTargets,
		&requestedDeploymentTargets,
	)
	if err != nil {
		return fmt.Errorf("validate protected history scope: %w", err)
	}
	if matchedCustomerOrganizations != requestedCustomerOrganizations {
		return errors.New("one or more customer organizations are absent or belong to another organization")
	}
	if matchedDeploymentTargets != requestedDeploymentTargets {
		return errors.New("one or more deployment targets are absent or belong to another organization")
	}
	return nil
}

func readProtectedHistoryRecords(
	ctx context.Context,
	schemaVersion uint64,
	args pgx.NamedArgs,
) ([]protectedhistory.RawRecord, error) {
	query, err := protectedHistoryRecordsSQLForSchema(schemaVersion)
	if err != nil {
		return nil, err
	}
	rows, err := internalctx.GetDb(ctx).Query(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("query protected history records: %w", err)
	}
	defer rows.Close()
	records := make([]protectedhistory.RawRecord, 0)
	for rows.Next() {
		var record protectedhistory.RawRecord
		var payload string
		if err := rows.Scan(&record.Kind, &record.ID, &payload); err != nil {
			return nil, fmt.Errorf("scan protected history record: %w", err)
		}
		record.Payload = json.RawMessage(payload)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate protected history records: %w", err)
	}
	return records, nil
}

func protectedHistoryRecordsSQLForSchema(schemaVersion uint64) (string, error) {
	switch {
	case schemaVersion >= 138 && schemaVersion <= 165:
		return protectedHistoryLegacyRecordsSQL, nil
	case schemaVersion == 166:
		return strings.ReplaceAll(protectedHistoryRecordsSQL, protectedHistoryVersionRecordsMarker, ""), nil
	case schemaVersion == 167:
		return strings.ReplaceAll(
			protectedHistoryRecordsSQL,
			protectedHistoryVersionRecordsMarker,
			protectedHistorySchema167RecordsSQL,
		), nil
	case schemaVersion == 168:
		return strings.ReplaceAll(
			protectedHistoryRecordsSQL,
			protectedHistoryVersionRecordsMarker,
			protectedHistorySchema167RecordsSQL+protectedHistorySchema168RecordsSQL,
		), nil
	case schemaVersion == 169:
		return strings.ReplaceAll(
			protectedHistoryRecordsSQL,
			protectedHistoryVersionRecordsMarker,
			protectedHistorySchema167RecordsSQL+
				protectedHistorySchema168RecordsSQL+
				protectedHistorySchema169RecordsSQL,
		), nil
	case schemaVersion == 170 || schemaVersion == 171 || schemaVersion == 172:
		return strings.ReplaceAll(
			protectedHistoryRecordsSQL,
			protectedHistoryVersionRecordsMarker,
			protectedHistorySchema167RecordsSQL+
				protectedHistorySchema168RecordsSQL+
				protectedHistorySchema169RecordsSQL+
				protectedHistorySchema170RecordsSQL,
		), nil
	default:
		return "", fmt.Errorf("schema version %d has no protected-history projection", schemaVersion)
	}
}

// Schema 138 already contains release, deployment, task, execution and
// timestamp-audit history. Whole-row JSON makes every field in that exact
// schema part of the fingerprint instead of silently omitting older history.
const protectedHistoryLegacyRecordsSQL = `
WITH
requested_customer_organizations(id) AS (
  SELECT unnest(@customerOrganizationIds::uuid[])
),
requested_deployment_targets(id) AS (
  SELECT unnest(@deploymentTargetIds::uuid[])
),
selected_targets(id) AS (
  SELECT dt.id
  FROM DeploymentTarget dt
  WHERE dt.organization_id = @organizationId
    AND (
      dt.id IN (SELECT id FROM requested_deployment_targets)
      OR dt.customer_organization_id IN (SELECT id FROM requested_customer_organizations)
    )
),
selected_deployments(id) AS (
  SELECT deployment.id FROM Deployment deployment
  WHERE deployment.deployment_target_id IN (SELECT id FROM selected_targets)
),
selected_revisions(id) AS (
  SELECT revision.id FROM DeploymentRevision revision
  WHERE revision.deployment_id IN (SELECT id FROM selected_deployments)
),
selected_plans(id) AS (
  SELECT DISTINCT target.deployment_plan_id
  FROM DeploymentPlanTarget target
  WHERE target.organization_id = @organizationId
    AND (
      target.deployment_target_id IN (SELECT id FROM selected_targets)
      OR target.customer_organization_id IN (SELECT id FROM requested_customer_organizations)
    )
),
selected_tasks(id) AS (
  SELECT task.id FROM Task task
  WHERE task.organization_id = @organizationId
    AND (
      task.deployment_plan_id IN (SELECT id FROM selected_plans)
      OR task.deployment_target_id IN (SELECT id FROM selected_targets)
    )
),
selected_release_bundles(id) AS (
  SELECT plan.release_bundle_id FROM DeploymentPlan plan
  WHERE plan.id IN (SELECT id FROM selected_plans)
),
selected_executions(id) AS (
  SELECT execution.id
  FROM ExternalExecution execution
  WHERE execution.organization_id = @organizationId
    AND (
      execution.deployment_target_id IN (SELECT id FROM selected_targets)
      OR execution.task_id IN (SELECT id FROM selected_tasks)
    )
),
selected_execution_events(id) AS (
  SELECT event.id FROM ExternalExecutionEvent event
  WHERE event.external_execution_id IN (SELECT id FROM selected_executions)
),
selected_timestamp_manifests(id) AS (
  SELECT DISTINCT provenance.manifest_id
  FROM ExternalExecutionTimestampCellProvenance provenance
  WHERE (
    provenance.source_table = 'externalexecution'
    AND provenance.source_row_id IN (SELECT id FROM selected_executions)
  ) OR (
    provenance.source_table = 'externalexecutionevent'
    AND provenance.source_row_id IN (SELECT id FROM selected_execution_events)
  )
),
selected_application_versions(id) AS (
  SELECT revision.application_version_id FROM DeploymentRevision revision
  WHERE revision.id IN (SELECT id FROM selected_revisions)
  UNION
  SELECT component.application_version_id FROM ReleaseBundleComponent component
  WHERE component.release_bundle_id IN (SELECT id FROM selected_release_bundles)
    AND component.application_version_id IS NOT NULL
),
selected_applications(id) AS (
  SELECT version.application_id FROM ApplicationVersion version
  WHERE version.id IN (SELECT id FROM selected_application_versions)
  UNION
  SELECT plan.application_id FROM DeploymentPlan plan
  WHERE plan.id IN (SELECT id FROM selected_plans)
),
logical_records(kind, id, payload) AS (
  SELECT 'application', application.id, to_jsonb(application.*)
  FROM Application application
  WHERE application.organization_id = @organizationId
    AND application.id IN (SELECT id FROM selected_applications)

  UNION ALL SELECT 'applicationversion', version.id, to_jsonb(version.*)
  FROM ApplicationVersion version
  WHERE version.id IN (SELECT id FROM selected_application_versions)

  UNION ALL SELECT 'customerorganization', co.id, to_jsonb(co.*)
  FROM CustomerOrganization co
  WHERE co.organization_id = @organizationId
    AND co.id IN (SELECT id FROM requested_customer_organizations)

  UNION ALL SELECT 'deploymenttarget', target.id, to_jsonb(target.*)
  FROM DeploymentTarget target
  WHERE target.organization_id = @organizationId
    AND target.id IN (SELECT id FROM selected_targets)

  UNION ALL SELECT 'deploymenttargetlogrecord', log.id, to_jsonb(log.*)
  FROM DeploymentTargetLogRecord log
  WHERE log.deployment_target_id IN (SELECT id FROM selected_targets)

  UNION ALL SELECT 'deployment', deployment.id, to_jsonb(deployment.*)
  FROM Deployment deployment
  WHERE deployment.id IN (SELECT id FROM selected_deployments)

  UNION ALL SELECT 'deploymentrevision', revision.id, to_jsonb(revision.*)
  FROM DeploymentRevision revision
  WHERE revision.id IN (SELECT id FROM selected_revisions)

  UNION ALL SELECT 'deploymentrevisionstatus', status.id, to_jsonb(status.*)
  FROM DeploymentRevisionStatus status
  WHERE status.deployment_revision_id IN (SELECT id FROM selected_revisions)

  UNION ALL SELECT 'deploymentlogrecord', log.id, to_jsonb(log.*)
  FROM DeploymentLogRecord log
  WHERE log.deployment_id IN (SELECT id FROM selected_deployments)

  UNION ALL SELECT 'releasebundle', bundle.id, to_jsonb(bundle.*)
  FROM ReleaseBundle bundle
  WHERE bundle.organization_id = @organizationId
    AND bundle.id IN (SELECT id FROM selected_release_bundles)

  UNION ALL SELECT 'releasebundlecomponent', component.id, to_jsonb(component.*)
  FROM ReleaseBundleComponent component
  WHERE component.release_bundle_id IN (SELECT id FROM selected_release_bundles)

  UNION ALL SELECT 'releasebundleauditevent', event.id, to_jsonb(event.*)
  FROM ReleaseBundleAuditEvent event
  WHERE event.organization_id = @organizationId
    AND event.release_bundle_id IN (SELECT id FROM selected_release_bundles)

  UNION ALL SELECT 'releasebundleidempotencykey', keyrow.id, to_jsonb(keyrow.*)
  FROM ReleaseBundleIdempotencyKey keyrow
  WHERE keyrow.organization_id = @organizationId
    AND keyrow.release_bundle_id IN (SELECT id FROM selected_release_bundles)

  UNION ALL SELECT 'processsnapshot', snapshot.id, to_jsonb(snapshot.*)
  FROM ProcessSnapshot snapshot
  WHERE snapshot.organization_id = @organizationId
    AND snapshot.id IN (
      SELECT plan.process_snapshot_id FROM DeploymentPlan plan
      WHERE plan.id IN (SELECT id FROM selected_plans) AND plan.process_snapshot_id IS NOT NULL
    )

  UNION ALL SELECT 'variablesnapshot', snapshot.id, to_jsonb(snapshot.*)
  FROM VariableSnapshot snapshot
  WHERE snapshot.organization_id = @organizationId
    AND snapshot.id IN (
      SELECT plan.variable_snapshot_id FROM DeploymentPlan plan
      WHERE plan.id IN (SELECT id FROM selected_plans) AND plan.variable_snapshot_id IS NOT NULL
    )

  UNION ALL SELECT 'variablesnapshotvalue', snapshot_value.id, to_jsonb(snapshot_value.*)
  FROM VariableSnapshotValue snapshot_value
  WHERE snapshot_value.variable_snapshot_id IN (
    SELECT plan.variable_snapshot_id FROM DeploymentPlan plan
    WHERE plan.id IN (SELECT id FROM selected_plans) AND plan.variable_snapshot_id IS NOT NULL
  )

  UNION ALL SELECT 'deploymentplan', plan.id, to_jsonb(plan.*)
  FROM DeploymentPlan plan
  WHERE plan.organization_id = @organizationId AND plan.id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentplanissue', issue.id, to_jsonb(issue.*)
  FROM DeploymentPlanIssue issue
  WHERE issue.organization_id = @organizationId AND issue.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentplanstep', step.id, to_jsonb(step.*)
  FROM DeploymentPlanStep step
  WHERE step.organization_id = @organizationId AND step.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentplantarget', target.id, to_jsonb(target.*)
  FROM DeploymentPlanTarget target
  WHERE target.organization_id = @organizationId AND target.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentplantargetcomponent', plan_component.id, to_jsonb(plan_component.*)
  FROM DeploymentPlanTargetComponent plan_component
  WHERE plan_component.organization_id = @organizationId
    AND plan_component.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentplanvariable', variable.id, to_jsonb(variable.*)
  FROM DeploymentPlanVariable variable
  WHERE variable.organization_id = @organizationId AND variable.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentpreflightrun', run.id, to_jsonb(run.*)
  FROM DeploymentPreflightRun run
  WHERE run.organization_id = @organizationId AND run.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentpreflightcheck', checkrow.id, to_jsonb(checkrow.*)
  FROM DeploymentPreflightCheck checkrow
  WHERE checkrow.organization_id = @organizationId AND checkrow.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'task', task.id, to_jsonb(task.*)
  FROM Task task
  WHERE task.organization_id = @organizationId AND task.id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'tasklease', lease.id, to_jsonb(lease.*)
  FROM TaskLease lease
  WHERE lease.organization_id = @organizationId AND lease.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'taskresourcelock', lockrow.id, to_jsonb(lockrow.*)
  FROM TaskResourceLock lockrow
  WHERE lockrow.organization_id = @organizationId AND lockrow.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'steprun', run.id, to_jsonb(run.*)
  FROM StepRun run
  WHERE run.organization_id = @organizationId AND run.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'steprunevent', event.id, to_jsonb(event.*)
  FROM StepRunEvent event
  WHERE event.organization_id = @organizationId AND event.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'steprunlogchunk', chunk.id, to_jsonb(chunk.*)
  FROM StepRunLogChunk chunk
  WHERE chunk.organization_id = @organizationId AND chunk.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'steprunoutput', output.id, to_jsonb(output.*)
  FROM StepRunOutput output
  WHERE output.organization_id = @organizationId AND output.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'targetcomponentstate', state.id, to_jsonb(state.*)
  FROM TargetComponentState state
  WHERE state.organization_id = @organizationId
    AND state.deployment_target_id IN (SELECT id FROM selected_targets)

  UNION ALL SELECT 'targetcomponentobservation', observation.id, to_jsonb(observation.*)
  FROM TargetComponentObservation observation
  WHERE observation.organization_id = @organizationId
    AND observation.deployment_target_id IN (SELECT id FROM selected_targets)

  UNION ALL SELECT 'externalexecution', execution.id, to_jsonb(execution.*)
  FROM ExternalExecution execution
  WHERE execution.organization_id = @organizationId
    AND execution.id IN (SELECT id FROM selected_executions)

  UNION ALL SELECT 'externalexecutionevent', event.id, to_jsonb(event.*)
  FROM ExternalExecutionEvent event
  WHERE event.organization_id = @organizationId
    AND event.id IN (SELECT id FROM selected_execution_events)

  UNION ALL SELECT 'externalexecutiontimestampmanifest', manifest.id, to_jsonb(manifest.*)
  FROM ExternalExecutionTimestampManifest manifest
  WHERE manifest.id IN (SELECT id FROM selected_timestamp_manifests)

  UNION ALL SELECT 'externalexecutiontimestampcellprovenance',
    md5(provenance.manifest_id::text || ':' || provenance.source_table || ':' ||
      provenance.source_row_id::text || ':' || provenance.source_column)::uuid,
    to_jsonb(provenance.*)
  FROM ExternalExecutionTimestampCellProvenance provenance
  WHERE provenance.manifest_id IN (SELECT id FROM selected_timestamp_manifests)

  UNION ALL SELECT 'externalexecutiontimestampdeletiontombstone',
    md5(tombstone.source_table || ':' || tombstone.source_row_id::text || ':' ||
      tombstone.source_column)::uuid,
    to_jsonb(tombstone.*)
  FROM ExternalExecutionTimestampDeletionTombstone tombstone
  WHERE tombstone.source_row_id IN (
    SELECT id FROM selected_executions UNION SELECT id FROM selected_execution_events
  )

  UNION ALL SELECT 'externalexecutiontimestampexpandstate',
    '00000000-0000-5000-8000-000000000138'::uuid, to_jsonb(state.*)
  FROM ExternalExecutionTimestampExpandState state

  UNION ALL SELECT 'externalexecutiontimestampcontractgate', gate.id, to_jsonb(gate.*)
  FROM ExternalExecutionTimestampContractGate gate
  WHERE gate.manifest_id IN (SELECT id FROM selected_timestamp_manifests)
)
SELECT kind, id::text, payload::text
FROM logical_records
ORDER BY kind, id
`

const protectedHistoryRecordsSQL = `
WITH
requested_customer_organizations(id) AS (
  SELECT unnest(@customerOrganizationIds::uuid[])
),
requested_deployment_targets(id) AS (
  SELECT unnest(@deploymentTargetIds::uuid[])
),
selected_targets(id) AS (
  SELECT dt.id
  FROM DeploymentTarget dt
  WHERE dt.organization_id = @organizationId
    AND (
      dt.id IN (SELECT id FROM requested_deployment_targets)
      OR dt.customer_organization_id IN (SELECT id FROM requested_customer_organizations)
    )
),
selected_plans(id) AS (
  SELECT DISTINCT dpt.deployment_plan_id
  FROM DeploymentPlanTarget dpt
  WHERE dpt.organization_id = @organizationId
    AND (
      dpt.deployment_target_id IN (SELECT id FROM selected_targets)
      OR dpt.customer_organization_id IN (SELECT id FROM requested_customer_organizations)
    )
),
selected_tasks(id) AS (
  SELECT task.id
  FROM Task task
  WHERE task.organization_id = @organizationId
    AND (
      task.deployment_plan_id IN (SELECT id FROM selected_plans)
      OR task.deployment_target_id IN (SELECT id FROM selected_targets)
    )
),
selected_release_bundles(id) AS (
  SELECT DISTINCT plan.release_bundle_id
  FROM DeploymentPlan plan
  WHERE plan.organization_id = @organizationId
    AND plan.id IN (SELECT id FROM selected_plans)
),
selected_execution_attempts(id) AS (
  SELECT attempt.id
  FROM ExecutionAttempt attempt
  WHERE attempt.organization_id = @organizationId
    AND attempt.task_id IN (SELECT id FROM selected_tasks)
),
selected_sample_retirement_jobs(id) AS (
  SELECT DISTINCT item.retirement_job_id
  FROM SampleRetirementItem item
  WHERE item.organization_id = @organizationId
    AND (
      (item.subject_type = 'deployment_target' AND item.subject_id IN (SELECT id FROM selected_targets))
      OR (item.subject_type = 'application' AND item.subject_id IN (
        SELECT plan.application_id FROM DeploymentPlan plan WHERE plan.id IN (SELECT id FROM selected_plans)
      ))
      OR (item.subject_type = 'environment' AND item.subject_id IN (
        SELECT plan.environment_id FROM DeploymentPlan plan WHERE plan.id IN (SELECT id FROM selected_plans)
      ))
    )
),
logical_records(kind, id, payload) AS (
  SELECT 'customerorganization', co.id, jsonb_build_object(
    'organizationId', co.organization_id,
    'partnerOrganizationId', co.partner_organization_id
  )
  FROM CustomerOrganization co
  WHERE co.organization_id = @organizationId
    AND co.id IN (SELECT id FROM requested_customer_organizations)

  UNION ALL SELECT 'deploymenttarget', dt.id, jsonb_build_object(
    'organizationId', dt.organization_id,
    'customerOrganizationId', dt.customer_organization_id,
    'type', dt.type,
    'platform', dt.platform
  ) FROM DeploymentTarget dt
  WHERE dt.organization_id = @organizationId AND dt.id IN (SELECT id FROM selected_targets)

  UNION ALL SELECT 'deploymentplan', plan.id, jsonb_build_object(
    'organizationId', plan.organization_id,
    'releaseBundleId', plan.release_bundle_id,
    'applicationId', plan.application_id,
    'channelId', plan.channel_id,
    'environmentId', plan.environment_id,
    'processSnapshotId', plan.process_snapshot_id,
    'variableSnapshotId', plan.variable_snapshot_id,
    'status', plan.status,
    'canonicalChecksum', plan.canonical_checksum
  ) FROM DeploymentPlan plan
  WHERE plan.organization_id = @organizationId AND plan.id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentplanissue', issue.id, jsonb_build_object(
    'deploymentPlanId', issue.deployment_plan_id, 'organizationId', issue.organization_id,
    'severity', issue.severity, 'code', issue.code, 'field', issue.field,
    'message', issue.message, 'sortOrder', issue.sort_order
  ) FROM DeploymentPlanIssue issue
  WHERE issue.organization_id = @organizationId AND issue.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentplanstep', step.id, jsonb_build_object(
    'deploymentPlanId', step.deployment_plan_id, 'organizationId', step.organization_id,
    'stepKey', step.step_key, 'name', step.name, 'actionType', step.action_type,
    'actionName', step.action_name, 'executionLocation', step.execution_location,
    'condition', step.condition, 'targetTags', step.target_tags, 'failureMode', step.failure_mode,
    'timeoutSeconds', step.timeout_seconds, 'retryMaxAttempts', step.retry_max_attempts,
    'retryIntervalSeconds', step.retry_interval_seconds,
    'requiredPermissions', step.required_permissions, 'sortOrder', step.sort_order,
    'dependencies', step.dependencies, 'included', step.included,
    'excludedReason', step.excluded_reason
  ) FROM DeploymentPlanStep step
  WHERE step.organization_id = @organizationId AND step.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentplantarget', target.id, jsonb_build_object(
    'deploymentPlanId', target.deployment_plan_id, 'organizationId', target.organization_id,
    'deploymentTargetId', target.deployment_target_id, 'customerOrganizationId', target.customer_organization_id,
    'name', target.name, 'type', target.type, 'platform', target.platform, 'sortOrder', target.sort_order
  ) FROM DeploymentPlanTarget target
  WHERE target.organization_id = @organizationId AND target.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentplantargetcomponent', component.id, jsonb_build_object(
    'deploymentPlanId', component.deployment_plan_id,
    'deploymentPlanTargetId', component.deployment_plan_target_id,
    'organizationId', component.organization_id, 'deploymentTargetId', component.deployment_target_id,
    'component', component.component, 'version', component.version, 'image', component.image,
    'platform', component.platform, 'contracts', component.contracts,
    'configChecksum', component.config_checksum, 'expectedStateVersion', component.expected_state_version,
    'expectedStateChecksum', component.expected_state_checksum,
    'expectedReleaseBundleId', component.expected_release_bundle_id, 'sortOrder', component.sort_order
  ) FROM DeploymentPlanTargetComponent component
  WHERE component.organization_id = @organizationId AND component.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentplanvariable', variable.id, jsonb_build_object(
    'deploymentPlanId', variable.deployment_plan_id, 'organizationId', variable.organization_id,
    'variableSetId', variable.variable_set_id, 'variableId', variable.variable_id,
    'key', variable.key, 'type', variable.type, 'isRequired', variable.is_required,
    'status', variable.status, 'source', variable.source,
    'referenceId', variable.reference_id, 'referenceName', variable.reference_name,
    'redacted', variable.redacted
  ) FROM DeploymentPlanVariable variable
  WHERE variable.organization_id = @organizationId AND variable.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentpreflightrun', run.id, jsonb_build_object(
    'organizationId', run.organization_id, 'deploymentPlanId', run.deployment_plan_id,
    'planChecksum', run.plan_checksum, 'actorUserAccountId', run.actor_user_account_id,
    'status', run.status, 'createdAt', to_char(run.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US')
  ) FROM DeploymentPreflightRun run
  WHERE run.organization_id = @organizationId AND run.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'deploymentpreflightcheck', checkrow.id, jsonb_build_object(
    'organizationId', checkrow.organization_id, 'deploymentPreflightRunId', checkrow.deployment_preflight_run_id,
    'deploymentPlanId', checkrow.deployment_plan_id, 'deploymentPlanTargetId', checkrow.deployment_plan_target_id,
    'deploymentTargetId', checkrow.deployment_target_id, 'taskId', checkrow.task_id,
    'component', checkrow.component, 'checkKey', checkrow.check_key,
    'status', checkrow.status, 'sortOrder', checkrow.sort_order,
    'createdAt', to_char(checkrow.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US')
  ) FROM DeploymentPreflightCheck checkrow
  WHERE checkrow.organization_id = @organizationId AND checkrow.deployment_plan_id IN (SELECT id FROM selected_plans)

  UNION ALL SELECT 'releasebundle', bundle.id, jsonb_build_object(
    'organizationId', bundle.organization_id, 'applicationId', bundle.application_id,
    'channelId', bundle.channel_id, 'releaseNumber', bundle.release_number,
    'sourceRevision', bundle.source_revision, 'status', bundle.status,
    'canonicalChecksum', bundle.canonical_checksum, 'sourceRepository', bundle.source_repository,
    'sourceBranch', bundle.source_branch, 'sourceTag', bundle.source_tag,
    'ciProvider', bundle.ci_provider, 'ciRunId', bundle.ci_run_id,
    'processSnapshotId', bundle.process_snapshot_id, 'variableSnapshotId', bundle.variable_snapshot_id,
    'retentionProtected', bundle.retention_protected
  ) FROM ReleaseBundle bundle
  WHERE bundle.organization_id = @organizationId AND bundle.id IN (SELECT id FROM selected_release_bundles)

  UNION ALL SELECT 'releasebundleauditevent', event.id, jsonb_build_object(
    'organizationId', event.organization_id, 'releaseBundleId', event.release_bundle_id,
    'actorUserAccountId', event.actor_user_account_id, 'eventType', event.event_type,
    'fromStatus', event.from_status, 'toStatus', event.to_status, 'reason', event.reason,
    'createdAt', to_char(event.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US')
  ) FROM ReleaseBundleAuditEvent event
  WHERE event.organization_id = @organizationId AND event.release_bundle_id IN (SELECT id FROM selected_release_bundles)

  UNION ALL SELECT 'releasebundlecomponent', component.id, jsonb_build_object(
    'releaseBundleId', component.release_bundle_id, 'key', component.key, 'name', component.name,
    'componentType', component.component_type, 'version', component.version,
    'applicationVersionId', component.application_version_id, 'packageRef', component.package_ref,
    'digest', component.digest, 'checksum', component.checksum,
    'childReleaseBundleId', component.child_release_bundle_id
  ) FROM ReleaseBundleComponent component
  WHERE component.release_bundle_id IN (SELECT id FROM selected_release_bundles)

  UNION ALL SELECT 'releasebundleidempotencykey', keyrow.id, jsonb_build_object(
    'organizationId', keyrow.organization_id, 'keyHash', keyrow.key_hash,
    'requestChecksum', keyrow.request_checksum, 'releaseBundleId', keyrow.release_bundle_id,
    'createdAt', to_char(keyrow.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US')
  ) FROM ReleaseBundleIdempotencyKey keyrow
  WHERE keyrow.organization_id = @organizationId AND keyrow.release_bundle_id IN (SELECT id FROM selected_release_bundles)

  UNION ALL SELECT 'task', task.id, jsonb_build_object(
    'organizationId', task.organization_id, 'deploymentPlanId', task.deployment_plan_id,
    'deploymentPlanTargetId', task.deployment_plan_target_id, 'deploymentTargetId', task.deployment_target_id,
    'applicationId', task.application_id, 'releaseBundleId', task.release_bundle_id,
    'channelId', task.channel_id, 'environmentId', task.environment_id,
    'status', task.status, 'queueOrder', task.queue_order, 'taskType', task.task_type,
    'actorUserAccountId', task.actor_user_account_id
  ) FROM Task task
  WHERE task.organization_id = @organizationId AND task.id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'tasklease', lease.id, jsonb_build_object(
    'organizationId', lease.organization_id, 'taskId', lease.task_id, 'agentId', lease.agent_id,
    'attempt', lease.attempt, 'executorType', lease.executor_type,
    'leasedAt', to_char(lease.leased_at, 'YYYY-MM-DD"T"HH24:MI:SS.US'),
    'releasedAt', to_char(lease.released_at, 'YYYY-MM-DD"T"HH24:MI:SS.US')
  ) FROM TaskLease lease
  WHERE lease.organization_id = @organizationId AND lease.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'taskresourcelock', lockrow.id, jsonb_build_object(
    'organizationId', lockrow.organization_id, 'taskId', lockrow.task_id,
    'resourceType', lockrow.resource_type, 'resourceKey', lockrow.resource_key,
    'concurrencyPolicy', lockrow.concurrency_policy,
    'acquiredAt', to_char(lockrow.acquired_at, 'YYYY-MM-DD"T"HH24:MI:SS.US'),
    'releasedAt', to_char(lockrow.released_at, 'YYYY-MM-DD"T"HH24:MI:SS.US')
  ) FROM TaskResourceLock lockrow
  WHERE lockrow.organization_id = @organizationId AND lockrow.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'steprun', run.id, jsonb_build_object(
    'organizationId', run.organization_id, 'taskId', run.task_id,
    'deploymentPlanId', run.deployment_plan_id, 'deploymentPlanStepId', run.deployment_plan_step_id,
    'stepKey', run.step_key, 'name', run.name, 'actionType', run.action_type,
    'status', run.status, 'sortOrder', run.sort_order, 'skippedReason', run.skipped_reason
  ) FROM StepRun run
  WHERE run.organization_id = @organizationId AND run.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'steprunevent', event.id, jsonb_build_object(
    'organizationId', event.organization_id, 'taskId', event.task_id, 'stepRunId', event.step_run_id,
    'taskLeaseId', event.task_lease_id, 'agentId', event.agent_id,
    'sequence', event.sequence, 'eventType', event.event_type,
    'progressPercent', event.progress_percent, 'payloadHash', event.payload_hash, 'redacted', event.redacted,
    'occurredAt', to_char(event.occurred_at, 'YYYY-MM-DD"T"HH24:MI:SS.US')
  ) FROM StepRunEvent event
  WHERE event.organization_id = @organizationId AND event.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'steprunoutput', output.id, jsonb_build_object(
    'eventId', output.event_id, 'organizationId', output.organization_id,
    'taskId', output.task_id, 'stepRunId', output.step_run_id,
    'taskLeaseId', output.task_lease_id, 'agentId', output.agent_id,
    'name', output.name, 'sensitive', output.sensitive, 'redacted', output.redacted
  ) FROM StepRunOutput output
  WHERE output.organization_id = @organizationId AND output.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'externalexecution', execution.id, jsonb_build_object(
    'organizationId', execution.organization_id, 'stepRunId', execution.step_run_id,
    'taskId', execution.task_id, 'deploymentPlanId', execution.deployment_plan_id,
    'deploymentPlanTargetId', execution.deployment_plan_target_id,
    'deploymentTargetId', execution.deployment_target_id, 'applicationId', execution.application_id,
    'releaseBundleId', execution.release_bundle_id, 'component', execution.component,
    'planChecksum', execution.plan_checksum, 'idempotencyKey', execution.idempotency_key,
    'expectedStateVersion', execution.expected_state_version,
    'expectedStateChecksum', execution.expected_state_checksum,
    'expectedVersion', execution.expected_version, 'expectedImage', execution.expected_image,
    'expectedPlatform', execution.expected_platform, 'expectedContracts', execution.expected_contracts,
    'expectedConfigChecksum', execution.expected_config_checksum, 'status', execution.status,
    'triggerAttempts', execution.trigger_attempts, 'lastCallbackSequence', execution.last_callback_sequence,
    'actualVersion', execution.actual_version, 'actualImage', execution.actual_image,
    'actualPlatform', execution.actual_platform, 'actualContracts', execution.actual_contracts,
    'actualConfigChecksum', execution.actual_config_checksum, 'actualHealth', execution.actual_health,
    'observedStateChecksum', execution.observed_state_checksum
  ) FROM ExternalExecution execution
  WHERE execution.organization_id = @organizationId AND execution.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'externalexecutionevent', event.id, jsonb_build_object(
    'organizationId', event.organization_id, 'externalExecutionId', event.external_execution_id,
    'sequence', event.sequence, 'status', event.status, 'payloadHash', event.payload_hash
  ) FROM ExternalExecutionEvent event
  JOIN ExternalExecution execution ON execution.id = event.external_execution_id
  WHERE event.organization_id = @organizationId AND execution.task_id IN (SELECT id FROM selected_tasks)

  UNION ALL SELECT 'targetcomponentstate', state.id, jsonb_build_object(
    'organizationId', state.organization_id, 'deploymentTargetId', state.deployment_target_id,
    'applicationId', state.application_id, 'component', state.component
  ) FROM TargetComponentState state
  WHERE state.organization_id = @organizationId AND state.deployment_target_id IN (SELECT id FROM selected_targets)

  UNION ALL SELECT 'targetcomponentobservation', observation.id, jsonb_build_object(
    'organizationId', observation.organization_id,
    'targetComponentStateId', observation.target_component_state_id,
    'deploymentTargetId', observation.deployment_target_id, 'applicationId', observation.application_id,
    'component', observation.component, 'stateVersion', observation.state_version,
    'stateChecksum', observation.state_checksum, 'releaseBundleId', observation.release_bundle_id,
    'version', observation.version, 'image', observation.image, 'platform', observation.platform,
    'contracts', observation.contracts, 'configChecksum', observation.config_checksum,
    'health', observation.health, 'externalExecutionId', observation.external_execution_id,
    'configReference', observation.config_reference,
    'observedAt', to_char(observation.observed_at, 'YYYY-MM-DD"T"HH24:MI:SS.US')
  ) FROM TargetComponentObservation observation
  WHERE observation.organization_id = @organizationId
    AND observation.deployment_target_id IN (SELECT id FROM selected_targets)

  UNION ALL SELECT 'executionattempt', attempt.id, to_jsonb(attempt.*)
  FROM ExecutionAttempt attempt
  WHERE attempt.organization_id = @organizationId
    AND attempt.id IN (SELECT id FROM selected_execution_attempts)

  UNION ALL SELECT 'executionintent', intent.id, jsonb_build_object(
    'createdAt', intent.created_at, 'organizationId', intent.organization_id,
    'executionAttemptId', intent.execution_attempt_id, 'checksum', intent.checksum,
    'keyId', intent.key_id, 'signature', intent.signature
  ) FROM ExecutionIntent intent
  WHERE intent.organization_id = @organizationId
    AND intent.execution_attempt_id IN (SELECT id FROM selected_execution_attempts)

  UNION ALL SELECT 'sampleretirementjob', job.id, to_jsonb(job.*)
  FROM SampleRetirementJob job
  WHERE job.organization_id = @organizationId
    AND job.id IN (SELECT id FROM selected_sample_retirement_jobs)

  UNION ALL SELECT 'sampleretirementitem', item.id, to_jsonb(item.*)
  FROM SampleRetirementItem item
  WHERE item.organization_id = @organizationId
    AND item.retirement_job_id IN (SELECT id FROM selected_sample_retirement_jobs)

  UNION ALL SELECT 'sampleretirementownershipevidence', evidence.id, to_jsonb(evidence.*)
  FROM SampleRetirementOwnershipEvidence evidence
  WHERE evidence.organization_id = @organizationId
    AND evidence.id IN (
      SELECT item.ownership_evidence_id FROM SampleRetirementItem item
      WHERE item.retirement_job_id IN (SELECT id FROM selected_sample_retirement_jobs)
    )

  UNION ALL SELECT 'approvalrequest', request.id, to_jsonb(request.*)
  FROM ApprovalRequest request
  WHERE request.organization_id = @organizationId
    AND request.subject_type = 'sample_retirement'
    AND request.subject_id IN (SELECT id FROM selected_sample_retirement_jobs)

  UNION ALL SELECT 'approvaldecision', decision.id, to_jsonb(decision.*)
  FROM ApprovalDecision decision
  WHERE decision.organization_id = @organizationId
    AND decision.approval_request_id IN (
      SELECT request.id FROM ApprovalRequest request
      WHERE request.organization_id = @organizationId
        AND request.subject_type = 'sample_retirement'
        AND request.subject_id IN (SELECT id FROM selected_sample_retirement_jobs)
    )

  /*PROTECTED_HISTORY_VERSION_RECORDS*/
)
SELECT kind, id::text, payload::text
FROM logical_records
ORDER BY kind, id
`

const protectedHistoryVersionRecordsMarker = "/*PROTECTED_HISTORY_VERSION_RECORDS*/"

const protectedHistorySchema167RecordsSQL = `
  UNION ALL SELECT 'executionruntimeevidence', evidence.id, to_jsonb(evidence.*)
  FROM ExecutionRuntimeEvidence evidence
  WHERE evidence.organization_id = @organizationId
    AND evidence.execution_attempt_id IN (SELECT id FROM selected_execution_attempts)
`

const protectedHistorySchema168RecordsSQL = `
  UNION ALL SELECT 'deploymentplanresolvedrequirement', requirement.id, to_jsonb(requirement.*)
  FROM DeploymentPlanResolvedRequirement requirement
  WHERE requirement.organization_id = @organizationId
    AND requirement.deployment_plan_id IN (SELECT id FROM selected_plans)
`

const protectedHistorySchema169RecordsSQL = `
  UNION ALL SELECT 'baselineadoptioncomponent', component.id, to_jsonb(component.*)
  FROM BaselineAdoptionComponent component
  WHERE component.organization_id = @organizationId
    AND component.deployment_plan_id IN (SELECT id FROM selected_plans)
`

const protectedHistorySchema170RecordsSQL = `
  UNION ALL SELECT 'protectedhistoryartifact', artifact.id, to_jsonb(artifact.*)
  FROM ProtectedHistoryArtifact artifact
  WHERE artifact.organization_id = @organizationId
    AND artifact.customer_organization_ids <@ @customerOrganizationIds::uuid[]
    AND artifact.deployment_target_ids <@ ARRAY(
      SELECT id FROM selected_targets ORDER BY id
    )

  UNION ALL SELECT 'controlplaneauditevent', event.id, to_jsonb(event.*)
  FROM ControlPlaneAuditEvent event
  JOIN ProtectedHistoryArtifact artifact
    ON artifact.id = event.protected_history_artifact_id
   AND artifact.organization_id = event.organization_id
  WHERE artifact.organization_id = @organizationId
    AND artifact.customer_organization_ids <@ @customerOrganizationIds::uuid[]
    AND artifact.deployment_target_ids <@ ARRAY(
      SELECT id FROM selected_targets ORDER BY id
    )
`
