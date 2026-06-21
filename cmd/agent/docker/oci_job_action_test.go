package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestOCIJobActionInputRejectsMutableImageTag(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	inputs := validOCIJobInputs()
	inputs["imageDigest"] = "registry.example.com/jobs/cleanup:latest"

	_, err := decodeOCIJobActionInput(inputs)

	g.Expect(err).To(MatchError(ContainSubstring("imageDigest must be an immutable sha256 digest reference")))
}

func TestOCIJobActionInputRejectsPolicyUnsafeSettings(t *testing.T) {
	setOCIJobPolicyEnv(t)
	tests := []struct {
		name    string
		setup   func(*testing.T)
		mutate  func(*testing.T, map[string]any)
		message string
	}{
		{
			name: "registry not allowlisted",
			setup: func(t *testing.T) {
				t.Setenv(ociJobAllowedRegistriesEnv, "registry.other.example.com")
			},
			message: "image registry is not allowlisted",
		},
		{
			name: "network not allowlisted",
			mutate: func(_ *testing.T, inputs map[string]any) {
				inputs["network"] = "bridge"
			},
			message: "network is not allowlisted",
		},
		{
			name: "writable host mount",
			mutate: func(t *testing.T, inputs map[string]any) {
				root := t.TempDir()
				source := filepath.Join(root, "input")
				g := NewWithT(t)
				g.Expect(os.Mkdir(source, 0o700)).To(Succeed())
				t.Setenv(ociJobAllowedMountRootsEnv, root)
				inputs["volumes"] = []any{
					map[string]any{"source": source, "target": "/input", "readOnly": false},
				}
			},
			message: "volumes must be read-only",
		},
		{
			name: "disallowed host mount root",
			mutate: func(t *testing.T, inputs map[string]any) {
				sourceRoot := t.TempDir()
				allowedRoot := t.TempDir()
				source := filepath.Join(sourceRoot, "input")
				g := NewWithT(t)
				g.Expect(os.Mkdir(source, 0o700)).To(Succeed())
				t.Setenv(ociJobAllowedMountRootsEnv, allowedRoot)
				inputs["volumes"] = []any{
					map[string]any{"source": source, "target": "/host/etc", "readOnly": true},
				}
			},
			message: "volume source is not under an allowlisted mount root",
		},
		{
			name: "relative host mount source",
			mutate: func(t *testing.T, inputs map[string]any) {
				t.Setenv(ociJobAllowedMountRootsEnv, t.TempDir())
				inputs["volumes"] = []any{
					map[string]any{"source": "relative/input", "target": "/input", "readOnly": true},
				}
			},
			message: "volume source must be an absolute path",
		},
		{
			name: "symlink escape from allowed mount root",
			mutate: func(t *testing.T, inputs map[string]any) {
				allowedRoot := t.TempDir()
				outsideRoot := t.TempDir()
				outsideSource := filepath.Join(outsideRoot, "actual")
				linkSource := filepath.Join(allowedRoot, "link")
				g := NewWithT(t)
				g.Expect(os.Mkdir(outsideSource, 0o700)).To(Succeed())
				if err := os.Symlink(outsideSource, linkSource); err != nil {
					t.Skipf("symlink creation unavailable: %v", err)
				}
				t.Setenv(ociJobAllowedMountRootsEnv, allowedRoot)
				inputs["volumes"] = []any{
					map[string]any{"source": linkSource, "target": "/input", "readOnly": true},
				}
			},
			message: "volume source is not under an allowlisted mount root",
		},
		{
			name: "privileged mode",
			mutate: func(_ *testing.T, inputs map[string]any) {
				inputs["security"] = map[string]any{"privileged": true}
			},
			message: "privileged mode is not supported",
		},
		{
			name: "writable root filesystem",
			mutate: func(_ *testing.T, inputs map[string]any) {
				inputs["security"] = map[string]any{"readOnlyRootFilesystem": false}
			},
			message: "read-only root filesystem cannot be disabled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			setOCIJobPolicyEnv(t)
			inputs := validOCIJobInputs()
			if tt.setup != nil {
				tt.setup(t)
			}
			if tt.mutate != nil {
				tt.mutate(t, inputs)
			}

			_, err := decodeOCIJobActionInput(inputs)

			g.Expect(err).To(MatchError(ContainSubstring(tt.message)))
		})
	}
}

