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
