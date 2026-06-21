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

func TestTaskLeaseRepositoryClaimsQueuedTaskForAgent(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).NotTo(BeNil())
	g.Expect(lease.TaskID).To(Equal(tasks[0].ID))
	g.Expect(lease.AgentID).To(Equal(tasks[0].DeploymentTargetID))
	g.Expect(lease.LeaseToken).NotTo(BeEmpty())
	g.Expect(lease.Attempt).To(Equal(1))
	g.Expect(lease.ExpiresAt).To(BeTemporally(">", lease.HeartbeatAt))
	g.Expect(lease.Task.Status).To(Equal(types.TaskStatusRunning))
	g.Expect(lease.Task.Locks).To(ContainElement(WithTransform(
		func(lock types.TaskResourceLock) bool { return lock.AcquiredAt != nil && lock.ReleasedAt == nil },
		BeTrue(),
	)))
	g.Expect(lease.Steps).To(HaveLen(1))
	g.Expect(lease.Steps[0].StepRunID).To(Equal(tasks[0].StepRuns[0].ID))
	g.Expect(lease.Steps[0].StepKey).To(Equal("deploy"))
	g.Expect(lease.Steps[0].ActionType).To(Equal("distr.http.check"))
	g.Expect(lease.Steps[0].ActionVersion).To(Equal(types.AgentActionVersionV1))
	g.Expect(lease.Steps[0].InputBindings).To(HaveKey("url"))
	g.Expect(lease.Steps[0].IdempotencyKey).To(HavePrefix("sha256:"))

	fetched, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Status).To(Equal(types.TaskStatusRunning))
}

func TestTaskLeaseRepositoryReturnsNilWhenNoQueuedTaskForAgent(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	orgID := createReleaseBundleTestOrganization(t, ctx)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, orgID, "cluster-a")

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: orgID,
		AgentID:        targetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).To(BeNil())
}

func TestTaskLeaseRepositorySkipsHubOnlyTask(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).To(BeNil())
}

func TestTaskLeaseRepositoryDoesNotClaimTaskForAnotherOrganization(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	otherOrgID := createReleaseBundleTestOrganization(t, ctx)

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: otherOrgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).To(BeNil())
}

func TestTaskLeaseRepositoryHeartbeatsActiveLease(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	time.Sleep(10 * time.Millisecond)

	heartbeat, err := db.HeartbeatAgentTaskLease(ctx, types.HeartbeatAgentTaskLeaseRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
		TaskID:         tasks[0].ID,
		LeaseToken:     lease.LeaseToken,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(heartbeat.ID).To(Equal(lease.ID))
	g.Expect(heartbeat.HeartbeatAt).To(BeTemporally(">", lease.HeartbeatAt))
	g.Expect(heartbeat.ExpiresAt).To(BeTemporally(">", lease.ExpiresAt))
}

func TestTaskLeaseRepositoryRejectsExpiredHeartbeat(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	expireTaskLeaseForTest(t, ctx, lease.ID)

	_, err = db.HeartbeatAgentTaskLease(ctx, types.HeartbeatAgentTaskLeaseRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
		TaskID:         tasks[0].ID,
		LeaseToken:     lease.LeaseToken,
	})

	g.Expect(errors.Is(err, apierrors.ErrConflict)).To(BeTrue())
}

func TestTaskLeaseRepositoryReclaimsExpiredLeaseWithNewAttempt(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	first, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	expireTaskLeaseForTest(t, ctx, first.ID)

	second, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).NotTo(BeNil())
	g.Expect(second.TaskID).To(Equal(first.TaskID))
	g.Expect(second.ID).NotTo(Equal(first.ID))
	g.Expect(second.LeaseToken).NotTo(Equal(first.LeaseToken))
	g.Expect(second.Attempt).To(Equal(2))
	g.Expect(countActiveTaskLeasesForTest(t, ctx, tasks[0].ID)).To(Equal(1))
	g.Expect(second.Task.Status).To(Equal(types.TaskStatusRunning))
}