func TestExecuteOCIJobStepUsesDigestAndDockerHardeningWithoutSecretInArgs(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	const secretValue = "super-secret-job-token"
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_RUN_OUTPUT", "job completed")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           validOCIJobInputs(),
		SecretReferences: []string{"secret:job_api_token"},
		IdempotencyKey:   "sha256:job-key",
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeSucceeded,
	}))
	commands := readFakeDockerCommands(t, argsFile)
	run := findFakeDockerCommand(commands, "run")
	g.Expect(run).NotTo(BeNil())
	joinedRun := strings.Join(run, " ")
	g.Expect(joinedRun).To(ContainSubstring("registry.example.com/jobs/cleanup@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	g.Expect(run).To(ContainElement("--read-only"))
	g.Expect(run).To(ContainElement("--security-opt"))
	g.Expect(run).To(ContainElement("no-new-privileges"))
	g.Expect(run).To(ContainElement("--cap-drop"))
	g.Expect(run).To(ContainElement("ALL"))
	g.Expect(run).To(ContainElement("--network"))
	g.Expect(run).To(ContainElement("none"))
	g.Expect(run).To(ContainElement("--env-file"))
	g.Expect(joinedRun).NotTo(ContainSubstring(secretValue))
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "exitCode",
		Value: 0,
	}))
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "status",
		Value: "job completed",
	}))
}

func TestExecuteOCIJobStepRedactsSecretFromFailureEventsAndReturnedError(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	const secretValue = "super-secret-job-token"
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_RUN_OUTPUT", "failed with "+secretValue)
	t.Setenv("FAKE_DOCKER_RUN_EXIT_CODE", "42")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           validOCIJobInputs(),
		SecretReferences: []string{"secret:job_api_token"},
		IdempotencyKey:   "sha256:job-key",
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).NotTo(ContainSubstring(secretValue))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
	payload, marshalErr := json.Marshal(recorder.events)
	g.Expect(marshalErr).ToNot(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring(secretValue))
	g.Expect(string(payload)).To(ContainSubstring("[REDACTED]"))
}

func TestExecuteOCIJobStepAcceptsExpectedNonZeroExitCode(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_RUN_OUTPUT", "migration reported no-op")
	t.Setenv("FAKE_DOCKER_RUN_EXIT_CODE", "42")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["expectedExitCodes"] = []any{0, 42}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:      uuid.New(),
		Key:            "cleanup",
		ActionType:     ociJobActionType,
		ActionVersion:  types.AgentActionVersionV1,
		Inputs:         inputs,
		IdempotencyKey: "sha256:job-key",
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeSucceeded,
	}))
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "exitCode",
		Value: 42,
	}))
}

func TestExecuteOCIJobStepTruncatesOversizedDockerOutput(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_RUN_OUTPUT", strings.Repeat("x", types.MaxStepRunLogChunkBodyLength+4096))
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:      uuid.New(),
		Key:            "cleanup",
		ActionType:     ociJobActionType,
		ActionVersion:  types.AgentActionVersionV1,
		Inputs:         validOCIJobInputs(),
		IdempotencyKey: "sha256:job-key",
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	var status string
	for _, output := range recorder.events[2].Outputs {
		if output.Name == "status" {
			status = output.Value.(string)
		}
	}
	g.Expect(status).To(HaveLen(types.MaxStepRunLogChunkBodyLength))
	g.Expect(status).To(ContainSubstring("[TRUNCATED]"))
}

