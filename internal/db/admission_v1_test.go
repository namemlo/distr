package db_test

import (
	"testing"

	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/types"
	"github.com/jackc/pgx/v5"
	. "github.com/onsi/gomega"
)

func TestV1TaskCreationFlagsOffRemainsUngatedAndEventCompatible(t *testing.T) {
	ctx := taskQueueDBTestContext(t)
	g := NewWithT(t)
	deps := createReadyDeploymentPlanForTaskQueue(t, ctx, "v1-flags-off")

	var policyCount, approvalCount, calendarCount, admissionCount int
	err := internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM DeploymentPolicy
		   WHERE organization_id = @organizationID),
		  (SELECT count(*) FROM ApprovalRequest
		   WHERE organization_id = @organizationID),
		  (SELECT count(*) FROM MaintenanceCalendar
		   WHERE organization_id = @organizationID),
		  (SELECT count(*) FROM AdmissionEvaluation
		   WHERE organization_id = @organizationID)
	`, pgx.NamedArgs{"organizationID": deps.orgID}).Scan(
		&policyCount,
		&approvalCount,
		&calendarCount,
		&admissionCount,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect([]int{policyCount, approvalCount, calendarCount, admissionCount}).
		To(Equal([]int{0, 0, 0, 0}))

	tasks, err := db.CreateTasksForDeploymentPlan(
		ctx,
		types.CreateTasksForDeploymentPlanRequest{
			OrganizationID:     deps.orgID,
			DeploymentPlanID:   deps.plan.ID,
			ActorUserAccountID: deps.actorID,
		},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(HaveLen(1))
	g.Expect(tasks[0].Status).To(Equal(types.TaskStatusQueued))
	g.Expect(tasks[0].StepRuns).To(HaveLen(1))
	g.Expect(tasks[0].StepRuns[0].Status).To(Equal(types.StepRunStatusPending))
	plan, err := db.GetDeploymentPlan(ctx, deps.plan.ID, deps.orgID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(plan.Status).To(Equal(types.DeploymentPlanStatusExecuted))

	var taskEventCount, admissionRows int
	err = internalctx.GetDb(ctx).QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM StepEvent
		   WHERE organization_id = @organizationID),
		  (SELECT count(*) FROM AdmissionEvaluation
		   WHERE organization_id = @organizationID)
	`, pgx.NamedArgs{"organizationID": deps.orgID}).Scan(
		&taskEventCount,
		&admissionRows,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(taskEventCount).To(Equal(0))
	g.Expect(admissionRows).To(Equal(0))
}
