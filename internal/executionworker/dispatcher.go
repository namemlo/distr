package executionworker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/distr-sh/distr/internal/types"
	"github.com/google/uuid"
)

type AdmissionRequest struct {
	OrganizationID uuid.UUID
	EnvironmentID  uuid.UUID
	PlanID         uuid.UUID
	StepKey        string
}

type AdmissionDecision struct {
	OperatorFlag     bool
	ExecutorFlag     bool
	ScopedEnrollment bool
	PlanApproved     bool
	PlanAdmitted     bool
	AdapterPreflight bool
}

func (d AdmissionDecision) denialReason() string {
	checks := []struct {
		ok     bool
		reason string
	}{
		{d.OperatorFlag, "operator_flag"},
		{d.ExecutorFlag, "executor_flag"},
		{d.ScopedEnrollment, "scoped_enrollment"},
		{d.PlanApproved, "plan_approval"},
		{d.PlanAdmitted, "plan_admission"},
		{d.AdapterPreflight, "adapter_preflight"},
	}
	for _, check := range checks {
		if !check.ok {
			return check.reason
		}
	}
	return ""
}

type AdmissionGate interface {
	EvaluateExecutionV2Admission(context.Context, AdmissionRequest) (AdmissionDecision, error)
}

type CreateAttemptRequest struct {
	OrganizationID uuid.UUID
	ExecutionID    uuid.UUID
	PlanID         uuid.UUID
	TaskID         uuid.UUID
	StepRunID      uuid.UUID
	StepKey        string
}

type AttemptCreator interface {
	CreateExecutionAttempt(context.Context, CreateAttemptRequest) (*types.ExecutionAttempt, error)
}

type DispatchRequest struct {
	OrganizationID uuid.UUID
	EnvironmentID  uuid.UUID
	ExecutionID    uuid.UUID
	PlanID         uuid.UUID
	TaskID         uuid.UUID
	StepRunID      uuid.UUID
	StepKey        string
}

type Dispatcher struct {
	gate    AdmissionGate
	creator AttemptCreator
}

func NewDispatcher(gate AdmissionGate, creator AttemptCreator) *Dispatcher {
	return &Dispatcher{gate: gate, creator: creator}
}

func (d *Dispatcher) Dispatch(
	ctx context.Context,
	request DispatchRequest,
) (*types.ExecutionAttempt, error) {
	if d == nil || d.gate == nil || d.creator == nil {
		return nil, errors.New("execution v2 dispatcher is not configured")
	}
	if request.OrganizationID == uuid.Nil || request.ExecutionID == uuid.Nil ||
		strings.TrimSpace(request.StepKey) == "" {
		return nil, errors.New("execution v2 dispatch request is invalid")
	}
	decision, err := d.gate.EvaluateExecutionV2Admission(ctx, AdmissionRequest{
		OrganizationID: request.OrganizationID, EnvironmentID: request.EnvironmentID,
		PlanID: request.PlanID, StepKey: strings.TrimSpace(request.StepKey),
	})
	if err != nil {
		return nil, fmt.Errorf("evaluate execution v2 admission: %w", err)
	}
	if reason := decision.denialReason(); reason != "" {
		return nil, fmt.Errorf("execution v2 admission denied: %s", reason)
	}
	return d.creator.CreateExecutionAttempt(ctx, CreateAttemptRequest{
		OrganizationID: request.OrganizationID, ExecutionID: request.ExecutionID,
		PlanID: request.PlanID, TaskID: request.TaskID, StepRunID: request.StepRunID,
		StepKey: strings.TrimSpace(request.StepKey),
	})
}
