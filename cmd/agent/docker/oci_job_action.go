package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/types"
)

var ociJobEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type ociJobActionInput struct {
	ImageDigest       string            `json:"imageDigest"`
	AllowedRegistries []string          `json:"allowedRegistries"`
	Command           []string          `json:"command"`
	Arguments         []string          `json:"arguments"`
	Environment       map[string]string `json:"environment"`
	SecretEnvironment map[string]string `json:"secretEnvironment"`
	Network           string            `json:"network"`
	AllowedNetworks   []string          `json:"allowedNetworks"`
	Volumes           []ociJobVolume    `json:"volumes"`
	AllowedMountRoots []string          `json:"allowedMountRoots"`
	TimeoutSeconds    int               `json:"timeoutSeconds"`
	ExpectedExitCodes []int             `json:"expectedExitCodes"`
	IdempotencyKey    string            `json:"idempotencyKey"`
	RunAsUser         string            `json:"runAsUser"`
	Resources         ociJobResources   `json:"resources"`
	Security          ociJobSecurity    `json:"security"`
}

type ociJobVolume struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly"`
}

type ociJobResources struct {
	CPUs        float64 `json:"cpus"`
	MemoryBytes int64   `json:"memoryBytes"`
}

type ociJobSecurity struct {
	Privileged              bool     `json:"privileged"`
	ReadOnlyRootFilesystem  *bool    `json:"readOnlyRootFilesystem"`
	DropCapabilities        []string `json:"dropCapabilities"`
	NoNewPrivilegesDisabled bool     `json:"noNewPrivilegesDisabled"`
}

type ociJobResult struct {
	ContainerName string
	ExitCode      int
	Status        string
}

type ociJobContainerState struct {
	Status   string `json:"Status"`
	Running  bool   `json:"Running"`
	ExitCode int    `json:"ExitCode"`
	Error    string `json:"Error"`
}

