package executionprotocol

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var ErrObserverNotAuthorized = errors.New("reconciliation observer is not authorized")

type ReconciliationObserverGate interface {
	AuthorizeReconciliationObserver(context.Context, types.ReconciliationEvidence) error
}

func ReconcileVerifiedEvidence(
	ctx context.Context,
	gate ReconciliationObserverGate,
	attempt types.ExecutionAttempt,
	evidence types.ReconciliationEvidence,
) (types.ReconciliationDecision, error) {
	if gate == nil {
		return types.ReconciliationDecision{}, ErrObserverNotAuthorized
	}
	if err := gate.AuthorizeReconciliationObserver(ctx, evidence); err != nil {
		return types.ReconciliationDecision{}, err
	}
	if attempt.Status.IsTerminal() {
		return types.ReconciliationDecision{}, errors.New("terminal execution attempt is immutable")
	}
	return ReconcileCallbackLoss(attempt, types.ReconciliationStatusInput{
		OrganizationID: evidence.OrganizationID, ExecutionID: evidence.ExecutionID,
		StatusQueryID: evidence.StatusQueryID, EventIdentity: evidence.EventIdentity,
		Outcome: evidence.Outcome, EvidenceChecksum: evidence.EvidenceChecksum,
		ObservedAt: evidence.ObservedAt, OperationIncomplete: evidence.OperationIncomplete,
		RetryRequested: evidence.RetryRequested,
	})
}

func EvaluateCancelRequest(
	attempt types.ExecutionAttempt,
	request types.CancelRequest,
) error {
	if request.ExecutionID == uuid.Nil || strings.TrimSpace(request.IdempotencyKey) == "" ||
		strings.TrimSpace(request.Reason) == "" {
		return errors.New("cancel request is invalid")
	}
	if attempt.Status.IsTerminal() {
		return errors.New("terminal execution attempt cannot be canceled")
	}
	if !attempt.Cancellable {
		return errors.New("execution step is not cancellable")
	}
	return nil
}

func IsExactDuplicateCancel(
	existing types.ExecutionCancelRequest,
	request types.CancelRequest,
) bool {
	return existing.ExecutionID == request.ExecutionID &&
		existing.IdempotencyKey == request.IdempotencyKey &&
		existing.Reason == request.Reason &&
		(existing.RequestedBy == uuid.Nil || request.RequestedBy == uuid.Nil ||
			existing.RequestedBy == request.RequestedBy)
}

func EvaluateRetryAfterCallbackLoss(
	attempt types.ExecutionAttempt,
	query *types.ExecutionStatusQuery,
	operationIncomplete bool,
) error {
	if attempt.AcknowledgedAt != nil &&
		(query == nil || query.Status != types.StatusQueryStatusReported) {
		return errors.New("reported status query is required before retry after acknowledged delivery")
	}
	if !attempt.RetrySafe {
		return errors.New("retry requires a declared-idempotent operation")
	}
	if !operationIncomplete {
		return errors.New("retry requires proof that the operation is incomplete")
	}
	return nil
}

func ValidateCallbackWindow(attempt types.ExecutionAttempt, callbackAt time.Time) error {
	if attempt.IntentExpiresAt.IsZero() || !callbackAt.UTC().Before(attempt.IntentExpiresAt.UTC()) {
		return errors.New("execution callback validity window is expired")
	}
	return nil
}

func ReconcileCallbackLoss(
	attempt types.ExecutionAttempt,
	input types.ReconciliationStatusInput,
) (types.ReconciliationDecision, error) {
	if input.EventIdentity == uuid.Nil || !input.Outcome.IsValid() ||
		!intentChecksumPattern.MatchString(input.EvidenceChecksum) ||
		input.ObservedAt.IsZero() {
		return types.ReconciliationDecision{}, errors.New("reconciliation status is invalid")
	}
	switch input.Outcome {
	case types.ReconciliationOutcomeProvenSucceeded:
		return types.ReconciliationDecision{
			Status:           types.ExecutionAttemptStatusSucceeded,
			RetryDisposition: types.RetryDispositionForbidden,
		}, nil
	case types.ReconciliationOutcomeProvenFailed:
		return types.ReconciliationDecision{
			Status:           types.ExecutionAttemptStatusFailed,
			RetryDisposition: types.RetryDispositionForbidden,
		}, nil
	case types.ReconciliationOutcomeUnknown:
		disposition := types.RetryDispositionNotRequested
		if input.RetryRequested {
			query := &types.ExecutionStatusQuery{Status: types.StatusQueryStatusReported}
			if err := EvaluateRetryAfterCallbackLoss(attempt, query, input.OperationIncomplete); err != nil {
				return types.ReconciliationDecision{}, err
			}
			disposition = types.RetryDispositionAllowed
		}
		return types.ReconciliationDecision{
			Status: types.ExecutionAttemptStatusUnknown, RetryDisposition: disposition,
		}, nil
	default:
		return types.ReconciliationDecision{}, errors.New("reconciliation outcome is invalid")
	}
}

func ValidateReconciliationEventIdentity(
	eventIdentity uuid.UUID,
	existing []types.ExecutionReconciliationEvent,
) error {
	if eventIdentity == uuid.Nil {
		return errors.New("reconciliation event identity is required")
	}
	for _, event := range existing {
		if event.EventIdentity == eventIdentity {
			return errors.New("reconciliation event identity must be new")
		}
	}
	return nil
}

type CampaignExecutionControlBridge interface {
	CancelCampaignExecution(context.Context, uuid.UUID) error
	RetryCampaignExecution(context.Context, uuid.UUID, types.RetryDisposition) error
}

type CampaignControlCoordinator struct {
	bridge CampaignExecutionControlBridge
}

type campaignControlCoordinatorContextKey struct{}

func WithCampaignControlCoordinator(
	ctx context.Context,
	coordinator *CampaignControlCoordinator,
) context.Context {
	return context.WithValue(ctx, campaignControlCoordinatorContextKey{}, coordinator)
}

func BridgeCampaignCancelIfConfigured(ctx context.Context, executionID uuid.UUID) error {
	coordinator, _ := ctx.Value(campaignControlCoordinatorContextKey{}).(*CampaignControlCoordinator)
	if coordinator == nil {
		return nil
	}
	return coordinator.Cancel(ctx, executionID)
}

func BridgeCampaignRetryIfConfigured(
	ctx context.Context,
	executionID uuid.UUID,
	disposition types.RetryDisposition,
) error {
	coordinator, _ := ctx.Value(campaignControlCoordinatorContextKey{}).(*CampaignControlCoordinator)
	if coordinator == nil {
		return nil
	}
	return coordinator.Retry(ctx, executionID, disposition)
}

func NewCampaignControlCoordinator(bridge CampaignExecutionControlBridge) *CampaignControlCoordinator {
	return &CampaignControlCoordinator{bridge: bridge}
}

func (c *CampaignControlCoordinator) Cancel(ctx context.Context, executionID uuid.UUID) error {
	if c == nil || c.bridge == nil {
		return errors.New("campaign execution control bridge is not configured")
	}
	return c.bridge.CancelCampaignExecution(ctx, executionID)
}

func (c *CampaignControlCoordinator) Retry(
	ctx context.Context,
	executionID uuid.UUID,
	disposition types.RetryDisposition,
) error {
	if c == nil || c.bridge == nil {
		return errors.New("campaign execution control bridge is not configured")
	}
	if disposition != types.RetryDispositionAllowed {
		return errors.New("campaign retry is not allowed")
	}
	return c.bridge.RetryCampaignExecution(ctx, executionID, disposition)
}
