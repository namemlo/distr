package db_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/apierrors"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

func TestTaskQueueRepositoryCreatesDefaultDeploymentTargetLocks(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")

	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(HaveLen(1))
	g.Expect(tasks[0].Locks).To(HaveLen(1))
	g.Expect(tasks[0].Locks[0].ResourceType).To(Equal(types.TaskLockResourceDeploymentTarget))
	g.Expect(tasks[0].Locks[0].ResourceKey).To(Equal(deps.plan.Targets[0].DeploymentTargetID.String()))
	g.Expect(tasks[0].Locks[0].ConcurrencyPolicy).To(Equal(types.TaskConcurrencyPolicyQueue))
}

func TestTaskQueueRepositoryQueuePolicyPreventsConcurrentRunningTasksForSameTarget(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	secondPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Task queue deploy two",
		deps.plan.Targets[0].DeploymentTargetID,
	)
	firstTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	secondTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   secondPlan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         firstTasks[0].ID,
		Status:         types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         secondTasks[0].ID,
		Status:         types.TaskStatusRunning,
	})
	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())

	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         firstTasks[0].ID,
		Status:         types.TaskStatusSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())
	running, err := db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         secondTasks[0].ID,
		Status:         types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(running.Status).To(Equal(types.TaskStatusRunning))
}

func TestTaskQueueRepositoryRejectNewPolicyRejectsConflictingQueuedTask(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	secondPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Task queue reject new",
		deps.plan.Targets[0].DeploymentTargetID,
	)
	_, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:      deps.orgID,
		DeploymentPlanID:    secondPlan.ID,
		ActorUserAccountID:  deps.actorID,
		ConcurrencyPolicy:   types.TaskConcurrencyPolicyRejectNew,
		AdditionalResources: nil,
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	listed, err := db.GetTasksByOrganizationID(ctx, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(HaveLen(1))
}

func TestTaskQueueRepositoryCancelOlderPolicyCancelsQueuedConflicts(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	secondPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Task queue cancel older",
		deps.plan.Targets[0].DeploymentTargetID,
	)
	firstTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	secondTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   secondPlan.ID,
		ActorUserAccountID: deps.actorID,
		ConcurrencyPolicy:  types.TaskConcurrencyPolicyCancelOlder,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondTasks).To(HaveLen(1))
	g.Expect(secondTasks[0].Status).To(Equal(types.TaskStatusQueued))
	first, err := db.GetTask(ctx, firstTasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first.Status).To(Equal(types.TaskStatusCanceled))
	g.Expect(first.CompletedAt).NotTo(BeNil())
	g.Expect(first.Locks).To(HaveLen(1))
	g.Expect(first.Locks[0].ReleasedAt).NotTo(BeNil())
}

func TestTaskQueueRepositoryAllowParallelPolicyAllowsConcurrentRunningTasksForSameTarget(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	secondPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Task queue allow parallel",
		deps.plan.Targets[0].DeploymentTargetID,
	)
	request := func(planID uuid.UUID) types.CreateTasksForDeploymentPlanRequest {
		return types.CreateTasksForDeploymentPlanRequest{
			OrganizationID:     deps.orgID,
			DeploymentPlanID:   planID,
			ActorUserAccountID: deps.actorID,
			ConcurrencyPolicy:  types.TaskConcurrencyPolicyAllowParallel,
		}
	}
	firstTasks, err := db.CreateTasksForDeploymentPlan(ctx, request(deps.plan.ID))
	g.Expect(err).NotTo(HaveOccurred())
	secondTasks, err := db.CreateTasksForDeploymentPlan(ctx, request(secondPlan.ID))
	g.Expect(err).NotTo(HaveOccurred())

	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         firstTasks[0].ID,
		Status:         types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         secondTasks[0].ID,
		Status:         types.TaskStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
}

func TestTaskQueueRepositorySerializesConcurrentLockAcquisition(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	secondPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Task queue race",
		deps.plan.Targets[0].DeploymentTargetID,
	)
	firstTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	secondTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   secondPlan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	start := make(chan struct{})
	errCh := make(chan error, 2)
	transition := func(taskID uuid.UUID) {
		<-start
		_, err := db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
			OrganizationID: deps.orgID,
			TaskID:         taskID,
			Status:         types.TaskStatusRunning,
		})
		errCh <- err
	}
	go transition(firstTasks[0].ID)
	go transition(secondTasks[0].ID)
	close(start)

	var successes int
	var conflicts int
	for range 2 {
		err := awaitTaskQueueOperation(t, errCh)
		switch {
		case err == nil:
			successes++
		case errors.Is(err, apierrors.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected transition error: %v", err)
		}
	}
	g.Expect(successes).To(Equal(1))
	g.Expect(conflicts).To(Equal(1))
}

func TestTaskQueueRepositoryAdditionalLocksApplyAcrossTargets(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	secondPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Task queue custom lock",
		createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-b"),
	)
	lock := types.TaskLockResourceRequest{
		ResourceType:      types.TaskLockResourceCustom,
		ResourceKey:       " shared-db ",
		ConcurrencyPolicy: types.TaskConcurrencyPolicyRejectNew,
	}
	firstTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:      deps.orgID,
		DeploymentPlanID:    deps.plan.ID,
		ActorUserAccountID:  deps.actorID,
		AdditionalResources: []types.TaskLockResourceRequest{lock},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(firstTasks[0].Locks).To(ContainElement(WithTransform(
		func(lock types.TaskResourceLock) string { return lock.ResourceKey },
		Equal("shared-db"),
	)))

	_, err = db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:      deps.orgID,
		DeploymentPlanID:    secondPlan.ID,
		ActorUserAccountID:  deps.actorID,
		AdditionalResources: []types.TaskLockResourceRequest{lock},
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
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