func TestExecuteTaskLeaseRunsOCIJobStep(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_RUN_OUTPUT", "ok")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	lease := api.AgentTaskLease{
		TaskID:     uuid.New(),
		LeaseToken: "lease-token",
		Steps: []api.AgentTaskLeaseStep{
			{
				StepRunID:      uuid.New(),
				Key:            "cleanup",
				ActionType:     ociJobActionType,
				ActionVersion:  types.AgentActionVersionV1,
				Inputs:         validOCIJobInputs(),
				IdempotencyKey: "sha256:job-key",
			},
		},
	}
	recorder := &recordingLeasedTaskClient{}
	apply := func(_ context.Context, _ api.AgentDeployment, _ composeDeployOptions, _ func(string)) (*AgentDeployment, string, error) {
		t.Fatal("compose apply should not run for OCI jobs")
		return nil, "", nil
	}

	err := executeTaskLease(ctx, lease, recorder, apply)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(recorder.heartbeatTaskIDs).To(Equal([]uuid.UUID{lease.TaskID}))
	g.Expect(recorder.heartbeatTokens).To(Equal([]string{"lease-token"}))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeSucceeded,
	}))
}

func TestExecuteOCIJobStepReusesExistingContainerForIdempotency(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_LOGS", "previous job output")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:      uuid.New(),
		Key:            "cleanup",
		ActionType:     ociJobActionType,
		ActionVersion:  types.AgentActionVersionV1,
		Inputs:         validOCIJobInputs(),
		IdempotencyKey: "sha256:job-key",
	}
	t.Setenv("FAKE_DOCKER_EXISTING_INSPECT", fakeOCIJobInspectJSON(t, lease, step, map[string]any{
		"Status":   "exited",
		"Running":  false,
		"ExitCode": 0,
	}))
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "run")).To(BeNil())
	g.Expect(findFakeDockerCommand(commands, "inspect")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "logs")).NotTo(BeNil())
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "status",
		Value: "previous job output",
	}))
}

func TestExecuteOCIJobStepWaitsForExistingRunningContainerAfterRestart(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_WAIT_EXIT_CODE", "0")
	t.Setenv("FAKE_DOCKER_LOGS", "restart recovered job output")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:      uuid.New(),
		Key:            "cleanup",
		ActionType:     ociJobActionType,
		ActionVersion:  types.AgentActionVersionV1,
		Inputs:         validOCIJobInputs(),
		IdempotencyKey: "sha256:job-key",
	}
	t.Setenv("FAKE_DOCKER_EXISTING_INSPECT", fakeOCIJobInspectJSON(t, lease, step, map[string]any{
		"Status":   "running",
		"Running":  true,
		"ExitCode": 0,
	}))
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "run")).To(BeNil())
	g.Expect(findFakeDockerCommand(commands, "inspect")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "wait")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "logs")).NotTo(BeNil())
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "status",
		Value: "restart recovered job output",
	}))
}

