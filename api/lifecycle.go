package api

import (
	"strings"
	"time"

	"github.com/distr-sh/distr/internal/validation"
	"github.com/google/uuid"
)

type CreateUpdateLifecycleRequest struct {
	Name        string                              `json:"name"`
	Description string                              `json:"description"`
	SortOrder   int                                 `json:"sortOrder"`
	Phases      []CreateUpdateLifecyclePhaseRequest `json:"phases"`
}

func (r *CreateUpdateLifecycleRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return validation.NewValidationFailedError("name is required")
	}
	if r.SortOrder < 0 {
		return validation.NewValidationFailedError("sortOrder must be non-negative")
	}
	for _, phase := range r.Phases {
		if err := phase.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type CreateUpdateLifecyclePhaseRequest struct {
	Name                         string      `json:"name"`
	Description                  string      `json:"description"`
	SortOrder                    int         `json:"sortOrder"`
	EnvironmentIDs               []uuid.UUID `json:"environmentIds"`
	Optional                     bool        `json:"optional"`
	AutomaticPromotion           bool        `json:"automaticPromotion"`
	MinimumSuccessfulDeployments int         `json:"minimumSuccessfulDeployments"`
	ApprovalPolicyID             *uuid.UUID  `json:"approvalPolicyId,omitempty"`
	RetentionPolicyID            *uuid.UUID  `json:"retentionPolicyId,omitempty"`
}

func (r *CreateUpdateLifecyclePhaseRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return validation.NewValidationFailedError("phase name is required")
	}
	if r.SortOrder < 0 {
		return validation.NewValidationFailedError("phase sortOrder must be non-negative")
	}
	if len(r.EnvironmentIDs) == 0 {
		return validation.NewValidationFailedError("phase must reference at least one environment")
	}
	if r.MinimumSuccessfulDeployments < 0 {
		return validation.NewValidationFailedError("phase minimumSuccessfulDeployments must be non-negative")
	}
	return nil
}

type UpdateLifecyclePhasesRequest struct {
	Phases []CreateUpdateLifecyclePhaseRequest `json:"phases"`
}

func (r *UpdateLifecyclePhasesRequest) Validate() error {
	for _, phase := range r.Phases {
		if err := phase.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type Lifecycle struct {
	ID          uuid.UUID        `json:"id"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	SortOrder   int              `json:"sortOrder"`
	Phases      []LifecyclePhase `json:"phases"`
}

type LifecyclePhase struct {
	ID                           uuid.UUID   `json:"id"`
	Name                         string      `json:"name"`
	Description                  string      `json:"description"`
	SortOrder                    int         `json:"sortOrder"`
	EnvironmentIDs               []uuid.UUID `json:"environmentIds"`
	Optional                     bool        `json:"optional"`
	AutomaticPromotion           bool        `json:"automaticPromotion"`
	MinimumSuccessfulDeployments int         `json:"minimumSuccessfulDeployments"`
	ApprovalPolicyID             *uuid.UUID  `json:"approvalPolicyId,omitempty"`
	RetentionPolicyID            *uuid.UUID  `json:"retentionPolicyId,omitempty"`
}
