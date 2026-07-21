package executionruntime

import (
	"context"
	"crypto/ed25519"
	"errors"

	"github.com/distr-sh/distr/internal/auth"
	"github.com/distr-sh/distr/internal/db"
	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/executionworker"
	"github.com/distr-sh/distr/internal/featureflags"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type ProductionConfig struct {
	Flags          featureflags.Registry
	SignerProvider executionworker.IntentSignerProvider
	ObserverKeys   map[string]ed25519.PublicKey
	Repository     executionworker.RuntimeRepository
	ObserverGate   executionprotocol.ReconciliationObserverGate
	CampaignBridge executionprotocol.CampaignExecutionControlBridge
}

func NewProductionDependencies(config ProductionConfig) (Dependencies, error) {
	if config.SignerProvider == nil {
		return Dependencies{}, errors.New("execution v2 intent signer provider is required")
	}
	if len(config.ObserverKeys) == 0 {
		return Dependencies{}, errors.New("execution v2 observer trust keys are required")
	}
	if config.Repository == nil {
		return Dependencies{}, errors.New("execution v2 runtime repository is required")
	}
	if config.ObserverGate == nil {
		config.ObserverGate = AuthenticatedReconciliationObserverGate{}
	}
	if config.CampaignBridge == nil {
		return Dependencies{}, errors.New("execution v2 campaign bridge is required")
	}
	gate := executionworker.NewRepositoryAdmissionGate(config.Flags, config.Repository)
	loader := executionworker.NewRepositoryFrozenAttemptInputsLoader(config.Repository)
	creator := executionworker.NewRepositoryAttemptCreator(loader, config.SignerProvider)
	return Dependencies{
		ProtocolDispatcher: executionworker.NewProtocolDispatcher(
			nil, executionworker.NewDispatcher(gate, creator),
		),
		ReconciliationEvidenceVerifier: executionprotocol.Ed25519ReconciliationEvidenceVerifier{
			Keys: config.ObserverKeys,
		},
		ReconciliationObserverGate: config.ObserverGate,
		CampaignControlCoordinator: executionprotocol.NewCampaignControlCoordinator(
			config.CampaignBridge,
		),
	}, nil
}

type AuthenticatedReconciliationObserverGate struct{}

func (AuthenticatedReconciliationObserverGate) AuthorizeReconciliationObserver(
	ctx context.Context,
	evidence types.ReconciliationEvidence,
) error {
	authInfo := auth.Authentication.Require(ctx)
	orgID := authInfo.CurrentOrgID()
	if orgID == nil || *orgID != evidence.OrganizationID ||
		authInfo.CurrentUserID().String() != evidence.ObserverID {
		return executionprotocol.ErrObserverNotAuthorized
	}
	return nil
}

type OperatorOrganizationResolver func(context.Context) (uuid.UUID, error)
type CampaignCancelHandoffRecorder func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
type ExecutionTaskLoader func(context.Context, uuid.UUID, uuid.UUID) (*types.Task, error)
type ExecutionTaskRetryDispatcher func(context.Context, types.Task) error

type TaskCampaignControlBridge struct {
	resolveOrganization OperatorOrganizationResolver
	recordCancel        CampaignCancelHandoffRecorder
	loadTask            ExecutionTaskLoader
	retryTask           ExecutionTaskRetryDispatcher
}

func NewTaskCampaignControlBridge(
	resolveOrganization OperatorOrganizationResolver,
	recordCancel CampaignCancelHandoffRecorder,
	loadTask ExecutionTaskLoader,
	retryTask ExecutionTaskRetryDispatcher,
) *TaskCampaignControlBridge {
	return &TaskCampaignControlBridge{
		resolveOrganization: resolveOrganization, recordCancel: recordCancel,
		loadTask: loadTask, retryTask: retryTask,
	}
}

func NewDatabaseCampaignControlBridge() *TaskCampaignControlBridge {
	return NewTaskCampaignControlBridge(
		func(ctx context.Context) (uuid.UUID, error) {
			authInfo := auth.Authentication.Require(ctx)
			orgID := authInfo.CurrentOrgID()
			if orgID == nil {
				return uuid.Nil, executionprotocol.ErrObserverNotAuthorized
			}
			return *orgID, nil
		},
		db.RecordCampaignExecutionCancelHandoff,
		db.GetTask,
		executionworker.DispatchTaskRetry,
	)
}

func (b *TaskCampaignControlBridge) CancelCampaignExecution(
	ctx context.Context,
	executionID, cancelRequestID uuid.UUID,
) error {
	if b == nil || b.resolveOrganization == nil || b.recordCancel == nil {
		return errors.New("campaign cancel handoff is not configured")
	}
	orgID, err := b.resolveOrganization(ctx)
	if err != nil {
		return err
	}
	return b.recordCancel(ctx, orgID, executionID, cancelRequestID)
}

func (b *TaskCampaignControlBridge) RetryCampaignExecution(
	ctx context.Context,
	executionID uuid.UUID,
	disposition types.RetryDisposition,
) error {
	if disposition != types.RetryDispositionAllowed {
		return errors.New("campaign retry is not allowed")
	}
	task, err := b.loadScopedTask(ctx, executionID)
	if err != nil {
		return err
	}
	if b.retryTask == nil {
		return errors.New("campaign retry dispatcher is not configured")
	}
	return b.retryTask(ctx, *task)
}

func (b *TaskCampaignControlBridge) loadScopedTask(
	ctx context.Context,
	executionID uuid.UUID,
) (*types.Task, error) {
	if b == nil || b.resolveOrganization == nil || b.loadTask == nil {
		return nil, errors.New("campaign task bridge is not configured")
	}
	orgID, err := b.resolveOrganization(ctx)
	if err != nil {
		return nil, err
	}
	return b.loadTask(ctx, executionID, orgID)
}
