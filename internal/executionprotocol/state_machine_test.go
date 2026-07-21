package executionprotocol

import (
	"testing"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
	. "github.com/onsi/gomega"
)

func TestExecutionV2StateMachineFencingAndIdempotency(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 1, 0, 0, 0, time.UTC)
	attempt := types.ExecutionAttempt{
		ID:       uuid.New(),
		Identity: types.ExecutionIdentity{ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy"},
		Status:   types.ExecutionAttemptStatusPending,
		Fence:    types.ExecutionFence{ResourceKey: "target:1", Generation: 3, LeaseExpiresAt: now.Add(time.Minute)},
	}
	machine, err := NewStateMachine(attempt)
	g.Expect(err).NotTo(HaveOccurred())

	claimed, err := machine.Claim(types.ClaimRequest{
		AttemptID: attempt.ID, ExecutorID: "executor-a", ExpectedGeneration: 3,
		Now: now, LeaseDuration: time.Minute,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(claimed.Status).To(Equal(types.ExecutionAttemptStatusClaimed))

	duplicate, err := machine.Claim(types.ClaimRequest{
		AttemptID: attempt.ID, ExecutorID: "executor-a", ExpectedGeneration: 3,
		Now: now.Add(time.Second), LeaseDuration: time.Minute,
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(duplicate.ClaimedBy).To(Equal("executor-a"))
	g.Expect(duplicate.AcknowledgedAt).To(BeNil())
	g.Expect(machine.Acknowledge(types.HeartbeatRequest{
		AttemptID: attempt.ID, ExecutorID: "executor-a", FenceGeneration: 3,
		Now: now.Add(time.Second),
	})).To(Succeed())
	g.Expect(machine.Attempt().AcknowledgedAt).NotTo(BeNil())

	first, duplicateEvent, err := machine.RecordEvent(types.ExecutionEventInput{
		AttemptID: attempt.ID, ExecutorID: "executor-a", Identity: attempt.Identity, FenceGeneration: 3,
		EventSequence: 1, Status: types.ExecutionEventStatusRunning, PayloadChecksum: "sha256:" + repeatHex("66"),
		OccurredAt: now.Add(2 * time.Second),
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(duplicateEvent).To(BeFalse())
	g.Expect(first.EventSequence).To(Equal(int64(1)))

	replayed, duplicateEvent, err := machine.RecordEvent(types.ExecutionEventInput{
		AttemptID: attempt.ID, ExecutorID: "executor-a", Identity: attempt.Identity, FenceGeneration: 3,
		EventSequence: 1, Status: types.ExecutionEventStatusRunning, PayloadChecksum: "sha256:" + repeatHex("66"),
		OccurredAt: now.Add(2 * time.Second),
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(duplicateEvent).To(BeTrue())
	g.Expect(replayed.ID).To(Equal(first.ID))

	_, _, err = machine.RecordEvent(types.ExecutionEventInput{
		AttemptID: attempt.ID, ExecutorID: "executor-a",
		Identity: attempt.Identity, FenceGeneration: 3,
		EventSequence: 1, Status: types.ExecutionEventStatusRunning,
		PayloadChecksum: "sha256:" + repeatHex("66"), Message: "changed",
		OccurredAt: now.Add(2 * time.Second),
	})
	g.Expect(err).To(MatchError(ContainSubstring("conflicting duplicate")))

	_, _, err = machine.RecordEvent(types.ExecutionEventInput{
		AttemptID: attempt.ID, ExecutorID: "executor-a", Identity: attempt.Identity, FenceGeneration: 3,
		EventSequence: 1, Status: types.ExecutionEventStatusRunning, PayloadChecksum: "sha256:" + repeatHex("77"),
		OccurredAt: now.Add(2 * time.Second),
	})
	g.Expect(err).To(MatchError(ContainSubstring("conflicting duplicate")))

	_, _, err = machine.RecordEvent(types.ExecutionEventInput{
		AttemptID: attempt.ID, ExecutorID: "executor-a", Identity: attempt.Identity, FenceGeneration: 3,
		EventSequence: 3, Status: types.ExecutionEventStatusRunning, PayloadChecksum: "sha256:" + repeatHex("88"),
		OccurredAt: now.Add(3 * time.Second),
	})
	g.Expect(err).To(MatchError(ContainSubstring("ordered")))

	fenced, err := machine.Fence("lease lost", now.Add(2*time.Minute))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fenced.Fence.Generation).To(Equal(int64(4)))
	g.Expect(fenced.Status).To(Equal(types.ExecutionAttemptStatusFenced))

	err = machine.Heartbeat(types.HeartbeatRequest{
		AttemptID: attempt.ID, ExecutorID: "executor-a", FenceGeneration: 3,
		Now: now.Add(2 * time.Minute), LeaseDuration: time.Minute,
	})
	g.Expect(err).To(MatchError(ContainSubstring("stale fence")))
}

func TestExecutionV2TerminalCompletionReleasesLease(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 2, 0, 0, 0, time.UTC)
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), Identity: types.ExecutionIdentity{ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy"},
		Status: types.ExecutionAttemptStatusClaimed, ClaimedBy: "executor-a",
		Fence: types.ExecutionFence{ResourceKey: "target:1", Generation: 1, LeaseExpiresAt: now.Add(time.Minute)},
	}
	machine, err := NewStateMachine(attempt)
	g.Expect(err).NotTo(HaveOccurred())
	err = machine.Complete(types.CompletionInput{
		AttemptID: attempt.ID, ExecutorID: "executor-a", FenceGeneration: 1,
		Status: types.ExecutionAttemptStatusSucceeded, CompletedAt: now,
	})
	g.Expect(err).NotTo(HaveOccurred())
	current := machine.Attempt()
	g.Expect(current.Status).To(Equal(types.ExecutionAttemptStatusSucceeded))
	g.Expect(current.Fence.LeaseExpiresAt.IsZero()).To(BeTrue())
	g.Expect(current.ClaimedBy).To(BeEmpty())
}

func TestExecutionV2CrashRecoveryTimeoutAndRestart(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 3, 0, 0, 0, time.UTC)
	identity := types.ExecutionIdentity{
		ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy",
	}

	beforeAcknowledge, err := NewStateMachine(types.ExecutionAttempt{
		ID: uuid.New(), Identity: identity, Status: types.ExecutionAttemptStatusPending,
		Fence: types.ExecutionFence{ResourceKey: "target:1", Generation: 1},
	})
	g.Expect(err).NotTo(HaveOccurred())
	_, err = beforeAcknowledge.Claim(types.ClaimRequest{
		AttemptID: beforeAcknowledge.Attempt().ID, ExecutorID: "executor-a",
		ExpectedGeneration: 1, Now: now, LeaseDuration: time.Minute,
	})
	g.Expect(err).NotTo(HaveOccurred())

	afterAcknowledge := beforeAcknowledge.Attempt()
	restarted, err := NewStateMachine(afterAcknowledge)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = restarted.Claim(types.ClaimRequest{
		AttemptID: afterAcknowledge.ID, ExecutorID: "executor-b",
		ExpectedGeneration: 1, Now: now.Add(time.Second), LeaseDuration: time.Minute,
	})
	g.Expect(err).To(MatchError(ContainSubstring("cannot be claimed")))

	running := afterAcknowledge
	running.Status = types.ExecutionAttemptStatusRunning
	running.LastEventSequence = 1
	restarted, err = NewStateMachine(running)
	g.Expect(err).NotTo(HaveOccurred())
	_, duplicate, err := restarted.RecordEvent(types.ExecutionEventInput{
		AttemptID: running.ID, ExecutorID: "executor-a", Identity: identity, FenceGeneration: 1,
		EventSequence: 2, Status: types.ExecutionEventStatusRunning,
		PayloadChecksum: "sha256:" + repeatHex("99"), OccurredAt: now.Add(2 * time.Second),
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(duplicate).To(BeFalse())
	err = restarted.Complete(types.CompletionInput{
		AttemptID: running.ID, ExecutorID: "executor-a", FenceGeneration: 1,
		Status: types.ExecutionAttemptStatusTimedOut, CompletedAt: now.Add(30 * time.Second),
		FailureReason: "deadline exceeded",
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(restarted.Attempt().Status).To(Equal(types.ExecutionAttemptStatusTimedOut))
}

func TestExecutionV2HeartbeatRejectsExpiredIntent(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC)
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), Identity: types.ExecutionIdentity{
			ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy",
		},
		Status:          types.ExecutionAttemptStatusRunning,
		ClaimedBy:       "executor-a",
		IntentExpiresAt: now.Add(-time.Second),
		Fence: types.ExecutionFence{
			ResourceKey: "target:1", Generation: 1, LeaseExpiresAt: now.Add(time.Minute),
		},
	}
	machine, err := NewStateMachine(attempt)
	g.Expect(err).NotTo(HaveOccurred())

	err = machine.Heartbeat(types.HeartbeatRequest{
		AttemptID: attempt.ID, ExecutorID: "executor-a", FenceGeneration: 1,
		Now: now, LeaseDuration: time.Minute,
	})
	g.Expect(err).To(MatchError(ContainSubstring("intent")))
}

func TestExecutionV2ExactEventReplaySurvivesTerminalCompletion(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 5, 0, 0, 0, time.UTC)
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), Identity: types.ExecutionIdentity{
			ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy",
		},
		Status: types.ExecutionAttemptStatusRunning, ClaimedBy: "executor-a",
		IntentExpiresAt: now.Add(time.Minute),
		Fence: types.ExecutionFence{
			ResourceKey: "target:1", Generation: 1, LeaseExpiresAt: now.Add(time.Minute),
		},
	}
	machine, err := NewStateMachine(attempt)
	g.Expect(err).NotTo(HaveOccurred())
	eventInput := types.ExecutionEventInput{
		AttemptID: attempt.ID, ExecutorID: "executor-a", Identity: attempt.Identity,
		FenceGeneration: 1, EventSequence: 1, Status: types.ExecutionEventStatusRunning,
		PayloadChecksum: "sha256:" + repeatHex("ab"), OccurredAt: now,
	}
	first, duplicate, err := machine.RecordEvent(eventInput)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(duplicate).To(BeFalse())
	g.Expect(machine.Complete(types.CompletionInput{
		AttemptID: attempt.ID, ExecutorID: "executor-a", FenceGeneration: 1,
		Status: types.ExecutionAttemptStatusSucceeded, CompletedAt: now.Add(time.Second),
	})).To(Succeed())

	replayed, duplicate, err := machine.RecordEvent(eventInput)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(duplicate).To(BeTrue())
	g.Expect(replayed.ID).To(Equal(first.ID))
}

func TestExecutionV2ExactEventReplaySurvivesFenceGenerationAdvance(t *testing.T) {
	g := NewWithT(t)
	now := time.Date(2026, 7, 18, 5, 30, 0, 0, time.UTC)
	attempt := types.ExecutionAttempt{
		ID: uuid.New(), Identity: types.ExecutionIdentity{
			ExecutionID: uuid.New(), AttemptNumber: 1, StepKey: "deploy",
		},
		Status: types.ExecutionAttemptStatusRunning, ClaimedBy: "executor-a",
		Fence: types.ExecutionFence{
			ResourceKey: "target:1", Generation: 3, LeaseExpiresAt: now.Add(time.Minute),
		},
	}
	machine, err := NewStateMachine(attempt)
	g.Expect(err).NotTo(HaveOccurred())
	eventInput := types.ExecutionEventInput{
		AttemptID: attempt.ID, ExecutorID: "executor-a", Identity: attempt.Identity,
		FenceGeneration: 3, EventSequence: 1, Status: types.ExecutionEventStatusRunning,
		PayloadChecksum: "sha256:" + repeatHex("cd"), OccurredAt: now,
	}
	first, _, err := machine.RecordEvent(eventInput)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = machine.Fence("lease expired", now.Add(time.Minute))
	g.Expect(err).NotTo(HaveOccurred())

	replayed, duplicate, err := machine.RecordEvent(eventInput)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(duplicate).To(BeTrue())
	g.Expect(replayed.ID).To(Equal(first.ID))
}
