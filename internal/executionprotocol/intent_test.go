package executionprotocol

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
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
		ID:                 uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		OrganizationID:     uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"),
		DeploymentTargetID: uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd"),
		TaskID:             uuid.MustParse("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"),
		StepRunID:          uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff"),
		Identity: types.ExecutionIdentity{
			ExecutionID:   uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
			AttemptNumber: 2,
			StepKey:       "deploy",
		},
		PlanChecksum:                  "sha256:" + repeatHex("11"),
		ArtifactDigest:                "sha256:" + repeatHex("22"),
		ConfigChecksum:                "sha256:" + repeatHex("33"),
		AdapterRevision:               "adapter.compose@2",
		RuntimeContractVersion:        types.ExecutionRuntimeContractVersionV3,
		ExpectedObservedStateVersion:  17,
		ExpectedObservedStateChecksum: "sha256:" + repeatHex("44"),
		ExpectedCurrentImageDigest:    "sha256:" + repeatHex("55"),
		ExpectedCurrentConfigChecksum: "sha256:" + repeatHex("66"),
		ExpectedPlatform:              types.DeploymentTargetPlatformLinuxAMD64,
		CallerBinding:                 "urn:distr:caller:organization:bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb:deployment-target:dddddddd-dddd-4ddd-8ddd-dddddddddddd",
		Audience:                      "urn:distr:audience:adapter-assignment:99999999-9999-4999-8999-999999999999",
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

	expectedPayload := `{"schema":"distr.execution-intent/v3","organizationId":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","deploymentTargetId":"dddddddd-dddd-4ddd-8ddd-dddddddddddd","attemptId":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","taskId":"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee","stepRunId":"ffffffff-ffff-4fff-8fff-ffffffffffff","executionId":"cccccccc-cccc-4ccc-8ccc-cccccccccccc","attemptNumber":2,"stepKey":"deploy","planChecksum":"sha256:` +
		repeatHex("11") + `","artifactDigest":"sha256:` + repeatHex("22") +
		`","configChecksum":"sha256:` + repeatHex("33") +
		`","adapterRevision":"adapter.compose@2","runtimeContractVersion":"v3","expectedObservedStateVersion":17,"expectedObservedStateChecksum":"sha256:` + repeatHex("44") +
		`","expectedCurrentImageDigest":"sha256:` + repeatHex("55") +
		`","expectedCurrentConfigChecksum":"sha256:` + repeatHex("66") +
		`","expectedPlatform":"linux/amd64","callerBinding":"urn:distr:caller:organization:bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb:deployment-target:dddddddd-dddd-4ddd-8ddd-dddddddddddd","audience":"urn:distr:audience:adapter-assignment:99999999-9999-4999-8999-999999999999","resourceKey":"deployment-target:dddddddd-dddd-4ddd-8ddd-dddddddddddd","fenceGeneration":7,"issuedAt":"2026-07-18T00:00:00Z","expiresAt":"2026-07-18T00:05:00Z"}`
	g.Expect(string(signed.Payload)).To(Equal(expectedPayload))
	sum := sha256.Sum256([]byte(expectedPayload))
	g.Expect(signed.Checksum).To(Equal("sha256:" + hex.EncodeToString(sum[:])))
	g.Expect(signed.KeyID).To(Equal(keyID))
	g.Expect(signed.Signature).NotTo(BeEmpty())

	policy := types.TrustPolicy{
		Keys:                          map[string]ed25519.PublicKey{keyID: publicKey},
		Now:                           func() time.Time { return issuedAt.Add(time.Minute) },
		ExpectedArtifactDigest:        attempt.ArtifactDigest,
		ExpectedConfigChecksum:        attempt.ConfigChecksum,
		ExpectedObservedStateVersion:  attempt.ExpectedObservedStateVersion,
		ExpectedObservedStateChecksum: attempt.ExpectedObservedStateChecksum,
		ExpectedCurrentImageDigest:    attempt.ExpectedCurrentImageDigest,
		ExpectedCurrentConfigChecksum: attempt.ExpectedCurrentConfigChecksum,
		ExpectedPlatform:              attempt.ExpectedPlatform,
		ExpectedCallerBinding:         attempt.CallerBinding,
		ExpectedAudience:              attempt.Audience,
	}
	g.Expect(VerifyExecutionIntent(signed, policy)).To(Succeed())
	g.Expect(ValidateExecutionIntentBinding(attempt, signed)).To(Succeed())
	differentAttempt := attempt
	differentAttempt.DeploymentTargetID = uuid.New()
	g.Expect(ValidateExecutionIntentBinding(differentAttempt, signed)).
		To(MatchError(ContainSubstring("binding")))

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

