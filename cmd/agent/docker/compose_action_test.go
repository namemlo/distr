package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

func TestExecuteComposeDeployStepRedactsRegistryPasswordFromEmittedEventsAndOutputs(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	const secretValue = "super-secret-password"
	inputs := validComposeDeployInputs()
	inputs["applicationVersion"].(map[string]any)["registryAuth"].(map[string]any)["registry.example.com"].(map[string]any)["password"] = secretValue
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		ActionType:    composeDeployActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}
	apply := func(_ context.Context, deployment api.AgentDeployment, _ composeDeployOptions, updateStatus func(string)) (*AgentDeployment, string, error) {
		updateStatus("pulling image with " + secretValue)
		return &AgentDeployment{
			ID:         deployment.ID,
			RevisionID: deployment.RevisionID,
			DockerType: *deployment.DockerType,
			State:      StateReady,
		}, "ready with " + secretValue, nil
	}

	err := executeComposeDeployStep(ctx, lease, step, recorder, apply)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeSucceeded,
	}))
	payload, err := json.Marshal(recorder.events)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring(secretValue))
	g.Expect(string(payload)).To(ContainSubstring("[REDACTED]"))
	g.Expect(recorder.events[1].Message).To(Equal("pulling image with [REDACTED]"))
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "status",
		Value: "ready with [REDACTED]",
	}))
}

func TestExecuteComposeDeployStepRedactsRegistryPasswordFromReturnedError(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	const secretValue = "super-secret-password"
	inputs := validComposeDeployInputs()
	inputs["applicationVersion"].(map[string]any)["registryAuth"].(map[string]any)["registry.example.com"].(map[string]any)["password"] = secretValue
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		ActionType:    composeDeployActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}
	apply := func(_ context.Context, _ api.AgentDeployment, _ composeDeployOptions, updateStatus func(string)) (*AgentDeployment, string, error) {
		updateStatus("pulling image with " + secretValue)
		return nil, "stderr contains " + secretValue, errors.New("compose failed with " + secretValue)
	}

	err := executeComposeDeployStep(ctx, lease, step, recorder, apply)

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).NotTo(ContainSubstring(secretValue))
	g.Expect(err.Error()).To(ContainSubstring("[REDACTED]"))
	payload, marshalErr := json.Marshal(recorder.events)
	g.Expect(marshalErr).ToNot(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring(secretValue))
}

func TestApplyComposeFileSwarmRedactsCommandOutputFromErrorAndAgentLogs(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	const secretValue = "super-secret-password"
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("FAKE_DOCKER_COMMAND_SECRET", secretValue)
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	collector := &recordingDeploymentTargetLogCollector{}
	oldCollector := platformLoggingCore.Collector
	platformLoggingCore.Collector = collector
	t.Cleanup(func() { platformLoggingCore.Collector = oldCollector })
	dockerType := types.DockerTypeSwarm
	deployment := api.AgentDeployment{
		ID:          uuid.New(),
		RevisionID:  uuid.New(),
		DockerType:  &dockerType,
		ComposeFile: []byte("name: secret-stack\nservices:\n  web:\n    image: registry.example.com/app:latest\n"),
		RegistryAuth: map[string]api.AgentRegistryAuth{
			"registry.example.com": {Username: "deploy-user", Password: secretValue},
		},
	}

	_, err := ApplyComposeFileSwarm(ctx, deployment, func(string) {})

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).NotTo(ContainSubstring(secretValue))
	g.Expect(err.Error()).To(ContainSubstring("[REDACTED]"))
	payload, marshalErr := json.Marshal(collector.records)
	g.Expect(marshalErr).ToNot(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring(secretValue))
	g.Expect(string(payload)).To(ContainSubstring("[REDACTED]"))
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

func TestExecuteComposeDeployStepCancelsApplyWhenProgressEventFails(t *testing.T) {
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
		recordingStepEventClient: recordingStepEventClient{
			stepEventErr:   errors.New("progress event rejected"),
			stepEventErrOn: types.StepRunEventTypeProgress,
		},
	}
	applyCanceled := false
	apply := func(ctx context.Context, _ api.AgentDeployment, _ composeDeployOptions, updateStatus func(string)) (*AgentDeployment, string, error) {
		updateStatus("creating services")
		select {
		case <-ctx.Done():
			applyCanceled = true
			return nil, "", ctx.Err()
		case <-time.After(25 * time.Millisecond):
			return nil, "", errors.New("apply context was not canceled")
		}
	}

	err := executeComposeDeployStep(ctx, lease, step, recorder, apply)

	g.Expect(err).To(MatchError(ContainSubstring("progress event rejected")))
	g.Expect(applyCanceled).To(BeTrue())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
	}))
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

