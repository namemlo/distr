package handlers

import (
	"errors"
	"net/http"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/mapping"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type CampaignRunLoader func(*http.Request, uuid.UUID) (*types.CampaignRun, error)

func GetDeploymentCampaignRunHandler(load CampaignRunLoader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID, err := uuid.Parse(r.PathValue("campaignRunId"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		run, err := load(r, runID)
		if errors.Is(err, apierrors.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		RespondJSON(w, mapping.DeploymentCampaignRunToAPI(*run))
	}
}
