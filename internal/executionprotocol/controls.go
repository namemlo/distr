package executionprotocol

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

func MatchesExecutionDispatch(existing, candidate types.ExecutionAttempt) bool {
	return existing.OrganizationID == candidate.OrganizationID &&
		existing.DeploymentTargetID == candidate.DeploymentTargetID &&
		existing.TaskID == candidate.TaskID &&
		existing.StepRunID == candidate.StepRunID &&
		existing.Identity == candidate.Identity &&
		existing.PlanChecksum == candidate.PlanChecksum &&
		existing.ArtifactDigest == candidate.ArtifactDigest &&
		existing.ConfigChecksum == candidate.ConfigChecksum &&
		existing.AdapterRevision == candidate.AdapterRevision &&
		existing.Cancellable == candidate.Cancellable &&
		existing.RetrySafe == candidate.RetrySafe &&
		existing.Fence.ResourceKey == candidate.Fence.ResourceKey
}

func IsExactExecutionEventReplay(
	existing types.ExecutionEvent,
	input types.ExecutionEventInput,
) bool {
	return existing.PayloadChecksum == input.PayloadChecksum &&
		existing.Status == input.Status && existing.Message == input.Message &&
		existing.OccurredAt.Equal(input.OccurredAt.UTC()) &&
		existing.Identity == input.Identity &&
		existing.FenceGeneration == input.FenceGeneration
}

func IsExactReconciliationReplay(
	existing types.ExecutionReconciliationEvent,
	input types.ReconciliationStatusInput,
	decision types.ReconciliationDecision,
) bool {
	return existing.OrganizationID == input.OrganizationID &&
		existing.ExecutionID == input.ExecutionID &&
		existing.ExecutionAttemptID == input.AttemptID &&
		existing.StatusQueryID == input.StatusQueryID &&
		existing.EventIdentity == input.EventIdentity &&
		existing.Outcome == input.Outcome &&
		existing.EvidenceChecksum == input.EvidenceChecksum &&
		bytes.Equal(existing.EvidencePayload, input.SignedEvidence.Payload) &&
		existing.EvidenceEnvelopeChecksum == input.SignedEvidence.Checksum &&
		existing.EvidenceKeyID == input.SignedEvidence.KeyID &&
		existing.EvidenceSignature == input.SignedEvidence.Signature &&
		existing.ObservedAt.Equal(input.ObservedAt.UTC()) &&
		existing.OperationIncomplete == input.OperationIncomplete &&
		existing.RetryRequested == input.RetryRequested &&
		existing.RetryDisposition == decision.RetryDisposition
}

func IsExactDuplicateStatus(
	existing types.ExecutionStatusQuery,
	request types.StatusRequest,
) bool {
	return existing.OrganizationID == request.OrganizationID &&
		existing.ExecutionID == request.ExecutionID &&
		existing.RequestedBy == request.RequestedBy &&
		existing.IdempotencyKey == request.IdempotencyKey &&
		existing.Reason == request.Reason &&
		existing.RequestedTTLSeconds == request.RequestedTTLSeconds
}

func ShouldFenceExpiredAttempt(attempt types.ExecutionAttempt, now time.Time) bool {
	if attempt.Status.IsTerminal() {
		return false
	}
	now = now.UTC()
	if !attempt.IntentExpiresAt.IsZero() && !attempt.IntentExpiresAt.After(now) {
		return true
	}
	if attempt.Status == types.ExecutionAttemptStatusClaimed ||
		attempt.Status == types.ExecutionAttemptStatusRunning {
		return attempt.Fence.LeaseExpiresAt.IsZero() ||
			!attempt.Fence.LeaseExpiresAt.After(now)
	}
	return false
}

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
		AttemptID:     evidence.AttemptID,
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
		return errors.New("campaign execution control bridge is not configured")
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
		return errors.New("campaign execution control bridge is not configured")
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
