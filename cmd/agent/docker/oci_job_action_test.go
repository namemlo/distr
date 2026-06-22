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
				createOCIJobTestSymlink(t, outsideSource, linkSource)
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
	inputs := validOCIJobInputs()
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": secretValue}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
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

func TestExecuteOCIJobStepDoesNotPersistSecretEnvironmentInContainerMetadata(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	const secretValue = "super-secret-job-token"
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	metadataFile := filepath.Join(t.TempDir(), "container-metadata.json")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_CONTAINER_METADATA_FILE", metadataFile)
	t.Setenv("FAKE_DOCKER_REQUIRE_SECRET_FILE_CONTAINER_READABLE", "1")
	t.Setenv("FAKE_DOCKER_RUN_OUTPUT", "job completed")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": secretValue}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
		SecretReferences: []string{"secret:job_api_token"},
		IdempotencyKey:   "sha256:job-key",
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	metadata, err := os.ReadFile(metadataFile)
	g.Expect(err).ToNot(HaveOccurred())
	metadataText := string(metadata)
	g.Expect(metadataText).To(ContainSubstring("MODE=once"))
	g.Expect(metadataText).NotTo(ContainSubstring(secretValue))
	run := findFakeDockerCommand(readFakeDockerCommands(t, argsFile), "run")
	g.Expect(run).NotTo(BeNil())
	g.Expect(strings.Join(run, " ")).NotTo(ContainSubstring(secretValue))
	secretSource := fakeDockerSecretEnvSourceFromRun(run)
	g.Expect(secretSource).NotTo(Equal(""))
	g.Expect(sameOrChildPath(filepath.Clean(secretSource), filepath.Clean(os.Getenv(ociJobSecretStagingDirEnv)))).To(BeTrue())
	_, statErr := os.Stat(secretSource)
	g.Expect(os.IsNotExist(statErr)).To(BeTrue())
	entrypoint := indexOfFakeDockerArg(run, "--entrypoint")
	g.Expect(entrypoint).NotTo(Equal(-1))
	g.Expect(run[entrypoint+1]).To(Equal("/bin/sh"))
	image := indexOfFakeDockerArg(run, validOCIJobInputs()["imageDigest"].(string))
	g.Expect(image).NotTo(Equal(-1))
	g.Expect(run[image+1]).To(Equal("-c"))
	g.Expect(run[image+2]).To(ContainSubstring("exec \"$@\""))
	g.Expect(run).To(ContainElement("--log-driver"))
	g.Expect(run).To(ContainElement("none"))
	g.Expect(metadataText).To(ContainSubstring(`"LogConfig":{"Type":"none"}`))
}

func TestExecuteOCIJobStepDisablesDockerLogRetentionForRawSecretOutput(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	const secretValue = "super-secret-job-token"
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	metadataFile := filepath.Join(t.TempDir(), "container-metadata.json")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_CONTAINER_METADATA_FILE", metadataFile)
	t.Setenv("FAKE_DOCKER_RUN_OUTPUT", "printed "+secretValue)
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": secretValue}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
		SecretReferences: []string{"secret:job_api_token"},
		IdempotencyKey:   "sha256:job-key",
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	metadata, err := os.ReadFile(metadataFile)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(metadata)).To(ContainSubstring(`"LogConfig":{"Type":"none"}`))
	run := findFakeDockerCommand(readFakeDockerCommands(t, argsFile), "run")
	g.Expect(run).NotTo(BeNil())
	g.Expect(run).To(ContainElement("--log-driver"))
	g.Expect(run).To(ContainElement("none"))
	outputs, err := json.Marshal(recorder.events[2].Outputs)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(outputs)).NotTo(ContainSubstring(secretValue))
}

func TestExecuteOCIJobStepRequiresHostVisibleSecretStagingDir(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	t.Setenv(ociJobSecretStagingDirEnv, "")
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": "super-secret-job-token"}
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

	g.Expect(err).To(MatchError(ContainSubstring(ociJobSecretStagingDirEnv + " is required when secretEnvironment is used")))
	g.Expect(findFakeDockerCommand(readFakeDockerCommands(t, argsFile), "run")).To(BeNil())
}