func TestTaskLeaseRepositoryReclaimSkipsSucceededStepRuns(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{
		taskLeaseHTTPCheckStep("prepare", "Prepare", 10),
		taskLeaseHTTPCheckStep("deploy", "Deploy", 20),
	})
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	first, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first.Steps).To(HaveLen(2))
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		first.Steps[0].StepRunID,
		first.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		first.Steps[0].StepRunID,
		first.LeaseToken,
		2,
		types.StepRunEventTypeSucceeded,
	)
	expireTaskLeaseForTest(t, ctx, first.ID)

	second, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).NotTo(BeNil())
	g.Expect(second.Attempt).To(Equal(2))
	g.Expect(second.Steps).To(HaveLen(1))
	g.Expect(second.Steps[0].StepKey).To(Equal("deploy"))
	g.Expect(second.Steps[0].StepRunID).To(Equal(first.Steps[1].StepRunID))
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		second.Steps[0].StepRunID,
		second.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
}

func TestTaskLeaseRepositoryReclaimResetsInterruptedRunningStepRunForRetry(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{
		taskLeaseComposeDeployStep("compose", "Compose deploy", 10),
	})
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	first, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first.Steps).To(HaveLen(1))
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		first.Steps[0].StepRunID,
		first.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
	expireTaskLeaseForTest(t, ctx, first.ID)

	second, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).NotTo(BeNil())
	g.Expect(second.Attempt).To(Equal(2))
	g.Expect(second.Steps).To(HaveLen(1))
	g.Expect(second.Steps[0].StepRunID).To(Equal(first.Steps[0].StepRunID))

	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		second.Steps[0].StepRunID,
		second.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
}

func TestTaskLeaseRepositoryReclaimDoesNotResetRunningHubStepRun(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	hubStep := taskLeaseHTTPCheckStep("hub-prepare", "Hub prepare", 10)
	hubStep.ExecutionLocation = "hub"
	deps := createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{
		hubStep,
		taskLeaseComposeDeployStep("compose", "Compose deploy", 20),
	})
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	hubRun := taskLeaseStepRunByKeyForTest(t, tasks[0], "hub-prepare")
	composeRun := taskLeaseStepRunByKeyForTest(t, tasks[0], "compose")
	first, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first.Steps).To(HaveLen(1))
	g.Expect(first.Steps[0].StepRunID).To(Equal(composeRun.ID))
	_, err = db.TransitionStepRunState(ctx, types.TransitionStepRunStateRequest{
		OrganizationID: deps.orgID,
		StepRunID:      hubRun.ID,
		Status:         types.StepRunStatusRunning,
	})
	g.Expect(err).NotTo(HaveOccurred())
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		composeRun.ID,
		first.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
	expireTaskLeaseForTest(t, ctx, first.ID)

	second, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).NotTo(BeNil())
	fetched, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(taskLeaseStepRunByKeyForTest(t, *fetched, "hub-prepare").Status).To(Equal(types.StepRunStatusRunning))
	g.Expect(taskLeaseStepRunByKeyForTest(t, *fetched, "compose").Status).To(Equal(types.StepRunStatusPending))
	recordTaskLeaseStepEventForTest(
		t,
		ctx,
		deps.orgID,
		tasks[0].DeploymentTargetID,
		second.Steps[0].StepRunID,
		second.LeaseToken,
		1,
		types.StepRunEventTypeStarted,
	)
}

func TestTaskLeaseRepositoryDoesNotClaimWhenExclusiveLockIsHeld(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	secondPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Task lease queued behind running",
		deps.plan.Targets[0].DeploymentTargetID,
	)
	firstTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   secondPlan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	firstLease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        firstTasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	secondLease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        firstLease.AgentID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secondLease).To(BeNil())
}