func decodeOCIJobActionInput(inputs map[string]any) (ociJobActionInput, error) {
	var input ociJobActionInput
	data, err := json.Marshal(inputs)
	if err != nil {
		return input, fmt.Errorf("encode OCI job inputs: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, fmt.Errorf("decode OCI job inputs: %w", err)
	}
	normalizeOCIJobActionInput(&input)
	if err := validateOCIJobActionInput(input); err != nil {
		return input, err
	}
	return input, nil
}

func normalizeOCIJobActionInput(input *ociJobActionInput) {
	input.ImageDigest = strings.TrimSpace(input.ImageDigest)
	input.Network = strings.TrimSpace(input.Network)
	if input.Network == "" {
		input.Network = "none"
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.RunAsUser = strings.TrimSpace(input.RunAsUser)
	input.AllowedRegistries = trimStringSlice(input.AllowedRegistries)
	input.AllowedNetworks = trimStringSlice(input.AllowedNetworks)
	if len(input.AllowedNetworks) == 0 {
		input.AllowedNetworks = []string{"none"}
	}
	input.AllowedMountRoots = trimStringSlice(input.AllowedMountRoots)
	input.Command = trimStringSlice(input.Command)
	if input.ExpectedExitCodes == nil {
		input.ExpectedExitCodes = []int{0}
	}
	if input.Environment == nil {
		input.Environment = map[string]string{}
	}
}

func validateOCIJobActionInput(input ociJobActionInput) error {
	registry, err := ociJobRegistryFromDigest(input.ImageDigest)
	if err != nil {
		return err
	}
	if !containsString(input.AllowedRegistries, registry) {
		return fmt.Errorf("image registry is not allowlisted")
	}
	if len(input.Command) == 0 {
		return fmt.Errorf("command is required")
	}
	if len(input.SecretEnvironment) > 0 {
		return fmt.Errorf("secretEnvironment must be resolved by the task lease")
	}
	if !containsString(input.AllowedNetworks, input.Network) {
		return fmt.Errorf("network is not allowlisted")
	}
	for name, value := range input.Environment {
		if !ociJobEnvNamePattern.MatchString(name) {
			return fmt.Errorf("environment variable name %q is invalid", name)
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("environment variable %q contains unsupported characters", name)
		}
	}
	for _, volume := range input.Volumes {
		if !volume.ReadOnly {
			return fmt.Errorf("volumes must be read-only")
		}
		if !ociJobMountUnderAllowedRoot(volume.Source, input.AllowedMountRoots) {
			return fmt.Errorf("volume source is not under an allowlisted mount root")
		}
	}
	if input.Security.Privileged {
		return fmt.Errorf("privileged mode is not supported")
	}
	if input.Security.NoNewPrivilegesDisabled {
		return fmt.Errorf("no-new-privileges cannot be disabled")
	}
	if input.Security.ReadOnlyRootFilesystem != nil && !*input.Security.ReadOnlyRootFilesystem {
		return fmt.Errorf("read-only root filesystem cannot be disabled")
	}
	for _, capability := range input.Security.DropCapabilities {
		if strings.TrimSpace(capability) == "" {
			return fmt.Errorf("dropCapabilities cannot contain empty values")
		}
	}
	if input.TimeoutSeconds < 0 {
		return fmt.Errorf("timeoutSeconds must be greater than or equal to 0")
	}
	if len(input.ExpectedExitCodes) == 0 {
		return fmt.Errorf("expectedExitCodes cannot be empty")
	}
	for _, code := range input.ExpectedExitCodes {
		if code < 0 || code > 255 {
			return fmt.Errorf("expectedExitCodes must be between 0 and 255")
		}
	}
	return nil
}

func ociJobRegistryFromDigest(imageDigest string) (string, error) {
	if strings.ContainsAny(imageDigest, " \t\r\n") {
		return "", fmt.Errorf("imageDigest must be an immutable sha256 digest reference")
	}
	parts := strings.Split(imageDigest, "@sha256:")
	if len(parts) != 2 || len(parts[1]) != 64 {
		return "", fmt.Errorf("imageDigest must be an immutable sha256 digest reference")
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", fmt.Errorf("imageDigest must be an immutable sha256 digest reference")
	}
	name := parts[0]
	slash := strings.IndexByte(name, '/')
	if slash <= 0 {
		return "", fmt.Errorf("imageDigest must include an explicit registry")
	}
	return name[:slash], nil
}

func executeOCIJobStep(
	ctx context.Context,
	lease api.AgentTaskLease,
	step api.AgentTaskLeaseStep,
	client leasedTaskClient,
) error {
	sequence := int64(1)
	if err := recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeStarted, "starting OCI job", nil, nil); err != nil {
		return err
	}
	secretValues := []string(nil)
	recordFailure := func(err error, status string) error {
		sequence++
		redactedErr := redactErrorWithSecretValues(err, secretValues)
		logs := []api.AgentStepRunLogChunkRequest(nil)
		if strings.TrimSpace(status) != "" {
			logs = []api.AgentStepRunLogChunkRequest{{
				Stream:   types.StepRunLogStreamStderr,
				Severity: types.StepRunLogSeverityError,
				Body:     status,
			}}
		}
		if recordErr := recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeFailed, redactedErr.Error(), logs, nil, secretValues...); recordErr != nil {
			return redactErrorWithSecretValues(recordErr, secretValues)
		}
		return redactedErr
	}
	if step.ActionType != ociJobActionType {
		return recordFailure(fmt.Errorf("unsupported actionType %q", step.ActionType), "")
	}
	if step.ActionVersion != types.AgentActionVersionV1 {
		return recordFailure(fmt.Errorf("unsupported actionVersion %q", step.ActionVersion), "")
	}
	input, err := decodeOCIJobActionInput(step.Inputs)
	if err != nil {
		return recordFailure(err, "")
	}
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = strings.TrimSpace(step.IdempotencyKey)
	}
	if input.IdempotencyKey == "" {
		return recordFailure(fmt.Errorf("idempotencyKey is required"), "")
	}
	secretValues = ociJobSecretValues(input)
	containerName := ociJobContainerName(input.IdempotencyKey)
	jobCtx, jobCancel := context.WithCancel(ctx)
	if input.TimeoutSeconds > 0 {
		jobCtx, jobCancel = context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	}
	defer jobCancel()
	heartbeatErrCh, stopHeartbeat := startTaskLeaseHeartbeat(jobCtx, lease, client, jobCancel)

	var progressErr error
	var progressErrMu sync.Mutex
	updateStatus := func(status string) {
		status = strings.TrimSpace(status)
		if status == "" {
			return
		}
		progressErrMu.Lock()
		defer progressErrMu.Unlock()
		if progressErr != nil {
			return
		}
		sequence++
		progressErr = recordStepEvent(jobCtx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeProgress, status, nil, nil, secretValues...)
		if progressErr != nil {
			jobCancel()
		}
	}
	result, err := runOCIJob(jobCtx, input, containerName, secretValues, updateStatus)
	stopHeartbeat()
	progressErrMu.Lock()
	callbackErr := progressErr
	progressErrMu.Unlock()
	if callbackErr != nil {
		return redactErrorWithSecretValues(callbackErr, secretValues)
	}
	if heartbeatErr := taskLeaseHeartbeatError(heartbeatErrCh); heartbeatErr != nil {
		return recordFailure(heartbeatErr, "")
	}
	if err != nil {
		return recordFailure(err, result.Status)
	}
	sequence++
	outputs := []api.AgentStepRunOutputRequest{
		{Name: "containerName", Value: result.ContainerName},
		{Name: "exitCode", Value: result.ExitCode},
		{Name: "status", Value: result.Status},
	}
	return recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeSucceeded, "OCI job succeeded", nil, outputs, secretValues...)
}

