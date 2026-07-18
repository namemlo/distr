package executionprotocol

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type StateMachine struct {
	mu      sync.Mutex
	attempt types.ExecutionAttempt
	events  map[int64]types.ExecutionEvent
}

func NewStateMachine(attempt types.ExecutionAttempt) (*StateMachine, error) {
	if attempt.ID == [16]byte{} || attempt.Identity.ExecutionID == [16]byte{} ||
		attempt.Identity.AttemptNumber <= 0 || strings.TrimSpace(attempt.Identity.StepKey) == "" {
		return nil, errors.New("execution attempt identity is invalid")
	}
	if attempt.Fence.Generation <= 0 || strings.TrimSpace(attempt.Fence.ResourceKey) == "" {
		return nil, errors.New("execution attempt fence is invalid")
	}
	return &StateMachine{attempt: attempt, events: map[int64]types.ExecutionEvent{}}, nil
}

func (m *StateMachine) Attempt() types.ExecutionAttempt {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.attempt
}

func (m *StateMachine) Claim(request types.ClaimRequest) (types.ExecutionAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if request.AttemptID != m.attempt.ID || strings.TrimSpace(request.ExecutorID) == "" {
		return types.ExecutionAttempt{}, errors.New("claim request is invalid")
	}
	if request.ExpectedGeneration != m.attempt.Fence.Generation {
		return types.ExecutionAttempt{}, errors.New("stale fence generation")
	}
	if m.attempt.Status == types.ExecutionAttemptStatusClaimed && m.attempt.ClaimedBy == request.ExecutorID {
		return m.attempt, nil
	}
	if m.attempt.Status != types.ExecutionAttemptStatusPending {
		return types.ExecutionAttempt{}, fmt.Errorf("attempt cannot be claimed from %s", m.attempt.Status)
	}
	m.attempt.Status = types.ExecutionAttemptStatusClaimed
	m.attempt.ClaimedBy = request.ExecutorID
	m.attempt.Fence.LeaseExpiresAt = request.Now.UTC().Add(request.LeaseDuration)
	return m.attempt, nil
}

func (m *StateMachine) Heartbeat(request types.HeartbeatRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if request.FenceGeneration != m.attempt.Fence.Generation {
		return errors.New("stale fence generation")
	}
	if request.ExecutorID != m.attempt.ClaimedBy {
		return errors.New("execution attempt is owned by another executor")
	}
	if m.attempt.Status != types.ExecutionAttemptStatusClaimed &&
		m.attempt.Status != types.ExecutionAttemptStatusRunning {
		return errors.New("execution attempt does not accept heartbeats")
	}
	if !m.attempt.Fence.LeaseExpiresAt.After(request.Now.UTC()) {
		return errors.New("execution attempt lease is lost")
	}
	m.attempt.Fence.LeaseExpiresAt = request.Now.UTC().Add(request.LeaseDuration)
	return nil
}

func (m *StateMachine) Acknowledge(request types.HeartbeatRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if request.AttemptID != m.attempt.ID ||
		request.ExecutorID != m.attempt.ClaimedBy ||
		request.FenceGeneration != m.attempt.Fence.Generation {
		return errors.New("execution acknowledgement identity is invalid")
	}
	if m.attempt.Status != types.ExecutionAttemptStatusClaimed &&
		m.attempt.Status != types.ExecutionAttemptStatusRunning {
		return errors.New("execution attempt does not accept acknowledgement")
	}
	if !m.attempt.Fence.LeaseExpiresAt.After(request.Now.UTC()) {
		return errors.New("execution attempt lease is lost")
	}
	if m.attempt.AcknowledgedAt == nil {
		acknowledgedAt := request.Now.UTC()
		m.attempt.AcknowledgedAt = &acknowledgedAt
	}
	m.attempt.Status = types.ExecutionAttemptStatusRunning
	return nil
}