func TestExecuteOCIJobStepCleansStaleSecretStagingOnRestart(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	containerName := ociJobContainerName(validOCIJobInputs()["idempotencyKey"].(string))
	staleDir := filepath.Join(os.Getenv(ociJobSecretStagingDirEnv), ociJobSecretStagingDirPrefix(containerName)+"stale")
	g.Expect(os.Mkdir(staleDir, 0o700)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(staleDir, "env"), []byte("API_TOKEN='old-secret-token'\n"), 0o400)).To(Succeed())
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_RUN_OUTPUT", "job completed")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": "new-secret-token"}
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
	_, statErr := os.Stat(staleDir)
	g.Expect(os.IsNotExist(statErr)).To(BeTrue())
	g.Expect(findFakeDockerCommand(readFakeDockerCommands(t, argsFile), "run")).NotTo(BeNil())
}

func TestExecuteOCIJobStepDoesNotDeleteConcurrentSecretStaging(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_RUN_OUTPUT", "job completed")
	t.Setenv("FAKE_DOCKER_RUN_SLEEP_MS", "1500")
	t.Setenv("FAKE_DOCKER_VERIFY_SECRET_FILE_AFTER_SLEEP", "1")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	makeStep := func(key, token string) api.AgentTaskLeaseStep {
		inputs := validOCIJobInputs()
		inputs["idempotencyKey"] = key
		inputs["secretEnvironment"] = map[string]any{"API_TOKEN": token}
		return api.AgentTaskLeaseStep{
			StepRunID:        uuid.New(),
			Key:              "cleanup",
			ActionType:       ociJobActionType,
			ActionVersion:    types.AgentActionVersionV1,
			Inputs:           inputs,
			SecretReferences: []string{"secret:job_api_token"},
			IdempotencyKey:   key,
		}
	}
	leaseA := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token-a"}
	stepA := makeStep("sha256:job-key-a", "first-secret-token")
	errA := make(chan error, 1)
	go func() {
		errA <- executeOCIJobStep(ctx, leaseA, stepA, &recordingLeasedTaskClient{})
	}()
	deadline := time.After(2 * time.Second)
	for {
		run := findFakeDockerCommand(readFakeDockerCommands(t, argsFile), "run")
		if run != nil {
			source := fakeDockerSecretEnvSourceFromRun(run)
			g.Expect(source).NotTo(Equal(""))
			_, err := os.Stat(source)
			g.Expect(err).ToNot(HaveOccurred())
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for first fake docker run")
		case <-time.After(10 * time.Millisecond):
		}
	}
	leaseB := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token-b"}
	stepB := makeStep("sha256:job-key-b", "second-secret-token")

	errB := executeOCIJobStep(ctx, leaseB, stepB, &recordingLeasedTaskClient{})
	firstErr := <-errA

	g.Expect(firstErr).ToNot(HaveOccurred())
	g.Expect(errB).ToNot(HaveOccurred())
	g.Expect(fakeDockerCommandCount(readFakeDockerCommands(t, argsFile), "run")).To(Equal(2))
}

