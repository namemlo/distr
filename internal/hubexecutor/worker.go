package hubexecutor

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/distr-sh/distr/api"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/stepredaction"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/webhookaction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const (
	defaultPollInterval         = time.Second
	defaultHeartbeatInterval    = 30 * time.Second
	defaultExternalPollInterval = time.Second
	defaultMaxConcurrency       = 4
)

type Options struct {
	PollInterval         time.Duration
	HeartbeatInterval    time.Duration
	MaxConcurrency       int
	RuntimeOptions       webhookaction.RuntimeOptions
	ExternalPollInterval time.Duration
	CallbackBaseURL      string
}

type taskStore interface {
	Lease(context.Context) (*types.TaskLease, error)
	Heartbeat(context.Context, types.HeartbeatHubTaskLeaseRequest) (*types.TaskLease, error)
	Record(context.Context, types.RecordHubStepRunEventRequest) (*types.StepRunEvent, error)
	PrepareExternalExecution(context.Context, types.PrepareExternalExecutionRequest) (*types.ExternalExecution, error)
	MarkExternalExecutionTriggered(
		context.Context,
		types.MarkExternalExecutionTriggeredRequest,
	) (*types.ExternalExecution, error)
	GetExternalExecution(context.Context, uuid.UUID, uuid.UUID) (*types.ExternalExecution, error)
	TimeoutExternalExecution(context.Context, types.TimeoutExternalExecutionRequest) (*types.ExternalExecution, error)
	FailExternalExecution(context.Context, types.FailExternalExecutionRequest) (*types.ExternalExecution, error)
}

type Worker struct {
	logger   *zap.Logger
	store    taskStore
	options  Options
	start    sync.Once
	stateMu  sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	running  sync.WaitGroup
	capacity chan struct{}
}

func New(logger *zap.Logger, pool *pgxpool.Pool, options Options) *Worker {
	return newWorker(logger, databaseStore{pool: pool, logger: logger}, options)
}

func newWorker(logger *zap.Logger, store taskStore, options Options) *Worker {
	if options.PollInterval <= 0 {
		options.PollInterval = defaultPollInterval
	}
	if options.HeartbeatInterval <= 0 {
		options.HeartbeatInterval = defaultHeartbeatInterval
	}
	if options.MaxConcurrency <= 0 {
		options.MaxConcurrency = defaultMaxConcurrency
	}
	if options.ExternalPollInterval <= 0 {
		options.ExternalPollInterval = defaultExternalPollInterval
	}
	if strings.TrimSpace(options.CallbackBaseURL) == "" {
		options.CallbackBaseURL = env.Host()
	}
	options.CallbackBaseURL = strings.TrimRight(strings.TrimSpace(options.CallbackBaseURL), "/")
	return &Worker{
		logger:   logger.With(zap.String("component", "hub-executor")),
		store:    store,
		options:  options,
		done:     make(chan struct{}),
		capacity: make(chan struct{}, options.MaxConcurrency),
	}
}

func (w *Worker) Start(ctx context.Context) {
	w.start.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		w.stateMu.Lock()
		w.cancel = cancel
		w.stateMu.Unlock()
		w.logger.Info("Hub task executor starting", zap.Int("maxConcurrency", cap(w.capacity)))
		go w.run(runCtx)
	})
}

func (w *Worker) Shutdown(ctx context.Context) error {
	w.stateMu.Lock()
	cancel := w.cancel
	w.stateMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) run(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.options.PollInterval)
	defer ticker.Stop()
	w.dispatch(ctx)
	for {
		select {
		case <-ctx.Done():
			w.running.Wait()
			w.logger.Info("Hub task executor stopped")
			return
		case <-ticker.C:
			w.dispatch(ctx)
		}
	}
}

func (w *Worker) dispatch(ctx context.Context) {
	for len(w.capacity) < cap(w.capacity) {
		lease, err := w.store.Lease(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				w.logger.Error("could not lease Hub task", zap.Error(err))
			}
			return
		}
		if lease == nil {
			return
		}
		w.capacity <- struct{}{}
		w.running.Add(1)
		go func() {
			defer func() {
				<-w.capacity
				w.running.Done()
			}()
			if err := w.executeLease(ctx, *lease); err != nil && !errors.Is(err, context.Canceled) {
				message, _ := stepredaction.RedactString(err.Error())
				w.logger.Warn("Hub task execution failed",
					zap.String("taskId", lease.TaskID.String()),
					zap.String("message", message))
			}
		}()
	}
}