func TestTaskLeaseRepositoryDoesNotClaimWhenReleaseBundleIsBlocked(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	blocked, err := db.BlockReleaseBundle(ctx, deps.plan.ReleaseBundleID, deps.orgID, deps.actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked.Status).To(Equal(types.ReleaseBundleStatusBlocked))

	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(lease).To(BeNil())
	fetched, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Status).To(Equal(types.TaskStatusQueued))
}

func TestTaskLeaseRepositoryDoesNotReclaimExpiredLeaseWhenReleaseBundleIsBlocked(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	first, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	expireTaskLeaseForTest(t, ctx, first.ID)
	blocked, err := db.BlockReleaseBundle(ctx, deps.plan.ReleaseBundleID, deps.orgID, deps.actorID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(blocked.Status).To(Equal(types.ReleaseBundleStatusBlocked))

	second, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(second).To(BeNil())
	g.Expect(countActiveTaskLeasesForTest(t, ctx, tasks[0].ID)).To(Equal(1))
}

func TestTaskLeaseRepositoryWaitsForConcurrentReleaseBundleBlockBeforeClaim(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	tx := lockAndBlockReleaseBundleForTest(t, ctx, deps.plan.ReleaseBundleID)
	resultCh := make(chan taskLeaseAsyncResult, 1)

	go func() {
		lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
			OrganizationID: deps.orgID,
			AgentID:        tasks[0].DeploymentTargetID,
		})
		resultCh <- taskLeaseAsyncResult{lease: lease, err: err}
	}()

	assertTaskLeaseOperationIsWaiting(t, resultCh)
	g.Expect(tx.Commit(ctx)).To(Succeed())
	result := awaitTaskLeaseOperation(t, resultCh)
	g.Expect(result.err).NotTo(HaveOccurred())
	g.Expect(result.lease).To(BeNil())
	fetched, err := db.GetTask(ctx, tasks[0].ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Status).To(Equal(types.TaskStatusQueued))
}

func TestTaskLeaseRepositoryTerminalTaskReleasesLeaseAndRejectsHeartbeat(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())

	terminal, err := db.TransitionTaskState(ctx, types.TransitionTaskStateRequest{
		OrganizationID: deps.orgID,
		TaskID:         tasks[0].ID,
		Status:         types.TaskStatusSucceeded,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(terminal.Status).To(Equal(types.TaskStatusSucceeded))
	g.Expect(countActiveTaskLeasesForTest(t, ctx, tasks[0].ID)).To(Equal(0))

	_, err = db.HeartbeatAgentTaskLease(ctx, types.HeartbeatAgentTaskLeaseRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
		TaskID:         tasks[0].ID,
		LeaseToken:     lease.LeaseToken,
	})

	g.Expect(errors.Is(err, apierrors.ErrNotFound)).To(BeTrue())
}

