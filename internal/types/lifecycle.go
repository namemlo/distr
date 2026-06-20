package types

import (
	"time"

	"github.com/google/uuid"
)

type Lifecycle struct {
	ID             uuid.UUID        `db:"id" json:"id"`
	CreatedAt      time.Time        `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time        `db:"updated_at" json:"updatedAt"`
	OrganizationID uuid.UUID        `db:"organization_id" json:"organizationId"`
	Name           string           `db:"name" json:"name"`
	Description    string           `db:"description" json:"description"`
	SortOrder      int              `db:"sort_order" json:"sortOrder"`
	Phases         []LifecyclePhase `db:"-" json:"phases"`
}

type LifecyclePhase struct {
	ID                           uuid.UUID   `db:"id" json:"id"`
	LifecycleID                  uuid.UUID   `db:"lifecycle_id" json:"lifecycleId"`
	Name                         string      `db:"name" json:"name"`
	Description                  string      `db:"description" json:"description"`
	SortOrder                    int         `db:"sort_order" json:"sortOrder"`
	EnvironmentIDs               []uuid.UUID `db:"-" json:"environmentIds"`
	Optional                     bool        `db:"optional" json:"optional"`
	AutomaticPromotion           bool        `db:"automatic_promotion" json:"automaticPromotion"`
	MinimumSuccessfulDeployments int         `db:"minimum_successful_deployments" json:"minimumSuccessfulDeployments"`
	ApprovalPolicyID             *uuid.UUID  `db:"approval_policy_id" json:"approvalPolicyId,omitempty"`
	RetentionPolicyID            *uuid.UUID  `db:"retention_policy_id" json:"retentionPolicyId,omitempty"`
}