func (w *Worker) executeLease(ctx context.Context, lease types.TaskLease) error {
	if lease.ExecutorType != types.TaskExecutorTypeHub {
		return fmt.Errorf("cannot execute %s lease in Hub worker", lease.ExecutorType)
	}
	for _, step := range lease.Steps {
		if err := w.executeStep(ctx, lease, step); err != nil {
			return err
		}
	}
	return nil
}

//nolint:gocyclo // Execution is an explicit state machine whose branches must share one durable event sequence.
func (w *Worker) executeStep(
	ctx context.Context,
	lease types.TaskLease,
	step types.TaskLeaseStep,
) error {
	sequence := int64(1)
	if err := w.record(ctx, lease, step, sequence, types.StepRunEventTypeStarted, "starting Hub action", nil); err != nil {
		return err
	}
	recordFailure := func(runErr error, secretValues []string) error {
		message, _ := stepredaction.RedactStringWithValues(runErr.Error(), secretValues)
		message = limitRunEventMessage(message)
		if _, err := w.store.Record(ctx, types.RecordHubStepRunEventRequest{
			OrganizationID:     lease.OrganizationID,
			DeploymentTargetID: lease.AgentID,
			StepRunID:          step.StepRunID,
			LeaseToken:         lease.LeaseToken,
			Sequence:           sequence + 1,
			Type:               types.StepRunEventTypeFailed,
			Message:            message,
		}); err != nil {
			return fmt.Errorf("record Hub action failure: %w", err)
		}
		return runErr
	}
	if step.ActionType != "distr.webhook" {
		return recordFailure(fmt.Errorf("unsupported Hub actionType %q", step.ActionType), nil)
	}
	if step.ActionVersion != types.AgentActionVersionV1 {
		return recordFailure(fmt.Errorf("unsupported Hub actionVersion %q", step.ActionVersion), nil)
	}
	input, err := webhookaction.DecodeInput(step.InputBindings)
	if err != nil {
		return recordFailure(err, nil)
	}
	input.TenantID = lease.OrganizationID
	input.LeaseID = lease.ID
	input.TaskID = lease.TaskID
	input.StepRunID = step.StepRunID
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = step.IdempotencyKey
	}
	secretValues := webhookaction.SecretValues(input)
	actionCtx, actionCancel := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go w.heartbeat(actionCtx, actionCancel, lease, heartbeatDone)
	stopHeartbeat := func() error {
		actionCancel()
		return <-heartbeatDone
	}
	var external *types.ExternalExecution
	if input.CompletionMode == webhookaction.CompletionModeCallback {
		external, err = w.store.PrepareExternalExecution(actionCtx, types.PrepareExternalExecutionRequest{
			OrganizationID: lease.OrganizationID, StepRunID: step.StepRunID,
			Component: input.Component, CallbackTimeoutSeconds: input.CallbackTimeoutSeconds,
		})
		if err != nil {
			heartbeatErr := stopHeartbeat()
			if heartbeatErr != nil {
				return recordFailure(fmt.Errorf("heartbeat Hub task lease: %w", heartbeatErr), secretValues)
			}
			return recordFailure(err, secretValues)
		}
		input.IdempotencyKey = external.IdempotencyKey
		input.RuntimeHeaders = externalExecutionRuntimeHeaders(*external, w.options.CallbackBaseURL)
	}
	var result webhookaction.Result
	var runErr error
	webhookInvoked := external == nil
	if external != nil && external.Status == types.ExternalExecutionStatusQueued {
		triggered, triggerErr := w.store.MarkExternalExecutionTriggered(
			actionCtx,
			types.MarkExternalExecutionTriggeredRequest{
				OrganizationID: lease.OrganizationID, ExternalExecutionID: external.ID, TriggerAttempts: 1,
			},
		)
		if triggerErr == nil {
			external = triggered
			webhookInvoked = true
		} else {
			current, readErr := w.store.GetExternalExecution(actionCtx, external.ID, lease.OrganizationID)
			if readErr == nil && current.Status != types.ExternalExecutionStatusQueued {
				external = current
				webhookInvoked = false
			} else if readErr != nil {
				runErr = fmt.Errorf("%w; reconcile external execution dispatch: %v", triggerErr, readErr)
			} else {
				runErr = triggerErr
			}
		}
		if runErr == nil && webhookInvoked {
			sequence++
			runErr = w.record(
				actionCtx, lease, step, sequence, types.StepRunEventTypeProgress,
				"external execution dispatch committed; invoking external executor", nil,
			)
		}
	}
	if runErr == nil && webhookInvoked {
		result, runErr = webhookaction.Run(actionCtx, input, func(message string) error {
			nextSequence := sequence + 1
			if err := w.record(
				actionCtx, lease, step, nextSequence, types.StepRunEventTypeProgress, message, nil,
			); err != nil {
				return err
			}
			sequence = nextSequence
			return nil
		}, w.options.RuntimeOptions)
		secretValues = append(secretValues, result.RedactionValues...)
	}
	if runErr == nil && external != nil {
		if runErr == nil {
			switch external.Status {
			case types.ExternalExecutionStatusRunning:
				sequence++
				message := "resuming wait for external execution callback"
				if webhookInvoked {
					message = "external executor accepted the request; waiting for callback"
				}
				runErr = w.record(
					actionCtx, lease, step, sequence, types.StepRunEventTypeProgress, message, nil,
				)
				if runErr == nil {
					external, runErr = w.waitForExternalExecution(actionCtx, lease, step, external, &sequence)
				}
			case types.ExternalExecutionStatusSucceeded:
				sequence++
				runErr = w.record(
					actionCtx, lease, step, sequence, types.StepRunEventTypeProgress,
					"external execution already succeeded; finalizing task", nil,
				)
			case types.ExternalExecutionStatusFailed,
				types.ExternalExecutionStatusCanceled,
				types.ExternalExecutionStatusTimedOut:
				runErr = externalExecutionError(*external)
			default:
				runErr = fmt.Errorf("unsupported external execution status %q", external.Status)
			}
		}
	}
	if runErr != nil && external != nil && !external.Status.IsTerminal() &&
		!errors.Is(runErr, context.Canceled) {
		failed, failErr := w.store.FailExternalExecution(actionCtx, types.FailExternalExecutionRequest{
			OrganizationID: lease.OrganizationID, ExternalExecutionID: external.ID,
			Message: runErr.Error(),
		})
		if failErr == nil {
			external = failed
			switch failed.Status {
			case types.ExternalExecutionStatusSucceeded:
				runErr = nil
			case types.ExternalExecutionStatusFailed,
				types.ExternalExecutionStatusCanceled,
				types.ExternalExecutionStatusTimedOut:
				runErr = externalExecutionError(*failed)
			}
		} else {
			runErr = fmt.Errorf("%w; persist external execution failure: %v", runErr, failErr)
		}
	}
	heartbeatErr := stopHeartbeat()
	if heartbeatErr != nil {
		return recordFailure(fmt.Errorf("heartbeat Hub task lease: %w", heartbeatErr), secretValues)
	}
	if runErr != nil {
		return recordFailure(runErr, secretValues)
	}
	outputs := []api.AgentStepRunOutputRequest{}
	if webhookInvoked && external == nil {
		outputs = append(outputs,
			api.AgentStepRunOutputRequest{Name: "statusCode", Value: result.StatusCode},
			api.AgentStepRunOutputRequest{Name: "attempts", Value: result.Attempts},
			api.AgentStepRunOutputRequest{Name: "signingKeyVersion", Value: result.SigningKeyVersion},
			api.AgentStepRunOutputRequest{Name: "keyRotationApplied", Value: result.KeyRotationApplied},
		)
		outputs = append(outputs, webhookaction.AuditOutputRequests(result.AuditTrail)...)
		outputs = append(outputs, result.Outputs...)
	}
	if external != nil {
		outputs = append(outputs, externalExecutionOutputs(*external)...)
	}
	recordOutputs := make([]types.RecordStepRunOutputRequest, 0, len(outputs))
	for _, output := range outputs {
		recordOutputs = append(recordOutputs, types.RecordStepRunOutputRequest{
			Name: output.Name, Value: output.Value, Sensitive: output.Sensitive,
		})
	}
	sequence++
	return w.record(
		ctx, lease, step, sequence, types.StepRunEventTypeSucceeded, "Hub webhook succeeded", recordOutputs,
	)
}

