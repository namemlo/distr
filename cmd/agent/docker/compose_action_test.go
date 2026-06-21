package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestComposeDeployActionInputBuildsAgentDeployment(t *testing.T) {
	g := NewWithT(t)
	stepRunID := uuid.New()

	input, err := decodeComposeDeployActionInput(validComposeDeployInputs())
	g.Expect(err).ToNot(HaveOccurred())

	deployment, err := agentDeploymentFromComposeAction(stepRunID, input)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(deployment.ID).To(Equal(stepRunID))
	g.Expect(deployment.RevisionID).To(Equal(stepRunID))
	g.Expect(deployment.EnvFile).To(Equal([]byte("PORT=8080\n")))
	g.Expect(deployment.RegistryAuth).To(HaveKeyWithValue("registry.example.com", api.AgentRegistryAuth{
		Username: "user",
		Password: "pass",
	}))
	g.Expect(deployment.DockerType).ToNot(BeNil())
	g.Expect(*deployment.DockerType).To(Equal(types.DockerTypeCompose))

	projectName, err := getProjectName(deployment.ComposeFile)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(projectName).To(Equal("distr-preview"))
}

func TestComposeDeployActionInputSupportsSwarmStrategy(t *testing.T) {
	g := NewWithT(t)
	inputs := validComposeDeployInputs()
	inputs["strategy"] = "swarm"
	inputs["waitForHealthy"] = false

	input, err := decodeComposeDeployActionInput(inputs)
	g.Expect(err).ToNot(HaveOccurred())

	deployment, err := agentDeploymentFromComposeAction(uuid.New(), input)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(deployment.DockerType).ToNot(BeNil())
	g.Expect(*deployment.DockerType).To(Equal(types.DockerTypeSwarm))
}

func TestExecuteComposeDeployStepEmitsLifecycleEventsAndOutputs(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		ActionType:    composeDeployActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        validComposeDeployInputs(),
	}
	recorder := &recordingLeasedTaskClient{}
	apply := func(_ context.Context, deployment api.AgentDeployment, options composeDeployOptions, updateStatus func(string)) (*AgentDeployment, string, error) {
		g.Expect(options.PullPolicy).To(Equal("missing"))
		g.Expect(options.WaitForHealthy).To(BeTrue())
		g.Expect(options.Timeout).To(Equal(120 * time.Second))
		g.Expect(options.LocalDeploymentSource).To(Equal(AgentDeploymentSourceTask))
		updateStatus("creating services")
		projectName, err := getProjectName(deployment.ComposeFile)
		g.Expect(err).ToNot(HaveOccurred())
		return &AgentDeployment{
			ID:          deployment.ID,
			RevisionID:  deployment.RevisionID,
			ProjectName: projectName,
			DockerType:  *deployment.DockerType,
			State:       StateReady,
		}, "deployment ready", nil
	}

	err := executeComposeDeployStep(ctx, lease, step, recorder, apply)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(recorder.stepRunIDs).To(Equal([]uuid.UUID{step.StepRunID, step.StepRunID, step.StepRunID}))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeSucceeded,
	}))
	g.Expect(recorder.events[0].LeaseToken).To(Equal("lease-token"))
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "projectName",
		Value: "distr-preview",
	}))
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "strategy",
		Value: "compose",
	}))
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "state",
		Value: string(StateReady),
	}))
}

func TestExecuteTaskLeaseHeartbeatsAndRunsComposeStep(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	lease := api.AgentTaskLease{
		TaskID:     uuid.New(),
		LeaseToken: "lease-token",
		Steps: []api.AgentTaskLeaseStep{
			{
				StepRunID:     uuid.New(),
				ActionType:    composeDeployActionType,
				ActionVersion: types.AgentActionVersionV1,
				Inputs:        validComposeDeployInputs(),
			},
		},
	}
	recorder := &recordingLeasedTaskClient{}
	apply := func(_ context.Context, deployment api.AgentDeployment, _ composeDeployOptions, _ func(string)) (*AgentDeployment, string, error) {
		return &AgentDeployment{
			ID:         deployment.ID,
			RevisionID: deployment.RevisionID,
			DockerType: *deployment.DockerType,
			State:      StateReady,
		}, "ok", nil
	}

	err := executeTaskLease(ctx, lease, recorder, apply)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(recorder.heartbeatTaskIDs).To(Equal([]uuid.UUID{lease.TaskID}))
	g.Expect(recorder.heartbeatTokens).To(Equal([]string{"lease-token"}))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeSucceeded,
	}))
}