func TestTaskQueueRepositoryRejectsReadyPlanWhenReleaseBundleIsBlocked(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	blocked, err := db.BlockReleaseBundle(ctx, deps.plan.ReleaseBundleID, deps.orgID, deps.actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked.Status).To(Equal(types.ReleaseBundleStatusBlocked))

	_, err = db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	listed, err := db.GetTasksByOrganizationID(ctx, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(BeEmpty())
}

func TestTaskQueueRepositoryRejectsCreateAfterConcurrentReleaseBundleBlock(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	tx := lockAndBlockReleaseBundleForTest(t, ctx, deps.plan.ReleaseBundleID)
	errCh := make(chan error, 1)

	go func() {
		_, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
			OrganizationID:     deps.orgID,
			DeploymentPlanID:   deps.plan.ID,
			ActorUserAccountID: deps.actorID,
		})
		errCh <- err
	}()

	assertTaskQueueOperationIsWaiting(t, errCh)
	g.Expect(tx.Commit(ctx)).To(Succeed())
	err := awaitTaskQueueOperation(t, errCh)

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
	listed, err := db.GetTasksByOrganizationID(ctx, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(listed).To(BeEmpty())
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

func TestTaskLockMigrationDefinesLockTables(t *testing.T) {
	g := NewWithT(t)
	sql, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "122_task_locks.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := string(sql)
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE TaskResourceLock"))
	g.Expect(upSQL).To(ContainSubstring("status IN ('QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELED')"))
	g.Expect(upSQL).To(ContainSubstring(
		"resource_type IN ('deployment_target', 'tenant_environment', 'application_environment', 'custom')",
	))
	g.Expect(upSQL).To(ContainSubstring("concurrency_policy IN ('QUEUE', 'CANCEL_OLDER', 'REJECT_NEW', 'ALLOW_PARALLEL')"))
	g.Expect(upSQL).To(ContainSubstring("INSERT INTO TaskResourceLock"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "122_task_locks.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	downSQL := string(down)
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS TaskResourceLock"))
	g.Expect(downSQL).To(ContainSubstring("status IN ('QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED')"))
}

type taskQueuePlanDeps struct {
	orgID            uuid.UUID
	applicationID    uuid.UUID
	channelID        uuid.UUID
	versionID        uuid.UUID
	devEnvironmentID uuid.UUID
	actorID          uuid.UUID
	plan             *types.DeploymentPlan
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
	return taskQueuePlanDeps{
		orgID:            deps.orgID,
		applicationID:    deps.applicationID,
		channelID:        deps.channelID,
		versionID:        deps.versionID,
		devEnvironmentID: deps.devEnvironmentID,
		actorID:          actorID,
		plan:             plan,
	}
}

func createReadyDeploymentPlanForTaskQueueWithTargets(
	t *testing.T,
	ctx context.Context,
	deps taskQueuePlanDeps,
	processName string,
	targetIDs ...uuid.UUID,
) *types.DeploymentPlan {
	t.Helper()
	g := NewWithT(t)
	_, revision := createReleaseBundleProcessRevision(t, ctx, deps.orgID, deps.applicationID, processName)
	bundle := releaseBundleFixture(deps.orgID, deps.applicationID, deps.channelID, deps.versionID)
	bundle.ReleaseNumber = "2026.06." + uuid.NewString()
	bundle.DeploymentProcessRevisionID = &revision.ID
	g.Expect(db.CreateReleaseBundle(ctx, &bundle)).To(Succeed())
	published, result, err := db.PublishReleaseBundle(ctx, bundle.ID, deps.orgID, deps.actorID)
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
	return plan
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

func lockAndBlockReleaseBundleForTest(t *testing.T, ctx context.Context, releaseBundleID uuid.UUID) pgx.Tx {
	t.Helper()
	pool, ok := internalctx.GetDb(ctx).(*pgxpool.Pool)
	if !ok {
		t.Fatalf("test context db is %T, expected *pgxpool.Pool", internalctx.GetDb(ctx))
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback(ctx)
	})
	if _, err := tx.Exec(
		ctx,
		`SELECT id FROM ReleaseBundle WHERE id = @id FOR UPDATE`,
		pgx.NamedArgs{"id": releaseBundleID},
	); err != nil {
		t.Fatalf("lock release bundle row: %v", err)
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE ReleaseBundle
		SET status = @status, updated_at = now()
		WHERE id = @id`,
		pgx.NamedArgs{
			"id":     releaseBundleID,
			"status": types.ReleaseBundleStatusBlocked,
		},
	); err != nil {
		t.Fatalf("block release bundle in transaction: %v", err)
	}
	return tx
}

func assertTaskQueueOperationIsWaiting(t *testing.T, errCh <-chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		t.Fatalf("task queue operation completed before release bundle lock was released: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
}

func awaitTaskQueueOperation(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("task queue operation did not finish after release bundle lock was released")
		return nil
	}
}
