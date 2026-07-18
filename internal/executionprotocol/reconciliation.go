package executionprotocol

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

const reconciliationSigningDomain = "distr.reconciliation-evidence.v1"

type reconciliationVerifierContextKey struct{}
type reconciliationObserverGateContextKey struct{}

type ReconciliationEvidenceVerifier interface {
	VerifyReconciliationEvidence(
		context.Context,
		types.SignedReconciliationEvidence,
	) (types.ReconciliationEvidence, error)
}

func WithReconciliationEvidenceVerifier(
	ctx context.Context,
	verifier ReconciliationEvidenceVerifier,
) context.Context {
	return context.WithValue(ctx, reconciliationVerifierContextKey{}, verifier)
}

func WithReconciliationObserverGate(
	ctx context.Context,
	gate ReconciliationObserverGate,
) context.Context {
	return context.WithValue(ctx, reconciliationObserverGateContextKey{}, gate)
}

func VerifyImportedReconciliationEvidence(
	ctx context.Context,
	signed types.SignedReconciliationEvidence,
) (types.ReconciliationEvidence, error) {
	verifier, ok := ctx.Value(reconciliationVerifierContextKey{}).(ReconciliationEvidenceVerifier)
	if !ok || verifier == nil {
		return types.ReconciliationEvidence{}, errors.New("reconciliation evidence verifier is not configured")
	}
	evidence, err := verifier.VerifyReconciliationEvidence(ctx, signed)
	if err != nil {
		return types.ReconciliationEvidence{}, err
	}
	gate, ok := ctx.Value(reconciliationObserverGateContextKey{}).(ReconciliationObserverGate)
	if !ok || gate == nil {
		return types.ReconciliationEvidence{}, ErrObserverNotAuthorized
	}
	if err := gate.AuthorizeReconciliationObserver(ctx, evidence); err != nil {
		return types.ReconciliationEvidence{}, err
	}
	return evidence, nil
}

type Ed25519ReconciliationEvidenceVerifier struct {
	Keys map[string]ed25519.PublicKey
}

func BuildReconciliationEvidence(
	ctx context.Context,
	evidence types.ReconciliationEvidence,
	signer IntentSigner,
) (types.SignedReconciliationEvidence, error) {
	if signer == nil {
		return types.SignedReconciliationEvidence{}, errors.New("reconciliation evidence signer is required")
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return types.SignedReconciliationEvidence{}, fmt.Errorf("marshal reconciliation evidence: %w", err)
	}
	sum := sha256.Sum256(payload)
	checksum := "sha256:" + hex.EncodeToString(sum[:])
	signature, err := signer.Sign(ctx, reconciliationSigningMessage(payload, checksum))
	if err != nil {
		return types.SignedReconciliationEvidence{}, fmt.Errorf("sign reconciliation evidence: %w", err)
	}
	return types.SignedReconciliationEvidence{
		Payload: payload, Checksum: checksum, KeyID: signer.KeyID(),
		Signature: encodeSignature(signature),
	}, nil
}

func (v Ed25519ReconciliationEvidenceVerifier) VerifyReconciliationEvidence(
	_ context.Context,
	signed types.SignedReconciliationEvidence,
) (types.ReconciliationEvidence, error) {
	sum := sha256.Sum256(signed.Payload)
	checksum := "sha256:" + hex.EncodeToString(sum[:])
	if checksum != signed.Checksum {
		return types.ReconciliationEvidence{}, errors.New("reconciliation evidence checksum mismatch")
	}
	publicKey, ok := v.Keys[signed.KeyID]
	if !ok || PublicKeyFingerprint(publicKey) != signed.KeyID {
		return types.ReconciliationEvidence{}, errors.New("reconciliation evidence key is not trusted")
	}
	signature, err := decodeSignature(signed.Signature)
	if err != nil {
		return types.ReconciliationEvidence{}, err
	}
	if !ed25519.Verify(
		publicKey,
		reconciliationSigningMessage(signed.Payload, signed.Checksum),
		signature,
	) {
		return types.ReconciliationEvidence{}, errors.New("reconciliation evidence signature is invalid")
	}
	var evidence types.ReconciliationEvidence
	if err := json.Unmarshal(signed.Payload, &evidence); err != nil {
		return types.ReconciliationEvidence{}, fmt.Errorf("reconciliation evidence payload is invalid: %w", err)
	}
	if evidence.OrganizationID == uuid.Nil || evidence.ExecutionID == uuid.Nil ||
		evidence.AttemptID == uuid.Nil || evidence.StatusQueryID == uuid.Nil ||
		evidence.EventIdentity == uuid.Nil || !evidence.Outcome.IsValid() ||
		!intentChecksumPattern.MatchString(evidence.EvidenceChecksum) ||
		evidence.ObservedAt.IsZero() || strings.TrimSpace(evidence.ObserverID) == "" {
		return types.ReconciliationEvidence{}, errors.New("reconciliation evidence identity is invalid")
	}
	return evidence, nil
}

func reconciliationSigningMessage(payload []byte, checksum string) []byte {
	result := make([]byte, 0, len(reconciliationSigningDomain)+len(checksum)+len(payload)+2)
	result = append(result, reconciliationSigningDomain...)
	result = append(result, '\n')
	result = append(result, checksum...)
	result = append(result, '\n')
	result = append(result, payload...)
	return result
}