func externalExecutionError(execution types.ExternalExecution) error {
	message := execution.ErrorSummary
	if message == "" {
		message = execution.LastMessage
	}
	if message == "" {
		message = "external execution ended with " + string(execution.Status)
	}
	return fmt.Errorf("%s", message)
}

func (w *Worker) waitForExternalExecution(
	ctx context.Context,
	lease types.TaskLease,
	step types.TaskLeaseStep,
	execution *types.ExternalExecution,
	sequence *int64,
) (*types.ExternalExecution, error) {
	lastCallbackSequence := execution.LastCallbackSequence
	for {
		current, err := w.store.GetExternalExecution(ctx, execution.ID, lease.OrganizationID)
		if err != nil {
			return execution, fmt.Errorf("read external execution callback state: %w", err)
		}
		execution = current
		if current.LastCallbackSequence > lastCallbackSequence {
			lastCallbackSequence = current.LastCallbackSequence
			message := current.LastMessage
			if message == "" {
				message = "external execution callback: " + string(current.Status)
			}
			*sequence++
			if err := w.record(ctx, lease, step, *sequence, types.StepRunEventTypeProgress, message, nil); err != nil {
				return execution, err
			}
		}
		switch current.Status {
		case types.ExternalExecutionStatusSucceeded:
			return current, nil
		case types.ExternalExecutionStatusFailed,
			types.ExternalExecutionStatusCanceled,
			types.ExternalExecutionStatusTimedOut:
			return current, externalExecutionError(*current)
		}
		wait := w.options.ExternalPollInterval
		untilDeadline := time.Until(current.CallbackDeadlineAt)
		if untilDeadline <= 0 {
			timedOut, timeoutErr := w.store.TimeoutExternalExecution(ctx, types.TimeoutExternalExecutionRequest{
				OrganizationID: lease.OrganizationID, ExternalExecutionID: current.ID,
				Message: "external execution callback timed out",
			})
			if timeoutErr != nil {
				return current, fmt.Errorf("time out external execution: %w", timeoutErr)
			}
			return timedOut, fmt.Errorf("external execution callback timed out")
		}
		if untilDeadline < wait {
			wait = untilDeadline
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return current, ctx.Err()
		case <-timer.C:
		}
	}
}

