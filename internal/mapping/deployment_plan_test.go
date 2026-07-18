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
	planTargetID := uuid.New()
	preflightRunID := uuid.New()
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
		Bootstrap:          true,
		Baselines: []types.DeploymentPlanBaseline{{
			ComponentInstanceID: uuid.New(),
			ComponentKey:        "loyalty-api",
			Projection:          types.BaselineProjectionBootstrap,
			Bootstrap:           true,
		}},
		Changes: []types.DeploymentPlanChangeEntry{{
			ComponentKey: "loyalty-api",
			Kind:         types.DeploymentPlanChangeBootstrap,
		}},
		Risks: []types.DeploymentPlanRiskEntry{{
			ComponentKey: "loyalty-api",
			Code:         "bootstrap_approval_required",
		}},
		Targets: []types.DeploymentPlanTarget{
			{
				ID:                 planTargetID,
				DeploymentTargetID: targetID,
				Name:               "cluster-a",
				Type:               types.DeploymentTypeDocker,
				SortOrder:          10,
			},
		},
		PreflightRuns: []types.DeploymentPreflightRun{
			{
				ID:               preflightRunID,
				CreatedAt:        createdAt.Add(time.Minute),
				DeploymentPlanID: uuid.New(),
				PlanChecksum:     "sha256:abc",
				Status:           types.DeploymentPreflightStatusPassed,
				Checks: []types.DeploymentPreflightCheck{
					{
						ID:                       uuid.New(),
						DeploymentPreflightRunID: preflightRunID,
						DeploymentPlanTargetID:   &planTargetID,
						DeploymentTargetID:       &targetID,
						CheckKey:                 "plan_checksum",
						Status:                   types.DeploymentPreflightCheckStatusPassed,
						Expected:                 map[string]any{"checksum": "sha256:abc"},
						Actual:                   map[string]any{"valid": true},
						Message:                  "plan canonical payload matches its checksum",
						SortOrder:                10,
					},
				},
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
	g.Expect(response.PreflightRuns).To(HaveLen(1))
	g.Expect(response.PreflightRuns[0].ID).To(Equal(preflightRunID))
	g.Expect(response.PreflightRuns[0].Checks).To(HaveLen(1))
	g.Expect(response.PreflightRuns[0].Checks[0].CheckKey).To(Equal("plan_checksum"))
	g.Expect(response.Steps).To(HaveLen(1))
	g.Expect(response.Steps[0].ActionName).To(Equal("HTTP check"))
	g.Expect(response.Variables).To(HaveLen(1))
	g.Expect(response.Variables[0].Value).To(MatchJSON(`"https://example.test"`))
	g.Expect(response.Issues).To(HaveLen(1))
	g.Expect(response.Issues[0].Code).To(Equal("dry_run_not_performed"))
	g.Expect(response.Bootstrap).To(BeTrue())
	g.Expect(response.Baselines).To(HaveLen(1))
	g.Expect(response.Changes[0].Kind).To(Equal(types.DeploymentPlanChangeBootstrap))
	g.Expect(response.Risks[0].Code).To(Equal("bootstrap_approval_required"))
}
