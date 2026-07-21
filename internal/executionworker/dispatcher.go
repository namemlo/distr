package executionworker

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type AdmissionRequest struct {
	OrganizationID     uuid.UUID
	DeploymentTargetID uuid.UUID
	EnvironmentID      uuid.UUID
	PlanID             uuid.UUID
	TaskID             uuid.UUID
	StepRunID          uuid.UUID
	StepKey            string
}

type AdmissionDecision struct {
	OperatorFlag     bool
	ExecutorFlag     bool
	ScopedEnrollment bool
	PlanApproved     bool
	PlanAdmitted     bool
	AdapterPreflight bool
}

func (d AdmissionDecision) denialReason() string {
	checks := []struct {
		ok     bool
		reason string
	}{
		{d.OperatorFlag, "operator_flag"},
		{d.ExecutorFlag, "executor_flag"},
		{d.ScopedEnrollment, "scoped_enrollment"},
		{d.PlanApproved, "plan_approval"},
		{d.PlanAdmitted, "plan_admission"},
		{d.AdapterPreflight, "adapter_preflight"},
	}
	for _, check := range checks {
		if !check.ok {
			return check.reason
		}
	}
	return ""
}

type AdmissionGate interface {
	EvaluateExecutionV2Admission(context.Context, AdmissionRequest) (AdmissionDecision, error)
}

type CreateAttemptRequest struct {
	OrganizationID     uuid.UUID
	DeploymentTargetID uuid.UUID
	ExecutionID        uuid.UUID
	PlanID             uuid.UUID
	TaskID             uuid.UUID
	StepRunID          uuid.UUID
	StepKey            string
	Retry              bool
}

type AttemptCreator interface {
	CreateExecutionAttempt(context.Context, CreateAttemptRequest) (*types.ExecutionAttempt, error)
}

type DispatchRequest struct {
	OrganizationID     uuid.UUID
	DeploymentTargetID uuid.UUID
	EnvironmentID      uuid.UUID
	ExecutionID        uuid.UUID
	PlanID             uuid.UUID
	TaskID             uuid.UUID
	StepRunID          uuid.UUID
	StepKey            string
	Retry              bool
}

type Dispatcher struct {
	gate    AdmissionGate
	creator AttemptCreator
}

func NewDispatcher(gate AdmissionGate, creator AttemptCreator) *Dispatcher {
	return &Dispatcher{gate: gate, creator: creator}
}

func (d *Dispatcher) Dispatch(
	ctx context.Context,
	request DispatchRequest,
) (*types.ExecutionAttempt, error) {
	if d == nil || d.gate == nil || d.creator == nil {
		return nil, errors.New("execution v2 dispatcher is not configured")
	}
	if request.OrganizationID == uuid.Nil || request.DeploymentTargetID == uuid.Nil ||
		request.ExecutionID == uuid.Nil ||
		strings.TrimSpace(request.StepKey) == "" {
		return nil, errors.New("execution v2 dispatch request is invalid")
	}
	decision, err := d.gate.EvaluateExecutionV2Admission(ctx, AdmissionRequest{
		OrganizationID: request.OrganizationID, DeploymentTargetID: request.DeploymentTargetID,
		EnvironmentID: request.EnvironmentID, PlanID: request.PlanID,
		TaskID: request.TaskID, StepRunID: request.StepRunID,
		StepKey: strings.TrimSpace(request.StepKey),
	})
	if err != nil {
		return nil, fmt.Errorf("evaluate execution v2 admission: %w", err)
	}
	if reason := decision.denialReason(); reason != "" {
		return nil, fmt.Errorf("execution v2 admission denied: %s", reason)
	}
	return d.creator.CreateExecutionAttempt(ctx, CreateAttemptRequest{
		OrganizationID: request.OrganizationID, DeploymentTargetID: request.DeploymentTargetID,
		ExecutionID: request.ExecutionID,
		PlanID:      request.PlanID, TaskID: request.TaskID, StepRunID: request.StepRunID,
		StepKey: strings.TrimSpace(request.StepKey), Retry: request.Retry,
	})
}

type V1Dispatcher interface {
	DispatchExecutionV1(context.Context, DispatchRequest) error
}

type ProtocolDispatcher struct {
	v1         V1Dispatcher
	v2         *Dispatcher
	readySteps ReadyStepRunsLoader
}

type ReadyStepRunsLoader interface {
	LoadExecutionV2ReadyStepRuns(context.Context, uuid.UUID, uuid.UUID) ([]types.StepRun, error)
}

type repositoryReadyStepRunsLoader struct{}

func (repositoryReadyStepRunsLoader) LoadExecutionV2ReadyStepRuns(
	ctx context.Context,
	organizationID, taskID uuid.UUID,
) ([]types.StepRun, error) {
	return db.GetExecutionV2ReadyStepRuns(ctx, organizationID, taskID)
}

