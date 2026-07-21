package executionprotocol

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestCancelEligibilityAndDuplicateIdentity(t *testing.T) {
	g := NewWithT(t)
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), Status: types.ExecutionAttemptStatusRunning, Cancellable: true,
	}
	request := types.CancelRequest{
		ExecutionID: uuid.New(), IdempotencyKey: "cancel-1", Reason: "operator requested",
	}
	g.Expect(EvaluateCancelRequest(attempt, request)).To(Succeed())

	attempt.Cancellable = false
	g.Expect(EvaluateCancelRequest(attempt, request)).
		To(MatchError(ContainSubstring("not cancellable")))
	attempt.Cancellable = true
	attempt.Status = types.ExecutionAttemptStatusSucceeded
	g.Expect(EvaluateCancelRequest(attempt, request)).
		To(MatchError(ContainSubstring("terminal")))

	first := types.ExecutionCancelRequest{
		ExecutionID: request.ExecutionID, IdempotencyKey: request.IdempotencyKey,
		Reason: request.Reason,
	}
	g.Expect(IsExactDuplicateCancel(first, request)).To(BeTrue())
	request.Reason = "different"
	g.Expect(IsExactDuplicateCancel(first, request)).To(BeFalse())
}

func TestAcknowledgedDeliveryRequiresStatusBeforeRetry(t *testing.T) {
	g := NewWithT(t)
	acknowledgedAt := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), Status: types.ExecutionAttemptStatusRunning,
		AcknowledgedAt: &acknowledgedAt, RetrySafe: true,
	}
	g.Expect(EvaluateRetryAfterCallbackLoss(attempt, nil, true)).
		To(MatchError(ContainSubstring("status query")))

	query := &types.ExecutionStatusQuery{ID: uuid.New(), Status: types.StatusQueryStatusReported}
	g.Expect(EvaluateRetryAfterCallbackLoss(attempt, query, true)).To(Succeed())
	g.Expect(EvaluateRetryAfterCallbackLoss(attempt, query, false)).
		To(MatchError(ContainSubstring("incomplete")))

	attempt.RetrySafe = false
	g.Expect(EvaluateRetryAfterCallbackLoss(attempt, query, true)).
		To(MatchError(ContainSubstring("idempotent")))
}

func TestCallbackLossReconciliationNeverInventsSuccess(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 5, 0, 0, 0, time.UTC)
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), Status: types.ExecutionAttemptStatusRunning,
		IntentExpiresAt: now.Add(-time.Minute), RetrySafe: true,
	}
	g.Expect(ValidateCallbackWindow(attempt, now)).
		To(MatchError(ContainSubstring("expired")))

	cases := []struct {
		outcome types.ReconciliationOutcome
		status  types.ExecutionAttemptStatus
		retry   types.RetryDisposition
	}{
		{types.ReconciliationOutcomeProvenSucceeded, types.ExecutionAttemptStatusSucceeded, types.RetryDispositionForbidden},
		{types.ReconciliationOutcomeProvenFailed, types.ExecutionAttemptStatusFailed, types.RetryDispositionForbidden},
		{types.ReconciliationOutcomeUnknown, types.ExecutionAttemptStatusUnknown, types.RetryDispositionAllowed},
	}
	for _, tc := range cases {
		input := types.ReconciliationStatusInput{
			EventIdentity: uuid.New(), Outcome: tc.outcome,
			EvidenceChecksum: "sha256:" + repeatHex("aa"),
			ObservedAt:       now, OperationIncomplete: true, RetryRequested: true,
		}
		decision, err := ReconcileCallbackLoss(attempt, input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(decision.Status).To(Equal(tc.status))
		g.Expect(decision.RetryDisposition).To(Equal(tc.retry))
		if tc.outcome != types.ReconciliationOutcomeProvenSucceeded {
			g.Expect(decision.Status).NotTo(Equal(types.ExecutionAttemptStatusSucceeded))
		}
	}
}

func TestReconciliationRequiresNewEventIdentity(t *testing.T) {
	g := NewWithT(t)
	existing := types.ExecutionReconciliationEvent{EventIdentity: uuid.New()}
	g.Expect(ValidateReconciliationEventIdentity(existing.EventIdentity, []types.ExecutionReconciliationEvent{existing})).
		To(MatchError(ContainSubstring("identity")))
	g.Expect(ValidateReconciliationEventIdentity(uuid.New(), []types.ExecutionReconciliationEvent{existing})).
		To(Succeed())
}

func TestStatusQueryDuplicateRequiresSameRequestedTTL(t *testing.T) {
	g := NewWithT(t)
	existing := types.ExecutionStatusQuery{
		ExecutionID: uuid.New(), RequestedBy: uuid.New(), IdempotencyKey: "status-1",
		Reason: "callback missing", RequestedTTLSeconds: 60,
	}
	request := types.StatusRequest{
		ExecutionID: existing.ExecutionID, RequestedBy: existing.RequestedBy,
		IdempotencyKey: existing.IdempotencyKey, Reason: existing.Reason,
		RequestedTTLSeconds: 60,
	}
	g.Expect(IsExactDuplicateStatus(existing, request)).To(BeTrue())
	request.RequestedTTLSeconds = 90
	g.Expect(IsExactDuplicateStatus(existing, request)).To(BeFalse())
}

func TestExpiredAttemptRecoveryDecisionUsesLeaseAndIntent(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 7, 0, 0, 0, time.UTC)
	active := types.ExecutionAttempt{
		Status:          types.ExecutionAttemptStatusRunning,
		IntentExpiresAt: now.Add(time.Minute),
		Fence:           types.ExecutionFence{LeaseExpiresAt: now.Add(time.Minute)},
	}
	g.Expect(ShouldFenceExpiredAttempt(active, now)).To(BeFalse())

	leaseExpired := active
	leaseExpired.Fence.LeaseExpiresAt = now.Add(-time.Second)
	g.Expect(ShouldFenceExpiredAttempt(leaseExpired, now)).To(BeTrue())

	pendingIntentExpired := active
	pendingIntentExpired.Status = types.ExecutionAttemptStatusPending
	pendingIntentExpired.Fence.LeaseExpiresAt = time.Time{}
	pendingIntentExpired.IntentExpiresAt = now.Add(-time.Second)
	g.Expect(ShouldFenceExpiredAttempt(pendingIntentExpired, now)).To(BeTrue())
}