func TestExecuteOCIJobStepRejectsExistingContainerWithDifferentOperation(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:      uuid.New(),
		Key:            "cleanup",
		ActionType:     ociJobActionType,
		ActionVersion:  types.AgentActionVersionV1,
		Inputs:         validOCIJobInputs(),
		IdempotencyKey: "sha256:job-key",
	}
	inspect := fakeOCIJobInspectJSON(t, lease, step, map[string]any{
		"Status":   "exited",
		"Running":  false,
		"ExitCode": 0,
	})
	inspect = strings.Replace(inspect, `"distr.operationHash":"`, `"distr.operationHash":"sha256:foreign`, 1)
	t.Setenv("FAKE_DOCKER_EXISTING_INSPECT", inspect)
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("existing OCI job container does not match operation identity")))
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "run")).To(BeNil())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteOCIJobStepStopsContainerOnTimeout(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_RUN_SLEEP_MS", "1500")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["timeoutSeconds"] = 1
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:      uuid.New(),
		Key:            "cleanup",
		ActionType:     ociJobActionType,
		ActionVersion:  types.AgentActionVersionV1,
		Inputs:         inputs,
		IdempotencyKey: "sha256:job-key",
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("OCI job timed out")))
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "stop")).NotTo(BeNil())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteOCIJobStepStopsExistingRunningContainerOnTimeout(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_WAIT_SLEEP_MS", "1500")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["timeoutSeconds"] = 1
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:      uuid.New(),
		Key:            "cleanup",
		ActionType:     ociJobActionType,
		ActionVersion:  types.AgentActionVersionV1,
		Inputs:         inputs,
		IdempotencyKey: "sha256:job-key",
	}
	t.Setenv("FAKE_DOCKER_EXISTING_INSPECT", fakeOCIJobInspectJSON(t, lease, step, map[string]any{
		"Status":   "running",
		"Running":  true,
		"ExitCode": 0,
	}))
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("OCI job timed out")))
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "wait")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "stop")).NotTo(BeNil())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteOCIJobStepStopsContainerOnCancellation(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_RUN_SLEEP_MS", "1500")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:      uuid.New(),
		Key:            "cleanup",
		ActionType:     ociJobActionType,
		ActionVersion:  types.AgentActionVersionV1,
		Inputs:         validOCIJobInputs(),
		IdempotencyKey: "sha256:job-key",
	}
	recorder := &recordingLeasedTaskClient{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- executeOCIJobStep(ctx, lease, step, recorder)
	}()
	deadline := time.After(2 * time.Second)
	for findFakeDockerCommand(readFakeDockerCommands(t, argsFile), "run") == nil {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for fake docker run")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	err := <-errCh

	g.Expect(err).To(MatchError(ContainSubstring("OCI job canceled")))
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "stop")).NotTo(BeNil())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func validOCIJobInputs() map[string]any {
	return map[string]any{
		"imageDigest": "registry.example.com/jobs/cleanup@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"command":     []any{"/bin/cleanup"},
		"arguments":   []any{"--tenant", "demo"},
		"environment": map[string]any{
			"MODE":      "once",
			"API_TOKEN": "super-secret-job-token",
		},
		"network":           "none",
		"timeoutSeconds":    30,
		"expectedExitCodes": []any{0},
		"idempotencyKey":    "sha256:job-key",
		"runAsUser":         "1000:1000",
		"resources":         map[string]any{"cpus": 0.5, "memoryBytes": 134217728},
		"security":          map[string]any{"readOnlyRootFilesystem": true},
	}
}

func setOCIJobPolicyEnv(t *testing.T) {
	t.Helper()
	t.Setenv(ociJobAllowedRegistriesEnv, "registry.example.com")
	t.Setenv(ociJobAllowedNetworksEnv, "none")
}

func readFakeDockerCommands(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read fake docker commands: %v", err)
	}
	var commands [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var command []string
		if err := json.Unmarshal([]byte(line), &command); err != nil {
			t.Fatalf("decode fake docker command %q: %v", line, err)
		}
		commands = append(commands, command)
	}
	return commands
}

func findFakeDockerCommand(commands [][]string, name string) []string {
	for _, command := range commands {
		if len(command) >= 2 && command[0] == "docker" && command[1] == name {
			return command
		}
	}
	return nil
}

func fakeOCIJobInspectJSON(
	t *testing.T,
	lease api.AgentTaskLease,
	step api.AgentTaskLeaseStep,
	state map[string]any,
) string {
	t.Helper()
	input, err := decodeOCIJobActionInput(step.Inputs)
	if err != nil {
		t.Fatalf("decode OCI job input for fake inspect: %v", err)
	}
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = step.IdempotencyKey
	}
	identity, err := ociJobOperationIdentityFromStep(lease, step, input)
	if err != nil {
		t.Fatalf("build OCI job identity for fake inspect: %v", err)
	}
	data, err := json.Marshal(map[string]any{
		"State": state,
		"Config": map[string]any{
			"Labels": ociJobExpectedLabels(identity),
		},
	})
	if err != nil {
		t.Fatalf("encode fake inspect: %v", err)
	}
	return string(data)
}
