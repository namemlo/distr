package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/internal/campaigns"
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

func TestCampaignV2RetryUnavailableHasExplicitHTTPContract(t *testing.T) {
	g := gomega.NewWithT(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/deployment-campaigns/id/retry", nil)
	g.Expect(writeCampaignControlError(
		response, request, campaigns.ErrCampaignV2RetryUnavailable,
	)).To(gomega.BeTrue())
	g.Expect(response.Code).To(gomega.Equal(http.StatusNotImplemented))
}
