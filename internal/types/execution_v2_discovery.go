package types

import (
	"time"

	"github.com/google/uuid"
)

type LeaseExecutionV2Request struct {
	OrganizationID     uuid.UUID
	DeploymentTargetID uuid.UUID
	ExecutorID         string
	AdapterRevision    string
	KeyID              string
	Now                time.Time
	LeaseDuration      time.Duration
}

type ExecutionV2Lease struct {
	Attempt ExecutionAttempt      `json:"attempt"`
	Intent  SignedExecutionIntent `json:"intent"`
}
