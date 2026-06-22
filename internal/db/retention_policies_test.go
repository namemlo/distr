package db_test

import (
	"context"
	"errors"
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

func TestRetentionPolicyRepositoryPreviewsReleaseAndTaskCandidates(t *testing.T) {
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
	ageTaskForRetention(t, ctx, deps.orgID, firstTasks[0].ID, time.Now().AddDate(0, 0, -30))

	currentPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Retention current release",
		targetID,
	)
	currentTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   currentPlan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(markTaskSucceeded(ctx, deps.orgID, currentTasks[0].ID)).To(Succeed())
	ageTaskForRetention(t, ctx, deps.orgID, currentTasks[0].ID, time.Now().AddDate(0, 0, -1))

	failedPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Retention failed task",
		targetID,
	)
	failedTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   failedPlan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(markTaskFailed(ctx, deps.orgID, failedTasks[0].ID)).To(Succeed())
	ageTaskForRetention(t, ctx, deps.orgID, failedTasks[0].ID, time.Now().AddDate(0, 0, -20))

	policy, err := db.CreateRetentionPolicy(ctx, types.CreateRetentionPolicyRequest{
		OrganizationID:                    deps.orgID,
		Name:                              "Default retention",
		Description:                       "Keep current and recent release history.",
		KeepLastSuccessfulReleases:        1,
		FailedTaskRetentionDays:           7,
		ProductionFailedTaskRetentionDays: 30,
		StepLogRetentionDays:              14,
		ProtectCurrentlyDeployedReleases:  true,
		ProtectRetentionProtectedReleases: true,
		MinimumAuditRetentionDays:         365,
	})
	g.Expect(err).NotTo(HaveOccurred())

	preview, err := db.PreviewRetentionCleanup(ctx, types.RetentionCleanupPreviewRequest{
		OrganizationID: deps.orgID,
		PolicyID:       policy.ID,
		Now:            time.Now(),
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.ReleaseCandidates).To(ContainElement(WithTransform(
		func(candidate types.RetentionReleaseCandidate) uuid.UUID { return candidate.ReleaseBundleID },
		Equal(deps.plan.ReleaseBundleID),
	)))
	g.Expect(preview.ReleaseCandidates).NotTo(ContainElement(WithTransform(
		func(candidate types.RetentionReleaseCandidate) uuid.UUID { return candidate.ReleaseBundleID },
		Equal(currentPlan.ReleaseBundleID),
	)))
	g.Expect(preview.FailedTaskCandidates).To(ContainElement(WithTransform(
		func(candidate types.RetentionTaskCandidate) uuid.UUID { return candidate.TaskID },
		Equal(failedTasks[0].ID),
	)))
	g.Expect(preview.SafetyBlocks).To(ContainElement(WithTransform(
		func(block types.RetentionSafetyBlock) types.RetentionSafetyReason { return block.Reason },
		Equal(types.RetentionSafetyCurrentlyDeployed),
	)))
}

