package db

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestExecutionV2RepositoryRequestValidation(t *testing.T) {
	g := NewWithT(t)
	request := types.ClaimRequest{
		OrganizationID: uuid.New(), AttemptID: uuid.New(), ExecutorID: "executor-a", ExpectedGeneration: 1,
		Now: time.Now().UTC(), LeaseDuration: time.Minute,
	}
	g.Expect(validateExecutionV2ClaimRequest(request)).To(Succeed())
	request.ExpectedGeneration = 0
	g.Expect(validateExecutionV2ClaimRequest(request)).To(MatchError(ContainSubstring("generation")))
}

func TestExecutionV2AttemptInsertValidation(t *testing.T) {
	g := NewWithT(t)
	seed := sha256.Sum256([]byte("repository-key"))
	publicKey := ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), OrganizationID: uuid.New(), TaskID: uuid.New(), StepRunID: uuid.New(),
		Identity: types.ExecutionIdentity{
			ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy",
		},
		Status:       types.ExecutionAttemptStatusPending,
		PlanChecksum: "sha256:" + repeatDBHex("11"), ArtifactDigest: "sha256:" + repeatDBHex("22"),
		ConfigChecksum: "sha256:" + repeatDBHex("33"), AdapterRevision: "adapter.compose@2",
		IntentIssuedAt: time.Now().UTC(), IntentExpiresAt: time.Now().UTC().Add(time.Minute),
		Fence: types.ExecutionFence{ResourceKey: "target:1", Generation: 1},
	}
	intent := types.SignedExecutionIntent{
		Payload:   []byte(`{}`),
		Signature: base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)),
	}
	payloadSum := sha256.Sum256(intent.Payload)
	keySum := sha256.Sum256(publicKey)
	intent.Checksum = "sha256:" + hex.EncodeToString(payloadSum[:])
	intent.KeyID = "sha256:" + hex.EncodeToString(keySum[:])
	g.Expect(validateNewExecutionAttempt(attempt, intent)).To(Succeed())
	attempt.Status = types.ExecutionAttemptStatusRunning
	g.Expect(validateNewExecutionAttempt(attempt, intent)).To(MatchError(ContainSubstring("PENDING")))
}

func repeatDBHex(pair string) string {
	result := ""
	for range 32 {
		result += pair
	}
	return result
}

func TestCancelStatusAndReconciliationRepositoryValidation(t *testing.T) {
	g := NewWithT(t)
	cancel := types.CancelRequest{
		OrganizationID: uuid.New(), ExecutionID: uuid.New(), RequestedBy: uuid.New(),
		IdempotencyKey: "cancel-1", Reason: "operator requested", RequestedAt: time.Now().UTC(),
	}
	g.Expect(validateCancelRequest(cancel)).To(Succeed())
	cancel.IdempotencyKey = ""
	g.Expect(validateCancelRequest(cancel)).To(MatchError(ContainSubstring("idempotency")))

	reconciliation := types.ReconciliationStatusInput{
		OrganizationID: uuid.New(), ExecutionID: uuid.New(), StatusQueryID: uuid.New(),
		EventIdentity: uuid.New(), Outcome: types.ReconciliationOutcomeUnknown,
		EvidenceChecksum: "sha256:" + repeatDBHex("dd"), ObservedAt: time.Now().UTC(),
	}
	g.Expect(validateReconciliationStatusInput(reconciliation)).To(Succeed())
}