func TestSignedIntentV4BindsSeparateRuntimeChecksumIdentities(t *testing.T) {
	g := NewWithT(t)
	seed := sha256.Sum256([]byte("distr-runtime-checksum-contract-v4"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyID := PublicKeyFingerprint(publicKey)
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		TaskID: uuid.New(), StepRunID: uuid.New(),
		Identity:     types.ExecutionIdentity{ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy"},
		PlanChecksum: checksumForIntentTest("1"), ArtifactDigest: checksumForIntentTest("2"),
		ConfigChecksum:                       checksumForIntentTest("3"),
		RuntimeManifestChecksum:              checksumForIntentTest("4"),
		DesiredServiceConfigChecksum:         checksumForIntentTest("5"),
		AdapterRevision:                      "adapter.compose@2",
		RuntimeContractVersion:               types.ExecutionRuntimeContractVersionV4,
		ExpectedObservedStateVersion:         9,
		ExpectedObservedStateChecksum:        checksumForIntentTest("6"),
		ExpectedCurrentImageDigest:           checksumForIntentTest("7"),
		ExpectedCurrentConfigChecksum:        checksumForIntentTest("8"),
		ExpectedCurrentServiceConfigChecksum: checksumForIntentTest("9"),
		ExpectedPlatform:                     types.DeploymentTargetPlatformLinuxAMD64,
		CallerBinding:                        "urn:distr:caller:test", Audience: "urn:distr:audience:test",
		Fence:          types.ExecutionFence{ResourceKey: "deployment-target:test", Generation: 3},
		IntentIssuedAt: now, IntentExpiresAt: now.Add(5 * time.Minute),
	}
	signer, err := NewEd25519IntentSigner(keyID, privateKey)
	g.Expect(err).NotTo(HaveOccurred())
	signed, err := BuildExecutionIntent(WithIntentSigner(context.Background(), signer), attempt)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(signed.Payload)).To(ContainSubstring(`"schema":"distr.execution-intent/v4"`))
	g.Expect(string(signed.Payload)).To(ContainSubstring(`"runtimeManifestChecksum":"` + attempt.RuntimeManifestChecksum + `"`))
	g.Expect(string(signed.Payload)).To(ContainSubstring(`"desiredServiceConfigChecksum":"` + attempt.DesiredServiceConfigChecksum + `"`))
	g.Expect(string(signed.Payload)).To(ContainSubstring(`"expectedCurrentServiceConfigChecksum":"` + attempt.ExpectedCurrentServiceConfigChecksum + `"`))

	policy := types.TrustPolicy{
		Keys: map[string]ed25519.PublicKey{keyID: publicKey}, Now: func() time.Time { return now.Add(time.Minute) },
		ExpectedArtifactDigest:               attempt.ArtifactDigest,
		ExpectedConfigChecksum:               attempt.ConfigChecksum,
		ExpectedRuntimeManifestChecksum:      attempt.RuntimeManifestChecksum,
		ExpectedDesiredServiceConfigChecksum: attempt.DesiredServiceConfigChecksum,
		ExpectedObservedStateVersion:         attempt.ExpectedObservedStateVersion,
		ExpectedObservedStateChecksum:        attempt.ExpectedObservedStateChecksum,
		ExpectedCurrentImageDigest:           attempt.ExpectedCurrentImageDigest,
		ExpectedCurrentConfigChecksum:        attempt.ExpectedCurrentConfigChecksum,
		ExpectedCurrentServiceConfigChecksum: attempt.ExpectedCurrentServiceConfigChecksum,
		ExpectedPlatform:                     attempt.ExpectedPlatform,
		ExpectedCallerBinding:                attempt.CallerBinding, ExpectedAudience: attempt.Audience,
	}
	g.Expect(VerifyExecutionIntent(signed, policy)).To(Succeed())
	g.Expect(ValidateExecutionIntentBinding(attempt, signed)).To(Succeed())

	mismatch := policy
	mismatch.ExpectedDesiredServiceConfigChecksum = checksumForIntentTest("a")
	g.Expect(VerifyExecutionIntent(signed, mismatch)).
		To(MatchError(ContainSubstring("desired service config checksum")))

	legacyWithV4Fields := attempt
	legacyWithV4Fields.RuntimeContractVersion = types.ExecutionRuntimeContractVersionV3
	_, err = BuildExecutionIntent(WithIntentSigner(context.Background(), signer), legacyWithV4Fields)
	g.Expect(err).To(MatchError(ContainSubstring("v3 runtime checksum identities must be empty")))
}

func checksumForIntentTest(value string) string {
	return "sha256:" + strings.Repeat(value, 64)
}

func repeatHex(pair string) string {
	result := ""
	for range 32 {
		result += pair
	}
	return result
}
