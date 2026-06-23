package db

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const deployPreviousReleaseWarning = "Deploy previous release creates a new deployment plan for the selected release. It does not reverse external state or database changes."

func GetDeploymentTimeline(
	ctx context.Context,
	query types.DeploymentTimelineQuery,
) (*types.DeploymentTimeline, error) {
	items, err := queryDeploymentTimelineItems(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	return &types.DeploymentTimeline{Items: items}, nil
}

func CompareDeploymentTimelineTasks(
	ctx context.Context,
	request types.DeploymentTimelineCompareRequest,
) (*types.DeploymentTimelineComparison, error) {
	if request.OrganizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if err := validateDeploymentTimelineCompareRef(
		"base",
		request.BaseTaskID,
		request.BaseLegacyDeploymentRevisionID,
	); err != nil {
		return nil, err
	}
	if err := validateDeploymentTimelineCompareRef(
		"compare",
		request.CompareTaskID,
		request.CompareLegacyDeploymentRevisionID,
	); err != nil {
		return nil, err
	}

	taskIDs := make([]uuid.UUID, 0, 2)
	if request.BaseTaskID != uuid.Nil {
		taskIDs = append(taskIDs, request.BaseTaskID)
	}
	if request.CompareTaskID != uuid.Nil {
		taskIDs = append(taskIDs, request.CompareTaskID)
	}

	items := []types.DeploymentTimelineItem{}
	if len(taskIDs) > 0 {
		taskItems, err := queryDeploymentTimelineItems(ctx, types.DeploymentTimelineQuery{
			OrganizationID:      request.OrganizationID,
			IncludeNonTerminal:  true,
			IncludeRedeployInfo: true,
		}, taskIDs)
		if err != nil {
			return nil, err
		}
		items = append(items, taskItems...)
	}

	legacyRevisionIDs := make([]uuid.UUID, 0, 2)
	if request.BaseLegacyDeploymentRevisionID != uuid.Nil {
		legacyRevisionIDs = append(legacyRevisionIDs, request.BaseLegacyDeploymentRevisionID)
	}
	if request.CompareLegacyDeploymentRevisionID != uuid.Nil {
		legacyRevisionIDs = append(legacyRevisionIDs, request.CompareLegacyDeploymentRevisionID)
	}
	if len(legacyRevisionIDs) > 0 {
		legacyItems, err := queryLegacyDeploymentTimelineItems(
			ctx,
			types.DeploymentTimelineQuery{OrganizationID: request.OrganizationID},
			len(legacyRevisionIDs),
			legacyRevisionIDs,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, legacyItems...)
	}

	base, ok := findDeploymentTimelineCompareItem(
		items,
		request.BaseTaskID,
		request.BaseLegacyDeploymentRevisionID,
	)
	if !ok {
		return nil, apierrors.ErrNotFound
	}
	compare, ok := findDeploymentTimelineCompareItem(
		items,
		request.CompareTaskID,
		request.CompareLegacyDeploymentRevisionID,
	)
	if !ok {
		return nil, apierrors.ErrNotFound
	}

	comparison := &types.DeploymentTimelineComparison{
		Base:       base,
		Compare:    compare,
		Components: compareTimelineComponents(base.Components, compare.Components),
	}
	if base.DeploymentPlanID == uuid.Nil || compare.DeploymentPlanID == uuid.Nil {
		return comparison, nil
	}

	basePlan, err := GetDeploymentPlan(ctx, base.DeploymentPlanID, request.OrganizationID)
	if err != nil {
		return nil, err
	}
	comparePlan, err := GetDeploymentPlan(ctx, compare.DeploymentPlanID, request.OrganizationID)
	if err != nil {
		return nil, err
	}
	process, err := compareTimelineProcess(ctx, basePlan, comparePlan, request.OrganizationID)
	if err != nil {
		return nil, err
	}
	comparison.Process = process
	comparison.Steps = compareTimelineSteps(basePlan.Steps, comparePlan.Steps)
	comparison.Variables = compareTimelineVariables(basePlan.Variables, comparePlan.Variables)
	return comparison, nil
}

func CreateDeploymentPlanFromTimelineTask(
	ctx context.Context,
	request types.CreateDeploymentTimelineRedeployRequest,
) (*types.DeploymentTimelineRedeploy, error) {
	if request.OrganizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	if request.TaskID == uuid.Nil {
		return nil, apierrors.NewBadRequest("taskId is required")
	}
	task, err := GetTask(ctx, request.TaskID, request.OrganizationID)
	if err != nil {
		return nil, err
	}
	if task.TaskType != types.TaskTypeDeployment {
		return nil, apierrors.ErrNotFound
	}
	plan, err := CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  request.OrganizationID,
		ReleaseBundleID: task.ReleaseBundleID,
		EnvironmentID:   task.EnvironmentID,
		TargetIDs:       []uuid.UUID{task.DeploymentTargetID},
	})
	if err != nil {
		return nil, err
	}
	return &types.DeploymentTimelineRedeploy{
		Plan:    *plan,
		Warning: deployPreviousReleaseWarning,
	}, nil
}

