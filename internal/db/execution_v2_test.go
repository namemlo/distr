package db

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/executionprotocol"
	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestExecutionV2RepositoryRequestValidation(t *testing.T) {
	g := NewWithT(t)
	request := types.ClaimRequest{
		OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		AttemptID: uuid.New(), ExecutorID: "executor-a", ExpectedGeneration: 1,
		Now: time.Now().UTC(), LeaseDuration: time.Minute,
	}
	g.Expect(validateExecutionV2ClaimRequest(request)).To(Succeed())
	request.ExpectedGeneration = 0
	g.Expect(validateExecutionV2ClaimRequest(request)).To(MatchError(ContainSubstring("generation")))
}

func TestExecutionV2AttemptInsertValidation(t *testing.T) {
	g := NewWithT(t)
	seed := sha256.Sum256([]byte("repository-key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	keyID := executionprotocol.PublicKeyFingerprint(privateKey.Public().(ed25519.PublicKey))
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), OrganizationID: uuid.New(), DeploymentTargetID: uuid.New(),
		TaskID: uuid.New(), StepRunID: uuid.New(),
		Identity: types.ExecutionIdentity{
			ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy",
		},
		Status:       types.ExecutionAttemptStatusPending,
		PlanChecksum: "sha256:" + repeatDBHex("11"), ArtifactDigest: "sha256:" + repeatDBHex("22"),
		ConfigChecksum: "sha256:" + repeatDBHex("33"), AdapterRevision: "adapter.compose@2",
		IntentIssuedAt: time.Now().UTC(), IntentExpiresAt: time.Now().UTC().Add(time.Minute),
		Fence: types.ExecutionFence{ResourceKey: "target:1", Generation: 1},
	}
	signer, err := executionprotocol.NewEd25519IntentSigner(keyID, privateKey)
	g.Expect(err).NotTo(HaveOccurred())
	intent, err := executionprotocol.BuildExecutionIntent(
		executionprotocol.WithIntentSigner(context.Background(), signer), attempt,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(validateNewExecutionAttempt(attempt, intent)).To(Succeed())
	attempt.Status = types.ExecutionAttemptStatusRunning
	g.Expect(validateNewExecutionAttempt(attempt, intent)).To(MatchError(ContainSubstring("PENDING")))
}

func TestExecutionV2AttemptRequiresCanonicalCompleteTaskResourceSet(t *testing.T) {
	g := NewWithT(t)
	orgID, taskID := uuid.New(), uuid.New()
	locks := []types.TaskResourceLock{
		{
			OrganizationID: orgID, TaskID: taskID,
			ResourceType: types.TaskLockResourceTargetComponent, ResourceKey: "shared",
		},
		{
			OrganizationID: orgID, TaskID: taskID,
			ResourceType: types.TaskLockResourceCustom, ResourceKey: "shared",
		},
		{
			OrganizationID: orgID, TaskID: taskID,
			ResourceType: types.TaskLockResourceDeploymentTarget, ResourceKey: "choice-tp-dev",
		},
	}
	canonical, err := CanonicalExecutionFenceResourceKey(locks)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(canonical).To(HavePrefix("task-resource-set:sha256:"))

	attempt := types.ExecutionAttempt{
		OrganizationID: orgID, TaskID: taskID,
		Fence: types.ExecutionFence{ResourceKey: canonical, Generation: 1},
	}
	g.Expect(validateExecutionAttemptTaskResourceLocks(attempt, locks)).To(Succeed())

	firstOnly, err := CanonicalExecutionFenceResourceKey(locks[:1])
	g.Expect(err).NotTo(HaveOccurred())
	attempt.Fence.ResourceKey = firstOnly
	g.Expect(validateExecutionAttemptTaskResourceLocks(attempt, locks)).To(
		MatchError(ContainSubstring("complete typed task resource lock set")),
	)

	reordered := []types.TaskResourceLock{locks[2], locks[0], locks[1]}
	reorderedCanonical, err := CanonicalExecutionFenceResourceKey(reordered)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(reorderedCanonical).To(Equal(canonical))

	withoutCustomType, err := CanonicalExecutionFenceResourceKey([]types.TaskResourceLock{locks[0], locks[2]})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(withoutCustomType).NotTo(Equal(canonical))
}

func TestExecutionV2AttemptRejectsMissingTaskResourceLocks(t *testing.T) {
	g := NewWithT(t)
	_, err := CanonicalExecutionFenceResourceKey(nil)
	g.Expect(err).To(MatchError(ContainSubstring("at least one typed task resource lock")))
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
		OrganizationID: uuid.New(), ExecutionID: uuid.New(), AttemptID: uuid.New(),
		StatusQueryID: uuid.New(),
		EventIdentity: uuid.New(), Outcome: types.ReconciliationOutcomeUnknown,
		EvidenceChecksum: "sha256:" + repeatDBHex("dd"), ObservedAt: time.Now().UTC(),
		SignedEvidence: types.SignedReconciliationEvidence{
			Payload:  []byte(`{"outcome":"UNKNOWN"}`),
			Checksum: "sha256:" + repeatDBHex("aa"),
			KeyID:    "sha256:" + repeatDBHex("bb"), Signature: "signed",
		},
	}
	g.Expect(validateReconciliationStatusInput(reconciliation)).To(Succeed())
}