func TestRetentionPolicyRepositoryCreatesDryRunCleanupJobAndRejectsApply(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	policy, err := db.CreateRetentionPolicy(ctx, types.CreateRetentionPolicyRequest{
		OrganizationID:                    deps.orgID,
		Name:                              "Dry run policy",
		KeepLastSuccessfulReleases:        2,
		FailedTaskRetentionDays:           7,
		StepLogRetentionDays:              14,
		ProtectCurrentlyDeployedReleases:  true,
		ProtectRetentionProtectedReleases: true,
		MinimumAuditRetentionDays:         365,
	})
	g.Expect(err).NotTo(HaveOccurred())

	job, err := db.CreateRetentionCleanupJob(ctx, types.CreateRetentionCleanupJobRequest{
		OrganizationID: deps.orgID,
		PolicyID:       policy.ID,
		ActorUserID:    deps.actorID,
		DryRun:         true,
		Now:            time.Now(),
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(job.Status).To(Equal(types.RetentionCleanupJobStatusPreviewed))
	g.Expect(job.DryRun).To(BeTrue())
	g.Expect(job.Plan.Policy.ID).To(Equal(policy.ID))

	_, err = db.CreateRetentionCleanupJob(ctx, types.CreateRetentionCleanupJobRequest{
		OrganizationID: deps.orgID,
		PolicyID:       policy.ID,
		ActorUserID:    deps.actorID,
		DryRun:         false,
		Now:            time.Now(),
	})
	g.Expect(errors.Is(err, apierrors.ErrBadRequest)).To(BeTrue())
}

func TestRetentionPolicyRepositorySafetyBlocksProtectedRelease(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	tasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(markTaskSucceeded(ctx, deps.orgID, tasks[0].ID)).To(Succeed())
	ageTaskForRetention(t, ctx, deps.orgID, tasks[0].ID, time.Now().AddDate(0, 0, -30))
	markReleaseBundleRetentionProtected(t, ctx, deps.orgID, deps.plan.ReleaseBundleID)

	policy, err := db.CreateRetentionPolicy(ctx, types.CreateRetentionPolicyRequest{
		OrganizationID:                    deps.orgID,
		Name:                              "Protected policy",
		FailedTaskRetentionDays:           7,
		StepLogRetentionDays:              14,
		ProtectRetentionProtectedReleases: true,
		MinimumAuditRetentionDays:         365,
	})
	g.Expect(err).NotTo(HaveOccurred())

	preview, err := db.PreviewRetentionCleanup(ctx, types.RetentionCleanupPreviewRequest{
		OrganizationID: deps.orgID,
		PolicyID:       policy.ID,
		Now:            time.Now(),
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.ReleaseCandidates).NotTo(ContainElement(WithTransform(
		func(candidate types.RetentionReleaseCandidate) uuid.UUID { return candidate.ReleaseBundleID },
		Equal(deps.plan.ReleaseBundleID),
	)))
	g.Expect(preview.SafetyBlocks).To(ContainElement(WithTransform(
		func(block types.RetentionSafetyBlock) types.RetentionSafetyReason { return block.Reason },
		Equal(types.RetentionSafetyProtectedRelease),
	)))
}

func TestRetentionPolicyRepositoryUsesCompletionOrderingForCurrentlyDeployedSafety(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "cluster-a")
	targetID := deps.plan.Targets[0].DeploymentTargetID
	oldTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(markTaskSucceeded(ctx, deps.orgID, oldTasks[0].ID)).To(Succeed())
	ageTaskForRetention(t, ctx, deps.orgID, oldTasks[0].ID, time.Now().AddDate(0, 0, -30))

	currentPlan := createReadyDeploymentPlanForTaskQueueWithTargets(
		t,
		ctx,
		deps,
		"Retention current by completion",
		targetID,
	)
	currentTasks, err := db.CreateTasksForDeploymentPlan(ctx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   currentPlan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(markTaskSucceeded(ctx, deps.orgID, currentTasks[0].ID)).To(Succeed())
	ageTaskForRetention(t, ctx, deps.orgID, currentTasks[0].ID, time.Now().AddDate(0, 0, -1))
	setTaskCreatedAtForRetention(t, ctx, deps.orgID, oldTasks[0].ID, time.Now().AddDate(0, 0, 1))

	policy, err := db.CreateRetentionPolicy(ctx, types.CreateRetentionPolicyRequest{
		OrganizationID:                   deps.orgID,
		Name:                             "Completion policy",
		FailedTaskRetentionDays:          7,
		StepLogRetentionDays:             14,
		ProtectCurrentlyDeployedReleases: true,
		MinimumAuditRetentionDays:        365,
	})
	g.Expect(err).NotTo(HaveOccurred())

	preview, err := db.PreviewRetentionCleanup(ctx, types.RetentionCleanupPreviewRequest{
		OrganizationID: deps.orgID,
		PolicyID:       policy.ID,
		Now:            time.Now(),
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(preview.ReleaseCandidates).To(ContainElement(WithTransform(
		func(candidate types.RetentionReleaseCandidate) uuid.UUID { return candidate.ReleaseBundleID },
		Equal(deps.plan.ReleaseBundleID),
	)))
	g.Expect(preview.ReleaseCandidates).NotTo(ContainElement(WithTransform(
		func(candidate types.RetentionReleaseCandidate) uuid.UUID { return candidate.ReleaseBundleID },
		Equal(currentPlan.ReleaseBundleID),
	)))
	g.Expect(preview.SafetyBlocks).To(ContainElement(WithTransform(
		func(block types.RetentionSafetyBlock) uuid.UUID { return block.ResourceID },
		Equal(currentPlan.ReleaseBundleID),
	)))
}

func markTaskFailed(ctx context.Context, orgID, taskID uuid.UUID) error {
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
		Status:         types.TaskStatusFailed,
	})
	return err
}

func markReleaseBundleRetentionProtected(t *testing.T, ctx context.Context, orgID, releaseBundleID uuid.UUID) {
	t.Helper()
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`UPDATE ReleaseBundle
		SET retention_protected = true
		WHERE id = @releaseBundleId
			AND organization_id = @organizationId`,
		pgx.NamedArgs{
			"releaseBundleId": releaseBundleID,
			"organizationId":  orgID,
		},
	)
	if err != nil {
		t.Fatalf("mark release bundle retention protected: %v", err)
	}
}

func ageTaskForRetention(t *testing.T, ctx context.Context, orgID, taskID uuid.UUID, completedAt time.Time) {
	t.Helper()
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`UPDATE Task
		SET completed_at = @completedAt,
			updated_at = @completedAt
		WHERE id = @taskId
			AND organization_id = @organizationId`,
		pgx.NamedArgs{
			"completedAt":    completedAt,
			"taskId":         taskID,
			"organizationId": orgID,
		},
	)
	if err != nil {
		t.Fatalf("age task for retention: %v", err)
	}
}

func setTaskCreatedAtForRetention(t *testing.T, ctx context.Context, orgID, taskID uuid.UUID, createdAt time.Time) {
	t.Helper()
	_, err := internalctx.GetDb(ctx).Exec(
		ctx,
		`UPDATE Task
		SET created_at = @createdAt
		WHERE id = @taskId
			AND organization_id = @organizationId`,
		pgx.NamedArgs{
			"createdAt":      createdAt,
			"taskId":         taskID,
			"organizationId": orgID,
		},
	)
	if err != nil {
		t.Fatalf("set task created at for retention: %v", err)
	}
}
