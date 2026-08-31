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

func (r ExecutionV2ClaimRequest) ToTypes(
	orgID, deploymentTargetID uuid.UUID,
	now time.Time,
) types.ClaimRequest {
	return types.ClaimRequest{
		OrganizationID: orgID, DeploymentTargetID: deploymentTargetID,
		AttemptID: r.AttemptID, ExecutorID: r.ExecutorID,
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
	orgID, deploymentTargetID, attemptID uuid.UUID,
	now time.Time,
) types.HeartbeatRequest {
	return types.HeartbeatRequest{
		OrganizationID: orgID, DeploymentTargetID: deploymentTargetID,
		AttemptID: attemptID, ExecutorID: r.ExecutorID,
		FenceGeneration: r.FenceGeneration, Now: now.UTC(),
		LeaseDuration: time.Duration(r.LeaseSeconds) * time.Second,
	}
}

type ExecutionV2AcknowledgeRequest struct {
	ExecutorID      string `json:"executorId"`
	FenceGeneration int64  `json:"fenceGeneration"`
}

func (r *ExecutionV2AcknowledgeRequest) Validate() error {
	r.ExecutorID = strings.TrimSpace(r.ExecutorID)
	if r.ExecutorID == "" || len(r.ExecutorID) > 128 ||
		strings.ContainsAny(r.ExecutorID, "\r\n") || r.FenceGeneration <= 0 {
		return validation.NewValidationFailedError("acknowledgement identity is invalid")
	}
	return nil
}

func (r ExecutionV2AcknowledgeRequest) ToTypes(
	orgID, deploymentTargetID, attemptID uuid.UUID,
) types.HeartbeatRequest {
	return types.HeartbeatRequest{
		OrganizationID: orgID, DeploymentTargetID: deploymentTargetID,
		AttemptID: attemptID, ExecutorID: r.ExecutorID,
		FenceGeneration: r.FenceGeneration,
	}
}

type ExecutionV2EventRequest struct {
	ExecutorID      string                     `json:"executorId"`
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
	r.ExecutorID = strings.TrimSpace(r.ExecutorID)
	r.Message = strings.TrimSpace(r.Message)
	if r.ExecutorID == "" || len(r.ExecutorID) > 128 ||
		strings.ContainsAny(r.ExecutorID, "\r\n") ||
		r.ExecutionID == uuid.Nil || r.AttemptNumber <= 0 || r.StepKey == "" {
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
	orgID, deploymentTargetID, attemptID uuid.UUID,
) types.ExecutionEventInput {
	return types.ExecutionEventInput{
		OrganizationID: orgID, DeploymentTargetID: deploymentTargetID,
		AttemptID: attemptID, ExecutorID: r.ExecutorID,
		Identity: types.ExecutionIdentity{
			ExecutionID: r.ExecutionID, AttemptNumber: r.AttemptNumber, StepKey: r.StepKey,
		},
		FenceGeneration: r.FenceGeneration, EventSequence: r.EventSequence,
		Status: r.Status, PayloadChecksum: r.PayloadChecksum, Message: r.Message,
		OccurredAt: r.OccurredAt.UTC().Truncate(time.Microsecond),
	}
}

type ExecutionV2RuntimeEvidenceRequest struct {
	EventIdentity                 uuid.UUID                      `json:"eventIdentity"`
	SchemaVersion                 string                         `json:"schemaVersion"`
	IntentChecksum                string                         `json:"intentChecksum"`
	ExecutorID                    string                         `json:"executorId"`
	CallerIdentity                string                         `json:"callerIdentity"`
	Audience                      string                         `json:"audience"`
	FenceGeneration               int64                          `json:"fenceGeneration"`
	ExpectedObservedStateVersion  int64                          `json:"expectedObservedStateVersion"`
	ExpectedObservedStateChecksum string                         `json:"expectedObservedStateChecksum"`
	PreExecutionImageDigest       string                         `json:"preExecutionImageDigest"`
	PreExecutionConfigChecksum    string                         `json:"preExecutionConfigChecksum"`
	ResultImageDigest             string                         `json:"resultImageDigest"`
	ResultConfigChecksum          string                         `json:"resultConfigChecksum"`
	Platform                      types.DeploymentTargetPlatform `json:"platform"`
	HealthStatus                  types.TargetComponentHealth    `json:"healthStatus"`
	ResultChecksum                string                         `json:"resultChecksum"`
	EvidenceReference             string                         `json:"evidenceReference"`
	EvidenceChecksum              string                         `json:"evidenceChecksum"`
	CapturedAt                    time.Time                      `json:"capturedAt"`
}

func (r *ExecutionV2RuntimeEvidenceRequest) Validate() error {
	r.ExecutorID = strings.TrimSpace(r.ExecutorID)
	r.CallerIdentity = strings.TrimSpace(r.CallerIdentity)
	r.Audience = strings.TrimSpace(r.Audience)
	r.Platform = types.DeploymentTargetPlatform(strings.TrimSpace(string(r.Platform)))
	r.HealthStatus = types.TargetComponentHealth(strings.TrimSpace(string(r.HealthStatus)))
	r.EvidenceReference = strings.TrimSpace(r.EvidenceReference)
	r.SchemaVersion = strings.TrimSpace(r.SchemaVersion)

	if r.EventIdentity == uuid.Nil {
		return validation.NewValidationFailedError("eventIdentity is required")
	}
	if r.SchemaVersion != types.ExecutionRuntimeEvidenceSchemaV1 {
		return validation.NewValidationFailedError("schemaVersion is invalid")
	}
	if !validExecutionV2Identity(r.ExecutorID, 128) {
		return validation.NewValidationFailedError("executorId is invalid")
	}
	if !validExecutionV2Identity(r.CallerIdentity, 512) {
		return validation.NewValidationFailedError("callerIdentity is invalid")
	}
	if !validExecutionV2Identity(r.Audience, 512) {
		return validation.NewValidationFailedError("audience is invalid")
	}
	if r.FenceGeneration <= 0 {
		return validation.NewValidationFailedError("fenceGeneration must be greater than 0")
	}
	if r.ExpectedObservedStateVersion <= 0 {
		return validation.NewValidationFailedError("expectedObservedStateVersion must be greater than 0")
	}
	for name, checksum := range map[string]string{
		"intentChecksum":                r.IntentChecksum,
		"expectedObservedStateChecksum": r.ExpectedObservedStateChecksum,
		"preExecutionImageDigest":       r.PreExecutionImageDigest,
		"preExecutionConfigChecksum":    r.PreExecutionConfigChecksum,
		"resultImageDigest":             r.ResultImageDigest,
		"resultConfigChecksum":          r.ResultConfigChecksum,
		"resultChecksum":                r.ResultChecksum,
		"evidenceChecksum":              r.EvidenceChecksum,
	} {
		if !executionV2ChecksumPattern.MatchString(checksum) {
			return validation.NewValidationFailedError(name + " must be a sha256 checksum")
		}
	}
	if !r.Platform.IsValid() {
		return validation.NewValidationFailedError("platform is invalid")
	}
	if r.HealthStatus != types.TargetComponentHealthHealthy &&
		r.HealthStatus != types.TargetComponentHealthUnhealthy {
		return validation.NewValidationFailedError("healthStatus is invalid")
	}
	if !validExecutionV2Identity(r.EvidenceReference, 2048) {
		return validation.NewValidationFailedError("evidenceReference is invalid")
	}
	if r.CapturedAt.IsZero() {
		return validation.NewValidationFailedError("capturedAt is required")
	}
	return nil
}

func (r ExecutionV2RuntimeEvidenceRequest) ToTypes(
	orgID, deploymentTargetID, attemptID uuid.UUID,
) types.ExecutionRuntimeEvidenceInput {
	return types.ExecutionRuntimeEvidenceInput{
		OrganizationID: orgID, DeploymentTargetID: deploymentTargetID, AttemptID: attemptID,
		ExecutorID: r.ExecutorID, EventIdentity: r.EventIdentity,
		SchemaVersion:  r.SchemaVersion,
		IntentChecksum: r.IntentChecksum, CallerIdentity: r.CallerIdentity, Audience: r.Audience,
		FenceGeneration:               r.FenceGeneration,
		ExpectedObservedStateVersion:  r.ExpectedObservedStateVersion,
		ExpectedObservedStateChecksum: r.ExpectedObservedStateChecksum,
		PreExecutionImageDigest:       r.PreExecutionImageDigest,
		PreExecutionConfigChecksum:    r.PreExecutionConfigChecksum,
		ResultImageDigest:             r.ResultImageDigest, ResultConfigChecksum: r.ResultConfigChecksum,
		Platform: r.Platform, HealthStatus: r.HealthStatus, ResultChecksum: r.ResultChecksum,
		EvidenceReference: r.EvidenceReference, EvidenceChecksum: r.EvidenceChecksum,
		CapturedAt: r.CapturedAt.UTC().Truncate(time.Microsecond),
	}
}

type ExecutionV2RuntimeEvidenceResponse struct {
	RuntimeEvidence types.ExecutionRuntimeEvidence `json:"runtimeEvidence"`
}

func validExecutionV2Identity(value string, maxLength int) bool {
	return value != "" && len(value) <= maxLength && !strings.ContainsAny(value, "\r\n")
}

type ExecutionV2CompletionRequest struct {
	ExecutorID              string                       `json:"executorId"`
	FenceGeneration         int64                        `json:"fenceGeneration"`
	Status                  types.ExecutionAttemptStatus `json:"status"`
	RuntimeEvidenceID       *uuid.UUID                   `json:"runtimeEvidenceId,omitempty"`
	RuntimeEvidenceChecksum string                       `json:"runtimeEvidenceChecksum,omitempty"`
	FailureReason           string                       `json:"failureReason,omitempty"`
	CompletedAt             time.Time                    `json:"completedAt"`
}

func (r *ExecutionV2CompletionRequest) Validate() error {
	r.ExecutorID = strings.TrimSpace(r.ExecutorID)
	r.RuntimeEvidenceChecksum = strings.TrimSpace(r.RuntimeEvidenceChecksum)
	r.FailureReason = strings.TrimSpace(r.FailureReason)
	if !validExecutionV2Identity(r.ExecutorID, 128) || r.FenceGeneration <= 0 {
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
	if r.Status == types.ExecutionAttemptStatusSucceeded {
		if r.RuntimeEvidenceID == nil || *r.RuntimeEvidenceID == uuid.Nil ||
			!executionV2ChecksumPattern.MatchString(r.RuntimeEvidenceChecksum) {
			return validation.NewValidationFailedError(
				"runtime evidence id and checksum are required for successful completion",
			)
		}
	} else if (r.RuntimeEvidenceID != nil && *r.RuntimeEvidenceID != uuid.Nil) ||
		r.RuntimeEvidenceChecksum != "" {
		return validation.NewValidationFailedError(
			"runtime evidence is only allowed for successful completion",
		)
	}
	if len(r.FailureReason) > 2048 || strings.ContainsAny(r.FailureReason, "\r\n") {
		return validation.NewValidationFailedError("failureReason is invalid")
	}
	return nil
}

func (r ExecutionV2CompletionRequest) ToTypes(
	orgID, deploymentTargetID, attemptID uuid.UUID,
) types.CompletionInput {
	runtimeEvidenceID := uuid.Nil
	if r.RuntimeEvidenceID != nil {
		runtimeEvidenceID = *r.RuntimeEvidenceID
	}
	return types.CompletionInput{
		OrganizationID: orgID, DeploymentTargetID: deploymentTargetID,
		AttemptID: attemptID, ExecutorID: r.ExecutorID,
		FenceGeneration: r.FenceGeneration, Status: r.Status,
		RuntimeEvidenceID: runtimeEvidenceID, RuntimeEvidenceChecksum: r.RuntimeEvidenceChecksum,
		FailureReason: r.FailureReason, CompletedAt: r.CompletedAt.UTC(),
	}
}

type ExecutionV2AttemptResponse struct {
	Attempt types.ExecutionAttempt       `json:"attempt"`
	Intent  *types.SignedExecutionIntent `json:"intent,omitempty"`
}

type ExecutionCancelRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	Reason         string `json:"reason"`
}

func (r *ExecutionCancelRequest) Validate() error {
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
	r.Reason = strings.TrimSpace(r.Reason)
	if r.IdempotencyKey == "" || len(r.IdempotencyKey) > 128 ||
		strings.ContainsAny(r.IdempotencyKey, "\r\n") {
		return validation.NewValidationFailedError("idempotencyKey is invalid")
	}
	if r.Reason == "" || len(r.Reason) > 2048 || strings.ContainsAny(r.Reason, "\r\n") {
		return validation.NewValidationFailedError("reason is invalid")
	}
	return nil
}

func (r ExecutionCancelRequest) ToTypes(
	orgID, executionID, actorID uuid.UUID,
	now time.Time,
) types.CancelRequest {
	return types.CancelRequest{
		OrganizationID: orgID, ExecutionID: executionID, RequestedBy: actorID,
		IdempotencyKey: r.IdempotencyKey, Reason: r.Reason, RequestedAt: now.UTC(),
	}
}

type ExecutionStatusRequest struct {
	IdempotencyKey   string `json:"idempotencyKey"`
	Reason           string `json:"reason"`
	ExpiresInSeconds int    `json:"expiresInSeconds"`
}

func (r *ExecutionStatusRequest) Validate() error {
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
	r.Reason = strings.TrimSpace(r.Reason)
	if r.IdempotencyKey == "" || len(r.IdempotencyKey) > 128 {
		return validation.NewValidationFailedError("idempotencyKey is invalid")
	}
	if r.Reason == "" || len(r.Reason) > 2048 || strings.ContainsAny(r.Reason, "\r\n") {
		return validation.NewValidationFailedError("reason is invalid")
	}
	if r.ExpiresInSeconds < 30 || r.ExpiresInSeconds > 3600 {
		return validation.NewValidationFailedError("expiresInSeconds must be between 30 and 3600")
	}
	return nil
}

func (r ExecutionStatusRequest) ToTypes(
	orgID, executionID, actorID uuid.UUID,
	now time.Time,
) types.StatusRequest {
	return types.StatusRequest{
		OrganizationID: orgID, ExecutionID: executionID, RequestedBy: actorID,
		IdempotencyKey: r.IdempotencyKey, Reason: r.Reason, RequestedAt: now.UTC(),
		ExpiresAt:           now.UTC().Add(time.Duration(r.ExpiresInSeconds) * time.Second),
		RequestedTTLSeconds: r.ExpiresInSeconds,
	}
}

type ExecutionReconciliationRequest struct {
	Evidence types.SignedReconciliationEvidence `json:"evidence"`
}

func (r *ExecutionReconciliationRequest) Validate() error {
	if len(r.Evidence.Payload) == 0 ||
		!executionV2ChecksumPattern.MatchString(r.Evidence.Checksum) ||
		!executionV2ChecksumPattern.MatchString(r.Evidence.KeyID) ||
		strings.TrimSpace(r.Evidence.Signature) == "" {
		return validation.NewValidationFailedError("signed reconciliation evidence is required")
	}
	return nil
}

func ReconciliationEvidenceToTypes(
	evidence types.ReconciliationEvidence,
	signed types.SignedReconciliationEvidence,
) types.ReconciliationStatusInput {
	return types.ReconciliationStatusInput{
		OrganizationID: evidence.OrganizationID, ExecutionID: evidence.ExecutionID,
		AttemptID:     evidence.AttemptID,
		StatusQueryID: evidence.StatusQueryID, EventIdentity: evidence.EventIdentity,
		Outcome: evidence.Outcome, EvidenceChecksum: evidence.EvidenceChecksum,
		ObservedAt:          evidence.ObservedAt.UTC(),
		OperationIncomplete: evidence.OperationIncomplete, RetryRequested: evidence.RetryRequested,
		SignedEvidence: signed,
	}
}

type ExecutionCancelAcknowledgementRequest struct {
	CancelRequestID uuid.UUID `json:"cancelRequestId"`
	ExecutorID      string    `json:"executorId"`
	FenceGeneration int64     `json:"fenceGeneration"`
	Accepted        bool      `json:"accepted"`
}

func (r *ExecutionCancelAcknowledgementRequest) Validate() error {
	r.ExecutorID = strings.TrimSpace(r.ExecutorID)
	if r.CancelRequestID == uuid.Nil || r.ExecutorID == "" || r.FenceGeneration <= 0 {
		return validation.NewValidationFailedError("cancel acknowledgement identity is invalid")
	}
	return nil
}

func (r ExecutionCancelAcknowledgementRequest) ToTypes(
	orgID, deploymentTargetID, attemptID uuid.UUID,
	now time.Time,
) types.CancelAcknowledgement {
	return types.CancelAcknowledgement{
		OrganizationID: orgID, DeploymentTargetID: deploymentTargetID,
		CancelRequestID: r.CancelRequestID, AttemptID: attemptID,
		ExecutorID: r.ExecutorID, FenceGeneration: r.FenceGeneration,
		Accepted: r.Accepted, AcknowledgedAt: now.UTC(),
	}
}