func (m *StateMachine) RecordEvent(
	input types.ExecutionEventInput,
) (types.ExecutionEvent, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if input.AttemptID != m.attempt.ID || input.Identity != m.attempt.Identity {
		return types.ExecutionEvent{}, false, errors.New("execution event identity mismatch")
	}
	if input.FenceGeneration != m.attempt.Fence.Generation {
		return types.ExecutionEvent{}, false, errors.New("stale fence generation")
	}
	if input.ExecutorID != m.attempt.ClaimedBy ||
		(m.attempt.Status != types.ExecutionAttemptStatusClaimed &&
			m.attempt.Status != types.ExecutionAttemptStatusRunning) {
		return types.ExecutionEvent{}, false, errors.New("execution event ownership is invalid")
	}
	if !m.attempt.Fence.LeaseExpiresAt.After(input.OccurredAt.UTC()) {
		return types.ExecutionEvent{}, false, errors.New("execution attempt lease is lost")
	}
	if existing, ok := m.events[input.EventSequence]; ok {
		if existing.PayloadChecksum != input.PayloadChecksum ||
			existing.Status != input.Status || existing.Message != input.Message ||
			!existing.OccurredAt.Equal(input.OccurredAt.UTC()) ||
			existing.Identity != input.Identity ||
			existing.FenceGeneration != input.FenceGeneration {
			return types.ExecutionEvent{}, false, errors.New("conflicting duplicate execution event")
		}
		return existing, true, nil
	}
	if input.EventSequence != m.attempt.LastEventSequence+1 {
		return types.ExecutionEvent{}, false, errors.New("execution events must be ordered")
	}
	if !input.Status.IsValid() {
		return types.ExecutionEvent{}, false, errors.New("execution event status is invalid")
	}
	event := types.ExecutionEvent{
		ID: uuid.New(), OrganizationID: input.OrganizationID,
		DeploymentTargetID: input.DeploymentTargetID, AttemptID: input.AttemptID,
		Identity: input.Identity, FenceGeneration: input.FenceGeneration,
		EventSequence: input.EventSequence, Status: input.Status, PayloadChecksum: input.PayloadChecksum,
		Message: input.Message, OccurredAt: input.OccurredAt.UTC(), CreatedAt: time.Now().UTC(),
	}
	m.events[input.EventSequence] = event
	m.attempt.LastEventSequence = input.EventSequence
	if input.Status == types.ExecutionEventStatusRunning {
		m.attempt.Status = types.ExecutionAttemptStatusRunning
	}
	return event, false, nil
}

func (m *StateMachine) Complete(input types.CompletionInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if input.AttemptID != m.attempt.ID || input.ExecutorID != m.attempt.ClaimedBy {
		return errors.New("completion identity is invalid")
	}
	if input.FenceGeneration != m.attempt.Fence.Generation {
		return errors.New("stale fence generation")
	}
	if m.attempt.Status != types.ExecutionAttemptStatusClaimed &&
		m.attempt.Status != types.ExecutionAttemptStatusRunning {
		return errors.New("completion requires an owned active attempt")
	}
	if !m.attempt.Fence.LeaseExpiresAt.After(input.CompletedAt.UTC()) {
		return errors.New("execution attempt lease is lost")
	}
	switch input.Status {
	case types.ExecutionAttemptStatusSucceeded, types.ExecutionAttemptStatusFailed,
		types.ExecutionAttemptStatusCanceled, types.ExecutionAttemptStatusTimedOut:
	default:
		return errors.New("completion status is not terminal")
	}
	completedAt := input.CompletedAt.UTC()
	m.attempt.Status = input.Status
	m.attempt.CompletedAt = &completedAt
	m.attempt.FailureReason = strings.TrimSpace(input.FailureReason)
	m.attempt.ClaimedBy = ""
	m.attempt.Fence.LeaseExpiresAt = time.Time{}
	return nil
}

func (m *StateMachine) Fence(reason string, at time.Time) (types.ExecutionAttempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.attempt.Status.IsTerminal() {
		return types.ExecutionAttempt{}, errors.New("terminal execution attempt cannot be fenced")
	}
	m.attempt.Status = types.ExecutionAttemptStatusFenced
	m.attempt.Fence.Generation++
	m.attempt.Fence.LeaseExpiresAt = time.Time{}
	m.attempt.ClaimedBy = ""
	m.attempt.FailureReason = strings.TrimSpace(reason)
	completedAt := at.UTC()
	m.attempt.CompletedAt = &completedAt
	return m.attempt, nil
}
