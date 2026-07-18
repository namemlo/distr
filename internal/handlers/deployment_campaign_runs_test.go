package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	"github.com/onsi/gomega"
)

func TestCampaignRunHandlerReturnsMappedRun(t *testing.T) {
	g := gomega.NewWithT(t)
	runID := uuid.New()
	handler := GetDeploymentCampaignRunHandler(func(
		_ *http.Request,
		id uuid.UUID,
	) (*types.CampaignRun, error) {
		g.Expect(id).To(gomega.Equal(runID))
		return &types.CampaignRun{ID: id, State: types.CampaignRunStateRunning, Version: 2}, nil
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/deployment-campaign-runs/"+runID.String(), nil)
	request.SetPathValue("campaignRunId", runID.String())
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	g.Expect(response.Code).To(gomega.Equal(http.StatusOK))
	g.Expect(response.Body.String()).To(gomega.ContainSubstring(runID.String()))
	g.Expect(response.Body.String()).To(gomega.ContainSubstring(`"state":"RUNNING"`))
}