func TestExecuteOCIJobStepUsesCanonicalMountSourceInDockerArgs(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, allowedRoot string) (mountSource string, canonicalSource string)
	}{
		{
			name: "cleaned path",
			setup: func(t *testing.T, allowedRoot string) (string, string) {
				t.Helper()
				g := NewWithT(t)
				actualSource := filepath.Join(allowedRoot, "actual")
				g.Expect(os.Mkdir(actualSource, 0o700)).To(Succeed())
				mountSource := actualSource + string(filepath.Separator) + "."
				canonicalSource, err := filepath.EvalSymlinks(mountSource)
				g.Expect(err).ToNot(HaveOccurred())
				return mountSource, canonicalSource
			},
		},
		{
			name: "symlink path",
			setup: func(t *testing.T, allowedRoot string) (string, string) {
				t.Helper()
				g := NewWithT(t)
				actualSource := filepath.Join(allowedRoot, "actual")
				linkSource := filepath.Join(allowedRoot, "link")
				g.Expect(os.Mkdir(actualSource, 0o700)).To(Succeed())
				createOCIJobTestSymlink(t, actualSource, linkSource)
				canonicalSource, err := filepath.EvalSymlinks(linkSource)
				g.Expect(err).ToNot(HaveOccurred())
				return linkSource, canonicalSource
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			setOCIJobPolicyEnv(t)
			ctx := context.Background()
			argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
			allowedRoot := t.TempDir()
			mountSource, canonicalSource := tt.setup(t, allowedRoot)
			if mountSource == canonicalSource {
				t.Skip("mount source is already canonical on this platform")
			}
			t.Setenv(ociJobAllowedMountRootsEnv, allowedRoot)
			t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
			t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
			t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
			t.Setenv("FAKE_DOCKER_RUN_OUTPUT", "job completed")
			oldExecCommandContext := execCommandContext
			execCommandContext = fakeDockerCommandContext
			t.Cleanup(func() { execCommandContext = oldExecCommandContext })
			inputs := validOCIJobInputs()
			inputs["volumes"] = []any{
				map[string]any{"source": mountSource, "target": "/input", "readOnly": true},
			}
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
			run := findFakeDockerCommand(readFakeDockerCommands(t, argsFile), "run")
			g.Expect(run).NotTo(BeNil())
			g.Expect(run).To(ContainElement(canonicalSource + ":/input:ro"))
			g.Expect(run).NotTo(ContainElement(mountSource + ":/input:ro"))
		})
	}
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
	inputs := validOCIJobInputs()
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": secretValue}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
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
	g.Expect(findFakeDockerCommand(commands, "logs")).To(BeNil())
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "status",
		Value: "reused existing OCI job container with exit code 0",
	}))
}

func TestExecuteOCIJobStepDoesNotReplayOldSecretLogsFromExistingContainer(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_LOGS", "old-secret-token")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": "new-secret-token"}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
		SecretReferences: []string{"secret:job_api_token"},
		IdempotencyKey:   "sha256:job-key",
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
	g.Expect(findFakeDockerCommand(commands, "logs")).To(BeNil())
	outputs, err := json.Marshal(recorder.events[2].Outputs)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(outputs)).NotTo(ContainSubstring("old-secret-token"))
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "status",
		Value: "reused existing OCI job container with exit code 0",
	}))
}

func TestExecuteOCIJobStepDeletesMountedSecretStagingAfterCrashReclaim(t *testing.T) {
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
	inputs := validOCIJobInputs()
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": "new-secret-token"}
	containerName := ociJobContainerName(inputs["idempotencyKey"].(string))
	secretDir, secretSource := writeFakeOCIJobSecretStaging(t, containerName, "crashed")
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
		SecretReferences: []string{"secret:job_api_token"},
		IdempotencyKey:   "sha256:job-key",
	}
	t.Setenv("FAKE_DOCKER_EXISTING_INSPECT", fakeOCIJobInspectJSONWithMounts(t, lease, step, map[string]any{
		"Status":   "exited",
		"Running":  false,
		"ExitCode": 0,
	}, []map[string]any{{
		"Source":      secretSource,
		"Destination": ociJobSecretEnvMountPath,
	}}))
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	_, statErr := os.Stat(secretDir)
	g.Expect(os.IsNotExist(statErr)).To(BeTrue())
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "run")).To(BeNil())
	g.Expect(findFakeDockerCommand(commands, "logs")).To(BeNil())
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "status",
		Value: "reused existing OCI job container with exit code 0",
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
	g.Expect(findFakeDockerCommand(commands, "logs")).To(BeNil())
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "status",
		Value: "reused existing OCI job container with exit code 0",
	}))
}

