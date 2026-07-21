package db

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"os"
	"strings"
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

func TestPendingDesiredInputForExecutionAttemptUsesFrozenDeployStep(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	organizationID := uuid.New()
	planID := uuid.New()
	unitID := uuid.New()
	componentID := uuid.New()
	targetID := uuid.New()
	artifactDigest := "sha256:" + repeatDBHex("22")
	configChecksum := "sha256:" + repeatDBHex("33")
	releaseChecksum := "sha256:" + repeatDBHex("44")
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), OrganizationID: organizationID, DeploymentTargetID: targetID,
		Identity: types.ExecutionIdentity{
			ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "component:api:deploy",
		},
		PlanChecksum:   "sha256:" + repeatDBHex("11"),
		ArtifactDigest: artifactDigest, ConfigChecksum: configChecksum,
	}
	canonical := types.TargetDeploymentPlanCanonical{
		Schema: types.TargetDeploymentPlanSchemaV2, DeploymentUnitID: unitID,
		DeploymentTargetID: targetID, TargetConfigSnapshotChecksum: configChecksum,
		TargetPlatform: "linux/amd64", ProtocolVersion: types.DeploymentPlanProtocolV2,
		ComponentReleasePins: []types.ComponentReleasePin{{
			ComponentKey: "api", Version: "2026.07.22", ReleaseChecksum: releaseChecksum,
			PlatformDigest: artifactDigest, Platforms: []string{"linux/amd64"},
		}},
		ComponentBindings: []types.ConfigComponentBinding{{
			ComponentKey: "api", ComponentInstanceID: componentID, PhysicalName: "choice-api",
		}},
		Graph: types.TargetPlanGraph{Steps: []types.TargetPlanStep{{
			StepKey: "component:api:deploy", ComponentKey: "api",
			ComponentInstanceID: &componentID, ActionName: "component.deploy",
			TimeoutSeconds: 900,
		}}},
	}

	input, err := pendingDesiredInputForExecutionAttempt(
		attempt, planID, canonical, now,
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(input).NotTo(BeNil())
	g.Expect(input.ExecutionAttemptID).To(Equal(attempt.ID))
	g.Expect(input.ExecutionID).To(Equal(attempt.Identity.ExecutionID))
	g.Expect(input.DeploymentPlanID).To(Equal(planID))
	g.Expect(input.DeploymentUnitID).To(Equal(unitID))
	g.Expect(input.ComponentInstanceID).To(Equal(componentID))
	g.Expect(input.ArtifactDigest).To(Equal(artifactDigest))
	g.Expect(input.ConfigChecksum).To(Equal(configChecksum))
	g.Expect(input.SchemaVersion).To(Equal("2026.07.22"))
	g.Expect(input.CapabilityChecksum).To(Equal(releaseChecksum))
	g.Expect(input.ObservationDeadline).To(Equal(now.Add(15 * time.Minute)))
	g.Expect(input.TopologyChecksum).To(MatchRegexp(`^sha256:[0-9a-f]{64}$`))

	canonical.Graph.Steps[0].ActionName = "component.health"
	input, err = pendingDesiredInputForExecutionAttempt(attempt, planID, canonical, now)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(input).To(BeNil())

	canonical.Graph.Steps[0].ActionName = "component.deploy"
	canonical.ComponentReleasePins[0].PlatformDigest = "sha256:" + strings.Repeat("5", 64)
	_, err = pendingDesiredInputForExecutionAttempt(attempt, planID, canonical, now)
	g.Expect(err).To(MatchError(ContainSubstring("artifact digest")))
}

func TestExecutionAttemptOutcomeMappingIsFailClosed(t *testing.T) {
	g := NewWithT(t)
	tests := []struct {
		status  types.ExecutionAttemptStatus
		outcome types.ExecutorOutcome
	}{
		{types.ExecutionAttemptStatusSucceeded, types.ExecutorOutcomeSucceeded},
		{types.ExecutionAttemptStatusFailed, types.ExecutorOutcomeFailed},
		{types.ExecutionAttemptStatusCanceled, types.ExecutorOutcomeCancelled},
		{types.ExecutionAttemptStatusTimedOut, types.ExecutorOutcomeFailed},
		{types.ExecutionAttemptStatusFenced, types.ExecutorOutcomeUnknown},
		{types.ExecutionAttemptStatusUnknown, types.ExecutorOutcomeUnknown},
	}
	for _, test := range tests {
		outcome, err := executorOutcomeForAttemptStatus(test.status)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(outcome).To(Equal(test.outcome))
	}
	_, err := executorOutcomeForAttemptStatus(types.ExecutionAttemptStatusRunning)
	g.Expect(err).To(HaveOccurred())
}

func TestDesiredTerminalStatusMapsToExactAttemptProjection(t *testing.T) {
	g := NewWithT(t)
	tests := []struct {
		pending types.PendingDesiredStatus
		attempt types.ExecutionAttemptStatus
	}{
		{types.PendingDesiredStatusVerified, types.ExecutionAttemptStatusSucceeded},
		{types.PendingDesiredStatusCancelled, types.ExecutionAttemptStatusCanceled},
		{types.PendingDesiredStatusPartial, types.ExecutionAttemptStatusFailed},
		{types.PendingDesiredStatusFailed, types.ExecutionAttemptStatusFailed},
		{types.PendingDesiredStatusUnknown, types.ExecutionAttemptStatusUnknown},
		{types.PendingDesiredStatusTimedOut, types.ExecutionAttemptStatusFailed},
		{types.PendingDesiredStatusConflict, types.ExecutionAttemptStatusFailed},
	}
	for _, test := range tests {
		status, err := executionProjectionStatusForDesiredTerminal(test.pending)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(status).To(Equal(test.attempt))
	}
	_, err := executionProjectionStatusForDesiredTerminal(types.PendingDesiredStatusPending)
	g.Expect(err).To(MatchError(ContainSubstring("not terminal")))
}

func TestTerminalExecutionEventIsAuditOnly(t *testing.T) {
	g := NewWithT(t)
	source, err := os.ReadFile("execution_v2.go")
	g.Expect(err).NotTo(HaveOccurred())
	text := string(source)
	start := strings.Index(text, "func RecordExecutionEvent(")
	end := strings.Index(text, "func CompleteExecutionAttempt(")
	g.Expect(start).To(BeNumerically(">=", 0))
	g.Expect(end).To(BeNumerically(">", start))
	eventPath := text[start:end]
	g.Expect(eventPath).NotTo(ContainSubstring("recordExecutionAttemptReportsTx"))
	g.Expect(eventPath).NotTo(ContainSubstring("projectExecutionV2Terminal"))
	g.Expect(eventPath).NotTo(ContainSubstring("ReconcilePendingDesiredRevision"))
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
