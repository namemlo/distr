package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestTaskCreationAuditIsTransactionalAndReplaySafe(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "audit-task-create")

	var events []types.ControlPlaneAuditEventInput
	auditCtx := db.WithExecutionV2AuditHook(ctx, db.ControlPlaneAuditAppendHookFunc(
		func(_ context.Context, event types.ControlPlaneAuditEventInput) error {
			events = append(events, event)
			return nil
		},
	))
	request := types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	}
	first, err := db.CreateTasksForDeploymentPlan(auditCtx, request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(first).To(HaveLen(1))
	g.Expect(events).To(HaveLen(1))
	g.Expect(events[0].EventType).To(Equal("execution.task_created"))
	g.Expect(events[0].Outcome).To(Equal("QUEUED"))
	g.Expect(events[0].DeploymentPlanID).NotTo(BeNil())
	g.Expect(*events[0].DeploymentPlanID).To(Equal(deps.plan.ID))
	g.Expect(events[0].TaskID).NotTo(BeNil())
	g.Expect(*events[0].TaskID).To(Equal(first[0].ID))

	replayed, err := db.CreateTasksForDeploymentPlan(auditCtx, request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replayed).To(HaveLen(1))
	g.Expect(replayed[0].ID).To(Equal(first[0].ID))
	g.Expect(events).To(HaveLen(1), "an exact task replay must not duplicate audit")
}

func TestTaskCreationAuditFailureRollsBackMutation(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "audit-task-rollback")
	auditFailure := errors.New("audit sink unavailable")
	auditCtx := db.WithExecutionV2AuditHook(ctx, db.ControlPlaneAuditAppendHookFunc(
		func(context.Context, types.ControlPlaneAuditEventInput) error {
			return auditFailure
		},
	))

	_, err := db.CreateTasksForDeploymentPlan(auditCtx, types.CreateTasksForDeploymentPlanRequest{
		OrganizationID:     deps.orgID,
		DeploymentPlanID:   deps.plan.ID,
		ActorUserAccountID: deps.actorID,
	})
	g.Expect(err).To(MatchError(auditFailure))
	tasks, err := db.GetTasksByOrganizationID(ctx, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(BeEmpty())
	plan, err := db.GetDeploymentPlan(ctx, deps.plan.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusReady))
}