func TestExecuteOCIJobStepStartsExistingCreatedContainerAfterRestart(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_WAIT_EXIT_CODE", "0")
	t.Setenv("FAKE_DOCKER_LOGS", "created container recovered job output")
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
		"Status":   "created",
		"Running":  false,
		"ExitCode": 0,
	}))
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "run")).To(BeNil())
	g.Expect(findFakeDockerCommand(commands, "inspect")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "start")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "wait")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "logs")).To(BeNil())
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "status",
		Value: "reused existing OCI job container with exit code 0",
	}))
}

func TestExecuteOCIJobStepRecreatesCreatedContainerWhenSecretMountMissing(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	missingSecretSource := filepath.Join(os.Getenv(ociJobSecretStagingDirEnv), "distr-oci-job-secret-env-missing", "env")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_RUN_OUTPUT", "job completed")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": "new-secret-token"}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:      uuid.New(),
		Key:            "cleanup",
		ActionType:     ociJobActionType,
		ActionVersion:  types.AgentActionVersionV1,
		Inputs:         inputs,
		IdempotencyKey: "sha256:job-key",
	}
	t.Setenv("FAKE_DOCKER_EXISTING_INSPECT", fakeOCIJobInspectJSONWithMounts(t, lease, step, map[string]any{
		"Status":   "created",
		"Running":  false,
		"ExitCode": 0,
	}, []map[string]any{{
		"Source":      missingSecretSource,
		"Destination": ociJobSecretEnvMountPath,
	}}))
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "rm")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "start")).To(BeNil())
	g.Expect(findFakeDockerCommand(commands, "run")).NotTo(BeNil())
}

