package mapping

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentProcessToAPI(t *testing.T) {
	g := NewWithT(t)
	id := uuid.New()
	appID := uuid.New()
	createdAt := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 21, 10, 30, 0, 0, time.UTC)

	response := DeploymentProcessToAPI(types.DeploymentProcess{
		ID:            id,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		ApplicationID: appID,
		Name:          "Standard deploy",
		Description:   "Deploys through the standard lifecycle",
		SortOrder:     20,
	})

	g.Expect(response).To(Equal(api.DeploymentProcess{
		ID:            id,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		ApplicationID: appID,
		Name:          "Standard deploy",
		Description:   "Deploys through the standard lifecycle",
		SortOrder:     20,
	}))
}

func TestDeploymentProcessRevisionToAPI(t *testing.T) {
	g := NewWithT(t)
	processID := uuid.New()
	revisionID := uuid.New()
	stepID := uuid.New()
	channelID := uuid.New()
	environmentID := uuid.New()
	stepTemplateVersionID := uuid.New()
	createdAt := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 21, 10, 30, 0, 0, time.UTC)

	response := DeploymentProcessRevisionToAPI(types.DeploymentProcessRevision{
		ID:                  revisionID,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
		DeploymentProcessID: processID,
		RevisionNumber:      2,
		Description:         "Adds deploy step",
		Steps: []types.DeploymentProcessStep{
			{
				ID:                          stepID,
				DeploymentProcessRevisionID: revisionID,
				Key:                         "deploy",
				Name:                        "Deploy",
				ActionType:                  "distr.http.check",
				StepTemplateVersionID:       &stepTemplateVersionID,
				ExecutionLocation:           "hub",
				InputBindings:               map[string]any{"url": "https://example.com/health"},
				Condition:                   "channel == stable",
				ChannelIDs:                  []uuid.UUID{channelID},
				EnvironmentIDs:              []uuid.UUID{environmentID},
				TargetTags:                  []string{"linux"},
				FailureMode:                 "fail",
				TimeoutSeconds:              120,
				RetryMaxAttempts:            3,
				RetryIntervalSeconds:        10,
				RequiredPermissions:         []string{"deploy:write"},
				SortOrder:                   20,
				Dependencies:                []string{"build"},
			},
		},
	})

	g.Expect(response).To(Equal(api.DeploymentProcessRevision{
		ID:                  revisionID,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
		DeploymentProcessID: processID,
		RevisionNumber:      2,
		Description:         "Adds deploy step",
		Steps: []api.DeploymentProcessStep{
			{
				ID:                          stepID,
				DeploymentProcessRevisionID: revisionID,
				Key:                         "deploy",
				Name:                        "Deploy",
				ActionType:                  "distr.http.check",
				StepTemplateVersionID:       &stepTemplateVersionID,
				ExecutionLocation:           "hub",
				InputBindings:               map[string]any{"url": "https://example.com/health"},
				Condition:                   "channel == stable",
				ChannelIDs:                  []uuid.UUID{channelID},
				EnvironmentIDs:              []uuid.UUID{environmentID},
				TargetTags:                  []string{"linux"},
				FailureMode:                 "fail",
				TimeoutSeconds:              120,
				RetryPolicy:                 api.DeploymentProcessStepRetryPolicy{MaxAttempts: 3, IntervalSeconds: 10},
				RequiredPermissions:         []string{"deploy:write"},
				SortOrder:                   20,
				Dependencies:                []string{"build"},
			},
		},
	}))
}
