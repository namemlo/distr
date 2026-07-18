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
		Identity: types.ExecutionIdentity{ExecutionID: request.ExecutionID, AttemptNumber: 1, StepKey: request.StepKey},
		Status:   types.ExecutionAttemptStatusPending,
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
		OrganizationID: uuid.New(), ExecutionID: uuid.New(), StepKey: "deploy",
	})
	g.Expect(err).To(MatchError(ContainSubstring("adapter_preflight")))
	g.Expect(creator.calls).To(Equal(0))

	dispatcher = NewDispatcher(admissionGateStub{decision: AdmissionDecision{
		OperatorFlag: true, ExecutorFlag: true, ScopedEnrollment: true,
		PlanApproved: true, PlanAdmitted: true, AdapterPreflight: true,
	}}, creator)
	attempt, err := dispatcher.Dispatch(context.Background(), DispatchRequest{
		OrganizationID: uuid.New(), ExecutionID: uuid.New(), StepKey: "deploy",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(attempt.Status).To(Equal(types.ExecutionAttemptStatusPending))
	g.Expect(creator.calls).To(Equal(1))
}
