package executionworker

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

type runtimeRepositoryStub struct {
	decision AdmissionDecision
	inputs   FrozenAttemptInputs
	requests []CreateAttemptRequest
}

func (s *runtimeRepositoryStub) EvaluateExecutionV2Admission(
	_ context.Context, request AdmissionRequest,
) (AdmissionDecision, error) {
	s.requests = append(s.requests, CreateAttemptRequest{TaskID: request.TaskID})
	return s.decision, nil
}

func (s *runtimeRepositoryStub) LoadFrozenAttemptInputs(
	_ context.Context, request CreateAttemptRequest,
) (FrozenAttemptInputs, error) {
	s.requests = append(s.requests, request)
	return s.inputs, nil
}

type admissionGateStub struct {
	decision AdmissionDecision
}

func (s admissionGateStub) EvaluateExecutionV2Admission(
	context.Context, AdmissionRequest,
) (AdmissionDecision, error) {
	return s.decision, nil
}

type attemptCreatorStub struct {
	calls       int
	lastRequest CreateAttemptRequest
}

type readyStepRunsLoaderStub struct {
	steps []types.StepRun
	err   error
	calls int
}

func (s *readyStepRunsLoaderStub) LoadExecutionV2ReadyStepRuns(
	context.Context, uuid.UUID, uuid.UUID,
) ([]types.StepRun, error) {
	s.calls++
	return s.steps, s.err
}

func (s *attemptCreatorStub) CreateExecutionAttempt(
	_ context.Context, request CreateAttemptRequest,
) (*types.ExecutionAttempt, error) {
	s.calls++
	s.lastRequest = request
	return &types.ExecutionAttempt{
		ID: uuid.New(), OrganizationID: request.OrganizationID,
		DeploymentTargetID: request.DeploymentTargetID,
		Identity:           types.ExecutionIdentity{ExecutionID: request.ExecutionID, AttemptNumber: 1, StepKey: request.StepKey},
		Status:             types.ExecutionAttemptStatusPending,
	}, nil
}

func TestExplicitRetryDispatchMarksAttemptCreationAsRetry(t *testing.T) {
	g := NewWithT(t)
	creator := &attemptCreatorStub{}
	dispatcher := NewProtocolDispatcher(nil, NewDispatcher(admissionGateStub{decision: AdmissionDecision{
		OperatorFlag: true, ExecutorFlag: true, ScopedEnrollment: true,
		PlanApproved: true, PlanAdmitted: true, AdapterPreflight: true,
	}}, creator))
	task := types.Task{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		EnvironmentID: uuid.New(), DeploymentPlanID: uuid.New(),
		ProtocolVersion: types.ExecutionProtocolVersionV2,
		StepRuns:        []types.StepRun{{ID: uuid.New(), StepKey: "deploy", Status: types.StepRunStatusPending}},
	}
	ctx := WithProtocolDispatcher(context.Background(), dispatcher)
	g.Expect(DispatchTaskRetry(ctx, task)).To(Succeed())
	g.Expect(creator.lastRequest.Retry).To(BeTrue())
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
	dispatcher.readySteps = &readyStepRunsLoaderStub{steps: v2Task.StepRuns}
	ctx := WithProtocolDispatcher(context.Background(), dispatcher)
	g.Expect(DispatchCreatedTasks(ctx, []types.Task{v1, v2Task})).To(Succeed())
	g.Expect(creator.calls).To(Equal(1))
}