func TestExecuteComposeDeployStepWaitsForRacingHeartbeatFailureBeforeSuccess(t *testing.T) {
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
	recorder := &racingHeartbeatClient{
		heartbeatStarted: make(chan struct{}),
		releaseHeartbeat: make(chan struct{}),
	}
	apply := func(_ context.Context, deployment api.AgentDeployment, _ composeDeployOptions, _ func(string)) (*AgentDeployment, string, error) {
		<-recorder.heartbeatStarted
		close(recorder.releaseHeartbeat)
		return &AgentDeployment{
			ID:         deployment.ID,
			RevisionID: deployment.RevisionID,
			DockerType: *deployment.DockerType,
			State:      StateReady,
		}, "ready", nil
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
	stepRunIDs     []uuid.UUID
	events         []api.AgentStepRunEventRequest
	stepEventErr   error
	stepEventErrOn types.StepRunEventType
}

func (c *recordingStepEventClient) RecordStepRunEvent(_ context.Context, stepRunID uuid.UUID, request api.AgentStepRunEventRequest) (*api.StepRunEvent, error) {
	if c.stepEventErr != nil && (c.stepEventErrOn == "" || c.stepEventErrOn == request.Type) {
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

type racingHeartbeatClient struct {
	recordingStepEventClient
	heartbeatStarted chan struct{}
	releaseHeartbeat chan struct{}
	startedClosed    bool
}

func (c *racingHeartbeatClient) HeartbeatTaskLease(_ context.Context, taskID uuid.UUID, leaseToken string) (*api.AgentTaskLease, error) {
	if !c.startedClosed {
		close(c.heartbeatStarted)
		c.startedClosed = true
	}
	<-c.releaseHeartbeat
	time.Sleep(25 * time.Millisecond)
	return nil, errors.New("heartbeat rejected")
}

func eventTypes(events []api.AgentStepRunEventRequest) []types.StepRunEventType {
	values := make([]types.StepRunEventType, 0, len(events))
	for _, event := range events {
		values = append(values, event.Type)
	}
	return values
}

type recordingDeploymentTargetLogCollector struct {
	records []api.DeploymentTargetLogRecord
}

func (c *recordingDeploymentTargetLogCollector) ExportDeploymentTargetLogs(records ...api.DeploymentTargetLogRecord) error {
	c.records = append(c.records, records...)
	return nil
}

func fakeDockerCommandContext(ctx context.Context, command string, args ...string) *exec.Cmd {
	helperArgs := []string{"-test.run=TestDockerCommandHelper", "--", command}
	helperArgs = append(helperArgs, args...)
	return exec.CommandContext(ctx, os.Args[0], helperArgs...)
}

func TestDockerCommandHelper(t *testing.T) {
	if os.Getenv("GO_WANT_DOCKER_COMMAND_HELPER") != "1" {
		return
	}
	args := os.Args
	separator := -1
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator == -1 || len(args) <= separator+1 {
		os.Exit(2)
	}
	dockerArgs := args[separator+1:]
	if dockerArgs[0] != "docker" {
		os.Exit(2)
	}
	if path := os.Getenv("FAKE_DOCKER_COMMAND_ARGS_FILE"); path != "" {
		data, _ := json.Marshal(dockerArgs)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			os.Exit(2)
		}
		_, _ = f.Write(append(data, '\n'))
		_ = f.Close()
	}
	if len(dockerArgs) >= 2 && dockerArgs[1] == "info" {
		fmt.Fprint(os.Stdout, "active")
		os.Exit(0)
	}
	if len(dockerArgs) >= 2 && dockerArgs[1] == "inspect" {
		if inspect := os.Getenv("FAKE_DOCKER_EXISTING_INSPECT"); inspect != "" {
			fmt.Fprint(os.Stdout, inspect)
			os.Exit(0)
		}
		state := os.Getenv("FAKE_DOCKER_EXISTING_STATE")
		if state == "" {
			fmt.Fprint(os.Stderr, "Error: No such object")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, `{"State":%s,"Config":{"Labels":{}}}`, state)
		os.Exit(0)
	}
	if len(dockerArgs) >= 2 && dockerArgs[1] == "logs" {
		fmt.Fprint(os.Stdout, os.Getenv("FAKE_DOCKER_LOGS"))
		os.Exit(0)
	}
	if len(dockerArgs) >= 2 && dockerArgs[1] == "wait" {
		if rawSleep := os.Getenv("FAKE_DOCKER_WAIT_SLEEP_MS"); rawSleep != "" {
			ms, err := strconv.Atoi(rawSleep)
			if err != nil {
				os.Exit(2)
			}
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
		code := os.Getenv("FAKE_DOCKER_WAIT_EXIT_CODE")
		if code == "" {
			code = "0"
		}
		fmt.Fprint(os.Stdout, code)
		os.Exit(0)
	}
	if len(dockerArgs) >= 2 && dockerArgs[1] == "stop" {
		os.Exit(0)
	}
	if len(dockerArgs) >= 2 && dockerArgs[1] == "rm" {
		os.Exit(0)
	}
	if len(dockerArgs) >= 2 && dockerArgs[1] == "start" {
		os.Exit(0)
	}
	if len(dockerArgs) >= 2 && dockerArgs[1] == "run" {
		if os.Getenv("FAKE_DOCKER_REQUIRE_SECRET_FILE_CONTAINER_READABLE") == "1" {
			if err := checkFakeDockerSecretFileContainerReadable(dockerArgs); err != nil {
				fmt.Fprint(os.Stderr, err.Error())
				os.Exit(2)
			}
		}
		if path := os.Getenv("FAKE_DOCKER_CONTAINER_METADATA_FILE"); path != "" {
			if err := writeFakeDockerContainerMetadata(path, dockerArgs); err != nil {
				fmt.Fprint(os.Stderr, err.Error())
				os.Exit(2)
			}
		}
		if rawSleep := os.Getenv("FAKE_DOCKER_RUN_SLEEP_MS"); rawSleep != "" {
			ms, err := strconv.Atoi(rawSleep)
			if err != nil {
				os.Exit(2)
			}
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
		if os.Getenv("FAKE_DOCKER_VERIFY_SECRET_FILE_AFTER_SLEEP") == "1" {
			source := fakeDockerSecretEnvSourceFromRun(dockerArgs)
			if source == "" {
				fmt.Fprint(os.Stderr, "secret env file volume was not mounted")
				os.Exit(2)
			}
			if _, err := os.Stat(source); err != nil {
				fmt.Fprint(os.Stderr, err.Error())
				os.Exit(2)
			}
		}
		fmt.Fprint(os.Stdout, os.Getenv("FAKE_DOCKER_RUN_OUTPUT"))
		if rawCode := os.Getenv("FAKE_DOCKER_RUN_EXIT_CODE"); rawCode != "" {
			code, err := strconv.Atoi(rawCode)
			if err != nil {
				os.Exit(2)
			}
			os.Exit(code)
		}
		os.Exit(0)
	}
	if len(dockerArgs) >= 3 && dockerArgs[1] == "stack" && dockerArgs[2] == "deploy" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		fmt.Fprintf(os.Stdout, "stack deploy failed with %s", os.Getenv("FAKE_DOCKER_COMMAND_SECRET"))
		os.Exit(1)
	}
	os.Exit(2)
}

func checkFakeDockerSecretFileContainerReadable(dockerArgs []string) error {
	suffix := ":" + ociJobSecretEnvMountPath + ":ro"
	for i := 2; i < len(dockerArgs)-1; i++ {
		if dockerArgs[i] != "--volume" {
			continue
		}
		bind := dockerArgs[i+1]
		if !strings.HasSuffix(bind, suffix) {
			continue
		}
		source := strings.TrimSuffix(bind, suffix)
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o004 == 0 {
			return fmt.Errorf("secret env file is not readable by a non-owner container user: %v", info.Mode().Perm())
		}
		return nil
	}
	return fmt.Errorf("secret env file volume was not mounted")
}

func writeFakeDockerContainerMetadata(path string, dockerArgs []string) error {
	env := []string{}
	labels := map[string]string{}
	binds := []string{}
	logDriver := ""
	image := ""
	cmd := []string{}
	for i := 2; i < len(dockerArgs); i++ {
		arg := dockerArgs[i]
		switch arg {
		case "--name", "--network", "--security-opt", "--cap-drop", "--user", "--cpus", "--memory", "--entrypoint":
			i++
		case "--log-driver":
			i++
			if i >= len(dockerArgs) {
				return fmt.Errorf("missing --log-driver value")
			}
			logDriver = dockerArgs[i]
		case "--env-file":
			i++
			if i >= len(dockerArgs) {
				return fmt.Errorf("missing --env-file value")
			}
			data, err := os.ReadFile(dockerArgs[i])
			if err != nil {
				return err
			}
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if strings.TrimSpace(line) != "" {
					env = append(env, line)
				}
			}
		case "--label":
			i++
			if i >= len(dockerArgs) {
				return fmt.Errorf("missing --label value")
			}
			key, value, ok := strings.Cut(dockerArgs[i], "=")
			if ok {
				labels[key] = value
			}
		case "--volume":
			i++
			if i >= len(dockerArgs) {
				return fmt.Errorf("missing --volume value")
			}
			binds = append(binds, dockerArgs[i])
		case "--read-only":
		default:
			image = arg
			cmd = append(cmd, dockerArgs[i+1:]...)
			i = len(dockerArgs)
		}
	}
	data, err := json.Marshal(map[string]any{
		"Config": map[string]any{
			"Env":    env,
			"Image":  image,
			"Cmd":    cmd,
			"Labels": labels,
		},
		"HostConfig": map[string]any{
			"Binds":     binds,
			"LogConfig": map[string]any{"Type": logDriver},
		},
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