func TestExecuteTaskLeaseDoesNotApplyWhenInitialHeartbeatFails(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	lease := api.AgentTaskLease{
		TaskID:     uuid.New(),
		LeaseToken: "lease-token",
		Steps: []api.AgentTaskLeaseStep{
			{
				StepRunID:     uuid.New(),
				ActionType:    composeDeployActionType,
				ActionVersion: types.AgentActionVersionV1,
				Inputs:        validComposeDeployInputs(),
			},
		},
	}
	recorder := &recordingLeasedTaskClient{heartbeatErr: errors.New("heartbeat endpoint unavailable")}
	applied := false
	apply := func(_ context.Context, _ api.AgentDeployment, _ composeDeployOptions, _ func(string)) (*AgentDeployment, string, error) {
		applied = true
		return nil, "", nil
	}

	err := executeTaskLease(ctx, lease, recorder, apply)

	g.Expect(err).To(MatchError(ContainSubstring("heartbeat task lease")))
	g.Expect(applied).To(BeFalse())
	g.Expect(recorder.events).To(BeEmpty())
}

func TestExecuteComposeDeployStepDoesNotApplyWhenStepEventEndpointFails(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		ActionType:    composeDeployActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        validComposeDeployInputs(),
	}
	recorder := &recordingLeasedTaskClient{
		recordingStepEventClient: recordingStepEventClient{stepEventErr: errors.New("step event endpoint unavailable")},
	}
	applied := false
	apply := func(_ context.Context, _ api.AgentDeployment, _ composeDeployOptions, _ func(string)) (*AgentDeployment, string, error) {
		applied = true
		return nil, "", nil
	}

	err := executeComposeDeployStep(ctx, lease, step, recorder, apply)

	g.Expect(err).To(MatchError(ContainSubstring("step event endpoint unavailable")))
	g.Expect(applied).To(BeFalse())
	g.Expect(recorder.events).To(BeEmpty())
}

func TestExecuteTaskLeaseStartsBeforeUnsupportedActionFailure(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	lease := api.AgentTaskLease{
		TaskID:     uuid.New(),
		LeaseToken: "lease-token",
		Steps: []api.AgentTaskLeaseStep{
			{
				StepRunID:     uuid.New(),
				ActionType:    "distr.unknown.action",
				ActionVersion: types.AgentActionVersionV1,
			},
		},
	}
	recorder := &recordingLeasedTaskClient{}
	apply := func(_ context.Context, _ api.AgentDeployment, _ composeDeployOptions, _ func(string)) (*AgentDeployment, string, error) {
		t.Fatal("apply should not run for unsupported actions")
		return nil, "", nil
	}

	err := executeTaskLease(ctx, lease, recorder, apply)

	g.Expect(err).To(MatchError(ContainSubstring("unsupported actionType")))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeFailed,
	}))
	g.Expect(recorder.events[0].Sequence).To(Equal(int64(1)))
	g.Expect(recorder.events[1].Sequence).To(Equal(int64(2)))
}

func TestExecuteComposeDeployStepStartsBeforeInvalidInputFailure(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	inputs := validComposeDeployInputs()
	inputs["applicationVersion"] = map[string]any{}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		ActionType:    composeDeployActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}
	apply := func(_ context.Context, _ api.AgentDeployment, _ composeDeployOptions, _ func(string)) (*AgentDeployment, string, error) {
		t.Fatal("apply should not run for invalid inputs")
		return nil, "", nil
	}

	err := executeComposeDeployStep(ctx, lease, step, recorder, apply)

	g.Expect(err).To(MatchError(ContainSubstring("applicationVersion.composeFile is required")))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeFailed,
	}))
	g.Expect(recorder.events[0].Sequence).To(Equal(int64(1)))
	g.Expect(recorder.events[1].Sequence).To(Equal(int64(2)))
}

func TestExecuteComposeDeployStepStartsBeforeSetupFailure(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	inputs := validComposeDeployInputs()
	inputs["applicationVersion"] = map[string]any{
		"composeFile": "services:\n  web:\n    image: [",
	}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		ActionType:    composeDeployActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}
	apply := func(_ context.Context, _ api.AgentDeployment, _ composeDeployOptions, _ func(string)) (*AgentDeployment, string, error) {
		t.Fatal("apply should not run when setup fails")
		return nil, "", nil
	}

	err := executeComposeDeployStep(ctx, lease, step, recorder, apply)

	g.Expect(err).To(HaveOccurred())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeFailed,
	}))
	g.Expect(recorder.events[0].Sequence).To(Equal(int64(1)))
	g.Expect(recorder.events[1].Sequence).To(Equal(int64(2)))
}

