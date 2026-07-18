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
	retried   uuid.UUID
}

func (s *campaignBridgeStub) CancelCampaignExecution(_ context.Context, id uuid.UUID) error {
	s.cancelled = id
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
	g.Expect(coordinator.Cancel(context.Background(), executionID)).To(Succeed())
	g.Expect(coordinator.Retry(
		context.Background(), executionID, types.RetryDispositionAllowed,
	)).To(Succeed())
	g.Expect(bridge.cancelled).To(Equal(executionID))
	g.Expect(bridge.retried).To(Equal(executionID))
}