func queryDeploymentTimelineItems(
	ctx context.Context,
	query types.DeploymentTimelineQuery,
	taskIDs []uuid.UUID,
) ([]types.DeploymentTimelineItem, error) {
	if query.OrganizationID == uuid.Nil {
		return nil, apierrors.NewBadRequest("organizationId is required")
	}
	includeLegacy := taskIDs == nil
	if taskIDs == nil {
		taskIDs = []uuid.UUID{}
	}
	limit := query.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(ctx,
		`SELECT
			t.id AS task_id,
			t.deployment_plan_id,
			t.deployment_plan_target_id,
			t.deployment_target_id,
			t.application_id,
			a.name AS application_name,
			t.release_bundle_id,
			rb.release_number,
			t.channel_id,
			c.name AS channel_name,
			t.environment_id,
			e.name AS environment_name,
			dpt.customer_organization_id,
			dpt.name AS deployment_target_name,
			t.actor_user_account_id,
			t.status,
			t.queued_at,
			t.started_at,
			t.completed_at,
			dp.process_snapshot_id,
			dp.variable_snapshot_id,
			t.status = @succeededStatus AND NOT EXISTS (
				SELECT 1
				FROM Task newer
				WHERE newer.organization_id = t.organization_id
					AND newer.task_type = @taskType
					AND newer.application_id = t.application_id
					AND newer.environment_id = t.environment_id
					AND newer.deployment_target_id = t.deployment_target_id
					AND newer.status = @succeededStatus
					AND (
						COALESCE(newer.completed_at, newer.started_at, newer.queued_at),
						newer.queue_order,
						newer.id
					) > (
						COALESCE(t.completed_at, t.started_at, t.queued_at),
						t.queue_order,
						t.id
					)
			) AS last_successful
		FROM Task t
		JOIN DeploymentPlan dp
			ON dp.id = t.deployment_plan_id
			AND dp.organization_id = t.organization_id
		JOIN DeploymentPlanTarget dpt
			ON dpt.id = t.deployment_plan_target_id
			AND dpt.deployment_plan_id = t.deployment_plan_id
			AND dpt.organization_id = t.organization_id
		JOIN ReleaseBundle rb
			ON rb.id = t.release_bundle_id
			AND rb.organization_id = t.organization_id
		JOIN Application a
			ON a.id = t.application_id
			AND a.organization_id = t.organization_id
		JOIN Channel c
			ON c.id = t.channel_id
			AND c.organization_id = t.organization_id
		JOIN Environment e
			ON e.id = t.environment_id
			AND e.organization_id = t.organization_id
		WHERE t.organization_id = @organizationId
			AND t.task_type = @taskType
			AND (cardinality(@taskIds::uuid[]) = 0 OR t.id = ANY(@taskIds::uuid[]))
			AND (@applicationId::uuid IS NULL OR t.application_id = @applicationId::uuid)
			AND (@releaseBundleId::uuid IS NULL OR t.release_bundle_id = @releaseBundleId::uuid)
			AND (@environmentId::uuid IS NULL OR t.environment_id = @environmentId::uuid)
			AND (@deploymentTargetId::uuid IS NULL OR t.deployment_target_id = @deploymentTargetId::uuid)
			AND (@customerOrganizationId::uuid IS NULL OR dpt.customer_organization_id = @customerOrganizationId::uuid)
			AND (@includeNonTerminal OR t.status IN (@succeededStatus, @failedStatus, @canceledStatus))
		ORDER BY COALESCE(t.completed_at, t.started_at, t.queued_at) DESC, t.queue_order DESC, t.id DESC
		LIMIT @limit`,
		pgx.NamedArgs{
			"organizationId":         query.OrganizationID,
			"taskType":               types.TaskTypeDeployment,
			"taskIds":                taskIDs,
			"applicationId":          query.ApplicationID,
			"releaseBundleId":        query.ReleaseBundleID,
			"environmentId":          query.EnvironmentID,
			"deploymentTargetId":     query.DeploymentTargetID,
			"customerOrganizationId": query.CustomerOrganizationID,
			"includeNonTerminal":     query.IncludeNonTerminal,
			"succeededStatus":        types.TaskStatusSucceeded,
			"failedStatus":           types.TaskStatusFailed,
			"canceledStatus":         types.TaskStatusCanceled,
			"limit":                  limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query deployment timeline: %w", err)
	}
	defer rows.Close()
	items := []types.DeploymentTimelineItem{}
	for rows.Next() {
		var item types.DeploymentTimelineItem
		if err := rows.Scan(
			&item.TaskID,
			&item.DeploymentPlanID,
			&item.DeploymentPlanTargetID,
			&item.DeploymentTargetID,
			&item.ApplicationID,
			&item.ApplicationName,
			&item.ReleaseBundleID,
			&item.ReleaseNumber,
			&item.ChannelID,
			&item.ChannelName,
			&item.EnvironmentID,
			&item.EnvironmentName,
			&item.CustomerOrganizationID,
			&item.DeploymentTargetName,
			&item.ActorUserAccountID,
			&item.Status,
			&item.QueuedAt,
			&item.StartedAt,
			&item.CompletedAt,
			&item.ProcessSnapshotID,
			&item.VariableSnapshotID,
			&item.LastSuccessful,
		); err != nil {
			return nil, fmt.Errorf("could not scan deployment timeline item: %w", err)
		}
		item.Source = types.DeploymentTimelineItemSourceTask
		item.Availability = types.DeploymentCompatibilityAvailability{
			ProcessSnapshot:  item.ProcessSnapshotID != nil,
			VariableSnapshot: item.VariableSnapshotID != nil,
			Channel:          true,
			Environment:      true,
			TaskLogs:         true,
			RedeployPlan:     query.IncludeRedeployInfo && item.ReleaseBundleID != uuid.Nil,
		}
		item.RedeployAvailable = query.IncludeRedeployInfo && item.ReleaseBundleID != uuid.Nil
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("could not collect deployment timeline: %w", rows.Err())
	}
	for i := range items {
		components, err := getDeploymentTimelineComponents(ctx, items[i].ReleaseBundleID, query.OrganizationID)
		if err != nil {
			return nil, err
		}
		items[i].Components = components
	}
	if includeLegacy {
		legacyItems, err := queryLegacyDeploymentTimelineItems(ctx, query, limit, nil)
		if err != nil {
			return nil, err
		}
		items = append(items, legacyItems...)
		sort.SliceStable(items, func(i, j int) bool {
			left := deploymentTimelineSortTime(items[i])
			right := deploymentTimelineSortTime(items[j])
			if left.Equal(right) {
				return items[i].LegacyDeploymentRevisionID.String()+items[i].TaskID.String() >
					items[j].LegacyDeploymentRevisionID.String()+items[j].TaskID.String()
			}
			return left.After(right)
		})
		if len(items) > limit {
			items = items[:limit]
		}
	}
	return items, nil
}

func queryLegacyDeploymentTimelineItems(
	ctx context.Context,
	query types.DeploymentTimelineQuery,
	limit int,
	legacyRevisionIDs []uuid.UUID,
) ([]types.DeploymentTimelineItem, error) {
	if len(legacyRevisionIDs) == 0 && (query.ReleaseBundleID != nil || query.EnvironmentID != nil) {
		return []types.DeploymentTimelineItem{}, nil
	}
	if legacyRevisionIDs == nil {
		legacyRevisionIDs = []uuid.UUID{}
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	db := internalctx.GetDb(ctx)
	rows, err := db.Query(
		ctx,
		`SELECT
			dcm.legacy_deployment_id,
			dcm.legacy_deployment_revision_id,
			dcm.deployment_target_id,
			dcm.application_id,
			a.name AS application_name,
			dcm.application_version_id,
			av.name AS application_version_name,
			dcm.synthetic_release_id,
			dr.created_at,
			dt.customer_organization_id,
			dt.name AS deployment_target_name,
			dcm.process_snapshot_available,
			dcm.variable_snapshot_available,
			dcm.channel_available,
			dcm.environment_available,
			dcm.task_logs_available,
			dcm.redeploy_plan_available
		FROM DeploymentCompatibilityMetadata dcm
		JOIN DeploymentRevision dr
			ON dr.id = dcm.legacy_deployment_revision_id
		JOIN DeploymentTarget dt
			ON dt.id = dcm.deployment_target_id
			AND dt.organization_id = dcm.organization_id
		JOIN Application a
			ON a.id = dcm.application_id
			AND a.organization_id = dcm.organization_id
		JOIN ApplicationVersion av
			ON av.id = dcm.application_version_id
		WHERE dcm.organization_id = @organizationId
			AND (cardinality(@legacyRevisionIds::uuid[]) = 0 OR dcm.legacy_deployment_revision_id = ANY(@legacyRevisionIds::uuid[]))
			AND (@applicationId::uuid IS NULL OR dcm.application_id = @applicationId::uuid)
			AND (@deploymentTargetId::uuid IS NULL OR dcm.deployment_target_id = @deploymentTargetId::uuid)
			AND (@customerOrganizationId::uuid IS NULL OR dt.customer_organization_id = @customerOrganizationId::uuid)
		ORDER BY dr.created_at DESC, dcm.legacy_deployment_revision_id DESC
		LIMIT @limit`,
		pgx.NamedArgs{
			"organizationId":         query.OrganizationID,
			"legacyRevisionIds":      legacyRevisionIDs,
			"applicationId":          query.ApplicationID,
			"deploymentTargetId":     query.DeploymentTargetID,
			"customerOrganizationId": query.CustomerOrganizationID,
			"limit":                  limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("could not query legacy deployment timeline: %w", err)
	}
	defer rows.Close()
	items := []types.DeploymentTimelineItem{}
	for rows.Next() {
		var item types.DeploymentTimelineItem
		var applicationVersionID uuid.UUID
		var applicationVersionName string
		if err := rows.Scan(
			&item.LegacyDeploymentID,
			&item.LegacyDeploymentRevisionID,
			&item.DeploymentTargetID,
			&item.ApplicationID,
			&item.ApplicationName,
			&applicationVersionID,
			&applicationVersionName,
			&item.SyntheticReleaseID,
			&item.QueuedAt,
			&item.CustomerOrganizationID,
			&item.DeploymentTargetName,
			&item.Availability.ProcessSnapshot,
			&item.Availability.VariableSnapshot,
			&item.Availability.Channel,
			&item.Availability.Environment,
			&item.Availability.TaskLogs,
			&item.Availability.RedeployPlan,
		); err != nil {
			return nil, fmt.Errorf("could not scan legacy deployment timeline item: %w", err)
		}
		item.Source = types.DeploymentTimelineItemSourceLegacyDeployment
		item.CompletedAt = timePtr(item.QueuedAt)
		item.Components = []types.DeploymentTimelineComponent{
			{
				Key:     "application",
				Name:    item.ApplicationName,
				Type:    types.ReleaseBundleComponentTypeApplicationVersion,
				Version: applicationVersionName,
			},
		}
		_ = applicationVersionID
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("could not collect legacy deployment timeline: %w", rows.Err())
	}
	return items, nil
}

func deploymentTimelineSortTime(item types.DeploymentTimelineItem) time.Time {
	if item.CompletedAt != nil {
		return *item.CompletedAt
	}
	if item.StartedAt != nil {
		return *item.StartedAt
	}
	return item.QueuedAt
}

func getDeploymentTimelineComponents(
	ctx context.Context,
	releaseBundleID uuid.UUID,
	orgID uuid.UUID,
) ([]types.DeploymentTimelineComponent, error) {
	bundle, err := GetReleaseBundle(ctx, releaseBundleID, orgID)
	if err != nil {
		return nil, err
	}
	components := make([]types.DeploymentTimelineComponent, 0, len(bundle.Components))
	for _, component := range bundle.Components {
		components = append(components, types.DeploymentTimelineComponent{
			Key:     component.Key,
			Name:    component.Name,
			Type:    component.Type,
			Version: component.Version,
		})
	}
	return components, nil
}

func validateDeploymentTimelineCompareRef(label string, taskID, legacyRevisionID uuid.UUID) error {
	count := 0
	if taskID != uuid.Nil {
		count++
	}
	if legacyRevisionID != uuid.Nil {
		count++
	}
	switch count {
	case 0:
		return apierrors.NewBadRequest(label + "TaskId or " + label + "LegacyDeploymentRevisionId is required")
	case 1:
		return nil
	default:
		return apierrors.NewBadRequest(label + " must reference exactly one timeline entry")
	}
}

func findDeploymentTimelineCompareItem(
	items []types.DeploymentTimelineItem,
	taskID uuid.UUID,
	legacyRevisionID uuid.UUID,
) (types.DeploymentTimelineItem, bool) {
	for _, item := range items {
		if taskID != uuid.Nil && item.TaskID == taskID {
			return item, true
		}
		if legacyRevisionID != uuid.Nil && item.LegacyDeploymentRevisionID == legacyRevisionID {
			return item, true
		}
	}
	return types.DeploymentTimelineItem{}, false
}

func compareTimelineProcess(
	ctx context.Context,
	basePlan *types.DeploymentPlan,
	comparePlan *types.DeploymentPlan,
	orgID uuid.UUID,
) (types.DeploymentTimelineProcessChange, error) {
	change := types.DeploymentTimelineProcessChange{
		BaseProcessSnapshotID:    basePlan.ProcessSnapshotID,
		CompareProcessSnapshotID: comparePlan.ProcessSnapshotID,
		Changed:                  !uuidPointersEqual(basePlan.ProcessSnapshotID, comparePlan.ProcessSnapshotID),
	}
	if basePlan.ProcessSnapshotID != nil {
		snapshot, err := GetProcessSnapshot(ctx, *basePlan.ProcessSnapshotID, orgID)
		if err != nil {
			return change, err
		}
		change.BaseRevisionNumber = snapshot.RevisionNumber
		change.BaseCanonicalChecksum = snapshot.CanonicalChecksum
	}
	if comparePlan.ProcessSnapshotID != nil {
		snapshot, err := GetProcessSnapshot(ctx, *comparePlan.ProcessSnapshotID, orgID)
		if err != nil {
			return change, err
		}
		change.CompareRevisionNumber = snapshot.RevisionNumber
		change.CompareCanonicalChecksum = snapshot.CanonicalChecksum
	}
	if change.BaseCanonicalChecksum != "" || change.CompareCanonicalChecksum != "" {
		change.Changed = change.BaseCanonicalChecksum != change.CompareCanonicalChecksum
	}
	return change, nil
}

func compareTimelineComponents(
	baseComponents []types.DeploymentTimelineComponent,
	compareComponents []types.DeploymentTimelineComponent,
) []types.DeploymentTimelineComponentChange {
	baseByKey := timelineComponentsByKey(baseComponents)
	compareByKey := timelineComponentsByKey(compareComponents)
	keys := sortedUnionKeys(baseByKey, compareByKey)
	changes := make([]types.DeploymentTimelineComponentChange, 0, len(keys))
	for _, key := range keys {
		base, baseOK := baseByKey[key]
		compare, compareOK := compareByKey[key]
		change := types.DeploymentTimelineComponentChange{Key: key, Kind: timelineChangeKind(baseOK, compareOK)}
		if baseOK {
			change.Name = base.Name
			change.BaseVersion = base.Version
			change.BaseType = base.Type
		}
		if compareOK {
			if change.Name == "" {
				change.Name = compare.Name
			}
			change.CompareVersion = compare.Version
			change.CompareType = compare.Type
		}
		if baseOK && compareOK && (base.Name != compare.Name || base.Version != compare.Version || base.Type != compare.Type) {
			change.Kind = types.DeploymentTimelineChangeChanged
		}
		changes = append(changes, change)
	}
	return changes
}

func compareTimelineSteps(
	baseSteps []types.DeploymentPlanStep,
	compareSteps []types.DeploymentPlanStep,
) []types.DeploymentTimelineStepChange {
	baseByKey := deploymentPlanStepsByKey(baseSteps)
	compareByKey := deploymentPlanStepsByKey(compareSteps)
	keys := sortedUnionKeys(baseByKey, compareByKey)
	changes := make([]types.DeploymentTimelineStepChange, 0, len(keys))
	for _, key := range keys {
		base, baseOK := baseByKey[key]
		compare, compareOK := compareByKey[key]
		change := types.DeploymentTimelineStepChange{StepKey: key, Kind: timelineChangeKind(baseOK, compareOK)}
		if baseOK {
			change.Name = base.Name
			change.BaseActionType = base.ActionType
			change.BaseIncluded = boolPtr(base.Included)
		}
		if compareOK {
			if change.Name == "" {
				change.Name = compare.Name
			}
			change.CompareActionType = compare.ActionType
			change.CompareIncluded = boolPtr(compare.Included)
		}
		if baseOK && compareOK && !deploymentPlanStepsEqual(base, compare) {
			change.Kind = types.DeploymentTimelineChangeChanged
		}
		changes = append(changes, change)
	}
	return changes
}

func compareTimelineVariables(
	baseVariables []types.DeploymentPlanVariable,
	compareVariables []types.DeploymentPlanVariable,
) []types.DeploymentTimelineVariableChange {
	baseByKey := deploymentPlanVariablesByKey(baseVariables)
	compareByKey := deploymentPlanVariablesByKey(compareVariables)
	keys := sortedUnionKeys(baseByKey, compareByKey)
	changes := make([]types.DeploymentTimelineVariableChange, 0, len(keys))
	for _, key := range keys {
		base, baseOK := baseByKey[key]
		compare, compareOK := compareByKey[key]
		change := types.DeploymentTimelineVariableChange{Key: key, Kind: timelineChangeKind(baseOK, compareOK)}
		if baseOK {
			change.BaseStatus = base.Status
			change.BaseSource = base.Source
			change.BaseRedacted = base.Redacted
			change.BaseReference = firstNonEmpty(base.ReferenceName, base.ReferenceID)
			if !base.Redacted {
				change.BaseValue = append(json.RawMessage(nil), base.Value...)
			}
		}
		if compareOK {
			change.CompareStatus = compare.Status
			change.CompareSource = compare.Source
			change.CompareRedacted = compare.Redacted
			change.CompareReference = firstNonEmpty(compare.ReferenceName, compare.ReferenceID)
			if !compare.Redacted {
				change.CompareValue = append(json.RawMessage(nil), compare.Value...)
			}
		}
		if baseOK && compareOK && !deploymentPlanVariablesEqual(base, compare) {
			change.Kind = types.DeploymentTimelineChangeChanged
		}
		changes = append(changes, change)
	}
	return changes
}

func timelineComponentsByKey(components []types.DeploymentTimelineComponent) map[string]types.DeploymentTimelineComponent {
	byKey := make(map[string]types.DeploymentTimelineComponent, len(components))
	for _, component := range components {
		byKey[component.Key] = component
	}
	return byKey
}

func deploymentPlanStepsByKey(steps []types.DeploymentPlanStep) map[string]types.DeploymentPlanStep {
	byKey := make(map[string]types.DeploymentPlanStep, len(steps))
	for _, step := range steps {
		byKey[step.StepKey] = step
	}
	return byKey
}

func deploymentPlanVariablesByKey(variables []types.DeploymentPlanVariable) map[string]types.DeploymentPlanVariable {
	byKey := make(map[string]types.DeploymentPlanVariable, len(variables))
	for _, variable := range variables {
		byKey[variable.Key] = variable
	}
	return byKey
}

func sortedUnionKeys[T any](base map[string]T, compare map[string]T) []string {
	seen := map[string]struct{}{}
	keys := make([]string, 0, len(base)+len(compare))
	for key := range base {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range compare {
		if _, ok := seen[key]; ok {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func timelineChangeKind(baseOK bool, compareOK bool) types.DeploymentTimelineChangeKind {
	switch {
	case baseOK && compareOK:
		return types.DeploymentTimelineChangeUnchanged
	case baseOK:
		return types.DeploymentTimelineChangeRemoved
	default:
		return types.DeploymentTimelineChangeAdded
	}
}

func deploymentPlanStepsEqual(base, compare types.DeploymentPlanStep) bool {
	return base.Name == compare.Name &&
		base.ActionType == compare.ActionType &&
		base.ActionName == compare.ActionName &&
		base.ExecutionLocation == compare.ExecutionLocation &&
		base.Condition == compare.Condition &&
		base.FailureMode == compare.FailureMode &&
		base.TimeoutSeconds == compare.TimeoutSeconds &&
		base.RetryMaxAttempts == compare.RetryMaxAttempts &&
		base.RetryIntervalSeconds == compare.RetryIntervalSeconds &&
		base.Included == compare.Included &&
		base.ExcludedReason == compare.ExcludedReason &&
		reflect.DeepEqual(base.InputBindings, compare.InputBindings) &&
		reflect.DeepEqual(base.TargetTags, compare.TargetTags) &&
		reflect.DeepEqual(base.RequiredPermissions, compare.RequiredPermissions) &&
		reflect.DeepEqual(base.Dependencies, compare.Dependencies)
}

func deploymentPlanVariablesEqual(base, compare types.DeploymentPlanVariable) bool {
	return base.Type == compare.Type &&
		base.IsRequired == compare.IsRequired &&
		base.Status == compare.Status &&
		base.Source == compare.Source &&
		base.ReferenceID == compare.ReferenceID &&
		base.ReferenceName == compare.ReferenceName &&
		base.Redacted == compare.Redacted &&
		(base.Redacted || compare.Redacted || bytes.Equal(base.Value, compare.Value))
}

func uuidPointersEqual(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func boolPtr(value bool) *bool {
	return &value
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