func runOCIJob(
	ctx context.Context,
	input ociJobActionInput,
	containerName string,
	secretValues []string,
	updateStatus func(string),
) (ociJobResult, error) {
	result := ociJobResult{ContainerName: containerName, ExitCode: -1}
	if updateStatus != nil {
		updateStatus("starting OCI job container")
	}
	if state, exists, err := inspectOCIJobContainer(ctx, containerName); err != nil {
		return result, err
	} else if exists {
		return finishExistingOCIJobContainer(ctx, containerName, state, input.ExpectedExitCodes, secretValues)
	}
	envFile, cleanup, err := writeOCIJobEnvFile(input.Environment)
	if err != nil {
		return result, err
	}
	defer cleanup()
	cmd := execCommandContext(ctx, "docker", dockerRunOCIJobArgs(input, containerName, envFile)...)
	out, runErr := cmd.CombinedOutput()
	status := redactStringWithSecretValues(strings.TrimSpace(string(out)), secretValues)
	result.Status = status
	result.ExitCode = dockerCommandExitCode(runErr)
	if ctxErr := ctx.Err(); ctxErr != nil {
		stopOCIJobContainer(containerName)
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return result, fmt.Errorf("OCI job timed out")
		}
		return result, fmt.Errorf("OCI job canceled")
	}
	if runErr != nil && result.ExitCode < 0 {
		redactedErr := redactErrorWithSecretValues(runErr, secretValues)
		return result, fmt.Errorf("run OCI job container: %w: %s", redactedErr, status)
	}
	if !ociJobExpectedExitCode(result.ExitCode, input.ExpectedExitCodes) {
		return result, fmt.Errorf("OCI job exited with code %d: %s", result.ExitCode, status)
	}
	if result.Status == "" {
		result.Status = fmt.Sprintf("job exited with code %d", result.ExitCode)
	}
	return result, nil
}

func inspectOCIJobContainer(ctx context.Context, containerName string) (ociJobContainerState, bool, error) {
	cmd := execCommandContext(ctx, "docker", "inspect", "--format", "{{json .State}}", "--type", "container", containerName)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(strings.ToLower(text), "no such") || strings.Contains(strings.ToLower(text), "not found") {
			return ociJobContainerState{}, false, nil
		}
		return ociJobContainerState{}, false, fmt.Errorf("inspect OCI job container: %w: %s", err, text)
	}
	var state ociJobContainerState
	stateJSON := strings.TrimSpace(string(out))
	if start := strings.IndexByte(stateJSON, '{'); start >= 0 {
		stateJSON = stateJSON[start:]
	}
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return ociJobContainerState{}, false, fmt.Errorf("decode OCI job container state: %w", err)
	}
	return state, true, nil
}

func finishExistingOCIJobContainer(
	ctx context.Context,
	containerName string,
	state ociJobContainerState,
	expectedExitCodes []int,
	secretValues []string,
) (ociJobResult, error) {
	result := ociJobResult{ContainerName: containerName, ExitCode: state.ExitCode}
	if state.Running {
		code, err := waitOCIJobContainer(ctx, containerName)
		if err != nil {
			return result, err
		}
		result.ExitCode = code
	}
	logs, err := readOCIJobContainerLogs(ctx, containerName, secretValues)
	if err != nil {
		return result, err
	}
	result.Status = logs
	if !ociJobExpectedExitCode(result.ExitCode, expectedExitCodes) {
		return result, fmt.Errorf("OCI job exited with code %d: %s", result.ExitCode, result.Status)
	}
	if result.Status == "" {
		result.Status = fmt.Sprintf("job exited with code %d", result.ExitCode)
	}
	return result, nil
}