func TestExecuteOCIJobStepRejectsExistingContainerInUnsupportedState(t *testing.T) {
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
	t.Setenv("FAKE_DOCKER_EXISTING_INSPECT", fakeOCIJobInspectJSON(t, lease, step, map[string]any{
		"Status":   "paused",
		"Running":  true,
		"ExitCode": 0,
	}))
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring(`existing OCI job container is in unsupported state "paused"`)))
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "run")).To(BeNil())
	g.Expect(findFakeDockerCommand(commands, "start")).To(BeNil())
	g.Expect(findFakeDockerCommand(commands, "wait")).To(BeNil())
	g.Expect(findFakeDockerCommand(commands, "logs")).To(BeNil())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
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
	t.Setenv("FAKE_DOCKER_STOP_STATE_FILE", filepath.Join(t.TempDir(), "stop-state"))
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
	t.Setenv("FAKE_DOCKER_STOP_STATE_FILE", filepath.Join(t.TempDir(), "stop-state"))
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["timeoutSeconds"] = 1
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": "new-secret-token"}
	containerName := ociJobContainerName(inputs["idempotencyKey"].(string))
	secretDir, secretSource := writeFakeOCIJobSecretStaging(t, containerName, "timeout")
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
		SecretReferences: []string{"secret:job_api_token"},
		IdempotencyKey:   "sha256:job-key",
	}
	t.Setenv("FAKE_DOCKER_EXISTING_INSPECT", fakeOCIJobInspectJSONWithMounts(t, lease, step, map[string]any{
		"Status":   "running",
		"Running":  true,
		"ExitCode": 0,
	}, []map[string]any{{
		"Source":      secretSource,
		"Destination": ociJobSecretEnvMountPath,
	}}))
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("OCI job timed out")))
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "wait")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "stop")).NotTo(BeNil())
	_, statErr := os.Stat(secretDir)
	g.Expect(os.IsNotExist(statErr)).To(BeTrue())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteOCIJobStepCleansMountedSecretStagingOnExistingCreatedTimeout(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_WAIT_SLEEP_MS", "1500")
	t.Setenv("FAKE_DOCKER_STOP_STATE_FILE", filepath.Join(t.TempDir(), "stop-state"))
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["timeoutSeconds"] = 1
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": "new-secret-token"}
	containerName := ociJobContainerName(inputs["idempotencyKey"].(string))
	secretDir, secretSource := writeFakeOCIJobSecretStaging(t, containerName, "created-timeout")
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
		SecretReferences: []string{"secret:job_api_token"},
		IdempotencyKey:   "sha256:job-key",
	}
	t.Setenv("FAKE_DOCKER_EXISTING_INSPECT", fakeOCIJobInspectJSONWithMounts(t, lease, step, map[string]any{
		"Status":   "created",
		"Running":  false,
		"ExitCode": 0,
	}, []map[string]any{{
		"Source":      secretSource,
		"Destination": ociJobSecretEnvMountPath,
	}}))
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("OCI job timed out")))
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "start")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "wait")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "stop")).NotTo(BeNil())
	_, statErr := os.Stat(secretDir)
	g.Expect(os.IsNotExist(statErr)).To(BeTrue())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteOCIJobStepCleansMountedSecretStagingOnExistingRunningCancellation(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_WAIT_SLEEP_MS", "1500")
	t.Setenv("FAKE_DOCKER_STOP_STATE_FILE", filepath.Join(t.TempDir(), "stop-state"))
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": "new-secret-token"}
	containerName := ociJobContainerName(inputs["idempotencyKey"].(string))
	secretDir, secretSource := writeFakeOCIJobSecretStaging(t, containerName, "canceled")
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
		SecretReferences: []string{"secret:job_api_token"},
		IdempotencyKey:   "sha256:job-key",
	}
	t.Setenv("FAKE_DOCKER_EXISTING_INSPECT", fakeOCIJobInspectJSONWithMounts(t, lease, step, map[string]any{
		"Status":   "running",
		"Running":  true,
		"ExitCode": 0,
	}, []map[string]any{{
		"Source":      secretSource,
		"Destination": ociJobSecretEnvMountPath,
	}}))
	recorder := &recordingLeasedTaskClient{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- executeOCIJobStep(ctx, lease, step, recorder)
	}()
	deadline := time.After(2 * time.Second)
	for findFakeDockerCommand(readFakeDockerCommands(t, argsFile), "wait") == nil {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for fake docker wait")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	err := <-errCh

	g.Expect(err).To(MatchError(ContainSubstring("OCI job canceled")))
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "wait")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "stop")).NotTo(BeNil())
	_, statErr := os.Stat(secretDir)
	g.Expect(os.IsNotExist(statErr)).To(BeTrue())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteOCIJobStepReportsFreshRunStopFailureAndKeepsSecretStaging(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_RUN_SLEEP_MS", "1500")
	t.Setenv("FAKE_DOCKER_STOP_EXIT_CODE", "1")
	t.Setenv("FAKE_DOCKER_KILL_EXIT_CODE", "1")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["timeoutSeconds"] = 1
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": "new-secret-token"}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
		SecretReferences: []string{"secret:job_api_token"},
		IdempotencyKey:   "sha256:job-key",
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).To(MatchError(And(ContainSubstring("OCI job timed out"), ContainSubstring("stop OCI job container"))))
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "run")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "stop")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "kill")).NotTo(BeNil())
	secretSource := fakeDockerSecretEnvSourceFromRun(findFakeDockerCommand(commands, "run"))
	g.Expect(secretSource).NotTo(Equal(""))
	_, statErr := os.Stat(secretSource)
	g.Expect(statErr).ToNot(HaveOccurred())
}