func TestTaskLeaseRepositoryHeartbeatWaitsForConcurrentTerminalTask(t *testing.T) {
	ctx := taskLeaseDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskLease(t, ctx)
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	lease, err := db.LeaseAgentTask(ctx, types.LeaseAgentTaskRequest{
		OrganizationID: deps.orgID,
		AgentID:        tasks[0].DeploymentTargetID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	tx := lockAndCompleteTaskForTest(t, ctx, tasks[0].ID, deps.orgID)
	resultCh := make(chan taskLeaseAsyncResult, 1)

	go func() {
		heartbeat, err := db.HeartbeatAgentTaskLease(ctx, types.HeartbeatAgentTaskLeaseRequest{
			OrganizationID: deps.orgID,
			AgentID:        tasks[0].DeploymentTargetID,
			TaskID:         tasks[0].ID,
			LeaseToken:     lease.LeaseToken,
		})
		resultCh <- taskLeaseAsyncResult{lease: heartbeat, err: err}
	}()

	assertTaskLeaseOperationIsWaiting(t, resultCh)
	g.Expect(tx.Commit(ctx)).To(Succeed())
	result := awaitTaskLeaseOperation(t, resultCh)
	g.Expect(errors.Is(result.err, apierrors.ErrNotFound)).To(BeTrue())
	g.Expect(result.lease).To(BeNil())
	g.Expect(countActiveTaskLeasesForTest(t, ctx, tasks[0].ID)).To(Equal(0))
}

func TestTaskLeaseMigrationDefinesLeaseTables(t *testing.T) {
	g := NewWithT(t)
	sql, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "124_task_leases.up.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	upSQL := string(sql)
	g.Expect(upSQL).To(ContainSubstring("CREATE TABLE TaskLease"))
	g.Expect(upSQL).To(ContainSubstring("lease_token_hash"))
	g.Expect(upSQL).To(ContainSubstring("FOREIGN KEY (task_id, organization_id)"))
	g.Expect(upSQL).To(ContainSubstring("FOREIGN KEY (agent_id, organization_id)"))
	g.Expect(upSQL).To(ContainSubstring("UNIQUE INDEX TaskLease_active_task"))

	down, err := os.ReadFile(filepath.Join("..", "migrations", "sql", "124_task_leases.down.sql"))
	g.Expect(err).NotTo(HaveOccurred())
	downSQL := string(down)
	g.Expect(downSQL).To(ContainSubstring("DROP TABLE IF EXISTS TaskLease"))
}

func taskLeaseDBTestContext(t *testing.T) context.Context {
	t.Helper()
	return taskQueueDBTestContext(t)
}

func createReadyDeploymentPlanForTaskLease(t *testing.T, ctx context.Context) taskQueuePlanDeps {
	t.Helper()
	return createReadyDeploymentPlanForTaskLeaseWithSteps(t, ctx, []types.DeploymentProcessStep{
		taskLeaseHTTPCheckStep("deploy", "Deploy", 10),
	})
}

func createReadyDeploymentPlanForTaskLeaseWithSteps(
	t *testing.T,
	ctx context.Context,
	steps []types.DeploymentProcessStep,
) taskQueuePlanDeps {
	t.Helper()
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	process := types.DeploymentProcess{
		OrganizationID: deps.orgID,
		ApplicationID:  deps.applicationID,
		Name:           "Task lease deploy " + uuid.NewString(),
	}
	g.Expect(db.CreateDeploymentProcess(ctx, &process)).To(Succeed())
	revision := types.DeploymentProcessRevision{
		OrganizationID:      deps.orgID,
		DeploymentProcessID: process.ID,
		Description:         "Initial revision",
		Steps:               steps,
	}
	g.Expect(db.CreateDeploymentProcessRevision(ctx, &revision)).To(Succeed())
	createDeploymentPlanVariableSet(t, ctx, deps.orgID, deps.applicationID)
	targetID := createReleaseBundleDockerTargetForOrganization(t, ctx, deps.orgID, "cluster-a")
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
		TargetIDs:       []uuid.UUID{targetID},
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

func taskLeaseHTTPCheckStep(key, name string, sortOrder int) types.DeploymentProcessStep {
	return types.DeploymentProcessStep{
		Key:                  key,
		Name:                 name,
		ActionType:           "distr.http.check",
		ExecutionLocation:    "target",
		InputBindings:        map[string]any{"url": "https://example.com/health"},
		FailureMode:          "fail",
		TimeoutSeconds:       120,
		RetryMaxAttempts:     3,
		RetryIntervalSeconds: 10,
		RequiredPermissions:  []string{"deploy:write"},
		SortOrder:            sortOrder,
	}
}

func taskLeaseComposeDeployStep(key, name string, sortOrder int) types.DeploymentProcessStep {
	return types.DeploymentProcessStep{
		Key:               key,
		Name:              name,
		ActionType:        "distr.compose.deploy",
		ExecutionLocation: "target",
		InputBindings: map[string]any{
			"applicationVersion": map[string]any{
				"composeFile": "services:\n  web:\n    image: nginx:alpine\n",
			},
			"projectName": "task-lease-retry",
		},
		FailureMode:          "fail",
		TimeoutSeconds:       120,
		RetryMaxAttempts:     3,
		RetryIntervalSeconds: 10,
		RequiredPermissions:  []string{"deploy:write"},
		SortOrder:            sortOrder,
	}
}

func recordTaskLeaseStepEventForTest(
	t *testing.T,
	ctx context.Context,
	orgID uuid.UUID,
	agentID uuid.UUID,
	stepRunID uuid.UUID,
	leaseToken string,
	sequence int64,
	eventType types.StepRunEventType,
) {
	t.Helper()
	g := NewWithT(t)
	_, err := db.RecordAgentStepRunEvent(ctx, types.RecordAgentStepRunEventRequest{
		OrganizationID: orgID,
		AgentID:        agentID,
		StepRunID:      stepRunID,
		LeaseToken:     leaseToken,
		Sequence:       sequence,
		Type:           eventType,
		Message:        string(eventType),
	})
	g.Expect(err).NotTo(HaveOccurred())
}

func taskLeaseStepRunByKeyForTest(t *testing.T, task types.Task, key string) types.StepRun {
	t.Helper()
	for _, stepRun := range task.StepRuns {
		if stepRun.StepKey == key {
			return stepRun
		}
	}
	t.Fatalf("step run %q not found", key)
	return types.StepRun{}
}

func expireTaskLeaseForTest(t *testing.T, ctx context.Context, leaseID uuid.UUID) {
	t.Helper()
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`UPDATE TaskLease
		SET expires_at = now() - interval '1 minute', heartbeat_at = now() - interval '2 minutes'
		WHERE id = @leaseId`,
		pgx.NamedArgs{"leaseId": leaseID},
	)
	if err != nil {
		t.Fatalf("expire task lease: %v", err)
	}
}

func countActiveTaskLeasesForTest(t *testing.T, ctx context.Context, taskID uuid.UUID) int {
	t.Helper()
	var count int
	err := internalctx.GetDb(ctx).QueryRow(
		ctx,
		`SELECT count(*) FROM TaskLease WHERE task_id = @taskId AND released_at IS NULL`,
		pgx.NamedArgs{"taskId": taskID},
	).Scan(&count)
	if err != nil {
		t.Fatalf("count active task leases: %v", err)
	}
	return count
}

type taskLeaseAsyncResult struct {
	lease *types.TaskLease
	err   error
}

func assertTaskLeaseOperationIsWaiting(t *testing.T, resultCh <-chan taskLeaseAsyncResult) {
	t.Helper()
	select {
	case result := <-resultCh:
		t.Fatalf("task lease operation completed before lock was released: lease=%v err=%v", result.lease, result.err)
	case <-time.After(200 * time.Millisecond):
	}
}

func awaitTaskLeaseOperation(t *testing.T, resultCh <-chan taskLeaseAsyncResult) taskLeaseAsyncResult {
	t.Helper()
	select {
	case result := <-resultCh:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("task lease operation did not finish after lock was released")
		return taskLeaseAsyncResult{}
	}
}

func lockAndCompleteTaskForTest(t *testing.T, ctx context.Context, taskID, orgID uuid.UUID) pgx.Tx {
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
		`SELECT id FROM Task
		WHERE id = @taskId AND organization_id = @organizationId
		FOR UPDATE`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	); err != nil {
		t.Fatalf("lock task row: %v", err)
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE Task
		SET status = @status, updated_at = now(), completed_at = now()
		WHERE id = @taskId AND organization_id = @organizationId`,
		pgx.NamedArgs{
			"taskId":         taskID,
			"organizationId": orgID,
			"status":         types.TaskStatusSucceeded,
		},
	); err != nil {
		t.Fatalf("complete task in transaction: %v", err)
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE TaskLease
		SET released_at = COALESCE(released_at, now()), updated_at = now()
		WHERE task_id = @taskId
			AND organization_id = @organizationId
			AND released_at IS NULL`,
		pgx.NamedArgs{"taskId": taskID, "organizationId": orgID},
	); err != nil {
		t.Fatalf("release task lease in transaction: %v", err)
	}
	return tx
}
