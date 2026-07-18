package executionprotocol

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

var intentChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type intentSignerContextKey struct{}

func WithIntentSigner(ctx context.Context, signer IntentSigner) context.Context {
	return context.WithValue(ctx, intentSignerContextKey{}, signer)
}

type canonicalIntent struct {
	Schema          string    `json:"schema"`
	ExecutionID     uuid.UUID `json:"executionId"`
	AttemptNumber   int       `json:"attemptNumber"`
	StepKey         string    `json:"stepKey"`
	PlanChecksum    string    `json:"planChecksum"`
	ArtifactDigest  string    `json:"artifactDigest"`
	ConfigChecksum  string    `json:"configChecksum"`
	AdapterRevision string    `json:"adapterRevision"`
	ResourceKey     string    `json:"resourceKey"`
	FenceGeneration int64     `json:"fenceGeneration"`
	IssuedAt        time.Time `json:"issuedAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

func BuildExecutionIntent(
	ctx context.Context,
	attempt types.ExecutionAttempt,
) (types.SignedExecutionIntent, error) {
	signer, ok := ctx.Value(intentSignerContextKey{}).(IntentSigner)
	if !ok || signer == nil {
		return types.SignedExecutionIntent{}, errors.New("intent signer is not configured")
	}
	if err := validateIntentAttempt(attempt); err != nil {
		return types.SignedExecutionIntent{}, err
	}
	value := canonicalIntent{
		Schema: "distr.execution-intent/v2", ExecutionID: attempt.Identity.ExecutionID,
		AttemptNumber: attempt.Identity.AttemptNumber, StepKey: strings.TrimSpace(attempt.Identity.StepKey),
		PlanChecksum: attempt.PlanChecksum, ArtifactDigest: attempt.ArtifactDigest,
		ConfigChecksum: attempt.ConfigChecksum, AdapterRevision: strings.TrimSpace(attempt.AdapterRevision),
		ResourceKey: strings.TrimSpace(attempt.Fence.ResourceKey), FenceGeneration: attempt.Fence.Generation,
		IssuedAt: attempt.IntentIssuedAt.UTC(), ExpiresAt: attempt.IntentExpiresAt.UTC(),
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return types.SignedExecutionIntent{}, fmt.Errorf("marshal execution intent: %w", err)
	}
	sum := sha256.Sum256(payload)
	checksum := "sha256:" + hex.EncodeToString(sum[:])
	signature, err := signer.Sign(ctx, signingMessage(payload, checksum))
	if err != nil {
		return types.SignedExecutionIntent{}, fmt.Errorf("sign execution intent: %w", err)
	}
	return types.SignedExecutionIntent{
		Payload: payload, Checksum: checksum, KeyID: signer.KeyID(), Signature: encodeSignature(signature),
	}, nil
}

func VerifyExecutionIntent(intent types.SignedExecutionIntent, policy types.TrustPolicy) error {
	if err := ValidateTrustPolicy(policy); err != nil {
		return err
	}
	sum := sha256.Sum256(intent.Payload)
	checksum := "sha256:" + hex.EncodeToString(sum[:])
	if checksum != intent.Checksum {
		return errors.New("execution intent checksum mismatch")
	}
	publicKey, ok := policy.Keys[intent.KeyID]
	if !ok {
		return errors.New("execution intent keyId is not trusted")
	}
	if PublicKeyFingerprint(publicKey) != intent.KeyID {
		return errors.New("execution intent key fingerprint mismatch")
	}
	signature, err := decodeSignature(intent.Signature)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, signingMessage(intent.Payload, intent.Checksum), signature) {
		return errors.New("execution intent signature is invalid")
	}
	var payload canonicalIntent
	if err := json.Unmarshal(intent.Payload, &payload); err != nil {
		return fmt.Errorf("execution intent payload is invalid: %w", err)
	}
	if payload.Schema != "distr.execution-intent/v2" {
		return errors.New("execution intent schema is invalid")
	}
	now := time.Now().UTC()
	if policy.Now != nil {
		now = policy.Now().UTC()
	}
	if !payload.ExpiresAt.After(now) {
		return errors.New("execution intent is expired")
	}
	if revokedAt, revoked := policy.RevokedKeyIDs[intent.KeyID]; revoked && !now.Before(revokedAt) {
		return errors.New("execution intent key is revoked")
	}
	if policy.ExpectedArtifactDigest != "" && payload.ArtifactDigest != policy.ExpectedArtifactDigest {
		return errors.New("execution intent artifact digest mismatch")
	}
	if policy.ExpectedConfigChecksum != "" && payload.ConfigChecksum != policy.ExpectedConfigChecksum {
		return errors.New("execution intent config checksum mismatch")
	}
	return nil
}

func ValidateTrustPolicy(policy types.TrustPolicy) error {
	if len(policy.Keys) == 0 {
		return errors.New("trust policy has no public keys")
	}
	for keyID, publicKey := range policy.Keys {
		if len(publicKey) != ed25519.PublicKeySize {
			return fmt.Errorf("trust policy key %q is not Ed25519", keyID)
		}
		if PublicKeyFingerprint(publicKey) != keyID {
			return fmt.Errorf("trust policy key %q does not match its public-key fingerprint", keyID)
		}
	}
	return nil
}

func validateIntentAttempt(attempt types.ExecutionAttempt) error {
	if attempt.Identity.ExecutionID == uuid.Nil || attempt.Identity.AttemptNumber <= 0 ||
		strings.TrimSpace(attempt.Identity.StepKey) == "" {
		return errors.New("execution identity is invalid")
	}
	if !intentChecksumPattern.MatchString(attempt.PlanChecksum) {
		return errors.New("plan checksum is invalid")
	}
	if !intentChecksumPattern.MatchString(attempt.ArtifactDigest) {
		return errors.New("artifact digest is invalid")
	}
	if !intentChecksumPattern.MatchString(attempt.ConfigChecksum) {
		return errors.New("config checksum is invalid")
	}
	if strings.TrimSpace(attempt.AdapterRevision) == "" {
		return errors.New("adapter revision is required")
	}
	if strings.TrimSpace(attempt.Fence.ResourceKey) == "" || attempt.Fence.Generation <= 0 {
		return errors.New("execution fence is invalid")
	}
	if attempt.IntentIssuedAt.IsZero() || !attempt.IntentExpiresAt.After(attempt.IntentIssuedAt) {
		return errors.New("execution intent validity interval is invalid")
	}
	return nil
}
