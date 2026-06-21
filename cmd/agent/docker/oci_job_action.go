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
	"path/filepath"
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

const (
	ociJobAllowedRegistriesEnv = "DISTR_OCI_JOB_ALLOWED_REGISTRIES"
	ociJobAllowedNetworksEnv   = "DISTR_OCI_JOB_ALLOWED_NETWORKS"
	ociJobAllowedMountRootsEnv = "DISTR_OCI_JOB_ALLOWED_MOUNT_ROOTS"
	ociJobMaxStatusBytes       = types.MaxStepRunLogChunkBodyLength
)

type ociJobActionInput struct {
	ImageDigest       string            `json:"imageDigest"`
	Command           []string          `json:"command"`
	Arguments         []string          `json:"arguments"`
	Environment       map[string]string `json:"environment"`
	SecretEnvironment map[string]string `json:"secretEnvironment"`
	Network           string            `json:"network"`
	Volumes           []ociJobVolume    `json:"volumes"`
	TimeoutSeconds    int               `json:"timeoutSeconds"`
	ExpectedExitCodes []int             `json:"expectedExitCodes"`
	IdempotencyKey    string            `json:"idempotencyKey"`
	RunAsUser         string            `json:"runAsUser"`
	Resources         ociJobResources   `json:"resources"`
	Security          ociJobSecurity    `json:"security"`
}

type ociJobPolicy struct {
	AllowedRegistries []string
	AllowedNetworks   []string
	AllowedMountRoots []string
}

