package executionprotocol

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestSignedIntentGoldenAndTamperCases(t *testing.T) {
	g := NewWithT(t)
	seed := sha256.Sum256([]byte("distr-pr-075-golden-signing-key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyID := PublicKeyFingerprint(publicKey)
	issuedAt := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	attempt := types.ExecutionAttempt{
		ID:             uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		OrganizationID: uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"),
		Identity: types.ExecutionIdentity{
			ExecutionID:   uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
			AttemptNumber: 2,
			StepKey:       "deploy",
		},
		PlanChecksum:    "sha256:" + repeatHex("11"),
		ArtifactDigest:  "sha256:" + repeatHex("22"),
		ConfigChecksum:  "sha256:" + repeatHex("33"),
		AdapterRevision: "adapter.compose@2",
		Fence: types.ExecutionFence{
			ResourceKey:    "deployment-target:dddddddd-dddd-4ddd-8ddd-dddddddddddd",
			Generation:     7,
			LeaseExpiresAt: issuedAt.Add(5 * time.Minute),
		},
		IntentIssuedAt:  issuedAt,
		IntentExpiresAt: issuedAt.Add(5 * time.Minute),
	}

	signer, err := NewEd25519IntentSigner(keyID, privateKey)
	g.Expect(err).NotTo(HaveOccurred())
	signed, err := BuildExecutionIntent(WithIntentSigner(context.Background(), signer), attempt)
	g.Expect(err).NotTo(HaveOccurred())

	expectedPayload := `{"schema":"distr.execution-intent/v2","executionId":"cccccccc-cccc-4ccc-8ccc-cccccccccccc","attemptNumber":2,"stepKey":"deploy","planChecksum":"sha256:` +
		repeatHex("11") + `","artifactDigest":"sha256:` + repeatHex("22") +
		`","configChecksum":"sha256:` + repeatHex("33") +
		`","adapterRevision":"adapter.compose@2","resourceKey":"deployment-target:dddddddd-dddd-4ddd-8ddd-dddddddddddd","fenceGeneration":7,"issuedAt":"2026-07-18T00:00:00Z","expiresAt":"2026-07-18T00:05:00Z"}`
	g.Expect(string(signed.Payload)).To(Equal(expectedPayload))
	sum := sha256.Sum256([]byte(expectedPayload))
	g.Expect(signed.Checksum).To(Equal("sha256:" + hex.EncodeToString(sum[:])))
	g.Expect(signed.KeyID).To(Equal(keyID))
	g.Expect(signed.Signature).NotTo(BeEmpty())

	policy := types.TrustPolicy{
		Keys:                   map[string]ed25519.PublicKey{keyID: publicKey},
		Now:                    func() time.Time { return issuedAt.Add(time.Minute) },
		ExpectedArtifactDigest: attempt.ArtifactDigest,
		ExpectedConfigChecksum: attempt.ConfigChecksum,
	}
	g.Expect(VerifyExecutionIntent(signed, policy)).To(Succeed())

	tampered := signed
	tampered.Payload = append([]byte(nil), signed.Payload...)
	tampered.Payload[len(tampered.Payload)-2] ^= 1
	g.Expect(VerifyExecutionIntent(tampered, policy)).To(MatchError(ContainSubstring("checksum")))

	wrongSeed := sha256.Sum256([]byte("wrong-key"))
	wrongKey := ed25519.NewKeyFromSeed(wrongSeed[:]).Public().(ed25519.PublicKey)
	wrongPolicy := policy
	wrongPolicy.Keys = map[string]ed25519.PublicKey{keyID: wrongKey}
	g.Expect(VerifyExecutionIntent(signed, wrongPolicy)).To(MatchError(ContainSubstring("fingerprint")))

	expired := policy
	expired.Now = func() time.Time { return issuedAt.Add(6 * time.Minute) }
	g.Expect(VerifyExecutionIntent(signed, expired)).To(MatchError(ContainSubstring("expired")))

	configMismatch := policy
	configMismatch.ExpectedConfigChecksum = "sha256:" + repeatHex("44")
	g.Expect(VerifyExecutionIntent(signed, configMismatch)).To(MatchError(ContainSubstring("config checksum")))

	artifactMismatch := policy
	artifactMismatch.ExpectedArtifactDigest = "sha256:" + repeatHex("55")
	g.Expect(VerifyExecutionIntent(signed, artifactMismatch)).To(MatchError(ContainSubstring("artifact digest")))
}

func TestTrustPolicyOverlapAndRevocation(t *testing.T) {
	g := NewWithT(t)
	seedA := sha256.Sum256([]byte("key-a"))
	seedB := sha256.Sum256([]byte("key-b"))
	keyA := ed25519.NewKeyFromSeed(seedA[:]).Public().(ed25519.PublicKey)
	keyB := ed25519.NewKeyFromSeed(seedB[:]).Public().(ed25519.PublicKey)
	idA := PublicKeyFingerprint(keyA)
	idB := PublicKeyFingerprint(keyB)
	policy := types.TrustPolicy{
		Keys:          map[string]ed25519.PublicKey{idA: keyA, idB: keyB},
		RevokedKeyIDs: map[string]time.Time{idA: time.Date(2026, 7, 18, 1, 0, 0, 0, time.UTC)},
	}
	g.Expect(ValidateTrustPolicy(policy)).To(Succeed())
	g.Expect(policy.Keys).To(HaveLen(2))
	g.Expect(idA).NotTo(Equal(idB))
}

func repeatHex(pair string) string {
	result := ""
	for range 32 {
		result += pair
	}
	return result
}
