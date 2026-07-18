package executionworker

import (
	"context"
	"testing"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

type admissionGateStub struct {
	decision AdmissionDecision
}

func (s admissionGateStub) EvaluateExecutionV2Admission(
	context.Context, AdmissionRequest,
) (AdmissionDecision, error) {
	return s.decision, nil
}

type attemptCreatorStub struct {
	calls int
}

func (s *attemptCreatorStub) CreateExecutionAttempt(
	_ context.Context, request CreateAttemptRequest,
) (*types.ExecutionAttempt, error) {
	s.calls++
	return &types.ExecutionAttempt{
		ID: uuid.New(), OrganizationID: request.OrganizationID,
		DeploymentTargetID: request.DeploymentTargetID,
		Identity:           types.ExecutionIdentity{ExecutionID: request.ExecutionID, AttemptNumber: 1, StepKey: request.StepKey},
		Status:             types.ExecutionAttemptStatusPending,
	}, nil
}

func TestExecutionV2DispatcherRequiresEveryFrozenAdmissionGate(t *testing.T) {
	g := NewWithT(t)
	creator := &attemptCreatorStub{}
	dispatcher := NewDispatcher(admissionGateStub{decision: AdmissionDecision{
		OperatorFlag: true, ExecutorFlag: true, ScopedEnrollment: true,
		PlanApproved: true, PlanAdmitted: true, AdapterPreflight: false,
	}}, creator)
	_, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		ExecutionID: uuid.New(), StepKey: "deploy",
	})
	g.Expect(err).To(MatchError(ContainSubstring("adapter_preflight")))
	g.Expect(creator.calls).To(Equal(0))

	dispatcher = NewDispatcher(admissionGateStub{decision: AdmissionDecision{
		OperatorFlag: true, ExecutorFlag: true, ScopedEnrollment: true,
		PlanApproved: true, PlanAdmitted: true, AdapterPreflight: true,
	}}, creator)
	attempt, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		ExecutionID: uuid.New(), StepKey: "deploy",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(attempt.Status).To(Equal(types.ExecutionAttemptStatusPending))
	g.Expect(creator.calls).To(Equal(1))
}

func TestCreatedTasksRouteV1ToLeaseWorkersAndV2ToSignedDispatcher(t *testing.T) {
	g := NewWithT(t)
	creator := &attemptCreatorStub{}
	v2 := NewDispatcher(admissionGateStub{decision: AdmissionDecision{
		OperatorFlag: true, ExecutorFlag: true, ScopedEnrollment: true,
		PlanApproved: true, PlanAdmitted: true, AdapterPreflight: true,
	}}, creator)
	dispatcher := NewProtocolDispatcher(nil, v2)
	v1 := types.Task{
		ID: uuid.New(), ProtocolVersion: types.ExecutionProtocolVersionV1,
	}
	v2Task := types.Task{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		EnvironmentID: uuid.New(), DeploymentPlanID: uuid.New(),
		ProtocolVersion: types.ExecutionProtocolVersionV2,
		StepRuns: []types.StepRun{{
			ID: uuid.New(), StepKey: "deploy", Status: types.StepRunStatusPending,
		}},
	}
	ctx := WithProtocolDispatcher(context.Background(), dispatcher)
	g.Expect(DispatchCreatedTasks(ctx, []types.Task{v1, v2Task})).To(Succeed())
	g.Expect(creator.calls).To(Equal(1))
}
