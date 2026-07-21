package api

import (
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/types"
	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type ExecutionV2LeaseRequest struct {
	ExecutorID      string `json:"executorId"`
	AdapterRevision string `json:"adapterRevision"`
	KeyID           string `json:"keyId"`
	LeaseSeconds    int    `json:"leaseSeconds"`
}

func (r *ExecutionV2LeaseRequest) Validate() error {
	r.ExecutorID = strings.TrimSpace(r.ExecutorID)
	r.AdapterRevision = strings.TrimSpace(r.AdapterRevision)
	r.KeyID = strings.TrimSpace(r.KeyID)
	if r.ExecutorID == "" || len(r.ExecutorID) > 128 || strings.ContainsAny(r.ExecutorID, "\r\n") {
		return validation.NewValidationFailedError("executorId is invalid")
	}
	if r.AdapterRevision == "" || len(r.AdapterRevision) > 256 ||
		strings.ContainsAny(r.AdapterRevision, "\r\n") {
		return validation.NewValidationFailedError("adapterRevision is invalid")
	}
	if !executionV2ChecksumPattern.MatchString(r.KeyID) {
		return validation.NewValidationFailedError("keyId must be a sha256 fingerprint")
	}
	if r.LeaseSeconds < 15 || r.LeaseSeconds > 300 {
		return validation.NewValidationFailedError("leaseSeconds must be between 15 and 300")
	}
	return nil
}

func (r ExecutionV2LeaseRequest) ToTypes(
	orgID, deploymentTargetID uuid.UUID,
	now time.Time,
) types.LeaseExecutionV2Request {
	return types.LeaseExecutionV2Request{
		OrganizationID: orgID, DeploymentTargetID: deploymentTargetID,
		ExecutorID: r.ExecutorID, AdapterRevision: r.AdapterRevision, KeyID: r.KeyID,
		Now: now.UTC(), LeaseDuration: time.Duration(r.LeaseSeconds) * time.Second,
	}
}

type ExecutionV2LeaseResponse struct {
	Attempt types.ExecutionAttempt      `json:"attempt"`
	Intent  types.SignedExecutionIntent `json:"intent"`
}