func TestExecuteComposeDeployStepEmitsFailureEventWhenApplyFails(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		ActionType:    composeDeployActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        validComposeDeployInputs(),
	}
	recorder := &recordingLeasedTaskClient{}
	applyErr := errors.New("compose up failed")
	apply := func(_ context.Context, _ api.AgentDeployment, _ composeDeployOptions, updateStatus func(string)) (*AgentDeployment, string, error) {
		updateStatus("pulling images")
		return nil, "stderr details", applyErr
	}

	err := executeComposeDeployStep(ctx, lease, step, recorder, apply)

	g.Expect(err).To(MatchError(ContainSubstring("compose up failed")))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
	failed := recorder.events[2]
	g.Expect(failed.Message).To(ContainSubstring("compose up failed"))
	g.Expect(failed.Logs).To(ContainElement(api.AgentStepRunLogChunkRequest{
		Stream:   types.StepRunLogStreamStderr,
		Severity: types.StepRunLogSeverityError,
		Body:     "stderr details",
	}))
}

func TestExecuteComposeDeployStepCancelsApplyWhenHeartbeatFails(t *testing.T) {
	g := NewWithT(t)
	oldHeartbeatEvery := taskLeaseHeartbeatEvery
	taskLeaseHeartbeatEvery = time.Millisecond
	t.Cleanup(func() { taskLeaseHeartbeatEvery = oldHeartbeatEvery })

	ctx := context.Background()
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		ActionType:    composeDeployActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        validComposeDeployInputs(),
	}
	recorder := &recordingLeasedTaskClient{heartbeatErr: errors.New("heartbeat rejected")}
	apply := func(ctx context.Context, _ api.AgentDeployment, _ composeDeployOptions, _ func(string)) (*AgentDeployment, string, error) {
		<-ctx.Done()
		return nil, "", ctx.Err()
	}

	err := executeComposeDeployStep(ctx, lease, step, recorder, apply)

	g.Expect(err).To(MatchError(ContainSubstring("heartbeat task lease")))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeFailed,
	}))
	g.Expect(recorder.events[1].Message).To(ContainSubstring("heartbeat rejected"))
}

func validComposeDeployInputs() map[string]any {
	return map[string]any{
		"applicationVersion": map[string]any{
			"composeFile": "services:\n  web:\n    image: nginx:latest\n",
			"registryAuth": map[string]any{
				"registry.example.com": map[string]any{
					"username": "user",
					"password": "pass",
				},
			},
		},
		"projectName":     " distr-preview ",
		"environmentFile": "PORT=8080\n",
		"pullPolicy":      "missing",
		"waitForHealthy":  true,
		"timeoutSeconds":  120,
		"strategy":        "compose",
	}
}

type recordingStepEventClient struct {
	stepRunIDs   []uuid.UUID
	events       []api.AgentStepRunEventRequest
	stepEventErr error
}

func (c *recordingStepEventClient) RecordStepRunEvent(_ context.Context, stepRunID uuid.UUID, request api.AgentStepRunEventRequest) (*api.StepRunEvent, error) {
	if c.stepEventErr != nil {
		return nil, c.stepEventErr
	}
	c.stepRunIDs = append(c.stepRunIDs, stepRunID)
	c.events = append(c.events, request)
	return nil, nil
}

type recordingLeasedTaskClient struct {
	recordingStepEventClient
	heartbeatTaskIDs []uuid.UUID
	heartbeatTokens  []string
	heartbeatErr     error
}

func (c *recordingLeasedTaskClient) HeartbeatTaskLease(_ context.Context, taskID uuid.UUID, leaseToken string) (*api.AgentTaskLease, error) {
	c.heartbeatTaskIDs = append(c.heartbeatTaskIDs, taskID)
	c.heartbeatTokens = append(c.heartbeatTokens, leaseToken)
	if c.heartbeatErr != nil {
		return nil, c.heartbeatErr
	}
	return &api.AgentTaskLease{TaskID: taskID, LeaseToken: leaseToken}, nil
}

func eventTypes(events []api.AgentStepRunEventRequest) []types.StepRunEventType {
	values := make([]types.StepRunEventType, 0, len(events))
	for _, event := range events {
		values = append(values, event.Type)
	}
	return values
}
