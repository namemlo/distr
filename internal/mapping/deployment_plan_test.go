package mapping

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestDeploymentPlanToAPI(t *testing.T) {
	g := NewWithT(t)
	processSnapshotID := uuid.New()
	variableSnapshotID := uuid.New()
	targetID := uuid.New()
	variableSetID := uuid.New()
	variableID := uuid.New()
	createdAt := time.Now().UTC()
	plan := types.DeploymentPlan{
		ID:                 uuid.New(),
		CreatedAt:          createdAt,
		ApplicationID:      uuid.New(),
		ReleaseBundleID:    uuid.New(),
		ChannelID:          uuid.New(),
		EnvironmentID:      uuid.New(),
		ProcessSnapshotID:  &processSnapshotID,
		VariableSnapshotID: &variableSnapshotID,
		Status:             types.DeploymentPlanStatusReady,
		CanonicalChecksum:  "sha256:abc",
		Targets: []types.DeploymentPlanTarget{
			{
				ID:                 uuid.New(),
				DeploymentTargetID: targetID,
				Name:               "cluster-a",
				Type:               types.DeploymentTypeDocker,
				SortOrder:          10,
			},
		},
		Steps: []types.DeploymentPlanStep{
			{
				ID:                uuid.New(),
				StepKey:           "deploy",
				Name:              "Deploy",
				ActionType:        "distr.http.check",
				ActionName:        "HTTP check",
				ExecutionLocation: "hub",
				InputBindings:     map[string]any{"url": "https://example.com/health"},
				SortOrder:         10,
				Included:          true,
			},
		},
		Variables: []types.DeploymentPlanVariable{
			{
				ID:            uuid.New(),
				VariableSetID: variableSetID,
				VariableID:    variableID,
				Key:           "api_url",
				Type:          types.VariableTypeString,
				Status:        types.VariableResolutionStatusResolved,
				Source:        types.VariableResolutionSourceDefault,
				Value:         json.RawMessage(`"https://example.test"`),
			},
		},
		Issues: []types.DeploymentPlanIssue{
			{
				ID:        uuid.New(),
				Severity:  types.DeploymentPlanIssueSeverityWarning,
				Code:      "dry_run_not_performed",
				Field:     "dryRun",
				Message:   "dry run unavailable",
				SortOrder: 10,
			},
		},
	}

	response := DeploymentPlanToAPI(plan)

	g.Expect(response.ID).To(Equal(plan.ID))
	g.Expect(response.CreatedAt).To(Equal(createdAt))
	g.Expect(response.ProcessSnapshotID).To(Equal(&processSnapshotID))
	g.Expect(response.VariableSnapshotID).To(Equal(&variableSnapshotID))
	g.Expect(response.Status).To(Equal(types.DeploymentPlanStatusReady))
	g.Expect(response.Targets).To(HaveLen(1))
	g.Expect(response.Targets[0].DeploymentTargetID).To(Equal(targetID))
	g.Expect(response.Steps).To(HaveLen(1))
	g.Expect(response.Steps[0].ActionName).To(Equal("HTTP check"))
	g.Expect(response.Variables).To(HaveLen(1))
	g.Expect(response.Variables[0].Value).To(MatchJSON(`"https://example.test"`))
	g.Expect(response.Issues).To(HaveLen(1))
	g.Expect(response.Issues[0].Code).To(Equal("dry_run_not_performed"))
}