func externalExecutionRuntimeHeaders(execution types.ExternalExecution, callbackBaseURL string) map[string]string {
	callbackURL := callbackBaseURL + "/api/v1/external-executions/" + execution.ID.String() + "/callbacks"
	return map[string]string{
		"X-Distr-External-Execution-ID":   execution.ID.String(),
		"X-Distr-Plan-Checksum":           execution.PlanChecksum,
		"X-Distr-Expected-State-Version":  strconv.FormatInt(execution.ExpectedStateVersion, 10),
		"X-Distr-Expected-State-Checksum": execution.ExpectedStateChecksum,
		"X-Distr-Callback-URL":            callbackURL,
	}
}

func externalExecutionOutputs(execution types.ExternalExecution) []api.AgentStepRunOutputRequest {
	outputs := []api.AgentStepRunOutputRequest{
		{Name: "externalExecutionId", Value: execution.ID.String()},
		{Name: "providerReference", Value: execution.ProviderReference},
		{Name: "providerUrl", Value: execution.ProviderURL},
		{Name: "actualVersion", Value: execution.ActualVersion},
		{Name: "actualImage", Value: execution.ActualImage},
		{Name: "actualConfigReference", Value: execution.ActualConfigReference},
		{Name: "actualConfigChecksum", Value: execution.ActualConfigChecksum},
		{Name: "observedStateChecksum", Value: execution.ObservedStateChecksum},
	}
	if execution.ActualPlatform != nil {
		outputs = append(outputs, api.AgentStepRunOutputRequest{Name: "actualPlatform", Value: *execution.ActualPlatform})
	}
	if execution.ActualHealth != nil {
		outputs = append(outputs, api.AgentStepRunOutputRequest{Name: "actualHealth", Value: *execution.ActualHealth})
	}
	return outputs
}

