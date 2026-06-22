package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/distr-sh/distr/api"
	"github.com/distr-sh/distr/internal/agentauth"
	"github.com/distr-sh/distr/internal/agentenv"
	"github.com/distr-sh/distr/internal/stepredaction"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var taskLeaseHeartbeatEvery = agentenv.Interval

type composeDeployActionInput struct {
	ApplicationVersion composeDeployApplicationVersion `json:"applicationVersion"`
	ProjectName        string                          `json:"projectName"`
	EnvironmentFile    string                          `json:"environmentFile"`
	PullPolicy         string                          `json:"pullPolicy"`
	WaitForHealthy     bool                            `json:"waitForHealthy"`
	TimeoutSeconds     int                             `json:"timeoutSeconds"`
	Strategy           string                          `json:"strategy"`
}

type composeDeployApplicationVersion struct {
	ComposeFile  string                           `json:"composeFile"`
	RegistryAuth map[string]api.AgentRegistryAuth `json:"registryAuth"`
}

type composeDeployOptions struct {
	PullPolicy            string
	WaitForHealthy        bool
	Timeout               time.Duration
	LocalDeploymentSource AgentDeploymentSource
}

type composeDeployApplyFunc func(
	context.Context,
	api.AgentDeployment,
	composeDeployOptions,
	func(string),
) (*AgentDeployment, string, error)

type stepEventClient interface {
	RecordStepRunEvent(context.Context, uuid.UUID, api.AgentStepRunEventRequest) (*api.StepRunEvent, error)
}

type leasedTaskClient interface {
	stepEventClient
	HeartbeatTaskLease(context.Context, uuid.UUID, string) (*api.AgentTaskLease, error)
}

var composeProjectNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func decodeComposeDeployActionInput(inputs map[string]any) (composeDeployActionInput, error) {
	var input composeDeployActionInput
	data, err := json.Marshal(inputs)
	if err != nil {
		return input, fmt.Errorf("encode compose deploy inputs: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, fmt.Errorf("decode compose deploy inputs: %w", err)
	}

	input.ProjectName = strings.TrimSpace(input.ProjectName)
	input.PullPolicy = strings.TrimSpace(input.PullPolicy)
	input.Strategy = strings.TrimSpace(input.Strategy)
	if input.Strategy == "" {
		input.Strategy = string(types.DockerTypeCompose)
	}
	if input.ApplicationVersion.RegistryAuth == nil {
		input.ApplicationVersion.RegistryAuth = map[string]api.AgentRegistryAuth{}
	}

	if strings.TrimSpace(input.ApplicationVersion.ComposeFile) == "" {
		return input, fmt.Errorf("applicationVersion.composeFile is required")
	}
	if input.ProjectName == "" {
		return input, fmt.Errorf("projectName is required")
	}
	if !composeProjectNamePattern.MatchString(input.ProjectName) {
		return input, fmt.Errorf("projectName must be a valid Docker Compose project name")
	}
	if input.PullPolicy != "" && !isSupportedComposePullPolicy(input.PullPolicy) {
		return input, fmt.Errorf("pullPolicy is unsupported")
	}
	switch input.Strategy {
	case string(types.DockerTypeCompose), string(types.DockerTypeSwarm):
	default:
		return input, fmt.Errorf("strategy is unsupported")
	}
	if input.Strategy == string(types.DockerTypeSwarm) && input.WaitForHealthy {
		return input, fmt.Errorf("waitForHealthy is only supported for compose strategy")
	}
	if input.TimeoutSeconds < 0 {
		return input, fmt.Errorf("timeoutSeconds must be greater than or equal to 0")
	}
	return input, nil
}

func isSupportedComposePullPolicy(value string) bool {
	switch value {
	case "always", "missing", "if_not_present", "never":
		return true
	default:
		return false
	}
}

func agentDeploymentFromComposeAction(stepRunID uuid.UUID, input composeDeployActionInput) (api.AgentDeployment, error) {
	dockerType := types.DockerType(input.Strategy)
	composeFile, err := composeFileWithProjectName([]byte(input.ApplicationVersion.ComposeFile), input.ProjectName)
	if err != nil {
		return api.AgentDeployment{}, fmt.Errorf("patch compose project name: %w", err)
	}
	return api.AgentDeployment{
		ID:           stepRunID,
		RevisionID:   stepRunID,
		RegistryAuth: input.ApplicationVersion.RegistryAuth,
		ComposeFile:  composeFile,
		EnvFile:      []byte(input.EnvironmentFile),
		DockerType:   &dockerType,
	}, nil
}

func composeOptionsFromAction(input composeDeployActionInput) composeDeployOptions {
	options := composeDeployOptions{
		PullPolicy:            input.PullPolicy,
		WaitForHealthy:        input.WaitForHealthy,
		LocalDeploymentSource: AgentDeploymentSourceTask,
	}
	if input.TimeoutSeconds > 0 {
		options.Timeout = time.Duration(input.TimeoutSeconds) * time.Second
	}
	return options
}

func composeFileWithProjectName(data []byte, projectName string) ([]byte, error) {
	compose, err := DecodeComposeFile(data)
	if err != nil {
		return nil, err
	}
	compose["name"] = projectName
	return EncodeComposeFile(compose)
}

func executeTaskLease(
	ctx context.Context,
	lease api.AgentTaskLease,
	client leasedTaskClient,
	apply composeDeployApplyFunc,
) error {
	for _, step := range lease.Steps {
		if _, err := client.HeartbeatTaskLease(ctx, lease.TaskID, lease.LeaseToken); err != nil {
			return fmt.Errorf("heartbeat task lease: %w", err)
		}
		switch step.ActionType {
		case composeDeployActionType:
			if err := executeComposeDeployStep(ctx, lease, step, client, apply); err != nil {
				return err
			}
		case ociJobActionType:
			if err := executeOCIJobStep(ctx, lease, step, client); err != nil {
				return err
			}
		case fileRenderActionType:
			if err := executeFileRenderStep(ctx, lease, step, client); err != nil {
				return err
			}
		default:
			if err := recordUnsupportedStep(ctx, lease, step, client); err != nil {
				return err
			}
			return fmt.Errorf("unsupported actionType %q", step.ActionType)
		}
	}
	return nil
}

func executeComposeDeployStep(
	ctx context.Context,
	lease api.AgentTaskLease,
	step api.AgentTaskLeaseStep,
	client leasedTaskClient,
	apply composeDeployApplyFunc,
) error {
	sequence := int64(1)
	if err := recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeStarted, "starting Compose deployment", nil, nil); err != nil {
		return err
	}
	var secretValues []string
	recordFailure := func(err error) error {
		sequence++
		redactedErr := redactErrorWithSecretValues(err, secretValues)
		if recordErr := recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeFailed, redactedErr.Error(), nil, nil, secretValues...); recordErr != nil {
			return redactErrorWithSecretValues(recordErr, secretValues)
		}
		return redactedErr
	}
	if step.ActionType != composeDeployActionType {
		return recordFailure(fmt.Errorf("unsupported actionType %q", step.ActionType))
	}
	if step.ActionVersion != types.AgentActionVersionV1 {
		return recordFailure(fmt.Errorf("unsupported actionVersion %q", step.ActionVersion))
	}

	input, err := decodeComposeDeployActionInput(step.Inputs)
	if err != nil {
		return recordFailure(err)
	}
	secretValues = composeDeploySecretValues(input)
	deployment, err := agentDeploymentFromComposeAction(step.StepRunID, input)
	if err != nil {
		return recordFailure(err)
	}

	options := composeOptionsFromAction(input)
	applyCtx, applyCancel := context.WithCancel(ctx)
	if options.Timeout > 0 {
		applyCtx, applyCancel = context.WithTimeout(ctx, options.Timeout)
	}
	defer applyCancel()
	heartbeatErrCh, stopHeartbeat := startTaskLeaseHeartbeat(applyCtx, lease, client, applyCancel)

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
		progressErr = recordStepEvent(applyCtx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeProgress, status, nil, nil, secretValues...)
		if progressErr != nil {
			applyCancel()
		}
	}

	agentDeployment, status, err := apply(applyCtx, deployment, options, updateStatus)
	stopHeartbeat()
	progressErrMu.Lock()
	callbackErr := progressErr
	progressErrMu.Unlock()
	if callbackErr != nil {
		return redactErrorWithSecretValues(callbackErr, secretValues)
	}
	if heartbeatErr := taskLeaseHeartbeatError(heartbeatErrCh); heartbeatErr != nil {
		sequence++
		redactedErr := redactErrorWithSecretValues(heartbeatErr, secretValues)
		if recordErr := recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeFailed, redactedErr.Error(), nil, nil, secretValues...); recordErr != nil {
			return redactErrorWithSecretValues(recordErr, secretValues)
		}
		return redactedErr
	}

	if err != nil {
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

	sequence++
	outputs := []api.AgentStepRunOutputRequest{
		{Name: "projectName", Value: input.ProjectName},
		{Name: "strategy", Value: input.Strategy},
		{Name: "status", Value: status},
	}
	if agentDeployment != nil {
		outputs = append(outputs, api.AgentStepRunOutputRequest{Name: "state", Value: string(agentDeployment.State)})
	}
	return recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, sequence, types.StepRunEventTypeSucceeded, "Compose deployment succeeded", nil, outputs, secretValues...)
}