type ociJobOperationIdentity struct {
	IdempotencyKey string
	TaskID         string
	StepRunID      string
	StepKey        string
	OperationHash  string
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

type ociJobContainerInspect struct {
	State  ociJobContainerState `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
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
	if err := validateOCIJobActionInput(input, ociJobPolicyFromEnv()); err != nil {
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
	input.Command = trimStringSlice(input.Command)
	if input.ExpectedExitCodes == nil {
		input.ExpectedExitCodes = []int{0}
	}
	if input.Environment == nil {
		input.Environment = map[string]string{}
	}
}

func validateOCIJobActionInput(input ociJobActionInput, policy ociJobPolicy) error {
	policy = normalizeOCIJobPolicy(policy)
	registry, err := ociJobRegistryFromDigest(input.ImageDigest)
	if err != nil {
		return err
	}
	if len(policy.AllowedRegistries) == 0 {
		return fmt.Errorf("%s must allow at least one registry", ociJobAllowedRegistriesEnv)
	}
	if !containsString(policy.AllowedRegistries, registry) {
		return fmt.Errorf("image registry is not allowlisted")
	}
	if len(input.Command) == 0 {
		return fmt.Errorf("command is required")
	}
	if len(input.SecretEnvironment) > 0 {
		return fmt.Errorf("secretEnvironment must be resolved by the task lease")
	}
	if !containsString(policy.AllowedNetworks, input.Network) {
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
		ok, err := ociJobMountUnderAllowedRoot(volume.Source, policy.AllowedMountRoots)
		if err != nil {
			return err
		}
		if !ok {
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

func ociJobPolicyFromEnv() ociJobPolicy {
	return ociJobPolicy{
		AllowedRegistries: splitOCIJobPolicyList(os.Getenv(ociJobAllowedRegistriesEnv)),
		AllowedNetworks:   splitOCIJobPolicyList(os.Getenv(ociJobAllowedNetworksEnv)),
		AllowedMountRoots: splitOCIJobPolicyList(os.Getenv(ociJobAllowedMountRootsEnv)),
	}
}

func normalizeOCIJobPolicy(policy ociJobPolicy) ociJobPolicy {
	policy.AllowedRegistries = trimStringSlice(policy.AllowedRegistries)
	policy.AllowedNetworks = trimStringSlice(policy.AllowedNetworks)
	if len(policy.AllowedNetworks) == 0 {
		policy.AllowedNetworks = []string{"none"}
	}
	policy.AllowedMountRoots = trimStringSlice(policy.AllowedMountRoots)
	return policy
}

func splitOCIJobPolicyList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Split(raw, ",")
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
	identity, err := ociJobOperationIdentityFromStep(lease, step, input)
	if err != nil {
		return recordFailure(err, "")
	}
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
	result, err := runOCIJob(jobCtx, input, containerName, identity, secretValues, updateStatus)
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
	identity ociJobOperationIdentity,
	secretValues []string,
	updateStatus func(string),
) (ociJobResult, error) {
	result := ociJobResult{ContainerName: containerName, ExitCode: -1}
	if updateStatus != nil {
		updateStatus("starting OCI job container")
	}
	if inspected, exists, err := inspectOCIJobContainer(ctx, containerName); err != nil {
		return result, err
	} else if exists {
		if err := validateOCIJobContainerIdentity(inspected, identity); err != nil {
			return result, err
		}
		return finishExistingOCIJobContainer(ctx, containerName, inspected.State, input.ExpectedExitCodes, secretValues)
	}
	envFile, cleanup, err := writeOCIJobEnvFile(input.Environment)
	if err != nil {
		return result, err
	}
	defer cleanup()
	out, runErr := runDockerCommandBounded(ctx, "docker", dockerRunOCIJobArgs(input, containerName, identity, envFile)...)
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
		return result, fmt.Errorf("run OCI job container: %w", redactedErr)
	}
	if !ociJobExpectedExitCode(result.ExitCode, input.ExpectedExitCodes) {
		return result, fmt.Errorf("OCI job exited with code %d", result.ExitCode)
	}
	if result.Status == "" {
		result.Status = fmt.Sprintf("job exited with code %d", result.ExitCode)
	}
	return result, nil
}

func inspectOCIJobContainer(ctx context.Context, containerName string) (ociJobContainerInspect, bool, error) {
	out, err := runDockerCommandBounded(ctx, "docker", "inspect", "--format", "{{json .}}", "--type", "container", containerName)
	text := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(strings.ToLower(text), "no such") || strings.Contains(strings.ToLower(text), "not found") {
			return ociJobContainerInspect{}, false, nil
		}
		return ociJobContainerInspect{}, false, fmt.Errorf("inspect OCI job container: %w: %s", err, text)
	}
	var inspected ociJobContainerInspect
	inspectJSON := strings.TrimSpace(string(out))
	if start := strings.IndexByte(inspectJSON, '{'); start >= 0 {
		inspectJSON = inspectJSON[start:]
	}
	if err := json.Unmarshal([]byte(inspectJSON), &inspected); err != nil {
		return ociJobContainerInspect{}, false, fmt.Errorf("decode OCI job container inspect: %w", err)
	}
	return inspected, true, nil
}

func validateOCIJobContainerIdentity(inspected ociJobContainerInspect, identity ociJobOperationIdentity) error {
	labels := inspected.Config.Labels
	expected := ociJobExpectedLabels(identity)
	for key, value := range expected {
		if labels[key] != value {
			return fmt.Errorf("existing OCI job container does not match operation identity")
		}
	}
	return nil
}

func finishExistingOCIJobContainer(
	ctx context.Context,
	containerName string,
	state ociJobContainerState,
	expectedExitCodes []int,
	secretValues []string,
) (ociJobResult, error) {
	result := ociJobResult{ContainerName: containerName, ExitCode: state.ExitCode}
	status := strings.ToLower(strings.TrimSpace(state.Status))
	switch status {
	case "running":
		code, err := waitOCIJobContainer(ctx, containerName)
		if err != nil {
			if ctx.Err() != nil {
				stopOCIJobContainer(containerName)
			}
			return result, err
		}
		result.ExitCode = code
	case "created":
		if err := startExistingOCIJobContainer(ctx, containerName); err != nil {
			if ctx.Err() != nil {
				stopOCIJobContainer(containerName)
			}
			return result, err
		}
		code, err := waitOCIJobContainer(ctx, containerName)
		if err != nil {
			if ctx.Err() != nil {
				stopOCIJobContainer(containerName)
			}
			return result, err
		}
		result.ExitCode = code
	case "exited":
	default:
		if status == "" {
			status = "unknown"
		}
		return result, fmt.Errorf("existing OCI job container is in unsupported state %q", status)
	}
	logs, err := readOCIJobContainerLogs(ctx, containerName, secretValues)
	if err != nil {
		return result, err
	}
	result.Status = logs
	if !ociJobExpectedExitCode(result.ExitCode, expectedExitCodes) {
		return result, fmt.Errorf("OCI job exited with code %d", result.ExitCode)
	}
	if result.Status == "" {
		result.Status = fmt.Sprintf("job exited with code %d", result.ExitCode)
	}
	return result, nil
}

func startExistingOCIJobContainer(ctx context.Context, containerName string) error {
	out, err := runDockerCommandBounded(ctx, "docker", "start", containerName)
	text := strings.TrimSpace(string(out))
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return fmt.Errorf("OCI job timed out")
		}
		return fmt.Errorf("OCI job canceled")
	}
	if err != nil {
		return fmt.Errorf("start existing OCI job container: %w: %s", err, text)
	}
	return nil
}

func waitOCIJobContainer(ctx context.Context, containerName string) (int, error) {
	out, err := runDockerCommandBounded(ctx, "docker", "wait", containerName)
	text := strings.TrimSpace(string(out))
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return -1, fmt.Errorf("OCI job timed out")
		}
		return -1, fmt.Errorf("OCI job canceled")
	}
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
	out, err := runDockerCommandBounded(ctx, "docker", "logs", containerName)
	text := redactStringWithSecretValues(strings.TrimSpace(string(out)), secretValues)
	if err != nil {
		redactedErr := redactErrorWithSecretValues(err, secretValues)
		return "", fmt.Errorf("read OCI job container logs: %w: %s", redactedErr, text)
	}
	return text, nil
}

func runDockerCommandBounded(ctx context.Context, command string, args ...string) ([]byte, error) {
	output := newBoundedOutput(ociJobMaxStatusBytes)
	cmd := execCommandContext(ctx, command, args...)
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	return []byte(output.String()), err
}

type boundedOutput struct {
	mu        sync.Mutex
	limit     int
	buf       bytes.Buffer
	truncated bool
}

func newBoundedOutput(limit int) *boundedOutput {
	return &boundedOutput{limit: limit}
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit <= 0 {
		b.truncated = b.truncated || len(data) > 0
		return len(data), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || len(data) > 0
		return len(data), nil
	}
	if len(data) > remaining {
		_, _ = b.buf.Write(data[:remaining])
		b.truncated = true
		return len(data), nil
	}
	_, _ = b.buf.Write(data)
	return len(data), nil
}

func (b *boundedOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	value := b.buf.String()
	if !b.truncated {
		return value
	}
	const suffix = "\n[TRUNCATED]"
	if b.limit <= len(suffix) {
		return suffix[len(suffix)-b.limit:]
	}
	if len(value)+len(suffix) <= b.limit {
		return value + suffix
	}
	return value[:b.limit-len(suffix)] + suffix
}

func stopOCIJobContainer(containerName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = runDockerCommandBounded(ctx, "docker", "stop", "--time", "5", containerName)
}

func dockerRunOCIJobArgs(input ociJobActionInput, containerName string, identity ociJobOperationIdentity, envFile string) []string {
	args := []string{
		"run",
		"--name", containerName,
		"--network", input.Network,
		"--read-only",
		"--security-opt", "no-new-privileges",
		"--cap-drop", "ALL",
	}
	labels := ociJobExpectedLabels(identity)
	labelKeys := make([]string, 0, len(labels))
	for key := range labels {
		labelKeys = append(labelKeys, key)
	}
	sort.Strings(labelKeys)
	for _, key := range labelKeys {
		value := labels[key]
		args = append(args, "--label", key+"="+value)
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

func ociJobOperationIdentityFromStep(
	lease api.AgentTaskLease,
	step api.AgentTaskLeaseStep,
	input ociJobActionInput,
) (ociJobOperationIdentity, error) {
	hash, err := ociJobOperationHash(input)
	if err != nil {
		return ociJobOperationIdentity{}, err
	}
	return ociJobOperationIdentity{
		IdempotencyKey: input.IdempotencyKey,
		TaskID:         lease.TaskID.String(),
		StepRunID:      step.StepRunID.String(),
		StepKey:        step.Key,
		OperationHash:  hash,
	}, nil
}

func ociJobOperationHash(input ociJobActionInput) (string, error) {
	environmentHash, err := ociJobHashValue(input.Environment)
	if err != nil {
		return "", err
	}
	value := struct {
		ImageDigest       string
		Command           []string
		Arguments         []string
		EnvironmentHash   string
		Network           string
		Volumes           []ociJobVolume
		TimeoutSeconds    int
		ExpectedExitCodes []int
		RunAsUser         string
		Resources         ociJobResources
		Security          ociJobSecurity
	}{
		ImageDigest:       input.ImageDigest,
		Command:           input.Command,
		Arguments:         input.Arguments,
		EnvironmentHash:   environmentHash,
		Network:           input.Network,
		Volumes:           input.Volumes,
		TimeoutSeconds:    input.TimeoutSeconds,
		ExpectedExitCodes: input.ExpectedExitCodes,
		RunAsUser:         input.RunAsUser,
		Resources:         input.Resources,
		Security:          input.Security,
	}
	return ociJobHashValue(value)
}

func ociJobHashValue(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("hash OCI job operation: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ociJobExpectedLabels(identity ociJobOperationIdentity) map[string]string {
	return map[string]string{
		"distr.action":         ociJobActionType,
		"distr.idempotencyKey": identity.IdempotencyKey,
		"distr.taskId":         identity.TaskID,
		"distr.stepRunId":      identity.StepRunID,
		"distr.stepKey":        identity.StepKey,
		"distr.operationHash":  identity.OperationHash,
	}
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

func ociJobMountUnderAllowedRoot(source string, roots []string) (bool, error) {
	if len(roots) == 0 {
		return false, nil
	}
	resolvedSource, err := resolveOCIJobMountPath(source, "volume source")
	if err != nil {
		return false, err
	}
	for _, root := range roots {
		resolvedRoot, err := resolveOCIJobMountPath(root, "allowed mount root")
		if err != nil {
			return false, err
		}
		if sameOrChildPath(resolvedSource, resolvedRoot) {
			return true, nil
		}
	}
	return false, nil
}

func resolveOCIJobMountPath(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("%s must be an absolute path", field)
	}
	resolved, err := filepath.EvalSymlinks(value)
	if err != nil {
		return "", fmt.Errorf("resolve %s symlinks: %w", field, err)
	}
	return filepath.Clean(resolved), nil
}

func sameOrChildPath(child, root string) bool {
	if child == root {
		return true
	}
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
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
