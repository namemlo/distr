package types

import (
	"time"

	"github.com/google/uuid"
)

type DeploymentPreflightStatus string

const (
	DeploymentPreflightStatusPassed DeploymentPreflightStatus = "PASSED"
	DeploymentPreflightStatusFailed DeploymentPreflightStatus = "FAILED"
)

type DeploymentPreflightCheckStatus string

const (
	DeploymentPreflightCheckStatusPassed DeploymentPreflightCheckStatus = "PASSED"
	DeploymentPreflightCheckStatusFailed DeploymentPreflightCheckStatus = "FAILED"
)

type DeploymentPreflightRun struct {
	ID                 uuid.UUID                  `db:"id" json:"id"`
	CreatedAt          time.Time                  `db:"created_at" json:"createdAt"`
	OrganizationID     uuid.UUID                  `db:"organization_id" json:"organizationId"`
	DeploymentPlanID   uuid.UUID                  `db:"deployment_plan_id" json:"deploymentPlanId"`
	PlanChecksum       string                     `db:"plan_checksum" json:"planChecksum"`
	ActorUserAccountID *uuid.UUID                 `db:"actor_user_account_id" json:"actorUserAccountId,omitempty"`
	Status             DeploymentPreflightStatus  `db:"status" json:"status"`
	Checks             []DeploymentPreflightCheck `db:"-" json:"checks"`
}

type DeploymentPreflightCheck struct {
	ID                       uuid.UUID                      `db:"id" json:"id"`
	CreatedAt                time.Time                      `db:"created_at" json:"createdAt"`
	OrganizationID           uuid.UUID                      `db:"organization_id" json:"organizationId"`
	DeploymentPreflightRunID uuid.UUID                      `db:"deployment_preflight_run_id" json:"deploymentPreflightRunId"` //nolint:lll
	DeploymentPlanID         uuid.UUID                      `db:"deployment_plan_id" json:"deploymentPlanId"`
	DeploymentPlanTargetID   *uuid.UUID                     `db:"deployment_plan_target_id" json:"deploymentPlanTargetId,omitempty"` //nolint:lll
	DeploymentTargetID       *uuid.UUID                     `db:"deployment_target_id" json:"deploymentTargetId,omitempty"`
	TaskID                   *uuid.UUID                     `db:"task_id" json:"taskId,omitempty"`
	Component                string                         `db:"component" json:"component,omitempty"`
	CheckKey                 string                         `db:"check_key" json:"checkKey"`
	Status                   DeploymentPreflightCheckStatus `db:"status" json:"status"`
	Expected                 map[string]any                 `db:"expected" json:"expected"`
	Actual                   map[string]any                 `db:"actual" json:"actual"`
	Message                  string                         `db:"message" json:"message"`
	SortOrder                int                            `db:"sort_order" json:"sortOrder"`
}