func composeDeploySecretValues(input composeDeployActionInput) []string {
	values := make([]string, 0, len(input.ApplicationVersion.RegistryAuth))
	for _, auth := range input.ApplicationVersion.RegistryAuth {
		if auth.Password != "" {
			values = append(values, auth.Password)
		}
	}
	return values
}

func startTaskLeaseHeartbeat(
	ctx context.Context,
	lease api.AgentTaskLease,
	client leasedTaskClient,
	cancelApply context.CancelFunc,
) (<-chan error, context.CancelFunc) {
	stopCh := make(chan struct{})
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() { close(stopCh) })
	}
	errCh := make(chan error, 1)
	interval := taskLeaseHeartbeatEvery
	if interval <= 0 {
		interval = time.Second
	}
	go func() {
		defer close(errCh)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-ticker.C:
				if _, err := client.HeartbeatTaskLease(ctx, lease.TaskID, lease.LeaseToken); err != nil {
					errCh <- fmt.Errorf("heartbeat task lease: %w", err)
					cancelApply()
					return
				}
			}
		}
	}()
	return errCh, stop
}

func taskLeaseHeartbeatError(errCh <-chan error) error {
	if err, ok := <-errCh; ok {
		return err
	}
	return nil
}

func recordUnsupportedStep(
	ctx context.Context,
	lease api.AgentTaskLease,
	step api.AgentTaskLeaseStep,
	client stepEventClient,
) error {
	message := fmt.Sprintf("unsupported actionType %q", step.ActionType)
	if err := recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, 1, types.StepRunEventTypeStarted, "starting task lease step", nil, nil); err != nil {
		return err
	}
	return recordStepEvent(ctx, client, step.StepRunID, lease.LeaseToken, 2, types.StepRunEventTypeFailed, message, nil, nil)
}

func recordStepEvent(
	ctx context.Context,
	client stepEventClient,
	stepRunID uuid.UUID,
	leaseToken string,
	sequence int64,
	eventType types.StepRunEventType,
	message string,
	logs []api.AgentStepRunLogChunkRequest,
	outputs []api.AgentStepRunOutputRequest,
	secretValues ...string,
) error {
	progress := (*int)(nil)
	if eventType == types.StepRunEventTypeSucceeded {
		value := 100
		progress = &value
	}
	message, _ = stepredaction.RedactStringWithValues(message, secretValues)
	for i := range logs {
		logs[i].Body, _ = stepredaction.RedactStringWithValues(logs[i].Body, secretValues)
	}
	for i := range outputs {
		if outputs[i].Sensitive {
			continue
		}
		outputs[i].Value, _ = stepredaction.RedactValueWithValues(outputs[i].Value, secretValues)
	}
	_, err := client.RecordStepRunEvent(ctx, stepRunID, api.AgentStepRunEventRequest{
		LeaseToken:      leaseToken,
		Sequence:        sequence,
		Type:            eventType,
		Message:         message,
		ProgressPercent: progress,
		Logs:            logs,
		Outputs:         outputs,
	})
	return err
}

func dockerComposeDeployActionApply(
	ctx context.Context,
	deployment api.AgentDeployment,
	options composeDeployOptions,
	updateStatus func(string),
) (*AgentDeployment, string, error) {
	if _, err := agentauth.EnsureAuth(ctx, client.RawToken(), deployment); err != nil {
		return nil, "", err
	}
	return DockerEngineApplyWithComposeOptions(ctx, deployment, options, updateStatus)
}
