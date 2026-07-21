package executionworker

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestEvaluatePersistedAdmissionRequiresExactExecutedV2TaskAndPassedPreflight(t *testing.T) {
	g := NewWithT(t)
	taskID, orgID, targetID, planID, environmentID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	task := types.Task{
		ID: taskID, OrganizationID: orgID, DeploymentTargetID: targetID,
		DeploymentPlanID: planID, EnvironmentID: environmentID,
		ProtocolVersion: types.ExecutionProtocolVersionV2, Status: types.TaskStatusQueued,
	}
	plan := types.DeploymentPlan{
		ID: planID, OrganizationID: orgID, EnvironmentID: environmentID,
		ProtocolVersion: string(types.ExecutionProtocolVersionV2), Status: types.DeploymentPlanStatusExecuted,
		Steps: []types.DeploymentPlanStep{{StepKey: "deploy", Included: true, ActionType: "distr.compose.deploy"}},
		PreflightRuns: []types.DeploymentPreflightRun{{
			Status: types.DeploymentPreflightStatusPassed,
			Checks: []types.DeploymentPreflightCheck{{
				TaskID: &taskID, Status: types.DeploymentPreflightCheckStatusPassed,
			}},
		}},
	}
	decision := evaluatePersistedAdmission(task, plan, AdmissionRequest{
		OrganizationID: orgID, DeploymentTargetID: targetID, EnvironmentID: environmentID,
		PlanID: planID, TaskID: taskID, StepKey: "deploy",
	})
	g.Expect(decision.ScopedEnrollment).To(BeTrue())
	g.Expect(decision.PlanApproved).To(BeTrue())
	g.Expect(decision.PlanAdmitted).To(BeTrue())
	g.Expect(decision.AdapterPreflight).To(BeTrue())

	plan.PreflightRuns[0].Checks[0].Status = types.DeploymentPreflightCheckStatusFailed
	g.Expect(evaluatePersistedAdmission(task, plan, AdmissionRequest{
		OrganizationID: orgID, DeploymentTargetID: targetID, EnvironmentID: environmentID,
		PlanID: planID, TaskID: taskID, StepKey: "deploy",
	}).AdapterPreflight).To(BeFalse())
}

func TestDeriveFrozenAttemptInputsReusesNormalDispatchAndAdvancesExplicitRetry(t *testing.T) {
	g := NewWithT(t)
	task := types.Task{
		ID: uuid.New(), DeploymentTargetID: uuid.New(),
		Locks: []types.TaskResourceLock{{ResourceKey: "deployment-target:choice-tp-dev"}},
	}
	plan := types.DeploymentPlan{
		CanonicalChecksum: checksumForFrozenInput("plan"),
		Steps: []types.DeploymentPlanStep{{
			StepKey: "deploy", ActionType: "distr.compose.deploy", ActionName: "compose-v1",
			TimeoutSeconds: 300, RetryMaxAttempts: 2, Included: true,
		}},
		TargetComponents: []types.DeploymentPlanTargetComponent{{
			DeploymentTargetID: task.DeploymentTargetID, Component: "transaction-api",
			ConfigChecksum: checksumForFrozenInput("config"),
		}},
	}
	bundle := types.ReleaseBundle{CanonicalChecksum: checksumForFrozenInput("bundle")}
	latest := &types.ExecutionAttempt{
		Identity:     types.ExecutionIdentity{AttemptNumber: 2},
		PlanChecksum: checksumForFrozenInput("old-plan"), ArtifactDigest: checksumForFrozenInput("old-artifact"),
		ConfigChecksum: checksumForFrozenInput("old-config"), AdapterRevision: "old:v1",
		Cancellable: true, RetrySafe: true,
		Fence: types.ExecutionFence{ResourceKey: "old-resource", Generation: 4},
	}

	replay, err := deriveFrozenAttemptInputs(task, plan, bundle, "deploy", latest, false)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(replay.AttemptNumber).To(Equal(2))
	g.Expect(replay.PlanChecksum).To(Equal(latest.PlanChecksum))
	g.Expect(replay.FenceGeneration).To(Equal(int64(4)))

	retry, err := deriveFrozenAttemptInputs(task, plan, bundle, "deploy", latest, true)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(retry.AttemptNumber).To(Equal(3))
	g.Expect(retry.PlanChecksum).To(Equal(plan.CanonicalChecksum))
	g.Expect(retry.ArtifactDigest).To(Equal(bundle.CanonicalChecksum))
	g.Expect(retry.FenceGeneration).To(Equal(int64(5)))
	g.Expect(retry.IntentTTL).To(Equal(5 * time.Minute))
}
