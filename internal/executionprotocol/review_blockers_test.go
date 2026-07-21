package executionprotocol

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

type observerGateStub struct {
	called bool
	allow  bool
}

func (s *observerGateStub) AuthorizeReconciliationObserver(
	context.Context, types.ReconciliationEvidence,
) error {
	s.called = true
	if !s.allow {
		return ErrObserverNotAuthorized
	}
	return nil
}

type campaignBridgeStub struct {
	cancelled uuid.UUID
	cancelID  uuid.UUID
	retried   uuid.UUID
}

func (s *campaignBridgeStub) CancelCampaignExecution(
	_ context.Context, id, cancelID uuid.UUID,
) error {
	s.cancelled = id
	s.cancelID = cancelID
	return nil
}

func (s *campaignBridgeStub) RetryCampaignExecution(
	_ context.Context, id uuid.UUID, _ types.RetryDisposition,
) error {
	s.retried = id
	return nil
}

func TestReconciliationRequiresInvokedObserverGateAndPreservesTerminalState(t *testing.T) {
	g := NewWithT(t)
	evidence := types.ReconciliationEvidence{
		OrganizationID: uuid.New(), ExecutionID: uuid.New(), AttemptID: uuid.New(),
		StatusQueryID: uuid.New(), EventIdentity: uuid.New(),
		Outcome: types.ReconciliationOutcomeUnknown, ObservedAt: time.Now().UTC(),
		EvidenceChecksum: "sha256:" + repeatHex("aa"),
	}
	gate := &observerGateStub{}
	_, err := ReconcileVerifiedEvidence(context.Background(), gate, types.ExecutionAttempt{}, evidence)
	g.Expect(err).To(MatchError(ErrObserverNotAuthorized))
	g.Expect(gate.called).To(BeTrue())

	gate.allow = true
	terminal := types.ExecutionAttempt{Status: types.ExecutionAttemptStatusSucceeded}
	_, err = ReconcileVerifiedEvidence(context.Background(), gate, terminal, evidence)
	g.Expect(err).To(MatchError(ContainSubstring("terminal")))
}

