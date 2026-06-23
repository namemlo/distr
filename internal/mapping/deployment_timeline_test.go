package mapping_test

import (
	"testing"

	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentTimelineItemToAPIExposesCompatibilityMetadata(t *testing.T) {
	g := NewWithT(t)
	legacyDeploymentID := uuid.New()
	legacyRevisionID := uuid.New()
	syntheticReleaseID := uuid.New()

	availability := types.DeploymentCompatibilityAvailability{
		ProcessSnapshot:  true,
		VariableSnapshot: false,
		Channel:          false,
		Environment:      true,
		TaskLogs:         false,
		RedeployPlan:     false,
	}

	item := mapping.DeploymentTimelineItemToAPI(types.DeploymentTimelineItem{
		Source:                     types.DeploymentTimelineItemSourceLegacyDeployment,
		LegacyDeploymentID:         legacyDeploymentID,
		LegacyDeploymentRevisionID: legacyRevisionID,
		SyntheticReleaseID:         syntheticReleaseID,
		Availability:               availability,
	})

	g.Expect(item.Source).To(Equal(types.DeploymentTimelineItemSourceLegacyDeployment))
	g.Expect(item.LegacyDeploymentID).To(Equal(legacyDeploymentID))
	g.Expect(item.LegacyDeploymentRevisionID).To(Equal(legacyRevisionID))
	g.Expect(item.SyntheticReleaseID).To(Equal(syntheticReleaseID))
	g.Expect(item.Availability).To(Equal(availability))
}
