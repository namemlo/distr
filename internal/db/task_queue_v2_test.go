package db

import (
	"testing"

	"github.com/distr-sh/distr/internal/apierrors"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestValidateDeploymentPlanTaskCreationKeepsPublicCreatorV1Only(t *testing.T) {
	g := NewWithT(t)
	err := validateDeploymentPlanTaskCreation(types.DeploymentPlan{
		PlanSchema: types.TargetDeploymentPlanSchemaV2,
		Status:     types.DeploymentPlanStatusReady,
	})

	g.Expect(err).To(MatchError(ContainSubstring("admitted v2")))
	g.Expect(err).To(MatchError(apierrors.ErrConflict))
}

func TestValidateDeploymentPlanTaskCreationAllowsReadyLegacyPlan(t *testing.T) {
	g := NewWithT(t)

	err := validateDeploymentPlanTaskCreation(types.DeploymentPlan{
		PlanSchema: types.LegacyDeploymentPlanSchemaV1,
		Status:     types.DeploymentPlanStatusReady,
	})

	g.Expect(err).NotTo(HaveOccurred())
}

func TestAdmittedTargetPlanV2WithoutExistingTasksContinuesToCreation(t *testing.T) {
	g := NewWithT(t)

	tasks, reused, err := reuseExistingAdmittedV2DeploymentPlanTasks(
		types.DeploymentPlan{
			PlanSchema:      types.TargetDeploymentPlanSchemaV2,
			ProtocolVersion: string(types.ExecutionProtocolVersionV2),
			Status:          types.DeploymentPlanStatusReady,
			Targets: []types.DeploymentPlanTarget{{
				ID: uuid.New(), DeploymentTargetID: uuid.New(),
			}},
		},
		nil,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reused).To(BeFalse())
	g.Expect(tasks).To(BeNil())
}

func TestAdmittedTargetPlanV2AllowsNewOccurrenceAfterExecution(t *testing.T) {
	g := NewWithT(t)
	plan, _ := targetPlanV2TaskReplayFixture()

	tasks, reused, err := reuseExistingAdmittedV2DeploymentPlanTasks(plan, nil)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reused).To(BeFalse())
	g.Expect(tasks).To(BeNil())
}

func TestAdmittedTargetPlanV2RevalidatesFrozenProtocol(t *testing.T) {
	g := NewWithT(t)
	plan, _ := targetPlanV2TaskReplayFixture()
	plan.ProtocolVersion = string(types.ExecutionProtocolVersionV1)

	_, _, err := reuseExistingAdmittedV2DeploymentPlanTasks(plan, nil)

	g.Expect(err).To(MatchError(ContainSubstring("protocol_version v2")))
	g.Expect(err).To(MatchError(apierrors.ErrConflict))
}

func TestAdmittedTargetPlanV2RejectsPlanWithoutTargets(t *testing.T) {
	g := NewWithT(t)
	plan, _ := targetPlanV2TaskReplayFixture()
	plan.Targets = nil

	_, _, err := reuseExistingAdmittedV2DeploymentPlanTasks(plan, nil)

	g.Expect(err).To(MatchError(ContainSubstring("target")))
	g.Expect(err).To(MatchError(apierrors.ErrConflict))
}

func TestExistingTargetPlanV2TasksReplayAfterExecution(t *testing.T) {
	g := NewWithT(t)
	plan, existing := targetPlanV2TaskReplayFixture()

	tasks, reused, err := reuseExistingAdmittedV2DeploymentPlanTasks(plan, existing)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reused).To(BeTrue())
	g.Expect(tasks).To(Equal(existing))
}

func TestExistingTargetPlanV2TasksFailClosedUnlessSetExactlyMatchesPlan(t *testing.T) {
	plan, existing := targetPlanV2TaskReplayFixture()
	tests := []struct {
		name     string
		plan     types.DeploymentPlan
		existing []types.Task
	}{
		{name: "incomplete", plan: plan, existing: existing[:1]},
		{name: "duplicate plan target", plan: plan, existing: []types.Task{existing[0], existing[0]}},
		{name: "conflicting target", plan: plan, existing: append(existing[:1:1], func() types.Task {
			conflicting := existing[1]
			conflicting.DeploymentTargetID = uuid.New()
			return conflicting
		}())},
		{name: "conflicting protocol", plan: plan, existing: append(existing[:1:1], func() types.Task {
			conflicting := existing[1]
			conflicting.ProtocolVersion = types.ExecutionProtocolVersionV1
			return conflicting
		}())},
		{name: "conflicting occurrence", plan: plan, existing: append(existing[:1:1], func() types.Task {
			conflicting := existing[1]
			conflicting.ExecutionOccurrenceID = uuid.New()
			return conflicting
		}())},
		{name: "plan not executed", plan: func() types.DeploymentPlan {
			ready := plan
			ready.Status = types.DeploymentPlanStatusReady
			return ready
		}(), existing: existing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			tasks, reused, err := reuseExistingAdmittedV2DeploymentPlanTasks(tt.plan, tt.existing)

			g.Expect(err).To(MatchError(apierrors.ErrConflict))
			g.Expect(reused).To(BeFalse())
			g.Expect(tasks).To(BeNil())
		})
	}
}

func TestExistingLegacyTasksRemainIdempotentAfterExecution(t *testing.T) {
	g := NewWithT(t)
	existing := []types.Task{{}}

	tasks, reused, err := reuseExistingDeploymentPlanTasks(
		types.DeploymentPlan{
			PlanSchema: types.LegacyDeploymentPlanSchemaV1,
			Status:     types.DeploymentPlanStatusExecuted,
		},
		existing,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reused).To(BeTrue())
	g.Expect(tasks).To(Equal(existing))
}

func targetPlanV2TaskReplayFixture() (types.DeploymentPlan, []types.Task) {
	executionOccurrenceID := uuid.New()
	plan := types.DeploymentPlan{
		ID:              uuid.New(),
		OrganizationID:  uuid.New(),
		ApplicationID:   uuid.New(),
		ReleaseBundleID: uuid.New(),
		ChannelID:       uuid.New(),
		EnvironmentID:   uuid.New(),
		PlanSchema:      types.TargetDeploymentPlanSchemaV2,
		ProtocolVersion: string(types.ExecutionProtocolVersionV2),
		Status:          types.DeploymentPlanStatusExecuted,
		Targets: []types.DeploymentPlanTarget{
			{ID: uuid.New(), DeploymentTargetID: uuid.New()},
			{ID: uuid.New(), DeploymentTargetID: uuid.New()},
		},
	}
	tasks := make([]types.Task, 0, len(plan.Targets))
	for _, target := range plan.Targets {
		tasks = append(tasks, types.Task{
			ID:                     uuid.New(),
			OrganizationID:         plan.OrganizationID,
			TaskType:               types.TaskTypeDeployment,
			DeploymentPlanID:       plan.ID,
			ExecutionOccurrenceID:  executionOccurrenceID,
			DeploymentPlanTargetID: target.ID,
			DeploymentTargetID:     target.DeploymentTargetID,
			ApplicationID:          plan.ApplicationID,
			ReleaseBundleID:        plan.ReleaseBundleID,
			ChannelID:              plan.ChannelID,
			EnvironmentID:          plan.EnvironmentID,
			ProtocolVersion:        types.ExecutionProtocolVersionV2,
		})
	}
	return plan, tasks
}
