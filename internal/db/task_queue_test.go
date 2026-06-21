package db_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestTaskQueueRepositoryCreatesTasksForReadyPlanInQueueOrder(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a", "cluster-b")

	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(HaveLen(2))
	g.Expect(tasks[0].Status).To(Equal(types.TaskStatusQueued))
	g.Expect(tasks[1].Status).To(Equal(types.TaskStatusQueued))
	g.Expect(tasks[0].QueueOrder).To(BeNumerically("<", tasks[1].QueueOrder))
	g.Expect(tasks[0].DeploymentPlanID).To(Equal(deps.plan.ID))
	g.Expect(tasks[0].DeploymentPlanTargetID).To(Equal(deps.plan.Targets[0].ID))
	g.Expect(tasks[0].DeploymentTargetID).To(Equal(deps.plan.Targets[0].DeploymentTargetID))
	g.Expect(tasks[0].StepRuns).To(HaveLen(1))
	g.Expect(tasks[0].StepRuns[0].StepKey).To(Equal("deploy"))
	g.Expect(tasks[0].StepRuns[0].Status).To(Equal(types.StepRunStatusPending))

	listed, err := db.GetTasksByOrganizationID(ctx, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(HaveLen(2))
	g.Expect(listed[0].ID).To(Equal(tasks[0].ID))
	g.Expect(listed[1].ID).To(Equal(tasks[1].ID))
}

func TestTaskQueueRepositoryCreateTasksForPlanIsIdempotent(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	request := types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	}

	first, err := db.CreateTasksForDeploymentPlan(ctx, request)
	g.Expect(err).NotTo(HaveOccurred())
	second, err := db.CreateTasksForDeploymentPlan(ctx, request)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).To(HaveLen(1))
	g.Expect(second[0].ID).To(Equal(first[0].ID))
	g.Expect(second[0].QueueOrder).To(Equal(first[0].QueueOrder))
	listed, err := db.GetTasksByOrganizationID(ctx, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(HaveLen(1))
}

func TestTaskQueueRepositoryRejectsBlockedPlan(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createBlockedDeploymentPlanForTaskQueue(t, ctx)

	_, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestTaskQueueRepositoryPreservesOrganizationIsolation(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	otherDeps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-b")

	_, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   otherDeps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())

	_, err = db.GetTask(ctx, uuid.New(), deps.orgID)
	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestTaskQueueRepositoryTransitionsTaskStates(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	running, err := db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         tasks[0].ID,
		Status:         types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(running.Status).To(Equal(types.TaskStatusRunning))
	g.Expect(running.StartedAt).NotTo(BeNil())

	succeeded, err := db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         tasks[0].ID,
		Status:         types.TaskStatusSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(succeeded.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(succeeded.CompletedAt).NotTo(BeNil())

	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         tasks[0].ID,
		Status:         types.TaskStatusRunning,
	})
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestTaskQueueRepositoryTransitionsStepRunStates(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	stepRun := tasks[0].StepRuns[0]

	running, err := db.TransitionStepRunState(ctx, types.TransitionStepRunStateRequest{
		OrganizationID: deps.orgID,
		StepRunID:      stepRun.ID,
		Status:         types.StepRunStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(running.Status).To(Equal(types.StepRunStatusRunning))
	g.Expect(running.StartedAt).NotTo(BeNil())

	succeeded, err := db.TransitionStepRunState(ctx, types.TransitionStepRunStateRequest{
		OrganizationID: deps.orgID,
		StepRunID:      stepRun.ID,
		Status:         types.StepRunStatusSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(succeeded.Status).To(Equal(types.StepRunStatusSucceeded))
	g.Expect(succeeded.CompletedAt).NotTo(BeNil())

	_, err = db.TransitionStepRunState(ctx, types.TransitionStepRunStateRequest{
		OrganizationID: deps.orgID,
		StepRunID:      stepRun.ID,
		Status:         types.StepRunStatusPending,
	})
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestTaskQueueMigrationDefinesTaskTables(t *testing.T) {
	g := NewWithT(t)
	sql, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "121_task_queue.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := string(sql)
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE Task"))
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE StepRun"))
	g.Expect(upSQL).To(ContainSubstring("Task_queue_order_seq"))
	g.Expect(upSQL).To(ContainSubstring("FOREIGN KEY (deployment_plan_id, organization_id)"))
	g.Expect(upSQL).To(ContainSubstring("FOREIGN KEY (task_id, deployment_plan_id, organization_id)"))
	g.Expect(upSQL).To(ContainSubstring("status IN ('QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED')"))
	g.Expect(upSQL).To(ContainSubstring("status IN ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'SKIPPED')"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "121_task_queue.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	downSQL := string(down)
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS StepRun"))
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS Task"))
	g.Expect(downSQL).To(ContainSubstring("DROP SEQUENCE IF EXISTS Task_queue_order_seq"))
}

type taskQueuePlanDeps struct {
	orgID   uuid.UUID
	actorID uuid.UUID
	plan    *types.DeploymentPlan
}

func taskQueueDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return deploymentPlanDBTestContext(t)
}

func createReadyDeploymentPlanForTaskQueue(
	t *testing.T,
	ctx context.Context,
	targetNames ...string,
) taskQueuePlanDeps {
	t.Helper()
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevision(t, ctx, deps.orgID, deps.applicationID, "Task queue deploy")
	createDeploymentPlanVariableSet(t, ctx, deps.orgID, deps.applicationID)
	targetIDs := make([]uuid.UUID, 0, len(targetNames))
	for _, name := range targetNames {
		targetIDs = append(targetIDs, createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, name))
	}
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	bundle.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Valid).To(BeTrue())
	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: published.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       targetIDs,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusReady))
	return taskQueuePlanDeps{orgID: deps.orgID, actorID: actorID, plan: plan}
}

func createBlockedDeploymentPlanForTaskQueue(t *testing.T, ctx context.Context) taskQueuePlanDeps {
	t.Helper()
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
	actorID := createReleaseBundleTestUser(t, ctx, deps.orgID)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	plan, err := db.CreateDeploymentPlan(ctx, types.CreateDeploymentPlanRequest{
		OrganizationID:  deps.orgID,
		ReleaseBundleID: bundle.ID,
		EnvironmentID:   deps.devEnvironmentID,
		TargetIDs:       []uuid.UUID{targetID},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusBlocked))
	return taskQueuePlanDeps{orgID: deps.orgID, actorID: actorID, plan: plan}
}
