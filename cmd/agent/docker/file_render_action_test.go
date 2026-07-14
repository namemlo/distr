package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestFileRenderActionInputRejectsUnsafeTemplatesAndPaths(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T, map[string]any)
		message string
	}{
		{
			name: "missing allowlisted root",
			setup: func(t *testing.T, _ map[string]any) {
				t.Setenv(fileRenderAllowedRootsEnv, "")
			},
			message: fileRenderAllowedRootsEnv + " is required",
		},
		{
			name: "absolute destination",
			setup: func(t *testing.T, inputs map[string]any) {
				setFileRenderPolicyEnv(t)
				inputs["destinationPath"] = filepath.Join(string(filepath.Separator), "etc", "passwd")
			},
			message: "destinationPath must be relative",
		},
		{
			name: "traversal destination",
			setup: func(t *testing.T, inputs map[string]any) {
				setFileRenderPolicyEnv(t)
				inputs["destinationPath"] = filepath.Join("..", "outside.env")
			},
			message: "destinationPath cannot contain traversal",
		},
		{
			name: "unsupported template language",
			setup: func(t *testing.T, inputs map[string]any) {
				setFileRenderPolicyEnv(t)
				inputs["template"] = "{{ .Env.SECRET }}"
			},
			message: "unsupported template syntax",
		},
		{
			name: "missing variable",
			setup: func(t *testing.T, inputs map[string]any) {
				setFileRenderPolicyEnv(t)
				inputs["template"] = "PORT=${missing}\n"
			},
			message: `template variable "missing" is not provided`,
		},
		{
			name: "variables conflict with secret variables",
			setup: func(t *testing.T, inputs map[string]any) {
				setFileRenderPolicyEnv(t)
				inputs["variables"] = map[string]any{"apiToken": "public"}
				inputs["secretVariables"] = map[string]any{"apiToken": "secret-token"}
			},
			message: "variables conflict with secretVariables",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			inputs := validFileRenderInputs()
			tt.setup(t, inputs)

			_, err := decodeFileRenderActionInput(inputs)

			g.Expect(err).To(MatchError(ContainSubstring(tt.message)))
		})
	}
}

func TestExecuteFileRenderStepWritesAtomicallyBacksUpAndRedactsSecrets(t *testing.T) {
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	destination := filepath.Join(root, "app", "config", "runtime.env")
	g.Expect(os.MkdirAll(filepath.Dir(destination), 0o700)).To(Succeed())
	g.Expect(os.WriteFile(destination, []byte("old=true\n"), 0o644)).To(Succeed())
	ctx := context.Background()
	const secretValue = "super-secret-render-token"
	inputs := validFileRenderInputs()
	inputs["secretVariables"] = map[string]any{"apiToken": secretValue}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:        uuid.New(),
		Key:              "render-config",
		ActionType:       fileRenderActionType,
		ActionVersion:    types.AgentActionVersionV1,
		Inputs:           inputs,
		SecretReferences: []string{"secret:api_token"},
		IdempotencyKey:   "sha256:file-render",
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeFileRenderStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	rendered, err := os.ReadFile(destination)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(rendered)).To(Equal("API_URL=https://api.example.com\nAPI_TOKEN=" + secretValue + "\n"))
	backup, err := os.ReadFile(destination + ".bak")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(backup)).To(Equal("old=true\n"))
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(destination)
		g.Expect(statErr).ToNot(HaveOccurred())
		g.Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o640)))
	}
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeSucceeded,
	}))
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "destinationPath",
		Value: destination,
	}))
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "changed",
		Value: true,
	}))
	payload, err := json.Marshal(recorder.events)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(payload)).NotTo(ContainSubstring(secretValue))
	assertNoFileRenderTempFiles(t, filepath.Dir(destination))
}

func TestExecuteFileRenderStepNoopsWhenRenderedContentAlreadyExists(t *testing.T) {
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	destination := filepath.Join(root, "app", "config", "runtime.env")
	g.Expect(os.MkdirAll(filepath.Dir(destination), 0o700)).To(Succeed())
	g.Expect(os.WriteFile(destination, []byte("API_URL=https://api.example.com\nAPI_TOKEN=super-secret-render-token\n"), 0o640)).To(Succeed())
	ctx := context.Background()
	inputs := validFileRenderInputs()
	inputs["secretVariables"] = map[string]any{"apiToken": "super-secret-render-token"}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeFileRenderStep(ctx, lease, step, recorder)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(destination + ".bak").NotTo(BeAnExistingFile())
	g.Expect(recorder.events[2].Outputs).To(ContainElement(api.AgentStepRunOutputRequest{
		Name:  "changed",
		Value: false,
	}))
}

