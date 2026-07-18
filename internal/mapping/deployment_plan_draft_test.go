package mapping

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentPlanDraftValidationToAPI(t *testing.T) {
	g := NewWithT(t)
	planID := uuid.New()
	input := types.PlanDraftValidation{
		Draft: types.PlanDraft{
			ID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
			Revision: 3, ProductReleaseID: uuid.New(), DeploymentUnitID: uuid.New(),
			EnvironmentAssignmentID: uuid.New(), TargetConfigSnapshotID: uuid.New(),
			ProtocolVersion:           types.DeploymentPlanProtocolV2,
			PublishedDeploymentPlanID: &planID,
		},
		PreviewChecksum: "sha256:preview",
		Issues:          []types.ValidationIssue{{Code: "blocked"}},
	}

	result := DeploymentPlanDraftValidationToAPI(input)

	g.Expect(result.Draft.ID).To(Equal(input.Draft.ID))
	g.Expect(result.Draft.PublishedDeploymentPlanID).To(Equal(&planID))
	g.Expect(result.PreviewChecksum).To(Equal(input.PreviewChecksum))
	g.Expect(result.Issues).To(Equal(input.Issues))
}
