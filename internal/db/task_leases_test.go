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
	g := NewWithT(t)
	deps := createReleaseBundleEligibilityDependencies(t, ctx)
	_, revision := createReleaseBundleProcessRevisionWithExecutionLocation(
		t,
		ctx,
		deps.orgID,
		deps.applicationID,
		"Task lease deploy",
		"target",
	)
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
