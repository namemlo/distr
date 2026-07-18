package api

import (
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

var executionV2ChecksumPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type ExecutionV2ClaimRequest struct {
	AttemptID          uuid.UUID `json:"attemptId"`
	ExecutorID         string    `json:"executorId"`
	ExpectedGeneration int64     `json:"expectedGeneration"`
	LeaseSeconds       int       `json:"leaseSeconds"`
}

func (r *ExecutionV2ClaimRequest) Validate() error {
	r.ExecutorID = strings.TrimSpace(r.ExecutorID)
	if r.AttemptID == uuid.Nil {
		return validation.NewValidationFailedError("attemptId is required")
	}
	if r.ExecutorID == "" || len(r.ExecutorID) > 128 || strings.ContainsAny(r.ExecutorID, "\r\n") {
		return validation.NewValidationFailedError("executorId is invalid")
	}
	if r.ExpectedGeneration <= 0 {
		return validation.NewValidationFailedError("expectedGeneration must be greater than 0")
	}
	if r.LeaseSeconds < 15 || r.LeaseSeconds > 300 {
		return validation.NewValidationFailedError("leaseSeconds must be between 15 and 300")
	}
	return nil
}

func (r ExecutionV2ClaimRequest) ToTypes(orgID uuid.UUID, now time.Time) types.ClaimRequest {
	return types.ClaimRequest{
		OrganizationID: orgID, AttemptID: r.AttemptID, ExecutorID: r.ExecutorID,
		ExpectedGeneration: r.ExpectedGeneration, Now: now.UTC(),
		LeaseDuration: time.Duration(r.LeaseSeconds) * time.Second,
	}
}

type ExecutionV2HeartbeatRequest struct {
	ExecutorID      string `json:"executorId"`
	FenceGeneration int64  `json:"fenceGeneration"`
	LeaseSeconds    int    `json:"leaseSeconds"`
}

func (r *ExecutionV2HeartbeatRequest) Validate() error {
	r.ExecutorID = strings.TrimSpace(r.ExecutorID)
	if r.ExecutorID == "" || len(r.ExecutorID) > 128 || strings.ContainsAny(r.ExecutorID, "\r\n") {
		return validation.NewValidationFailedError("executorId is invalid")
	}
	if r.FenceGeneration <= 0 {
		return validation.NewValidationFailedError("fenceGeneration must be greater than 0")
	}
	if r.LeaseSeconds < 15 || r.LeaseSeconds > 300 {
		return validation.NewValidationFailedError("leaseSeconds must be between 15 and 300")
	}
	return nil
}

func (r ExecutionV2HeartbeatRequest) ToTypes(
	orgID, attemptID uuid.UUID,
	now time.Time,
) types.HeartbeatRequest {
	return types.HeartbeatRequest{
		OrganizationID: orgID, AttemptID: attemptID, ExecutorID: r.ExecutorID,
		FenceGeneration: r.FenceGeneration, Now: now.UTC(),
		LeaseDuration: time.Duration(r.LeaseSeconds) * time.Second,
	}
}

type ExecutionV2EventRequest struct {
	ExecutionID     uuid.UUID                  `json:"executionId"`
	AttemptNumber   int                        `json:"attemptNumber"`
	StepKey         string                     `json:"stepKey"`
	FenceGeneration int64                      `json:"fenceGeneration"`
	EventSequence   int64                      `json:"eventSequence"`
	Status          types.ExecutionEventStatus `json:"status"`
	PayloadChecksum string                     `json:"payloadChecksum"`
	Message         string                     `json:"message,omitempty"`
	OccurredAt      time.Time                  `json:"occurredAt"`
}

func (r *ExecutionV2EventRequest) Validate() error {
	r.StepKey = strings.TrimSpace(r.StepKey)
	r.Message = strings.TrimSpace(r.Message)
	if r.ExecutionID == uuid.Nil || r.AttemptNumber <= 0 || r.StepKey == "" {
		return validation.NewValidationFailedError("execution identity is invalid")
	}
	if r.FenceGeneration <= 0 {
		return validation.NewValidationFailedError("fenceGeneration must be greater than 0")
	}
	if r.EventSequence <= 0 {
		return validation.NewValidationFailedError("eventSequence must be greater than 0")
	}
	if !r.Status.IsValid() {
		return validation.NewValidationFailedError("status is invalid")
	}
	if !executionV2ChecksumPattern.MatchString(r.PayloadChecksum) {
		return validation.NewValidationFailedError("payloadChecksum must be a sha256 checksum")
	}
	if len(r.Message) > 2048 || strings.ContainsAny(r.Message, "\r\n") {
		return validation.NewValidationFailedError("message is invalid")
	}
	if r.OccurredAt.IsZero() {
		return validation.NewValidationFailedError("occurredAt is required")
	}
	return nil
}

func (r ExecutionV2EventRequest) ToTypes(
	orgID, attemptID uuid.UUID,
) types.ExecutionEventInput {
	return types.ExecutionEventInput{
		OrganizationID: orgID, AttemptID: attemptID,
		Identity: types.ExecutionIdentity{
			ExecutionID: r.ExecutionID, AttemptNumber: r.AttemptNumber, StepKey: r.StepKey,
		},
		FenceGeneration: r.FenceGeneration, EventSequence: r.EventSequence,
		Status: r.Status, PayloadChecksum: r.PayloadChecksum, Message: r.Message,
		OccurredAt: r.OccurredAt.UTC(),
	}
}

type ExecutionV2CompletionRequest struct {
	ExecutorID      string                       `json:"executorId"`
	FenceGeneration int64                        `json:"fenceGeneration"`
	Status          types.ExecutionAttemptStatus `json:"status"`
	FailureReason   string                       `json:"failureReason,omitempty"`
	CompletedAt     time.Time                    `json:"completedAt"`
}

func (r *ExecutionV2CompletionRequest) Validate() error {
	r.ExecutorID = strings.TrimSpace(r.ExecutorID)
	r.FailureReason = strings.TrimSpace(r.FailureReason)
	if r.ExecutorID == "" || r.FenceGeneration <= 0 {
		return validation.NewValidationFailedError("completion identity is invalid")
	}
	switch r.Status {
	case types.ExecutionAttemptStatusSucceeded, types.ExecutionAttemptStatusFailed,
		types.ExecutionAttemptStatusCanceled, types.ExecutionAttemptStatusTimedOut:
	default:
		return validation.NewValidationFailedError("status must be terminal")
	}
	if r.CompletedAt.IsZero() {
		return validation.NewValidationFailedError("completedAt is required")
	}
	if len(r.FailureReason) > 2048 || strings.ContainsAny(r.FailureReason, "\r\n") {
		return validation.NewValidationFailedError("failureReason is invalid")
	}
	return nil
}

func (r ExecutionV2CompletionRequest) ToTypes(
	orgID, attemptID uuid.UUID,
) types.CompletionInput {
	return types.CompletionInput{
		OrganizationID: orgID, AttemptID: attemptID, ExecutorID: r.ExecutorID,
		FenceGeneration: r.FenceGeneration, Status: r.Status,
		FailureReason: r.FailureReason, CompletedAt: r.CompletedAt.UTC(),
	}
}

type ExecutionV2AttemptResponse struct {
	Attempt types.ExecutionAttempt       `json:"attempt"`
	Intent  *types.SignedExecutionIntent `json:"intent,omitempty"`
}
