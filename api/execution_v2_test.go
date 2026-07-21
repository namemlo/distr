package api

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestExecutionV2EventRequestValidation(t *testing.T) {
	g := NewWithT(t)
	request := ExecutionV2EventRequest{
		ExecutorID: "executor-a", ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy",
		FenceGeneration: 2, EventSequence: 1, Status: types.ExecutionEventStatusRunning,
		PayloadChecksum: "sha256:" + repeatAPIHex("ab"), OccurredAt: time.Now().UTC(),
	}
	g.Expect(request.Validate()).To(Succeed())
	request.EventSequence = 0
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("eventSequence")))
}

func TestExecutionV2ClaimRequiresExecutorIdentity(t *testing.T) {
	g := NewWithT(t)
	request := ExecutionV2ClaimRequest{
		AttemptID: uuid.New(), ExecutorID: "executor-a", ExpectedGeneration: 1,
		LeaseSeconds: 30,
	}
	g.Expect(request.Validate()).To(Succeed())
	request.ExecutorID = " "
	g.Expect(request.Validate()).To(MatchError(ContainSubstring("executorId")))
}

func TestCancelStatusAndReconciliationRequests(t *testing.T) {
	g := NewWithT(t)
	cancel := ExecutionCancelRequest{
		IdempotencyKey: "cancel-1", Reason: "operator requested",
	}
	g.Expect(cancel.Validate()).To(Succeed())
	cancel.Reason = ""
	g.Expect(cancel.Validate()).To(MatchError(ContainSubstring("reason")))

	status := ExecutionStatusRequest{
		IdempotencyKey: "status-1", Reason: "callback missing", ExpiresInSeconds: 60,
	}
	g.Expect(status.Validate()).To(Succeed())

	reconciliation := ExecutionReconciliationRequest{
		Evidence: types.SignedReconciliationEvidence{
			Payload:   []byte(`{"outcome":"UNKNOWN"}`),
			Checksum:  "sha256:" + repeatAPIHex("ef"),
			KeyID:     "sha256:" + repeatAPIHex("ab"),
			Signature: "signed",
		},
	}
	g.Expect(reconciliation.Validate()).To(Succeed())
	reconciliation.Evidence.Checksum = "not-a-checksum"
	g.Expect(reconciliation.Validate()).To(MatchError(ContainSubstring("signed reconciliation")))
}

func TestReconciliationEvidenceConversionPreservesAttemptBinding(t *testing.T) {
	g := NewWithT(t)
	evidence := types.ReconciliationEvidence{
		OrganizationID: uuid.New(), ExecutionID: uuid.New(), AttemptID: uuid.New(),
		StatusQueryID: uuid.New(), EventIdentity: uuid.New(),
		Outcome: types.ReconciliationOutcomeUnknown,
	}
	input := ReconciliationEvidenceToTypes(evidence, types.SignedReconciliationEvidence{})
	g.Expect(input.AttemptID).To(Equal(evidence.AttemptID))
}

func TestExecutionStatusRequestPreservesRequestedTTL(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 6, 0, 0, 0, time.UTC)
	request := ExecutionStatusRequest{
		IdempotencyKey: "status-1", Reason: "callback missing", ExpiresInSeconds: 75,
	}
	input := request.ToTypes(uuid.New(), uuid.New(), uuid.New(), now)
	g.Expect(input.RequestedTTLSeconds).To(Equal(75))
}

func repeatAPIHex(pair string) string {
	result := ""
	for range 32 {
		result += pair
	}
	return result
}
