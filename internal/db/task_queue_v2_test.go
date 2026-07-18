package db

import (
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	. "github.com/onsi/gomega"
)

func TestValidateDeploymentPlanTaskCreationRejectsTargetPlanV2EvenIfReady(t *testing.T) {
	g := NewWithT(t)
	err := validateDeploymentPlanTaskCreation(types.DeploymentPlan{
		PlanSchema: types.TargetDeploymentPlanSchemaV2,
		Status:     types.DeploymentPlanStatusReady,
	})

	g.Expect(err).To(HaveOccurred())
	g.Expect(err).To(MatchError(ContainSubstring("PR-075")))
	g.Expect(err).To(MatchError(apierrors.ErrConflict))
}

func TestValidateDeploymentPlanTaskCreationAllowsReadyLegacyPlan(t *testing.T) {
	g := NewWithT(t)

	err := validateDeploymentPlanTaskCreation(types.DeploymentPlan{
		PlanSchema: types.LegacyDeploymentPlanSchemaV1,
		Status:     types.DeploymentPlanStatusReady,
	})

	g.Expect(err).NotTo(HaveOccurred())
}

func TestExistingTasksCannotBypassTargetPlanV2Denial(t *testing.T) {
	g := NewWithT(t)
	existing := []types.Task{{}}

	tasks, reused, err := reuseExistingDeploymentPlanTasks(
		types.DeploymentPlan{
			PlanSchema: types.TargetDeploymentPlanSchemaV2,
			Status:     types.DeploymentPlanStatusReady,
		},
		existing,
	)

	g.Expect(err).To(MatchError(ContainSubstring("PR-075")))
	g.Expect(err).To(MatchError(apierrors.ErrConflict))
	g.Expect(reused).To(BeFalse())
	g.Expect(tasks).To(BeNil())
}

func TestExistingLegacyTasksRemainIdempotentAfterExecution(t *testing.T) {
	g := NewWithT(t)
	existing := []types.Task{{}}

	tasks, reused, err := reuseExistingDeploymentPlanTasks(
		types.DeploymentPlan{
			PlanSchema: types.LegacyDeploymentPlanSchemaV1,
			Status:     types.DeploymentPlanStatusExecuted,
		},
		existing,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reused).To(BeTrue())
	g.Expect(tasks).To(Equal(existing))
}