func (w *Worker) heartbeat(
	ctx context.Context,
	cancel context.CancelFunc,
	lease types.TaskLease,
	done chan<- error,
) {
	ticker := time.NewTicker(w.options.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			_, err := w.store.Heartbeat(ctx, types.HeartbeatHubTaskLeaseRequest{
				OrganizationID:     lease.OrganizationID,
				DeploymentTargetID: lease.AgentID,
				TaskID:             lease.TaskID,
				LeaseToken:         lease.LeaseToken,
			})
			if err != nil {
				if ctx.Err() != nil {
					done <- nil
					return
				}
				cancel()
				done <- err
				return
			}
		}
	}
}

func (w *Worker) record(
	ctx context.Context,
	lease types.TaskLease,
	step types.TaskLeaseStep,
	sequence int64,
	eventType types.StepRunEventType,
	message string,
	outputs []types.RecordStepRunOutputRequest,
) error {
	_, err := w.store.Record(ctx, types.RecordHubStepRunEventRequest{
		OrganizationID:     lease.OrganizationID,
		DeploymentTargetID: lease.AgentID,
		StepRunID:          step.StepRunID,
		LeaseToken:         lease.LeaseToken,
		Sequence:           sequence,
		Type:               eventType,
		Message:            limitRunEventMessage(message),
		Outputs:            outputs,
	})
	return err
}

func limitRunEventMessage(message string) string {
	runes := []rune(message)
	if len(runes) > types.MaxStepRunEventMessageLength {
		return string(runes[:types.MaxStepRunEventMessageLength])
	}
	return message
}

type databaseStore struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func (s databaseStore) context(ctx context.Context) context.Context {
	return internalctx.WithLogger(internalctx.WithDb(ctx, s.pool), s.logger)
}

func (s databaseStore) Lease(ctx context.Context) (*types.TaskLease, error) {
	return db.LeaseHubTask(s.context(ctx))
}

func (s databaseStore) Heartbeat(
	ctx context.Context,
	request types.HeartbeatHubTaskLeaseRequest,
) (*types.TaskLease, error) {
	return db.HeartbeatHubTaskLease(s.context(ctx), request)
}

func (s databaseStore) Record(
	ctx context.Context,
	request types.RecordHubStepRunEventRequest,
) (*types.StepRunEvent, error) {
	return db.RecordHubStepRunEvent(s.context(ctx), request)
}

func (s databaseStore) PrepareExternalExecution(
	ctx context.Context,
	request types.PrepareExternalExecutionRequest,
) (*types.ExternalExecution, error) {
	return db.PrepareExternalExecution(s.context(ctx), request)
}

func (s databaseStore) MarkExternalExecutionTriggered(
	ctx context.Context,
	request types.MarkExternalExecutionTriggeredRequest,
) (*types.ExternalExecution, error) {
	return db.MarkExternalExecutionTriggered(s.context(ctx), request)
}

func (s databaseStore) GetExternalExecution(
	ctx context.Context,
	id, orgID uuid.UUID,
) (*types.ExternalExecution, error) {
	return db.GetExternalExecution(s.context(ctx), id, orgID)
}

func (s databaseStore) TimeoutExternalExecution(
	ctx context.Context,
	request types.TimeoutExternalExecutionRequest,
) (*types.ExternalExecution, error) {
	return db.TimeoutExternalExecution(s.context(ctx), request)
}

func (s databaseStore) FailExternalExecution(
	ctx context.Context,
	request types.FailExternalExecutionRequest,
) (*types.ExternalExecution, error) {
	return db.FailExternalExecution(s.context(ctx), request)
}