func TestImportedReconciliationEvidenceRequiresIndependentSignatureAndObserver(t *testing.T) {
	g := NewWithT(t)
	seed := sha256.Sum256([]byte("independent-observer"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyID := PublicKeyFingerprint(publicKey)
	signer, err := NewEd25519IntentSigner(keyID, privateKey)
	g.Expect(err).NotTo(HaveOccurred())
	evidence := types.ReconciliationEvidence{
		OrganizationID: uuid.New(), ExecutionID: uuid.New(), AttemptID: uuid.New(),
		StatusQueryID: uuid.New(), EventIdentity: uuid.New(),
		Outcome:          types.ReconciliationOutcomeUnknown,
		EvidenceChecksum: "sha256:" + repeatHex("bb"),
		ObservedAt:       time.Now().UTC(), ObserverID: "observer-a",
	}
	signed, err := BuildReconciliationEvidence(context.Background(), evidence, signer)
	g.Expect(err).NotTo(HaveOccurred())
	gate := &observerGateStub{allow: true}
	ctx := WithReconciliationEvidenceVerifier(
		context.Background(),
		Ed25519ReconciliationEvidenceVerifier{
			Keys: map[string]ed25519.PublicKey{keyID: publicKey},
		},
	)
	ctx = WithReconciliationObserverGate(ctx, gate)
	got, err := VerifyImportedReconciliationEvidence(ctx, signed)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got).To(Equal(evidence))
	g.Expect(gate.called).To(BeTrue())

	tampered := signed
	tampered.Payload = append([]byte(nil), signed.Payload...)
	tampered.Payload[0] ^= 1
	_, err = VerifyImportedReconciliationEvidence(ctx, tampered)
	g.Expect(err).To(MatchError(ContainSubstring("checksum")))
}

func TestCampaignControlCoordinatorInvokesBridge(t *testing.T) {
	g := NewWithT(t)
	bridge := &campaignBridgeStub{}
	coordinator := NewCampaignControlCoordinator(bridge)
	executionID := uuid.New()
	cancelID := uuid.New()
	g.Expect(coordinator.Cancel(context.Background(), executionID, cancelID)).To(Succeed())
	g.Expect(coordinator.Retry(
		context.Background(), executionID, types.RetryDispositionAllowed,
	)).To(Succeed())
	g.Expect(bridge.cancelled).To(Equal(executionID))
	g.Expect(bridge.cancelID).To(Equal(cancelID))
	g.Expect(bridge.retried).To(Equal(executionID))
}

func TestCampaignControlSeamFailsClosedWhenNotBound(t *testing.T) {
	g := NewWithT(t)
	executionID := uuid.New()
	g.Expect(BridgeCampaignCancelIfConfigured(context.Background(), executionID, uuid.New())).
		To(MatchError(ContainSubstring("not configured")))
	g.Expect(BridgeCampaignRetryIfConfigured(
		context.Background(), executionID, types.RetryDispositionAllowed,
	)).To(MatchError(ContainSubstring("not configured")))
}

func TestExecutionDispatchReplayMatchesFrozenInputsNotEnvelopeIdentity(t *testing.T) {
	g := NewWithT(t)
	existing := types.ExecutionAttempt{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		TaskID: uuid.New(), StepRunID: uuid.New(), Identity: types.ExecutionIdentity{
			ExecutionID: uuid.New(), AttemptNumber: 2, StepKey: "deploy",
		},
		PlanChecksum:   "sha256:" + repeatHex("11"),
		ArtifactDigest: "sha256:" + repeatHex("22"),
		ConfigChecksum: "sha256:" + repeatHex("33"), AdapterRevision: "compose@2",
		Cancellable: true, RetrySafe: true,
		Fence: types.ExecutionFence{ResourceKey: "target:1", Generation: 4},
	}
	replay := existing
	replay.ID = uuid.New()
	replay.IntentIssuedAt = time.Now().UTC()
	replay.IntentExpiresAt = replay.IntentIssuedAt.Add(time.Minute)
	replay.Fence.Generation = 1
	g.Expect(MatchesExecutionDispatch(existing, replay)).To(BeTrue())

	replay.ConfigChecksum = "sha256:" + repeatHex("44")
	g.Expect(MatchesExecutionDispatch(existing, replay)).To(BeFalse())
}

func TestExactReconciliationReplayCanResumeCampaignDelivery(t *testing.T) {
	g := NewWithT(t)
	input := types.ReconciliationStatusInput{
		OrganizationID: uuid.New(), ExecutionID: uuid.New(), AttemptID: uuid.New(),
		StatusQueryID: uuid.New(), EventIdentity: uuid.New(),
		Outcome:          types.ReconciliationOutcomeUnknown,
		EvidenceChecksum: "sha256:" + repeatHex("55"), ObservedAt: time.Now().UTC(),
		OperationIncomplete: true, RetryRequested: true,
		SignedEvidence: types.SignedReconciliationEvidence{
			Payload:  []byte(`{"outcome":"UNKNOWN"}`),
			Checksum: "sha256:" + repeatHex("66"), KeyID: "sha256:" + repeatHex("77"),
			Signature: "signature",
		},
	}
	existing := types.ExecutionReconciliationEvent{
		OrganizationID: input.OrganizationID, ExecutionID: input.ExecutionID,
		ExecutionAttemptID: input.AttemptID, StatusQueryID: input.StatusQueryID,
		EventIdentity: input.EventIdentity, Outcome: input.Outcome,
		EvidenceChecksum: input.EvidenceChecksum, EvidencePayload: input.SignedEvidence.Payload,
		EvidenceEnvelopeChecksum: input.SignedEvidence.Checksum,
		EvidenceKeyID:            input.SignedEvidence.KeyID, EvidenceSignature: input.SignedEvidence.Signature,
		ObservedAt: input.ObservedAt, OperationIncomplete: true, RetryRequested: true,
		RetryDisposition: types.RetryDispositionAllowed,
	}
	g.Expect(IsExactReconciliationReplay(existing, input, types.ExecutionReconciliationDecision{
		Status: types.ExecutionAttemptStatusUnknown, RetryDisposition: types.RetryDispositionAllowed,
	})).To(BeTrue())
}
