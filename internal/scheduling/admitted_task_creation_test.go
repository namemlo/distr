package scheduling

import (
	"context"
	"strings"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCreateTasksForAdmittedV2PlanRequiresFrozenV2Identity(t *testing.T) {
	g := NewWithT(t)
	admissionCalls := 0
	taskCalls := 0
	request := admittedTaskCreationTestRequest()
	dependencies := admittedTaskCreationTestDependencies()
	dependencies.LoadPlanSnapshot = func(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (types.AdmissionPlanSnapshot, error) {
		return types.AdmissionPlanSnapshot{
			PlanSchema:      "distr.deployment-plan/v1",
			ProtocolVersion: "v1",
		}, nil
	}
	dependencies.AdmitDeploymentPlan = func(
		context.Context,
		types.AdmitDeploymentPlanRequest,
	) (*types.AdmissionEvaluation, error) {
		admissionCalls++
		return nil, nil
	}
	dependencies.CreateTasks = func(
		context.Context,
		types.CreateTasksForDeploymentPlanRequest,
	) ([]types.Task, error) {
		taskCalls++
		return nil, nil
	}

	_, err := CreateTasksForAdmittedV2Plan(context.Background(), request, dependencies)

	g.Expect(err).To(MatchError(ContainSubstring("plan_schema v2")))
	g.Expect(admissionCalls).To(Equal(0))
	g.Expect(taskCalls).To(Equal(0))
}

func TestCreateTasksForAdmittedV2PlanRequiresExecutionOccurrence(t *testing.T) {
	g := NewWithT(t)
	request := admittedTaskCreationTestRequest()
	request.ExecutionOccurrenceID = uuid.Nil

	_, err := CreateTasksForAdmittedV2Plan(
		context.Background(),
		request,
		admittedTaskCreationTestDependencies(),
	)

	g.Expect(err).To(MatchError(ContainSubstring("executionOccurrenceId")))
}

func TestCreateTasksForAdmittedV2PlanCreatesTasksOnlyAfterAdmit(t *testing.T) {
	g := NewWithT(t)
	request := admittedTaskCreationTestRequest()
	dependencies := admittedTaskCreationTestDependencies()
	taskCalls := 0
	dependencies.AdmitDeploymentPlan = func(
		context.Context,
		types.AdmitDeploymentPlanRequest,
	) (*types.AdmissionEvaluation, error) {
		return &types.AdmissionEvaluation{Decision: types.AdmissionDecisionWait}, nil
	}
	dependencies.CreateTasks = func(
		_ context.Context,
		createRequest types.CreateTasksForDeploymentPlanRequest,
	) ([]types.Task, error) {
		taskCalls++
		g.Expect(createRequest.ExecutionOccurrenceID).To(Equal(request.ExecutionOccurrenceID))
		return []types.Task{{ID: uuid.New()}}, nil
	}

	_, err := CreateTasksForAdmittedV2Plan(context.Background(), request, dependencies)

	g.Expect(err).To(MatchError(ContainSubstring("WAIT")))
	g.Expect(taskCalls).To(Equal(0))

	admissionID := uuid.New()
	decisionChecksum := "sha256:" + strings.Repeat("a", 64)
	dependencies.AdmitDeploymentPlan = func(
		_ context.Context,
		admissionRequest types.AdmitDeploymentPlanRequest,
	) (*types.AdmissionEvaluation, error) {
		g.Expect(admissionRequest.DeploymentPlanID).To(Equal(request.DeploymentPlanID))
		g.Expect(admissionRequest.SchedulerIdempotencyKey).
			To(Equal(request.SchedulerIdempotencyKey))
		return &types.AdmissionEvaluation{
			ID: admissionID, Decision: types.AdmissionDecisionAdmit,
			DecisionChecksum: decisionChecksum,
		}, nil
	}
	dependencies.CreateTasks = func(
		_ context.Context,
		createRequest types.CreateTasksForDeploymentPlanRequest,
	) ([]types.Task, error) {
		taskCalls++
		g.Expect(createRequest.AdmissionEvaluationID).To(Equal(admissionID))
		g.Expect(createRequest.AdmissionDecisionChecksum).To(Equal(decisionChecksum))
		return []types.Task{{ID: uuid.New()}}, nil
	}
	tasks, err := CreateTasksForAdmittedV2Plan(
		context.Background(),
		request,
		dependencies,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(tasks).To(HaveLen(1))
	g.Expect(taskCalls).To(Equal(1))
}

func TestCreateTasksForAdmittedV2PlanPreservesCampaignRetryMemberScope(t *testing.T) {
	g := NewWithT(t)
	request := admittedTaskCreationTestRequest()
	request.CampaignRetryMemberRunID = uuid.New()
	request.Campaign = &types.AdmissionCampaignEvidence{
		ID: uuid.New(), Revision: 3, Checksum: "sha256:" + strings.Repeat("b", 64),
	}
	dependencies := admittedTaskCreationTestDependencies()
	dependencies.CreateTasks = func(
		_ context.Context,
		createRequest types.CreateTasksForDeploymentPlanRequest,
	) ([]types.Task, error) {
		g.Expect(createRequest.CampaignRetryMemberRunID).
			To(Equal(request.CampaignRetryMemberRunID))
		return []types.Task{{ID: uuid.New()}}, nil
	}

	_, err := CreateTasksForAdmittedV2Plan(context.Background(), request, dependencies)

	g.Expect(err).NotTo(HaveOccurred())
}

func TestCreateTasksForAdmittedV2PlanRejectsUnboundCampaignRetryScope(t *testing.T) {
	request := admittedTaskCreationTestRequest()
	request.CampaignRetryMemberRunID = uuid.New()

	_, err := CreateTasksForAdmittedV2Plan(
		context.Background(), request, admittedTaskCreationTestDependencies(),
	)

	NewWithT(t).Expect(err).To(MatchError(ContainSubstring("campaign evidence")))
}

func admittedTaskCreationTestRequest() types.CreateTasksForAdmittedV2PlanRequest {
	return types.CreateTasksForAdmittedV2PlanRequest{
		OrganizationID:          uuid.New(),
		DeploymentPlanID:        uuid.New(),
		ExecutionOccurrenceID:   uuid.New(),
		ActorUserAccountID:      uuid.New(),
		SchedulerIdempotencyKey: "scheduler:plan:1",
	}
}

func admittedTaskCreationTestDependencies() AdmittedTaskCreationDependencies {
	return AdmittedTaskCreationDependencies{
		LoadPlanSnapshot: func(
			context.Context,
			uuid.UUID,
			uuid.UUID,
		) (types.AdmissionPlanSnapshot, error) {
			return types.AdmissionPlanSnapshot{
				PlanSchema:      types.AdmissionRequiredPlanSchemaV2,
				ProtocolVersion: types.AdmissionRequiredProtocolV2,
			}, nil
		},
		AdmitDeploymentPlan: func(
			context.Context,
			types.AdmitDeploymentPlanRequest,
		) (*types.AdmissionEvaluation, error) {
			return &types.AdmissionEvaluation{Decision: types.AdmissionDecisionAdmit}, nil
		},
		CreateTasks: func(
			context.Context,
			types.CreateTasksForDeploymentPlanRequest,
		) ([]types.Task, error) {
			return []types.Task{{ID: uuid.New()}}, nil
		},
	}
}