func TestExecuteFileRenderStepDefaultsSecretRenderedFilesAndBackupsToOwnerOnlyMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows chmod does not expose POSIX owner-only mode")
	}
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	destination := filepath.Join(root, "app", "config", "runtime.env")
	g.Expect(os.MkdirAll(filepath.Dir(destination), 0o700)).To(Succeed())
	g.Expect(os.WriteFile(destination, []byte("old-secret=true\n"), 0o644)).To(Succeed())
	inputs := validFileRenderInputs()
	delete(inputs, "mode")
	inputs["secretVariables"] = map[string]any{"apiToken": "super-secret-render-token"}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}

	err := executeFileRenderStep(context.Background(), lease, step, &recordingLeasedTaskClient{})

	g.Expect(err).ToNot(HaveOccurred())
	destinationInfo, err := os.Stat(destination)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(destinationInfo.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	backupInfo, err := os.Stat(destination + ".bak")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(backupInfo.Mode().Perm()).To(Equal(os.FileMode(0o600)))
}

func TestExecuteFileRenderStepDoesNotWidenExistingFileModeForBackup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows chmod does not expose POSIX owner-only mode")
	}
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	destination := filepath.Join(root, "app", "config", "runtime.env")
	g.Expect(os.MkdirAll(filepath.Dir(destination), 0o700)).To(Succeed())
	g.Expect(os.WriteFile(destination, []byte("OLD_SECRET=still-sensitive\n"), 0o600)).To(Succeed())
	inputs := validFileRenderInputs()
	inputs["template"] = "API_URL=${apiUrl}\n"
	delete(inputs, "secretVariables")
	delete(inputs, "mode")
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}

	err := executeFileRenderStep(context.Background(), lease, step, &recordingLeasedTaskClient{})

	g.Expect(err).ToNot(HaveOccurred())
	destinationInfo, err := os.Stat(destination)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(destinationInfo.Mode().Perm()).To(Equal(os.FileMode(0o644)))
	backupInfo, err := os.Stat(destination + ".bak")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(backupInfo.Mode().Perm()).To(Equal(os.FileMode(0o600)))
}