func waitOCIJobContainer(ctx context.Context, containerName string) (int, error) {
	cmd := execCommandContext(ctx, "docker", "wait", containerName)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return -1, fmt.Errorf("wait for OCI job container: %w: %s", err, text)
	}
	code, err := strconv.Atoi(text)
	if err != nil {
		return -1, fmt.Errorf("decode OCI job exit code %q: %w", text, err)
	}
	return code, nil
}

func readOCIJobContainerLogs(ctx context.Context, containerName string, secretValues []string) (string, error) {
	cmd := execCommandContext(ctx, "docker", "logs", containerName)
	out, err := cmd.CombinedOutput()
	text := redactStringWithSecretValues(strings.TrimSpace(string(out)), secretValues)
	if err != nil {
		redactedErr := redactErrorWithSecretValues(err, secretValues)
		return "", fmt.Errorf("read OCI job container logs: %w: %s", redactedErr, text)
	}
	return text, nil
}

func stopOCIJobContainer(containerName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = execCommandContext(ctx, "docker", "stop", "--time", "5", containerName).CombinedOutput()
}

func dockerRunOCIJobArgs(input ociJobActionInput, containerName, envFile string) []string {
	args := []string{
		"run",
		"--name", containerName,
		"--network", input.Network,
		"--read-only",
		"--security-opt", "no-new-privileges",
		"--cap-drop", "ALL",
		"--label", "distr.action=distr.oci.job",
		"--label", "distr.idempotencyKey=" + input.IdempotencyKey,
	}
	if envFile != "" {
		args = append(args, "--env-file", envFile)
	}
	for _, volume := range input.Volumes {
		args = append(args, "--volume", volume.Source+":"+volume.Target+":ro")
	}
	if input.RunAsUser != "" {
		args = append(args, "--user", input.RunAsUser)
	}
	if input.Resources.CPUs > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(input.Resources.CPUs, 'f', -1, 64))
	}
	if input.Resources.MemoryBytes > 0 {
		args = append(args, "--memory", strconv.FormatInt(input.Resources.MemoryBytes, 10))
	}
	args = append(args, input.ImageDigest)
	args = append(args, input.Command...)
	args = append(args, input.Arguments...)
	return args
}

func writeOCIJobEnvFile(environment map[string]string) (string, func(), error) {
	if len(environment) == 0 {
		return "", func() {}, nil
	}
	file, err := os.CreateTemp("", "distr-oci-job-env-*")
	if err != nil {
		return "", nil, fmt.Errorf("create OCI job env file: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(file.Name())
	}
	names := make([]string, 0, len(environment))
	for name := range environment {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := fmt.Fprintf(file, "%s=%s\n", name, environment[name]); err != nil {
			_ = file.Close()
			cleanup()
			return "", nil, fmt.Errorf("write OCI job env file: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close OCI job env file: %w", err)
	}
	return file.Name(), cleanup, nil
}

func ociJobSecretValues(input ociJobActionInput) []string {
	values := make([]string, 0, len(input.Environment))
	for _, value := range input.Environment {
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func ociJobContainerName(idempotencyKey string) string {
	sum := sha256.Sum256([]byte(idempotencyKey))
	return "distr-job-" + hex.EncodeToString(sum[:])[:24]
}

func dockerCommandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func ociJobExpectedExitCode(code int, expected []int) bool {
	for _, item := range expected {
		if code == item {
			return true
		}
	}
	return false
}

func ociJobMountUnderAllowedRoot(source string, roots []string) bool {
	if len(roots) == 0 {
		return false
	}
	source = cleanMountPath(source)
	if source == "" {
		return false
	}
	for _, root := range roots {
		root = cleanMountPath(root)
		if root == "" {
			continue
		}
		prefix := strings.TrimRight(root, "/") + "/"
		if source == root || strings.HasPrefix(source, prefix) {
			return true
		}
	}
	return false
}

func cleanMountPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = path.Clean(strings.ReplaceAll(value, "\\", "/"))
	if value == "." {
		return ""
	}
	return value
}

func trimStringSlice(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
