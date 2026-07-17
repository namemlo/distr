package db_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/db/queryable"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestDeploymentTimelineRepositoryListsFilteredItemsAndMarksLastSuccessful(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	targetID := deps.plan.Targets[0].DeploymentTargetID
	firstTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(markTaskSucceeded(ctx, deps.orgID, firstTasks[0].ID)).To(Succeed())

	secondPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Task queue deploy updated",
		targetID,
	)
	secondTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   secondPlan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(markTaskSucceeded(ctx, deps.orgID, secondTasks[0].ID)).To(Succeed())

	timeline, err := db.GetDeploymentTimeline(ctx, types.DeploymentTimelineQuery{
		OrganizationID:      deps.orgID,
		ApplicationID:       &deps.applicationID,
		EnvironmentID:       &deps.devEnvironmentID,
		DeploymentTargetID:  &targetID,
		Limit:               20,
		IncludeNonTerminal:  true,
		IncludeRedeployInfo: true,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(timeline.Items).To(HaveLen(2))
	g.Expect(timeline.Items[0].TaskID).To(Equal(secondTasks[0].ID))
	g.Expect(timeline.Items[0].ReleaseNumber).NotTo(BeEmpty())
	g.Expect(timeline.Items[0].DeploymentTargetName).To(Equal(deps.plan.Targets[0].Name))
	g.Expect(timeline.Items[0].ActorUserAccountID).To(Equal(&deps.actorID))
	g.Expect(timeline.Items[0].LastSuccessful).To(BeTrue())
	g.Expect(timeline.Items[1].TaskID).To(Equal(firstTasks[0].ID))
	g.Expect(timeline.Items[1].LastSuccessful).To(BeFalse())
}

func TestDeploymentTimelineRepositoryIgnoresNonDeploymentTasks(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	deploymentTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	runbookPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Runbook-shaped task",
		deps.plan.Targets[0].DeploymentTargetID,
	)
	runbookTaskID := insertTimelineTestTaskWithType(t, ctx, runbookPlan.ID, deps.orgID, types.TaskTypeRunbook)

	timeline, err := db.GetDeploymentTimeline(ctx, types.DeploymentTimelineQuery{
		OrganizationID:     deps.orgID,
		IncludeNonTerminal: true,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(timeline.Items).To(ContainElement(WithTransform(
		func(item types.DeploymentTimelineItem) uuid.UUID { return item.TaskID },
		Equal(deploymentTasks[0].ID),
	)))
	g.Expect(timeline.Items).NotTo(ContainElement(WithTransform(
		func(item types.DeploymentTimelineItem) uuid.UUID { return item.TaskID },
		Equal(runbookTaskID),
	)))

	_, err = db.CompareDeploymentTimelineTasks(ctx, types.DeploymentTimelineCompareRequest{
		OrganizationID: deps.orgID,
		BaseTaskID:     deploymentTasks[0].ID,
		CompareTaskID:  runbookTaskID,
	})
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestDeploymentTimelineRepositoryAllowsHistoricalTasksWithoutActor(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:   deps.orgID,
		DeploymentPlanID: deps.plan.ID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	timeline, err := db.GetDeploymentTimeline(ctx, types.DeploymentTimelineQuery{
		OrganizationID:      deps.orgID,
		IncludeNonTerminal:  true,
		IncludeRedeployInfo: true,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(timeline.Items).To(HaveLen(1))
	g.Expect(timeline.Items[0].TaskID).To(Equal(tasks[0].ID))
	g.Expect(timeline.Items[0].ActorUserAccountID).To(BeNil())
	g.Expect(timeline.Items[0].RedeployAvailable).To(BeFalse())
}

func TestDeploymentTimelineRepositoryIncludesLegacyCompatibilityEntries(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, _, versionID := createReleaseBundleDependencies(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, orgID, "legacy-target")
	request := api.DeploymentRequest{
		DeploymentTargetID:   targetID,
		ApplicationVersionID: versionID,
		ValuesHash:           []byte("stored-values-hash"),
	}
	g.Expect(db.CreateDeployment(ctx, &request)).To(Succeed())
	revision, err := db.CreateDeploymentRevision(ctx, &request)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.BackfillLegacyDeploymentCompatibility(ctx, types.DeploymentCompatibilityBackfillRequest{
		OrganizationID: orgID,
		Apply:          true,
		BatchSize:      10,
	})
	g.Expect(err).NotTo(HaveOccurred())

	timeline, err := db.GetDeploymentTimeline(ctx, types.DeploymentTimelineQuery{
		OrganizationID:      orgID,
		ApplicationID:       &applicationID,
		DeploymentTargetID:  &targetID,
		IncludeRedeployInfo: true,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(timeline.Items).To(HaveLen(1))
	item := timeline.Items[0]
	g.Expect(item.Source).To(Equal(types.DeploymentTimelineItemSourceLegacyDeployment))
	g.Expect(item.TaskID).To(Equal(uuid.Nil))
	g.Expect(item.DeploymentPlanID).To(Equal(uuid.Nil))
	g.Expect(item.ReleaseBundleID).To(Equal(uuid.Nil))
	g.Expect(item.ChannelID).To(Equal(uuid.Nil))
	g.Expect(item.EnvironmentID).To(Equal(uuid.Nil))
	g.Expect(item.LegacyDeploymentID).To(Equal(*request.DeploymentID))
	g.Expect(item.LegacyDeploymentRevisionID).To(Equal(revision.ID))
	g.Expect(item.DeploymentTargetID).To(Equal(targetID))
	g.Expect(item.ApplicationID).To(Equal(applicationID))
	g.Expect(item.Components).To(HaveLen(1))
	g.Expect(item.Components[0].Type).To(Equal(types.ReleaseBundleComponentTypeApplicationVersion))
	g.Expect(item.Availability.ProcessSnapshot).To(BeFalse())
	g.Expect(item.Availability.VariableSnapshot).To(BeFalse())
	g.Expect(item.Availability.Channel).To(BeFalse())
	g.Expect(item.Availability.Environment).To(BeFalse())
	g.Expect(item.Availability.TaskLogs).To(BeFalse())
	g.Expect(item.Availability.RedeployPlan).To(BeFalse())
	g.Expect(item.RedeployAvailable).To(BeFalse())
}

func TestDeploymentTimelineRepositoryReadsThroughPartialCompatibilityBackfill(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, _, versionID := createReleaseBundleDependencies(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, orgID, "legacy-target")
	firstRequest, firstRevision := createLegacyDeploymentRevisionForTimelineTest(
		t,
		ctx,
		targetID,
		versionID,
		"first-values-hash",
	)
	_, secondRevision := createLegacyDeploymentRevisionForTimelineTest(
		t,
		ctx,
		targetID,
		versionID,
		"second-values-hash",
	)
	insertDeploymentCompatibilityMetadataForTimelineTest(
		t,
		ctx,
		orgID,
		*firstRequest.DeploymentID,
		firstRevision.ID,
		targetID,
		applicationID,
		versionID,
	)

	timeline, err := db.GetDeploymentTimeline(ctx, types.DeploymentTimelineQuery{
		OrganizationID:      orgID,
		ApplicationID:       &applicationID,
		DeploymentTargetID:  &targetID,
		IncludeRedeployInfo: true,
		Limit:               10,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(timeline.Items).To(HaveLen(2))
	itemsByRevision := map[uuid.UUID]types.DeploymentTimelineItem{}
	for _, item := range timeline.Items {
		itemsByRevision[item.LegacyDeploymentRevisionID] = item
	}
	persisted := itemsByRevision[firstRevision.ID]
	readThrough := itemsByRevision[secondRevision.ID]
	g.Expect(persisted.Source).To(Equal(types.DeploymentTimelineItemSourceLegacyDeployment))
	g.Expect(readThrough.Source).To(Equal(types.DeploymentTimelineItemSourceLegacyDeployment))
	g.Expect(readThrough.LegacyDeploymentRevisionID).To(Equal(secondRevision.ID))
	g.Expect(readThrough.LegacyDeploymentID).NotTo(Equal(uuid.Nil))
	g.Expect(readThrough.SyntheticReleaseID).NotTo(Equal(uuid.Nil))
	g.Expect(readThrough.Components).To(HaveLen(1))
	g.Expect(readThrough.Components[0].Type).To(Equal(types.ReleaseBundleComponentTypeApplicationVersion))

	comparison, err := db.CompareDeploymentTimelineTasks(ctx, types.DeploymentTimelineCompareRequest{
		OrganizationID:                    orgID,
		BaseLegacyDeploymentRevisionID:    firstRevision.ID,
		CompareLegacyDeploymentRevisionID: secondRevision.ID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(comparison.Base.LegacyDeploymentRevisionID).To(Equal(firstRevision.ID))
	g.Expect(comparison.Compare.LegacyDeploymentRevisionID).To(Equal(secondRevision.ID))
	g.Expect(comparison.Components).NotTo(BeEmpty())
	expectComparisonAvailability(t, g, comparison, false, false, false)
}

func TestDeploymentTimelineRepositoryReadsLegacyEntriesWithoutPerRowQueries(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	orgID, applicationID, _, versionID := createReleaseBundleDependencies(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, orgID, "legacy-target")
	for i := 0; i < 5; i++ {
		createLegacyDeploymentRevisionForTimelineTest(t, ctx, targetID, versionID, "values-hash")
	}

	counting := &countingQueryable{Queryable: internalctx.GetDb(ctx)}
	countingCtx := internalctx.WithDb(ctx, counting)
	timeline, err := db.GetDeploymentTimeline(countingCtx, types.DeploymentTimelineQuery{
		OrganizationID:     orgID,
		ApplicationID:      &applicationID,
		DeploymentTargetID: &targetID,
		Limit:              10,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(timeline.Items).To(HaveLen(5))
	g.Expect(counting.queryCount).To(BeNumerically("<=", 2))
}

func TestDeploymentTimelineRepositoryComparesLegacyCompatibilityEntries(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	targetID := deps.plan.Targets[0].DeploymentTargetID
	task, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	firstRequest := api.DeploymentRequest{
		DeploymentTargetID:   targetID,
		ApplicationVersionID: deps.versionID,
		ValuesHash:           []byte("first-values-hash"),
	}
	g.Expect(db.CreateDeployment(ctx, &firstRequest)).To(Succeed())
	firstRevision, err := db.CreateDeploymentRevision(ctx, &firstRequest)
	g.Expect(err).NotTo(HaveOccurred())
	secondRequest := api.DeploymentRequest{
		DeploymentTargetID:   targetID,
		ApplicationVersionID: deps.versionID,
		ValuesHash:           []byte("second-values-hash"),
	}
	g.Expect(db.CreateDeployment(ctx, &secondRequest)).To(Succeed())
	secondRevision, err := db.CreateDeploymentRevision(ctx, &secondRequest)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.BackfillLegacyDeploymentCompatibility(ctx, types.DeploymentCompatibilityBackfillRequest{
		OrganizationID: deps.orgID,
		Apply:          true,
		BatchSize:      10,
	})
	g.Expect(err).NotTo(HaveOccurred())

	legacyComparison, err := db.CompareDeploymentTimelineTasks(ctx, types.DeploymentTimelineCompareRequest{
		OrganizationID:                    deps.orgID,
		BaseLegacyDeploymentRevisionID:    firstRevision.ID,
		CompareLegacyDeploymentRevisionID: secondRevision.ID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(legacyComparison.Base.LegacyDeploymentRevisionID).To(Equal(firstRevision.ID))
	g.Expect(legacyComparison.Compare.LegacyDeploymentRevisionID).To(Equal(secondRevision.ID))
	g.Expect(legacyComparison.Base.Source).To(Equal(types.DeploymentTimelineItemSourceLegacyDeployment))
	g.Expect(legacyComparison.Compare.Source).To(Equal(types.DeploymentTimelineItemSourceLegacyDeployment))
	g.Expect(legacyComparison.Components).NotTo(BeEmpty())
	g.Expect(legacyComparison.Steps).To(BeEmpty())
	g.Expect(legacyComparison.Variables).To(BeEmpty())
	g.Expect(legacyComparison.Process.Changed).To(BeFalse())
	expectComparisonAvailability(t, g, legacyComparison, false, false, false)

	mixedComparison, err := db.CompareDeploymentTimelineTasks(ctx, types.DeploymentTimelineCompareRequest{
		OrganizationID:                 deps.orgID,
		BaseLegacyDeploymentRevisionID: firstRevision.ID,
		CompareTaskID:                  task[0].ID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(mixedComparison.Base.LegacyDeploymentRevisionID).To(Equal(firstRevision.ID))
	g.Expect(mixedComparison.Compare.TaskID).To(Equal(task[0].ID))
	g.Expect(mixedComparison.Components).NotTo(BeEmpty())
	g.Expect(mixedComparison.Steps).To(BeEmpty())
	g.Expect(mixedComparison.Variables).To(BeEmpty())
	expectComparisonAvailability(t, g, mixedComparison, false, false, false)
}

func TestDeploymentTimelineRepositoryComparesReleaseProcessAndVariables(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	targetID := deps.plan.Targets[0].DeploymentTargetID
	firstTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	secondPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Task queue deploy with process changes",
		targetID,
	)
	secondTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   secondPlan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	comparison, err := db.CompareDeploymentTimelineTasks(ctx, types.DeploymentTimelineCompareRequest{
		OrganizationID: deps.orgID,
		BaseTaskID:     firstTasks[0].ID,
		CompareTaskID:  secondTasks[0].ID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(comparison.Base.TaskID).To(Equal(firstTasks[0].ID))
	g.Expect(comparison.Compare.TaskID).To(Equal(secondTasks[0].ID))
	g.Expect(comparison.Process.Changed).To(BeTrue())
	g.Expect(comparison.Process.BaseRevisionNumber).To(Equal(1))
	g.Expect(comparison.Process.CompareRevisionNumber).To(Equal(1))
	g.Expect(comparison.Components).To(ContainElement(WithTransform(
		func(change types.DeploymentTimelineComponentChange) string { return change.Key },
		Equal("api"),
	)))
	g.Expect(comparison.Variables).To(ContainElement(WithTransform(
		func(change types.DeploymentTimelineVariableChange) string { return change.Key },
		Equal("api_token"),
	)))
	expectComparisonAvailability(t, g, comparison, true, true, true)
}

func TestDeploymentTimelineRepositoryCreatesRedeployPlanFromTask(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	task, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(markTaskSucceeded(ctx, deps.orgID, task[0].ID)).To(Succeed())

	redeploy, err := db.CreateDeploymentPlanFromTimelineTask(ctx, types.CreateDeploymentTimelineRedeployRequest{
		OrganizationID:     deps.orgID,
		TaskID:             task[0].ID,
		ActorUserAccountID: deps.actorID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(redeploy.Warning).To(ContainSubstring("Deploy previous release"))
	g.Expect(redeploy.Plan.ID).NotTo(Equal(deps.plan.ID))
	g.Expect(redeploy.Plan.ReleaseBundleID).To(Equal(deps.plan.ReleaseBundleID))
	g.Expect(redeploy.Plan.EnvironmentID).To(Equal(deps.plan.EnvironmentID))
	g.Expect(redeploy.Plan.Targets).To(HaveLen(1))
	g.Expect(redeploy.Plan.Targets[0].DeploymentTargetID).To(Equal(deps.plan.Targets[0].DeploymentTargetID))
}

func TestDeploymentTimelineRepositoryRejectsRedeployFromUnsuccessfulTask(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.CreateDeploymentPlanFromTimelineTask(ctx, types.CreateDeploymentTimelineRedeployRequest{
		OrganizationID:     deps.orgID,
		TaskID:             tasks[0].ID,
		ActorUserAccountID: deps.actorID,
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func markTaskSucceeded(ctx context.Context, orgID, taskID uuid.UUID) error {
	if _, err := db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: orgID,
		TaskID:         taskID,
		Status:         types.TaskStatusRunning,
	}); err != nil {
		return err
	}
	_, err := db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: orgID,
		TaskID:         taskID,
		Status:         types.TaskStatusSucceeded,
	})
	return err
}

func insertTimelineTestTaskWithType(
	t testing.TB,
	ctx context.Context,
	planID uuid.UUID,
	orgID uuid.UUID,
	taskType types.TaskType,
) uuid.UUID {
	t.Helper()
	var taskID uuid.UUID
	err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`INSERT INTO Task AS t (
			organization_id,
			task_type,
			deployment_plan_id,
			deployment_plan_target_id,
			deployment_target_id,
			application_id,
			release_bundle_id,
			channel_id,
			environment_id,
			status
		)
		SELECT
			dp.organization_id,
			@taskType,
			dp.id,
			dpt.id,
			dpt.deployment_target_id,
			dp.application_id,
			dp.release_bundle_id,
			dp.channel_id,
			dp.environment_id,
			@status
		FROM DeploymentPlan dp
		JOIN DeploymentPlanTarget dpt
			ON dpt.deployment_plan_id = dp.id
			AND dpt.organization_id = dp.organization_id
		WHERE dp.id = @deploymentPlanId
			AND dp.organization_id = @organizationId
		ORDER BY dpt.sort_order, dpt.deployment_target_id
		LIMIT 1
		RETURNING t.id`,
		pgx.NamedArgs{
			"deploymentPlanId": planID,
			"organizationId":   orgID,
			"taskType":         taskType,
			"status":           types.TaskStatusQueued,
		},
	).Scan(&taskID)
	if err != nil {
		t.Fatalf("insert %s task: %v", taskType, err)
	}
	return taskID
}

func createLegacyDeploymentRevisionForTimelineTest(
	t testing.TB,
	ctx context.Context,
	targetID uuid.UUID,
	versionID uuid.UUID,
	valuesHash string,
) (api.DeploymentRequest, types.DeploymentRevision) {
	t.Helper()
	request := api.DeploymentRequest{
		DeploymentTargetID:   targetID,
		ApplicationVersionID: versionID,
		ValuesHash:           []byte(valuesHash),
	}
	if err := db.CreateDeployment(ctx, &request); err != nil {
		t.Fatalf("create legacy deployment: %v", err)
	}
	revision, err := db.CreateDeploymentRevision(ctx, &request)
	if err != nil {
		t.Fatalf("create legacy deployment revision: %v", err)
	}
	return request, *revision
}

func insertDeploymentCompatibilityMetadataForTimelineTest(
	t testing.TB,
	ctx context.Context,
	orgID uuid.UUID,
	deploymentID uuid.UUID,
	revisionID uuid.UUID,
	targetID uuid.UUID,
	applicationID uuid.UUID,
	versionID uuid.UUID,
) {
	t.Helper()
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`INSERT INTO DeploymentCompatibilityMetadata (
            organization_id,
            legacy_deployment_id,
            legacy_deployment_revision_id,
            deployment_target_id,
            application_id,
            application_version_id,
            synthetic_release_id,
            source,
            canonical_checksum,
            canonical_payload
        ) VALUES (
            @organizationId,
            @legacyDeploymentId,
            @legacyDeploymentRevisionId,
            @deploymentTargetId,
            @applicationId,
            @applicationVersionId,
            @syntheticReleaseId,
            @source,
            @canonicalChecksum,
            @canonicalPayload
        )`,
		pgx.NamedArgs{
			"organizationId":             orgID,
			"legacyDeploymentId":         deploymentID,
			"legacyDeploymentRevisionId": revisionID,
			"deploymentTargetId":         targetID,
			"applicationId":              applicationID,
			"applicationVersionId":       versionID,
			"syntheticReleaseId":         uuid.New(),
			"source":                     types.DeploymentCompatibilitySourceLegacyDirectDeployment,
			"canonicalChecksum":          "sha256:test",
			"canonicalPayload":           []byte(`{"source":"test"}`),
		},
	)
	if err != nil {
		t.Fatalf("insert compatibility metadata: %v", err)
	}
}

func expectComparisonAvailability(
	t testing.TB,
	g *WithT,
	comparison *types.DeploymentTimelineComparison,
	process bool,
	steps bool,
	variables bool,
) {
	t.Helper()
	availability := reflect.ValueOf(comparison).Elem().FieldByName("Availability")
	g.Expect(availability.IsValid()).To(BeTrue())
	g.Expect(availability.FieldByName("Process").Bool()).To(Equal(process))
	g.Expect(availability.FieldByName("Steps").Bool()).To(Equal(steps))
	g.Expect(availability.FieldByName("Variables").Bool()).To(Equal(variables))
}

type countingQueryable struct {
	queryable.Queryable
	queryCount int
}

func (q *countingQueryable) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	q.queryCount++
	return q.Queryable.Query(ctx, sql, args...)
}