func TestExecuteFileRenderStepRejectsDestinationSwapBeforeBackupCopy(t *testing.T) {
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	destination := filepath.Join(root, "app", "config", "runtime.env")
	victim := filepath.Join(root, "app", "config", "victim.env")
	g.Expect(os.MkdirAll(filepath.Dir(destination), 0o700)).To(Succeed())
	g.Expect(os.WriteFile(destination, []byte("old=true\n"), 0o600)).To(Succeed())
	g.Expect(os.WriteFile(victim, []byte("victim=true\n"), 0o600)).To(Succeed())
	oldHook := fileRenderBeforeBackupForTest
	fileRenderBeforeBackupForTest = func(destination fileRenderDestination) {
		g.Expect(os.Remove(filepath.Join(destination.rootPath, destination.relativePath))).To(Succeed())
		createFileRenderTestSymlink(t, victim, filepath.Join(destination.rootPath, destination.relativePath))
	}
	t.Cleanup(func() { fileRenderBeforeBackupForTest = oldHook })
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        validFileRenderInputs(),
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeFileRenderStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("destinationPath cannot be a symlink")))
	g.Expect(destination + ".bak").NotTo(BeAnExistingFile())
	g.Expect(string(mustReadFile(t, victim))).To(Equal("victim=true\n"))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteFileRenderStepRejectsDestinationSwapBeforeNoopMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow replacing an open file for this race test")
	}
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	destination := filepath.Join(root, "app", "config", "runtime.env")
	victim := filepath.Join(root, "app", "config", "victim.env")
	rendered := []byte("API_URL=https://api.example.com\nAPI_TOKEN=super-secret-render-token\n")
	g.Expect(os.MkdirAll(filepath.Dir(destination), 0o700)).To(Succeed())
	g.Expect(os.WriteFile(destination, rendered, 0o644)).To(Succeed())
	g.Expect(os.WriteFile(victim, []byte("victim=true\n"), 0o644)).To(Succeed())
	oldHook := fileRenderBeforeExistingMetadataForTest
	fileRenderBeforeExistingMetadataForTest = func(destination fileRenderDestination) {
		g.Expect(os.Remove(filepath.Join(destination.rootPath, destination.relativePath))).To(Succeed())
		createFileRenderTestSymlink(t, victim, filepath.Join(destination.rootPath, destination.relativePath))
	}
	t.Cleanup(func() { fileRenderBeforeExistingMetadataForTest = oldHook })
	inputs := validFileRenderInputs()
	inputs["secretVariables"] = map[string]any{"apiToken": "super-secret-render-token"}
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeFileRenderStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("destinationPath cannot be a symlink")))
	victimInfo, err := os.Stat(victim)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(victimInfo.Mode().Perm()).To(Equal(os.FileMode(0o644)))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteFileRenderStepRejectsSymlinkEscape(t *testing.T) {
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "runtime.env")
	g.Expect(os.WriteFile(outsideFile, []byte("outside=true\n"), 0o600)).To(Succeed())
	linkPath := filepath.Join(root, "runtime.env")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	inputs := validFileRenderInputs()
	inputs["destinationPath"] = "runtime.env"
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeFileRenderStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("destinationPath cannot be a symlink")))
	g.Expect(string(mustReadFile(t, outsideFile))).To(Equal("outside=true\n"))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteFileRenderStepRejectsParentPathSwapBeforeAtomicWrite(t *testing.T) {
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	outside := t.TempDir()
	parent := filepath.Join(root, "app", "config")
	g.Expect(os.MkdirAll(parent, 0o700)).To(Succeed())
	oldHook := fileRenderBeforeAtomicWriteForTest
	fileRenderBeforeAtomicWriteForTest = func(destination fileRenderDestination) {
		g.Expect(os.Rename(parent, parent+".moved")).To(Succeed())
		createFileRenderTestSymlink(t, outside, parent)
	}
	t.Cleanup(func() { fileRenderBeforeAtomicWriteForTest = oldHook })
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        validFileRenderInputs(),
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeFileRenderStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("destinationPath escapes allowlisted root")))
	g.Expect(filepath.Join(outside, "runtime.env")).NotTo(BeAnExistingFile())
	g.Expect(filepath.Join(outside, ".distr-file-render-write")).NotTo(BeAnExistingFile())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteFileRenderStepLeavesOriginalDestinationWhenTempMetadataFails(t *testing.T) {
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	destination := filepath.Join(root, "app", "config", "runtime.env")
	g.Expect(os.MkdirAll(filepath.Dir(destination), 0o700)).To(Succeed())
	g.Expect(os.WriteFile(destination, []byte("old=true\n"), 0o600)).To(Succeed())
	oldHook := fileRenderApplyMetadataForTest
	fileRenderApplyMetadataForTest = func(_ *os.File, _ os.FileInfo, _ os.FileMode, _, _ string) error {
		return fmt.Errorf("injected metadata failure")
	}
	t.Cleanup(func() { fileRenderApplyMetadataForTest = oldHook })
	inputs := validFileRenderInputs()
	inputs["backup"] = false
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeFileRenderStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("injected metadata failure")))
	g.Expect(string(mustReadFile(t, destination))).To(Equal("old=true\n"))
	assertNoFileRenderTempFiles(t, filepath.Dir(destination))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteFileRenderStepLeavesEqualContentMetadataUnchangedWhenTempMetadataFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows chmod does not expose POSIX mode rollback")
	}
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	destination := filepath.Join(root, "app", "config", "runtime.env")
	rendered := []byte("API_URL=https://api.example.com\nAPI_TOKEN=api_token\n")
	g.Expect(os.MkdirAll(filepath.Dir(destination), 0o700)).To(Succeed())
	g.Expect(os.WriteFile(destination, rendered, 0o600)).To(Succeed())
	oldHook := fileRenderApplyMetadataForTest
	fileRenderApplyMetadataForTest = func(file *os.File, _ os.FileInfo, mode os.FileMode, _, _ string) error {
		if err := file.Chmod(mode); err != nil {
			return err
		}
		return fmt.Errorf("injected chown failure")
	}
	t.Cleanup(func() { fileRenderApplyMetadataForTest = oldHook })
	inputs := validFileRenderInputs()
	inputs["backup"] = false
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeFileRenderStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("injected chown failure")))
	g.Expect(mustReadFile(t, destination)).To(Equal(rendered))
	destinationInfo, err := os.Stat(destination)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(destinationInfo.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	assertNoFileRenderTempFiles(t, filepath.Dir(destination))
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteFileRenderStepRejectsTemporaryDestinationSwapBeforeRename(t *testing.T) {
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	destination := filepath.Join(root, "app", "config", "runtime.env")
	g.Expect(os.MkdirAll(filepath.Dir(destination), 0o700)).To(Succeed())
	oldHook := fileRenderBeforeTempRenameForTest
	var tamperedPath string
	fileRenderBeforeTempRenameForTest = func(destination fileRenderDestination, tempRelativePath string) {
		if !strings.Contains(filepath.Base(tempRelativePath), "-write-") {
			return
		}
		tamperedPath = filepath.Join(destination.rootPath, tempRelativePath)
		if runtime.GOOS != "windows" {
			tempInfo, err := os.Stat(tamperedPath)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(tempInfo.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		}
		g.Expect(os.Remove(tamperedPath)).To(Succeed())
		g.Expect(os.WriteFile(tamperedPath, []byte("tampered=true\n"), 0o600)).To(Succeed())
	}
	t.Cleanup(func() { fileRenderBeforeTempRenameForTest = oldHook })
	inputs := validFileRenderInputs()
	inputs["mode"] = "0666"
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeFileRenderStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("temporary destination changed during file render")))
	g.Expect(destination).NotTo(BeAnExistingFile())
	g.Expect(tamperedPath).NotTo(BeAnExistingFile())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func TestExecuteFileRenderStepRejectsParentSymlinkEscapeBeforeCreatingOutsideDirectory(t *testing.T) {
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	outside := t.TempDir()
	linkPath := filepath.Join(root, "linked")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	inputs := validFileRenderInputs()
	inputs["destinationPath"] = filepath.Join("linked", "created-outside", "runtime.env")
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        inputs,
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeFileRenderStep(context.Background(), lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("destinationPath escapes allowlisted root")))
	g.Expect(filepath.Join(outside, "created-outside")).NotTo(BeADirectory())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeFailed,
	}))
}

func createFileRenderTestSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
}

func TestExecuteFileRenderStepHonorsCancellationBeforeWrite(t *testing.T) {
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	destination := filepath.Join(root, "app", "config", "runtime.env")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lease := api.AgentTaskLease{TaskID: uuid.New(), LeaseToken: "lease-token"}
	step := api.AgentTaskLeaseStep{
		StepRunID:     uuid.New(),
		Key:           "render-config",
		ActionType:    fileRenderActionType,
		ActionVersion: types.AgentActionVersionV1,
		Inputs:        validFileRenderInputs(),
	}
	recorder := &recordingLeasedTaskClient{}

	err := executeFileRenderStep(ctx, lease, step, recorder)

	g.Expect(err).To(MatchError(ContainSubstring("file render canceled")))
	g.Expect(destination).NotTo(BeAnExistingFile())
}

func TestExecuteTaskLeaseHeartbeatsAndRunsFileRenderStep(t *testing.T) {
	g := NewWithT(t)
	root := setFileRenderPolicyEnv(t)
	lease := api.AgentTaskLease{
		TaskID:     uuid.New(),
		LeaseToken: "lease-token",
		Steps: []api.AgentTaskLeaseStep{
			{
				StepRunID:     uuid.New(),
				Key:           "render-config",
				ActionType:    fileRenderActionType,
				ActionVersion: types.AgentActionVersionV1,
				Inputs:        validFileRenderInputs(),
			},
		},
	}
	recorder := &recordingLeasedTaskClient{}
	apply := func(_ context.Context, _ api.AgentDeployment, _ composeDeployOptions, _ func(string)) (*AgentDeployment, string, error) {
		t.Fatal("compose apply should not run for file render actions")
		return nil, "", nil
	}

	err := executeTaskLease(context.Background(), lease, recorder, apply)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(recorder.heartbeatTaskIDs).To(Equal([]uuid.UUID{lease.TaskID}))
	g.Expect(filepath.Join(root, "app", "config", "runtime.env")).To(BeAnExistingFile())
	g.Expect(eventTypes(recorder.events)).To(Equal([]types.StepRunEventType{
		types.StepRunEventTypeStarted,
		types.StepRunEventTypeProgress,
		types.StepRunEventTypeSucceeded,
	}))
}

func validFileRenderInputs() map[string]any {
	return map[string]any{
		"destinationPath": "app/config/runtime.env",
		"template":        "API_URL=${apiUrl}\nAPI_TOKEN=${secrets.apiToken}\n",
		"variables": map[string]any{
			"apiUrl": "https://api.example.com",
		},
		"secretVariables": map[string]any{
			"apiToken": "api_token",
		},
		"mode":           "0640",
		"backup":         true,
		"idempotencyKey": "runtime-config",
		"timeoutSeconds": 30,
	}
}

func setFileRenderPolicyEnv(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(fileRenderAllowedRootsEnv, root)
	return root
}

func assertNoFileRenderTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read render dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".distr-file-render-") {
			t.Fatalf("temporary render file was not cleaned up: %s", entry.Name())
		}
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
