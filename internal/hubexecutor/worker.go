package hubexecutor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/distr-sh/distr/api"
	internalctx "github.com/distr-sh/distr/internal/context"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/stepredaction"
	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/webhookaction"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const (
	defaultPollInterval      = time.Second
	defaultHeartbeatInterval = 30 * time.Second
	defaultMaxConcurrency    = 4
)

type Options struct {
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	MaxConcurrency    int
	RuntimeOptions    webhookaction.RuntimeOptions
}

type taskStore interface {
	Lease(context.Context) (*types.TaskLease, error)
	Heartbeat(context.Context, types.HeartbeatHubTaskLeaseRequest) (*types.TaskLease, error)
	Record(context.Context, types.RecordHubStepRunEventRequest) (*types.StepRunEvent, error)
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
	result, runErr := webhookaction.Run(actionCtx, input, func(message string) error {
		nextSequence := sequence + 1
		if err := w.record(
			actionCtx, lease, step, nextSequence, types.StepRunEventTypeProgress, message, nil,
		); err != nil {
			return err
		}
		sequence = nextSequence
		return nil
	}, w.options.RuntimeOptions)
	actionCancel()
	heartbeatErr := <-heartbeatDone
	secretValues = append(secretValues, result.RedactionValues...)
	if heartbeatErr != nil {
		return recordFailure(fmt.Errorf("heartbeat Hub task lease: %w", heartbeatErr), secretValues)
	}
	if runErr != nil {
		return recordFailure(runErr, secretValues)
	}
	outputs := []api.AgentStepRunOutputRequest{
		{Name: "statusCode", Value: result.StatusCode},
		{Name: "attempts", Value: result.Attempts},
		{Name: "signingKeyVersion", Value: result.SigningKeyVersion},
		{Name: "keyRotationApplied", Value: result.KeyRotationApplied},
	}
	outputs = append(outputs, webhookaction.AuditOutputRequests(result.AuditTrail)...)
	outputs = append(outputs, result.Outputs...)
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
