package handlers

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestDeploymentCampaignControlRoutesAreComplete(t *testing.T) {
	g := gomega.NewWithT(t)
	g.Expect(DeploymentCampaignControlRoutePaths()).To(gomega.Equal([]string{
		"POST /api/v1/deployment-campaigns/{id}/pause",
		"POST /api/v1/deployment-campaigns/{id}/resume",
		"POST /api/v1/deployment-campaigns/{id}/retry",
		"POST /api/v1/deployment-campaigns/{id}/exclude",
		"POST /api/v1/deployment-campaigns/{id}/cancel",
	}))
}
