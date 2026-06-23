package mapping_test

import (
	"encoding/json"
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
	g.Expect(item.LegacyDeploymentID).NotTo(BeNil())
	g.Expect(*item.LegacyDeploymentID).To(Equal(legacyDeploymentID))
	g.Expect(item.LegacyDeploymentRevisionID).NotTo(BeNil())
	g.Expect(*item.LegacyDeploymentRevisionID).To(Equal(legacyRevisionID))
	g.Expect(item.SyntheticReleaseID).NotTo(BeNil())
	g.Expect(*item.SyntheticReleaseID).To(Equal(syntheticReleaseID))
	g.Expect(item.Availability).To(Equal(availability))

	payload, err := json.Marshal(item)
	g.Expect(err).NotTo(HaveOccurred())
	var raw map[string]any
	g.Expect(json.Unmarshal(payload, &raw)).To(Succeed())
	g.Expect(raw).To(HaveKeyWithValue("source", string(types.DeploymentTimelineItemSourceLegacyDeployment)))
	g.Expect(raw).To(HaveKeyWithValue("legacyDeploymentId", legacyDeploymentID.String()))
	g.Expect(raw).To(HaveKeyWithValue("legacyDeploymentRevisionId", legacyRevisionID.String()))
	g.Expect(raw).To(HaveKeyWithValue("syntheticReleaseId", syntheticReleaseID.String()))
	g.Expect(raw).NotTo(HaveKey("taskId"))
	g.Expect(raw).NotTo(HaveKey("deploymentPlanId"))
	g.Expect(raw).NotTo(HaveKey("deploymentPlanTargetId"))
	g.Expect(raw).NotTo(HaveKey("releaseBundleId"))
	g.Expect(raw).NotTo(HaveKey("channelId"))
	g.Expect(raw).NotTo(HaveKey("environmentId"))
}