func TestExecuteOCIJobStepReportsReclaimStopFailureAndKeepsSecretStaging(t *testing.T) {
	g := NewWithT(t)
	setOCIJobPolicyEnv(t)
	ctx := context.Background()
	argsFile := filepath.Join(t.TempDir(), "docker-commands.jsonl")
	t.Setenv("GO_WANT_DOCKER_COMMAND_HELPER", "1")
	t.Setenv("DISTR_AGENT_VERSION_ID", uuid.NewString())
	t.Setenv("FAKE_DOCKER_COMMAND_ARGS_FILE", argsFile)
	t.Setenv("FAKE_DOCKER_WAIT_SLEEP_MS", "1500")
	t.Setenv("FAKE_DOCKER_STOP_EXIT_CODE", "1")
	t.Setenv("FAKE_DOCKER_KILL_EXIT_CODE", "1")
	oldExecCommandContext := execCommandContext
	execCommandContext = fakeDockerCommandContext
	t.Cleanup(func() { execCommandContext = oldExecCommandContext })
	inputs := validOCIJobInputs()
	inputs["timeoutSeconds"] = 1
	inputs["secretEnvironment"] = map[string]any{"API_TOKEN": "new-secret-token"}
	containerName := ociJobContainerName(inputs["idempotencyKey"].(string))
	secretDir, secretSource := writeFakeOCIJobSecretStaging(t, containerName, "stop-failure")
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "cleanup",
		ActionType:       ociJobActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
		SecretReferences: []string{"secret:job_api_token"},
		IdempotencyKey:   "sha256:job-key",
	}
	t.Setenv("FAKE_DOCKER_EXISTING_INSPECT", fakeOCIJobInspectJSONWithMounts(t, lease, step, map[string]any{
		"Status":   "running",
		"Running":  true,
		"ExitCode": 0,
	}, []map[string]any{{
		"Source":      secretSource,
		"Destination": ociJobSecretEnvMountPath,
	}}))
	recorder := &recordingLeasedTaskClient{}

	err := executeOCIJobStep(ctx, lease, step, recorder)

	g.Expect(err).To(MatchError(And(ContainSubstring("OCI job timed out"), ContainSubstring("stop OCI job container"))))
	commands := readFakeDockerCommands(t, argsFile)
	g.Expect(findFakeDockerCommand(commands, "wait")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "stop")).NotTo(BeNil())
	g.Expect(findFakeDockerCommand(commands, "kill")).NotTo(BeNil())
	_, statErr := os.Stat(secretDir)
	g.Expect(statErr).ToNot(HaveOccurred())
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
	t.Setenv("FAKE_DOCKER_STOP_STATE_FILE", filepath.Join(t.TempDir(), "stop-state"))
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
			"MODE": "once",
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
	t.Setenv(ociJobSecretStagingDirEnv, t.TempDir())
}

func writeFakeOCIJobSecretStaging(t *testing.T, containerName, suffix string) (string, string) {
	t.Helper()
	dir := filepath.Join(os.Getenv(ociJobSecretStagingDirEnv), ociJobSecretStagingDirPrefix(containerName)+suffix)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("create fake OCI job secret staging dir: %v", err)
	}
	source := filepath.Join(dir, "env")
	if err := os.WriteFile(source, []byte("API_TOKEN='old-secret-token'\n"), 0o444); err != nil {
		t.Fatalf("write fake OCI job secret staging file: %v", err)
	}
	return dir, source
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

func fakeDockerCommandCount(commands [][]string, name string) int {
	count := 0
	for _, command := range commands {
		if len(command) >= 2 && command[0] == "docker" && command[1] == name {
			count++
		}
	}
	return count
}

func fakeDockerSecretEnvSourceFromRun(command []string) string {
	suffix := ":" + ociJobSecretEnvMountPath + ":ro"
	for index, arg := range command {
		if arg != "--volume" || index+1 >= len(command) {
			continue
		}
		bind := command[index+1]
		if strings.HasSuffix(bind, suffix) {
			return strings.TrimSuffix(bind, suffix)
		}
	}
	return ""
}

func indexOfFakeDockerArg(command []string, value string) int {
	for index, arg := range command {
		if arg == value {
			return index
		}
	}
	return -1
}

func createOCIJobTestSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
}

func fakeOCIJobInspectJSON(
	t *testing.T,
	lease api.AgentTaskLease,
	step api.AgentTaskLeaseStep,
	state map[string]any,
) string {
	return fakeOCIJobInspectJSONWithMounts(t, lease, step, state, nil)
}

func fakeOCIJobInspectJSONWithMounts(
	t *testing.T,
	lease api.AgentTaskLease,
	step api.AgentTaskLeaseStep,
	state map[string]any,
	mounts []map[string]any,
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
		"State":  state,
		"Mounts": mounts,
		"Config": map[string]any{
			"Labels": ociJobExpectedLabels(identity),
		},
	})
	if err != nil {
		t.Fatalf("encode fake inspect: %v", err)
	}
	return string(data)
}