type protocolDispatcherContextKey struct{}

func WithProtocolDispatcher(ctx context.Context, dispatcher *ProtocolDispatcher) context.Context {
	return context.WithValue(ctx, protocolDispatcherContextKey{}, dispatcher)
}

// DispatchCreatedTasks is the production handoff from durable task creation.
// V1 remains on the established lease workers; V2 is admitted and persisted as
// a signed attempt and never falls through into the V1 lease path.
func DispatchCreatedTasks(ctx context.Context, tasks []types.Task) error {
	for _, task := range tasks {
		if task.ProtocolVersion == types.ExecutionProtocolVersionV1 {
			continue
		}
		if err := DispatchReadyTaskSteps(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

// DispatchRecoveredTasks dispatches the ready StepRuns loaded by the bounded
// recovery query. It deliberately does not reload each task's steps, keeping a
// recovery tick to a fixed number of database queries.
func DispatchRecoveredTasks(ctx context.Context, tasks []types.Task) error {
	for _, task := range tasks {
		if task.ProtocolVersion == types.ExecutionProtocolVersionV1 {
			continue
		}
		dispatcher, ok := ctx.Value(protocolDispatcherContextKey{}).(*ProtocolDispatcher)
		if !ok || dispatcher == nil {
			return errors.New("execution protocol dispatcher is not configured")
		}
		if err := dispatchReadyTaskSteps(ctx, dispatcher, task, task.StepRuns); err != nil {
			return err
		}
	}
	return nil
}

func DispatchTaskRetry(ctx context.Context, task types.Task) error {
	if task.ProtocolVersion != types.ExecutionProtocolVersionV2 {
		return errors.New("execution retry requires protocol v2")
	}
	return dispatchTaskRetry(ctx, task)
}

func DispatchReadyTaskSteps(ctx context.Context, task types.Task) error {
	dispatcher, ok := ctx.Value(protocolDispatcherContextKey{}).(*ProtocolDispatcher)
	if !ok || dispatcher == nil {
		return errors.New("execution protocol dispatcher is not configured")
	}
	if task.ProtocolVersion != types.ExecutionProtocolVersionV2 {
		return nil
	}
	if dispatcher.readySteps == nil {
		return errors.New("execution v2 ready-step repository is not configured")
	}
	steps, err := dispatcher.readySteps.LoadExecutionV2ReadyStepRuns(
		ctx, task.OrganizationID, task.ID,
	)
	if err != nil {
		return fmt.Errorf("load execution v2 ready steps: %w", err)
	}
	return dispatchReadyTaskSteps(ctx, dispatcher, task, steps)
}

func dispatchReadyTaskSteps(
	ctx context.Context,
	dispatcher *ProtocolDispatcher,
	task types.Task,
	steps []types.StepRun,
) error {
	for _, step := range steps {
		if _, err := dispatcher.Dispatch(ctx, task.ProtocolVersion, DispatchRequest{
			OrganizationID: task.OrganizationID, DeploymentTargetID: task.DeploymentTargetID,
			EnvironmentID: task.EnvironmentID, ExecutionID: task.ID,
			PlanID: task.DeploymentPlanID, TaskID: task.ID,
			StepRunID: step.ID, StepKey: step.StepKey,
		}); err != nil {
			return err
		}
	}
	return nil
}

func dispatchTaskRetry(ctx context.Context, task types.Task) error {
	dispatcher, ok := ctx.Value(protocolDispatcherContextKey{}).(*ProtocolDispatcher)
	if !ok || dispatcher == nil {
		return errors.New("execution protocol dispatcher is not configured")
	}
	var step *types.StepRun
	for i := range task.StepRuns {
		if task.StepRuns[i].Status == types.StepRunStatusPending {
			step = &task.StepRuns[i]
			break
		}
	}
	if step == nil {
		return errors.New("execution v2 task has no pending step")
	}
	_, err := dispatcher.Dispatch(ctx, task.ProtocolVersion, DispatchRequest{
		OrganizationID: task.OrganizationID, DeploymentTargetID: task.DeploymentTargetID,
		EnvironmentID: task.EnvironmentID, ExecutionID: task.ID,
		PlanID: task.DeploymentPlanID, TaskID: task.ID,
		StepRunID: step.ID, StepKey: step.StepKey, Retry: true,
	})
	return err
}

func NewProtocolDispatcher(v1 V1Dispatcher, v2 *Dispatcher) *ProtocolDispatcher {
	return &ProtocolDispatcher{
		v1: v1, v2: v2, readySteps: repositoryReadyStepRunsLoader{},
	}
}

func (d *ProtocolDispatcher) Dispatch(
	ctx context.Context,
	version types.ExecutionProtocolVersion,
	request DispatchRequest,
) (*types.ExecutionAttempt, error) {
	switch version {
	case types.ExecutionProtocolVersionV1:
		if d == nil || d.v1 == nil {
			return nil, errors.New("execution v1 dispatcher is not configured")
		}
		return nil, d.v1.DispatchExecutionV1(ctx, request)
	case types.ExecutionProtocolVersionV2:
		if d == nil || d.v2 == nil {
			return nil, errors.New("execution v2 dispatcher is not configured")
		}
		return d.v2.Dispatch(ctx, request)
	default:
		return nil, errors.New("execution protocol version is invalid")
	}
}

type FrozenAttemptInputs struct {
	AttemptNumber                int
	PlanChecksum                 string
	ArtifactDigest               string
	ConfigChecksum               string
	AdapterRevision              string
	ResourceKey                  string
	FenceGeneration              int64
	Cancellable                  bool
	RetrySafe                    bool
	IntentTTL                    time.Duration
	PublicKeyFingerprint         string
	SigningKeyReference          string
	SigningKeyVersionFingerprint string
}

type FrozenAttemptInputsLoader interface {
	LoadFrozenAttemptInputs(context.Context, CreateAttemptRequest) (FrozenAttemptInputs, error)
}

type RepositoryAttemptCreator struct {
	loader         FrozenAttemptInputsLoader
	signerProvider IntentSignerProvider
}

type IntentSignerProvider interface {
	ResolveIntentSigner(context.Context, string, string) (executionprotocol.IntentSigner, error)
}

func NewRepositoryAttemptCreator(
	loader FrozenAttemptInputsLoader,
	signerProvider IntentSignerProvider,
) *RepositoryAttemptCreator {
	return &RepositoryAttemptCreator{loader: loader, signerProvider: signerProvider}
}

func (c *RepositoryAttemptCreator) CreateExecutionAttempt(
	ctx context.Context,
	request CreateAttemptRequest,
) (*types.ExecutionAttempt, error) {
	if c == nil || c.loader == nil || c.signerProvider == nil {
		return nil, errors.New("repository attempt creator is not configured")
	}
	inputs, err := c.loader.LoadFrozenAttemptInputs(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("load frozen execution inputs: %w", err)
	}
	signer, err := resolveFrozenIntentSigner(ctx, c.signerProvider, inputs)
	if err != nil {
		return nil, err
	}
	now, err := db.GetTrustedExecutionTime(ctx)
	if err != nil {
		return nil, err
	}
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), OrganizationID: request.OrganizationID,
		DeploymentTargetID: request.DeploymentTargetID,
		TaskID:             request.TaskID, StepRunID: request.StepRunID,
		Identity: types.ExecutionIdentity{
			ExecutionID: request.ExecutionID, AttemptNumber: inputs.AttemptNumber,
			StepKey: strings.TrimSpace(request.StepKey),
		},
		Status:       types.ExecutionAttemptStatusPending,
		PlanChecksum: inputs.PlanChecksum, ArtifactDigest: inputs.ArtifactDigest,
		ConfigChecksum: inputs.ConfigChecksum, AdapterRevision: inputs.AdapterRevision,
		IntentIssuedAt: now, IntentExpiresAt: now.Add(inputs.IntentTTL),
		Cancellable: inputs.Cancellable, RetrySafe: inputs.RetrySafe,
		Fence: types.ExecutionFence{
			ResourceKey: inputs.ResourceKey, Generation: inputs.FenceGeneration,
		},
	}
	intent, err := executionprotocol.BuildExecutionIntent(
		executionprotocol.WithIntentSigner(ctx, signer), attempt,
	)
	if err != nil {
		return nil, err
	}
	return db.CreateExecutionAttempt(ctx, attempt, intent, types.TrustPolicy{
		Keys: map[string]ed25519.PublicKey{signer.KeyID(): signer.PublicKey()},
	})
}

func resolveFrozenIntentSigner(
	ctx context.Context,
	provider IntentSignerProvider,
	inputs FrozenAttemptInputs,
) (executionprotocol.IntentSigner, error) {
	if provider == nil || strings.TrimSpace(inputs.SigningKeyReference) == "" ||
		strings.TrimSpace(inputs.SigningKeyVersionFingerprint) == "" ||
		strings.TrimSpace(inputs.PublicKeyFingerprint) == "" {
		return nil, errors.New("frozen adapter signing lineage is incomplete")
	}
	signer, err := provider.ResolveIntentSigner(
		ctx, inputs.SigningKeyReference, inputs.SigningKeyVersionFingerprint,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve frozen adapter signing key: %w", err)
	}
	if signer == nil || signer.KeyID() != inputs.PublicKeyFingerprint {
		return nil, errors.New("resolved signer does not match frozen adapter public-key fingerprint")
	}
	return signer, nil
}