func TestCreatedTaskDispatchesEveryDependencyReadyStepWithoutAnAttempt(t *testing.T) {
	g := NewWithT(t)
	creator := &attemptCreatorStub{}
	first := types.StepRun{ID: uuid.New(), StepKey: "prepare", Status: types.StepRunStatusPending}
	second := types.StepRun{ID: uuid.New(), StepKey: "deploy", Status: types.StepRunStatusPending}
	loader := &readyStepRunsLoaderStub{steps: []types.StepRun{first, second}}
	dispatcher := NewProtocolDispatcher(nil, NewDispatcher(admissionGateStub{decision: AdmissionDecision{
		OperatorFlag: true, ExecutorFlag: true, ScopedEnrollment: true,
		PlanApproved: true, PlanAdmitted: true, AdapterPreflight: true,
	}}, creator))
	dispatcher.readySteps = loader
	task := types.Task{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		EnvironmentID: uuid.New(), DeploymentPlanID: uuid.New(),
		ProtocolVersion: types.ExecutionProtocolVersionV2,
		StepRuns:        []types.StepRun{first, second},
	}

	err := DispatchCreatedTasks(
		WithProtocolDispatcher(context.Background(), dispatcher), []types.Task{task},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(loader.calls).To(Equal(1))
	g.Expect(creator.calls).To(Equal(2))
}

func TestDispatchReadyTaskStepsIsNoOpWhenNoDependencyReadyStepRemains(t *testing.T) {
	g := NewWithT(t)
	creator := &attemptCreatorStub{}
	loader := &readyStepRunsLoaderStub{}
	dispatcher := NewProtocolDispatcher(nil, NewDispatcher(admissionGateStub{decision: AdmissionDecision{
		OperatorFlag: true, ExecutorFlag: true, ScopedEnrollment: true,
		PlanApproved: true, PlanAdmitted: true, AdapterPreflight: true,
	}}, creator))
	dispatcher.readySteps = loader
	task := types.Task{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		ProtocolVersion: types.ExecutionProtocolVersionV2,
	}

	err := DispatchReadyTaskSteps(
		WithProtocolDispatcher(context.Background(), dispatcher), task,
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(creator.calls).To(Equal(0))
}

func TestDispatchRecoveredTasksUsesPreloadedReadyStepsWithoutRepositoryLookup(t *testing.T) {
	g := NewWithT(t)
	creator := &attemptCreatorStub{}
	loader := &readyStepRunsLoaderStub{err: fmt.Errorf("recovery must not query steps per task")}
	dispatcher := NewProtocolDispatcher(nil, NewDispatcher(admissionGateStub{decision: AdmissionDecision{
		OperatorFlag: true, ExecutorFlag: true, ScopedEnrollment: true,
		PlanApproved: true, PlanAdmitted: true, AdapterPreflight: true,
	}}, creator))
	dispatcher.readySteps = loader
	task := types.Task{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		EnvironmentID: uuid.New(), DeploymentPlanID: uuid.New(),
		ProtocolVersion: types.ExecutionProtocolVersionV2,
		StepRuns: []types.StepRun{
			{ID: uuid.New(), StepKey: "prepare", Status: types.StepRunStatusPending},
			{ID: uuid.New(), StepKey: "deploy", Status: types.StepRunStatusPending},
		},
	}

	err := DispatchRecoveredTasks(
		WithProtocolDispatcher(context.Background(), dispatcher), []types.Task{task},
	)

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(loader.calls).To(Equal(0))
	g.Expect(creator.calls).To(Equal(2))
}

func TestRepositoryAdmissionGateUsesProcessFlagsAndDurableDecision(t *testing.T) {
	g := NewWithT(t)
	repository := &runtimeRepositoryStub{decision: AdmissionDecision{
		ScopedEnrollment: true, PlanApproved: true, PlanAdmitted: true, AdapterPreflight: true,
	}}
	flags := featureflags.NewRegistry([]featureflags.Key{
		featureflags.KeyOperatorControlPlaneV2, featureflags.KeyExecutorProtocolV2,
	})
	gate := NewRepositoryAdmissionGate(flags, repository)
	decision, err := gate.EvaluateExecutionV2Admission(context.Background(), AdmissionRequest{
		OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(), TaskID: uuid.New(),
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decision.OperatorFlag).To(BeTrue())
	g.Expect(decision.ExecutorFlag).To(BeTrue())
	g.Expect(decision.ScopedEnrollment).To(BeTrue())
	g.Expect(repository.requests).To(HaveLen(1))
}

func TestRepositoryFrozenInputsLoaderUsesDurableSnapshot(t *testing.T) {
	g := NewWithT(t)
	checksum := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return "sha256:" + fmt.Sprintf("%x", sum)
	}
	repository := &runtimeRepositoryStub{inputs: FrozenAttemptInputs{
		AttemptNumber: 1, PlanChecksum: checksum("plan"), ArtifactDigest: checksum("artifact"),
		ConfigChecksum: checksum("config"), AdapterRevision: "distr.compose.deploy:v1",
		ResourceKey: "deployment-target:test", FenceGeneration: 1,
		Cancellable: true, RetrySafe: true, IntentTTL: 5 * time.Minute,
	}}
	loader := NewRepositoryFrozenAttemptInputsLoader(repository)
	request := CreateAttemptRequest{OrganizationID: uuid.New(), TaskID: uuid.New(), StepKey: "deploy"}
	inputs, err := loader.LoadFrozenAttemptInputs(context.Background(), request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(inputs).To(Equal(repository.inputs))
	g.Expect(repository.requests).To(ConsistOf(request))
}
